// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package il2p

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FuzzIL2PDecodeFrame covers the IL2P receive path: header FEC, descrambling
// and the payload blocks.
func FuzzIL2PDecodeFrame(f *testing.F) {
	testutils.DiscardLogrus(f)

	Init(0)

	var pp = ax25.FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)

	for _, version := range []Version{Version04, Version06} {
		var encoded, length = il2p_encode_frame(pp, version, 0)
		require.Positive(f, length)
		f.Add(encoded, int(version))
	}

	f.Add(make([]byte, 30), int(Version04))

	f.Fuzz(func(t *testing.T, irec []byte, version int) {
		il2p_decode_frame(irec, Version(version))
	})
}

// recBitFuzzMaxStream bounds the bytes, each eight received bits, the receiver
// target feeds the receiver.  The largest IL2P frame, sync word and all, is
// about 1,200 bytes, so this is room for one with some to spare.
const recBitFuzzMaxStream = 2048

// Settings packed into the receiver target's settings byte: the version, taken
// modulo the three there are, and whether to expect a trailing CRC.
const (
	recBitFuzzVersionMask = 0x03
	recBitFuzzCRC         = 0x04
)

// recBitFuzzLevels is what a Sender puts on the line for fn, one level per
// bit, packed eight to a byte, least significant bit first, as the receiver
// target unpacks them.
func recBitFuzzLevels(fn func(s *Sender)) []byte {
	var out []byte

	var n = 0

	fn(NewSender(linecode.NewEncoder(func(level int) {
		if n%8 == 0 {
			out = append(out, 0)
		}

		if level != 0 {
			out[len(out)-1] |= 1 << (n % 8)
		}

		n++
	}), 0))

	return out
}

// recBitFuzzReceive feeds stream to a new Receiver with the settings packed in
// settings, then one more bit to see the last byte through, and returns how
// many packets it delivered.  Every delivery is checked against what the
// receiver was asked to do.
func recBitFuzzReceive(tb testing.TB, stream []byte, settings byte) int {
	tb.Helper()

	var version = Version(int(settings&recBitFuzzVersionMask) % 3)

	var alevel = ax25.ALevel{Rec: 42, Mark: 41, Space: 43}

	var delivered = 0

	var rx = NewReceiver(1, 2, 3, version, settings&recBitFuzzCRC != 0,
		func(int, int) ax25.ALevel { return alevel },
		func(channel int, subchannel int, slice int, pp *ax25.Packet, gotAlevel ax25.ALevel, retries phy.BitFixLevel, fecType phy.FECType) {
			delivered++

			assert.Equal(tb, []int{1, 2, 3}, []int{channel, subchannel, slice}, "a packet should say where it was heard")
			assert.Equal(tb, alevel, gotAlevel)
			assert.Equal(tb, phy.FECIL2P, fecType)
			assert.GreaterOrEqual(tb, int(retries), 0, "a negative number of symbols can't have been corrected")

			if assert.NotNil(tb, pp) {
				// What the receive path does next with a packet, short of
				// queueing it.
				pp.FormatAddrs()
				pp.Info()
				pp.Pack()
			}
		})

	for _, b := range stream {
		for i := range 8 {
			rx.RecBit(int(b>>i) & 1)
		}
	}

	rx.RecBit(0)

	return delivered
}

// FuzzReceiverRecBit covers the IL2P receiver, which is handed whatever bits
// the demodulator makes of anything transmitting on the channel: hunting for
// the sync word, then the header, payload and CRC it announces, through the
// same decoding FuzzIL2PDecodeFrame covers.
func FuzzReceiverRecBit(f *testing.F) {
	testutils.DiscardLogrus(f)

	Init(0)

	var pp = ax25.FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)

	// A frame in each version, with and without the CRC, as a receiver set
	// up to match would take it.
	for v := range byte(3) {
		var version = Version(v)

		for _, crc := range []bool{false, true} {
			var stream = recBitFuzzLevels(func(s *Sender) {
				s.SendPreamble(4, 0)
				require.Positive(f, s.SendFrame(pp, version, 1, crc, 0))
			})

			var settings = v
			if crc {
				settings |= recBitFuzzCRC
			}

			require.Equal(f, 1, recBitFuzzReceive(f, stream, settings), "the seed should be one the receiver accepts")

			f.Add(stream, settings)
		}
	}

	// Polarity 2 throws in errors for the FEC to correct.
	f.Add(recBitFuzzLevels(func(s *Sender) {
		s.SendPreamble(4, 0)
		s.SendFrame(pp, Version06, 1, true, 2)
	}), byte(Version06)|recBitFuzzCRC)

	f.Add([]byte{0x55, 0x55, 0x55, 0x55}, byte(0))
	f.Add([]byte{0xff, 0x00, 0xaa, 0x55, 0x7e, 0x81}, byte(recBitFuzzCRC))

	f.Fuzz(func(t *testing.T, stream []byte, settings byte) {
		if len(stream) > recBitFuzzMaxStream {
			t.Skip()
		}

		recBitFuzzReceive(t, stream, settings)
	})
}
