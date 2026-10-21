// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package node

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/mheard"
	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testClient  = 7
	testChannel = 1
	nodeCall    = "Q1TEST-7"
	nodeAlias   = "SAMNOD"
	userCall    = "Q2TEST"

	invalidCommand = "SAMNOD:Q1TEST-7} Invalid command - type ? for help\r"
)

// sent is a frame of data the node asked the data link to send.
type sent struct {
	channel int
	from    string
	to      string
	data    string
}

// fakeLink is a DataLink that remembers what it is asked to do.
type fakeLink struct {
	mu          sync.Mutex
	registered  []string
	connects    []sent
	sent        []sent
	pids        []int
	disconnects []string
}

func (f *fakeLink) ConnectRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, _ int, _ int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.connects = append(f.connects, sent{channel: channel, from: addrs[ax25.Source], to: addrs[ax25.Destination], data: ""})
}

func (f *fakeLink) RegisterCallsign(addr string, channel int, client int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.registered = append(f.registered, fmt.Sprintf("%s@%d/%d", addr, channel, client))
}

func (f *fakeLink) DisconnectRequest(addrs [ax25.MaxAddrs]string, _ int, _ int, _ int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.disconnects = append(f.disconnects, addrs[ax25.Destination])
}

func (f *fakeLink) XmitDataRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, _ int, pid int, xdata []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.pids = append(f.pids, pid)

	f.sent = append(f.sent, sent{channel: channel, from: addrs[ax25.Source], to: addrs[ax25.Destination], data: string(xdata)})
}

// output is everything sent so far, run together.
func (f *fakeLink) output() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var b strings.Builder
	for _, s := range f.sent {
		b.WriteString(s.data)
	}

	return b.String()
}

// reset forgets what has been sent.
func (f *fakeLink) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sent = nil
	f.pids = nil
}

// noneHeard is HeardStations for when nothing has been heard.
func noneHeard(int) []mheard.Heard {
	return nil
}

// testConfig is the configuration of a node with an alias on testChannel,
// changed by each of options.
func testConfig(options ...func(*Config)) Config {
	var config = Config{
		Address:        nodeCall,
		Alias:          nodeAlias,
		Ports:          []Port{{Channel: testChannel, Description: "Radio"}},
		InfoMessage:    "",
		ConnectMessage: "",
	}

	for _, option := range options {
		option(&config)
	}

	return config
}

func withConnectMessage(message string) func(*Config) {
	return func(config *Config) {
		config.ConnectMessage = message
	}
}

func withoutAlias(config *Config) {
	config.Alias = ""
}

func onChannels(channels ...int) func(*Config) {
	return func(config *Config) {
		config.Ports = nil
		for _, c := range channels {
			config.Ports = append(config.Ports, Port{Channel: c, Description: fmt.Sprintf("Port %d", c)})
		}
	}
}

func withInfoMessage(info string) func(*Config) {
	return func(config *Config) {
		config.InfoMessage = info
	}
}

// connected makes a node with a user connected to it, and the greeting
// already forgotten.
func connected(t *testing.T) (*Node, *fakeLink) {
	t.Helper()

	return connectedWith(t, testConfig())
}

// connectedWith is connected, for a node configured as config.
func connectedWith(t *testing.T, config Config) (*Node, *fakeLink) {
	t.Helper()

	var link = new(fakeLink)
	var n = New(config, link, noneHeard, nil, testClient)

	n.LinkEstablished(testChannel, testClient, userCall, nodeCall, true)
	link.reset()

	return n, link
}

func TestStartRegistersCallsignAndAlias(t *testing.T) {
	t.Parallel()

	var link = new(fakeLink)
	var n = New(testConfig(onChannels(0, 2)), link, noneHeard, nil, testClient)

	n.Start()

	assert.Equal(t, []string{nodeCall + "@0/7", nodeAlias + "@0/7", nodeCall + "@2/7", nodeAlias + "@2/7"}, link.registered)
}

func TestStartWithoutAlias(t *testing.T) {
	t.Parallel()

	var link = new(fakeLink)
	var n = New(testConfig(withoutAlias, onChannels(0)), link, noneHeard, nil, testClient)

	n.Start()

	assert.Equal(t, []string{nodeCall + "@0/7"}, link.registered)
}

func TestGreeting(t *testing.T) {
	t.Parallel()

	var link = new(fakeLink)
	var n = New(testConfig(), link, noneHeard, nil, testClient)

	n.LinkEstablished(testChannel, testClient, userCall, nodeCall, true)

	require.Len(t, link.sent, 1)
	assert.Equal(t, sent{channel: testChannel, from: nodeCall, to: userCall, data: "SAMNOD:Q1TEST-7} Welcome to Samoyed\r"}, link.sent[0])
}

func TestGreetingWithoutAlias(t *testing.T) {
	t.Parallel()

	var link = new(fakeLink)
	var n = New(testConfig(withoutAlias), link, noneHeard, nil, testClient)

	n.LinkEstablished(testChannel, testClient, userCall, nodeCall, true)

	assert.Equal(t, "Q1TEST-7} Welcome to Samoyed\r", link.output())
}

