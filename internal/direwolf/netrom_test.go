// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/doismellburning/samoyed/internal/nodemap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // Serial: captures os.Stdout and changes logrus's global logger.
func Test_config_yaml_netrom(t *testing.T) {
	t.Run("a node with the defaults", func(t *testing.T) {
		var c = parseYAMLConfig(t, "netrom: {callsign: q1test-7, alias: qnodea, channels: [0]}\n")
		assert.Zero(t, c.errors, c.output)

		var got, ok = c.misc.netrom.Get()
		require.True(t, ok)

		var want = netrom.DefaultConfig()
		want.Channels = []int{0}
		want.Callsign = "Q1TEST-7"
		want.Alias = "QNODEA"
		assert.Equal(t, want, got.config)
	})

	t.Run("every setting", func(t *testing.T) {
		var c = parseYAMLConfig(t, "netrom:\n  callsign: Q1TEST-7\n  alias: QNODEA\n  channels: [1, 0]\n  ttl: 7\n  nodesInterval: 600\n  quality: 0\n  minQuality: 50\n")
		assert.Zero(t, c.errors, c.output)

		var got, ok = c.misc.netrom.Get()
		require.True(t, ok)
		assert.Equal(t, []int{1, 0}, got.config.Channels)
		assert.Equal(t, byte(7), got.config.TTL)
		assert.Equal(t, 600*time.Second, got.config.NodesInterval)
		assert.Equal(t, byte(0), got.config.Quality, "0 is a quality like any other")
		assert.Equal(t, byte(50), got.config.MinQuality)
	})

	t.Run("a channel a NET/ROM node can't use yet is left for netromConfig", func(t *testing.T) {
		var c = parseYAMLConfig(t, fmt.Sprintf("netrom: {callsign: Q1TEST-7, alias: QNODEA, channels: [%d]}\n", MAX_TOTAL_CHANS-1))
		assert.Zero(t, c.errors, c.output)
		assert.True(t, c.misc.netrom.IsJust())
	})

	t.Run("without channels, they are left for netromConfig", func(t *testing.T) {
		var c = parseYAMLConfig(t, "netrom: {callsign: Q1TEST-7, alias: QNODEA}\n")
		assert.Zero(t, c.errors, c.output)

		var got, ok = c.misc.netrom.Get()
		require.True(t, ok)
		assert.False(t, got.integrated)
		assert.Empty(t, got.config.Channels)

		var config, err = c.audio.netromConfig(got, c.misc.node)
		require.NoError(t, err)
		assert.Equal(t, []int{0}, config.Channels, "every channel connected mode can be used on")
	})

	t.Run("no netrom means no node", func(t *testing.T) {
		var c = parseYAMLConfig(t, "agwPort: 8000\n")
		assert.True(t, c.misc.netrom.IsNothing())
	})

	for _, bad := range []string{
		"{alias: QNODEA, channels: [0]}",
		"{callsign: Q1TEST-77, alias: QNODEA, channels: [0]}",
		"{callsign: Q1TEST-7, channels: [0]}",
		"{callsign: Q1TEST-7, alias: TOOLONGX, channels: [0]}",
		"{callsign: Q1TEST-7, alias: 'Q N', channels: [0]}",
		"{callsign: Q1TEST-7, alias: QNODEA, channels: []}",
		"{callsign: Q1TEST-7, alias: QNODEA, channels: [-1]}",
		"{callsign: Q1TEST-7, alias: QNODEA, channels: [0, 1, 0]}",
		"{callsign: Q1TEST-7, alias: QNODEA, channel: 0}",
		fmt.Sprintf("{callsign: Q1TEST-7, alias: QNODEA, channels: [%d]}", MAX_TOTAL_CHANS),
		"{callsign: Q1TEST-7, alias: QNODEA, channels: [0], ttl: 0}",
		"{callsign: Q1TEST-7, alias: QNODEA, channels: [0], ttl: 256}",
		"{callsign: Q1TEST-7, alias: QNODEA, channels: [0], nodesInterval: 0}",
		"{callsign: Q1TEST-7, alias: QNODEA, channels: [0], nodesInterval: 10000000000}",
		"{callsign: Q1TEST-7, alias: QNODEA, channels: [0], quality: -1}",
		"{callsign: Q1TEST-7, alias: QNODEA, channels: [0], minQuality: 300}",
	} {
		t.Run("refused: "+bad, func(t *testing.T) {
			var c = parseYAMLConfig(t, "netrom: "+bad+"\n")

			assert.Equal(t, 1, c.errors, c.output)
			assert.True(t, c.misc.netrom.IsNothing(), "nothing configured that nobody asked for")
		})
	}
}

