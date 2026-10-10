// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Link is how a Router reaches the outside world.  Its methods are called on
// the Router's goroutine, so must not block.
type Link interface {
	// SendPacket hands an encoded NET/ROM packet to a neighbour, over a
	// connected AX.25 link, opening one if there is none yet.
	SendPacket(to NeighbourKey, packet []byte)

	// BroadcastNodes sends the information field of a NODES broadcast as a UI
	// frame on port.
	BroadcastNodes(port int, info []byte)
}

// PortConfig is how NET/ROM uses one channel.
type PortConfig struct {
	Port      int
	Quality   int  // The quality given to neighbours heard on it.
	Broadcast bool // Whether NODES broadcasts are sent on it.
}

// LockedNeighbour is a neighbour configured rather than learned.
type LockedNeighbour struct {
	Port    int
	Call    string
	Alias   string
	Quality int
}

// Config is a Router's configuration.  Zero durations and counts take the
// defaults DefaultConfig gives.
type Config struct {
	Call  string // This node's callsign.
	Alias string // Its alias.

	Ports      []PortConfig
	Neighbours []LockedNeighbour

	MinQuality               int
	ObsolescenceInit         int
	MinObsolescenceBroadcast int
	BroadcastInterval        time.Duration

	TTL         int           // Hops a packet starts out with.
	Window      int           // Transport window offered.
	Timeout     time.Duration // Transport retry timeout.
	Retries     int
	AckDelay    time.Duration
	BusyDelay   time.Duration
	IdleTimeout time.Duration // Zero for none.
}

// DefaultConfig returns the defaults for everything but the node's identity
// and ports, close to what other NET/ROM implementations use.
func DefaultConfig() Config {
	return Config{
		Call:                     "",
		Alias:                    "",
		Ports:                    nil,
		Neighbours:               nil,
		MinQuality:               50,
		ObsolescenceInit:         6,
		MinObsolescenceBroadcast: 5,
		BroadcastInterval:        30 * time.Minute,
		TTL:                      16,
		Window:                   4,
		Timeout:                  120 * time.Second,
		Retries:                  3,
		AckDelay:                 3 * time.Second,
		BusyDelay:                180 * time.Second,
		IdleTimeout:              15 * time.Minute,
	}
}

// Validate checks c, normalising its callsigns and aliases.
func (c *Config) Validate() error {
	var call, err = NormaliseCall(c.Call)
	if err != nil {
		return err
	}

	c.Call = call

	var alias, aerr = NormaliseAlias(c.Alias)
	if aerr != nil {
		return aerr
	}

	c.Alias = alias

	return c.ValidateParameters()
}

// ValidateParameters checks everything in c but the node's own callsign and
// alias, normalising the neighbours' callsigns and aliases.
func (c *Config) ValidateParameters() error {
	for i := range c.Neighbours {
		var n = &c.Neighbours[i]

		var ncall, nerr = NormaliseCall(n.Call)
		if nerr != nil {
			return nerr
		}

		n.Call = ncall

		var nalias, naerr = NormaliseAlias(n.Alias)
		if naerr != nil {
			return naerr
		}

		n.Alias = nalias

		if n.Quality < 0 || n.Quality > 255 {
			return fmt.Errorf("netrom: neighbour %s quality %d not in 0 to 255", n.Call, n.Quality)
		}
	}

	var seen = make(map[int]bool)

	for _, p := range c.Ports {
		if seen[p.Port] {
			return fmt.Errorf("netrom: port %d given twice", p.Port)
		}

		seen[p.Port] = true

		if p.Quality < 0 || p.Quality > 255 {
			return fmt.Errorf("netrom: port %d quality %d not in 0 to 255", p.Port, p.Quality)
		}
	}

	switch {
	case c.MinQuality < 0 || c.MinQuality > 255:
		return fmt.Errorf("netrom: minimum quality %d not in 0 to 255", c.MinQuality)
	case c.ObsolescenceInit < 1 || c.ObsolescenceInit > 255:
		return fmt.Errorf("netrom: obsolescence count %d not in 1 to 255", c.ObsolescenceInit)
	case c.TTL < 1 || c.TTL > 255:
		return fmt.Errorf("netrom: TTL %d not in 1 to 255", c.TTL)
	case c.Window < 1 || c.Window > 127:
		return fmt.Errorf("netrom: window %d not in 1 to 127", c.Window)
	case c.Retries < 0:
		return fmt.Errorf("netrom: retries %d is negative", c.Retries)
	case c.BroadcastInterval <= 0, c.Timeout <= 0, c.AckDelay <= 0, c.BusyDelay <= 0, c.IdleTimeout < 0:
		return errors.New("netrom: intervals and timeouts must be positive")
	}

	return nil
}

