// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// The same two platforms as kiss_test.go, whose drainKissFrame this uses.
//go:build linux || darwin

package direwolf

// KISS over a serial port is how a client application on another machine, or
// over Bluetooth, talks to us.  A pseudo terminal stands in for the wire, as it
// does in serial_port_test.go, so none of this needs any hardware.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestSerialDevice hands back the name of something that behaves like a
// serial port - the far side of a pseudo terminal - along with the near side,
// which is where a test plays the client application.
func newTestSerialDevice(t *testing.T) (string, *os.File) {
	t.Helper()

	var master, slave, openErr = pty.Open()
	require.NoError(t, openErr)

	var name = slave.Name()

	// SerialPortOpen opens the device by name, so the handle here is surplus;
	// the terminal lives on as long as the master is open.
	require.NoError(t, slave.Close())

	// The master comes out of pty.Open in blocking mode and outside the
	// runtime poller, which would mean no read deadlines and a test that
	// hangs rather than fails.
	var client, pollableErr = pollable(master)
	require.NoError(t, pollableErr)

	t.Cleanup(func() { client.Close() })

	return name, client
}

// startKissSerial does what kissserial_init does for a configured serial port
// - open it, if it is not the polling case, and run the listening goroutine
// against ctx - but hands back a channel that is closed once that goroutine
// has finished.
//
// The port cannot be closed out from under a blocked read, so the goroutine
// ends when the far end of the wire goes away.  The globals it clears on the
// way out are put back only once it has, which is also the happens-before edge
// the race detector wants.
func startKissSerial(ctx context.Context, t *testing.T, mc *misc_config_s) <-chan struct{} {
	t.Helper()

	var origConfig, origFrame, origFD, origDebug = g_misc_config_p, kf, serialport_fd, kissserial_debug

	g_misc_config_p = mc
	kf = new(KISSFrame)
	serialport_fd = nil

	if mc.kiss_serial_poll == 0 {
		serialport_fd = SerialPortOpen(mc.kiss_serial_port, mc.kiss_serial_speed)
		require.NotNil(t, serialport_fd, "could not open %s", mc.kiss_serial_port)
	}

	var done = make(chan struct{})

	go func() {
		defer close(done)

		kissserial_listen_thread(ctx)
	}()

	t.Cleanup(func() {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("kissserial_listen_thread did not finish")
		}

		g_misc_config_p, kf, serialport_fd, kissserial_debug = origConfig, origFrame, origFD, origDebug
	})

	return done
}

// openKissSerialPort opens the serial port KISS TNC's port on a pseudo
// terminal and hands back the client end of the wire, with no listening
// goroutine running.
//
// The sending path and that goroutine share the port handle with no
// synchronisation - the goroutine gives the port up on a read error while the
// receive path may be part-way through writing to it - so a test of one keeps
// away from the other.
func openKissSerialPort(t *testing.T) *os.File {
	t.Helper()

	var name, client = newTestSerialDevice(t)

	var origConfig, origFrame, origFD, origDebug = g_misc_config_p, kf, serialport_fd, kissserial_debug

	t.Cleanup(func() {
		closeSerialPortKISS()

		g_misc_config_p, kf, serialport_fd, kissserial_debug = origConfig, origFrame, origFD, origDebug
	})

	g_misc_config_p = new(misc_config_s)
	g_misc_config_p.kiss_serial_port = name
	kf = new(KISSFrame)

	serialport_fd = SerialPortOpen(name, 0)
	require.NotNil(t, serialport_fd, "could not open %s", name)

	return client
}

// readSerialKissFrame reads one whole KISS frame - everything up to and
// including the closing FEND - from the client end of the wire.
func readSerialKissFrame(t *testing.T, client *os.File) []byte {
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

		if buf[0] == FEND && len(frame) > 1 {
			return frame
		}
	}
}

// readSerialText reads want bytes of the plain text the TNC sends when it is
// humouring a client that thinks it is talking to a command-mode TNC.
func readSerialText(t *testing.T, client *os.File, want int) string {
	t.Helper()

	require.NoError(t, client.SetReadDeadline(time.Now().Add(10*time.Second)))

	var got = make([]byte, 0, want)

	for len(got) < want {
		var buf = make([]byte, want-len(got))

		var n, err = client.Read(buf)
		require.NoError(t, err, "gave up waiting for text; got %q so far", got)

		got = append(got, buf[:n]...)
	}

	return string(got)
}