//nolint:paralleltest // Serial: captures os.Stdout and changes logrus's global logger.
func Test_config_yaml_netrom_with_node(t *testing.T) {
	t.Run("sharing the node's names, on its channels", func(t *testing.T) {
		var c = parseYAMLConfig(t, "node: {address: q1test-7, alias: qnodea}\nnetrom: {}\n")
		assert.Zero(t, c.errors, c.output)
		assert.True(t, c.misc.node.IsJust())

		var got, ok = c.misc.netrom.Get()
		require.True(t, ok)
		assert.True(t, got.integrated)

		var want = netrom.DefaultConfig()
		want.Callsign = "Q1TEST-7"
		want.Alias = "QNODEA"
		assert.Equal(t, want, got.config, "the channels are left for netromConfig")

		var config, err = c.audio.netromConfig(got, c.misc.node)
		require.NoError(t, err)
		assert.Equal(t, []int{0}, config.Channels, "the node's")
	})

	t.Run("the node's names whichever comes first in the file", func(t *testing.T) {
		var c = parseYAMLConfig(t, "netrom: {}\nnode: {address: Q1TEST-7, alias: QNODEA}\n")
		assert.Zero(t, c.errors, c.output)

		var got, ok = c.misc.netrom.Get()
		require.True(t, ok)
		assert.True(t, got.integrated)
		assert.Equal(t, "Q1TEST-7", got.config.Callsign)
	})

	t.Run("sharing the node's names, on all its several channels", func(t *testing.T) {
		var c = parseYAMLConfig(t, "node: {address: Q1TEST-7, alias: QNODEA, channels: [0, 1]}\nnetrom: {}\n")
		assert.Zero(t, c.errors, c.output)

		var got, ok = c.misc.netrom.Get()
		require.True(t, ok)
		assert.Empty(t, got.config.Channels, "the channels are left for netromConfig")
	})

	t.Run("every setting", func(t *testing.T) {
		var c = parseYAMLConfig(t, "node:\n  address: Q1TEST-7\n  alias: QNODEA\n  channels: [0, 1]\n"+
			"netrom:\n  channels: [1]\n  ttl: 7\n  nodesInterval: 600\n  quality: 255\n  minQuality: 50\n")
		assert.Zero(t, c.errors, c.output)

		var got, ok = c.misc.netrom.Get()
		require.True(t, ok)
		assert.Equal(t, []int{1}, got.config.Channels)
		assert.Equal(t, byte(7), got.config.TTL)
		assert.Equal(t, 600*time.Second, got.config.NodesInterval)
		assert.Equal(t, byte(255), got.config.Quality)
		assert.Equal(t, byte(50), got.config.MinQuality)
	})

	for _, bad := range []string{
		"{address: Q1TEST-7}\nnetrom: {}",
		"{address: Q1TEST-7, alias: TOOLONGX}\nnetrom: {}",
		"{address: Q1TEST-7, alias: QNODEA-1}\nnetrom: {}",
		"{address: Q1TEST-7, alias: QNODEA}\nnetrom: {ttl: 0}",
		"{address: Q1TEST-7, alias: QNODEA}\nnetrom: {callsign: Q1TEST-7}",
		"{address: Q1TEST-7, alias: QNODEA}\nnetrom: {callsign: Q1TEST-8}",
		"{address: Q1TEST-7, alias: QNODEA}\nnetrom: {alias: QNODEB}",
		"{address: Q1TEST, alias: SAMNOD}\nnetrom: {callsign: Q1TEST-7, alias: QNODEA, channels: [0]}",
		"{address: Q1TEST-7, alias: QNODEA}\nnetrom: {channels: []}",
		"{address: Q1TEST-7, alias: QNODEA, netrom: {}}",
	} {
		t.Run("refused: "+bad, func(t *testing.T) {
			var c = parseYAMLConfig(t, "node: "+bad+"\n")

			assert.Equal(t, 1, c.errors, c.output)
			assert.True(t, c.misc.netrom.IsNothing(), "nothing configured that nobody asked for")
		})
	}

	// Whether the channels are the node's waits for netromConfig, whose
	// complaint names the netrom block's line, not the node's.
	t.Run("off the node's channels, refused by netromConfig", func(t *testing.T) {
		var c = parseYAMLConfig(t, "node: {address: Q1TEST-7, alias: QNODEA, channels: [0]}\nnetrom: {channels: [1]}\n")
		assert.Zero(t, c.errors, c.output)

		var got, ok = c.misc.netrom.Get()
		require.True(t, ok)

		var _, err = c.audio.netromConfig(got, c.misc.node)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "line 3:")
	})
}