// Acceptor decides whether to accept an incoming circuit, returning who is to
// be told what happens on it, or nil to refuse it.
type Acceptor func(c *Circuit) CircuitHandler

type listener struct {
	alias   string
	quality int
	accept  Acceptor
}

type circuitKey struct {
	index, id byte
}

// Event is something that happened, for anyone watching a Router.
type Event struct {
	Kind    EventKind
	Circuit CircuitInfo // For the circuit events.
	Err     error       // Why a circuit closed, if not in an orderly way.
}

func routesChanged() Event {
	var none CircuitInfo

	return Event{Kind: EventRoutesChanged, Circuit: none, Err: nil}
}

func circuitEvent(kind EventKind, c *Circuit, err error) Event {
	return Event{Kind: kind, Circuit: c.Info(), Err: err}
}

// EventKind says what an Event is about.
type EventKind int

const (
	EventRoutesChanged EventKind = iota
	EventCircuitUp
	EventCircuitDown
)

// Router is a NET/ROM node's network and transport layers.
type Router struct {
	cfg  Config
	link Link
	now  time.Time

	table     *Table
	listeners map[string]*listener
	circuits  map[circuitKey]*Circuit
	nextIndex byte
	nextID    byte

	nextBroadcast time.Time

	// looped holds packets this node sent to itself, delivered once whatever
	// sent them has finished, rather than from inside it.
	looped []Packet

	// OnEvent, if set, is told what happens, on the Router's goroutine.
	OnEvent func(Event)
}

// NewRouter returns a Router for cfg, sending through link, starting at now.
func NewRouter(cfg Config, link Link, now time.Time) (*Router, error) {
	var err = cfg.Validate()
	if err != nil {
		return nil, err
	}

	var r = new(Router)
	r.cfg = cfg
	r.link = link
	r.now = now
	r.table = NewTable(TableConfig{MyCall: cfg.Call, MinQuality: cfg.MinQuality, ObsolescenceInit: cfg.ObsolescenceInit})
	r.listeners = make(map[string]*listener)
	r.circuits = make(map[circuitKey]*Circuit)
	r.nextIndex = 1
	r.nextBroadcast = now // Introduce ourselves straight away.

	for _, n := range cfg.Neighbours {
		r.table.LockNeighbour(NeighbourKey{Port: n.Port, Call: n.Call}, n.Alias, n.Quality)
	}

	return r, nil
}

// Config returns the Router's configuration.
func (r *Router) Config() Config { return r.cfg }

// Table returns the routing table, which the caller must only read, and only
// on the Router's goroutine.
func (r *Router) Table() *Table { return r.table }

// Circuits returns a snapshot of every circuit.
func (r *Router) Circuits() []CircuitInfo {
	var infos = make([]CircuitInfo, 0, len(r.circuits))
	for _, c := range r.circuits {
		infos = append(infos, c.Info())
	}

	slices.SortFunc(infos, func(a, b CircuitInfo) int { return a.Started.Compare(b.Started) })

	return infos
}

// Listen accepts circuits to call, which may be the node's own or another this
// node answers for - an application's.  Any other call is advertised in NODES
// broadcasts, with alias and quality, so the network can find it.
func (r *Router) Listen(call string, alias string, quality int, accept Acceptor) error {
	var c, err = NormaliseCall(call)
	if err != nil {
		return err
	}

	var a, aerr = NormaliseAlias(alias)
	if aerr != nil {
		return aerr
	}

	if quality < 0 || quality > 255 {
		return fmt.Errorf("netrom: quality %d not in 0 to 255", quality)
	}

	r.listeners[c] = &listener{alias: a, quality: quality, accept: accept}

	return nil
}

// isLocal says whether call is this node or one it answers for.
func (r *Router) isLocal(call string) bool {
	if call == r.cfg.Call {
		return true
	}

	var _, ok = r.listeners[call]

	return ok
}

// HeardNodes takes in a NODES broadcast's information field, heard from the
// station from on port.
func (r *Router) HeardNodes(port int, from string, info []byte, now time.Time) {
	r.now = now

	var pc, ok = r.cfg.PortConfig(port)
	if !ok {
		return // Not a port we do NET/ROM on.
	}

	var call, err = NormaliseCall(from)
	if err != nil {
		return
	}

	var nb, derr = DecodeNodes(info)
	if derr != nil {
		return
	}

	if r.table.Heard(NeighbourKey{Port: port, Call: call}, pc.Quality, nb) {
		r.emit(routesChanged())
	}
}

