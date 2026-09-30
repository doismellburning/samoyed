// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package kiss implements the framing of the KISS TNC protocol, described in
// http://www.ka9q.net/papers/kiss.html
//
// Briefly, a frame is composed of
//
//   - FEND (0xC0)
//   - Contents - with special escape sequences so a 0xC0 byte in the data is
//     not taken as end of frame.
//   - FEND
//
// The first byte of the frame contains the radio channel in the upper nybble
// (the KISS doc says "port", but that has too many meanings) and the command
// in the lower nybble.
//
// Commands from application to TNC:
//
//	_0	Data Frame	AX.25 frame in raw format.
//	_1	TXDELAY		See explanation in xmit.go.
//	_2	Persistence	"	"
//	_3	SlotTime	"	"
//	_4	TXtail		"	"
//				Spec says it is obsolete but Xastir
//				sends it and we respect it.
//	_5	FullDuplex	Full Duplex.  Transmit immediately without
//				waiting for channel to be clear.
//	_6	SetHardware	TNC specific.
//	_C	XKISS extension - not supported.
//	_E	XKISS extension - not supported.
//	FF	Return		Exit KISS mode.  Ignored.
//
// Messages sent to client application:
//
//	_0	Data Frame	Received AX.25 frame in raw format.
//	_6	SetHardware	TNC specific.
//				Usually a response to a query.
//
// An extended form, to handle multiple TNCs on a single serial port, is
// described in http://he.fi/pub/oh7lzb/bpq/multi-kiss.pdf but not supported.
package kiss

import (
	"bytes"
	"fmt"

	"github.com/sirupsen/logrus"
)

// Commands, carried in the lower nybble of a frame's first byte.
const (
	CmdDataFrame   = 0
	CmdTxDelay     = 1
	CmdPersistence = 2
	CmdSlotTime    = 3
	CmdTxTail      = 4
	CmdFullDuplex  = 5
	CmdSetHardware = 6
	CmdXKissData   = 12 // Not supported. http://he.fi/pub/oh7lzb/bpq/multi-kiss.pdf
	CmdXKissPoll   = 14 // Not supported.
	CmdEndKiss     = 15
)

// Special characters used by SLIP protocol.
const (
	FEND  = 0xC0
	FESC  = 0xDB
	TFEND = 0xDC
	TFESC = 0xDD
)

// Encapsulate wraps a frame in FENDs, escaping any FEND or FESC within it.
//
// The first byte of in is the "type indicator" with type and channel, but
// that doesn't matter here: if it happens to be FEND or FESC, it is escaped
// like any other byte.
//
// in is "binary" data and can contain nul (0x00) values - don't treat it like
// a text string. The result is at most twice the length of in, plus 2.
func Encapsulate(in []byte) []byte {
	var buf bytes.Buffer

	buf.WriteByte(FEND)

	for _, b := range in {
		switch b {
		case FEND:
			buf.WriteByte(FESC)
			buf.WriteByte(TFEND)
		case FESC:
			buf.WriteByte(FESC)
			buf.WriteByte(TFESC)
		default:
			buf.WriteByte(b)
		}
	}

	buf.WriteByte(FEND)

	return buf.Bytes()
}

// Unwrap extracts the original data from a KISS frame: an optional leading
// FEND, the escaped data, and a trailing FEND.
//
// The first byte of the result is the "type indicator" with type and channel;
// it's treated like any other byte here.
//
// This is for a live TNC, which has to do something with whatever it was
// given, so a malformed frame is logged and unwrapped as well as it can be
// rather than rejected.
func Unwrap(in []byte) []byte {
	if len(in) < 2 {
		/* Need at least the "type indicator" byte and FEND. */
		/* Probably more. */
		logrus.WithField("length", len(in)).Error("KISS message less than minimum length.")

		return []byte{}
	}

	if in[len(in)-1] == FEND {
		in = in[:len(in)-1] // Ignore last FEND
	} else {
		logrus.Error("KISS frame should end with FEND.")
	}

	if in[0] == FEND {
		in = in[1:] // Skip over optional leading FEND
	}

	var escapedMode = false
	var buf bytes.Buffer

	for _, b := range in {
		if b == FEND {
			logrus.Error("KISS frame should not have FEND in the middle.")
		}

		if escapedMode {
			switch b {
			case TFESC:
				buf.WriteByte(FESC)
			case TFEND:
				buf.WriteByte(FEND)
			default:
				logrus.Errorf("KISS protocol error.  Found 0x%02x after FESC.", b)
			}

			escapedMode = false
		} else if b == FESC {
			escapedMode = true
		} else {
			buf.WriteByte(b)
		}
	}

	return buf.Bytes()
}

// Unescape undoes the KISS transposition of FEND and FESC in the contents of
// one frame, without the surrounding FENDs, returning the original bytes and
// everything wrong with the escaping.
//
// Unwrap does this for a live TNC, where carrying on with a complaint is the
// right thing to do. Something inspecting a capture instead wants to know
// exactly where a bad escape sequence is, and to decide for itself how to
// report it, so the problems are returned rather than logged.
//
// Recovery differs too: Unwrap drops an unexpected byte after FESC, where
// this keeps it, so that what is described accounts for every byte of the
// capture.
func Unescape(in []byte) ([]byte, []error) {
	var out bytes.Buffer

	var problems []error

	for i := 0; i < len(in); i++ {
		if in[i] != FESC {
			out.WriteByte(in[i])

			continue
		}

		if i == len(in)-1 {
			problems = append(problems, fmt.Errorf("frame ends with FESC (0x%02x) at offset %d - the escaped byte is missing", FESC, i))

			break
		}

		i++

		switch in[i] {
		case TFEND:
			out.WriteByte(FEND)
		case TFESC:
			out.WriteByte(FESC)
		default:
			problems = append(problems, fmt.Errorf("FESC (0x%02x) at offset %d is followed by 0x%02x, not TFEND (0x%02x) or TFESC (0x%02x) - taking it literally",
				FESC, i-1, in[i], TFEND, TFESC))

			out.WriteByte(in[i])
		}
	}

	return out.Bytes(), problems
}
