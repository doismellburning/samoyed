// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package dwcore

import (
	"fmt"

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

// PLLStats is how far a slicer's clock recovery has had to nudge its PLL, and
// over how many symbols, since the HDLC receiver last reset them: enough to
// work out how fast or slow the sender's clock runs.  The demodulator adds to
// it, and the HDLC receiver reads and resets it at each frame.
type PLLStats struct {
	NudgeTotal  int64 // Sum of DPLL nudge amounts.
	SymbolCount int   // Number of symbols NudgeTotal accumulated over.
}

// FrameSink takes each frame a receiver extracts, without its FCS, with the
// slicer that heard it and how.
type FrameSink func(from SlicerID, frame []byte, rxInfo RxInfo)

// PacketSink is FrameSink for a receiver that hands over packets it has
// already parsed, rather than raw frames.
type PacketSink func(from SlicerID, pp *ax25.Packet, rxInfo RxInfo)

// SourceKind is what a received frame came from: a radio channel's
// demodulators, or something standing in for one.
type SourceKind int

const (
	// SourceRadio is a radio channel's demodulators - or a beacon sent to
	// one as though heard on it.
	SourceRadio SourceKind = iota

	// SourceDTMF is a channel's touch tone decoder, via the APRStt gateway.
	SourceDTMF

	// SourceAPRSIS is the APRS-IS server, via the IGate.
	SourceAPRSIS

	// SourceNetTNC is a network TNC the channel is mapped to.
	SourceNetTNC

	// SourceAXUDP is another node, over AX.25 over UDP.
	SourceAXUDP

	// SourceSerialTNC is a serial TNC the channel is mapped to.
	SourceSerialTNC
)

// String names the kind of source as the web interface shows it.
func (k SourceKind) String() string {
	switch k {
	case SourceRadio:
		return "radio"
	case SourceDTMF:
		return "dtmf"
	case SourceAPRSIS:
		return "aprs-is"
	case SourceNetTNC:
		return "network"
	case SourceAXUDP:
		return "axudp"
	case SourceSerialTNC:
		return "serial"
	default:
		return fmt.Sprintf("SourceKind(%d)", int(k))
	}
}

// Source is where a received frame came from: which kind of source, on
// which channel, and - for SourceRadio - which slicer heard it.
type Source struct {
	Kind SourceKind

	// Slicer's Channel is the channel the frame arrived on, whatever its
	// kind.  Its Subchannel and Slice say which slicer heard it only for
	// SourceRadio, and are 0 otherwise.
	Slicer SlicerID
}

// RadioSource is a frame heard by the slicer from.
func RadioSource(from SlicerID) Source {
	return Source{Kind: SourceRadio, Slicer: from}
}

// ChannelSource is a frame that arrived on channel from a kind of source
// with no slicers of its own.
func ChannelSource(kind SourceKind, channel int) Source {
	return Source{Kind: kind, Slicer: SlicerID{Channel: channel, Subchannel: 0, Slice: 0}}
}

// FrameSender is where a packet to transmit goes, on the channel at the
// priority (PrioHigh or PrioLow): TransmitQueue.Append, or a stand-in for it.
type FrameSender func(channel int, prio int, pp *ax25.Packet)

// The transmit queue priorities a frame to send can be given.
const (
	// PrioHigh is for a frame to go out as soon as the channel is clear,
	// such as an APRS digipeater repeating on the channel it heard on.
	PrioHigh = 0

	// PrioLow is for everything else, which waits its turn.
	PrioLow = 1
)