// PortConfig returns the configuration of port, if NET/ROM runs on it.
func (c *Config) PortConfig(port int) (PortConfig, bool) {
	for _, p := range c.Ports {
		if p.Port == port {
			return p, true
		}
	}

	var none PortConfig

	return none, false
}

// ReceivePacket takes in a NET/ROM packet a neighbour sent over AX.25.
func (r *Router) ReceivePacket(from NeighbourKey, b []byte, now time.Time) {
	r.now = now

	var p, err = DecodePacket(b)
	if err != nil {
		return
	}

	if r.isLocal(p.Destination) {
		r.receiveTransport(p)
		r.deliverLooped()

		return
	}

	// Not ours: pass it on, unless it has gone far enough.
	p.TTL--
	if p.TTL <= 0 {
		return
	}

	var to, ok = r.nextHop(p.Destination, from)
	if !ok {
		return
	}

	var out, eerr = p.Encode()
	if eerr != nil {
		return
	}

	r.link.SendPacket(to, out)
}

// nextHop picks the neighbour to send a packet for dest to, other than the one
// it came from, which would only send it back.
func (r *Router) nextHop(dest string, from NeighbourKey) (NeighbourKey, bool) {
	var none NeighbourKey

	var d, ok = r.table.Lookup(dest)
	if !ok || d.Call != dest {
		return none, false
	}

	for _, route := range d.Routes {
		if route.Neighbour != from {
			return route.Neighbour, true
		}
	}

	return none, false
}

// send sends a transport packet from our address local to the node dest.
func (r *Router) send(local string, dest string, p Packet) {
	p.Origin = local
	p.Destination = dest
	p.TTL = r.cfg.TTL

	var b, err = p.Encode()
	if err != nil {
		return
	}

	if r.isLocal(dest) {
		// A circuit to ourselves goes nowhere near the radio.
		var looped, _ = DecodePacket(b)
		r.looped = append(r.looped, looped)

		return
	}

	var to, ok = r.nextHop(dest, NeighbourKey{Port: -1, Call: ""})
	if !ok {
		return
	}

	r.link.SendPacket(to, b)
}

// deliverLooped delivers the packets this node sent itself, and any those
// provoke in turn.
func (r *Router) deliverLooped() {
	for len(r.looped) > 0 {
		var p = r.looped[0]
		r.looped = r.looped[1:]
		r.receiveTransport(p)
	}
}

func (r *Router) receiveTransport(p Packet) {
	if p.Opcode == OpConnectRequest {
		r.receiveConnectRequest(p)

		return
	}

	if p.Index == 0 && p.ID == 0 {
		return
	}

	var c, ok = r.circuits[circuitKey{p.Index, p.ID}]
	if !ok || c.remote != p.Origin {
		return
	}

	c.receive(p)
}

func (r *Router) receiveConnectRequest(p Packet) {
	// A repeat of one already accepted, whose ack went missing.
	for _, c := range r.circuits {
		if c.incoming && c.remote == p.Origin && c.yourIndex == p.Index && c.yourID == p.ID {
			c.receive(p)

			return
		}
	}

	var cr, err = DecodeConnectRequest(p.Payload)
	if err != nil {
		return
	}

	var l, ok = r.listeners[p.Destination]
	if !ok || l.accept == nil {
		r.refuse(p)

		return
	}

	var key, free = r.allocate()
	if !free {
		r.refuse(p)

		return
	}

	var c = r.newCircuit(key, p.Destination, p.Origin)
	c.incoming = true
	c.yourIndex = p.Index
	c.yourID = p.ID
	c.user = cr.User
	c.userNode = cr.Node
	c.window = min(r.cfg.Window, max(cr.Window, 1))
	c.state = CircuitConnected

	if timeout, has := cr.Timeout.Get(); has && timeout > 0 {
		c.timeout = min(c.timeout, time.Duration(timeout)*time.Second)
	}

	var h = l.accept(c)
	if h == nil {
		r.refuse(p)

		return
	}

	c.handler = h
	r.circuits[key] = c
	r.sendConnectAck(c)
	r.emit(circuitEvent(EventCircuitUp, c, nil))
	h.Connected(c)
}

func (r *Router) sendConnectAck(c *Circuit) {
	r.send(c.local, c.remote, transport(c.yourIndex, c.yourID, c.myIndex, c.myID, OpConnectAck, 0, []byte{byte(c.window & 0x7f)}))
}

// refuse turns down the connect request p.
func (r *Router) refuse(p Packet) {
	r.send(p.Destination, p.Origin, transport(p.Index, p.ID, 0, 0, OpConnectAck, FlagChoke, []byte{0}))
}

