// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"slices"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A receiver hands each frame it extracts to the sink it was made with, along
// with the audio level from the function it was made with, the FEC type, and
// the bytes the FEC corrected as the fix level.
func TestFX25ReceiverHandsFramesToItsSink(t *testing.T) {
	type delivery struct {
		channel, subchannel, slice int
		frame                      []byte
		alevel                     ax25.ALevel
		retries                    phy.BitFixLevel
		fecType                    phy.FECType
	}

	var got []delivery

	var alevel = ax25.ALevel{Rec: 42, Mark: 41, Space: 43}

	var rx = newFX25Receiver(1, 2, 3, 0,
		func(channel int, subchannel int) ax25.ALevel {
			assert.Equal(t, 1, channel)
			assert.Equal(t, 2, subchannel)

			return alevel
		},
		func(channel int, subchannel int, slice int, frame []byte, alevel ax25.ALevel, retries phy.BitFixLevel, fecType phy.FECType) {
			got = append(got, delivery{channel, subchannel, slice, slices.Clone(frame), alevel, retries, fecType})
		})

	var ctagNum, data, check = fx25_encode_frame(0, slices.Clone(fxTestFrame), 16, 0)
	require.GreaterOrEqual(t, ctagNum, CTAG_MIN)

	// Damage two bytes of the codeblock, past the 16 flags and the 8 byte
	// correlation tag fxTestBlock puts before it, for the FEC to correct
	// and count.
	var block = fxTestBlock(ctagNum, data, check)
	block[30] ^= 0xff
	block[35] ^= 0xff

	for _, b := range block {
		for imask := byte(0x01); imask != 0; imask <<= 1 {
			rx.recBit(int(b & imask))
		}
	}

	require.Len(t, got, 1)
	assert.Equal(t, delivery{1, 2, 3, fxTestFrame, alevel, phy.BitFixLevel(2), phy.FECFX25}, got[0])
}

// silentAudioLevel stands in for a demodulator when a test drives a receiver
// directly, with no audio for it to have heard.
func silentAudioLevel(int, int) ax25.ALevel {
	return ax25.ALevel{Rec: 0, Mark: 0, Space: 0}
}
