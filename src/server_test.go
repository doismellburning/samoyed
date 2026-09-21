// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loginFrame builds an AGW 'P' (Application Login) message the way a client
// does: two fixed size NUL padded fields.
func loginFrame(user string, password string) *AGWPEMessage {
	var data = make([]byte, 2*AGW_LOGIN_FIELD_LEN)
	copy(data[:AGW_LOGIN_FIELD_LEN], user)
	copy(data[AGW_LOGIN_FIELD_LEN:], password)

	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'P'
	cmd.Header.DataLen = uint32(len(data))
	cmd.Data = data

	return cmd
}

// requireLogins returns a server that wants the given credentials, with client
// 0 logged out.  No pairs means no login is required.
func requireLogins(t *testing.T, pairs ...string) *AGWServer {
	t.Helper()
	require.Zero(t, len(pairs)%2, "want a password for every user name")

	var s = new(AGWServer)

	for i := 0; i < len(pairs); i += 2 {
		var login = new(agwpe_login_s)
		login.user = pairs[i]
		login.password = pairs[i+1]
		s.logins = append(s.logins, *login)
	}

	return s
}

func TestLoginRequired(t *testing.T) {
	assert.False(t, requireLogins(t).loginRequired(), "no credentials configured means no login")

	assert.True(t, requireLogins(t, "Q1TEST", "hunter2").loginRequired())
}

func TestParseAGWLogin(t *testing.T) {
	var user, password, ok = parseAGWLogin(loginFrame("Q1TEST", "hunter2").Data)
	require.True(t, ok)
	assert.Equal(t, "Q1TEST", user)
	assert.Equal(t, "hunter2", password)

	// A short frame is not a login we can check.
	_, _, ok = parseAGWLogin(make([]byte, 2*AGW_LOGIN_FIELD_LEN-1))
	assert.False(t, ok)

	_, _, ok = parseAGWLogin(nil)
	assert.False(t, ok)
}

// The protocol describes each field as "ended with 0x00 filled till 255 bytes",
// so a client is within its rights to leave anything it likes after the
// terminator.  Stopping at the first NUL, rather than trimming trailing ones,
// is what makes such a client able to log in.
func TestParseAGWLogin_FieldsEndAtFirstNUL(t *testing.T) {
	var data = make([]byte, 2*AGW_LOGIN_FIELD_LEN)
	for i := range data {
		data[i] = 0xAA /* Pad with something that is not NUL. */
	}
	copy(data, "Q1TEST\x00")
	copy(data[AGW_LOGIN_FIELD_LEN:], "hunter2\x00")

	var user, password, ok = parseAGWLogin(data)
	require.True(t, ok)
	assert.Equal(t, "Q1TEST", user)
	assert.Equal(t, "hunter2", password)
}

// A field with no terminator at all is taken whole rather than dropped.
func TestParseAGWLogin_FieldWithoutTerminator(t *testing.T) {
	var data = make([]byte, 2*AGW_LOGIN_FIELD_LEN)
	var long = strings.Repeat("x", AGW_LOGIN_FIELD_LEN)
	copy(data, long)
	copy(data[AGW_LOGIN_FIELD_LEN:], "hunter2")

	var user, password, ok = parseAGWLogin(data)
	require.True(t, ok)
	assert.Equal(t, long, user)
	assert.Equal(t, "hunter2", password)
}

// The worked example from the AGWPE API documentation: a 510 byte information
// part, user name "LU7DID" at offset 0 and password "LIZARD" at offset 255.
func TestParseAGWLogin_SpecExample(t *testing.T) {
	var data = make([]byte, 510)
	copy(data, "LU7DID")
	copy(data[255:], "LIZARD")

	var user, password, ok = parseAGWLogin(data)
	require.True(t, ok)
	assert.Equal(t, "LU7DID", user)
	assert.Equal(t, "LIZARD", password)

	// And that is the frame our own test helper builds.
	assert.Equal(t, data, loginFrame("LU7DID", "LIZARD").Data)
}

