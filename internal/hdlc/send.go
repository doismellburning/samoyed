// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package hdlc

import (
	"github.com/doismellburning/samoyed/internal/fcs"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/sirupsen/logrus"
)

// Sender sends AX.25 frames as HDLC on a channel's line: between flags,
// bit stuffed, with the FCS appended, and NRZI.  It holds the run of ones
// that decides when to bit stuff; the line holds the NRZI level.
type Sender struct {
	line    *linecode.Encoder
	channel int // For logging.

	bitsSent int // Count number of bits sent by SendFrame or SendFlags.

	// Count number of "1" bits to keep track of when we need to break up a
	// long run by "bit stuffing."
	stuff int
}

// NewSender makes a Sender for channel that sends on line.
func NewSender(line *linecode.Encoder, channel int) *Sender {
	var s = new(Sender)
	s.line = line
	s.channel = channel

	return s
}

// SendFlags sends nbytes of the 01111110 flag pattern, NRZI and with no bit
// stuffing, which is what the transmitter sends before, between and after
// frames.  It returns the number of bits sent.
func (s *Sender) SendFlags(nbytes int) int {
	s.bitsSent = 0

	for range nbytes {
		s.sendControlNRZI(0x7e)
	}

	return s.bitsSent
}

// SendFrame is ax25_only_hdlc_send_frame in Dire Wolf.  It sends fbuf, an
// AX.25 frame without its FCS, and returns the number of bits sent.  badFCS
// sends a corrupt FCS instead of the right one, for testing.
func (s *Sender) SendFrame(fbuf []byte, badFCS bool) int {
	s.bitsSent = 0

	logrus.WithFields(logrus.Fields{
		"channel": s.channel,
		"flen":    len(fbuf),
		"bad_fcs": badFCS,
	}).Debug("hdlc_send_frame")

	s.sendControlNRZI(0x7e) /* Start frame */

	for j := range fbuf {
		s.sendDataNRZI(fbuf[j])
	}

	var frameFCS = fcs.Calc(fbuf)

	if badFCS {
		/* For testing only - Simulate a frame getting corrupted along the way. */
		s.sendDataNRZI(byte(^frameFCS & 0xff))
		s.sendDataNRZI(byte((^frameFCS)>>8) & 0xff)
	} else {
		s.sendDataNRZI(byte(frameFCS & 0xff))
		s.sendDataNRZI(byte((frameFCS >> 8) & 0xff))
	}

	s.sendControlNRZI(0x7e) /* End frame */

	return s.bitsSent
}

// The following are only for HDLC.
// All bits are sent NRZI.
// Data (non flags) use bit stuffing.

func (s *Sender) sendControlNRZI(x byte) {
	for range 8 {
		s.sendBitNRZI(x&1 != 0)
		x >>= 1
	}

	s.stuff = 0
}

func (s *Sender) sendDataNRZI(x byte) {
	for range 8 {
		s.sendBitNRZI(x&1 != 0)

		if x&1 > 0 {
			s.stuff++
			if s.stuff == 5 {
				s.sendBitNRZI(false)
				s.stuff = 0
			}
		} else {
			s.stuff = 0
		}

		x >>= 1
	}
}

/*
 * NRZI encoding.
 * data 1 bit -> no change.
 * data 0 bit -> invert signal.
 */

func (s *Sender) sendBitNRZI(b bool) {
	s.line.WriteNRZI(b)

	s.bitsSent++
}

/* end hdlc_send.c */
