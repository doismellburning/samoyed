// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agwclient is a client for the AGWPE TCP API, through which an
// application uses a TNC's connected-mode AX.25 without implementing the link
// layer itself.  samoyed-direwolf and Dire Wolf both serve it.
//
// One Client holds one connection to the server, and carries any number of
// links over it.  Each Link is an io.ReadWriteCloser; writing to one waits
// while the server has more of its frames unacknowledged than the client is
// configured to allow, so a long transfer doesn't flood the transmit queue.
//
// Run keeps the connection up: it registers the client's callsign on each of
// its ports, and if the connection drops, every link on it fails and Run
// reconnects.
package agwclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/agwpe"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
)

var (
	// ErrNotConnected says there is no connection to the server just now.
	ErrNotConnected = errors.New("not connected to the AGW server")

	// ErrConnectFailed says the far station didn't take a connection.
	ErrConnectFailed = errors.New("AX.25 connection failed")

	// ErrLinkExists says there is already a link, or an attempt at one,
	// between the same two callsigns on the same port.
	ErrLinkExists = errors.New("AX.25 link already exists")

	// ErrLinkClosed says the link has been closed, by either end.
	ErrLinkClosed = errors.New("AX.25 link closed")
)

// Credentials are a user name and password for an AGW server that wants a
// login, such as samoyed-direwolf with AGWLOGIN.
type Credentials struct {
	User     string
	Password string
}

// Config is what a Client needs.  Addr, Call and Ports are required; zero
// values elsewhere take the defaults below.
type Config struct {
	// Addr is the server's host:port.
	Addr string

	// Login, if there is one, is sent before anything else.
	Login maybe.Maybe[Credentials]

	// Call is the callsign registered for incoming connections.
	Call string

	// Ports are the server ports to register Call on, counting from 0.
	// With none, Call is registered on every port the server has.
	Ports []int

	// PacLen is the most data sent in one frame to the server.
	PacLen int

	// HighWater is how many frames a link may have unacknowledged before a
	// write waits.
	HighWater uint32

	// YTimeout is how long to wait for the server to say how many frames are
	// outstanding.  A server that doesn't answer in time is taken not to
	// answer at all, and writes are paced by FallbackDelay instead.
	YTimeout time.Duration

	// YPoll is how often to ask again while a write waits.
	YPoll time.Duration

	// FallbackDelay is the pause after each frame written to a server that
	// doesn't say how many frames are outstanding.
	FallbackDelay time.Duration

	// ConnectTimeout is how long an outgoing connection may take.
	ConnectTimeout time.Duration

	// CloseTimeout is how long Close waits for the server to confirm a
	// disconnection.
	CloseTimeout time.Duration

	// StartupTimeout is how long the server may take to answer the
	// requests made on connecting to it.
	StartupTimeout time.Duration

	// Backoff is the first wait before reconnecting; it doubles on each
	// failure, up to MaxBackoff.
	Backoff    time.Duration
	MaxBackoff time.Duration

	// MaxData is the largest frame data accepted from the server.
	MaxData uint32
}

// withDefaults returns cfg with its zero values filled in.
func (cfg Config) withDefaults() Config {
	var orDefault = func(d *time.Duration, def time.Duration) {
		if *d <= 0 {
			*d = def
		}
	}

	if cfg.PacLen <= 0 {
		cfg.PacLen = 128
	}

	if cfg.HighWater == 0 {
		cfg.HighWater = 8
	}

	orDefault(&cfg.YTimeout, 5*time.Second)
	orDefault(&cfg.YPoll, 500*time.Millisecond)
	orDefault(&cfg.FallbackDelay, time.Second)
	orDefault(&cfg.ConnectTimeout, 2*time.Minute)
	orDefault(&cfg.CloseTimeout, 30*time.Second)
	orDefault(&cfg.StartupTimeout, 10*time.Second)
	orDefault(&cfg.Backoff, time.Second)
	orDefault(&cfg.MaxBackoff, time.Minute)

	if cfg.MaxData == 0 {
		cfg.MaxData = DefaultMaxData
	}

	cfg.Call = normaliseCall(cfg.Call)

	return cfg
}

// acceptBacklog is how many incoming links may wait for Accept before more
// are turned away.
const acceptBacklog = 16

// linkKey identifies a link: the port it is on and the callsigns at each end.
type linkKey struct {
	port   int
	own    string
	remote string
}

