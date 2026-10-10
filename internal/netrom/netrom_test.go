// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

import (
	"bytes"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallRoundTrip(t *testing.T) {
	for _, call := range []string{"Q1TEST", "Q2TEST-15", "Q3T-1", "A"} {
		var b, err = encodeCall(nil, call)
		require.NoError(t, err)
		require.Len(t, b, CallLen)

		var got, derr = decodeCall(b)
		require.NoError(t, derr)
		assert.Equal(t, call, got)
	}
}

func TestCallRejectsBad(t *testing.T) {
	for _, call := range []string{"", "TOOLONGX", "Q1TEST-16", "Q1 TEST", "Q1TEST-X"} {
		var _, err = encodeCall(nil, call)
		require.Error(t, err, call)
	}

	// An embedded space, then more characters.
	var b, _ = encodeCall(nil, "Q1")
	b[3] = 'X' << 1
	var _, err = decodeCall(b)
	assert.Error(t, err)
}

func TestNormaliseCall(t *testing.T) {
	var got, err = NormaliseCall(" q1test-0 ")
	require.NoError(t, err)
	assert.Equal(t, "Q1TEST", got)
}

func TestNodesRoundTrip(t *testing.T) {
	var entries []NodesEntry
	for i := range 25 {
		entries = append(entries, NodesEntry{
			Call:          "Q1TEST-" + string(rune('0'+i%10)),
			Alias:         "NODE",
			BestNeighbour: "Q2TEST",
			Quality:       i * 10,
		})
	}

	// The SSID "-0" is dropped in decoding.
	entries[0].Call = "Q1TEST"
	entries[10].Call = "Q1TEST"
	entries[20].Call = "Q1TEST"

	var frames, err = EncodeNodes("ALIAS", entries)
	require.NoError(t, err)
	require.Len(t, frames, 3, "eleven entries a frame")

	var got []NodesEntry

	for _, f := range frames {
		assert.LessOrEqual(t, len(f), 256)

		var nb, derr = DecodeNodes(f)
		require.NoError(t, derr)
		assert.Equal(t, "ALIAS", nb.Alias)

		got = append(got, nb.Entries...)
	}

	assert.Equal(t, entries, got)
}

func TestNodesEmptyStillAnnounces(t *testing.T) {
	var frames, err = EncodeNodes("ALIAS", nil)
	require.NoError(t, err)
	require.Len(t, frames, 1)

	var nb, derr = DecodeNodes(frames[0])
	require.NoError(t, derr)
	assert.Equal(t, "ALIAS", nb.Alias)
	assert.Empty(t, nb.Entries)
}

func TestNodesDecodeSkipsBadEntriesAndTrailer(t *testing.T) {
	var frames, err = EncodeNodes("A", []NodesEntry{
		{Call: "Q1TEST", Alias: "ONE", BestNeighbour: "Q2TEST", Quality: 100},
		{Call: "Q3TEST", Alias: "TWO", BestNeighbour: "Q2TEST", Quality: 100},
	})
	require.NoError(t, err)

	var f = frames[0]
	f[1+AliasLen] = 0x01 // Spoil the first entry's callsign.
	f = append(f, 1, 2, 3)

	var nb, derr = DecodeNodes(f)
	require.NoError(t, derr)
	require.Len(t, nb.Entries, 1)
	assert.Equal(t, "Q3TEST", nb.Entries[0].Call)
}

func TestNodesDecodeRejects(t *testing.T) {
	for _, info := range [][]byte{nil, {0xff}, {0xfe, 'A', ' ', ' ', ' ', ' ', ' '}, {0xff, '!', ' ', ' ', ' ', ' ', ' '}} {
		var _, err = DecodeNodes(info)
		assert.Error(t, err)
	}
}

func TestPacketRoundTrip(t *testing.T) {
	var p = Packet{
		Origin: "Q1TEST", Destination: "Q2TEST-3", TTL: 7,
		Index: 1, ID: 2, TxSeq: 3, RxSeq: 4,
		Opcode: OpInfo, Flags: FlagMore | FlagChoke,
		Payload: []byte("hello"),
	}

	var b, err = p.Encode()
	require.NoError(t, err)
	assert.Len(t, b, L3HeaderLen+L4HeaderLen+5)

	var got, derr = DecodePacket(b)
	require.NoError(t, derr)
	assert.Equal(t, p, got)
}

func TestPacketDecodeRejectsINP3(t *testing.T) {
	var b = make([]byte, 40)
	b[0] = 0xff

	var _, err = DecodePacket(b)
	assert.Error(t, err)
}

func TestConnectRequestRoundTrip(t *testing.T) {
	for _, cr := range []ConnectRequest{
		{Window: 4, User: "Q1TEST", Node: "Q2TEST-1", Timeout: maybe.Nothing[int]()},
		{Window: 127, User: "Q1TEST", Node: "Q2TEST", Timeout: maybe.Just(300)},
	} {
		var b, err = cr.Encode()
		require.NoError(t, err)

		var got, derr = DecodeConnectRequest(b)
		require.NoError(t, derr)
		assert.Equal(t, cr, got)
	}
}

func TestTableDeratesQuality(t *testing.T) {
	var tab = NewTable(TableConfig{MyCall: "Q1TEST", MinQuality: 50, ObsolescenceInit: 6})
	var key = NeighbourKey{Port: 0, Call: "Q2TEST"}

	var changed = tab.Heard(key, 192, NodesBroadcast{Alias: "TWO", Entries: []NodesEntry{
		{Call: "Q3TEST", Alias: "THREE", BestNeighbour: "Q4TEST", Quality: 192},
		{Call: "Q5TEST", Alias: "FIVE", BestNeighbour: "Q1TEST", Quality: 255}, // Back through us.
		{Call: "Q6TEST", Alias: "SIX", BestNeighbour: "Q4TEST", Quality: 40},   // Too poor once derated.
	}})
	assert.True(t, changed)

	var two, ok = tab.Lookup("TWO")
	require.True(t, ok)
	assert.Equal(t, 192, two.Routes[0].Quality)

	var three, ok3 = tab.Lookup("Q3TEST")
	require.True(t, ok3)
	assert.Equal(t, "THREE", three.Alias)
	assert.Equal(t, (192*192+128)/256, three.Routes[0].Quality)

	var _, ok5 = tab.Lookup("Q5TEST")
	assert.False(t, ok5)

	var _, ok6 = tab.Lookup("SIX")
	assert.False(t, ok6)
}

func TestTableKeepsBestRoutes(t *testing.T) {
	var tab = NewTable(TableConfig{MyCall: "Q1TEST", MinQuality: 1, ObsolescenceInit: 6})

	for i, q := range []int{100, 200, 150, 50} {
		var key = NeighbourKey{Port: 0, Call: "Q2TEST-" + string(rune('1'+i))}
		tab.Heard(key, 255, NodesBroadcast{Alias: "N", Entries: []NodesEntry{
			{Call: "Q9TEST", Alias: "DEST", BestNeighbour: "Q8TEST", Quality: q},
		}})
	}

	var d, ok = tab.Lookup("DEST")
	require.True(t, ok)
	require.Len(t, d.Routes, MaxRoutes)
	assert.Equal(t, "Q2TEST-2", d.Routes[0].Neighbour.Call)
	assert.Equal(t, "Q2TEST-3", d.Routes[1].Neighbour.Call)
	assert.Equal(t, "Q2TEST-1", d.Routes[2].Neighbour.Call)
}

func TestTableAgesOut(t *testing.T) {
	var tab = NewTable(TableConfig{MyCall: "Q1TEST", MinQuality: 1, ObsolescenceInit: 2})
	var key = NeighbourKey{Port: 0, Call: "Q2TEST"}
	tab.Heard(key, 200, NodesBroadcast{Alias: "TWO", Entries: nil})
	tab.LockNeighbour(NeighbourKey{Port: 1, Call: "Q3TEST"}, "THREE", 255)

	assert.False(t, tab.Age())
	assert.Len(t, tab.Advertise(5), 1, "only the locked route is fresh enough to advertise")
	assert.True(t, tab.Age())

	var _, ok = tab.Lookup("TWO")
	assert.False(t, ok)
	assert.Len(t, tab.Neighbours(), 1)

	var _, ok3 = tab.Lookup("THREE")
	assert.True(t, ok3, "locked routes never age")
}

func TestTableLockedQualityNotOverridden(t *testing.T) {
	var tab = NewTable(TableConfig{MyCall: "Q1TEST", MinQuality: 1, ObsolescenceInit: 6})
	var key = NeighbourKey{Port: 0, Call: "Q2TEST"}
	tab.LockNeighbour(key, "TWO", 100)
	tab.Heard(key, 255, NodesBroadcast{Alias: "TWO", Entries: []NodesEntry{
		{Call: "Q3TEST", Alias: "THREE", BestNeighbour: "Q4TEST", Quality: 255},
	}})

	var d, _ = tab.Lookup("TWO")
	assert.Equal(t, 100, d.Routes[0].Quality)

	var three, _ = tab.Lookup("THREE")
	assert.Equal(t, (100*255+128)/256, three.Routes[0].Quality)
}

// testNet is a network of Routers whose links are a fixed graph, delivering
// what they send when pumped, and losing what drop says to.
type testNet struct {
	t       *testing.T
	now     time.Time
	routers map[string]*Router
	hears   map[string][]string // Who hears whom, on port 0.
	queue   []testFrame
	drop    func(f testFrame) bool
}

type testFrame struct {
	from, to string
	nodes    bool
	data     []byte
}

type testLink struct {
	net  *testNet
	call string
}

func (l testLink) SendPacket(to NeighbourKey, packet []byte) {
	l.net.queue = append(l.net.queue, testFrame{from: l.call, to: to.Call, nodes: false, data: packet})
}

func (l testLink) BroadcastNodes(_ int, info []byte) {
	for _, to := range l.net.hears[l.call] {
		l.net.queue = append(l.net.queue, testFrame{from: l.call, to: to, nodes: true, data: info})
	}
}

func newTestNet(t *testing.T, links [][2]string, aliases map[string]string) *testNet {
	t.Helper()

	var n = &testNet{t: t, now: time.Unix(1_000_000, 0), routers: map[string]*Router{}, hears: map[string][]string{}, queue: nil, drop: nil}

	for _, l := range links {
		n.hears[l[0]] = append(n.hears[l[0]], l[1])
		n.hears[l[1]] = append(n.hears[l[1]], l[0])
	}

	for call, alias := range aliases {
		var cfg = DefaultConfig()
		cfg.Call = call
		cfg.Alias = alias
		cfg.Ports = []PortConfig{{Port: 0, Quality: 192, Broadcast: true}}

		var r, err = NewRouter(cfg, testLink{net: n, call: call}, n.now)
		require.NoError(t, err)

		n.routers[call] = r
	}

	return n
}

func (n *testNet) pump() {
	for i := 0; len(n.queue) > 0; i++ {
		require.Less(n.t, i, 10000, "the network never went quiet")

		var f = n.queue[0]
		n.queue = n.queue[1:]

		if n.drop != nil && n.drop(f) {
			continue
		}

		var r = n.routers[f.to]
		if f.nodes {
			r.HeardNodes(0, f.from, f.data, n.now)
		} else {
			r.ReceivePacket(NeighbourKey{Port: 0, Call: f.from}, f.data, n.now)
		}
	}
}

func (n *testNet) advance(d time.Duration) {
	n.now = n.now.Add(d)
	for _, call := range []string{"Q1TEST", "Q2TEST", "Q3TEST"} {
		if r, ok := n.routers[call]; ok {
			r.Tick(n.now)
		}
	}

	n.pump()
}

// recorder is a CircuitHandler that remembers what it was told.
type recorder struct {
	connected bool
	data      bytes.Buffer
	closed    bool
	err       error
}

func (r *recorder) Connected(*Circuit)            { r.connected = true }
func (r *recorder) Received(_ *Circuit, d []byte) { r.data.Write(d) }
func (r *recorder) Closed(_ *Circuit, err error)  { r.closed = true; r.err = err }

func lineNet(t *testing.T) *testNet {
	t.Helper()

	var n = newTestNet(t,
		[][2]string{{"Q1TEST", "Q2TEST"}, {"Q2TEST", "Q3TEST"}},
		map[string]string{"Q1TEST": "ONE", "Q2TEST": "TWO", "Q3TEST": "THREE"})

	// One round of broadcasts to learn the neighbours, a second to learn
	// what is beyond them.
	n.advance(0)
	n.advance(DefaultConfig().BroadcastInterval)

	return n
}

func TestNetworkLearnsRoutes(t *testing.T) {
	var n = lineNet(t)

	var d, ok = n.routers["Q1TEST"].Table().Lookup("THREE")
	require.True(t, ok)
	assert.Equal(t, "Q3TEST", d.Call)
	assert.Equal(t, "Q2TEST", d.Routes[0].Neighbour.Call)
	assert.Equal(t, (192*192+128)/256, d.Routes[0].Quality)
}

func TestCircuitCarriesDataBothWays(t *testing.T) {
	var n = lineNet(t)

	var far = new(recorder)

	require.NoError(t, n.routers["Q3TEST"].Listen("Q3TEST", "THREE", 0, func(c *Circuit) CircuitHandler {
		assert.Equal(t, "Q1TEST-5", c.User())
		assert.Equal(t, "Q1TEST", c.UserNode())

		return far
	}))

	var near = new(recorder)

	var c, err = n.routers["Q1TEST"].Connect("THREE", "Q1TEST-5", near, n.now)
	require.NoError(t, err)
	n.pump()

	require.True(t, near.connected)
	require.True(t, far.connected)

	// More than fits one packet, and more than one window's worth.
	var big = bytes.Repeat([]byte("0123456789"), 300)
	require.NoError(t, c.Write(big))
	n.pump()
	n.advance(DefaultConfig().AckDelay)
	assert.Equal(t, big, far.data.Bytes())

	var farCircuit = n.routers["Q3TEST"].sortedCircuits()[0]
	require.NoError(t, farCircuit.Write([]byte("reply")))
	n.pump()
	assert.Equal(t, "reply", near.data.String())

	c.Close()
	n.pump()
	assert.True(t, near.closed)
	require.NoError(t, near.err)
	assert.True(t, far.closed)
	require.NoError(t, far.err)
	assert.Empty(t, n.routers["Q1TEST"].Circuits())
	assert.Empty(t, n.routers["Q3TEST"].Circuits())
}

func TestCircuitRecoversFromLoss(t *testing.T) {
	var n = lineNet(t)
	var far = new(recorder)

	require.NoError(t, n.routers["Q3TEST"].Listen("Q3TEST", "THREE", 0, func(*Circuit) CircuitHandler { return far }))

	var near = new(recorder)

	var c, err = n.routers["Q1TEST"].Connect("Q3TEST", "Q1TEST", near, n.now)
	require.NoError(t, err)
	n.pump()
	require.True(t, near.connected)

	// Lose the second information packet the first time it goes by.
	var seen = 0
	n.drop = func(f testFrame) bool {
		var p, perr = DecodePacket(f.data)
		if perr != nil || p.Opcode != OpInfo || f.to != "Q2TEST" {
			return false
		}

		seen++

		return seen == 2
	}

	require.NoError(t, c.Write(bytes.Repeat([]byte("x"), 3*MaxInfoLen)))
	n.pump()

	for range 5 {
		n.advance(DefaultConfig().AckDelay)
	}

	n.advance(DefaultConfig().Timeout)
	assert.Equal(t, 3*MaxInfoLen, far.data.Len())
}

func TestConnectRefusedWithoutListener(t *testing.T) {
	var n = lineNet(t)
	var near = new(recorder)

	var _, err = n.routers["Q1TEST"].Connect("THREE", "Q1TEST", near, n.now)
	require.NoError(t, err)
	n.pump()

	assert.True(t, near.closed)
	assert.ErrorIs(t, near.err, ErrRefused)
}

func TestConnectTimesOut(t *testing.T) {
	var n = lineNet(t)
	n.drop = func(testFrame) bool { return true }

	var near = new(recorder)

	var _, err = n.routers["Q1TEST"].Connect("THREE", "Q1TEST", near, n.now)
	require.NoError(t, err)

	for range DefaultConfig().Retries + 2 {
		n.advance(DefaultConfig().Timeout)
	}

	assert.True(t, near.closed)
	assert.ErrorIs(t, near.err, ErrTimeout)
}

func TestConnectNoRoute(t *testing.T) {
	var n = lineNet(t)

	var _, err = n.routers["Q1TEST"].Connect("NOWHERE", "Q1TEST", new(recorder), n.now)
	assert.ErrorIs(t, err, ErrNoRoute)
}

func TestPacketTTLExpires(t *testing.T) {
	var n = lineNet(t)

	var p = transport(1, 0, 0, 0, OpInfo, 0, nil)
	p.Origin = "Q1TEST"
	p.Destination = "Q3TEST"
	p.TTL = 1
	var b, err = p.Encode()
	require.NoError(t, err)

	n.routers["Q2TEST"].ReceivePacket(NeighbourKey{Port: 0, Call: "Q1TEST"}, b, n.now)
	assert.Empty(t, n.queue, "a packet out of hops is not forwarded")

	p.TTL = 2
	b, _ = p.Encode()
	n.routers["Q2TEST"].ReceivePacket(NeighbourKey{Port: 0, Call: "Q1TEST"}, b, n.now)
	require.Len(t, n.queue, 1)
	assert.Equal(t, "Q3TEST", n.queue[0].to)

	var fwd, _ = DecodePacket(n.queue[0].data)
	assert.Equal(t, 1, fwd.TTL)
}

func TestLocalApplicationAdvertised(t *testing.T) {
	var n = lineNet(t)

	require.NoError(t, n.routers["Q3TEST"].Listen("Q3TEST-1", "BBS", 200, func(*Circuit) CircuitHandler { return new(recorder) }))
	n.advance(DefaultConfig().BroadcastInterval)
	n.advance(DefaultConfig().BroadcastInterval)

	var d, ok = n.routers["Q1TEST"].Table().Lookup("BBS")
	require.True(t, ok)
	assert.Equal(t, "Q3TEST-1", d.Call)
}

func TestCircuitToSelf(t *testing.T) {
	var n = lineNet(t)
	var far = new(recorder)

	require.NoError(t, n.routers["Q1TEST"].Listen("Q1TEST", "ONE", 0, func(*Circuit) CircuitHandler { return far }))

	var near = new(recorder)

	var c, err = n.routers["Q1TEST"].Connect("Q1TEST", "Q1TEST-1", near, n.now)
	require.NoError(t, err)
	assert.True(t, near.connected)
	assert.Empty(t, n.queue, "nothing goes near the radio")

	require.NoError(t, c.Write([]byte("loop")))
	assert.Equal(t, "loop", far.data.String())

	c.Close()
	assert.True(t, far.closed)
	assert.True(t, near.closed)
}

func TestCircuitNAKsOutOfOrder(t *testing.T) {
	var n = lineNet(t)
	var far = new(recorder)

	require.NoError(t, n.routers["Q3TEST"].Listen("Q3TEST", "THREE", 0, func(*Circuit) CircuitHandler { return far }))

	var near = new(recorder)

	var c, err = n.routers["Q1TEST"].Connect("Q3TEST", "Q1TEST", near, n.now)
	require.NoError(t, err)
	n.pump()

	// Hold back the first information packet, so the rest arrive early.
	var held []testFrame

	var naks = 0
	n.drop = func(f testFrame) bool {
		var p, _ = DecodePacket(f.data)
		if p.Opcode == OpInfo && p.TxSeq == 0 && f.to == "Q2TEST" && len(held) == 0 {
			held = append(held, f)

			return true
		}

		if p.Opcode == OpInfoAck && p.Flags&FlagNAK != 0 && f.to == "Q1TEST" {
			naks++
		}

		return false
	}

	require.NoError(t, c.Write(bytes.Repeat([]byte("y"), 3*MaxInfoLen)))
	n.pump()
	assert.Equal(t, 1, naks, "one NAK for the gap")
	assert.Equal(t, 3*MaxInfoLen, far.data.Len(), "the NAK brought the missing packet back")
}

func TestCircuitIdleTimeout(t *testing.T) {
	var n = lineNet(t)
	var far = new(recorder)

	require.NoError(t, n.routers["Q3TEST"].Listen("Q3TEST", "THREE", 0, func(*Circuit) CircuitHandler { return far }))

	var near = new(recorder)

	var _, err = n.routers["Q1TEST"].Connect("Q3TEST", "Q1TEST", near, n.now)
	require.NoError(t, err)
	n.pump()

	n.advance(DefaultConfig().IdleTimeout)
	assert.True(t, near.closed)
	assert.True(t, far.closed)
	assert.Empty(t, n.routers["Q1TEST"].Circuits())
	assert.Empty(t, n.routers["Q3TEST"].Circuits())
}

func TestNeighbourFailedClosesCircuits(t *testing.T) {
	var n = lineNet(t)

	require.NoError(t, n.routers["Q3TEST"].Listen("Q3TEST", "THREE", 0, func(*Circuit) CircuitHandler { return new(recorder) }))

	var near = new(recorder)

	var _, err = n.routers["Q1TEST"].Connect("Q3TEST", "Q1TEST", near, n.now)
	require.NoError(t, err)
	n.pump()

	var events []EventKind
	n.routers["Q1TEST"].OnEvent = func(e Event) { events = append(events, e.Kind) }

	n.routers["Q1TEST"].NeighbourFailed(NeighbourKey{Port: 0, Call: "Q2TEST"}, n.now)
	assert.True(t, near.closed)
	require.ErrorIs(t, near.err, ErrNoRoute)
	assert.Equal(t, []EventKind{EventRoutesChanged, EventCircuitDown}, events)
	assert.Empty(t, n.routers["Q1TEST"].Table().Destinations())
}

func TestFarEndChokes(t *testing.T) {
	var n = lineNet(t)
	var far = new(recorder)

	require.NoError(t, n.routers["Q3TEST"].Listen("Q3TEST", "THREE", 0, func(*Circuit) CircuitHandler { return far }))

	var near = new(recorder)

	var c, err = n.routers["Q1TEST"].Connect("Q3TEST", "Q1TEST", near, n.now)
	require.NoError(t, err)
	n.pump()

	// The far end says it is busy.
	var fc = n.routers["Q3TEST"].sortedCircuits()[0]
	fc.sendInfoAck(FlagChoke)
	n.pump()
	require.True(t, c.peerBusy)

	require.NoError(t, c.Write([]byte("wait")))
	n.pump()
	assert.Zero(t, far.data.Len(), "nothing sent while choked")

	n.advance(DefaultConfig().BusyDelay)
	n.advance(DefaultConfig().AckDelay)
	assert.Equal(t, "wait", far.data.String())
}

func TestConfigValidate(t *testing.T) {
	var cfg = DefaultConfig()
	cfg.Call = "q1test"
	cfg.Alias = "one"
	require.NoError(t, cfg.Validate())
	assert.Equal(t, "Q1TEST", cfg.Call)
	assert.Equal(t, "ONE", cfg.Alias)

	for _, spoil := range []func(*Config){
		func(c *Config) { c.Call = "" },
		func(c *Config) { c.Alias = "TOOLONGALIAS" },
		func(c *Config) { c.Window = 0 },
		func(c *Config) { c.TTL = 256 },
		func(c *Config) { c.Timeout = 0 },
		func(c *Config) { c.Ports = []PortConfig{{Port: 0, Quality: 300, Broadcast: true}} },
		func(c *Config) { c.Neighbours = []LockedNeighbour{{Port: 0, Call: "!", Alias: "", Quality: 1}} },
	} {
		var bad = DefaultConfig()
		bad.Call = "Q1TEST"
		spoil(&bad)
		assert.Error(t, bad.Validate())
	}
}