// Data longer than the two fields is still a login; the fields are where the
// protocol puts them regardless of what follows.
func TestParseAGWLogin_OversizedData(t *testing.T) {
	var data = append(loginFrame("Q1TEST", "hunter2").Data, []byte("and then some")...)

	var user, password, ok = parseAGWLogin(data)
	require.True(t, ok)
	assert.Equal(t, "Q1TEST", user)
	assert.Equal(t, "hunter2", password)
}

func TestHandleClientCommand_P_CorrectCredentialsLogIn(t *testing.T) {
	var s = requireLogins(t, "Q1TEST", "hunter2")

	s.handleClientCommand(0, loginFrame("Q1TEST", "hunter2"))

	assert.True(t, s.isLoggedIn(0))
}

func TestHandleClientCommand_P_WrongCredentialsDoNotLogIn(t *testing.T) {
	var s = requireLogins(t, "Q1TEST", "hunter2")

	s.handleClientCommand(0, loginFrame("Q1TEST", "hunter3"))
	assert.False(t, s.isLoggedIn(0), "wrong password")

	s.handleClientCommand(0, loginFrame("Q2TEST", "hunter2"))
	assert.False(t, s.isLoggedIn(0), "wrong user name")

	s.handleClientCommand(0, loginFrame("", ""))
	assert.False(t, s.isLoggedIn(0), "empty credentials")
}

// AGWPE accepts any one of the user name and password combinations it has been
// given, so each set has to work in its own right.
func TestHandleClientCommand_P_AnyConfiguredCredentialsLogIn(t *testing.T) {
	var s = requireLogins(t, "Q1TEST", "hunter2", "Q2TEST", "correct horse")

	s.handleClientCommand(0, loginFrame("Q1TEST", "hunter2"))
	assert.True(t, s.isLoggedIn(0), "first set")

	s.clientLoggedOut(0)

	s.handleClientCommand(0, loginFrame("Q2TEST", "correct horse"))
	assert.True(t, s.isLoggedIn(0), "second set")
}

// Credentials are matched as a pair, so one user's password does not open
// another user's account.
func TestHandleClientCommand_P_CredentialsDoNotCrossBetweenUsers(t *testing.T) {
	var s = requireLogins(t, "Q1TEST", "hunter2", "Q2TEST", "correct horse")

	s.handleClientCommand(0, loginFrame("Q1TEST", "correct horse"))
	assert.False(t, s.isLoggedIn(0))

	s.handleClientCommand(0, loginFrame("Q2TEST", "hunter2"))
	assert.False(t, s.isLoggedIn(0))
}

func TestMatchLogin(t *testing.T) {
	var s = requireLogins(t, "Q1TEST", "hunter2", "Q2TEST", "correct horse")

	var matched, accepted = s.matchLogin("Q2TEST", "correct horse")
	assert.True(t, accepted)
	assert.Equal(t, "Q2TEST", matched, "the name reported is the one we configured")

	matched, accepted = s.matchLogin("Q3TEST", "hunter2")
	assert.False(t, accepted)
	assert.Empty(t, matched)
}

// An accepted login is not permanent: getting it wrong afterwards takes it away
// again, so a client cannot log in and then hand the socket to someone else.
func TestHandleClientCommand_P_FailureAfterSuccessLogsOut(t *testing.T) {
	var s = requireLogins(t, "Q1TEST", "hunter2")

	s.handleClientCommand(0, loginFrame("Q1TEST", "hunter2"))
	require.True(t, s.isLoggedIn(0))

	s.handleClientCommand(0, loginFrame("Q1TEST", "wrong"))
	assert.False(t, s.isLoggedIn(0))
}

