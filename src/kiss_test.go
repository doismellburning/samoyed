// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Only where the terminal settings ioctls below are spelled the way
// termios_linux_test.go and termios_darwin_test.go spell them, which is every
// platform this is built for.
//go:build linux || darwin

package direwolf

// The pseudo terminal KISS TNC is what "direwolf -p" offers, and what
// kissattach, and anything else that wants a serial TNC without the serial
// port, attaches to.  A pty pair is a loopback: the test writes to the far end
// and reads back what the TNC sent, so none of this needs any hardware.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// rawTerminal takes the line discipline out of the way of a test's bytes.
// A pseudo terminal starts in canonical mode with echo and newline
// translation on, all of which mangles binary KISS framing; kissattach puts
// the slave into raw mode for the same reason.
func rawTerminal(t *testing.T, f *os.File) {
	t.Helper()

	// Through SyscallConn rather than Fd, which would take the terminal out
	// of the runtime poller and with it the read deadlines these tests rely
	// on not to hang.
	var conn, connErr = f.SyscallConn()
	require.NoError(t, connErr)

	var ioctlErr error

	require.NoError(t, conn.Control(func(fd uintptr) {
		var termios, getErr = unix.IoctlGetTermios(int(fd), tcGetAttr)
		if getErr != nil {
			ioctlErr = getErr

			return
		}

		termios.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
			unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
		termios.Oflag &^= unix.OPOST
		termios.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
		termios.Cflag &^= unix.CSIZE | unix.PARENB
		termios.Cflag |= unix.CS8

		ioctlErr = unix.IoctlSetTermios(int(fd), tcSetAttr, termios)
	}))

	require.NoError(t, ioctlErr)
}

// startKissPTListener does what kisspt_init does for an enabled pseudo
// terminal - open it and run the listening goroutine against ctx - but hands
// back a channel that is closed once that goroutine has finished.
//
// The terminal lives in package globals which the goroutine clears on its way
// out, so a test has to be able to tell when it has gone before putting them
// back; waiting on the channel is also the happens-before edge that keeps the
// race detector quiet.
func startKissPTListener(ctx context.Context, t *testing.T) (*os.File, <-chan struct{}) {
	t.Helper()

	kisspt_kf = new(KISSFrame)

	kisspt_open_pt()

	require.NotNil(t, pt_master, "no pseudo terminal was opened for the KISS TNC")

	rawTerminal(t, pt_slave)

	var done = make(chan struct{})

	go func() {
		defer close(done)

		kisspt_listen_thread(ctx)
	}()

	return pt_slave, done
}

// startKissPT brings up the pseudo terminal KISS TNC, with its listening
// goroutine running until the test ends, and hands back the far end of the
// pseudo terminal - what a client application would open.
//
// Everything here lives in package globals, so they are saved and put back
// once the goroutine has stopped, however the test finishes.
func startKissPT(t *testing.T) *os.File {
	t.Helper()

	var origMaster, origSlave, origFrame, origDebug = pt_master, pt_slave, kisspt_kf, kisspt_debug

	var ctx, cancel = context.WithCancel(t.Context())

	var client, done = startKissPTListener(ctx, t)

	t.Cleanup(func() {
		cancel()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("kisspt_listen_thread did not finish after its context was cancelled")
		}

		pt_master, pt_slave, kisspt_kf, kisspt_debug = origMaster, origSlave, origFrame, origDebug
	})

	return client
}

// readKissFrame reads one whole KISS frame - everything up to and including
// the closing FEND - from the client end of the pseudo terminal.
func readKissFrame(t *testing.T, client *os.File) []byte {
	t.Helper()

	require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))

	var frame []byte

	var buf = make([]byte, 1)

	for {
		var n, err = client.Read(buf)
		require.NoError(t, err, "gave up waiting for a KISS frame; got %v so far", frame)

		if n == 0 {
			continue
		}

		frame = append(frame, buf[0])

		// The leading FEND does not end anything, so only count one that
		// has something in front of it.
		if buf[0] == FEND && len(frame) > 1 {
			return frame
		}
	}
}