func TestConnectMessage(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, message, want string
	}{
		{"one line", "Welcome to the club node", "SAMNOD:Q1TEST-7} Welcome to the club node\r"},
		{"LF lines", "Welcome\nType ? for help\n", "SAMNOD:Q1TEST-7} Welcome\rType ? for help\r"},
		{"CR LF lines", "Welcome\r\nType ? for help\r\n", "SAMNOD:Q1TEST-7} Welcome\rType ? for help\r"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var link = new(fakeLink)
			var n = New(testConfig(withConnectMessage(tc.message)), link, noneHeard, nil, testClient)

			n.LinkEstablished(testChannel, testClient, userCall, nodeCall, true)

			assert.Equal(t, tc.want, link.output())
		})
	}
}

// A user who connected to the alias is answered from the alias, which is the
// link the data link has.
func TestAnswersFromTheCallsignConnectedTo(t *testing.T) {
	t.Parallel()

	var link = new(fakeLink)
	var n = New(testConfig(), link, noneHeard, nil, testClient)

	n.LinkEstablished(testChannel, testClient, userCall, nodeAlias, true)
	n.RecConnData(testChannel, testClient, userCall, nodeAlias, ax25.PIDNoLayer3, []byte("XYZZY\r"))

	require.Len(t, link.sent, 2)
	assert.Equal(t, nodeAlias, link.sent[1].from)
}

func TestOutgoingLinkIsNotASession(t *testing.T) {
	t.Parallel()

	var link = new(fakeLink)
	var n = New(testConfig(withoutAlias), link, noneHeard, nil, testClient)

	n.LinkEstablished(testChannel, testClient, userCall, nodeCall, false)
	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("XYZZY\r"))

	assert.Empty(t, link.sent)
}

func TestInvalidCommand(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("XYZZY\r"))

	assert.Equal(t, invalidCommand, link.output())
}

func TestBye(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"B", "BYE", "bye", "Q", "quit", "  Bye  "} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			var n, link = connected(t)

			n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte(command+"\r"))

			assert.Equal(t, []string{userCall}, link.disconnects)
			assert.Empty(t, link.output())
		})
	}
}

// Nothing typed after BYE is answered: the user is on the way out.
func TestNothingAfterBye(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("BYE\rXYZZY\r"))

	assert.Empty(t, link.output())
}

// Regression test: what arrived after BYE, before the link was down, was
// still run, so a second BYE asked for a second disconnect.
func TestNothingAfterByeInALaterFrame(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("B\r"))
	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("B\r"))
	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("XYZZY\r"))

	assert.Equal(t, []string{userCall}, link.disconnects)
	assert.Empty(t, link.output())
}

func TestLineEndings(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]string{
		"CR":    "XYZZY\r",
		"LF":    "XYZZY\n",
		"CR LF": "XYZZY\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var n, link = connected(t)

			n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte(input))

			assert.Equal(t, invalidCommand, link.output())
		})
	}
}

func TestBlankLinesIgnored(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("\r\r  \t \r\n"))

	assert.Empty(t, link.output())
}

func TestLineSplitAcrossFrames(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("B"))
	assert.Empty(t, link.disconnects)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("YE\r"))
	assert.Equal(t, []string{userCall}, link.disconnects)
}

func TestSeveralLinesInOneFrame(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("ONE\rTWO\r"))

	assert.Equal(t, strings.Repeat(invalidCommand, 2), link.output())
}

// A line too long to hold is dropped whole - none of it is taken as a
// command, not even what came after the cut - and the next line is read
// afresh.
func TestOverlongLine(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	var long = strings.Repeat("X", maxLineLen) + "BYE\r"

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte(long))

	assert.Equal(t, "SAMNOD:Q1TEST-7} Line too long\r", link.output())
	assert.Empty(t, link.disconnects)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("BYE\r"))
	assert.Equal(t, []string{userCall}, link.disconnects)
}

func TestLongestLineAccepted(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte(strings.Repeat("X", maxLineLen)+"\r"))

	assert.Equal(t, invalidCommand, link.output())
}

func TestTerminatedSessionIsForgotten(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	n.LinkTerminated(testChannel, testClient, userCall, nodeCall, false)
	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("XYZZY\r"))

	assert.Empty(t, link.output())
}

// Each user has a session of their own, so what one has half typed does not
// run into another's.
func TestSessionsAreSeparate(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	const otherCall = "Q3TEST"

	n.LinkEstablished(testChannel, testClient, otherCall, nodeCall, true)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("B"))
	n.RecConnData(testChannel, testClient, otherCall, nodeCall, ax25.PIDNoLayer3, []byte("YE\r"))

	assert.Empty(t, link.disconnects)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("YE\r"))

	assert.Equal(t, []string{userCall}, link.disconnects)
}

// Regression test: the data link says a link is established again when the
// far end resets it, which made a new session, losing what was half typed,
// and greeted the user again.
func TestLinkResetKeepsSession(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("B"))
	n.LinkEstablished(testChannel, testClient, userCall, nodeCall, true)
	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("YE\r"))

	assert.Empty(t, link.output())
	assert.Equal(t, []string{userCall}, link.disconnects)
}

// Regression test: a line too long was complained about before the replies
// to the lines that came before it.
func TestOverlongLineAnsweredInTurn(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	var input = "XYZZY\r" + strings.Repeat("X", maxLineLen+1) + "\rXYZZY\r"

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte(input))

	assert.Equal(t, invalidCommand+
		"SAMNOD:Q1TEST-7} Line too long\r"+
		invalidCommand, link.output())
}