func newLinkKey(port int, own string, remote string) linkKey {
	var key = new(linkKey)

	key.port = port
	key.own = normaliseCall(own)
	key.remote = normaliseCall(remote)

	return *key
}

// pendingConnect is an outgoing connection waiting for the server to say how
// it went.  One given up on stays until the server does say, so that a late
// answer isn't mistaken for an incoming connection.
type pendingConnect struct {
	result    chan connectResult
	abandoned bool
}

type connectResult struct {
	link *Link
	err  error
}

// Client is a connection to an AGWPE server, carrying any number of links.
type Client struct {
	cfg Config

	accepted chan *Link

	// mu guards everything below.
	mu      sync.Mutex
	conn    *conn
	links   map[linkKey]*Link
	pending map[linkKey]*pendingConnect
	ports   []PortInfo
	ready   chan struct{} // Closed while connected and registered
}

// New makes a Client.  Nothing happens until Run is called.
func New(cfg Config) *Client {
	var c = new(Client)

	c.cfg = cfg.withDefaults()
	c.accepted = make(chan *Link, acceptBacklog)
	c.links = make(map[linkKey]*Link)
	c.pending = make(map[linkKey]*pendingConnect)
	c.ready = make(chan struct{})

	return c
}

// Run connects to the server and keeps connected until ctx is cancelled,
// reconnecting with a growing backoff whenever the connection fails or drops.
// It returns nil once ctx is cancelled.
func (c *Client) Run(ctx context.Context) error {
	var backoff = c.cfg.Backoff

	for ctx.Err() == nil {
		if c.connectOnce(ctx, backoff) {
			backoff = c.cfg.Backoff
		}

		if !dwutil.SleepCtx(ctx, backoff) {
			break
		}

		backoff = min(2*backoff, c.cfg.MaxBackoff)
	}

	return nil
}

// connectOnce makes one connection to the server and serves it until it
// ends, logging why it ended.  It reports whether the connection got as far as
// being registered, and so whether the backoff should start again.
func (c *Client) connectOnce(ctx context.Context, backoff time.Duration) bool {
	var wasUp, err = c.session(ctx)
	if wasUp {
		backoff = c.cfg.Backoff
	}

	if ctx.Err() == nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"addr":  c.cfg.Addr,
			"retry": backoff,
		}).Warn("Lost AGW server connection")
	}

	return wasUp
}

// Ready returns a channel that is closed once the client is connected and
// registered.  When the connection drops, the next call returns a fresh one.
func (c *Client) Ready() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.ready
}

// Ports returns the server's ports, as it described them when the client
// last connected.
func (c *Client) Ports() []PortInfo {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Clone(c.ports)
}

