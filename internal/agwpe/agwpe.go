// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package agwpe holds the wire format of the AGWPE TCP API, which Dire Wolf's
// network server speaks and the AGWPE client tools use to talk to it.
package agwpe

import (
	"encoding/binary"
	"io"

	"github.com/doismellburning/samoyed/internal/dwutil"
)

// Callsign is a callsign as an AGWPE header carries it, padded out with
// NULs.  String trims the padding, so it formats as the callsign with %s.
type Callsign [10]byte

func (c Callsign) String() string {
	return dwutil.ByteArrayToString(c[:])
}

type Header struct {
	Portx        byte
	Reserved1    byte
	Reserved2    byte
	Reserved3    byte
	DataKind     byte
	Reserved4    byte
	PID          byte
	Reserved5    byte
	CallFrom     Callsign
	CallTo       Callsign
	DataLen      uint32
	UserReserved [4]byte
}

type Message struct {
	Header Header
	Data   []byte
}

// Write sends msg to w. binary.Write won't send variable-length slices, and I keep forgetting that, so...
func (msg *Message) Write(w io.Writer, order binary.ByteOrder) (int, error) {
	var headerErr = binary.Write(w, order, msg.Header)
	if headerErr != nil {
		return 0, headerErr
	}

	if msg.Header.DataLen > 0 {
		return w.Write(msg.Data)
	}

	return 0, nil
}
