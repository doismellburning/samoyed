// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ccoveille/go-safecast/v2"
	"github.com/doismellburning/samoyed/internal/agwpe"
	"github.com/doismellburning/samoyed/internal/bbs/store/sqlite"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

// underBBS puts content under a top-level "bbs" key.
func underBBS(content string) []byte {
	var lines = strings.Split(content, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = "  " + line
		}
	}

	return []byte("bbs:\n" + strings.Join(lines, "\n"))
}

// writeConfig writes a configuration with its store in the test's directory.
func writeConfig(t *testing.T) string {
	t.Helper()

	var dir = t.TempDir()
	var file = filepath.Join(dir, "samoyed-bbs.yaml")

	require.NoError(t, os.WriteFile(file, underBBS("call: Q1BBS\nstore: "+filepath.Join(dir, "bbs.db")+"\n"), 0o600))

	return file
}

func TestHelp(t *testing.T) {
	t.Parallel()

	var result = testutils.RunMain(t, "", "--help")

	assert.Equal(t, 0, result.Status)
	assert.Contains(t, result.Output(), "packet BBS (experimental)")
	assert.Contains(t, result.Output(), "don't rely on it for mail that matters")
}

func TestConfigCheck(t *testing.T) {
	t.Parallel()

	var good = testutils.RunMain(t, "", "-c", writeConfig(t), "--config-check")
	assert.Equal(t, 0, good.Status)
	assert.Contains(t, good.Output(), "is OK")
	assert.NotContains(t, good.Output(), "experimental", "a check is for scripts, so says only how the file is")

	var badFile = filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(badFile, underBBS("call: Q1BBS-16\n"), 0o600))

	var bad = testutils.RunMain(t, "", "-c", badFile, "--config-check")
	assert.Equal(t, 1, bad.Status)
	assert.Contains(t, bad.Output(), `bbs.call: "Q1BBS-16" is not a callsign`)
	assert.Contains(t, bad.Output(), "bbs.store: the path of the message database is needed")

	var missing = testutils.RunMain(t, "", "-c", filepath.Join(t.TempDir(), "missing.yaml"))
	assert.Equal(t, 1, missing.Status)
	assert.Contains(t, missing.Output(), "no such file")
}

func TestNothingToServe(t *testing.T) {
	t.Parallel()

	var result = testutils.RunMain(t, "", "-c", writeConfig(t))

	assert.Equal(t, 1, result.Status)
	assert.Contains(t, result.Output(), "no agw block")
	assert.Contains(t, result.Output(), "use --console CALL")
}

func TestConsoleRefusesABadCall(t *testing.T) {
	t.Parallel()

	var result = testutils.RunMain(t, "", "-c", writeConfig(t), "--console", "not a call")

	assert.Equal(t, 1, result.Status)
	assert.Contains(t, result.Output(), "not a callsign")
}

func TestConsoleSessions(t *testing.T) {
	t.Parallel()

	var file = writeConfig(t)

	var first = testutils.RunMain(t, "SP Q1TEST\nHello\nHello there.\n/EX\nL\nB\n", "-c", file, "--console", "q2test")

	assert.Equal(t, 0, first.Status)
	assert.Contains(t, first.Output(), "Welcome to Q1BBS, a Samoyed BBS (experimental).\n[SAMBBS-0.0-H$]\nde Q1BBS>\n")
	assert.Contains(t, first.Output(), "Message 1 saved, ID 1_Q1BBS.")
	assert.Contains(t, first.Output(), "Q1TEST          Q2TEST")
	assert.NotContains(t, first.Output(), "Begin session", "the console is kept for the session unless asked")
	assert.Contains(t, first.Output(), "samoyed-bbs is experimental", "but it is warned the BBS is experimental")

	// The message is still there for the next session.
	var second = testutils.RunMain(t, "R 1\n", "-c", file, "--console", "Q1TEST", "-v")

	assert.Equal(t, 0, second.Status, second.Output())
	assert.Contains(t, second.Output(), "Hello there.")
	assert.Contains(t, second.Output(), "Begin session")
}

func TestConsoleWithAnUnopenableStore(t *testing.T) {
	t.Parallel()

	var file = filepath.Join(t.TempDir(), "samoyed-bbs.yaml")
	require.NoError(t, os.WriteFile(file, underBBS("call: Q1BBS\nstore: "+filepath.Join(t.TempDir(), "no", "such", "dir", "bbs.db")+"\n"), 0o600))

	var result = testutils.RunMain(t, "", "-c", file, "--console", "Q1TEST")

	assert.Equal(t, 1, result.Status)
	assert.Contains(t, result.Output(), "message store")
}

// readTNC reads one frame the BBS sent to the TNC.
func readTNC(t *testing.T, conn net.Conn) *agwpe.Message {
	t.Helper()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))

	var msg = new(agwpe.Message)

	require.NoError(t, binary.Read(conn, binary.LittleEndian, &msg.Header))

	msg.Data = make([]byte, msg.Header.DataLen)

	var _, err = io.ReadFull(conn, msg.Data)
	require.NoError(t, err)

	return msg
}

