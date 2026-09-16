// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"net"
	"strings"
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

// requireLogins configures credentials for the AGW port for the duration of the
// test, and starts client 0 logged out.  No pairs means no login required.
func requireLogins(t *testing.T, pairs ...string) {
	t.Helper()
	require.Zero(t, len(pairs)%2, "want a password for every user name")

	var logins []agwpe_login_s
	for i := 0; i < len(pairs); i += 2 {
		var login = new(agwpe_login_s)
		login.user = pairs[i]
		login.password = pairs[i+1]
		logins = append(logins, *login)
	}

	var old = agwpe_logins
	agwpe_logins = logins
	client_login_exempt[0].Store(false)
	client_logged_in[0].Store(false)

	t.Cleanup(func() {
		agwpe_logins = old
		client_login_exempt[0].Store(false)
		client_logged_in[0].Store(false)
	})
}

func TestAgwLoginRequired(t *testing.T) {
	requireLogins(t)
	assert.False(t, agwLoginRequired(), "no credentials configured means no login")

	requireLogins(t, "Q1TEST", "hunter2")
	assert.True(t, agwLoginRequired())
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
	requireLogins(t, "Q1TEST", "hunter2")

	handleClientCommand(0, loginFrame("Q1TEST", "hunter2"))

	assert.True(t, client_logged_in[0].Load())
}

func TestHandleClientCommand_P_WrongCredentialsDoNotLogIn(t *testing.T) {
	requireLogins(t, "Q1TEST", "hunter2")

	handleClientCommand(0, loginFrame("Q1TEST", "hunter3"))
	assert.False(t, client_logged_in[0].Load(), "wrong password")

	handleClientCommand(0, loginFrame("Q2TEST", "hunter2"))
	assert.False(t, client_logged_in[0].Load(), "wrong user name")

	handleClientCommand(0, loginFrame("", ""))
	assert.False(t, client_logged_in[0].Load(), "empty credentials")
}

// AGWPE accepts any one of the user name and password combinations it has been
// given, so each set has to work in its own right.
func TestHandleClientCommand_P_AnyConfiguredCredentialsLogIn(t *testing.T) {
	requireLogins(t, "Q1TEST", "hunter2", "Q2TEST", "correct horse")

	handleClientCommand(0, loginFrame("Q1TEST", "hunter2"))
	assert.True(t, client_logged_in[0].Load(), "first set")

	client_logged_in[0].Store(false)

	handleClientCommand(0, loginFrame("Q2TEST", "correct horse"))
	assert.True(t, client_logged_in[0].Load(), "second set")
}

// Credentials are matched as a pair, so one user's password does not open
// another user's account.
func TestHandleClientCommand_P_CredentialsDoNotCrossBetweenUsers(t *testing.T) {
	requireLogins(t, "Q1TEST", "hunter2", "Q2TEST", "correct horse")

	handleClientCommand(0, loginFrame("Q1TEST", "correct horse"))
	assert.False(t, client_logged_in[0].Load())

	handleClientCommand(0, loginFrame("Q2TEST", "hunter2"))
	assert.False(t, client_logged_in[0].Load())
}

func TestAgwMatchLogin(t *testing.T) {
	requireLogins(t, "Q1TEST", "hunter2", "Q2TEST", "correct horse")

	var matched, accepted = agwMatchLogin("Q2TEST", "correct horse")
	assert.True(t, accepted)
	assert.Equal(t, "Q2TEST", matched, "the name reported is the one we configured")

	matched, accepted = agwMatchLogin("Q3TEST", "hunter2")
	assert.False(t, accepted)
	assert.Empty(t, matched)
}

// An accepted login is not permanent: getting it wrong afterwards takes it away
// again, so a client cannot log in and then hand the socket to someone else.
func TestHandleClientCommand_P_FailureAfterSuccessLogsOut(t *testing.T) {
	requireLogins(t, "Q1TEST", "hunter2")

	handleClientCommand(0, loginFrame("Q1TEST", "hunter2"))
	require.True(t, client_logged_in[0].Load())

	handleClientCommand(0, loginFrame("Q1TEST", "wrong"))
	assert.False(t, client_logged_in[0].Load())
}

