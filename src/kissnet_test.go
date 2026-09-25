// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

// KISS over TCP is how Xastir, APRSIS32, pat and friends attach, and it is all
// loopback: bind a listener, dial it, and what goes in one end comes out the
// other.
//
// The sending side is tested against a service assembled by hand, with its
// clients already attached, rather than one with listening goroutines running.
// Those goroutines outlive the test that started them - there is nothing to
// wait on - so a test of the sending path keeps clear of them.

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connectedTCPPair returns the two ends of a TCP connection: a real socket
// pair rather than net.Pipe, whose synchronous writes would block the sending
// path until somebody read.
func connectedTCPPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()

	var listener, listenErr = new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, listenErr)

	defer listener.Close()

	type accepted struct {
		conn net.Conn
		err  error
	}

	var done = make(chan accepted, 1)

	go func() {
		var conn, err = listener.Accept()

		done <- accepted{conn: conn, err: err}
	}()

	var here, dialErr = new(net.Dialer).DialContext(t.Context(), "tcp", listener.Addr().String())
	require.NoError(t, dialErr)

	var there = <-done
	require.NoError(t, there.err)

	t.Cleanup(func() {
		here.Close()
		there.conn.Close()
	})

	return there.conn, here
}

// newAttachedKissNet assembles a KISS TCP service carrying the given radio
// channel (-1 for all) with numClients already attached, and hands back the
// service along with the client applications' ends of their connections.
func newAttachedKissNet(t *testing.T, channel int, copyBetweenClients bool, numClients int) (*KissNetService, []net.Conn) {
	t.Helper()

	var kns = new(KissNetService)
	kns.miscConfigP = new(misc_config_s)
	kns.miscConfigP.kiss_copy = copyBetweenClients

	var kps = new(kissport_status_s)
	kps.channel = channel
	kps.tcp_port = 8001

	kns.allPorts = kps

	var clients []net.Conn

	for c := range numClients {
		var server, client = connectedTCPPair(t)

		require.True(t, kps.attachClient(c, server))

		clients = append(clients, client)
	}

	return kns, clients
}

// startKissNet brings up a real KISS TCP service, with its listening
// goroutines, carrying the given radio channel (-1 for all).
func startKissNet(t *testing.T, channel int) (*KissNetService, int) {
	t.Helper()

	var port = freeTCPPort(t)

	var mc = new(misc_config_s)
	mc.kiss_port[0] = port
	mc.kiss_chan[0] = channel

	return NewKissNetService(t.Context(), mc), port
}

// dialKissNet attaches a client application to a running service, and hands
// back its connection together with the client slot the service put it in.
//
// Dialling is retried until the port is bound, so there is no need to wait for
// the listener separately - and no probe connection taking a client slot that
// the test then has to work around.
func dialKissNet(t *testing.T, kns *KissNetService, port int) (net.Conn, int) {
	t.Helper()

	var before [MAX_NET_CLIENTS]bool

	for c := range MAX_NET_CLIENTS {
		before[c] = kns.allPorts.clientConn(c) != nil
	}

	var conn net.Conn

	var slot = -1

	require.Eventually(t, func() bool {
		if conn == nil {
			var dialled, dialErr = new(net.Dialer).DialContext(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if dialErr != nil {
				return false
			}

			conn = dialled

			t.Cleanup(func() { conn.Close() })
		}

		for c := range MAX_NET_CLIENTS {
			if !before[c] && kns.allPorts.clientConn(c) != nil {
				slot = c

				return true
			}
		}

		return false
	}, 10*time.Second, 10*time.Millisecond, "the client never attached")

	return conn, slot
}

// readKissNetFrame reads one whole KISS frame - everything up to and including
// the closing FEND - from a client's socket.
func readKissNetFrame(t *testing.T, conn net.Conn) []byte {
	t.Helper()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))

	var frame []byte

	var buf = make([]byte, 1)

	for {
		var n, err = conn.Read(buf)
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

// requireNothingToRead checks that nothing was sent to a client.  A read that
// times out is the only way to say so, so the wait is short.
func requireNothingToRead(t *testing.T, conn net.Conn, msgAndArgs ...any) {
	t.Helper()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(250*time.Millisecond)))

	var _, readErr = conn.Read(make([]byte, 1))
	assert.Error(t, readErr, msgAndArgs...)
}

