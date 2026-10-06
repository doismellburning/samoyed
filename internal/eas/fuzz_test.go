// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package eas

import (
	"bytes"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fuzzMaxStream bounds the bytes, each eight received bits, one input feeds
// the receiver: room for a couple of the longest messages, preambles and all.
const fuzzMaxStream = 1024

// fuzzLevels is what a Sender puts on the line for message, one level per
// bit, packed eight to a byte, least significant bit first, as the fuzz target
// unpacks them.
func fuzzLevels(message string) []byte {
	return testutils.PackLevels(testutils.LineLevels(func(line *linecode.Encoder) { NewSender(line).SendMessage([]byte(message)) }))
}

// fuzzReceive feeds stream to a new Receiver and returns how many messages it
// delivered.  Every delivery is checked against what the receiver promises.
func fuzzReceive(tb testing.TB, stream []byte) int {
	tb.Helper()

	var alevel = ax25.ALevel{Rec: 42, Mark: 41, Space: 43}

	var delivered = 0

	var rx = NewReceiver(1, 2, 3,
		func(int, int) ax25.ALevel { return alevel },
		func(channel int, subchannel int, slice int, frame []byte, gotAlevel ax25.ALevel, retries phy.BitFixLevel, fecType phy.FECType) {
			delivered++

			assert.Equal(tb, []int{1, 2, 3}, []int{channel, subchannel, slice}, "a message should say where it was heard")
			assert.Equal(tb, alevel, gotAlevel)
			assert.Equal(tb, phy.BitFixNone, retries, "SAME has no fixing up")
			assert.Equal(tb, phy.FECNone, fecType, "SAME has no FEC")

			assert.LessOrEqual(tb, len(frame), EAS_MAX_LEN, "a message longer than SAME allows should not be delivered")

			if !assert.GreaterOrEqual(tb, len(frame), 4) {
				return
			}

			assert.True(tb, bytes.HasPrefix(frame, []byte("ZCZC")) || bytes.HasPrefix(frame, []byte("NNNN")),
				"a message should start with ZCZC or NNNN, not %q", frame[:4])

			for i, ch := range frame[4:] {
				assert.True(tb, (ch >= ' ' && ch <= 0x7f) || ch == '\r' || ch == '\n',
					"character %d, 0x%02x, should not have been accepted", 4+i, ch)
			}
		})

	for _, b := range stream {
		for i := range 8 {
			rx.RecBit(int(b>>i)&1, 0)
		}
	}

	return delivered
}

// FuzzReceiverRecBit covers the EAS SAME receiver, which is handed whatever
// bits the demodulator makes of anything transmitting on the channel: hunting
// for a preamble, then gathering and checking the header that follows.
func FuzzReceiverRecBit(f *testing.F) {
	for _, message := range []string{
		"ZCZC-EAS-RWT-012057+0030-2780415-WTSP/TV-",
		"NNNN",
	} {
		var stream = fuzzLevels(message)
		require.Equal(f, 1, fuzzReceive(f, stream), "the seed should be one the receiver accepts")
		f.Add(stream)
	}

	// A header that runs on too long to be one.
	f.Add(fuzzLevels("ZCZC-" + string(bytes.Repeat([]byte("1"), EAS_MAX_LEN))))

	// A header with a character SAME doesn't allow.
	f.Add(fuzzLevels("ZCZC-EAS-RWT-012057\x01+0030-2780415-WTSP/TV-"))

	f.Add([]byte{0xab, 0xab, 0xab, 0xab, 0xff, 0x00, 0x55})

	f.Fuzz(func(t *testing.T, stream []byte) {
		if len(stream) > fuzzMaxStream {
			t.Skip()
		}

		fuzzReceive(t, stream)
	})
}
