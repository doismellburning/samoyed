package direwolf

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

type FX25RecState int

const (
	FX_TAG FX25RecState = iota
	FX_DATA
	FX_CHECK
)

// fx25Receiver is the FX.25 receive state for one slicer of one demodulator
// ("subchannel") of one channel.
type fx25Receiver struct {
	channel, subchannel, slice int
	debug                      int             // FX.25's debug level.
	sink                       fx25_frame_sink // Where each extracted frame goes.

	state        FX25RecState
	accum        uint64 // Accumulate bits for matching to correlation tag.
	ctag_num     int    // Correlation tag number, CTAG_MIN to CTAG_MAX if approx. match found.
	k_data_radio int    // Expected size of "data" sent over radio.
	coffs        int    // Starting offset of the check part.
	nroots       int    // Expected number of check bytes.
	dlen         int    // Accumulated length in "data" below.
	clen         int    // Accumulated length in "check" below.
	imask        byte   // Mask for storing a bit.
	block        [FX25_BLOCK_SIZE + 1]byte
}

func newFX25Receiver(channel int, subchannel int, slice int, debug int, sink fx25_frame_sink) *fx25Receiver {
	dwutil.Assert(channel >= 0 && channel < MAX_RADIO_CHANS)
	dwutil.Assert(subchannel >= 0 && subchannel < MAX_SUBCHANS)
	dwutil.Assert(slice >= 0 && slice < MAX_SLICERS)

	var F = new(fx25Receiver)
	F.channel = channel
	F.subchannel = subchannel
	F.slice = slice
	F.debug = debug
	F.sink = sink

	return F
}

func (F *fx25Receiver) logEntry() *logrus.Entry {
	return logrus.WithFields(logrus.Fields{
		"channel":    F.channel,
		"subchannel": F.subchannel,
		"slice":      F.slice,
	})
}

/***********************************************************************************
 *
 * Name:        fx25Receiver.recBit
 *
 * Purpose:     Extract FX.25 codeblocks from a stream of bits.
 *		In a completely integrated AX.25 / FX.25 receive system,
 *		this would see the same bit stream as hdlcReceiver.RecBit.
 *
 * Inputs:      dbit	- Data bit after NRZI and any descrambling.
 *			  Any non-zero value is logic '1'.
 *
 * Description: This is called once for each received bit.
 *              Each valid frame is handed to the receiver's sink, which in
 *              normal operation is fx25_deliver_frame.
 *		It can gather multiple candidates from different parallel demodulators
 *		("subchannels") and slicers, then decide which one is the best.
 *
 ***********************************************************************************/

const FENCE = 0x55 // to detect buffer overflow.

// fx25_frame_sink is handed each AX.25 frame, with the FCS removed, extracted
// from the received bit stream, along with the number of bytes that the FEC
// decoder had to correct.
type fx25_frame_sink func(channel int, subchannel int, slice int, frame []byte, derrors int)

// fx25_deliver_frame is the sink used in normal operation, passing the frame on
// to the rest of the receive path.
func fx25_deliver_frame(channel int, subchannel int, slice int, frame []byte, derrors int) {
	var alevel = demod_get_audio_level(channel, subchannel)

	multi_modem_process_rec_frame(channel, subchannel, slice, frame, alevel, BitFixLevel(derrors), 1)
}

// Note that the sink is called before the state machine is reset, so that
// HDLCReceiver.fx25Busy still reports reception in progress during delivery.
func (F *fx25Receiver) recBit(dbit int) {
	// State machine to identify correlation tag then gather appropriate number of data and check bytes.

	switch F.state {
	case FX_TAG:
		F.accum >>= 1
		if dbit != 0 {
			F.accum |= 1 << 63
		}

		var c = fx25_tag_find_match(F.accum)
		if c >= CTAG_MIN && c <= CTAG_MAX {
			F.ctag_num = c
			F.k_data_radio = fx25_get_k_data_radio(F.ctag_num)
			F.nroots = fx25_get_nroots(F.ctag_num)
			F.coffs = fx25_get_k_data_rs(F.ctag_num)
			dwutil.Assert(F.coffs == FX25_BLOCK_SIZE-F.nroots)

			if F.debug >= 2 {
				F.logEntry().WithFields(logrus.Fields{
					"ctag":        c,
					"bit_errors":  bits.OnesCount(uint(F.accum ^ fx25_get_ctag_value(c))),
					"data_bytes":  F.k_data_radio,
					"check_bytes": F.nroots,
				}).Debug("FX.25: Matched correlation tag")
			}

			F.imask = 0x01
			F.dlen = 0
			F.clen = 0
			F.block = [FX25_BLOCK_SIZE + 1]byte{}
			F.block[FX25_BLOCK_SIZE] = FENCE
			F.state = FX_DATA
		}

	case FX_DATA:
		if dbit != 0 {
			F.block[F.dlen] |= F.imask
		}

		F.imask <<= 1
		if F.imask == 0 {
			F.imask = 0x01

			F.dlen++
			if F.dlen >= F.k_data_radio {
				F.state = FX_CHECK
			}
		}

	case FX_CHECK:
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
				F.state = FX_TAG
			}
		}
	}
}