func TestHelp(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"HELP", "h", "?"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			var n, link = connected(t)

			n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte(command+"\r"))

			assert.Equal(t, "SAMNOD:Q1TEST-7} Commands:\r"+
				"  BYE, B, QUIT, Q - Disconnect\r"+
				"  CONNECT, C - Connect to a station: C [port] callsign\r"+
				"  HELP, H, ? - List the commands\r"+
				"  INFO, I - About this node\r"+
				"  PORTS, P - List the ports\r"+
				"  MHEARD, MH - List the stations heard\r"+
				"  NODES, N - List the nodes known: N [node] for its routes\r"+
				"  ROUTES, R - List the neighbouring nodes\r", link.output())
		})
	}
}

func TestInfo(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, info, want string
	}{
		{"none", "", "SAMNOD:Q1TEST-7} SAMNOD:Q1TEST-7 is a Samoyed node\r"},
		{"one line", "Test node", "SAMNOD:Q1TEST-7} Test node\r"},
		{"LF lines", "Test node\nin a test\n", "SAMNOD:Q1TEST-7} Test node\rin a test\r"},
		{"CR LF lines", "Test node\r\nin a test\r\n", "SAMNOD:Q1TEST-7} Test node\rin a test\r"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var n, link = connectedWith(t, testConfig(withInfoMessage(tc.info)))

			n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("i\r"))

			assert.Equal(t, tc.want, link.output())
		})
	}
}

func TestPorts(t *testing.T) {
	t.Parallel()

	var n, link = connectedWith(t, testConfig(onChannels(0, testChannel, 10)))

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("p\r"))

	assert.Equal(t, "SAMNOD:Q1TEST-7} Ports:\r"+
		"   0 Port 0\r"+
		"   1 Port 1\r"+
		"  10 Port 10\r", link.output())
}

func TestMHeard(t *testing.T) {
	t.Parallel()

	var now = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

	var link = new(fakeLink)

	var asked int

	var n = New(testConfig(), link, func(limit int) []mheard.Heard {
		asked = limit

		return []mheard.Heard{
			{Callsign: "Q3TEST-15", Channel: 10, Hops: 0, Packets: 1, FirstHeard: time.Time{}, LastHeard: now.Add(-5 * time.Second), Lat: maybe.Nothing[float64](), Lon: maybe.Nothing[float64]()},
			{
				Callsign:   "Q4TEST",
				Channel:    0,
				Hops:       2,
				Packets:    1,
				FirstHeard: time.Time{},
				LastHeard:  now.Add(-(26*time.Hour + 3*time.Minute + 4*time.Second)),
				Lat:        maybe.Nothing[float64](),
				Lon:        maybe.Nothing[float64](),
			},
		}
	}, nil, testClient)
	n.now = func() time.Time { return now }

	n.LinkEstablished(testChannel, testClient, userCall, nodeCall, true)
	link.reset()

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("mh\r"))

	assert.Equal(t, maxHeard, asked)
	assert.Equal(t, "SAMNOD:Q1TEST-7} Heard:\r"+
		"  Q3TEST-15 10 0:00:05\r"+
		"  Q4TEST     0 26:03:04\r", link.output())
}

func TestMHeardNothing(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("MHEARD\r"))

	assert.Equal(t, "SAMNOD:Q1TEST-7} Heard:\r", link.output())
}

func TestNodesAndRoutesWithoutNetROM(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"NODES", "n", "N QNODEB", "ROUTES", "r"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			var n, link = connected(t)

			n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte(command+"\r"))

			assert.Equal(t, "SAMNOD:Q1TEST-7} This node does not do NET/ROM\r", link.output())
		})
	}
}

// testNetROM is what a NET/ROM node knows: five nodes, reached through three
// neighbours on two ports, one of which is failing, and a fourth neighbour
// that is the first's callsign on the other port, which reaches none.
func testNetROM() netrom.Status {
	return netrom.Status{
		Channels: []int{testChannel},
		Callsign: nodeCall,
		Alias:    nodeAlias,
		Neighbours: []netrom.NeighbourStatus{
			{Channel: 0, Callsign: "Q2TEST-7", Link: "up", Quality: 192, Failures: 0, Failing: false},
			{Channel: 2, Callsign: "Q2TEST-7", Link: "down", Quality: 192, Failures: 0, Failing: false},
			{Channel: 0, Callsign: "Q5TEST-7", Link: "down", Quality: 192, Failures: 2, Failing: true},
			{Channel: 2, Callsign: "Q6TEST-7", Link: "connecting", Quality: 100, Failures: 0, Failing: false},
		},
		Destinations: []netrom.DestinationStatus{
			{Callsign: "Q2TEST-7", Alias: "QNODEB", Routes: []netrom.RouteStatus{
				{Channel: 0, Neighbour: "Q2TEST-7", Quality: 192, Obsolescence: 6},
			}},
			{Callsign: "Q3TEST-7", Alias: "QNODEC", Routes: []netrom.RouteStatus{
				{Channel: 0, Neighbour: "Q2TEST-7", Quality: 144, Obsolescence: 5},
				{Channel: 0, Neighbour: "Q5TEST-7", Quality: 100, Obsolescence: 4},
			}},
			{Callsign: "Q4TEST-7", Alias: "", Routes: []netrom.RouteStatus{
				{Channel: 0, Neighbour: "Q2TEST-7", Quality: 90, Obsolescence: 6},
			}},
			{Callsign: "Q5TEST-7", Alias: "AAA", Routes: []netrom.RouteStatus{
				{Channel: 0, Neighbour: "Q5TEST-7", Quality: 192, Obsolescence: 3},
			}},
			{Callsign: "Q6TEST-7", Alias: "QNODEF", Routes: []netrom.RouteStatus{
				{Channel: 2, Neighbour: "Q6TEST-7", Quality: 192, Obsolescence: 6},
			}},
		},
		Circuits: nil,
	}
}

