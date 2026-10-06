// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package direwolf

import (
	"github.com/doismellburning/samoyed/internal/linecode"
)

/********************************************************************************
 *
 * Purpose:	Serialize EAS SAME for transmission.
 *
 *		SAME is not HDLC: it has no flags, bit stuffing or NRZI.
 *
 *******************************************************************************/

// EASSender sends EAS SAME messages on a channel's line: each byte as it is,
// least significant bit first, with no NRZI, so it leaves the NRZI level the
// line carries from one HDLC frame to the next alone.
type EASSender struct {
	line *linecode.Encoder

	bitsSent int // Count number of bits sent by SendMessage.
}

// NewEASSender makes an EASSender that sends on line.
func NewEASSender(line *linecode.Encoder) *EASSender {
	var s = new(EASSender)
	s.line = line

	return s
}

// SendMessage sends one repeat of str: the preamble, then the message.  It
// returns the number of bits sent.
func (s *EASSender) SendMessage(str []byte) int {
	s.bitsSent = 0

	for range 16 {
		s.putByte(0xAB)
	}

	for _, p := range str {
		s.putByte(p)
	}

	return s.bitsSent
}

func (s *EASSender) putByte(b byte) {
	for range 8 {
		s.line.Write(b&1 != 0, false)
		b >>= 1

		s.bitsSent++
	}
}