/***********************************************************************************
 *
 * Name:        HDLCReceiver.fx25Busy
 *
 * Purpose:     Is FX.25 reception currently in progress?
 *
 * Inputs:      channel    - Channel number.
 *
 * Returns:	True if currently in progress for the specified channel.
 *
 * Description: This is required for duplicate removal.  One channel and can have
 *		multiple demodulators (called subchannels) running in parallel.
 *		Each of them can have multiple slicers.  Duplicates need to be
 *		removed.  Normally a delay of a couple bits (or more accurately
 *		symbols) was fine because they all took about the same amount of time.
 *		Now, we can have an additional delay of up to 64 check bytes and
 *		some filler in the data portion.  We can't simply wait that long.
 *		With normal AX.25 a couple frames can come and go during that time.
 *		We want to delay the duplicate removal while FX.25 block reception
 *		is going on.
 *
 ***********************************************************************************/

func (r *HDLCReceiver) fx25Busy(channel int) bool {
	dwutil.Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	if r == nil {
		return false
	}

	// This could be a little faster if we knew number of
	// subchannels and slicers but it is probably insignificant.

	for sub := range MAX_SUBCHANS {
		for slice := range MAX_SLICERS {
			var s = r.slicer[channel][sub][slice]
			if s != nil && s.fx25.busy() {
				return true
			}
		}
	}

	return false
}

// busy reports whether an FX.25 codeblock is part way through being received.
func (F *fx25Receiver) busy() bool {
	return F.state != FX_TAG
}

/***********************************************************************************
 *
 * Name:	fx25Receiver.processRSBlock
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

func (F *fx25Receiver) processRSBlock() {
	var channel = F.channel
	var subchannel = F.subchannel
	var slice = F.slice

	if F.debug >= 3 {
		F.logEntry().Debug("FX.25: Received RS codeblock")
		dwutil.LogHexDump(F.logEntry(), logrus.DebugLevel, F.block[:FX25_BLOCK_SIZE])
	}

	dwutil.Assert(F.block[FX25_BLOCK_SIZE] == FENCE)

	var rs = fx25_get_rs(F.ctag_num)

	var derrlocs, decodeErr = rs.Decode(F.block[:FX25_BLOCK_SIZE], nil)

	var derrors = len(derrlocs)
	if decodeErr != nil {
		derrors = -1
	}

	if derrors >= 0 { // -1 for failure.  >= 0 for success, number of bytes corrected.
		if F.debug >= 2 {
			F.logEntry().WithFields(logrus.Fields{
				"errors":    derrors,
				"positions": derrlocs,
			}).Debug("FX.25: FEC complete")
		}

		var frame_buf = my_unstuff(channel, subchannel, slice, F.block[:], F.dlen)
		var frame_len = len(frame_buf)

		if frame_len >= 14+1+2 { // Minimum length: Two addresses & control & FCS.
			var actual_fcs = uint16(frame_buf[frame_len-2]) | (uint16(frame_buf[frame_len-1]) << 8)

			var expected_fcs = fcs.Calc(frame_buf[:frame_len-2])
			if actual_fcs == expected_fcs {
				if F.debug >= 3 {
					F.logEntry().Debug("FX.25: Extracted AX.25 frame")
					dwutil.LogHexDump(F.logEntry(), logrus.DebugLevel, frame_buf[:frame_len])
				}

				F.sink(channel, subchannel, slice, frame_buf[:frame_len-2], derrors) /* len-2 to remove FCS. */
			} else {
				// Most likely cause is defective sender software.
				F.logEntry().Warn("FX.25: Bad FCS for AX.25 frame")
				dwutil.LogHexDump(F.logEntry(), logrus.WarnLevel, F.block[:F.dlen])
				dwutil.LogHexDump(F.logEntry(), logrus.WarnLevel, frame_buf[:frame_len])
			}
		} else {
			// Most likely cause is defective sender software.
			F.logEntry().Warn("FX.25: AX.25 frame is shorter than minimum length")
			dwutil.LogHexDump(F.logEntry(), logrus.WarnLevel, F.block[:F.dlen])
			dwutil.LogHexDump(F.logEntry(), logrus.WarnLevel, frame_buf)
		}
	} else if F.debug >= 2 {
		F.logEntry().Debug("FX.25: FEC failed.  Too many errors.")
	}
}

/***********************************************************************************
 *
 * Name:	my_unstuff
 *
 * Purpose:	Remove HDLC bit stuffing and surrounding flag delimiters.
 *
 * Inputs:      channel, subchannel, slice	- For error messages.
 *
 *		pin	- "data" part of RS codeblock.
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

func my_unstuff(channel int, subchannel int, slice int, pin []byte, ilen int) []byte {
	var logEntry = logrus.WithFields(logrus.Fields{
		"channel":    channel,
		"subchannel": subchannel,
		"slice":      slice,
	})

	var pat_det byte = 0 // Pattern detector.
	var oacc byte = 0    // Accumulator for a byte out.
	var olen = 0         // Number of good bits in oacc.

	if pin[0] != 0x7e {
		logEntry.Warn("FX.25: Data section did not start with 0x7e")
		dwutil.LogHexDump(logEntry, logrus.WarnLevel, pin[:ilen])

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
				logEntry.Warn("FX.25: Invalid AX.25 frame - Seven '1' bits in a row")
				dwutil.LogHexDump(logEntry, logrus.WarnLevel, pin[i:ilen])

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
						logEntry.Warn("FX.25: Invalid AX.25 frame - Not a whole number of bytes")
						dwutil.LogHexDump(logEntry, logrus.WarnLevel, pin[i:ilen])

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

	logEntry.Warn("FX.25: Invalid AX.25 frame - Terminating flag not found")
	dwutil.LogHexDump(logEntry, logrus.WarnLevel, pin[:ilen])

	return nil // Should never fall off the end.
}
