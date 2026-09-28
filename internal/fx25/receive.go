// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package fx25

/********************************************************************************
 *
 * Purpose:     Extract FX.25 codeblocks from a stream of bits and process them.
 *
 *******************************************************************************/

import (
	"math/bits"

	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/fcs"
	"github.com/sirupsen/logrus"
)

type rxState int

const (
	rxTag rxState = iota
	rxData
	rxCheck
)

// Receiver is the FX.25 receive state for one slicer of one demodulator
// ("subchannel") of one channel.
type Receiver struct {
	channel, subchannel, slice int
	debug                      int       // FX.25's debug level.
	sink                       FrameSink // Where each extracted frame goes.

	state        rxState
	accum        uint64 // Accumulate bits for matching to correlation tag.
	ctag_num     int    // Correlation tag number, CTAG_MIN to CTAG_MAX if approx. match found.
	k_data_radio int    // Expected size of "data" sent over radio.
	coffs        int    // Starting offset of the check part.
	nroots       int    // Expected number of check bytes.
	dlen         int    // Accumulated length in "data" below.
	clen         int    // Accumulated length in "check" below.
	imask        byte   // Mask for storing a bit.
	block        [blockSize + 1]byte
}

// NewReceiver makes a receiver for one slicer of one demodulator of one
// channel, at FX.25 debug level debug, handing each frame it extracts to
// sink.  The channel, subchannel and slice are for the sink and log entries;
// the receiver makes no other use of them.
func NewReceiver(channel int, subchannel int, slice int, debug int, sink FrameSink) *Receiver {
	dwutil.Assert(channel >= 0 && subchannel >= 0 && slice >= 0)

	var F = new(Receiver)
	F.channel = channel
	F.subchannel = subchannel
	F.slice = slice
	F.debug = debug
	F.sink = sink

	return F
}

// Debug is the FX.25 debug level the receiver was made with.
func (F *Receiver) Debug() int {
	return F.debug
}

/***********************************************************************************
 *
 * Name:        Receiver.RecBit
 *
 * Purpose:     Extract FX.25 codeblocks from a stream of bits.
 *		In a completely integrated AX.25 / FX.25 receive system,
 *		this would see the same bit stream as the HDLC receiver.
 *
 * Inputs:      dbit	- Data bit after NRZI and any descrambling.
 *			  Any non-zero value is logic '1'.
 *
 * Description: This is called once for each received bit.
 *              Each valid frame is handed to the receiver's sink.
 *		It can gather multiple candidates from different parallel demodulators
 *		("subchannels") and slicers, then decide which one is the best.
 *
 ***********************************************************************************/

const fence = 0x55 // to detect buffer overflow.

// FrameSink is handed each AX.25 frame, with the FCS removed, extracted
// from the received bit stream, along with the number of bytes that the FEC
// decoder had to correct.
type FrameSink func(channel int, subchannel int, slice int, frame []byte, derrors int)

// RecBit takes the next bit, after NRZI and any descrambling.
//
// Note that the sink is called before the state machine is reset, so that
// Busy still reports reception in progress during delivery.
func (F *Receiver) RecBit(dbit int) {
	// State machine to identify correlation tag then gather appropriate number of data and check bytes.

	switch F.state {
	case rxTag:
		F.accum >>= 1
		if dbit != 0 {
			F.accum |= 1 << 63
		}

		var c = findTag(F.accum)
		if c >= CTagMin && c <= CTagMax {
			F.ctag_num = c
			F.k_data_radio = kDataRadio(F.ctag_num)
			F.nroots = nRoots(F.ctag_num)
			F.coffs = kDataRS(F.ctag_num)
			dwutil.Assert(F.coffs == blockSize-F.nroots)

			if F.debug >= 2 {
				F.log().WithFields(logrus.Fields{
					"ctag":        c,
					"bit_errors":  bits.OnesCount(uint(F.accum ^ TagValue(c))),
					"data_bytes":  F.k_data_radio,
					"check_bytes": F.nroots,
				}).Debug("FX.25: Matched correlation tag")
			}

			F.imask = 0x01
			F.dlen = 0
			F.clen = 0
			F.block = [blockSize + 1]byte{}
			F.block[blockSize] = fence
			F.state = rxData
		}

	case rxData:
		if dbit != 0 {
			F.block[F.dlen] |= F.imask
		}

		F.imask <<= 1
		if F.imask == 0 {
			F.imask = 0x01

			F.dlen++
			if F.dlen >= F.k_data_radio {
				F.state = rxCheck
			}
		}

	case rxCheck:
		if dbit != 0 {
			F.block[F.coffs+F.clen] |= F.imask
		}

		F.imask <<= 1
		if F.imask == 0 {
			F.imask = 0x01

			F.clen++
			if F.clen >= F.nroots {
				F.processRSBlock() // see below

				F.ctag_num = -1
				F.accum = 0
				F.state = rxTag
			}
		}
	}
}

