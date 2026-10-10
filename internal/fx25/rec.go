// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package fx25

/********************************************************************************
 *
 * Purpose:     Extract FX.25 codeblocks from a stream of bits and process them.
 *
 *******************************************************************************/

import (
	"math/bits"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/bitstuff"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/fcs"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/sirupsen/logrus"
)

type FX25RecState int

const (
	FX_TAG FX25RecState = iota
	FX_DATA
	FX_CHECK
)

// AudioLevelFunc reports the audio level a subchannel's demodulator is
// hearing, which the receiver delivers each frame with.
type AudioLevelFunc func(channel int, subchannel int) ax25.ALevel

// FrameSink takes each frame the receiver extracts, without its FCS, with the
// audio level it was heard at, how many bytes the Reed-Solomon decoder
// corrected, and phy.FECFX25.
type FrameSink func(channel int, subchannel int, slice int, frame []byte, alevel ax25.ALevel, retries phy.BitFixLevel, fecType phy.FECType)

// Receiver is the FX.25 receive state for one slicer of one demodulator
// ("subchannel") of one channel.
type Receiver struct {
	channel, subchannel, slice int
	debug                      int            // FX.25's debug level.
	audioLevel                 AudioLevelFunc // For the audio level to deliver each frame with.
	sink                       FrameSink      // Where each extracted frame goes.

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

// NewReceiver makes a Receiver for one slicer, reporting at FX.25 debug level
// debug.  It gives each frame it extracts to sink, with the level audioLevel
// reports.
func NewReceiver(channel int, subchannel int, slice int, debug int, audioLevel AudioLevelFunc, sink FrameSink) *Receiver {
	dwutil.Assert(channel >= 0 && channel < phy.MaxRadioChans)
	dwutil.Assert(subchannel >= 0 && subchannel < phy.MaxSubchans)
	dwutil.Assert(slice >= 0 && slice < phy.MaxSlicers)

	var F = new(Receiver)
	F.channel = channel
	F.subchannel = subchannel
	F.slice = slice
	F.debug = debug
	F.audioLevel = audioLevel
	F.sink = sink

	return F
}

// Debug returns the receiver's FX.25 debug level.
func (F *Receiver) Debug() int {
	return F.debug
}

func (F *Receiver) logEntry() *logrus.Entry {
	return logrus.WithFields(logrus.Fields{
		"channel":    F.channel,
		"subchannel": F.subchannel,
		"slice":      F.slice,
	})
}

/***********************************************************************************
 *
 * Name:        Receiver.RecBit
 *
 * Purpose:     Extract FX.25 codeblocks from a stream of bits.
 *		In a completely integrated AX.25 / FX.25 receive system,
 *		this would see the same bit stream as layer2Receiver.RecBit.
 *
 * Inputs:      dbit	- Data bit after NRZI and any descrambling.
 *			  Any non-zero value is logic '1'.
 *
 * Description: This is called once for each received bit.
 *              Each valid frame is handed to the receiver's sink, which in
 *              normal operation is the layer 2 receiver's recFrame.
 *		It can gather multiple candidates from different parallel demodulators
 *		("subchannels") and slicers, then decide which one is the best.
 *
 ***********************************************************************************/

const FENCE = 0x55 // to detect buffer overflow.

// RecBit takes the next data bit, as above.  It calls the sink before it
// resets the state machine, so that Busy still reports reception in progress,
// as Layer2Receiver.fx25Busy relies on, during delivery.
func (F *Receiver) RecBit(dbit int) {
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

// Busy reports whether an FX.25 codeblock is part way through being received.
func (F *Receiver) Busy() bool {
	return F.state != FX_TAG
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

		var frame_buf, err = bitstuff.Unstuff(F.block[:F.dlen])
		if err != nil {
			// Most likely cause is defective sender software.
			F.logEntry().WithError(err).Warn("FX.25: Invalid AX.25 frame")
			dwutil.LogHexDump(F.logEntry(), logrus.WarnLevel, F.block[:F.dlen])

			return
		}

		var frame_len = len(frame_buf)

		if frame_len >= 14+1+2 { // Minimum length: Two addresses & control & FCS.
			var actual_fcs = uint16(frame_buf[frame_len-2]) | (uint16(frame_buf[frame_len-1]) << 8)

			var expected_fcs = fcs.Calc(frame_buf[:frame_len-2])
			if actual_fcs == expected_fcs {
				if F.debug >= 3 {
					F.logEntry().Debug("FX.25: Extracted AX.25 frame")
					dwutil.LogHexDump(F.logEntry(), logrus.DebugLevel, frame_buf[:frame_len])
				}

				// The number of bytes the FEC decoder had to correct stands
				// for how much fixing the frame took.
				F.sink(channel, subchannel, slice, frame_buf[:frame_len-2], F.audioLevel(channel, subchannel), phy.BitFixLevel(derrors), phy.FECFX25) /* len-2 to remove FCS. */
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