// connectedWithNetROM is connected, for a node whose NET/ROM node knows
// status.
func connectedWithNetROM(t *testing.T, status netrom.Status) (*Node, *fakeLink) {
	t.Helper()

	var n, link, _ = connectedWithNetROMOn(t, testConfig(), status)

	return n, link
}

// fakeNetROM is a NetROM that knows status, and remembers what circuits it is
// asked to open, send on and close.
type fakeNetROM struct {
	status netrom.Status

	// refuse says Connect takes the circuit for none of its own.
	refuse bool

	mu          sync.Mutex
	connects    []sent
	sent        []sent
	disconnects []string
}

func (f *fakeNetROM) Status() netrom.Status {
	return f.status
}

func (f *fakeNetROM) Connect(channel int, _ int, ownCall string, remoteCall string, pid int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	if pid != ax25.PIDNetROM {
		panic("circuit asked for with the wrong PID")
	}

	f.connects = append(f.connects, sent{channel: channel, from: ownCall, to: remoteCall, data: ""})

	return !f.refuse
}

func (f *fakeNetROM) Send(channel int, _ int, ownCall string, remoteCall string, data []byte) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sent = append(f.sent, sent{channel: channel, from: ownCall, to: remoteCall, data: string(data)})

	return true
}

func (f *fakeNetROM) Disconnect(_ int, _ int, _ string, remoteCall string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.disconnects = append(f.disconnects, remoteCall)

	return true
}

// connectedWithNetROMOn is connectedWithNetROM, for a node configured as
// config, also giving its NET/ROM node.
func connectedWithNetROMOn(t *testing.T, config Config, status netrom.Status) (*Node, *fakeLink, *fakeNetROM) {
	t.Helper()

	var link = new(fakeLink)
	var nr = new(fakeNetROM)
	nr.status = status

	var n = New(config, link, noneHeard, nr, testClient)

	n.LinkEstablished(testChannel, testClient, userCall, nodeCall, true)
	link.reset()

	return n, link, nr
}

func TestNodes(t *testing.T) {
	t.Parallel()

	var n, link = connectedWithNetROM(t, testNetROM())

	typeLine(n, "n")

	assert.Equal(t, "SAMNOD:Q1TEST-7} Nodes:\r"+
		"AAA:Q5TEST-7      QNODEB:Q2TEST-7   QNODEC:Q3TEST-7\r"+
		"QNODEF:Q6TEST-7   Q4TEST-7\r", link.output())
}

func TestNodesNoneKnown(t *testing.T) {
	t.Parallel()

	var nothing netrom.Status

	var n, link = connectedWithNetROM(t, nothing)

	typeLine(n, "NODES")

	assert.Equal(t, "SAMNOD:Q1TEST-7} Nodes:\r", link.output())
}

func TestNodeRoutes(t *testing.T) {
	t.Parallel()

	const want = "SAMNOD:Q1TEST-7} Routes to QNODEC:Q3TEST-7:\r" +
		"   0 Q2TEST-7  quality 144 obsolescence 5\r" +
		"   0 Q5TEST-7  quality 100 obsolescence 4\r"

	for _, name := range []string{"QNODEC", "qnodec", "Q3TEST-7", "q3test-7"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var n, link = connectedWithNetROM(t, testNetROM())

			typeLine(n, "N "+name)

			assert.Equal(t, want, link.output())
		})
	}
}

func TestNodeRoutesUnknown(t *testing.T) {
	t.Parallel()

	var n, link = connectedWithNetROM(t, testNetROM())

	typeLine(n, "N qnodex")

	assert.Equal(t, "SAMNOD:Q1TEST-7} No such node QNODEX\r", link.output())
}

func TestRoutes(t *testing.T) {
	t.Parallel()

	var n, link = connectedWithNetROM(t, testNetROM())

	typeLine(n, "R")

	assert.Equal(t, "SAMNOD:Q1TEST-7} Routes:\r"+
		"   0 Q2TEST-7  up         quality 192 3 nodes\r"+
		"   2 Q2TEST-7  down       quality 192 0 nodes\r"+
		"   0 Q5TEST-7  down       quality 192 2 nodes failing\r"+
		"   2 Q6TEST-7  connecting quality 100 1 node\r", link.output())
}

const farCall = "Q3TEST"

// typeLine sends what the user types, ended in CR.
func typeLine(n *Node, line string) {
	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte(line+"\r"))
}

// connectedOnward makes a node with a user connected to it, and through it
// to farCall, and forgets all that was said on the way.
func connectedOnward(t *testing.T) (*Node, *fakeLink) {
	t.Helper()

	var n, link = connected(t)

	typeLine(n, "C "+farCall)
	n.LinkEstablished(testChannel, testClient, farCall, nodeCall, false)
	link.reset()

	return n, link
}

