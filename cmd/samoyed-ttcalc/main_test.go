// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"encoding/binary"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runMainEnv, when set, has the test binary run main instead of the tests,
// since main never returns.
const runMainEnv = "SAMOYED_TTCALC_RUN_MAIN"

func TestMain(m *testing.M) {
	if _, ok := os.LookupEnv(runMainEnv); ok {
		main()
		os.Exit(0)
	}

	os.Exit(m.Run())
}

func Test_calculator(t *testing.T) {
	assert.Equal(t, 46, calculator("12a34#"))
	assert.Equal(t, 10, calculator("2*3A4#"))
	assert.Equal(t, 503, calculator("5*100A3#"))
	assert.Equal(t, 50, calculator("6a4*5#"))
}

// Regression test for #671: the sequence arrives over the air, so it need not
// end with the "#" that terminates a well-formed one.  It used to fall out of
// the loop and panic.
func Test_calculator_unterminated(t *testing.T) {
	assert.Equal(t, 46, calculator("12a34"))
	assert.Equal(t, 10, calculator("2*3A4"))
	assert.Equal(t, 12, calculator("12"))
	assert.Equal(t, 0, calculator(""))
	assert.Equal(t, 0, calculator("#"))
}

// Subtraction and division have no key to call them yet, but are there for
// whoever adds one.
func Test_do_lastop(t *testing.T) {
	assert.Equal(t, 7, do_lastop(NONE, 3, 7))
	assert.Equal(t, 10, do_lastop(ADD, 3, 7))
	assert.Equal(t, -4, do_lastop(SUB, 3, 7))
	assert.Equal(t, 21, do_lastop(MUL, 3, 7))
	assert.Equal(t, 3, do_lastop(DIV, 21, 7))
}

func Test_connect_to_server(t *testing.T) {
	var ln, listenErr = new(net.ListenConfig).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	require.NoError(t, listenErr)

	var _, port, splitErr = net.SplitHostPort(ln.Addr().String())
	require.NoError(t, splitErr)

	var conn, err = connect_to_server("127.0.0.1", port)
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	// Nothing is listening once this is closed.
	require.NoError(t, ln.Close())

	var _, closedErr = connect_to_server("127.0.0.1", port)
	assert.Error(t, closedErr)
}

// sendMonitored sends a TNC's report of hearing monitor on channel.
func sendMonitored(t *testing.T, conn net.Conn, channel byte, monitor string) {
	t.Helper()

	var pp = direwolf.AX25FromText(monitor, true)
	require.NotNil(t, pp)

	var frame = append([]byte{channel << 4}, direwolf.AX25Pack(pp)...)

	var header = new(direwolf.AGWPEHeader)
	header.Portx = channel
	header.DataKind = 'K'
	header.DataLen = uint32(len(frame))

	require.NoError(t, binary.Write(conn, binary.LittleEndian, header))

	var _, err = conn.Write(frame)
	require.NoError(t, err)
}

// main always talks to a TNC on port 8000 of this machine, so this plays one
// there, if it can have the port.
func Test_main(t *testing.T) {
	var ln, listenErr = new(net.ListenConfig).Listen(t.Context(), "tcp4", "127.0.0.1:8000")
	if listenErr != nil {
		t.Skipf("Can't play the TNC on port 8000: %v", listenErr)
	}

	defer ln.Close()

	var cmd = exec.CommandContext(t.Context(), os.Args[0]) //nolint:gosec
	cmd.Env = append(os.Environ(), runMainEnv+"=1")

	var stdout strings.Builder

	cmd.Stdout = &stdout

	require.NoError(t, cmd.Start())

	var tnc, acceptErr = ln.Accept()
	require.NoError(t, acceptErr)

	require.NoError(t, tnc.SetDeadline(time.Now().Add(10*time.Second)))

	var header direwolf.AGWPEHeader

	require.NoError(t, binary.Read(tnc, binary.LittleEndian, &header))
	assert.Equal(t, byte('k'), header.DataKind, "should ask for raw frames")

	// Something other than touch tones is only printed.
	sendMonitored(t, tnc, 1, "Q1TEST>APDW17:>Not for the calculator")

	// A touch tone sequence is worked out, and the answer sent back to be
	// spoken on the channel it was heard on.
	sendMonitored(t, tnc, 1, "Q2TEST>APDW17:t2*3A4#")

	require.NoError(t, binary.Read(tnc, binary.LittleEndian, &header))
	assert.Equal(t, byte('K'), header.DataKind)
	assert.Equal(t, byte(1), header.Portx)

	var reply = make([]byte, header.DataLen)

	var _, readErr = io.ReadFull(tnc, reply)
	require.NoError(t, readErr)

	var pp = direwolf.AX25FromFrame(reply[1:], direwolf.ALevel{})
	require.NotNil(t, pp)
	assert.Equal(t, "N0CALL>SPEECH:10", direwolf.AX25FormatAddrs(pp)+string(direwolf.AX25GetInfo(pp)))

	// The TNC going away ends it.
	require.NoError(t, tnc.Close())

	var waitErr = cmd.Wait()

	var exitErr *exec.ExitError
	require.ErrorAs(t, waitErr, &exitErr)
	assert.Equal(t, 1, exitErr.ExitCode())

	assert.Contains(t, stdout.String(), "Client app now connected to localhost, port 8000")
	assert.Contains(t, stdout.String(), "[1] Q1TEST>APDW17:>Not for the calculator\n")
	assert.Contains(t, stdout.String(), "[1] Q2TEST>APDW17:t2*3A4#\n")
	assert.Contains(t, stdout.String(), "Calculator returns 10")
	assert.Contains(t, stdout.String(), "Connection to server closed.")
}