// A login we cannot read is a failed login like any other, so it takes an
// earlier success away rather than leaving the socket authorized.
func TestHandleClientCommand_P_MalformedFrameAfterSuccessLogsOut(t *testing.T) {
	var s = requireLogins(t, "Q1TEST", "hunter2")

	s.handleClientCommand(0, loginFrame("Q1TEST", "hunter2"))
	require.True(t, s.isLoggedIn(0))

	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'P'
	cmd.Data = []byte("Q1TEST\x00hunter2")
	cmd.Header.DataLen = uint32(len(cmd.Data))

	s.handleClientCommand(0, cmd)

	assert.False(t, s.isLoggedIn(0))
}

func TestHandleClientCommand_P_MalformedFrameDoesNotLogIn(t *testing.T) {
	var s = requireLogins(t, "Q1TEST", "hunter2")

	// Shorter than the two fixed size fields, so there is nothing to compare.
	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'P'
	cmd.Data = []byte("Q1TEST\x00hunter2")
	cmd.Header.DataLen = uint32(len(cmd.Data))

	s.handleClientCommand(0, cmd)

	assert.False(t, s.isLoggedIn(0))
}

// Without AGWLOGIN, a login frame is silently ignored, as Dire Wolf does.
func TestHandleClientCommand_P_IgnoredWhenNoLoginConfigured(t *testing.T) {
	var s = requireLogins(t)

	s.handleClientCommand(0, loginFrame("Q1TEST", "hunter2"))

	assert.False(t, s.isLoggedIn(0))
}

func TestHandleClientCommand_CommandsIgnoredUntilLoggedIn(t *testing.T) {
	var s = requireLogins(t, "Q1TEST", "hunter2")

	var client = setupClientPipe(t, s)
	var replyCh = asyncReply(client)

	// 'R' always gets a version reply - unless we have not logged in.
	var version = new(AGWPEMessage)
	version.Header.DataKind = 'R'
	s.handleClientCommand(0, version)

	select {
	case reply := <-replyCh:
		t.Fatalf("expected no reply before logging in, got '%c'", reply.Header.DataKind)
	default:
	}

	s.handleClientCommand(0, loginFrame("Q1TEST", "hunter2"))
	require.True(t, s.isLoggedIn(0))

	s.handleClientCommand(0, version)

	var reply = <-replyCh
	require.NotNil(t, reply)
	assert.Equal(t, byte('R'), reply.Header.DataKind)
}

// Without AGWLOGIN nothing changes: commands work without any login at all.
func TestHandleClientCommand_NoLoginConfiguredCommandsWork(t *testing.T) {
	var s = requireLogins(t)

	var client = setupClientPipe(t, s)
	var replyCh = asyncReply(client)

	var version = new(AGWPEMessage)
	version.Header.DataKind = 'R'
	s.handleClientCommand(0, version)

	var reply = <-replyCh
	require.NotNil(t, reply)
	assert.Equal(t, byte('R'), reply.Header.DataKind)
}

// --- Clients on this machine ---

// stubAddrConn is a net.Conn that reports whatever remote address we give it.
// Only RemoteAddr is ever called.
type stubAddrConn struct {
	net.Conn

	addr net.Addr
}

func (c *stubAddrConn) RemoteAddr() net.Addr { return c.addr }

func connWithRemoteAddr(addr net.Addr) net.Conn {
	var conn = new(stubAddrConn)
	conn.addr = addr

	return conn
}

func tcpAddr(t *testing.T, ip string) net.Addr {
	t.Helper()

	var addr = new(net.TCPAddr)
	addr.IP = net.ParseIP(ip)
	require.NotNil(t, addr.IP, "unparseable test address %q", ip)
	addr.Port = 8000

	return addr
}