func TestConnect(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	typeLine(n, "c q3test")

	assert.Equal(t, "SAMNOD:Q1TEST-7} Trying Q3TEST on port 1\r", link.output())
	assert.Equal(t, []sent{{channel: testChannel, from: nodeCall, to: farCall, data: ""}}, link.connects)

	link.reset()
	n.LinkEstablished(testChannel, testClient, farCall, nodeCall, false)

	assert.Equal(t, "SAMNOD:Q1TEST-7} Connected to Q3TEST\r", link.output())
}

// The node calls from its callsign even for a user who connected to its
// alias.
func TestConnectFromAlias(t *testing.T) {
	t.Parallel()

	var link = new(fakeLink)
	var n = New(testConfig(), link, noneHeard, nil, testClient)

	n.LinkEstablished(testChannel, testClient, userCall, nodeAlias, true)
	n.RecConnData(testChannel, testClient, userCall, nodeAlias, ax25.PIDNoLayer3, []byte("C Q3TEST\r"))

	assert.Equal(t, []sent{{channel: testChannel, from: nodeCall, to: farCall, data: ""}}, link.connects)
}

func TestConnectOnPort(t *testing.T) {
	t.Parallel()

	var n, link = connectedWith(t, testConfig(onChannels(testChannel, 10)))

	typeLine(n, "C 10 Q3TEST-2")

	assert.Equal(t, "SAMNOD:Q1TEST-7} Trying Q3TEST-2 on port 10\r", link.output())
	assert.Equal(t, []sent{{channel: 10, from: nodeCall, to: "Q3TEST-2", data: ""}}, link.connects)
}

func TestConnectRefused(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		line, want string
	}{
		{"C", "Usage: C [port] callsign"},
		{"C Q3TEST", "Usage: C [port] callsign"},
		{"C 1 Q3TEST Q4TEST", "Usage: C [port] callsign"},
		{"C one Q3TEST", "Usage: C [port] callsign"},
		{"C 5 Q3TEST", "No port 5 - type P for the ports"},
		{"C 1 Q3TEST-99", "Invalid callsign Q3TEST-99"},
		{"C 1 Q1TEST-7", "That is this node"},
		{"C 1 samnod", "That is this node"},
		{"C 1 Q2TEST", "Busy - Q2TEST is already connected to this node"},
	} {
		t.Run(tc.line, func(t *testing.T) {
			t.Parallel()

			var n, link = connectedWith(t, testConfig(onChannels(testChannel, 10)))

			typeLine(n, tc.line)

			assert.Equal(t, "SAMNOD:Q1TEST-7} "+tc.want+"\r", link.output())
			assert.Empty(t, link.connects)
		})
	}
}

// One downlink to a station at a time: the data link would take a second
// for the first.
func TestConnectBusy(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	const otherCall = "Q4TEST"

	n.LinkEstablished(testChannel, testClient, otherCall, nodeCall, true)
	typeLine(n, "C "+farCall)
	link.reset()

	n.RecConnData(testChannel, testClient, otherCall, nodeCall, ax25.PIDNoLayer3, []byte("C Q3TEST\r"))

	assert.Equal(t, "SAMNOD:Q1TEST-7} Busy - Q3TEST is already connected to this node\r", link.output())
	assert.Len(t, link.connects, 1)
}

// What came after CONNECT in the same frame is not taken as cancelling it:
// it was sent before the user could know what CONNECT would do.
func TestConnectIgnoresRestOfFrame(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("C Q3TEST\rXYZZY\r"))

	assert.Equal(t, "SAMNOD:Q1TEST-7} Trying Q3TEST on port 1\r", link.output())
	assert.Empty(t, link.disconnects)
}

// Regression test: what the user typed while the downlink came up went
// nowhere, so there was no giving up on a station that was not there, short
// of waiting for every retry to run out.
func TestConnectCancelled(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	typeLine(n, "C "+farCall)
	link.reset()

	typeLine(n, "XYZZY")
	typeLine(n, "XYZZY")

	assert.Equal(t, []string{farCall}, link.disconnects)

	n.LinkTerminated(testChannel, testClient, farCall, nodeCall, false)
	typeLine(n, "XYZZY")

	assert.Equal(t, "SAMNOD:Q1TEST-7} Gave up on Q3TEST\r"+invalidCommand, link.output())
}

func TestByeWhileConnecting(t *testing.T) {
	t.Parallel()

	for name, typed := range map[string][]string{
		"at once":         {"BYE"},
		"after giving up": {"XYZZY", "BYE"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var n, link = connected(t)

			typeLine(n, "C "+farCall)

			for _, line := range typed {
				typeLine(n, line)
			}

			assert.Equal(t, []string{farCall, userCall}, link.disconnects)
		})
	}
}

// Regression test: part of a line sent after CONNECT, in the same frame, was
// kept, and run into the first line typed back at the node.
func TestConnectDropsPartLine(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("C Q3TEST\rXY"))
	n.LinkTerminated(testChannel, testClient, farCall, nodeCall, true)
	typeLine(n, "BYE")

	assert.Equal(t, []string{userCall}, link.disconnects)
}

