// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package fx25

import (
	"github.com/doismellburning/samoyed/internal/bitstuff"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/fcs"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/sirupsen/logrus"
)

// Sender sends FX.25 frames on a channel's line.  An FX.25 codeblock
// carries a complete HDLC frame, so goes out NRZI like one, carrying on from
// whatever level the last frame left the line at, but with no further bit
// stuffing: the frame inside was stuffed before it was encoded.
type Sender struct {
	line    *linecode.Encoder
	channel int // For logging.
	debug   int // FX.25's debug level.

	bitsSent int // Count number of bits sent by SendFrame.
}

// NewSender makes a Sender for channel that sends on line, with
// FX.25's debug level at debug.
func NewSender(line *linecode.Encoder, channel int, debug int) *Sender {
	var s = new(Sender)
	s.line = line
	s.channel = channel
	s.debug = debug

	return s
}

// Debug returns the sender's FX.25 debug level.
func (s *Sender) Debug() int {
	return s.debug
}

/*-------------------------------------------------------------
 *
 * Name:	SendFrame (fx25_send_frame in Dire Wolf)
 *
 * Purpose:	Convert HDLC frames to a stream of bits.
 *
 * Inputs:	fbuf	- Frame buffer address.
 *
 *		fx_mode	- Normally, this would be 16, 32, or 64 for the desired number
 *			  of check bytes.  The shortest format, adequate for the
 *			  required data length will be picked automatically.
 *			  0x01 thru 0x0b may also be specified for a specific format
 *			  but this is expected to be mostly for testing, not normal
 *			  operation.
 *
 * Outputs:	Bits are shipped out to the sender's tone generator.
 *
 * Returns:	Number of bits sent including "flags" and the
 *		stuffing bits.
 *		The required time can be calculated by dividing this
 *		number by the transmit rate of bits/sec.
 *		-1 is returned for failure.
 *
 * Description:	Generate an AX.25 frame in the usual way then wrap
 *		it inside of the FX.25 correlation tag and check bytes.
 *
 * Assumptions:	It is assumed that the tone_gen module has been
 *		properly initialized so that bits sent with
 *		the tone generator are processed correctly.
 *
 * Errors:	If something goes wrong, return -1 and the caller should
 *		fallback to sending normal AX.25.
 *
 *		This could happen if the frame is too large.
 *
 *--------------------------------------------------------------*/

func (s *Sender) SendFrame(fbuf []byte, fx_mode int) int {
	var ctag_num, data, check = fx25_encode_frame(s.channel, fbuf, fx_mode, s.debug)
	if ctag_num < CTAG_MIN {
		return (-1)
	}

	// The correlation tag, least significant byte first, then the data
	// and check bytes of the codeblock.
	var ctag_value = fx25_get_ctag_value(ctag_num)

	var frame = make([]byte, 0, 8+len(data)+len(check))
	for k := range 8 {
		frame = append(frame, byte((ctag_value>>(k*8))&0xff))
	}

	frame = append(frame, data...)
	frame = append(frame, check...)

	s.bitsSent = 0

	s.sendBytes(frame)

	return s.bitsSent
}

/*-------------------------------------------------------------
 *
 * Name:	fx25_encode_frame
 *
 * Purpose:	Wrap an AX.25 frame up as an FX.25 codeblock.
 *
 * Inputs:	channel, fx_mode - As for Sender.SendFrame.
 *
 *		debug	- FX.25's debug level.
 *
 *		fbuf	- Frame buffer, without the FCS.
 *
 * Returns:	The correlation tag number, the "data" part to be transmitted,
 *		and the check bytes.
 *		The tag number is -1, and the other two are nil, for failure.
 *
 *--------------------------------------------------------------*/

func fx25_encode_frame(channel int, fbuf []byte, fx_mode int, debug int) (int, []byte, []byte) {
	var logEntry = logrus.WithField("channel", channel)

	if debug >= 3 {
		logEntry.WithField("fx_mode", fx_mode).Debug("FX.25: send frame")
		dwutil.LogHexDump(logEntry, logrus.DebugLevel, fbuf)
	}

	// Append the FCS.

	var frameFCS = fcs.Calc(fbuf)
	fbuf = append(fbuf, byte(frameFCS&0xff))
	fbuf = append(fbuf, byte((frameFCS>>8)&0xff))

	// Add bit-stuffing, filling to MaxData bytes with flag patterns
	var stuffedBytes, meaningfulLen = bitstuff.Stuff(fbuf, MaxData)
	var dlen = meaningfulLen // Use meaningful length, not total buffer size

	// Pick suitable correlation tag depending on
	// user's preference, for number of check bytes,
	// and the data size.
	var ctag_num = fx25_pick_mode(fx_mode, dlen)

	if ctag_num < CTAG_MIN || ctag_num > CTAG_MAX {
		logEntry.WithFields(logrus.Fields{
			"fx_mode": fx_mode,
			"dlen":    dlen,
		}).Warn("FX.25: Could not find suitable format for requested mode and data length")

		return -1, nil, nil
	}

	var k_data_radio = fx25_get_k_data_radio(ctag_num)
	var k_data_rs = fx25_get_k_data_rs(ctag_num)

	// Zero out part of data which won't be transmitted
	var shorten_by = MaxData - k_data_radio
	if shorten_by > 0 {
		for i := k_data_radio; i < MaxData; i++ {
			stuffedBytes[i] = 0
		}
	}

	var data = stuffedBytes

	// Compute the check bytes.

	var rs = fx25_get_rs(ctag_num)
	var nroots = rs.NRoots()

	dwutil.Assert(k_data_rs+nroots == rs.N())

	var check = rs.Encode(data[:k_data_rs])

	if debug >= 3 {
		logEntry.WithFields(logrus.Fields{
			"data_bytes": k_data_radio,
			"ctag":       ctag_num,
		}).Debug("FX.25: transmit data bytes")
		dwutil.LogHexDump(logEntry, logrus.DebugLevel, data[:k_data_radio])
		logEntry.WithField("check_bytes", nroots).Debug("FX.25: transmit check bytes")
		dwutil.LogHexDump(logEntry, logrus.DebugLevel, check[:nroots])
	}

	return ctag_num, data[:k_data_radio], check[:nroots]
}

// sendBytes sends NRZI, least significant bit first, with no stuffing: the
// codeblock was stuffed before it was encoded.  It shares the line level
// with AX.25, since the receiver sees only the one line.
func (s *Sender) sendBytes(b []byte) {
	for _, x := range b {
		for range 8 {
			s.line.WriteNRZI(x&0x01 != 0)
			x >>= 1

			s.bitsSent++
		}
	}
}
