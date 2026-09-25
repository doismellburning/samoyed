// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux || darwin

package main

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/doismellburning/samoyed/internal/testutils"
	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/pkg/term"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

// shortWriter takes at most a few bytes at a time.
type shortWriter struct {
	written []byte
}

func (w *shortWriter) Write(p []byte) (int, error) {
	var n = min(len(p), 3)

	w.written = append(w.written, p[:n]...)

	return n, nil
}

type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("broken")
}

func Test_writeFull(t *testing.T) {
	var w = new(shortWriter)

	require.NoError(t, writeFull(w, []byte("0001 send data\r")))
	assert.Equal(t, "0001 send data\r", string(w.written))

	assert.Error(t, writeFull(failingWriter{}, []byte("x")))
}

// resetState puts the globals back as main would find them, once the test is
// over.
func resetState(t *testing.T) {
	t.Helper()

	var saved = struct {
		usingTCP [MAX_TNC]bool
		sock     [MAX_TNC]net.Conn
		serial   [MAX_TNC]*term.Term
		address  [MAX_TNC]string
		width    int
	}{
		tnctest_using_tcp, tnctest_server_sock, tnctest_serial_fd, tnc_address, column_width,
	}

	tnc_address = [MAX_TNC]string{"DW0", "DW1"}

	// Each TNC's state starts afresh, as it would in a run of its own.
	var clearState = func() {
		for j := range MAX_TNC {
			busy[j].Store(false)
			have_cmd_prompt[j].Store(false)
			last_rec_seq[j].Store(0)
		}
	}

	clearState()

	t.Cleanup(func() {
		tnctest_using_tcp, tnctest_server_sock, tnctest_serial_fd = saved.usingTCP, saved.sock, saved.serial
		tnc_address, column_width = saved.address, saved.width

		clearState()
	})
}

func Test_process_rec_data(t *testing.T) {
	resetState(t)

	// The answering end counts what is sent to it...
	process_rec_data(1, "0001 send data\r")
	process_rec_data(1, "0002 send data\r")
	assert.Equal(t, int64(2), last_rec_seq[1].Load())

	// ...and the calling end counts the replies.
	process_rec_data(0, "0001 reply\r")
	assert.Equal(t, int64(1), last_rec_seq[0].Load())

	// Each only counts its own half of the conversation.
	process_rec_data(0, "0003 send data\r")
	process_rec_data(1, "0002 reply\r")
	assert.Equal(t, int64(1), last_rec_seq[0].Load())
	assert.Equal(t, int64(2), last_rec_seq[1].Load())

	// Pieces of the alphabet test segmentation, and don't count.
	process_rec_data(0, "ABCDE\r")
	assert.Equal(t, int64(1), last_rec_seq[0].Load())

	assert.Panics(t, func() { process_rec_data(0, "Something else") })
}

// agwConn points TNC from at one end of a TCP connection and hands back the
// other, where the test plays the TNC.
func agwConn(t *testing.T, from int) net.Conn {
	t.Helper()

	var ln, port = testutils.Listen(t)

	var conn, dialErr = new(net.Dialer).DialContext(t.Context(), "tcp", net.JoinHostPort("127.0.0.1", port))
	require.NoError(t, dialErr)

	t.Cleanup(func() { conn.Close() })

	var tnc = testutils.Accept(t, ln)

	tnctest_using_tcp[from] = true
	tnctest_server_sock[from] = conn

	return tnc
}

func readHeader(t *testing.T, r io.Reader) direwolf.AGWPEHeader {
	t.Helper()

	var header direwolf.AGWPEHeader
	require.NoError(t, binary.Read(r, binary.LittleEndian, &header))

	return header
}

func callsign(b [10]byte) string {
	return strings.TrimRight(string(b[:]), "\x00")
}

func Test_tnc_commands_net(t *testing.T) {
	resetState(t)

	var tnc = agwConn(t, 0)

	tnc_connect(0, 1)

	var header = readHeader(t, tnc)
	assert.Equal(t, byte('C'), header.DataKind)
	assert.Equal(t, "DW0", callsign(header.CallFrom))
	assert.Equal(t, "DW1", callsign(header.CallTo))

	tnc_send_data(0, 1, "0001 send data\r")

	header = readHeader(t, tnc)
	assert.Equal(t, byte('D'), header.DataKind)
	assert.Equal(t, byte(0xf0), header.PID)
	assert.Equal(t, "DW0", callsign(header.CallFrom))
	assert.Equal(t, "DW1", callsign(header.CallTo))
	require.Equal(t, uint32(len("0001 send data\r")), header.DataLen)

	var data = make([]byte, header.DataLen)

	var _, readErr = io.ReadFull(tnc, data)
	require.NoError(t, readErr)
	assert.Equal(t, "0001 send data\r", string(data))

	tnc_disconnect(0, 1)

	header = readHeader(t, tnc)
	assert.Equal(t, byte('d'), header.DataKind)
	assert.Equal(t, "DW0", callsign(header.CallFrom))
	assert.Equal(t, "DW1", callsign(header.CallTo))

	// There's no reset for an AGW TNC, so nothing more should turn up.
	tnc_reset(0, 1)

	require.NoError(t, tnc.SetReadDeadline(time.Now().Add(100*time.Millisecond)))

	var _, err = tnc.Read(make([]byte, 1))

	var netErr net.Error
	require.ErrorAs(t, err, &netErr)
	assert.True(t, netErr.Timeout())
}