// Regression test: the data link says a link is established, as though the
// far end had connected to us, when the far end resets it - which made a
// downlink a session of its own, greeted as a user, and left it there.
func TestDownlinkReset(t *testing.T) {
	t.Parallel()

	var n, link = connectedOnward(t)

	n.LinkEstablished(testChannel, testClient, farCall, nodeCall, true)
	n.RecConnData(testChannel, testClient, farCall, nodeCall, ax25.PIDNoLayer3, []byte("BYE\r"))

	assert.Equal(t, []sent{{channel: testChannel, from: nodeCall, to: userCall, data: "BYE\r"}}, link.sent)
	assert.Empty(t, link.disconnects)

	link.reset()
	n.LinkTerminated(testChannel, testClient, farCall, nodeCall, false)
	typeLine(n, "C "+farCall)

	assert.Equal(t, "SAMNOD:Q1TEST-7} Disconnected from Q3TEST\r"+
		"SAMNOD:Q1TEST-7} Trying Q3TEST on port 1\r", link.output())
}

// A user's link reset while they are connected onward leaves them so.
func TestUserResetWhileConnectedOnward(t *testing.T) {
	t.Parallel()

	var n, link = connectedOnward(t)

	n.LinkEstablished(testChannel, testClient, userCall, nodeCall, true)
	typeLine(n, "hello")

	assert.Equal(t, []sent{{channel: testChannel, from: nodeCall, to: farCall, data: "hello\r"}}, link.sent)
}

func TestConnectedPassesDataBothWays(t *testing.T) {
	t.Parallel()

	var n, link = connectedOnward(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("BYE\rhello"))
	n.RecConnData(testChannel, testClient, farCall, nodeCall, ax25.PIDNoLayer3, []byte("hi there\r"))

	assert.Equal(t, []sent{
		{channel: testChannel, from: nodeCall, to: farCall, data: "BYE\rhello"},
		{channel: testChannel, from: nodeCall, to: userCall, data: "hi there\r"},
	}, link.sent)
	assert.Empty(t, link.disconnects)
}

func TestDownlinkDisconnectReturnsToNode(t *testing.T) {
	t.Parallel()

	var n, link = connectedOnward(t)

	n.LinkTerminated(testChannel, testClient, farCall, nodeCall, false)
	typeLine(n, "XYZZY")

	assert.Equal(t, "SAMNOD:Q1TEST-7} Disconnected from Q3TEST\r"+invalidCommand, link.output())

	// And the station can be connected to again.
	link.reset()
	typeLine(n, "C "+farCall)
	assert.Equal(t, "SAMNOD:Q1TEST-7} Trying Q3TEST on port 1\r", link.output())
}

func TestConnectFails(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		timeout bool
		want    string
	}{
		{"no answer", true, "Failure with Q3TEST - no answer"},
		{"refused", false, "Failure with Q3TEST - busy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var n, link = connected(t)

			typeLine(n, "C "+farCall)
			link.reset()

			n.LinkTerminated(testChannel, testClient, farCall, nodeCall, tc.timeout)
			typeLine(n, "XYZZY")

			assert.Equal(t, "SAMNOD:Q1TEST-7} "+tc.want+"\r"+invalidCommand, link.output())
		})
	}
}

// What a station that is not the node's to talk to sends is not taken for a
// user's.
func TestUnknownLinkIgnored(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	n.RecConnData(testChannel, testClient, farCall, nodeCall, ax25.PIDNoLayer3, []byte("BYE\r"))
	n.LinkEstablished(testChannel, testClient, farCall, nodeCall, false)
	n.LinkTerminated(testChannel, testClient, farCall, nodeCall, false)

	assert.Empty(t, link.sent)
	assert.Empty(t, link.disconnects)
}

// Regression test: part of a line typed while the downlink came up was kept,
// and run into the first line typed back at the node.
func TestPartLineWhileConnectingDropped(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	typeLine(n, "C "+farCall)
	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("XY"))
	n.LinkEstablished(testChannel, testClient, farCall, nodeCall, false)
	n.LinkTerminated(testChannel, testClient, farCall, nodeCall, false)
	typeLine(n, "BYE")

	assert.Equal(t, []string{userCall}, link.disconnects)
}

// Regression test: a downlink given up on could still come up - the far
// end's SABM crossing our DISC - and what it sent then reached the user,
// back at the node.
func TestCancelledDownlinkComingUpAnyway(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	typeLine(n, "C "+farCall)
	typeLine(n, "XYZZY")
	link.reset()

	n.LinkEstablished(testChannel, testClient, farCall, nodeCall, true)
	n.RecConnData(testChannel, testClient, farCall, nodeCall, ax25.PIDNoLayer3, []byte("hello\r"))

	assert.Empty(t, link.output())
	assert.Equal(t, []string{farCall, farCall}, link.disconnects)
}

// Regression test: the protocol ID of what crossed the bridge was dropped,
// and everything sent on as plain text.
func TestConnectedPassesPID(t *testing.T) {
	t.Parallel()

	const pidNetROM = 0xcf

	var n, link = connectedOnward(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, pidNetROM, []byte("up"))
	n.RecConnData(testChannel, testClient, farCall, nodeCall, pidNetROM, []byte("down"))

	assert.Equal(t, []int{pidNetROM, pidNetROM}, link.pids)
}

func TestUserLeavingTakesDownlink(t *testing.T) {
	t.Parallel()

	var n, link = connectedOnward(t)

	n.LinkTerminated(testChannel, testClient, userCall, nodeCall, false)

	assert.Equal(t, []string{farCall}, link.disconnects)

	// When the downlink goes too, nobody is left to tell.
	n.LinkTerminated(testChannel, testClient, farCall, nodeCall, false)

	assert.Empty(t, link.output())
}

