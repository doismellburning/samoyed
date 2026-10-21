// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/netrom"
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

// The web interface is told about the NET/ROM node, if there is one, as the
// node sees itself.
func TestWebNetROMStatus(t *testing.T) {
	t.Parallel()

	assert.Equal(t, maybe.Nothing[webui.NetROMStatus](), webNetROMStatus(nil))

	var config = netrom.DefaultConfig()
	config.Channels = []int{0}
	config.Callsign = "Q1TEST-7"
	config.Alias = "QNODEA"

	var node = netrom.New(config, new(recordingAGWDataLink), func(int, int, *ax25.Packet) {}, new(recordingReporter), netrom.RealClock{}, netromClient)

	var want webui.NetROMStatus
	want.Channels = []int{0}
	want.Callsign = "Q1TEST-7"
	want.Alias = "QNODEA"
	want.Neighbours = []webui.NetROMNeighbour{}
	want.Destinations = []webui.NetROMDestination{}
	want.Circuits = []webui.NetROMCircuit{}

	assert.Equal(t, maybe.Just(want), webNetROMStatus(node), "lists that are empty, not missing")

	var nodes = ax25.FromText("Q2TEST-7>NODES:x", true)
	require.NotNil(t, nodes)
	nodes.SetPID(ax25.PIDNetROM)
	nodes.SetInfo(append([]byte{0xFF}, "QNODEB"...))
	node.HeardUI(0, nodes)

	require.True(t, node.Connect(0, 1, "Q1TEST", "QNODEB", ax25.PIDNetROM))

	want.Neighbours = []webui.NetROMNeighbour{{Callsign: "Q2TEST-7", Link: "connecting", Failures: 0, Failing: false}}
	want.Destinations = []webui.NetROMDestination{{
		Callsign: "Q2TEST-7",
		Alias:    "QNODEB",
		Routes:   []webui.NetROMRoute{{Neighbour: "Q2TEST-7", Quality: int(netrom.DefaultQuality), Obsolescence: 6}},
	}}
	want.Circuits = []webui.NetROMCircuit{{
		OwnCall:     "Q1TEST",
		RemoteCall:  "QNODEB",
		RemoteNode:  "Q2TEST-7",
		User:        "Q1TEST",
		Incoming:    false,
		State:       "connecting",
		Outstanding: 0,
	}}

	assert.Equal(t, maybe.Just(want), webNetROMStatus(node))
}

// Only a radio channel has a transmit queue to show.
func TestWebQueues(t *testing.T) {
	t.Parallel()

	var audio = new(RadioConfig)
	audio.chan_medium[0] = MEDIUM_RADIO
	audio.chan_medium[1] = MEDIUM_NETTNC
	audio.chan_medium[2] = MEDIUM_RADIO

	var lengths [MAX_RADIO_CHANS][TQ_NUM_PRIO]int
	lengths[0][TQ_PRIO_0_HI] = 1
	lengths[0][TQ_PRIO_1_LO] = 2
	lengths[2][TQ_PRIO_1_LO] = 3

	assert.Equal(t, []webui.QueueStatus{
		{Channel: 0, High: 1, Low: 2},
		{Channel: 2, High: 0, Low: 3},
	}, webQueues(audio, lengths))
	assert.Nil(t, webQueues(nil, lengths))
}

// Every client attached over AGW and KISS TCP is shown, with what it has asked
// for, and with whether it has logged in only where it has to: not where it
// connected from this machine.
func TestWebClients(t *testing.T) {
	t.Parallel()

	var agw = requireLogins(t, "Q1TEST", "hunter2")
	agw.clients[1].conn = connWithRemoteAddr(tcpAddr(t, "192.168.1.10"))
	agw.clients[1].sendMonitor = true
	agw.clients[2].conn = connWithRemoteAddr(tcpAddr(t, "192.168.1.13"))
	agw.clients[2].loggedIn = true
	agw.clients[0].conn = connWithRemoteAddr(tcpAddr(t, "127.0.0.1"))
	agw.clients[0].loggedIn = true
	agw.clients[0].loginExempt = true

	// The service's list of ports is newest first.
	var allChannels = new(kissport_status_s)
	allChannels.tcp_port = 8001
	allChannels.channel = -1
	allChannels.client_sock[2] = connWithRemoteAddr(tcpAddr(t, "192.168.1.11"))

	var oneChannel = new(kissport_status_s)
	oneChannel.tcp_port = 8002
	oneChannel.channel = 1
	oneChannel.client_sock[0] = connWithRemoteAddr(tcpAddr(t, "192.168.1.12"))
	oneChannel.pnext = allChannels

	var kissNet = new(KissNetService)
	kissNet.allPorts = oneChannel

	var want = []webui.ClientStatus{
		{Protocol: "agw", Port: 0, Address: "127.0.0.1:8000", Channel: maybe.Nothing[int](),
			Raw: false, Monitor: false, LoggedIn: maybe.Nothing[bool]()},
		{Protocol: "agw", Port: 0, Address: "192.168.1.10:8000", Channel: maybe.Nothing[int](),
			Raw: false, Monitor: true, LoggedIn: maybe.Just(false)},
		{Protocol: "agw", Port: 0, Address: "192.168.1.13:8000", Channel: maybe.Nothing[int](),
			Raw: false, Monitor: false, LoggedIn: maybe.Just(true)},
		{Protocol: "kiss", Port: 8001, Address: "192.168.1.11:8000", Channel: maybe.Nothing[int](),
			Raw: false, Monitor: false, LoggedIn: maybe.Nothing[bool]()},
		{Protocol: "kiss", Port: 8002, Address: "192.168.1.12:8000", Channel: maybe.Just(1),
			Raw: false, Monitor: false, LoggedIn: maybe.Nothing[bool]()},
	}

	assert.Equal(t, want, webClients(agw, kissNet))
}

// Where no login is needed, nothing is said about one.
func TestWebClientsWithoutALogin(t *testing.T) {
	t.Parallel()

	var agw = requireLogins(t)
	agw.clients[0].conn = connWithRemoteAddr(tcpAddr(t, "192.168.1.10"))

	var got = webClients(agw, nil)
	require.Len(t, got, 1)
	assert.Equal(t, maybe.Nothing[bool](), got[0].LoggedIn)
}

func TestWebClientsWithoutServers(t *testing.T) {
	t.Parallel()

	assert.Empty(t, webClients(nil, nil))
}
