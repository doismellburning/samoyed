// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package node

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
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
	sent        []sent
	disconnects []string
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

func (f *fakeLink) XmitDataRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, _ int, _ int, xdata []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()

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
}

// testConfig is the configuration of a node with an alias on testChannel,
// changed by each of options.
func testConfig(options ...func(*Config)) Config {
	var config = Config{
		Address: nodeCall,
		Alias:   nodeAlias,
		Ports:   []Port{{Channel: testChannel, Description: "Radio"}},
		Info:    "",
	}

	for _, option := range options {
		option(&config)
	}

	return config
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

func withInfo(info string) func(*Config) {
	return func(config *Config) {
		config.Info = info
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
	var n = New(config, link, testClient)

	n.LinkEstablished(testChannel, testClient, userCall, nodeCall, true)
	link.reset()

	return n, link
}

func TestStartRegistersCallsignAndAlias(t *testing.T) {
	t.Parallel()

	var link = new(fakeLink)
	var n = New(testConfig(onChannels(0, 2)), link, testClient)

	n.Start()

	assert.Equal(t, []string{nodeCall + "@0/7", nodeAlias + "@0/7", nodeCall + "@2/7", nodeAlias + "@2/7"}, link.registered)
}

func TestStartWithoutAlias(t *testing.T) {
	t.Parallel()

	var link = new(fakeLink)
	var n = New(testConfig(withoutAlias, onChannels(0)), link, testClient)

	n.Start()

	assert.Equal(t, []string{nodeCall + "@0/7"}, link.registered)
}

func TestGreeting(t *testing.T) {
	t.Parallel()

	var link = new(fakeLink)
	var n = New(testConfig(), link, testClient)

	n.LinkEstablished(testChannel, testClient, userCall, nodeCall, true)

	require.Len(t, link.sent, 1)
	assert.Equal(t, sent{channel: testChannel, from: nodeCall, to: userCall, data: "SAMNOD:Q1TEST-7} Welcome to Samoyed\r"}, link.sent[0])
}

func TestGreetingWithoutAlias(t *testing.T) {
	t.Parallel()

	var link = new(fakeLink)
	var n = New(testConfig(withoutAlias), link, testClient)

	n.LinkEstablished(testChannel, testClient, userCall, nodeCall, true)

	assert.Equal(t, "Q1TEST-7} Welcome to Samoyed\r", link.output())
}

// A user who connected to the alias is answered from the alias, which is the
// link the data link has.
func TestAnswersFromTheCallsignConnectedTo(t *testing.T) {
	t.Parallel()

	var link = new(fakeLink)
	var n = New(testConfig(), link, testClient)

	n.LinkEstablished(testChannel, testClient, userCall, nodeAlias, true)
	n.RecConnData(testChannel, testClient, userCall, nodeAlias, ax25.PIDNoLayer3, []byte("XYZZY\r"))

	require.Len(t, link.sent, 2)
	assert.Equal(t, nodeAlias, link.sent[1].from)
}

func TestOutgoingLinkIsNotASession(t *testing.T) {
	t.Parallel()

	var link = new(fakeLink)
	var n = New(testConfig(withoutAlias), link, testClient)

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
				"  HELP, H, ? - List the commands\r"+
				"  INFO, I - About this node\r"+
				"  PORTS, P - List the ports\r", link.output())
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

			var n, link = connectedWith(t, testConfig(withInfo(tc.info)))

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