// A frame received over the radio goes out to the attached client as KISS,
// with the radio channel in the top half of the first byte.
func TestKissNetSendRecPacket(t *testing.T) {
	var kns, clients = newAttachedKissNet(t, -1, false, 1)

	const channel = 2

	var frame = []byte{'h', 'i', FEND}

	kns.SendRecPacket(channel, KISS_CMD_DATA_FRAME, frame, len(frame), nil, -1)

	assert.Equal(t,
		[]byte{FEND, channel << 4, 'h', 'i', FESC, TFEND, FEND},
		readKissNetFrame(t, clients[0]))
}

// A port carrying a single radio channel shows the application channel 0 - it
// thinks it has a one-radio TNC - and passes on nothing from any other
// channel.
func TestKissNetSendRecPacketSingleChannelPort(t *testing.T) {
	const channel = 1

	var kns, clients = newAttachedKissNet(t, channel, false, 1)

	// A frame from a channel this port does not carry is not passed on, so
	// the one after it is what turns up.
	kns.SendRecPacket(channel+1, KISS_CMD_DATA_FRAME, []byte("other"), 5, nil, -1)
	kns.SendRecPacket(channel, KISS_CMD_DATA_FRAME, []byte("mine"), 4, nil, -1)

	assert.Equal(t, []byte{FEND, 0x00, 'm', 'i', 'n', 'e', FEND}, readKissNetFrame(t, clients[0]))
}

// A response to a command from one client goes to that client only, not to
// everyone attached.
func TestKissNetSendRecPacketToOneClient(t *testing.T) {
	var kns, clients = newAttachedKissNet(t, -1, false, 2)

	kns.SendRecPacket(0, KISS_CMD_SET_HARDWARE, []byte("TXBUF:0"), 7, kns.allPorts, 1)

	assert.Equal(t,
		KissEncapsulate(append([]byte{KISS_CMD_SET_HARDWARE}, []byte("TXBUF:0")...)),
		readKissNetFrame(t, clients[1]))

	requireNothingToRead(t, clients[0], "the answer went to a client that did not ask")
}

// A length of -1 is the fake command prompt, which goes out as it is - and
// says why, because it means the application is treating us as an old TNC.
func TestKissNetSendRecPacketText(t *testing.T) {
	var kns, clients = newAttachedKissNet(t, -1, false, 1)

	var output = testutils.CaptureOutput(t, func() {
		kns.SendRecPacket(0, 0, []byte("\r\ncmd:"), -1, nil, -1)
	})

	assert.Contains(t, output, "Is client app treating this like an old TNC with command mode?")

	require.NoError(t, clients[0].SetReadDeadline(time.Now().Add(10*time.Second)))

	var buf = make([]byte, len("\r\ncmd:"))
	var n, readErr = clients[0].Read(buf)
	require.NoError(t, readErr)
	assert.Equal(t, "\r\ncmd:", string(buf[:n]))
}

// A client that has gone away is hung up on rather than written to again on
// every received frame.
func TestKissNetSendRecPacketWriteErrorDetachesTheClient(t *testing.T) {
	var kns, clients = newAttachedKissNet(t, -1, false, 1)

	require.NoError(t, clients[0].Close())

	// The first write after the client goes away often succeeds - the bytes
	// go into the kernel's buffer and the reset comes back afterwards - so
	// what matters is that it does not stay attached forever.
	assert.Eventually(t, func() bool {
		kns.SendRecPacket(0, KISS_CMD_DATA_FRAME, []byte("hello"), 5, nil, -1)

		return kns.allPorts.clientConn(0) == nil
	}, 10*time.Second, 50*time.Millisecond, "a client that had gone away was never detached")
}

// KISSCOPY is for two applications sharing a TNC: what one of them transmits
// is shown to the other, so that it can see the whole conversation.
func TestKissNetCopyBetweenClients(t *testing.T) {
	var kns, clients = newAttachedKissNet(t, -1, true, 2)

	const channel = 1

	kns.Copy([]byte{0x00, 'h', 'i'}, channel, KISS_CMD_DATA_FRAME, kns.allPorts, 0)

	assert.Equal(t,
		[]byte{FEND, channel << 4, 'h', 'i', FEND},
		readKissNetFrame(t, clients[1]),
		"the frame should carry the radio channel it was transmitted on")

	requireNothingToRead(t, clients[0], "a frame was copied back to the client it came from")
}

