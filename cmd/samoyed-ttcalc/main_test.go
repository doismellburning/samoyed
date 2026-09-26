// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"encoding/binary"
	"io"
	"net"
	"os"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

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

	sendRaw(t, conn, channel, append([]byte{channel << 4}, direwolf.AX25Pack(direwolf.MustAX25FromText(monitor))...))
}

// sendRaw sends a TNC's report of hearing data, which ought to be a KISS
// command byte followed by an AX.25 frame, on channel.
func sendRaw(t *testing.T, conn net.Conn, channel byte, data []byte) {
	t.Helper()

	var header = new(direwolf.AGWPEHeader)
	header.Portx = channel
	header.DataKind = 'K'
	header.DataLen = uint32(len(data))

	require.NoError(t, binary.Write(conn, binary.LittleEndian, header))

	var _, err = conn.Write(data)
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

	var p = testutils.StartMain(t)

	var tnc = testutils.Accept(t, ln)

	p.WaitFor(t, "Client app now connected to localhost, port 8000")

	var header direwolf.AGWPEHeader

	require.NoError(t, binary.Read(tnc, binary.LittleEndian, &header))
	assert.Equal(t, byte('k'), header.DataKind, "should ask for raw frames")

	// Something other than touch tones is only printed.
	sendMonitored(t, tnc, 1, "Q1TEST>APDW17:>Not for the calculator")
	p.WaitFor(t, "[1] Q1TEST>APDW17:>Not for the calculator")

	// What's heard off the air need not be AX.25, or anything at all, and
	// is reported and skipped.
	sendRaw(t, tnc, 1, []byte{0x10, 0x01, 0x02})
	p.WaitFor(t, "[1] Invalid AX.25 frame from server.")

	sendRaw(t, tnc, 1, nil)
	p.WaitFor(t, "[1] Empty frame from server.")

	// A touch tone sequence is worked out, and the answer sent back to be
	// spoken on the channel it was heard on.
	sendMonitored(t, tnc, 1, "Q2TEST>APDW17:t2*3A4#")
	p.WaitFor(t, "[1] Q2TEST>APDW17:t2*3A4#")
	p.WaitFor(t, "Calculator returns 10")

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

	p.WaitFor(t, "Connection to server closed.")
	assert.Equal(t, 1, p.Wait())
}