// drainKissFrame reads one whole KISS frame from the client end of a pseudo
// terminal, in the background, so that a send bigger than the terminal will
// hold can get on with it.
//
// macOS gives a pseudo terminal about a kilobyte of buffer, where Linux gives
// several, so a longer write blocks part-way through until the far end is
// drained.  Reading after the send returned would mean the two waiting for
// each other, which is exactly how the truncation test hung on macOS and not
// here.
//
// Nothing in the goroutine fails the test: t.FailNow may only be called from
// the goroutine running the test, so a read that goes wrong arrives as a short
// frame for the caller to assert on.
func drainKissFrame(t *testing.T, client *os.File) <-chan []byte {
	t.Helper()

	require.NoError(t, client.SetReadDeadline(time.Now().Add(20*time.Second)))

	var frame = make(chan []byte, 1)

	go func() {
		var got []byte

		var buf = make([]byte, 256)

		for {
			var n, err = client.Read(buf)

			got = append(got, buf[:n]...)

			// The leading FEND does not end anything, so only stop on one
			// with something in front of it.
			if err != nil || (len(got) > 1 && got[len(got)-1] == FEND) {
				break
			}
		}

		frame <- got
	}()

	return frame
}

// readKissText reads want bytes of the plain text the TNC sends when it is
// humouring a client that thinks it is talking to a command-mode TNC.
func readKissText(t *testing.T, client *os.File, want int) string {
	t.Helper()

	require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))

	var got = make([]byte, 0, want)

	for len(got) < want {
		var buf = make([]byte, want-len(got))

		var n, err = client.Read(buf)
		require.NoError(t, err, "gave up waiting for text; got %q so far", got)

		got = append(got, buf[:n]...)
	}

	return string(got)
}

// A TNC nobody asked for should not be there: without -p there is no pseudo
// terminal, and nothing for the sending path to write to.
func TestKissPTNotEnabled(t *testing.T) {
	var origMaster, origFrame = pt_master, kisspt_kf

	t.Cleanup(func() { pt_master, kisspt_kf = origMaster, origFrame })

	kisspt_init(t.Context(), new(misc_config_s))

	assert.Nil(t, pt_master, "a pseudo terminal was opened although KISS pt was not enabled")
	assert.NotNil(t, kisspt_kf, "the frame decoder state should be ready even with no terminal")

	// With no terminal, sending to the client is a no-op rather than a crash.
	assert.NotPanics(t, func() {
		kisspt_send_rec_packet(0, KISS_CMD_DATA_FRAME, []byte("nowhere to go"), 13, nil, -1)
	})
}

// The device name of a pseudo terminal is different every time, which would
// otherwise mean editing the client application's configuration on every
// restart.  The symlink is what saves that, so it has to point at the terminal
// we actually opened.
func TestKissPTSymlinkPointsAtTheTerminal(t *testing.T) {
	var client = startKissPT(t)

	var target, err = os.Readlink(TMP_KISSTNC_SYMLINK)
	require.NoError(t, err, "no symlink was created for the KISS TNC")

	assert.Equal(t, client.Name(), target)
}

// A frame received over the radio reaches the client as KISS: the channel and
// command in the first byte, then the frame, wrapped in FENDs.
func TestKissPTSendRecPacket(t *testing.T) {
	var client = startKissPT(t)

	const channel = 3

	var frame = []byte("some received frame")

	kisspt_send_rec_packet(channel, KISS_CMD_DATA_FRAME, frame, len(frame), nil, -1)

	var want = KissEncapsulate(append([]byte{byte(channel<<4 | KISS_CMD_DATA_FRAME)}, frame...))

	assert.Equal(t, want, readKissFrame(t, client))
}

// FEND and FESC in the frame contents would otherwise look like framing to the
// client, so they are escaped on the way out.
func TestKissPTSendRecPacketEscapes(t *testing.T) {
	var client = startKissPT(t)

	var frame = []byte{FEND, 'a', FESC, 'b'}

	kisspt_send_rec_packet(0, KISS_CMD_DATA_FRAME, frame, len(frame), nil, -1)

	assert.Equal(t,
		[]byte{FEND, 0x00, FESC, TFEND, 'a', FESC, TFESC, 'b', FEND},
		readKissFrame(t, client))
}