// Busy reports whether an FX.25 codeblock is part way through being received.
func (F *Receiver) Busy() bool {
	return F.state != rxTag
}

/***********************************************************************************
 *
 * Name:	Receiver.processRSBlock
 *
 * Purpose:     After the correlation tag was detected and the appropriate number
 *		of data and check bytes are accumulated, this performs the processing
 *
 * Inputs:	F.ctag_num	- Correlation tag number  (index into table)
 *
 *		F.dlen		- Number of "data" bytes.
 *
 *		F.clen		- Number of "check" bytes"
 *
 *		F.block	- Codeblock.  Always 255 total bytes.
 *				  Anything left over after data and check
 *				  bytes is filled with zeros.
 *
 *		<- - - - - - - - - - - 255 bytes total - - - - - - - - ->
 *		+-----------------------+---------------+---------------+
 *		|  dlen bytes "data"    |  zero fill    |  check bytes  |
 *		+-----------------------+---------------+---------------+
 *
 * Description:	Use Reed-Solomon decoder to fix up any errors.
 *		Extract the AX.25 frame from the corrected data and hand it to sink.
 *
 ***********************************************************************************/

func (F *Receiver) processRSBlock() {
	var channel = F.channel
	var subchannel = F.subchannel
	var slice = F.slice

	if F.debug >= 3 {
		F.log().Debug("FX.25: Received RS codeblock")
		dwutil.HexDump(F.block[:blockSize])
	}

	dwutil.Assert(F.block[blockSize] == fence)

	var rs = codecFor(F.ctag_num)

	var derrlocs, decodeErr = rs.Decode(F.block[:blockSize], nil)

	var derrors = len(derrlocs)
	if decodeErr != nil {
		derrors = -1
	}

	if derrors >= 0 { // -1 for failure.  >= 0 for success, number of bytes corrected.
		if F.debug >= 2 {
			if derrors == 0 {
				F.log().Debug("FX.25: FEC complete with no errors")
			} else {
				F.log().WithField("positions", derrlocs).Debug("FX.25: FEC complete, fixed errors")
			}
		}

		var frame_buf = F.unstuff(F.block[:], F.dlen)
		var frame_len = len(frame_buf)

		if frame_len >= 14+1+2 { // Minimum length: Two addresses & control & FCS.
			var actual_fcs = uint16(frame_buf[frame_len-2]) | (uint16(frame_buf[frame_len-1]) << 8)

			var expected_fcs = fcs.Calc(frame_buf[:frame_len-2])
			if actual_fcs == expected_fcs {
				if F.debug >= 3 {
					F.log().Debug("FX.25: Extracted AX.25 frame")
					dwutil.HexDump(frame_buf[:frame_len])
				}

				F.sink(channel, subchannel, slice, frame_buf[:frame_len-2], derrors) /* len-2 to remove FCS. */
			} else {
				// Most likely cause is defective sender software.
				F.log().Warn("FX.25: Bad FCS for AX.25 frame")
				dwutil.HexDump(F.block[:F.dlen])
				dwutil.HexDump(frame_buf[:frame_len])
			}
		} else {
			// Most likely cause is defective sender software.
			F.log().Warn("FX.25: AX.25 frame is shorter than minimum length")
			dwutil.HexDump(F.block[:F.dlen])

			if frame_len > 0 {
				dwutil.HexDump(frame_buf[:frame_len])
			}
		}
	} else if F.debug >= 2 {
		F.log().Debug("FX.25: FEC failed, too many errors")
	}
}