// A single-channel port shows the application channel 0 here too, and is not
// sent anything from a channel it does not carry.
func TestKissNetCopySingleChannelPort(t *testing.T) {
	const channel = 1

	var kns, clients = newAttachedKissNet(t, channel, true, 2)

	kns.Copy([]byte{0x00, 'n', 'o'}, channel+1, KISS_CMD_DATA_FRAME, kns.allPorts, 0)
	kns.Copy([]byte{0x00, 'h', 'i'}, channel, KISS_CMD_DATA_FRAME, kns.allPorts, 0)

	assert.Equal(t, []byte{FEND, 0x00, 'h', 'i', FEND}, readKissNetFrame(t, clients[1]))
}

// Without KISSCOPY nothing is passed between clients at all.
func TestKissNetCopyDisabled(t *testing.T) {
	var kns, clients = newAttachedKissNet(t, -1, false, 2)

	kns.Copy([]byte{0x00, 'h', 'i'}, 0, KISS_CMD_DATA_FRAME, kns.allPorts, 0)

	requireNothingToRead(t, clients[1], "a frame was copied with KISSCOPY disabled")
}

// A client that has gone away is hung up on here too.
func TestKissNetCopyWriteErrorDetachesTheClient(t *testing.T) {
	var kns, clients = newAttachedKissNet(t, -1, true, 2)

	require.NoError(t, clients[1].Close())

	assert.Eventually(t, func() bool {
		kns.Copy([]byte{0x00, 'h', 'i'}, 0, KISS_CMD_DATA_FRAME, kns.allPorts, 0)

		return kns.allPorts.clientConn(1) == nil
	}, 10*time.Second, 50*time.Millisecond, "a client that had gone away was never detached")
}

// Copying must not change the caller's frame: it goes on to be transmitted
// after this, and the first byte here is rewritten per destination port.
func TestKissNetCopyDoesNotModifyTheCallersFrame(t *testing.T) {
	var kns, _ = newAttachedKissNet(t, -1, true, 2)

	var msg = []byte{0x00, 'h', 'i'}

	kns.Copy(msg, 2, KISS_CMD_DATA_FRAME, kns.allPorts, 0)

	assert.Equal(t, []byte{0x00, 'h', 'i'}, msg)
}

// With the debug option the traffic to the client is printed, so that an
// application that is not being understood can be looked at.
func TestKissNetDebugPrints(t *testing.T) {
	var kns, clients = newAttachedKissNet(t, -1, false, 1)

	kns.SetDebug(2)

	var output = testutils.CaptureOutput(t, func() {
		kns.SendRecPacket(1, KISS_CMD_DATA_FRAME, []byte("hello"), 5, nil, -1)

		readKissNetFrame(t, clients[0])
	})

	assert.Contains(t, output, "Packet content before adding KISS framing")
	assert.Contains(t, output, ">>> Data frame to KISS client application, channel 1")

	// And the fake command prompt, which is not a KISS frame at all, says so.
	output = testutils.CaptureOutput(t, func() {
		kns.SendRecPacket(0, 0, []byte("\r\ncmd:"), -1, nil, -1)
	})

	assert.Contains(t, output, "Fake command prompt")
}

// A KISS TCP port of 0 is how the configuration says "no KISS over TCP", and
// nothing is bound.
func TestKissNetDisabled(t *testing.T) {
	var kns = new(KissNetService)
	kns.miscConfigP = new(misc_config_s)

	var kps = new(kissport_status_s)
	kps.channel = -1

	var output = testutils.CaptureOutput(t, func() { kns.initOne(t.Context(), kps) })

	assert.Contains(t, output, "Disabled KISS network client port")
}