func testNetromSettings(callsign string, alias string, channels ...int) netromSettings {
	var config = netrom.DefaultConfig()
	config.Callsign = callsign
	config.Alias = alias
	config.Channels = channels

	return netromSettings{config: config, integrated: false, line: 3}
}

func integratedNetromSettings(callsign string, alias string, channels ...int) netromSettings {
	var settings = testNetromSettings(callsign, alias, channels...)
	settings.integrated = true

	return settings
}

func TestNetromConfig(t *testing.T) {
	t.Parallel()

	var radio = new(RadioConfig)
	radio.chan_medium[0] = MEDIUM_RADIO
	radio.chan_medium[1] = MEDIUM_RADIO
	radio.chan_medium[MAX_RADIO_CHANS] = MEDIUM_AXUDP
	radio.chan_medium[MAX_RADIO_CHANS+1] = MEDIUM_IGATE

	var shell = maybe.Just(nodeSettings{
		address:        "Q1TEST",
		alias:          "SAMNOD",
		channels:       []int{0, MAX_RADIO_CHANS},
		line:           1,
		infoMessage:    "",
		connectMessage: "",
		nodeMap:        maybe.Nothing[nodemap.Config](),
		mapPorts:       nil,
	})

	var testCases = []struct {
		name     string
		settings netromSettings
		shell    maybe.Maybe[nodeSettings]
		ok       bool
	}{
		{"a radio channel", testNetromSettings("Q1TEST-7", "QNODEA", 0), maybe.Nothing[nodeSettings](), true},
		{"an AXUDP channel", testNetromSettings("Q1TEST-7", "QNODEA", MAX_RADIO_CHANS), maybe.Nothing[nodeSettings](), true},
		{"an IGate channel", testNetromSettings("Q1TEST-7", "QNODEA", MAX_RADIO_CHANS+1), maybe.Nothing[nodeSettings](), false},
		{"an unconfigured channel", testNetromSettings("Q1TEST-7", "QNODEA", 2), maybe.Nothing[nodeSettings](), false},
		{"sharing the node shell's names", integratedNetromSettings("Q1TEST", "SAMNOD", 0), shell, true},
		{"sharing the node shell's names off its channels", integratedNetromSettings("Q1TEST", "SAMNOD", 1), shell, false},
		{"several channels", testNetromSettings("Q1TEST-7", "QNODEA", 0, 1, MAX_RADIO_CHANS), maybe.Nothing[nodeSettings](), true},
		{"several channels, one it can't use", testNetromSettings("Q1TEST-7", "QNODEA", 0, MAX_RADIO_CHANS+1), maybe.Nothing[nodeSettings](), false},
		{"sharing the node shell's names on several of its channels", integratedNetromSettings("Q1TEST", "SAMNOD", MAX_RADIO_CHANS, 0), shell, true},
		{"sharing the node shell's names, one off its channels", integratedNetromSettings("Q1TEST", "SAMNOD", 0, 1), shell, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var config, err = radio.netromConfig(tc.settings, tc.shell)
			if !tc.ok {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "line 3")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.settings.config, config)
		})
	}
}