/***********************************************************************************
 *
 * Name:	Receiver.unstuff
 *
 * Purpose:	Remove HDLC bit stuffing and surrounding flag delimiters.
 *
 * Inputs:	pin	- "data" part of RS codeblock.
 *			  First byte must be HDLC "flag".
 *			  May be followed by additional flags.
 *			  There must be terminating flag but it might not be byte aligned.
 *
 *		ilen	- Number of bytes in pin.
 *
 * Outputs:	frame_buf - Frame contents including FCS.
 *			    Bit stuffing is gone so it should be a whole number of bytes.
 *
 * Returns:	Number of bytes in frame_buf, including 2 for FCS.
 *		This can never be larger than the max "data" size.
 *		0 if any error.
 *
 * Errors:	First byte is not not flag.
 *		Found seven '1' bits in a row.
 *		Result is not whole number of bytes after removing bit stuffing.
 *		Trailing flag not found.
 *		Most likely cause, for all of these, is defective sender software.
 *
 ***********************************************************************************/

func (F *Receiver) unstuff(pin []byte, ilen int) []byte {
	var pat_det byte = 0 // Pattern detector.
	var oacc byte = 0    // Accumulator for a byte out.
	var olen = 0         // Number of good bits in oacc.

	if pin[0] != 0x7e {
		F.log().Warn("FX.25: Data section did not start with 0x7e")
		dwutil.HexDump(pin[:ilen])

		return nil
	}

	for ilen > 0 && pin[0] == 0x7e {
		ilen--
		pin = pin[1:] // Skip over leading flag byte(s).
	}

	var frame_buf []byte
	for i := range ilen {
		for imask := byte(0x01); imask != 0; imask <<= 1 {
			var dbit = byte(dwutil.IfThenElse((pin[i]&imask) != 0, 1, 0))

			pat_det >>= 1 // Shift the most recent eight bits thru the pattern detector.
			pat_det |= dbit << 7

			if pat_det == 0xfe {
				F.log().Warn("FX.25: Invalid AX.25 frame - Seven '1' bits in a row")
				dwutil.HexDump(pin[i:ilen])

				return nil
			}

			if dbit != 0 {
				oacc >>= 1
				oacc |= 0x80
			} else {
				if pat_det == 0x7e { // "flag" pattern - End of frame.
					if olen == 7 {
						return frame_buf // Whole number of bytes in result including CRC
					} else {
						F.log().Warn("FX.25: Invalid AX.25 frame - Not a whole number of bytes")
						dwutil.HexDump(pin[i:ilen])

						return nil
					}
				} else if (pat_det >> 2) == 0x1f {
					continue // Five '1' bits in a row, followed by '0'.  Discard the '0'.
				}

				oacc >>= 1
			}

			olen++
			if olen&8 != 0 {
				olen = 0

				frame_buf = append(frame_buf, oacc)
			}
		}
	} /* end of loop on all bits in block */

	F.log().Warn("FX.25: Invalid AX.25 frame - Terminating flag not found")
	dwutil.HexDump(pin[:ilen])

	return nil // Should never fall off the end.
}

// log is a logrus entry naming where the receiver sits.
func (F *Receiver) log() *logrus.Entry {
	return logrus.WithFields(logrus.Fields{
		"channel":    F.channel,
		"subchannel": F.subchannel,
		"slice":      F.slice,
	})
}