// Connect opens a circuit from user, on this node, to dest - a callsign or an
// alias - telling h what happens on it.
func (r *Router) Connect(dest string, user string, h CircuitHandler, now time.Time) (*Circuit, error) {
	r.now = now

	var call string

	if d, ok := r.table.Lookup(dest); ok {
		call = d.Call
	} else {
		var c, err = NormaliseCall(dest)
		if err != nil || !r.isLocal(c) {
			return nil, fmt.Errorf("%w: %s", ErrNoRoute, dest)
		}

		call = c
	}

	var u, err = NormaliseCall(user)
	if err != nil {
		return nil, err
	}

	var key, free = r.allocate()
	if !free {
		return nil, errors.New("netrom: no free circuits")
	}

	var c = r.newCircuit(key, r.cfg.Call, call)
	c.user = u
	c.userNode = r.cfg.Call
	c.handler = h
	c.window = r.cfg.Window
	c.state = CircuitConnecting
	r.circuits[key] = c
	c.retryDue = now.Add(c.timeout)
	c.sendConnectRequest()
	r.deliverLooped()

	return c, nil
}

func (r *Router) newCircuit(key circuitKey, local string, remote string) *Circuit {
	var c = new(Circuit)
	c.r = r
	c.handler = nopHandler{}
	c.myIndex = key.index
	c.myID = key.id
	c.local = local
	c.remote = remote
	c.timeout = r.cfg.Timeout
	c.lastActive = r.now
	c.started = r.now

	return c
}

// allocate picks an unused circuit index and ID.  Index 0 is never used, so
// that 0/0 can never name a live circuit.
func (r *Router) allocate() (circuitKey, bool) {
	for range 255 {
		var key = circuitKey{index: r.nextIndex, id: r.nextID}

		r.nextIndex++
		if r.nextIndex == 0 {
			r.nextIndex = 1
			r.nextID++
		}

		if _, used := r.circuits[key]; !used {
			return key, true
		}
	}

	return circuitKey{index: 0, id: 0}, false
}

// forget drops a circuit closed because of err.
func (r *Router) forget(c *Circuit, err error) {
	var key = circuitKey{c.myIndex, c.myID}
	if r.circuits[key] == c {
		delete(r.circuits, key)
		r.emit(circuitEvent(EventCircuitDown, c, err))
	}
}

// NeighbourFailed says a neighbour could not be reached: routes through it
// are dropped until it is heard again, and circuits whose only way to their
// far end went through it are closed.
func (r *Router) NeighbourFailed(key NeighbourKey, now time.Time) {
	r.now = now
	r.table.Failed(key)
	r.emit(routesChanged())

	for _, c := range r.sortedCircuits() {
		if r.isLocal(c.remote) {
			continue
		}

		if _, ok := r.table.Lookup(c.remote); !ok {
			c.finish(ErrNoRoute)
		}
	}
}

func (r *Router) sortedCircuits() []*Circuit {
	var cs = make([]*Circuit, 0, len(r.circuits))
	for _, c := range r.circuits {
		cs = append(cs, c)
	}

	slices.SortFunc(cs, func(a, b *Circuit) int { return a.started.Compare(b.started) })

	return cs
}

// Tick runs the timers: broadcasts, route ageing and circuit retries.
func (r *Router) Tick(now time.Time) {
	r.now = now

	if !now.Before(r.nextBroadcast) {
		r.nextBroadcast = now.Add(r.cfg.BroadcastInterval)

		if r.table.Age() {
			r.emit(routesChanged())
		}

		r.broadcast()
	}

	for _, c := range r.sortedCircuits() {
		c.tick()
	}

	r.deliverLooped()
}

// BroadcastNow sends a NODES broadcast now, without waiting for the interval.
func (r *Router) BroadcastNow(now time.Time) {
	r.now = now
	r.broadcast()
}

func (r *Router) broadcast() {
	var entries = r.table.Advertise(r.cfg.MinObsolescenceBroadcast)

	var calls = make([]string, 0, len(r.listeners))
	for call := range r.listeners {
		calls = append(calls, call)
	}

	slices.Sort(calls)

	for _, call := range calls {
		var l = r.listeners[call]
		if call == r.cfg.Call || l.quality == 0 {
			continue
		}

		entries = append(entries, NodesEntry{Call: call, Alias: l.alias, BestNeighbour: r.cfg.Call, Quality: l.quality})
	}

	var frames, err = EncodeNodes(r.cfg.Alias, entries)
	if err != nil {
		return
	}

	for _, p := range r.cfg.Ports {
		if !p.Broadcast {
			continue
		}

		for _, f := range frames {
			r.link.BroadcastNodes(p.Port, f)
		}
	}
}

func (r *Router) emit(e Event) {
	if r.OnEvent != nil {
		r.OnEvent(e)
	}
}