// Without KISSPORT in the configuration there is no serial TNC at all, and
// nothing for the sending path to write to.
func TestKissSerialNoPortConfigured(t *testing.T) {
	var origConfig, origFrame, origFD = g_misc_config_p, kf, serialport_fd

	t.Cleanup(func() { g_misc_config_p, kf, serialport_fd = origConfig, origFrame, origFD })

	serialport_fd = nil

	kissserial_init(t.Context(), new(misc_config_s))

	assert.Nil(t, serialport_fd)
	assert.NotNil(t, kf, "the frame decoder state should be ready even with no port")

	// With no port, sending to the client is a no-op rather than a crash.
	assert.NotPanics(t, func() {
		kissserial_send_rec_packet(0, KISS_CMD_DATA_FRAME, []byte("nowhere to go"), 13, nil, -1)
	})
}

// A device that is not there, and no polling asked for, is reported once and
// then left alone - there is no listening goroutine to start.
func TestKissSerialDeviceNotThere(t *testing.T) {
	var origConfig, origFrame, origFD = g_misc_config_p, kf, serialport_fd

	t.Cleanup(func() { g_misc_config_p, kf, serialport_fd = origConfig, origFrame, origFD })

	serialport_fd = nil

	var mc = new(misc_config_s)
	mc.kiss_serial_port = "/dev/there-is-no-such-serial-port"

	var output = CaptureOutput(t, func() { kissserial_init(t.Context(), mc) })

	assert.Contains(t, output, "Could not open serial port /dev/there-is-no-such-serial-port")
	assert.Nil(t, serialport_fd)
}

// A frame received over the radio reaches the client as KISS: the channel and
// command in the first byte, then the frame, wrapped in FENDs.
func TestKissSerialSendRecPacket(t *testing.T) {
	var client = openKissSerialPort(t)

	const channel = 2

	var frame = []byte{'h', 'i', FEND, FESC}

	kissserial_send_rec_packet(channel, KISS_CMD_DATA_FRAME, frame, len(frame), nil, -1)

	assert.Equal(t,
		[]byte{FEND, channel << 4, 'h', 'i', FESC, TFEND, FESC, TFESC, FEND},
		readSerialKissFrame(t, client))
}

// A port that has gone away cannot be written to, so it is given up rather
// than written to again on the next received frame.
func TestKissSerialSendRecPacketWriteErrorClosesThePort(t *testing.T) {
	var client = openKissSerialPort(t)

	require.NoError(t, client.Close())

	var output = CaptureOutput(t, func() {
		kissserial_send_rec_packet(0, KISS_CMD_DATA_FRAME, []byte("hello"), 5, nil, -1)
	})

	assert.Contains(t, output, "Error sending KISS message to client application thru serial port")
	assert.Nil(t, serialport_fd, "the serial port was not given up after the write error")
}

// A length of -1 says the caller has built the bytes itself - the fake command
// prompt - and they go out as they are, with no framing or escaping added.
func TestKissSerialSendRecPacketText(t *testing.T) {
	var client = openKissSerialPort(t)

	kissserial_send_rec_packet(0, 0, []byte("\r\ncmd:"), -1, nil, -1)

	assert.Equal(t, "\r\ncmd:", readSerialText(t, client, len("\r\ncmd:")))
}

// A frame longer than AX.25 allows is truncated rather than sent on: a client
// that is told the frame was truncated and then handed the whole thing anyway
// has been told a lie about bytes it cannot parse.
func TestKissSerialSendRecPacketTruncates(t *testing.T) {
	var client = openKissSerialPort(t)

	var frame = make([]byte, AX25_MAX_PACKET_LEN+10)
	for i := range frame {
		frame[i] = 'x'
	}

	// Draining has to be under way before the send, not after it: the frame
	// is longer than the terminal will hold.
	var got = drainKissFrame(t, client)

	var output = CaptureOutput(t, func() {
		kissserial_send_rec_packet(0, KISS_CMD_DATA_FRAME, frame, len(frame), nil, -1)
	})

	assert.Contains(t, output, "Truncated")

	// FEND, the type indicator, the frame, FEND - and 'x' needs no escaping.
	assert.Len(t, <-got, AX25_MAX_PACKET_LEN+3)
}

// The whole point of the serial port: a client writes a KISS data frame into
// it and the frame is queued for transmission on the channel the KISS frame
// names.
func TestKissSerialClientFrameIsQueuedForTransmission(t *testing.T) {
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

	var name, client = newTestSerialDevice(t)

	var mc = new(misc_config_s)
	mc.kiss_serial_port = name

	startKissSerial(t.Context(), t, mc)

	var pp = newTestPacket(t)

	var _, writeErr = client.Write(KissEncapsulate(append(
		[]byte{byte(channel<<4 | KISS_CMD_DATA_FRAME)}, ax25_get_frame_data(pp)...)))
	require.NoError(t, writeErr)

	// tq_count rather than tq_peek: the queue is being filled by the
	// listening goroutine, and only tq_count reads it under the lock.
	assert.Eventually(t, func() bool {
		return tq_count(channel, TQ_PRIO_1_LO, "", "", false) > 0
	}, 5*time.Second, 10*time.Millisecond, "the frame from the KISS client was not queued for transmission")

	require.NoError(t, client.Close())
}

