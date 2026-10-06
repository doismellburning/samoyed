// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package hdlc

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fuzzMaxStream bounds the bytes, eight bits each, one input feeds the
// receiver.  Fixing bits is quadratic in a frame's length, as every flip
// means decoding the frame again, so a longer stream only spends the fuzzing
// time on fewer inputs.
const fuzzMaxStream = 256

// Settings packed into a fuzz input's settings byte.
const (
	fuzzFixBitsMask = 0x03 // Up to phy.BitFixTriple: phy.BitFixTwoSep tries every pair of bits, which is cubic.
	fuzzPassall     = 0x04
	fuzzAIS         = 0x08
	fuzzSanityShift = 4 // Two bits, taken modulo the three sanity tests.
	fuzzScrambled   = 0x40
	fuzzSanityMask  = 0x03
)

// fuzzLevels is what a Sender puts on the line for fn, one level per bit,
// packed eight to a byte, least significant bit first, as the fuzz target
// unpacks them.
func fuzzLevels(fn func(s *Sender)) []byte {
	return testutils.PackLevels(testutils.LineLevels(func(line *linecode.Encoder) { fn(NewSender(line, 0)) }))
}

// fuzzReceive feeds stream to a new Receiver with the settings packed in
// settings, and returns how many frames it delivered.  Every delivery is
// checked against what the receiver was asked to do.
func fuzzReceive(tb testing.TB, stream []byte, settings byte) int {
	tb.Helper()

	var config = Config{
		FixBits:    phy.BitFixLevel(settings & fuzzFixBitsMask),
		Passall:    settings&fuzzPassall != 0,
		AIS:        settings&fuzzAIS != 0,
		SanityTest: phy.Sanity(int((settings>>fuzzSanityShift)&fuzzSanityMask) % 3),
	}

	var scrambled = settings&fuzzScrambled != 0

	var alevel = ax25.ALevel{Rec: 42, Mark: 41, Space: 43}

	var delivered = 0

	var line linecode.Decoder

	var rx = NewReceiver(config, 1, 2, 3, scrambled, &line,
		func(int, int) ax25.ALevel { return alevel },
		func(channel int, subchannel int, slice int, frame []byte, gotAlevel ax25.ALevel, retries phy.BitFixLevel, fecType phy.FECType) {
			delivered++

			assert.Equal(tb, []int{1, 2, 3}, []int{channel, subchannel, slice}, "a frame should say where it was heard")
			assert.Equal(tb, alevel, gotAlevel)
			assert.Equal(tb, phy.FECNone, fecType, "plain HDLC has no FEC")

			assert.GreaterOrEqual(tb, len(frame), MinFrameLen-2, "a frame shorter than the shortest AX.25 frame should not be delivered")
			assert.LessOrEqual(tb, len(frame), MaxFrameLen-2, "a frame longer than the longest AX.25 frame should not be delivered")

			if retries == phy.BitFixPassall {
				assert.True(tb, config.Passall, "only passall lets a frame through with a bad FCS")
			} else {
				assert.LessOrEqual(tb, retries, config.FixBits, "no more bits should be fixed than asked for")
			}

			// What the receive path does next with a frame, short of
			// queueing it.
			ax25.FromFrame(frame, gotAlevel)
		})

	var pllNudgeTotal int64

	var pllSymbolCount int

	for _, b := range stream {
		for i := range 8 {
			var raw = b&(1<<i) != 0
			rx.RecBit(raw, line.Decode(raw, scrambled), scrambled, &pllNudgeTotal, &pllSymbolCount)
		}
	}

	return delivered
}

// FuzzReceiverRecBit covers the HDLC receiver, which is handed whatever bits
// the demodulator makes of anything transmitting on the channel, through
// finding frames between flags, fixing bits, and the sanity checks.
func FuzzReceiverRecBit(f *testing.F) {
	testutils.DiscardLogrus(f)

	var pp = ax25.FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)

	var frame = pp.FrameData()

	var clean = fuzzLevels(func(s *Sender) {
		s.SendFlags(4)
		s.SendFrame(frame, false)
		s.SendFlags(2)
	})

	require.Equal(f, 1, fuzzReceive(f, clean, 0), "the clean seed should be one the receiver accepts")

	var settingsSeeds = []byte{
		0,
		byte(phy.BitFixSingle),
		byte(phy.BitFixTriple) | fuzzPassall,
		fuzzAIS,
		byte(phy.BitFixDouble) | 1<<fuzzSanityShift,
		byte(phy.BitFixSingle) | 2<<fuzzSanityShift,
	}

	for _, settings := range settingsSeeds {
		f.Add(clean, settings)
	}

	// One bit flipped in the middle of the frame, for the fix-ups to find.
	var flipped = append([]byte{}, clean...)
	flipped[len(flipped)/2] ^= 0x10

	for _, settings := range settingsSeeds {
		f.Add(flipped, settings)
	}

	// A bad FCS, for passall.
	f.Add(fuzzLevels(func(s *Sender) {
		s.SendFlags(4)
		s.SendFrame(frame, true)
		s.SendFlags(2)
	}), byte(fuzzPassall))

	// Flags and noise.
	f.Add([]byte{0x7e, 0x7e, 0x7e, 0x7e}, byte(0))
	f.Add([]byte{0xff, 0x00, 0xaa, 0x55, 0x7e, 0x81}, byte(fuzzScrambled))

	f.Fuzz(func(t *testing.T, stream []byte, settings byte) {
		if len(stream) > fuzzMaxStream {
			t.Skip()
		}

		fuzzReceive(t, stream, settings)
	})
}