// With names of its own and given no channels, NET/ROM runs on every channel
// connected mode can be used on, and there must be some.
func TestNetromConfigOnEveryChannel(t *testing.T) {
	t.Parallel()

	var radio = new(RadioConfig)
	radio.chan_medium[0] = MEDIUM_RADIO
	radio.chan_medium[1] = MEDIUM_RADIO
	radio.chan_medium[MAX_RADIO_CHANS] = MEDIUM_AXUDP
	radio.chan_medium[MAX_RADIO_CHANS+1] = MEDIUM_IGATE

	var config, err = radio.netromConfig(testNetromSettings("Q1TEST-7", "QNODEA"), maybe.Nothing[nodeSettings]())
	require.NoError(t, err)
	assert.Equal(t, []int{0, 1, MAX_RADIO_CHANS}, config.Channels)

	_, err = new(RadioConfig).netromConfig(testNetromSettings("Q1TEST-7", "QNODEA"), maybe.Nothing[nodeSettings]())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 3")
}

// Sharing the node shell's names and given no channels, NET/ROM runs on all
// the shell's.
func TestNetromConfigOnTheShellsChannels(t *testing.T) {
	t.Parallel()

	var radio = new(RadioConfig)
	radio.chan_medium[0] = MEDIUM_RADIO
	radio.chan_medium[1] = MEDIUM_RADIO
	radio.chan_medium[MAX_RADIO_CHANS] = MEDIUM_AXUDP

	for _, tc := range []struct {
		name     string
		channels []int
		want     []int
	}{
		{"some", []int{MAX_RADIO_CHANS, 0}, []int{MAX_RADIO_CHANS, 0}},
		{"every channel connected mode can be used on", nil, []int{0, 1, MAX_RADIO_CHANS}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var shell = maybe.Just(nodeSettings{
				address:        "Q1TEST-7",
				alias:          "QNODEA",
				channels:       tc.channels,
				line:           1,
				infoMessage:    "",
				connectMessage: "",
				nodeMap:        maybe.Nothing[nodemap.Config](),
				mapPorts:       nil,
			})

			var config, err = radio.netromConfig(integratedNetromSettings("Q1TEST-7", "QNODEA"), shell)
			require.NoError(t, err)
			assert.Equal(t, tc.want, config.Channels)
		})
	}
}

// A node shell listening on every channel connected mode can be used on, as
// it does when given none, is on the NET/ROM node's channels too, the command
// line's included.
func TestNetromConfigShellOnEveryChannel(t *testing.T) {
	t.Parallel()

	var radio = new(RadioConfig)
	radio.chan_medium[0] = MEDIUM_RADIO
	radio.chan_medium[1] = MEDIUM_RADIO

	var shell = maybe.Just(nodeSettings{
		address:        "Q1TEST-7",
		alias:          "",
		channels:       nil,
		line:           1,
		infoMessage:    "",
		connectMessage: "",
		nodeMap:        maybe.Nothing[nodemap.Config](),
		mapPorts:       nil,
	})

	var settings = integratedNetromSettings("Q1TEST-7", "QNODEA", 1)

	var config, err = radio.netromConfig(settings, shell)
	require.NoError(t, err)
	assert.Equal(t, settings.config, config)
}