// serialTNC points TNC from at a pseudo terminal and hands back the far end,
// where the test plays the TNC.
func serialTNC(t *testing.T, from int) *os.File {
	t.Helper()

	var master, slave, openErr = pty.Open()
	require.NoError(t, openErr)

	var fd = direwolf.SerialPortOpen(slave.Name(), 9600)
	require.NotNil(t, fd)

	require.NoError(t, slave.Close())

	t.Cleanup(func() {
		fd.Close()
		master.Close()
	})

	tnctest_using_tcp[from] = false
	tnctest_serial_fd[from] = fd

	return master
}

// readUntil reads from f until what it has read ends with want.
func readUntil(t *testing.T, f *os.File, want string) string {
	t.Helper()

	var got = make(chan string, 1)

	go func() {
		var read []byte

		var buf = make([]byte, 1)

		for !strings.HasSuffix(string(read), want) {
			var _, err = f.Read(buf)
			if err != nil {
				break
			}

			read = append(read, buf[0])
		}

		got <- string(read)
	}()

	select {
	case s := <-got:
		return s
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out reading from the TNC", "wanted %q", want)

		return ""
	}
}

// With the TNC already showing its command prompt, there's no need to break
// into command mode first, and none of its long waits.
func Test_tnc_commands_serial(t *testing.T) {
	resetState(t)

	tnc_address = [MAX_TNC]string{"TNC0", "TNC1"}

	var tnc = serialTNC(t, 0)

	have_cmd_prompt[0].Store(true)

	tnc_connect(0, 1)
	assert.Equal(t, "connect TNC1\r", readUntil(t, tnc, "\r"))

	tnc_send_data(0, 1, "0001 send data\r")
	assert.Equal(t, "0001 send data\r", readUntil(t, tnc, "\r"))

	tnc_disconnect(0, 1)
	assert.Equal(t, "disconnect\r", readUntil(t, tnc, "\r"))

	// A reset always breaks into command mode first, prompt or no prompt.
	tnc_reset(0, 1)
	assert.Equal(t, ETX_BREAK+"\rreset\r", readUntil(t, tnc, "reset\r"))
}

// main takes hours over a full run, and its TNC goroutines exit the process
// when a TNC goes away, so it runs on its own.
func Test_main_connects(t *testing.T) {
	var ln0, port0 = testutils.Listen(t)
	var ln1, port1 = testutils.Listen(t)

	var p = testutils.StartMain(t, "127.0.0.1:"+port0+"=Caller", "127.0.0.1:"+port1+"=Answerer")

	var tnc0 = testutils.Accept(t, ln0)
	var tnc1 = testutils.Accept(t, ln1)

	// Each TNC is asked for raw frames and to register its callsign.
	for i, tnc := range []net.Conn{tnc0, tnc1} {
		assert.Equal(t, byte('k'), readHeader(t, tnc).DataKind)

		var register = readHeader(t, tnc)
		assert.Equal(t, byte('X'), register.DataKind)
		assert.Equal(t, []string{"DW0", "DW1"}[i], callsign(register.CallFrom))
	}

	p.WaitFor(t, "Andiamo!")

	// The first then calls the second.
	var connect = readHeader(t, tnc0)
	assert.Equal(t, byte('C'), connect.DataKind)
	assert.Equal(t, "DW0", callsign(connect.CallFrom))
	assert.Equal(t, "DW1", callsign(connect.CallTo))

	// The first end reports the connection.
	var connected = new(direwolf.AGWPEHeader)
	connected.DataKind = 'C'
	copy(connected.CallFrom[:], "DW1")

	require.NoError(t, binary.Write(tnc0, binary.LittleEndian, connected))

	p.WaitFor(t, "*** Connected to DW1")
}

func Test_main_badArguments(t *testing.T) {
	var gone = "127.0.0.1:" + testutils.UnusedPort(t)

	var testCases = map[string]struct {
		args []string
		want string
	}{
		"too few":        {[]string{"8000=one"}, "Specify minimum 2, maximum 2 TNCs on the command line."},
		"too many":       {[]string{"1=a", "2=b", "3=c"}, "Specify minimum 2, maximum 2 TNCs on the command line."},
		"no description": {[]string{"8000", "8001=two"}, "Internal error 1"},
		"unreachable":    {[]string{gone + "=Gone", gone + "=Gone"}, "unable to connect to Gone"},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var result = testutils.RunMain(t, "", tc.args...)

			assert.Equal(t, 1, result.Status)
			assert.Contains(t, result.Output(), tc.want)
		})
	}
}