// Two things cannot have the same port, and the one that loses says so rather
// than sitting there looking attached.
func TestKissNetListenFails(t *testing.T) {
	// Every address, as the service itself binds, rather than loopback: with
	// SO_REUSEADDR - which Go sets on a TCP listener - the BSDs, macOS among
	// them, let a bind of every address succeed alongside a bind of one of
	// them.  Taking the same thing the service will ask for is what makes the
	// bind below fail on every platform rather than only on Linux.
	var listener, listenErr = new(net.ListenConfig).Listen(t.Context(), "tcp", ":0")
	require.NoError(t, listenErr)

	defer listener.Close()

	var kps = new(kissport_status_s)
	kps.tcp_port = listener.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert // A TCP listener has a TCP address.
	kps.channel = -1

	var kns = new(KissNetService)
	kns.miscConfigP = new(misc_config_s)

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	// In a goroutine, so that a bind which somehow succeeds fails this test
	// rather than leaving it in the accept loop until the whole run times
	// out, which is how the loopback address above showed up.
	var ctx, cancel = context.WithCancel(t.Context())

	defer cancel()

	var done = make(chan struct{})

	go func() {
		defer close(done)

		kns.connectListenThread(ctx, kps)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("connectListenThread did not give up although the port was taken")
	}

	var entry = hook.LastEntry()
	require.NotNil(t, entry, "nothing was said about the port that could not be bound")
	assert.Contains(t, entry.Message, "Listen failed")
}

// setupKissNetTNC gives the KISS command handling what it reaches for, but
// deliberately not the transmit queue.
//
// A service's listening goroutines outlive the test that started them - there
// is nothing to wait on - and TransmitQueue.Init writes the queue's fields without
// holding its lock, so a later test initialising the queue would race with
// anything one of these goroutines had put on it.  Hence the end-to-end tests
// here exercise commands that are answered rather than transmitted; a client's
// data frame reaching the queue is covered against the transports whose
// goroutine a test can wait for, in kiss_test.go and kissserial_test.go.
func setupKissNetTNC(t *testing.T) {
	t.Helper()

	var origAudio, origXmit, origKissNet = save_audio_config_p, xmitSvc, kissNetSvc

	t.Cleanup(func() {
		save_audio_config_p, xmitSvc, kissNetSvc = origAudio, origXmit, origKissNet
	})

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[0] = MEDIUM_RADIO

	kiss_frame_init(audioConfig)

	xmitSvc = new(XmitService)
}

// The round trip: a client's command is collected from the socket, acted on,
// and the answer written back to that same client.
func TestKissNetClientCommandIsAnswered(t *testing.T) {
	setupKissNetTNC(t)

	var kns, port = startKissNet(t, -1)

	kissNetSvc = kns

	var conn, _ = dialKissNet(t, kns, port)

	var _, writeErr = conn.Write(KissEncapsulate(append([]byte{KISS_CMD_SET_HARDWARE}, []byte("TNC:")...)))
	require.NoError(t, writeErr)

	var answer = readKissNetFrame(t, conn)

	var unwrapped = kiss_unwrap(answer)
	require.NotEmpty(t, unwrapped)

	assert.Equal(t, byte(KISS_CMD_SET_HARDWARE), unwrapped[0]&0xf)
	assert.Contains(t, string(unwrapped[1:]), "DIREWOLF ")
}

// An application that thinks it is driving an old command-mode TNC is
// answered with a command prompt, which is what stops it asking.
func TestKissNetAnswersCommandModeNoise(t *testing.T) {
	setupKissNetTNC(t)

	var kns, port = startKissNet(t, -1)

	kissNetSvc = kns

	var conn, _ = dialKissNet(t, kns, port)

	var _, writeErr = conn.Write([]byte("KISS ON\r"))
	require.NoError(t, writeErr)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))

	var buf = make([]byte, len("\r\ncmd:"))
	var n, readErr = conn.Read(buf)
	require.NoError(t, readErr)
	assert.Equal(t, "\r\ncmd:", string(buf[:n]))
}

// A client that disconnects is noticed and its slot given back, so that it -
// or somebody else - can attach again without restarting us.
func TestKissNetClientCanReattach(t *testing.T) {
	var kns, port = startKissNet(t, -1)

	var conn, slot = dialKissNet(t, kns, port)

	require.NoError(t, conn.Close())

	require.Eventually(t, func() bool {
		return kns.allPorts.clientConn(slot) == nil
	}, 10*time.Second, 10*time.Millisecond, "the client going away was not noticed")

	var _, reattachedSlot = dialKissNet(t, kns, port)

	assert.GreaterOrEqual(t, reattachedSlot, 0, "a client could not attach again")
}
