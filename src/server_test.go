// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"strings"
	"testing"

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
	client_logged_in[0].Store(false)

	t.Cleanup(func() {
		agwpe_logins = old
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
