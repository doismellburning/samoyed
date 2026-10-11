// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package dwcore

import (
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/phy"
)

// SlicerID says which slicer, of which demodulator ("subchannel") of which
// radio channel, heard something.
type SlicerID struct {
	Channel    int
	Subchannel int
	Slice      int
}

// RxInfo is how a frame was heard: the audio level it arrived at, how much
// fixing up it took, and the FEC, if any, that carried it.
type RxInfo struct {
	ALevel  ax25.ALevel
	Retries phy.BitFixLevel
	FEC     phy.FECType
}

// AudioLevelFunc reports the audio level a subchannel's demodulator is
// hearing, which a receiver delivers each frame with.
type AudioLevelFunc func(channel int, subchannel int) ax25.ALevel

// FrameSink takes each frame a receiver extracts, without its FCS, with the
// slicer that heard it and how.
type FrameSink func(from SlicerID, frame []byte, rxInfo RxInfo)

// PacketSink is FrameSink for a receiver that hands over packets it has
// already parsed, rather than raw frames.
type PacketSink func(from SlicerID, pp *ax25.Packet, rxInfo RxInfo)
