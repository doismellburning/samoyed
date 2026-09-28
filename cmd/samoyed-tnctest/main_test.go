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
	"github.com/doismellburning/samoyed/internal/direwolf"
	"github.com/doismellburning/samoyed/internal/serialport"
	"github.com/doismellburning/samoyed/internal/testutils"
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
}

// A serial TNC says more than the test's own data - its prompt, its
// connection reports, its answers to our commands - and none of that is for
// process_rec_data to count or to object to.
func Test_process_rec_data_serialChatter(t *testing.T) {
	resetState(t)

	for _, line := range []string{"cmd:", "*** CONNECTED to Q2TEST", "*** DISCONNECTED", "MYCALL was Q1TEST", "1 retry"} {
		for i := range MAX_TNC {
			assert.NotPanics(t, func() { process_rec_data(i, line) }, "%q", line)
			assert.Zero(t, last_rec_seq[i].Load(), "%q", line)
		}
	}
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

func callsign(c direwolf.AGWPECallsign) string {
	return c.String()
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

	var fd = serialport.SerialPortOpen(slave.Name(), 9600)
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

// A serial TNC's prompt and connection reports pass through the line handling
// without upsetting it, the reports keep track of the connection, and the
// test's own data is still answered.
func Test_tnc_serial_line_chatter(t *testing.T) {
	resetState(t)

	var saved = is_connected[1].Load()

	t.Cleanup(func() { is_connected[1].Store(saved) })

	is_connected[1].Store(0)

	var tnc = serialTNC(t, 1)

	tnc_serial_line(1, "cmd:")
	tnc_serial_line(1, "1 retry")

	tnc_serial_line(1, "*** CONNECTED to Q1TEST")
	assert.Equal(t, int32(1), is_connected[1].Load())

	tnc_serial_line(1, "0001 send data")
	assert.Equal(t, "0001 reply\r", readUntil(t, tnc, "\r"))
	assert.Equal(t, int64(1), last_rec_seq[1].Load())

	tnc_serial_line(1, "*** DISCONNECTED")
	assert.Equal(t, int32(0), is_connected[1].Load())
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

	p.WaitFor(t, "*** Connected to DW1 ***")
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
		"no serial port": {[]string{"/nonexistent/tty0=Gone", "/nonexistent/tty1=Gone"}, "unable to connect to Gone on /nonexistent/tty"},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var result = testutils.RunMain(t, "", tc.args...)

			assert.Equal(t, 1, result.Status)
			assert.Contains(t, result.Output(), tc.want)
		})
	}
}

// sendFrame has the TNC at the far end of conn send an AGW frame of the given
// kind, carrying data.
func sendFrame(t *testing.T, conn net.Conn, kind byte, from string, data string) {
	t.Helper()

	var header = new(direwolf.AGWPEHeader)
	header.DataKind = kind
	header.DataLen = uint32(len(data))
	copy(header.CallFrom[:], from)

	require.NoError(t, binary.Write(conn, binary.LittleEndian, header))
	require.NoError(t, writeFull(conn, []byte(data)))
}

// readFrame reads an AGW frame, header and data, from conn.
func readFrame(t *testing.T, conn net.Conn) (direwolf.AGWPEHeader, string) {
	t.Helper()

	var header = readHeader(t, conn)

	var data = make([]byte, header.DataLen)

	var _, err = io.ReadFull(conn, data)
	require.NoError(t, err)

	return header, string(data)
}

// startNet runs main against two fake AGW TNCs, the first given as host:port
// and the second as a bare port, and hands them back once main has set both
// up and asked the first to connect.
func startNet(t *testing.T) (*testutils.Process, net.Conn, net.Conn) {
	t.Helper()

	var ln0, port0 = testutils.Listen(t)
	var ln1, port1 = testutils.Listen(t)

	var p = testutils.StartMain(t, "127.0.0.1:"+port0+"=Caller", port1+"=Answerer")

	var tnc0 = testutils.Accept(t, ln0)
	var tnc1 = testutils.Accept(t, ln1)

	for _, tnc := range []net.Conn{tnc0, tnc1} {
		readHeader(t, tnc) // Raw frames
		readHeader(t, tnc) // Register callsign
	}

	p.WaitFor(t, "Andiamo!")

	assert.Equal(t, byte('C'), readHeader(t, tnc0).DataKind)

	return p, tnc0, tnc1
}