// A login we cannot read is a failed login like any other, so it takes an
// earlier success away rather than leaving the socket authorized.
func TestHandleClientCommand_P_MalformedFrameAfterSuccessLogsOut(t *testing.T) {
	requireLogins(t, "Q1TEST", "hunter2")

	handleClientCommand(0, loginFrame("Q1TEST", "hunter2"))
	require.True(t, client_logged_in[0].Load())

	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'P'
	cmd.Data = []byte("Q1TEST\x00hunter2")
	cmd.Header.DataLen = uint32(len(cmd.Data))

	handleClientCommand(0, cmd)

	assert.False(t, client_logged_in[0].Load())
}

func TestHandleClientCommand_P_MalformedFrameDoesNotLogIn(t *testing.T) {
	requireLogins(t, "Q1TEST", "hunter2")

	// Shorter than the two fixed size fields, so there is nothing to compare.
	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'P'
	cmd.Data = []byte("Q1TEST\x00hunter2")
	cmd.Header.DataLen = uint32(len(cmd.Data))

	handleClientCommand(0, cmd)

	assert.False(t, client_logged_in[0].Load())
}

// Without AGWLOGIN, a login frame is silently ignored, as Dire Wolf does.
func TestHandleClientCommand_P_IgnoredWhenNoLoginConfigured(t *testing.T) {
	requireLogins(t)

	handleClientCommand(0, loginFrame("Q1TEST", "hunter2"))

	assert.False(t, client_logged_in[0].Load())
}

func TestHandleClientCommand_CommandsIgnoredUntilLoggedIn(t *testing.T) {
	requireLogins(t, "Q1TEST", "hunter2")

	var client = setupClientPipe(t)
	var replyCh = asyncReply(client)

	// 'R' always gets a version reply - unless we have not logged in.
	var version = new(AGWPEMessage)
	version.Header.DataKind = 'R'
	handleClientCommand(0, version)

	select {
	case reply := <-replyCh:
		t.Fatalf("expected no reply before logging in, got '%c'", reply.Header.DataKind)
	default:
	}

	handleClientCommand(0, loginFrame("Q1TEST", "hunter2"))
	require.True(t, client_logged_in[0].Load())

	handleClientCommand(0, version)

	var reply = <-replyCh
	require.NotNil(t, reply)
	assert.Equal(t, byte('R'), reply.Header.DataKind)
}