// A length of -1 says the caller has built the bytes itself - the fake command
// prompt - and they go out as they are, with no framing or escaping added.
func TestKissPTSendRecPacketText(t *testing.T) {
	var client = startKissPT(t)

	kisspt_send_rec_packet(0, 0, []byte("\r\ncmd:"), -1, nil, -1)

	assert.Equal(t, "\r\ncmd:", readKissText(t, client, len("\r\ncmd:")))
}

// A frame longer than AX.25 allows is truncated rather than sent on, and the
// user is told, because silently passing it would hand the client something it
// cannot parse.
func TestKissPTSendRecPacketTruncates(t *testing.T) {
	var client = startKissPT(t)

	var frame = make([]byte, AX25_MAX_PACKET_LEN+10)
	for i := range frame {
		frame[i] = 'x'
	}

	// Draining has to be under way before the send, not after it: the frame
	// is longer than the terminal will hold.
	var got = drainKissFrame(t, client)

	var output = CaptureOutput(t, func() {
		kisspt_send_rec_packet(0, KISS_CMD_DATA_FRAME, frame, len(frame), nil, -1)
	})

	assert.Contains(t, output, "Truncated")

	// FEND, the type indicator, the frame, FEND - and 'x' needs no escaping.
	assert.Len(t, <-got, AX25_MAX_PACKET_LEN+3)
}

// The whole point of the pseudo terminal: a client writes a KISS data frame
// into it and the frame is queued for transmission on the channel the KISS
// frame names.
func TestKissPTClientFrameIsQueuedForTransmission(t *testing.T) {
	const channel = 0

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[channel] = MEDIUM_RADIO

	var origAudioConfig, origXmitSvc, origKissNetSvc = save_audio_config_p, xmitSvc, kissNetSvc

	t.Cleanup(func() { save_audio_config_p, xmitSvc, kissNetSvc = origAudioConfig, origXmitSvc, origKissNetSvc })

	kiss_frame_init(audioConfig)

	xmitSvc = new(XmitService)
	kissNetSvc = NewKissNetService(t.Context(), new(misc_config_s))

	tq_init(t.Context(), audioConfig)

	t.Cleanup(func() {
		for tq_remove(channel, TQ_PRIO_1_LO) != nil { //revive:disable-line:empty-block
		}
	})

	var client = startKissPT(t)

	var pp = newTestPacket(t)

	var kissFrame = KissEncapsulate(append(
		[]byte{byte(channel<<4 | KISS_CMD_DATA_FRAME)}, ax25_get_frame_data(pp)...))

	var _, writeErr = client.Write(kissFrame)
	require.NoError(t, writeErr)

	// tq_count rather than tq_peek: the queue is being filled by the
	// listening goroutine, and only tq_count reads it under the lock.
	assert.Eventually(t, func() bool {
		return tq_count(channel, TQ_PRIO_1_LO, "", "", false) > 0
	}, 5*time.Second, 10*time.Millisecond, "the frame from the KISS client was not queued for transmission")
}

// An application that thinks it is driving an old command-mode TNC sends
// things like "KISS ON\r" over and over until it gets an answer.  Answering
// with a command prompt is what stops it.
func TestKissPTAnswersCommandModeNoise(t *testing.T) {
	var client = startKissPT(t)

	var _, writeErr = client.WriteString("KISS ON\r")
	require.NoError(t, writeErr)

	assert.Equal(t, "\r\ncmd:", readKissText(t, client, len("\r\ncmd:")))
}

// "RESTART" is answered with a pair of FENDs instead - an empty KISS frame -
// because that is what the applications sending it are waiting for.
func TestKissPTAnswersRestart(t *testing.T) {
	var client = startKissPT(t)

	var _, writeErr = client.WriteString("restart\r")
	require.NoError(t, writeErr)

	assert.Equal(t, "\xc0\xc0", readKissText(t, client, 2))
}

