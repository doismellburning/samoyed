// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package eas

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A receiver hands each SAME message it finds to the sink it was made with,
// along with the audio level from the function it was made with.
func TestEASReceiverHandsMessagesToItsSink(t *testing.T) {
	for _, message := range []string{
		"ZCZC-EAS-RWT-012057+0030-2780415-WTSP/TV-",
		"NNNN",
	} {
		t.Run(message, func(t *testing.T) {
			type delivery struct {
				from    dwcore.SlicerID
				message string
				rxInfo  dwcore.RxInfo
			}

			var got []delivery

			var alevel = ax25.ALevel{Rec: 42, Mark: 41, Space: 43}

			var from = dwcore.SlicerID{Channel: 1, Subchannel: 2, Slice: 3}

			var rx = NewReceiver(from,
				func(channel int, subchannel int) ax25.ALevel {
					assert.Equal(t, 1, channel)
					assert.Equal(t, 2, subchannel)

					return alevel
				},
				func(from dwcore.SlicerID, frame []byte, rxInfo dwcore.RxInfo) {
					got = append(got, delivery{from, string(frame), rxInfo})
				})

			// The preamble, then the message, each byte least significant
			// bit first, as same_send.go sends them.
			for _, b := range append([]byte{0xab, 0xab, 0xab, 0xab}, message...) {
				for i := range 8 {
					rx.RecBit(int(b>>i)&1, 0)
				}
			}

			require.Len(t, got, 1)
			assert.Equal(t, delivery{from, message, dwcore.RxInfo{ALevel: alevel, Retries: phy.BitFixNone, FEC: phy.FECNone}}, got[0])
		})
	}
}