// Without AGWLOGIN nothing changes: commands work without any login at all.
func TestHandleClientCommand_NoLoginConfiguredCommandsWork(t *testing.T) {
	requireLogins(t)

	var client = setupClientPipe(t)
	var replyCh = asyncReply(client)

	var version = new(AGWPEMessage)
	version.Header.DataKind = 'R'
	handleClientCommand(0, version)

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
func TestAgwClientAccepted_LocalClientNeedsNoLogin(t *testing.T) {
	requireLogins(t, "Q1TEST", "hunter2")

	var server, client = loopbackConns(t)
	t.Cleanup(func() { client_sock[0] = nil })

	agwClientAccepted(0, server)
	assert.True(t, client_logged_in[0].Load())

	var version = new(AGWPEMessage)
	version.Header.DataKind = 'R'
	handleClientCommand(0, version)

	var reply, readErr = readReplyFrom(client)
	require.NoError(t, readErr)
	assert.Equal(t, byte('R'), reply.Header.DataKind)
}

func TestAgwClientAccepted_RemoteClientMustLogIn(t *testing.T) {
	requireLogins(t, "Q1TEST", "hunter2")
	t.Cleanup(func() { client_sock[0] = nil })

	agwClientAccepted(0, connWithRemoteAddr(tcpAddr(t, "192.168.1.10")))

	assert.False(t, client_logged_in[0].Load())
}

// Without AGWLOGIN the exemption changes nothing, because nobody has to log in.
func TestAgwClientAccepted_NoLoginConfigured(t *testing.T) {
	requireLogins(t)
	t.Cleanup(func() { client_sock[0] = nil })

	agwClientAccepted(0, connWithRemoteAddr(tcpAddr(t, "192.168.1.10")))

	assert.False(t, agwLoginRequired())
}

// A client on this machine is exempt however its login frames turn out, so
// getting the credentials wrong does not cost it the access it never had to
// ask for.
func TestAgwClientAccepted_LocalClientSurvivesAFailedLogin(t *testing.T) {
	requireLogins(t, "Q1TEST", "hunter2")
	t.Cleanup(func() { client_sock[0] = nil })

	agwClientAccepted(0, connWithRemoteAddr(tcpAddr(t, "127.0.0.1")))
	require.True(t, client_logged_in[0].Load())

	handleClientCommand(0, loginFrame("Q1TEST", "wrong"))
	assert.True(t, client_logged_in[0].Load(), "wrong credentials")

	var malformed = new(AGWPEMessage)
	malformed.Header.DataKind = 'P'
	malformed.Data = []byte("Q1TEST\x00hunter2")
	malformed.Header.DataLen = uint32(len(malformed.Data))

	handleClientCommand(0, malformed)
	assert.True(t, client_logged_in[0].Load(), "malformed frame")
}

// A remote client does not inherit the exemption of whoever held the slot
// before it.
func TestAgwClientAccepted_ExemptionDoesNotOutlastTheLocalClient(t *testing.T) {
	requireLogins(t, "Q1TEST", "hunter2")
	t.Cleanup(func() { client_sock[0] = nil })

	agwClientAccepted(0, connWithRemoteAddr(tcpAddr(t, "127.0.0.1")))
	require.True(t, client_logged_in[0].Load())

	agwClientAccepted(0, connWithRemoteAddr(tcpAddr(t, "192.168.1.10")))
	assert.False(t, client_logged_in[0].Load())

	handleClientCommand(0, loginFrame("Q1TEST", "wrong"))
	assert.False(t, client_logged_in[0].Load(), "a failed login leaves it logged out")
}

// watchingConn reports the value of client_sock at the moment it is asked for
// its remote address, which is how agwClientAccepted settles a new client's
// login state.
type watchingConn struct {
	net.Conn

	addr          net.Addr
	sockWhenAsked net.Conn
}

func (c *watchingConn) RemoteAddr() net.Addr {
	c.sockWhenAsked = client_sock[0]

	return c.addr
}

// cmd_listen_thread watches the slot for a socket and acts on whatever it finds
// the moment one appears, so the socket has to be published only once the state
// a command is judged against is settled.  Otherwise a remote client that gets
// in quickly enough is let through on the strength of the previous holder's
// login - a local one's, in particular.
func TestAgwClientAccepted_PublishesTheSocketLast(t *testing.T) {
	requireLogins(t, "Q1TEST", "hunter2")
	client_logged_in[0].Store(true) /* As a previous client would have left it. */

	t.Cleanup(func() { client_sock[0] = nil })

	var conn = new(watchingConn)
	conn.addr = tcpAddr(t, "192.168.1.10")

	agwClientAccepted(0, conn)

	assert.Nil(t, conn.sockWhenAsked, "socket published before the login state was settled")
	assert.Equal(t, net.Conn(conn), client_sock[0])
	assert.False(t, client_logged_in[0].Load())
}

// Accepting a connection clears whatever the previous holder of the slot
// switched on.
func TestAgwClientAccepted_ResetsMonitoringState(t *testing.T) {
	enable_send_raw_to_client[0] = true
	enable_send_monitor_to_client[0] = true
	t.Cleanup(func() {
		enable_send_raw_to_client[0] = false
		enable_send_monitor_to_client[0] = false
		client_sock[0] = nil
	})

	var server, _ = loopbackConns(t)
	agwClientAccepted(0, server)

	assert.False(t, enable_send_raw_to_client[0])
	assert.False(t, enable_send_monitor_to_client[0])
	assert.Equal(t, server, client_sock[0])
}