// recordingAGWDataLink notes what reaches the data link.
type recordingAGWDataLink struct {
	mu    sync.Mutex
	calls []string
}

func (l *recordingAGWDataLink) notef(format string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.calls = append(l.calls, fmt.Sprintf(format, a...))
}

func (l *recordingAGWDataLink) ConnectRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, client int, pid int) {
	l.notef("connect %d %d %s>%s %#x", channel, client, addrs[ax25.Source], addrs[ax25.Destination], pid)
}

func (l *recordingAGWDataLink) DisconnectRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, client int) {
	l.notef("disconnect %d %d %s>%s", channel, client, addrs[ax25.Source], addrs[ax25.Destination])
}

func (l *recordingAGWDataLink) OutstandingFramesRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, client int) {
	l.notef("outstanding %d %d %s>%s", channel, client, addrs[ax25.Source], addrs[ax25.Destination])
}

func (l *recordingAGWDataLink) XmitDataRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, client int, _ int, xdata []byte) {
	l.notef("data %d %d %s>%s %q", channel, client, addrs[ax25.Source], addrs[ax25.Destination], xdata)
}

func (l *recordingAGWDataLink) RegisterCallsign(addr string, channel int, client int) {
	l.notef("register %s %d %d", addr, channel, client)
}

func (l *recordingAGWDataLink) UnregisterCallsign(addr string, channel int, client int) {
	l.notef("unregister %s %d %d", addr, channel, client)
}

func (l *recordingAGWDataLink) ClientCleanup(client int) {
	l.notef("cleanup %d", client)
}

func (l *recordingAGWDataLink) taken() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	var calls = l.calls
	l.calls = nil

	return calls
}

// recordingReporter notes what the NET/ROM node tells clients about their
// circuits.
type recordingReporter struct {
	mu          sync.Mutex
	terminated  []string
	outstanding []string
}

func (r *recordingReporter) LinkEstablished(int, int, string, string, bool) {}

func (r *recordingReporter) LinkTerminated(_ int, client int, remoteCall string, _ string, _ bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.terminated = append(r.terminated, fmt.Sprintf("%d %s", client, remoteCall))
}

func (r *recordingReporter) RecConnData(int, int, string, string, int, []byte) {}

func (r *recordingReporter) OutstandingFramesReply(_ int, client int, _ string, remoteCall string, count int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.outstanding = append(r.outstanding, fmt.Sprintf("%d %s %d", client, remoteCall, count))
}

