// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package phy

// Medium is the kind of communication medium associated with a channel.
type Medium int

const (
	MediumNone      Medium = iota // Channel is not valid for use.
	MediumRadio                   // Internal modem for radio.
	MediumIGate                   // Access IGate as ordinary channel.
	MediumNetTNC                  // Remote network TNC.  (new in 1.8)
	MediumSerialTNC               // Local serial TNC.  (new in 1.8)
	MediumAXUDP                   // AX.25 over UDP to other nodes.
)

// CarriesAX25 reports whether a channel of this medium sends and receives
// AX.25 frames itself: a radio, a network or serial KISS TNC, or AXUDP.
// An IGate channel, which hands packets to APRS-IS instead, and an unused
// channel do not.
//
// The switch names every Medium and has no default, so the exhaustive linter
// asks a new one to say which side it falls on.
func (m Medium) CarriesAX25() bool {
	switch m {
	case MediumRadio, MediumNetTNC, MediumSerialTNC, MediumAXUDP:
		return true
	case MediumNone, MediumIGate:
		return false
	}

	return false
}