func TestUserLeavingWhileConnecting(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	typeLine(n, "C "+farCall)
	link.reset()

	n.LinkTerminated(testChannel, testClient, userCall, nodeCall, false)

	assert.Equal(t, []string{farCall}, link.disconnects)

	// The downlink may come up before the disconnect gets to it, which
	// is no reason to tell anyone, or to leave it up.
	n.LinkEstablished(testChannel, testClient, farCall, nodeCall, false)
	n.RecConnData(testChannel, testClient, farCall, nodeCall, ax25.PIDNoLayer3, []byte("hello\r"))

	assert.Empty(t, link.output())
	assert.Equal(t, []string{farCall, farCall}, link.disconnects)
}

// A user who leaves and comes back while their old downlink is going down
// is not handed what was meant for their old session.
func TestUserReturningIsNotHandedOldDownlink(t *testing.T) {
	t.Parallel()

	var n, link = connected(t)

	typeLine(n, "C "+farCall)
	n.LinkTerminated(testChannel, testClient, userCall, nodeCall, false)
	n.LinkEstablished(testChannel, testClient, userCall, nodeCall, true)
	link.reset()

	n.LinkEstablished(testChannel, testClient, farCall, nodeCall, false)
	n.RecConnData(testChannel, testClient, farCall, nodeCall, ax25.PIDNoLayer3, []byte("hello\r"))
	n.LinkTerminated(testChannel, testClient, farCall, nodeCall, false)

	assert.Empty(t, link.output())

	// Back at the node, and free to connect again.
	typeLine(n, "C "+farCall)
	assert.Equal(t, "SAMNOD:Q1TEST-7} Trying Q3TEST on port 1\r", link.output())
}

// farNode is a node NET/ROM knows, reached through a neighbour.
const (
	farNode     = "Q3TEST-7"
	farNodeName = "QNODEC:Q3TEST-7"
)

// connectedOverCircuit makes a node with a user connected to it, and through
// it over a circuit to farNode, and forgets all that was said on the way.
func connectedOverCircuit(t *testing.T) (*Node, *fakeLink, *fakeNetROM) {
	t.Helper()

	var n, link, nr = connectedWithNetROMOn(t, testConfig(), testNetROM())

	typeLine(n, "C "+farNode)
	n.Circuits().LinkEstablished(testChannel, testClient, farNode, userCall, false)
	link.reset()

	return n, link, nr
}

func TestConnectToNode(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"QNODEC", "qnodec", "Q3TEST-7", "q3test-7"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var n, link, nr = connectedWithNetROMOn(t, testConfig(), testNetROM())

			typeLine(n, "C "+name)

			assert.Equal(t, "SAMNOD:Q1TEST-7} Trying QNODEC:Q3TEST-7\r", link.output())
			assert.Equal(t, []sent{{channel: testChannel, from: userCall, to: farNode, data: ""}}, nr.connects)
			assert.Empty(t, link.connects)

			link.reset()
			n.Circuits().LinkEstablished(testChannel, testClient, farNode, userCall, false)

			assert.Equal(t, "SAMNOD:Q1TEST-7} Connected to QNODEC:Q3TEST-7\r", link.output())
		})
	}
}

// Naming the port asks for a station on it, even one that is a node too.
func TestConnectToNodeOnPort(t *testing.T) {
	t.Parallel()

	var n, link, nr = connectedWithNetROMOn(t, testConfig(), testNetROM())

	typeLine(n, "C 1 QNODEC")

	assert.Equal(t, "SAMNOD:Q1TEST-7} Trying QNODEC on port 1\r", link.output())
	assert.Equal(t, []sent{{channel: testChannel, from: nodeCall, to: "QNODEC", data: ""}}, link.connects)
	assert.Empty(t, nr.connects)
}

// A name NET/ROM does not know is a station on the only port, as it is
// without NET/ROM.
func TestConnectToStationNotNode(t *testing.T) {
	t.Parallel()

	var n, link, nr = connectedWithNetROMOn(t, testConfig(), testNetROM())

	typeLine(n, "C Q9TEST")

	assert.Equal(t, "SAMNOD:Q1TEST-7} Trying Q9TEST on port 1\r", link.output())
	assert.Equal(t, []sent{{channel: testChannel, from: nodeCall, to: "Q9TEST", data: ""}}, link.connects)
	assert.Empty(t, nr.connects)
}

func TestConnectNoSuchNode(t *testing.T) {
	t.Parallel()

	var n, link, nr = connectedWithNetROMOn(t, testConfig(onChannels(testChannel, 10)), testNetROM())

	typeLine(n, "C q9test")

	assert.Equal(t, "SAMNOD:Q1TEST-7} No such node Q9TEST - C port callsign for a station\r", link.output())
	assert.Empty(t, link.connects)
	assert.Empty(t, nr.connects)
}