// The AGW server's requests pass through netromAGWDataLink: what is the
// NET/ROM node's goes to the node, and everything else to the data link as
// before.
func TestNetromAGWDataLink(t *testing.T) {
	t.Parallel()

	var addrs = func(from string, to string) [ax25.MaxAddrs]string {
		var a [ax25.MaxAddrs]string
		a[ax25.Source] = from
		a[ax25.Destination] = to

		return a
	}

	var dataLink = new(recordingAGWDataLink)
	var reporter = new(recordingReporter)

	var config = netrom.DefaultConfig()
	config.Channels = []int{0}
	config.Callsign = "Q1TEST-7"
	config.Alias = "QNODEA"

	var l = newNetromAGWDataLink(dataLink)
	l.node = netrom.New(config, dataLink, func(int, int, *ax25.Packet) {}, reporter, netrom.RealClock{}, netromClient)

	// A connect with the NET/ROM PID on one of the node's channels is the node's.
	l.ConnectRequest(addrs("Q1TEST", "QNODEB"), 2, 0, 1, ax25.PIDNetROM)
	assert.Empty(t, dataLink.taken())

	reporter.mu.Lock()
	assert.Equal(t, []string{"1 QNODEB"}, reporter.terminated, "with no route there, the client is told at once")
	reporter.mu.Unlock()

	// An ordinary connect is AX.25.
	l.ConnectRequest(addrs("Q1TEST", "Q2TEST"), 2, 0, 1, 0xF0)
	l.ConnectRequest(addrs("Q1TEST", "Q2TEST"), 2, 1, 1, ax25.PIDNetROM)
	assert.Equal(t, []string{"connect 0 1 Q1TEST>Q2TEST 0xf0", "connect 1 1 Q1TEST>Q2TEST 0xcf"}, dataLink.taken())

	// Data and disconnects for anything but a circuit are AX.25.
	l.XmitDataRequest(addrs("Q1TEST", "Q2TEST"), 2, 0, 1, 0xF0, []byte("hi"))
	l.DisconnectRequest(addrs("Q1TEST", "Q2TEST"), 2, 0, 1)
	l.OutstandingFramesRequest(addrs("Q1TEST", "Q2TEST"), 2, 0, 1)
	assert.Equal(t, []string{
		`data 0 1 Q1TEST>Q2TEST "hi"`,
		"disconnect 0 1 Q1TEST>Q2TEST",
		"outstanding 0 1 Q1TEST>Q2TEST",
	}, dataLink.taken())

	// Registering the node's callsign is the node's.
	l.RegisterCallsign("Q1TEST-7", 0, 1)
	l.UnregisterCallsign("Q1TEST-7", 0, 1)
	assert.Empty(t, dataLink.taken(), "the data link must not hand the client the node's neighbour links")

	l.RegisterCallsign("Q1TEST", 0, 1)
	l.UnregisterCallsign("Q1TEST", 0, 1)
	assert.Equal(t, []string{"register Q1TEST 0 1", "unregister Q1TEST 0 1"}, dataLink.taken())

	// Outstanding frames on a client's circuit are the node's to answer for.
	var nodes, nodesErr = netrom.EncodeNodes("QNODEB", nil)
	require.NoError(t, nodesErr)

	var broadcast [ax25.MaxAddrs]string
	broadcast[ax25.Destination] = netrom.NodesCallsign
	broadcast[ax25.Source] = "Q2TEST-7"

	var pp = ax25.UFrame(broadcast, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, ax25.PIDNetROM, nodes[0])
	require.NotNil(t, pp)
	l.node.HeardUI(0, pp)

	l.ConnectRequest(addrs("Q1TEST", "QNODEB"), 2, 0, 1, ax25.PIDNetROM)
	dataLink.taken() // The node connecting to its neighbour.

	l.OutstandingFramesRequest(addrs("Q1TEST", "QNODEB"), 2, 0, 1)
	assert.Empty(t, dataLink.taken(), "not the data link's to answer")

	reporter.mu.Lock()
	assert.Equal(t, []string{"1 QNODEB 0"}, reporter.outstanding)
	reporter.mu.Unlock()

	// A client going away is both's.
	l.ClientCleanup(1)
	assert.Equal(t, []string{"cleanup 1"}, dataLink.taken())
}

// A frame for a virtual channel is printed by the transmit queue itself,
// never reaching the monitor code in xmit.go, and a NET/ROM node can sit on
// one.
//
//nolint:paralleltest // Serial: captures os.Stdout.
func TestTransmitQueueDecodesNetromOnAVirtualChannel(t *testing.T) {
	const channel = MAX_RADIO_CHANS

	var radio = new(RadioConfig)
	radio.chan_medium[channel] = MEDIUM_NETTNC

	var tq = newTestTransmitQueue(radio)

	var frames, err = netrom.EncodeNodes("QNODEA", nil)
	require.NoError(t, err)

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Destination] = netrom.NodesCallsign
	addrs[ax25.Source] = "Q1TEST-7"

	var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, ax25.PIDNetROM, frames[0])
	require.NotNil(t, pp)

	var out = captureStdout(t, func() { tq.Append(channel, TQ_PRIO_1_LO, pp) })

	assert.Contains(t, out, "NET/ROM NODES from QNODEA")
}