// Accept waits for an incoming link.
func (c *Client) Accept(ctx context.Context) (*Link, error) {
	select {
	case link := <-c.accepted:
		return link, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Connect opens a link from one callsign to another on port, through the
// given digipeaters if there are any.  It returns once the far station has
// taken the connection, or failed to.
func (c *Client) Connect(ctx context.Context, port int, from string, to string, via []string) (*Link, error) {
	var key = newLinkKey(port, from, to)

	var msg, frameErr = connectFrame(port, key.own, key.remote, via)
	if frameErr != nil {
		return nil, frameErr
	}

	var cn, pending, startErr = c.startConnect(key)
	if startErr != nil {
		return nil, startErr
	}

	var sendErr = cn.send(msg)
	if sendErr != nil {
		c.abandonConnect(key, cn)

		return nil, sendErr
	}

	var timer = time.NewTimer(c.cfg.ConnectTimeout)
	defer timer.Stop()

	select {
	case result := <-pending.result:
		return result.link, result.err
	case <-ctx.Done():
		c.abandonConnect(key, cn)

		return nil, ctx.Err()
	case <-timer.C:
		c.abandonConnect(key, cn)

		return nil, fmt.Errorf("%w: no answer from %s within %v", ErrConnectFailed, key.remote, c.cfg.ConnectTimeout)
	case <-cn.done:
		return nil, ErrNotConnected
	}
}

// startConnect records an outgoing connection as pending, if there is a
// connection to make it on and nothing else between the same two stations.
func (c *Client) startConnect(key linkKey) (*conn, *pendingConnect, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, nil, ErrNotConnected
	}

	// An attempt given up on may never hear back from the server, so it
	// doesn't stand in the way of a new one, which takes its place.
	if c.links[key] != nil || (c.pending[key] != nil && !c.pending[key].abandoned) {
		return nil, nil, fmt.Errorf("%w: %s to %s on port %d", ErrLinkExists, key.own, key.remote, key.port)
	}

	var pending = new(pendingConnect)
	pending.result = make(chan connectResult, 1)
	c.pending[key] = pending

	return c.conn, pending, nil
}

// abandonConnect gives up on an outgoing connection, asking the server to
// stop trying.  Its pending entry stays, marked abandoned, until the server
// answers one way or the other.
func (c *Client) abandonConnect(key linkKey, cn *conn) {
	c.mu.Lock()

	var pending = c.pending[key]
	if pending != nil {
		pending.abandoned = true
	}

	c.mu.Unlock()

	cn.sendDisconnect(key)
}

// session makes one connection to the server and serves it until it ends.
// It reports whether the connection got as far as being registered.
func (c *Client) session(ctx context.Context) (bool, error) {
	var dialer = new(net.Dialer)

	var netConn, dialErr = dialer.DialContext(ctx, "tcp", c.cfg.Addr)
	if dialErr != nil {
		return false, fmt.Errorf("connecting to AGW server: %w", dialErr)
	}

	var stopClose = dwutil.CloseOnDone(ctx, netConn)
	defer stopClose()

	var cn = newConn(netConn)
	defer cn.close()

	var ports, registered, backlog, startErr = c.startup(cn)
	if startErr != nil {
		return false, startErr
	}

	var writerDone = make(chan struct{})

	go func() {
		defer close(writerDone)
		cn.writeLoop()
	}()

	c.up(cn, ports)
	defer c.down(cn)

	logrus.WithFields(logrus.Fields{
		"addr":  c.cfg.Addr,
		"call":  c.cfg.Call,
		"ports": registered,
	}).Info("Connected to AGW server")

	for _, msg := range backlog {
		c.dispatch(cn, msg)
	}

	var readErr = c.readLoop(cn)

	cn.close()
	<-writerDone

	return true, readErr
}

// startup makes the requests a new connection begins with and waits for their
// answers: login, version and ports first, then registering the callsign on
// each port, which can only be done once the ports are known when the client
// is to register on all of them.  Anything else that arrives meanwhile is
// returned to be dealt with once the connection is up.
func (c *Client) startup(cn *conn) ([]PortInfo, []int, []*agwpe.Message, error) {
	var deadlineErr = cn.netConn.SetDeadline(time.Now().Add(c.cfg.StartupTimeout))
	if deadlineErr != nil {
		return nil, nil, nil, fmt.Errorf("setting AGW startup deadline: %w", deadlineErr)
	}

	var introErr = c.sendIntro(cn)
	if introErr != nil {
		return nil, nil, nil, introErr
	}

	var ports, backlog, portsErr = c.awaitIntro(cn)
	if portsErr != nil {
		return nil, nil, nil, portsErr
	}

	var register, registerErr = c.portsToRegister(ports)
	if registerErr != nil {
		return nil, nil, nil, registerErr
	}

	var more, regErr = c.register(cn, register)
	if regErr != nil {
		return nil, nil, nil, regErr
	}

	var clearErr = cn.netConn.SetDeadline(time.Time{})
	if clearErr != nil {
		return nil, nil, nil, fmt.Errorf("clearing AGW startup deadline: %w", clearErr)
	}

	return ports, register, append(backlog, more...), nil
}

// sendIntro sends the login, if there is one, and asks for the server's
// version and ports.
func (c *Client) sendIntro(cn *conn) error {
	if login, ok := c.cfg.Login.Get(); ok {
		var msg, err = loginFrame(login.User, login.Password)
		if err == nil {
			err = writeFrame(cn.netConn, msg)
		}

		if err != nil {
			return err
		}
	}

	for _, kind := range []byte{kindVersion, kindPorts} {
		var msg, err = newFrame(0, kind, "", "", nil)
		if err == nil {
			err = writeFrame(cn.netConn, msg)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// awaitIntro waits for the answers to sendIntro's version and ports requests.
func (c *Client) awaitIntro(cn *conn) ([]PortInfo, []*agwpe.Message, error) {
	var gotVersion = false
	var gotPorts = false
	var ports []PortInfo
	var backlog []*agwpe.Message

	for !gotVersion || !gotPorts {
		var msg, err = readFrame(cn.netConn, c.cfg.MaxData)
		if err != nil {
			if !gotVersion {
				// The server ignores everything from a client that
				// hasn't logged in, without saying so.
				return nil, nil, fmt.Errorf("no reply from AGW server to the version request (is a login needed, or the wrong one given?): %w", err)
			}

			return nil, nil, fmt.Errorf("waiting for AGW server's port information: %w", err)
		}

		switch msg.Header.DataKind {
		case kindVersion:
			gotVersion = true

			logrus.WithField("version", fmt.Sprintf("% x", msg.Data)).Debug("AGW server version")

		case kindPorts:
			ports, err = parsePorts(msg.Data)
			if err != nil {
				return nil, nil, err
			}

			gotPorts = true

		default:
			backlog = append(backlog, msg)
		}
	}

	return ports, backlog, nil
}

// portsToRegister is the configured ports, each of which the server must
// have, or with none configured, every port the server has.
func (c *Client) portsToRegister(ports []PortInfo) ([]int, error) {
	if len(c.cfg.Ports) == 0 {
		var all = make([]int, 0, len(ports))

		for _, p := range ports {
			all = append(all, p.Number)
		}

		return all, nil
	}

	for _, want := range c.cfg.Ports {
		if !slices.ContainsFunc(ports, func(p PortInfo) bool { return p.Number == want }) {
			return nil, fmt.Errorf("AGW server has no port %d (it has %v)", want, ports)
		}
	}

	return c.cfg.Ports, nil
}

// register registers the callsign on each of ports, and waits for the server
// to say each has taken.
func (c *Client) register(cn *conn, ports []int) ([]*agwpe.Message, error) {
	var unregistered = make(map[int]bool)

	for _, port := range ports {
		var msg, err = newFrame(port, kindRegister, c.cfg.Call, "", nil)
		if err == nil {
			err = writeFrame(cn.netConn, msg)
		}

		if err != nil {
			return nil, err
		}

		unregistered[port] = true
	}

	var backlog []*agwpe.Message

	for len(unregistered) > 0 {
		var msg, err = readFrame(cn.netConn, c.cfg.MaxData)
		if err != nil {
			return nil, fmt.Errorf("waiting for AGW server to register %s: %w", c.cfg.Call, err)
		}

		if msg.Header.DataKind != kindRegister {
			backlog = append(backlog, msg)

			continue
		}

		var port = int(msg.Header.Portx)
		if !unregistered[port] {
			continue
		}

		if len(msg.Data) < 1 || msg.Data[0] != 1 {
			return nil, fmt.Errorf("AGW server refused to register %s on port %d", c.cfg.Call, port)
		}

		delete(unregistered, port)
	}

	return backlog, nil
}

// up records cn as the connection in use.
func (c *Client) up(cn *conn, ports []PortInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.conn = cn
	c.ports = ports
	close(c.ready)
}

// down forgets cn, failing every link and outgoing connection on it.
func (c *Client) down(cn *conn) {
	c.mu.Lock()

	if c.conn != cn {
		c.mu.Unlock()

		return
	}

	c.conn = nil
	c.ready = make(chan struct{})

	var links = c.links
	var pending = c.pending

	c.links = make(map[linkKey]*Link)
	c.pending = make(map[linkKey]*pendingConnect)

	c.mu.Unlock()

	for _, link := range links {
		link.end(errConnectionLost)
	}

	for _, p := range pending {
		p.result <- connectResult{link: nil, err: ErrNotConnected}
	}
}

// readLoop reads frames from the server and acts on them until the
// connection ends.
func (c *Client) readLoop(cn *conn) error {
	for {
		var msg, err = readFrame(cn.netConn, c.cfg.MaxData)
		if err != nil {
			return err
		}

		c.dispatch(cn, msg)
	}
}

// dispatch acts on one frame from the server.
func (c *Client) dispatch(cn *conn, msg *agwpe.Message) {
	var port = int(msg.Header.Portx)
	var from = msg.Header.CallFrom.String()
	var to = msg.Header.CallTo.String()

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		logrus.WithFields(logrus.Fields{
			"kind":   string(rune(msg.Header.DataKind)),
			"port":   port,
			"from":   from,
			"to":     to,
			"length": len(msg.Data),
		}).Trace("AGW frame from server")
	}

	switch msg.Header.DataKind {
	case kindConnect:
		// The server names the far end first on everything about a link
		// except the outstanding frames reply.
		c.linkUp(cn, newLinkKey(port, to, from))

	case kindData:
		if link := c.link(newLinkKey(port, to, from)); link != nil {
			link.deliver(msg.Data)
		}

	case kindDisconnect:
		c.linkDown(newLinkKey(port, to, from))

	case kindOutstanding:
		var count, err = outstandingCount(msg)
		if err != nil {
			logrus.WithError(err).Warn("Bad reply from AGW server")

			return
		}

		if link := c.link(newLinkKey(port, from, to)); link != nil {
			link.outstanding(count)
		}

	default:
		logrus.WithField("kind", string(rune(msg.Header.DataKind))).Debug("Ignoring AGW frame")
	}
}

// link returns the link with key, or nil if there is none.
func (c *Client) link(key linkKey) *Link {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.links[key]
}

// linkUp acts on the server saying a link is up: either an outgoing
// connection succeeding, or an incoming one to hand to Accept.
func (c *Client) linkUp(cn *conn, key linkKey) {
	c.mu.Lock()

	if c.links[key] != nil {
		c.mu.Unlock()

		return // Already up; nothing new.
	}

	var pending = c.pending[key]
	if pending != nil && pending.abandoned {
		delete(c.pending, key)
		c.mu.Unlock()

		cn.sendDisconnect(key) // Too late: it was given up on.

		return
	}

	var link = newLink(c, cn, key)
	c.links[key] = link

	if pending != nil {
		delete(c.pending, key)
		c.mu.Unlock()

		pending.result <- connectResult{link: link, err: nil}

		return
	}

	c.mu.Unlock()

	select {
	case c.accepted <- link:
		logrus.WithFields(logrus.Fields{"port": key.port, "remote": key.remote, "own": key.own}).Debug("Incoming AX.25 link")
	default:
		logrus.WithFields(logrus.Fields{"port": key.port, "remote": key.remote}).Warn("Too many incoming AX.25 links waiting to be accepted; disconnecting")
		c.forget(link)
		cn.sendDisconnect(key)
	}
}

// linkDown acts on the server saying a link is down, or that an outgoing
// connection failed.
func (c *Client) linkDown(key linkKey) {
	c.mu.Lock()

	var pending = c.pending[key]
	if pending != nil {
		delete(c.pending, key)
		c.mu.Unlock()

		if !pending.abandoned {
			pending.result <- connectResult{link: nil, err: fmt.Errorf("%w: %s did not answer", ErrConnectFailed, key.remote)}
		}

		return
	}

	var link = c.links[key]
	delete(c.links, key)

	c.mu.Unlock()

	if link != nil {
		link.end(errRemoteClosed)
	}
}

// forget drops link from the client's table, if it is still the one there.
func (c *Client) forget(link *Link) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.links[link.key] == link {
		delete(c.links, link.key)
	}
}

// conn is one TCP connection to the server.  Once it is up, frames go out
// through writeLoop alone, so the socket is only ever written from one place.
type conn struct {
	netConn net.Conn
	writes  chan *agwpe.Message
	done    chan struct{}
	once    sync.Once
}

func newConn(netConn net.Conn) *conn {
	var cn = new(conn)

	cn.netConn = netConn
	cn.writes = make(chan *agwpe.Message)
	cn.done = make(chan struct{})

	return cn
}

// send queues msg to go to the server.
func (cn *conn) send(msg *agwpe.Message) error {
	select {
	case cn.writes <- msg:
		return nil
	case <-cn.done:
		return ErrNotConnected
	}
}

// sendDisconnect asks the server to take down the link with key, logging
// rather than failing if it can't.
func (cn *conn) sendDisconnect(key linkKey) {
	var msg, err = newFrame(key.port, kindDisconnect, key.own, key.remote, nil)
	if err == nil {
		err = cn.send(msg)
	}

	if err != nil {
		logrus.WithError(err).WithField("remote", key.remote).Debug("Could not send AGW disconnect")
	}
}

// writeLoop sends queued frames until the connection ends.
func (cn *conn) writeLoop() {
	for {
		select {
		case msg := <-cn.writes:
			var err = writeFrame(cn.netConn, msg)
			if err != nil {
				logrus.WithError(err).Warn("Writing to AGW server failed")
				cn.close()

				return
			}
		case <-cn.done:
			return
		}
	}
}

// close ends the connection.  It is safe to call more than once.
func (cn *conn) close() {
	cn.once.Do(func() {
		close(cn.done)
		_ = cn.netConn.Close()
	})
}
