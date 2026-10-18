// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/webui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelDescription(t *testing.T) {
	t.Parallel()

	var audio = new(RadioConfig)
	audio.chan_medium[0] = MEDIUM_RADIO
	audio.chan_medium[1] = MEDIUM_IGATE
	audio.chan_medium[2] = MEDIUM_NETTNC
	audio.chan_medium[4] = MEDIUM_AXUDP
	audio.chan_medium[5] = MediumSerialTNC

	assert.Equal(t, maybe.Just("radio"), channelDescription(audio, 0))
	assert.Equal(t, maybe.Just("aprs-is"), channelDescription(audio, 1))
	assert.Equal(t, maybe.Just("network"), channelDescription(audio, 2))
	assert.Equal(t, maybe.Just("axudp"), channelDescription(audio, 4))
	assert.Equal(t, maybe.Just("serial"), channelDescription(audio, 5))
	assert.Equal(t, maybe.Nothing[string](), channelDescription(audio, 3))
	assert.Equal(t, maybe.Nothing[string](), channelDescription(audio, MAX_TOTAL_CHANS))
	assert.Equal(t, maybe.Nothing[string](), channelDescription(nil, 0))
}

// A channel is shown with the station's own callsign on it, unless that is a
// placeholder.
func TestWebChannelCarriesItsCallsign(t *testing.T) {
	t.Parallel()

	var audio = new(RadioConfig)
	audio.chan_medium[0] = MEDIUM_RADIO
	audio.mycall[0] = "Q1TEST-1"
	audio.chan_medium[1] = MEDIUM_RADIO
	audio.mycall[1] = "NOCALL"
	audio.mycall[2] = "Q2TEST"

	var want0 webui.Channel
	want0.Number = 0
	want0.Description = "radio"
	want0.Callsign = "Q1TEST-1"

	var want1 webui.Channel
	want1.Number = 1
	want1.Description = "radio"

	assert.Equal(t, maybe.Just(want0), webChannel(audio, 0))
	assert.Equal(t, maybe.Just(want1), webChannel(audio, 1))
	assert.Equal(t, maybe.Nothing[webui.Channel](), webChannel(audio, 2))
}

// A received frame is shown with the kind of source it came from, and with
// its audio level only when that was a radio's: anything else has no audio
// to measure.
func TestWebPublishReceivedSaysWhereTheFrameCameFrom(t *testing.T) {
	t.Parallel()

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
		{dwcore.ChannelSource(dwcore.SourceSerialTNC, 1), "serial", maybe.Nothing[int]()},
	} {
		t.Run(tc.via, func(t *testing.T) {
			t.Parallel()

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

// An IGate with no server to connect to isn't one, as far as the web interface
// is concerned.
func TestWebIGateStatusUnconfigured(t *testing.T) {
	t.Parallel()

	var ig, _ = setupIGate(t)

	ig.config.t2_server_name = ""

	assert.Equal(t, maybe.Nothing[webui.IGateStatus](), webIGateStatus(ig))
	assert.Equal(t, maybe.Nothing[webui.IGateStatus](), webIGateStatus(nil))
}

func TestWebIGateStatus(t *testing.T) {
	t.Parallel()

	var ig, _ = setupIGate(t)

	ig.config.t2_server_port = 14580

	var connectedAt = time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)

	ig.updateStats(func(s *igateStats) {
		s.connectedAt = connectedAt
		s.uplinkPackets = 1
		s.uplinkBytes = 2
		s.downlinkPackets = 3
		s.downlinkBytes = 4
		s.rfXmitPackets = 5
	})

	var want webui.IGateStatus
	want.Server = "localhost:14580"
	want.Connected = true
	want.LoggedIn = true
	want.ConnectedAt = maybe.Just(connectedAt)
	want.UplinkPackets = 1
	want.UplinkBytes = 2
	want.DownlinkPackets = 3
	want.DownlinkBytes = 4
	want.RFPackets = 5

	assert.Equal(t, maybe.Just(want), webIGateStatus(ig))

	// Once the connection drops, the time of the last attempt to connect is
	// no longer the time of any connection, but the counts stand.
	var conn, _ = ig.connection()
	ig.dropConnection(conn)

	want.Connected = false
	want.LoggedIn = false
	want.ConnectedAt = maybe.Nothing[time.Time]()

	assert.Equal(t, maybe.Just(want), webIGateStatus(ig))
}