// loopbackConns returns a connected pair of real TCP sockets over the loopback
// interface, as the accept path would see.
func loopbackConns(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()

	var ctx = t.Context()

	var listener, listenErr = new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, listenErr)
	defer listener.Close()

	var accepted = make(chan net.Conn, 1)
	go func() {
		var conn, _ = listener.Accept()
		accepted <- conn
	}()

	var client, dialErr = new(net.Dialer).DialContext(ctx, "tcp", listener.Addr().String())
	require.NoError(t, dialErr)

	var server = <-accepted
	require.NotNil(t, server)

	t.Cleanup(func() {
		client.Close()
		server.Close()
	})

	/*
	 * Nothing here should ever wait on the other end for long, and a test that
	 * fails by hanging tells nobody anything.
	 */
	require.NoError(t, client.SetDeadline(time.Now().Add(5*time.Second)))
	require.NoError(t, server.SetDeadline(time.Now().Add(5*time.Second)))

	return server, client
}

func TestAgwClientIsLocal(t *testing.T) {
	var server, _ = loopbackConns(t)
	assert.True(t, agwClientIsLocal(server), "a real connection over the loopback interface")

	assert.True(t, agwClientIsLocal(connWithRemoteAddr(tcpAddr(t, "127.0.0.1"))))
	assert.True(t, agwClientIsLocal(connWithRemoteAddr(tcpAddr(t, "127.1.2.3"))), "all of 127/8")
	assert.True(t, agwClientIsLocal(connWithRemoteAddr(tcpAddr(t, "::1"))))
	assert.True(t, agwClientIsLocal(connWithRemoteAddr(tcpAddr(t, "::ffff:127.0.0.1"))),
		"IPv4 loopback mapped into IPv6")

	assert.False(t, agwClientIsLocal(connWithRemoteAddr(tcpAddr(t, "192.168.1.10"))))
	assert.False(t, agwClientIsLocal(connWithRemoteAddr(tcpAddr(t, "8.8.8.8"))))
	assert.False(t, agwClientIsLocal(connWithRemoteAddr(tcpAddr(t, "2001:db8::1"))))

	// Anything we can't judge has to log in like everyone else.
	assert.False(t, agwClientIsLocal(nil))

	var pipeEnd, otherEnd = net.Pipe()
	t.Cleanup(func() {
		pipeEnd.Close()
		otherEnd.Close()
	})
	assert.False(t, agwClientIsLocal(pipeEnd), "not a TCP address")
}

// A client on this machine is exempt, as it is with AGWPE, and so may issue
// commands without ever sending a login frame.
func TestClientAccepted_LocalClientNeedsNoLogin(t *testing.T) {
	var s = requireLogins(t, "Q1TEST", "hunter2")

	var server, client = loopbackConns(t)

	s.clientAccepted(0, server)
	assert.True(t, s.isLoggedIn(0))

	var version = new(AGWPEMessage)
	version.Header.DataKind = 'R'
	s.handleClientCommand(0, version)

	var reply, readErr = readReplyFrom(client)
	require.NoError(t, readErr)
	assert.Equal(t, byte('R'), reply.Header.DataKind)
}

func TestClientAccepted_RemoteClientMustLogIn(t *testing.T) {
	var s = requireLogins(t, "Q1TEST", "hunter2")

	s.clientAccepted(0, connWithRemoteAddr(tcpAddr(t, "192.168.1.10")))

	assert.False(t, s.isLoggedIn(0))
}

// Without AGWLOGIN the exemption changes nothing, because nobody has to log in.
func TestClientAccepted_NoLoginConfigured(t *testing.T) {
	var s = requireLogins(t)

	s.clientAccepted(0, connWithRemoteAddr(tcpAddr(t, "192.168.1.10")))

	assert.False(t, s.loginRequired())
}