// writeTNC sends the BBS a frame as the TNC.
func writeTNC(t *testing.T, conn net.Conn, kind byte, from string, to string, data []byte) {
	t.Helper()

	var msg = new(agwpe.Message)

	msg.Header.DataKind = kind
	copy(msg.Header.CallFrom[:], from)
	copy(msg.Header.CallTo[:], to)
	msg.Header.DataLen = safecast.RequireConvert[uint32](t, len(data))
	msg.Data = data

	var _, err = msg.Write(conn, binary.LittleEndian)
	if !errors.Is(err, net.ErrClosed) {
		require.NoError(t, err)
	}
}

// readTNCUntil reads frames from the BBS until one of kind whose data
// contains want, answering every outstanding frames question with 0.
func readTNCUntil(t *testing.T, conn net.Conn, kind byte, want string) *agwpe.Message {
	t.Helper()

	for {
		var msg = readTNC(t, conn)

		if msg.Header.DataKind == 'Y' {
			writeTNC(t, conn, 'Y', msg.Header.CallFrom.String(), msg.Header.CallTo.String(), binary.LittleEndian.AppendUint32(nil, 0))

			continue
		}

		if msg.Header.DataKind == kind && strings.Contains(string(msg.Data), want) {
			return msg
		}
	}
}

// readText reads what the BBS sends the station, frame by frame, until it
// includes want, and returns all of it.
func readText(t *testing.T, conn net.Conn, want string) string {
	t.Helper()

	var text strings.Builder

	for !strings.Contains(text.String(), want) {
		text.Write(readTNCUntil(t, conn, 'D', "").Data)
	}

	return text.String()
}

// The BBS registers its callsign with the AGW server, and serves a station
// that connects through it.
func TestServingOverAGW(t *testing.T) {
	t.Parallel()

	var ln, port = testutils.Listen(t)

	var dir = t.TempDir()
	var file = filepath.Join(dir, "samoyed-bbs.yaml")

	require.NoError(t, os.WriteFile(file, underBBS("call: Q1BBS\nstore: "+filepath.Join(dir, "bbs.db")+"\nagw:\n  addr: 127.0.0.1:"+port+"\n"), 0o600))

	var p = testutils.StartMain(t, "-c", file)
	var tnc = testutils.Accept(t, ln)

	assert.Equal(t, byte('R'), readTNC(t, tnc).Header.DataKind)
	writeTNC(t, tnc, 'R', "", "", make([]byte, 8))

	assert.Equal(t, byte('G'), readTNC(t, tnc).Header.DataKind)
	writeTNC(t, tnc, 'G', "", "", []byte("1;Port1 first soundcard mono;"))

	var reg = readTNC(t, tnc)
	assert.Equal(t, byte('X'), reg.Header.DataKind)
	assert.Equal(t, "Q1BBS", reg.Header.CallFrom.String())
	writeTNC(t, tnc, 'X', "Q1BBS", "", []byte{1})

	p.WaitFor(t, "Connected to AGW server")
	assert.Contains(t, p.Output(), "samoyed-bbs is experimental")

	// A station connects, is greeted, lists the messages and leaves.
	writeTNC(t, tnc, 'C', "Q2TEST", "Q1BBS", []byte("*** CONNECTED To Station Q2TEST\r\x00"))

	var greeting = readText(t, tnc, "de Q1BBS>")
	assert.Contains(t, greeting, "Welcome to Q1BBS, a Samoyed BBS (experimental).\r[SAMBBS-0.0-H$]\r")

	writeTNC(t, tnc, 'D', "Q2TEST", "Q1BBS", []byte("L\r"))
	readText(t, tnc, "No messages.")

	writeTNC(t, tnc, 'D', "Q2TEST", "Q1BBS", []byte("B\r"))
	readText(t, tnc, "73")

	var hangUp = readTNCUntil(t, tnc, 'd', "")
	assert.Equal(t, "Q2TEST", hangUp.Header.CallTo.String())

	writeTNC(t, tnc, 'd', "Q2TEST", "Q1BBS", []byte("*** DISCONNECTED From Station Q2TEST\r\x00"))
	p.WaitFor(t, "End session")

	p.Signal(t, syscall.SIGTERM)
	assert.Equal(t, 0, p.Wait())
}

// A user's message for another BBS is queued for the partner that covers it,
// or held if none does.
func TestRoutingWhatUsersSend(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()
	var file = filepath.Join(dir, "samoyed-bbs.yaml")
	var db = filepath.Join(dir, "bbs.db")

	require.NoError(t, os.WriteFile(file, underBBS("call: Q1BBS\nstore: "+db+"\npartners:\n  - call: Q3BBS\n    covers: [GBR.EURO]\n"), 0o600))

	var result = testutils.RunMain(t, "SP Q1TEST@Q6BBS.GBR.EURO\nNear\nHi.\n/EX\nSP Q1TEST@Q6BBS.USA.NOAM\nFar\nHi.\n/EX\nB\n", "-c", file, "--console", "Q2TEST")

	assert.Equal(t, 0, result.Status, result.Output())
	assert.Contains(t, result.Output(), "Message 1 saved, ID 1_Q1BBS.\nde Q1BBS>")
	assert.Contains(t, result.Output(), "Message 2 saved, ID 2_Q1BBS.\nIt is held for the sysop")

	var store, err = sqlite.Open(t.Context(), db)
	require.NoError(t, err)

	t.Cleanup(func() { _ = store.Close() })

	var queue, queueErr = store.QueueFor(t.Context(), "Q3BBS")
	require.NoError(t, queueErr)
	assert.Equal(t, []int{1}, queue)
}