// The TNC goes away with the rest of the program: a cancelled context closes
// the pseudo terminal and forgets it, rather than leaving a goroutine blocked
// on a read of it forever.
//
// This is the regression test for the terminal being opened in blocking mode.
// pty.Open's ioctls go through (*os.File).Fd(), which leaves the master's
// descriptor blocking and outside the runtime poller, and closing such a
// descriptor does not interrupt a read already waiting on it - so the
// listening goroutine below stayed in its read for the life of the process,
// however the context was cancelled.
func TestKissPTStopsWhenCancelled(t *testing.T) {
	var origMaster, origSlave, origFrame = pt_master, pt_slave, kisspt_kf

	var ctx, cancel = context.WithCancel(t.Context())

	var client, done = startKissPTListener(ctx, t)

	t.Cleanup(func() { pt_master, pt_slave, kisspt_kf = origMaster, origSlave, origFrame })

	cancel()

	// Nothing obliges a client to send anything, so the listening goroutine
	// is sitting in a read that only closing the terminal ends.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("kisspt_listen_thread did not finish after its context was cancelled")
	}

	assert.Nil(t, pt_master, "the pseudo terminal was still open after cancellation")

	// And the client's end goes with it.
	require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))

	var _, readErr = client.Read(make([]byte, 1))
	assert.Error(t, readErr, "the client end of the pseudo terminal was not closed")
}

// closeKissPT is called from more than one place on the way out - the
// listening goroutine has it registered twice over - so it has to cope with
// the terminal already being gone.
func TestCloseKissPTTwice(t *testing.T) {
	var origMaster, origSlave = pt_master, pt_slave

	t.Cleanup(func() {
		closeKissPT()

		pt_master, pt_slave = origMaster, origSlave
	})

	// Opened without kisspt_init, so there is no goroutine reading from the
	// terminal we are closing underneath it.
	kisspt_open_pt()
	require.NotNil(t, pt_master)

	closeKissPT()
	assert.Nil(t, pt_master)

	assert.NotPanics(t, closeKissPT)

	// The symlink is a promise that there is a TNC on the other end, so it
	// goes when the terminal does.
	var _, statErr = os.Lstat(TMP_KISSTNC_SYMLINK)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "the symlink outlived the pseudo terminal")
}

// A client that closes its end leaves the terminal unusable - a read of the
// master gets EIO once no process holds the slave - so the TNC gives it up
// rather than spinning on an error forever.
func TestKissPTClientHangingUpClosesTheTerminal(t *testing.T) {
	var origMaster, origSlave, origFrame = pt_master, pt_slave, kisspt_kf

	var client, done = startKissPTListener(t.Context(), t)

	t.Cleanup(func() { pt_master, pt_slave, kisspt_kf = origMaster, origSlave, origFrame })

	require.NoError(t, client.Close())

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("kisspt_listen_thread did not finish after the client closed the terminal")
	}

	assert.Nil(t, pt_master, "the pseudo terminal was not given up after the client went away")

	var _, statErr = os.Lstat(TMP_KISSTNC_SYMLINK)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "the symlink outlived the pseudo terminal")
}

// With "-d k" the traffic in both directions is printed, so that a client
// application that is not being understood can be looked at.
func TestKissPTDebugPrintsBothDirections(t *testing.T) {
	var client = startKissPT(t)

	kisspt_set_debug(2)

	var output = CaptureOutput(t, func() {
		kisspt_send_rec_packet(1, KISS_CMD_DATA_FRAME, []byte("hello"), 5, nil, -1)

		readKissFrame(t, client)
	})

	assert.Contains(t, output, "Packet content before adding KISS framing")
	assert.Contains(t, output, ">>> Data frame to KISS client application, channel 1")

	// And the fake command prompt, which is not a KISS frame at all, says so.
	output = CaptureOutput(t, func() {
		kisspt_send_rec_packet(0, 0, []byte("\r\ncmd:"), -1, nil, -1)

		readKissText(t, client, len("\r\ncmd:"))
	})

	assert.Contains(t, output, "Fake command prompt")
}

func TestKissPTSetDebug(t *testing.T) {
	var orig = kisspt_debug

	t.Cleanup(func() { kisspt_debug = orig })

	kisspt_set_debug(2)

	assert.Equal(t, 2, kisspt_debug)
}