// A client on this machine is exempt however its login frames turn out, so
// getting the credentials wrong does not cost it the access it never had to
// ask for.
func TestClientAccepted_LocalClientSurvivesAFailedLogin(t *testing.T) {
	var s = requireLogins(t, "Q1TEST", "hunter2")

	s.clientAccepted(0, connWithRemoteAddr(tcpAddr(t, "127.0.0.1")))
	require.True(t, s.isLoggedIn(0))

	s.handleClientCommand(0, loginFrame("Q1TEST", "wrong"))
	assert.True(t, s.isLoggedIn(0), "wrong credentials")

	var malformed = new(AGWPEMessage)
	malformed.Header.DataKind = 'P'
	malformed.Data = []byte("Q1TEST\x00hunter2")
	malformed.Header.DataLen = uint32(len(malformed.Data))

	s.handleClientCommand(0, malformed)
	assert.True(t, s.isLoggedIn(0), "malformed frame")
}

// A remote client does not inherit the exemption of whoever held the slot
// before it.
func TestClientAccepted_ExemptionDoesNotOutlastTheLocalClient(t *testing.T) {
	var s = requireLogins(t, "Q1TEST", "hunter2")

	s.clientAccepted(0, connWithRemoteAddr(tcpAddr(t, "127.0.0.1")))
	require.True(t, s.isLoggedIn(0))

	s.clientAccepted(0, connWithRemoteAddr(tcpAddr(t, "192.168.1.10")))
	assert.False(t, s.isLoggedIn(0))

	s.handleClientCommand(0, loginFrame("Q1TEST", "wrong"))
	assert.False(t, s.isLoggedIn(0), "a failed login leaves it logged out")
}

// watchingConn reports the socket attached to client 0 at the moment it is
// asked for its remote address, which is how clientAccepted settles a new
// client's login state.
type watchingConn struct {
	net.Conn

	srv           *AGWServer
	addr          net.Addr
	sockWhenAsked net.Conn
}

func (c *watchingConn) RemoteAddr() net.Addr {
	c.sockWhenAsked = c.srv.clients[0].conn

	return c.addr
}

// cmdListenThread watches the slot for a socket and acts on whatever it finds
// the moment one appears, so the socket has to be published only once the state
// a command is judged against is settled.  Otherwise a remote client that gets
// in quickly enough is let through on the strength of the previous holder's
// login - a local one's, in particular.
func TestClientAccepted_PublishesTheSocketLast(t *testing.T) {
	var s = requireLogins(t, "Q1TEST", "hunter2")
	s.setLoggedIn(0) /* As a previous client would have left it. */

	var conn = new(watchingConn)
	conn.srv = s
	conn.addr = tcpAddr(t, "192.168.1.10")

	s.clientAccepted(0, conn)

	assert.Nil(t, conn.sockWhenAsked, "socket published before the login state was settled")
	assert.Equal(t, net.Conn(conn), s.clients[0].conn)
	assert.False(t, s.isLoggedIn(0))
}

// Accepting a connection clears whatever the previous holder of the slot
// switched on.
func TestClientAccepted_ResetsMonitoringState(t *testing.T) {
	var s = new(AGWServer)
	s.clients[0].sendRaw = true
	s.clients[0].sendMonitor = true

	var server, _ = loopbackConns(t)
	s.clientAccepted(0, server)

	assert.False(t, s.clients[0].sendRaw)
	assert.False(t, s.clients[0].sendMonitor)
	assert.Equal(t, server, s.clients[0].conn)
}

// --- The client table under concurrent use ---

// nullConn swallows anything written to it and reports a remote address, which
// is all the server asks of a client's socket here.
type nullConn struct {
	net.Conn

	addr net.Addr
}

func (c *nullConn) Write(b []byte) (int, error) { return len(b), nil }
func (c *nullConn) Close() error                { return nil }
func (c *nullConn) RemoteAddr() net.Addr        { return c.addr }

