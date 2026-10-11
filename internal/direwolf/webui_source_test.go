// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/webui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelDescription(t *testing.T) {
	var audio = new(RadioConfig)
	audio.chan_medium[0] = MEDIUM_RADIO
	audio.chan_medium[1] = MEDIUM_IGATE
	audio.chan_medium[2] = MEDIUM_NETTNC
	audio.chan_medium[4] = MEDIUM_AXUDP

	assert.Equal(t, maybe.Just("radio"), channelDescription(audio, 0))
	assert.Equal(t, maybe.Just("aprs-is"), channelDescription(audio, 1))
	assert.Equal(t, maybe.Just("network"), channelDescription(audio, 2))
	assert.Equal(t, maybe.Just("axudp"), channelDescription(audio, 4))
	assert.Equal(t, maybe.Nothing[string](), channelDescription(audio, 3))
	assert.Equal(t, maybe.Nothing[string](), channelDescription(audio, MAX_TOTAL_CHANS))
	assert.Equal(t, maybe.Nothing[string](), channelDescription(nil, 0))
}

// A received frame is shown with the kind of source it came from, and with
// its audio level only when that was a radio's: anything else has no audio
// to measure.
func TestWebPublishReceivedSaysWhereTheFrameCameFrom(t *testing.T) {
	var pp = ax25.FromText("Q1TEST>APRS:>hello", true)
	require.NotNil(t, pp)

	var alevel = ax25.ALevel{Rec: 42, Mark: 0, Space: 0}

	for _, tc := range []struct {
		src        dwcore.Source
		via        string
		audioLevel maybe.Maybe[int]
	}{
		{dwcore.RadioSource(dwcore.SlicerID{Channel: 1, Subchannel: 2, Slice: 3}), "radio", maybe.Just(42)},
		{dwcore.ChannelSource(dwcore.SourceDTMF, 1), "dtmf", maybe.Nothing[int]()},
		{dwcore.ChannelSource(dwcore.SourceAPRSIS, 1), "aprs-is", maybe.Nothing[int]()},
		{dwcore.ChannelSource(dwcore.SourceNetTNC, 1), "network", maybe.Nothing[int]()},
		{dwcore.ChannelSource(dwcore.SourceAXUDP, 1), "axudp", maybe.Nothing[int]()},
	} {
		t.Run(tc.via, func(t *testing.T) {
			var hub = webui.NewHub()

			webPublishReceived(hub, tc.src, pp, nil, alevel)

			var got = hub.Packets()
			require.Len(t, got, 1)
			assert.Equal(t, 1, got[0].Channel)
			assert.Equal(t, tc.via, got[0].Via)
			assert.Equal(t, tc.audioLevel, got[0].AudioLevel)
		})
	}
}