// The conversation main runs, from the TNCs' side, as far as the first
// exchange, after which the calling TNC goes away.
func Test_main_conversation(t *testing.T) {
	t.Parallel()

	var p, tnc0, tnc1 = startNet(t)

	// Both ends report the connection, which lets main get on with it.
	sendFrame(t, tnc0, 'C', "DW1", "")
	p.WaitFor(t, "*** Connected to DW1 ***")
	sendFrame(t, tnc1, 'C', "DW0", "")
	p.WaitFor(t, "*** Connected to DW0 ***")

	// The answering end replies to the first piece of data, and follows it
	// with pieces of the alphabet of every length, to test segmentation.
	sendFrame(t, tnc1, 'D', "DW0", "0001 send data\r")

	var header, data = readFrame(t, tnc1)
	assert.Equal(t, byte('D'), header.DataKind)
	assert.Equal(t, "DW1", callsign(header.CallFrom))
	assert.Equal(t, "DW0", callsign(header.CallTo))
	assert.Equal(t, "0001 reply\r", data)

	for j := 1; j <= 26; j++ {
		_, data = readFrame(t, tnc1)
		assert.Equal(t, "ABCDEFGHIJKLMNOPQRSTUVWXYZ"[:j]+"\r", data)
	}

	// The calling end takes the reply, and the pieces, in its stride.
	sendFrame(t, tnc0, 'D', "DW1", "0001 reply\r")
	sendFrame(t, tnc0, 'D', "DW1", "ABC\r")
	sendFrame(t, tnc0, 'y', "", "")
	sendFrame(t, tnc0, 'K', "", "ignored")
	p.WaitFor(t, "*** Outstanding frames waiting")

	sendFrame(t, tnc1, 'd', "DW0", "")
	p.WaitFor(t, "*** Disconnected from DW0 ***")

	require.NoError(t, tnc0.Close())

	p.WaitFor(t, "TNC 0 connection closed.")
	assert.Equal(t, 1, p.Wait())
}

// A TNC that goes away part way through a frame ends the test.
func Test_main_brokenFrame(t *testing.T) {
	t.Parallel()

	var promised = new(direwolf.AGWPEHeader)
	promised.DataKind = 'D'
	promised.DataLen = 10

	var frame, appendErr = binary.Append(nil, binary.LittleEndian, promised)
	require.NoError(t, appendErr)

	var testCases = map[string]struct {
		send []byte
		want string
	}{
		"header": {frame[:3], "Read error, TNC 1 got unexpected EOF."},
		"data":   {append(frame, 'x'), "Read error, TNC 1 got unexpected EOF reading data."},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var p, _, tnc1 = startNet(t)

			var _, err = tnc1.Write(tc.send)
			require.NoError(t, err)
			require.NoError(t, tnc1.Close())

			p.WaitFor(t, tc.want)
			assert.Equal(t, 1, p.Wait())
		})
	}
}

// A serial TNC is set up, then answers what the other sends it, and minds
// its flow control, until it goes away.  Setting it up takes a few seconds.
func Test_main_serial(t *testing.T) {
	t.Parallel()

	var master, slave, openErr = pty.Open()
	require.NoError(t, openErr)

	t.Cleanup(func() { master.Close() })

	var ln0, port0 = testutils.Listen(t)

	var p = testutils.StartMain(t, "127.0.0.1:"+port0+"=Caller", slave.Name()+"=Answerer")

	testutils.Accept(t, ln0)

	assert.Equal(t, "\003\rreset\recho on\rmycall TNC1\rflow off\recho off\r", readUntil(t, master, "echo off\r"))

	p.WaitFor(t, "TNC 1 now available.  Answerer on "+slave.Name())

	// The command has the port open now, so it goes on without ours.
	require.NoError(t, slave.Close())

	// XOFF and XON, a blank line, and something unprintable along with the
	// data.
	var _, writeErr = master.WriteString("\x13\x11\r\n0001 send data\x01\r")
	require.NoError(t, writeErr)

	p.WaitFor(t, "<XOFF>")
	p.WaitFor(t, "<XON>")
	p.WaitFor(t, "0001 send data<x01>")

	assert.Equal(t, "0001 reply\r", readUntil(t, master, "\r"))

	require.NoError(t, master.Close())

	p.WaitFor(t, "TNC 1 fatal read error")
	assert.Equal(t, 1, p.Wait())
}

// While the TNC says it is busy, data waits until it isn't.
func Test_tnc_send_data_busy(t *testing.T) {
	resetState(t)

	var tnc = serialTNC(t, 0)

	busy[0].Store(true)

	go func() {
		direwolf.SLEEP_MS(150)
		busy[0].Store(false)
	}()

	tnc_send_data(0, 1, "0001 send data\r")
	assert.Equal(t, "0001 send data\r", readUntil(t, tnc, "\r"))
}
