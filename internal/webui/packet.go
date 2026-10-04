// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package webui

import (
	"github.com/doismellburning/samoyed/internal/aprs"
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
)

// NewPacket describes a frame for the web interface.  A is its APRS decode, or
// nil if it isn't APRS.  The caller fills in what only it knows: the channel,
// and for a received frame where it came from and its audio level.
func NewPacket(pp *ax25.Packet, A *aprs.Decoded, direction Direction) Packet {
	var p Packet
	p.Direction = direction
	p.Monitor = pp.FormatAddrs() + string(pp.Info())

	if A == nil {
		if pp.NumAddr() > 0 {
			p.Station = pp.AddrWithSSID(ax25.Source)
		}

		return p
	}

	// An object or item is filed under its own name, so its position
	// doesn't move the station that sent it (Dire Wolf issue 545).
	p.Station = A.Src
	if A.Name != "" && (A.PacketType == aprs.PacketTypeObject || A.PacketType == aprs.PacketTypeItem) {
		p.Station = A.Name
	}

	p.Description = A.DataTypeDesc
	p.Comment = A.Comment
	p.Position = maybe.LiftA2(func(lat, lon float64) Position {
		return Position{Lat: lat, Lon: lon}
	}, A.Lat, A.Lon)

	if A.SymbolTable != 0 && A.SymbolCode != 0 {
		p.Symbol = string([]byte{A.SymbolTable, A.SymbolCode})
	}

	return p
}