// A user connected to both the node's callsign and its alias can have one
// circuit to a node: NET/ROM knows it by their callsign alone.
func TestConnectToNodeBusy(t *testing.T) {
	t.Parallel()

	var n, link, nr = connectedWithNetROMOn(t, testConfig(), testNetROM())

	n.LinkEstablished(testChannel, testClient, userCall, nodeAlias, true)
	typeLine(n, "C QNODEC")
	link.reset()

	n.RecConnData(testChannel, testClient, userCall, nodeAlias, ax25.PIDNoLayer3, []byte("C QNODEC\r"))

	assert.Equal(t, "SAMNOD:Q1TEST-7} Busy - you already have a circuit to QNODEC:Q3TEST-7\r", link.output())
	assert.Len(t, nr.connects, 1)
}

func TestCircuitPassesDataBothWays(t *testing.T) {
	t.Parallel()

	var n, link, nr = connectedOverCircuit(t)

	typeLine(n, "BYE")
	n.Circuits().RecConnData(testChannel, testClient, farNode, userCall, ax25.PIDNetROM, []byte("hi there\r"))

	assert.Equal(t, []sent{{channel: testChannel, from: userCall, to: farNode, data: "BYE\r"}}, nr.sent)

	// As plain text to the user, not as the NET/ROM frame it travelled in.
	assert.Equal(t, []sent{{channel: testChannel, from: nodeCall, to: userCall, data: "hi there\r"}}, link.sent)
	assert.Equal(t, []int{ax25.PIDNoLayer3}, link.pids)
	assert.Empty(t, link.disconnects)
}

func TestCircuitFails(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		timeout bool
		want    string
	}{
		{"no answer", true, "Failure with QNODEC:Q3TEST-7 - no answer"},
		{"refused", false, "Failure with QNODEC:Q3TEST-7 - busy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var n, link, _ = connectedWithNetROMOn(t, testConfig(), testNetROM())

			typeLine(n, "C QNODEC")
			link.reset()

			n.Circuits().LinkTerminated(testChannel, testClient, farNode, userCall, tc.timeout)
			typeLine(n, "XYZZY")

			assert.Equal(t, "SAMNOD:Q1TEST-7} "+tc.want+"\r"+invalidCommand, link.output())
		})
	}
}

// A circuit the NET/ROM node will not take at all fails at once.
func TestCircuitNotNetROMs(t *testing.T) {
	t.Parallel()

	var n, link, nr = connectedWithNetROMOn(t, testConfig(), testNetROM())
	nr.refuse = true

	typeLine(n, "C QNODEC")
	typeLine(n, "XYZZY")

	assert.Equal(t, "SAMNOD:Q1TEST-7} Trying QNODEC:Q3TEST-7\r"+
		"SAMNOD:Q1TEST-7} Failure with QNODEC:Q3TEST-7 - busy\r"+invalidCommand, link.output())
}

func TestCircuitDisconnectReturnsToNode(t *testing.T) {
	t.Parallel()

	var n, link, _ = connectedOverCircuit(t)

	n.Circuits().LinkTerminated(testChannel, testClient, farNode, userCall, false)
	typeLine(n, "XYZZY")

	assert.Equal(t, "SAMNOD:Q1TEST-7} Disconnected from QNODEC:Q3TEST-7\r"+invalidCommand, link.output())
}

func TestCircuitCancelled(t *testing.T) {
	t.Parallel()

	var n, link, nr = connectedWithNetROMOn(t, testConfig(), testNetROM())

	typeLine(n, "C QNODEC")
	typeLine(n, "XYZZY")

	assert.Equal(t, []string{farNode}, nr.disconnects)
	assert.Empty(t, link.disconnects)

	link.reset()
	n.Circuits().LinkTerminated(testChannel, testClient, farNode, userCall, false)

	assert.Equal(t, "SAMNOD:Q1TEST-7} Gave up on QNODEC:Q3TEST-7\r", link.output())
}

func TestUserLeavingTakesCircuit(t *testing.T) {
	t.Parallel()

	var n, link, nr = connectedOverCircuit(t)

	n.LinkTerminated(testChannel, testClient, userCall, nodeCall, false)

	assert.Equal(t, []string{farNode}, nr.disconnects)
	assert.Empty(t, link.disconnects)

	n.Circuits().LinkTerminated(testChannel, testClient, farNode, userCall, false)

	assert.Empty(t, link.output())
}

// A circuit and an AX.25 link between the same callsigns are not the same
// downlink.
func TestCircuitIsNotALink(t *testing.T) {
	t.Parallel()

	var n, link, nr = connectedOverCircuit(t)

	n.LinkTerminated(testChannel, testClient, farNode, userCall, false)
	n.RecConnData(testChannel, testClient, farNode, userCall, ax25.PIDNoLayer3, []byte("hello\r"))
	typeLine(n, "hi")

	assert.Empty(t, link.output())
	assert.Equal(t, []sent{{channel: testChannel, from: userCall, to: farNode, data: "hi\r"}}, nr.sent)
}

func TestHelpWithNetROM(t *testing.T) {
	t.Parallel()

	var n, link, _ = connectedWithNetROMOn(t, testConfig(), testNetROM())

	typeLine(n, "?")

	assert.Contains(t, link.output(), "  CONNECT, C - Connect to a station: C [port] callsign, or a node: C node\r")
}

func TestConnectUsageWithNetROM(t *testing.T) {
	t.Parallel()

	var n, link, _ = connectedWithNetROMOn(t, testConfig(), testNetROM())

	typeLine(n, "C")

	assert.Equal(t, "SAMNOD:Q1TEST-7} Usage: C [port] callsign, or C node\r", link.output())
}