// An application that thinks it is driving an old command-mode TNC sends
// things like "KISS ON\r" over and over until it gets an answer.  Answering
// with a command prompt is what stops it.
func TestKissSerialAnswersCommandModeNoise(t *testing.T) {
	var name, client = newTestSerialDevice(t)

	var mc = new(misc_config_s)
	mc.kiss_serial_port = name

	startKissSerial(t.Context(), t, mc)

	var _, writeErr = client.WriteString("KISS ON\r")
	require.NoError(t, writeErr)

	assert.Equal(t, "\r\ncmd:", readSerialText(t, client, len("\r\ncmd:")))

	require.NoError(t, client.Close())
}

// The far end going away leaves a port that can only report errors, so it is
// given up rather than read from forever.
func TestKissSerialReadErrorClosesThePort(t *testing.T) {
	var name, client = newTestSerialDevice(t)

	var mc = new(misc_config_s)
	mc.kiss_serial_port = name

	var done = startKissSerial(t.Context(), t, mc)

	require.NoError(t, client.Close())

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("kissserial_listen_thread did not finish after the client went away")
	}

	assert.Nil(t, serialport_fd, "the serial port was not given up after the read error")
}

// Bluetooth is the reason for the polling option: the device turns up when the
// far end connects, some time after we started, and we are expected to notice.
func TestKissSerialPollsForTheDeviceToAppear(t *testing.T) {
	var device = filepath.Join(t.TempDir(), "rfcomm0")

	var mc = new(misc_config_s)
	mc.kiss_serial_port = device
	mc.kiss_serial_poll = 1

	var ctx, cancel = context.WithCancel(t.Context())

	var done = startKissSerial(ctx, t, mc)

	var name, client = newTestSerialDevice(t)
	require.NoError(t, os.Symlink(name, device))

	// A client that thinks it is talking to a command-mode TNC asks over and
	// over until it gets an answer, which is also what keeps this test from
	// depending on exactly when the polling notices the device - or on
	// anything written before it did having been kept.
	var heard []byte

	require.Eventually(t, func() bool {
		// Both deadlines: nothing reads the far end until the device is
		// noticed, and a pseudo terminal holds only so much - about a
		// kilobyte on macOS - so the write has to be able to give up too.
		var deadlineErr = client.SetDeadline(time.Now().Add(500 * time.Millisecond))
		if deadlineErr != nil {
			return false
		}

		var _, writeErr = client.WriteString("KISS ON\r")
		if writeErr != nil {
			return false
		}

		var buf = make([]byte, 64)

		var n, _ = client.Read(buf)
		heard = append(heard, buf[:n]...)

		return bytes.Contains(heard, []byte("cmd:"))
	}, 30*time.Second, 100*time.Millisecond, "the device appearing went unnoticed")

	// The cancellation alone would not get the goroutine back: it spends its
	// time in a blocking read of the port, which nothing can interrupt.  The
	// client going away ends that read; the polling case then goes back to
	// waiting for the device to reappear, which is what the cancellation
	// stops.
	require.NoError(t, client.Close())

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the polling loop did not finish after its context was cancelled")
	}
}

// Nothing has turned up and nothing ever will: the polling loop waits between
// looks rather than spinning, and a cancellation cuts that wait short.
func TestKissSerialPollingStopsWhenCancelled(t *testing.T) {
	var mc = new(misc_config_s)
	mc.kiss_serial_port = filepath.Join(t.TempDir(), "never-appears")
	mc.kiss_serial_poll = 1

	var ctx, cancel = context.WithCancel(t.Context())

	var done = startKissSerial(ctx, t, mc)

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the polling loop did not finish after its context was cancelled")
	}
}

// With "-d k" the traffic is printed, so that a client application that is not
// being understood can be looked at.
func TestKissSerialDebugPrints(t *testing.T) {
	var client = openKissSerialPort(t)

	kissserial_set_debug(2)

	var output = CaptureOutput(t, func() {
		kissserial_send_rec_packet(1, KISS_CMD_DATA_FRAME, []byte("hello"), 5, nil, -1)

		readSerialKissFrame(t, client)
	})

	assert.Contains(t, output, "Packet content before adding KISS framing")
	assert.Contains(t, output, ">>> Data frame to KISS client application, channel 1")

	output = CaptureOutput(t, func() {
		kissserial_send_rec_packet(0, 0, []byte("\r\ncmd:"), -1, nil, -1)
	})

	assert.Contains(t, output, "Fake command prompt")
}
