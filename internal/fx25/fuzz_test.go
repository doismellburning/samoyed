// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package fx25

import (
	"slices"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/require"
)

// fx25FuzzMaxStream bounds the bytes, each eight received bits, the FX.25
// target feeds the receiver.  The largest codeblock, tag and all, is under
// 300 bytes, so this is room for a handful of them back to back.
const fx25FuzzMaxStream = 2048

// FuzzFX25RecBit covers the FX.25 receive path: hunting the bit stream for a
// correlation tag, gathering the codeblock it announces, then the
// Reed-Solomon decoder and the HDLC unstuffing of what it hands back.  Anyone
// transmitting on the channel controls those bits.
func FuzzFX25RecBit(f *testing.F) {
	testutils.DiscardLogrus(f)

	var pp = ax25.FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)

	// A real codeblock for each correlation tag, so the fuzzer starts from
	// something the receiver accepts.  Only the short test frame fits the
	// smallest of them.
	for ctag := CTAG_MIN; ctag <= CTAG_MAX; ctag++ {
		var ctagNum, data, check = fx25_encode_frame(0, slices.Clone(fxTestFrame), 100+ctag, 0)
		require.Equal(f, ctag, ctagNum)
		f.Add(fxTestBlock(ctagNum, data, check), byte(0))
	}

	// The APRS frame, as an ordinary FX.25 configuration would send it.
	for _, checkBytes := range []int{16, 32, 64} {
		var ctagNum, data, check = fx25_encode_frame(0, pp.FrameData(), checkBytes, 0)
		require.Positive(f, ctagNum)

		var block = fxTestBlock(ctagNum, data, check)
		var frames, _ = fxTestReceive(block)
		require.Len(f, frames, 1, "the seed should be one the receiver accepts")
		f.Add(block, byte(2))

		// More damage than the check bytes can repair.
		var damaged = slices.Clone(block)
		for j := 24; j < 24+checkBytes; j++ {
			damaged[j] ^= 0xff
		}

		f.Add(damaged, byte(3))
	}

	f.Fuzz(func(t *testing.T, stream []byte, debug byte) {
		if len(stream) > fx25FuzzMaxStream {
			t.Skip()
		}

		// What the receive path does next with a frame, short of queueing it.
		var audioLevel = func(int, int) ax25.ALevel {
			return ax25.ALevel{Rec: 50, Mark: 50, Space: 50}
		}

		var sink = func(_ int, _ int, _ int, frame []byte, alevel ax25.ALevel, _ phy.BitFixLevel, _ phy.FECType) {
			ax25.FromFrame(frame, alevel)
		}

		var rx = NewReceiver(0, 0, 0, int(debug%4), audioLevel, sink)

		for _, b := range stream {
			for imask := byte(0x01); imask != 0; imask <<= 1 {
				rx.RecBit(int(b & imask))
			}
		}
	})
}
