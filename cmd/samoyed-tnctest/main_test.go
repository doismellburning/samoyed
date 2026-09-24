// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux || darwin

package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/pkg/term"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		usingTCP   [MAX_TNC]bool
		sock       [MAX_TNC]net.Conn
		serial     [MAX_TNC]*term.Term
		busy       [MAX_TNC]bool
		address    [MAX_TNC]string
		prompt     [MAX_TNC]bool
		lastRecSeq [MAX_TNC]int
		width      int
	}{
		tnctest_using_tcp, tnctest_server_sock, tnctest_serial_fd, busy,
		tnc_address, have_cmd_prompt, last_rec_seq, column_width,
	}

	tnc_address = [MAX_TNC]string{"DW0", "DW1"}

	t.Cleanup(func() {
		tnctest_using_tcp, tnctest_server_sock, tnctest_serial_fd, busy = saved.usingTCP, saved.sock, saved.serial, saved.busy
		tnc_address, have_cmd_prompt, last_rec_seq, column_width = saved.address, saved.prompt, saved.lastRecSeq, saved.width
	})
}

func Test_process_rec_data(t *testing.T) {
	resetState(t)

	// The answering end counts what is sent to it...
	process_rec_data(1, "0001 send data\r")
	process_rec_data(1, "0002 send data\r")
	assert.Equal(t, 2, last_rec_seq[1])

	// ...and the calling end counts the replies.
	process_rec_data(0, "0001 reply\r")
	assert.Equal(t, 1, last_rec_seq[0])

	// Each only counts its own half of the conversation.
	process_rec_data(0, "0003 send data\r")
	process_rec_data(1, "0002 reply\r")
	assert.Equal(t, 1, last_rec_seq[0])
	assert.Equal(t, 2, last_rec_seq[1])

	// Pieces of the alphabet test segmentation, and don't count.
	process_rec_data(0, "ABCDE\r")
	assert.Equal(t, 1, last_rec_seq[0])

	assert.Panics(t, func() { process_rec_data(0, "Something else") })
}

// agwConn points TNC from at one end of a TCP connection and hands back the
// other, where the test plays the TNC.
func agwConn(t *testing.T, from int) net.Conn {
	t.Helper()

	var ln, listenErr = new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, listenErr)

	defer ln.Close()

	var conn, dialErr = new(net.Dialer).DialContext(t.Context(), "tcp", ln.Addr().String())
	require.NoError(t, dialErr)

	var tnc, acceptErr = ln.Accept()
	require.NoError(t, acceptErr)

	t.Cleanup(func() {
		conn.Close()
		tnc.Close()
	})

	tnctest_using_tcp[from] = true
	tnctest_server_sock[from] = conn

	require.NoError(t, tnc.SetReadDeadline(time.Now().Add(5*time.Second)))

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

	have_cmd_prompt[0] = true

	tnc_connect(0, 1)
	assert.Equal(t, "connect TNC1\r", readUntil(t, tnc, "\r"))

	tnc_send_data(0, 1, "0001 send data\r")
	assert.Equal(t, "0001 send data\r", readUntil(t, tnc, "\r"))

	tnc_disconnect(0, 1)
	assert.Equal(t, "disconnect\r", readUntil(t, tnc, "\r"))
}

// buildTnctest builds the command under test.  main takes hours over a full
// run, and its TNC goroutines exit the process when a TNC goes away, so it
// runs on its own and is killed once it has got as far as a test needs.
func buildTnctest(t *testing.T) string {
	t.Helper()

	var binary = filepath.Join(t.TempDir(), "samoyed-tnctest")

	var build = exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".") //nolint:gosec

	var out, err = build.CombinedOutput()
	require.NoError(t, err, "Building the command failed: %s", out)

	return binary
}

// fakeAGW listens as an AGW TNC would, and hands back its address along with
// a channel that delivers the connection once one has been made.
func fakeAGW(t *testing.T) (string, <-chan net.Conn) {
	t.Helper()

	var ln, listenErr = new(net.ListenConfig).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	require.NoError(t, listenErr)

	t.Cleanup(func() { ln.Close() })

	var conns = make(chan net.Conn, 1)

	go func() {
		var conn, err = ln.Accept()
		if err != nil {
			return
		}

		t.Cleanup(func() { conn.Close() })

		conns <- conn
	}()

	return ln.Addr().String(), conns
}

func accept(t *testing.T, conns <-chan net.Conn) net.Conn {
	t.Helper()

	select {
	case conn := <-conns:
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))

		return conn
	case <-time.After(10 * time.Second):
		require.FailNow(t, "timed out waiting for a TNC connection")

		return nil
	}
}

// waitFor reads lines until one contains want.
func waitFor(t *testing.T, lines <-chan string, want string) {
	t.Helper()

	var timeout = time.After(10 * time.Second)

	for {
		select {
		case line, ok := <-lines:
			require.True(t, ok, "output ended without %q", want)

			if strings.Contains(line, want) {
				return
			}
		case <-timeout:
			require.FailNow(t, "timed out waiting for output", "wanted %q", want)
		}
	}
}

func Test_main_connects(t *testing.T) {
	var executable = buildTnctest(t)

	var addr0, conns0 = fakeAGW(t)
	var addr1, conns1 = fakeAGW(t)

	var ctx, cancel = context.WithCancel(context.Background())

	var cmd = exec.CommandContext(ctx, executable, addr0+"=Caller", addr1+"=Answerer") //nolint:gosec

	var stdout, stdoutErr = cmd.StdoutPipe()
	require.NoError(t, stdoutErr)

	require.NoError(t, cmd.Start())

	var lines = make(chan string, 100)

	go func() {
		defer close(lines)

		var scanner = bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	t.Cleanup(func() {
		cancel()

		for range lines { // Drain what's left so Wait can finish.
		}

		_ = cmd.Wait()
	})

	var tnc0 = accept(t, conns0)
	var tnc1 = accept(t, conns1)

	// Each TNC is asked for raw frames and to register its callsign.
	for i, tnc := range []net.Conn{tnc0, tnc1} {
		assert.Equal(t, byte('k'), readHeader(t, tnc).DataKind)

		var register = readHeader(t, tnc)
		assert.Equal(t, byte('X'), register.DataKind)
		assert.Equal(t, []string{"DW0", "DW1"}[i], callsign(register.CallFrom))
	}

	waitFor(t, lines, "Andiamo!")

	// The first then calls the second.
	var connect = readHeader(t, tnc0)
	assert.Equal(t, byte('C'), connect.DataKind)
	assert.Equal(t, "DW0", callsign(connect.CallFrom))
	assert.Equal(t, "DW1", callsign(connect.CallTo))

	// The first end reports the connection.  (The callsign is printed with
	// the NUL padding of its fixed-width field, hence not matching the
	// whole line.)
	var connected = new(direwolf.AGWPEHeader)
	connected.DataKind = 'C'
	copy(connected.CallFrom[:], "DW1")

	require.NoError(t, binary.Write(tnc0, binary.LittleEndian, connected))

	waitFor(t, lines, "*** Connected to DW1")
}

func Test_main_badArguments(t *testing.T) {
	var executable = buildTnctest(t)

	var ln, listenErr = new(net.ListenConfig).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	require.NoError(t, listenErr)

	var gone = ln.Addr().String()

	// Nothing is listening once this is closed.
	require.NoError(t, ln.Close())

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
			var out, err = exec.CommandContext(t.Context(), executable, tc.args...).Output() //nolint:gosec

			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			assert.Equal(t, 1, exitErr.ExitCode())
			assert.Contains(t, string(out), tc.want)
		})
	}
}