// The client table is reached from more than one goroutine at a time: the
// thread accepting connections attaches a client to a slot, that client's own
// command thread switches its monitoring on and off, and the receive and
// transmit paths walk the whole table for every frame that goes past.  Run
// under -race, this fails if the table is not guarded.
func TestAGWServer_ClientTableUnderConcurrentUse(t *testing.T) {
	var s = new(AGWServer)

	var pp = AX25FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var conn = new(nullConn)
	conn.addr = tcpAddr(t, "192.168.1.10")

	var rawToggle = new(AGWPEMessage)
	rawToggle.Header.DataKind = 'k'

	var monitorToggle = new(AGWPEMessage)
	monitorToggle.Header.DataKind = 'm'

	var done = make(chan struct{})
	var wg sync.WaitGroup

	// Detaching a client tells the data link machinery it has gone, and
	// nothing here drains that queue, so put it back afterwards rather than
	// leave a pile of cleanups behind for whatever test runs next.
	dlqAppended(func() {
		wg.Go(func() { /* The receive path, for every frame heard. */
			for {
				select {
				case <-done:
					return
				default:
				}

				s.SendRecPacket(0, pp, []byte{0x01, 0x02})
			}
		})

		wg.Go(func() { /* Connections coming and going, and what they ask for. */
			defer close(done)

			for range 200 {
				s.clientAccepted(0, conn)
				s.handleClientCommand(0, rawToggle)
				s.handleClientCommand(0, monitorToggle)
				s.detachClient(0, conn)
			}
		})

		wg.Wait()
	})
}

// --- Detaching a client that has already been replaced ---

// A write that fails on a connection already replaced in its slot must leave
// the new one alone.  dl_client_cleanup goes by client number, so a cleanup
// meant for the old connection takes the new client's links and registered
// callsigns away, while its socket stays attached and it is told nothing.
func TestDetachClient_StaleConnLeavesItsSuccessorAlone(t *testing.T) {
	var s = new(AGWServer)

	var first = new(nullConn)
	first.addr = tcpAddr(t, "192.168.1.10")

	var second = new(nullConn)
	second.addr = tcpAddr(t, "192.168.1.11")

	s.clientAccepted(0, first)
	s.clientAccepted(0, second) /* The slot has moved on. */

	var item = dlqAppended(func() { s.detachClient(0, first) })

	assert.Nil(t, item, "cleanup queued against the client that holds the slot now")
	assert.Equal(t, net.Conn(second), s.clientConn(0), "the newer connection was detached")
}

// The ordinary case still cleans up, of course.
func TestDetachClient_AttachedConnIsCleanedUp(t *testing.T) {
	var s = new(AGWServer)

	var conn = new(nullConn)
	conn.addr = tcpAddr(t, "192.168.1.10")

	s.clientAccepted(0, conn)

	var item = dlqAppended(func() { s.detachClient(0, conn) })

	require.NotNil(t, item, "no cleanup queued for a client that really has gone")
	assert.Equal(t, DLQ_CLIENT_CLEANUP, item._type)
	assert.Equal(t, 0, item.client)
	assert.Nil(t, s.clientConn(0))
}

// A reply that cannot be delivered hangs up on the client, as the receive and
// transmit paths already did for the frames they send.  The client's own
// command thread would notice its connection had gone and detach it too, but
// only once its read returns; until then this is the one place that knows, and
// dropping the error left it saying nothing at all.
func TestSendToClient_WriteErrorDetachesTheClient(t *testing.T) {
	var s = new(AGWServer)

	var server, client = net.Pipe()
	client.Close()
	server.Close() /* So a write fails rather than blocking for a reader. */

	s.clientAccepted(0, server)
	require.NotNil(t, s.clientConn(0), "nothing attached to detach")

	var version = new(AGWPEMessage)
	version.Header.DataKind = 'R'

	var item = dlqAppended(func() { s.handleClientCommand(0, version) })

	assert.Nil(t, s.clientConn(0), "the connection we could not write to is still attached")
	require.NotNil(t, item, "connected mode was not told the client had gone")
	assert.Equal(t, DLQ_CLIENT_CLEANUP, item._type)
}
