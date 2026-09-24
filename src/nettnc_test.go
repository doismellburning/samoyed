// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

// An NCHANNEL is a radio channel provided by somebody else's KISS TNC over
// TCP, so everything here is a loopback test: a listener stands in for the
// TNC, and what we send it and what it sends us are both just bytes on a
// socket.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const nettncTestChannel = 3

// newTestNetTNC listens on a free port, playing the network KISS TNC, and
// hands back the port along with a channel delivering each connection it
// accepts - one per attach, and another per reattach.
func newTestNetTNC(ctx context.Context, t *testing.T) (int, <-chan net.Conn) {
	t.Helper()

	var listener, listenErr = new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, listenErr)

	t.Cleanup(func() { listener.Close() })

	var conns = make(chan net.Conn, 4)

	go func() {
		defer close(conns)

		for {
			var conn, acceptErr = listener.Accept()
			if acceptErr != nil {
				return
			}

			conns <- conn
		}
	}()

	var port = listener.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert // A TCP listener has a TCP address.

	return port, conns
}

// nextTestNetTNCConn waits for the next connection from the channel under
// test, and arranges for it to be closed when the test ends.
func nextTestNetTNCConn(t *testing.T, conns <-chan net.Conn) net.Conn {
	t.Helper()

	select {
	case conn, ok := <-conns:
		require.True(t, ok, "the fake network TNC stopped listening")

		t.Cleanup(func() { conn.Close() })

		return conn
	case <-time.After(20 * time.Second):
		t.Fatal("nothing attached to the network TNC")

		return nil
	}
}

// attachTestNetTNC attaches channel nettncTestChannel to a fake network TNC,
// and hands back the TNC's end of the connection.
func attachTestNetTNC(ctx context.Context, t *testing.T) (net.Conn, <-chan net.Conn) {
	t.Helper()

	var port, conns = newTestNetTNC(ctx, t)

	var orig = s_net_tncs[nettncTestChannel]

	t.Cleanup(func() { s_net_tncs[nettncTestChannel] = orig })

	require.Zero(t, nettnc_attach(ctx, nettncTestChannel, "127.0.0.1", port))

	return nextTestNetTNCConn(t, conns), conns
}

// expectReceivedFrames empties the received queue, so that a test sees only the
// frames it put there itself.
func expectReceivedFrames(t *testing.T) {
	t.Helper()

	t.Cleanup(dataLinkQueue.Init)

	dataLinkQueue.Init()
}

// kissFrameFor wraps a packet's on-air bytes the way a KISS TNC would before
// putting them on the wire.
func kissFrameFor(pp *packet_t) []byte {
	return KissEncapsulate(append([]byte{0}, ax25_get_frame_data(pp)...))
}

// A TNC that is not there cannot be attached to, and says so rather than
// leaving a channel that looks connected.
func TestNetTNCAttachRefused(t *testing.T) {
	var orig = s_net_tncs[nettncTestChannel]

	t.Cleanup(func() { s_net_tncs[nettncTestChannel] = orig })

	// A port nothing is listening on: one taken and given straight back.
	var port = freeTCPPort(t)

	assert.Equal(t, -1, nettnc_attach(t.Context(), nettncTestChannel, "127.0.0.1", port))
}

// A frame from the TNC is a frame off the air as far as the rest of the
// program is concerned, so it goes on the received queue against the NCHANNEL
// number rather than the channel in the KISS frame.
func TestNetTNCReceivedFrameReachesTheQueue(t *testing.T) {
	expectReceivedFrames(t)

	var tnc, _ = attachTestNetTNC(t.Context(), t)

	var pp = newTestPacket(t)

	// KISS channel 0, which is not the NCHANNEL number, so a frame arriving
	// on the wrong channel would show it.
	var _, writeErr = tnc.Write(kissFrameFor(pp))
	require.NoError(t, writeErr)

	var item *dlq_item_t

	require.Eventually(t, func() bool {
		item = dataLinkQueue.Remove()

		return item != nil
	}, 10*time.Second, 10*time.Millisecond, "the frame from the network TNC never reached the received queue")

	assert.Equal(t, nettncTestChannel, item._chan)
	assert.Equal(t, "Network TNC", item.spectrum)
	require.NotNil(t, item.pp)
	assert.Equal(t, ax25_get_frame_data(pp), ax25_get_frame_data(item.pp))
}

// Transmitting on an NCHANNEL means handing the frame to the TNC as KISS, with
// the KISS channel set to 0 - the TNC has only the one radio.
func TestNetTNCSendPacket(t *testing.T) {
	var tnc, _ = attachTestNetTNC(t.Context(), t)

	var pp = newTestPacket(t)

	nettnc_send_packet(nettncTestChannel, pp)

	require.NoError(t, tnc.SetReadDeadline(time.Now().Add(10*time.Second)))

	var want = kissFrameFor(pp)
	var got = make([]byte, len(want))

	var _, readErr = readFullFrom(tnc, got)
	require.NoError(t, readErr)

	assert.Equal(t, want, got)
}

// A channel with no NCHANNEL configured has nowhere to send, and says so
// rather than crashing on the nil that stands for "no TNC".
func TestNetTNCSendPacketNoTNC(t *testing.T) {
	var orig = s_net_tncs[nettncTestChannel]

	t.Cleanup(func() { s_net_tncs[nettncTestChannel] = orig })

	s_net_tncs[nettncTestChannel] = nil

	var output = CaptureOutput(t, func() { nettnc_send_packet(nettncTestChannel, newTestPacket(t)) })

	assert.Contains(t, output, "Not connected to network TNC for channel 3")
}

// A TNC that has gone away is not connected either, even though we attached to
// it once.
func TestNetTNCSendPacketNotConnected(t *testing.T) {
	var orig = s_net_tncs[nettncTestChannel]

	t.Cleanup(func() { s_net_tncs[nettncTestChannel] = orig })

	var nt = new(NetTNC)
	nt.host = "127.0.0.1"
	s_net_tncs[nettncTestChannel] = nt

	var output = CaptureOutput(t, func() { nettnc_send_packet(nettncTestChannel, newTestPacket(t)) })

	assert.Contains(t, output, "Not connected to network TNC for channel 3")
}

// A write that fails means the connection is no use any more, so it is given
// up and the listening goroutine left to reattach.
func TestNetTNCSendPacketWriteErrorClosesTheConnection(t *testing.T) {
	var orig = s_net_tncs[nettncTestChannel]

	t.Cleanup(func() { s_net_tncs[nettncTestChannel] = orig })

	// Straight to a NetTNC rather than through attach, so that there is no
	// listening goroutine to reattach behind the assertion below.
	var here, there = net.Pipe()

	var nt = new(NetTNC)
	nt.setSock(here)
	s_net_tncs[nettncTestChannel] = nt

	require.NoError(t, there.Close())

	var output = CaptureOutput(t, func() { nettnc_send_packet(nettncTestChannel, newTestPacket(t)) })

	assert.Contains(t, output, "sending packet to KISS Network TNC for channel 3")
	assert.Nil(t, nt.getSock(), "the connection was not given up after the write failed")
}

// A network TNC is somebody else's process, and it can restart.  Losing the
// connection is not losing the channel: we attach again and carry on.
func TestNetTNCReattachesAfterTheTNCGoesAway(t *testing.T) {
	expectReceivedFrames(t)

	var tnc, conns = attachTestNetTNC(t.Context(), t)

	require.NoError(t, tnc.Close())

	// Reattaching waits five seconds between attempts, so this is the slow
	// one.
	var reattached = nextTestNetTNCConn(t, conns)

	var pp = newTestPacket(t)

	var _, writeErr = reattached.Write(kissFrameFor(pp))
	require.NoError(t, writeErr)

	require.Eventually(t, func() bool {
		return dataLinkQueue.Remove() != nil
	}, 10*time.Second, 10*time.Millisecond, "nothing was heard on the reattached connection")
}

// The channel goes away with the rest of the program: a cancelled context
// stops the listening goroutine and hangs up on the TNC, rather than leaving a
// goroutine blocked on a read of it forever.
func TestNetTNCStopsWhenCancelled(t *testing.T) {
	var ctx, cancel = context.WithCancel(t.Context())

	var tnc, _ = attachTestNetTNC(ctx, t)

	cancel()

	require.NoError(t, tnc.SetReadDeadline(time.Now().Add(10*time.Second)))

	var _, readErr = tnc.Read(make([]byte, 1))
	require.Error(t, readErr, "the network TNC was not hung up on when the channel was stopped")

	// Being hung up on is what we are checking for, not how the hanging up
	// reads: a closed socket gives the other end EOF or, if the stack sends a
	// reset instead, ECONNRESET.  Only the read deadline expiring means it
	// didn't happen at all.
	var netErr net.Error
	assert.False(t, errors.As(readErr, &netErr) && netErr.Timeout(),
		"the network TNC was not hung up on when the channel was stopped: %v", readErr)
}

// Everything before the opening FEND is noise from something that is not
// speaking KISS, and is dropped rather than taken for frame contents.
func TestNetTNCNoiseBeforeAFrameIsIgnored(t *testing.T) {
	expectReceivedFrames(t)

	var pp = newTestPacket(t)

	var kf = new(KISSFrame)

	for _, b := range append([]byte("cmd:\r\n"), kissFrameFor(pp)...) {
		my_kiss_rec_byte(kf, b, 0, nettncTestChannel)
	}

	var item = dataLinkQueue.Remove()
	require.NotNil(t, item, "the frame after the noise was not decoded")
	assert.Equal(t, ax25_get_frame_data(pp), ax25_get_frame_data(item.pp))
}

// FENDs with nothing between them are how some TNCs idle, and are not frames.
func TestNetTNCEmptyFramesAreNotFrames(t *testing.T) {
	expectReceivedFrames(t)

	var kf = new(KISSFrame)

	for _, b := range []byte{FEND, FEND, FEND, FEND} {
		my_kiss_rec_byte(kf, b, 0, nettncTestChannel)
	}

	assert.Nil(t, dataLinkQueue.Remove(), "an empty KISS frame was taken for a received frame")
}

// A frame too short to be AX.25 cannot be made into a packet, and is reported
// rather than passed on as something the rest of the program has to cope with.
func TestNetTNCUndecodableFrameIsReported(t *testing.T) {
	expectReceivedFrames(t)

	var kf = new(KISSFrame)

	var output = CaptureOutput(t, func() {
		for _, b := range KissEncapsulate([]byte{0, 'n', 'o', 't', ' ', 'a', 'x', '2', '5'}) {
			my_kiss_rec_byte(kf, b, 0, nettncTestChannel)
		}
	})

	assert.Contains(t, output, "Failed to create packet object for KISS frame from channel 3 network TNC")
	assert.Nil(t, dataLinkQueue.Remove())
}

// A TNC that never sends a FEND would otherwise fill the frame buffer without
// limit, so the collecting stops at the maximum and says so.
func TestNetTNCOverlongFrameIsReported(t *testing.T) {
	var kf = new(KISSFrame)

	var output = CaptureOutput(t, func() {
		my_kiss_rec_byte(kf, FEND, 0, nettncTestChannel)

		for range MAX_KISS_LEN + 10 {
			my_kiss_rec_byte(kf, 'x', 0, nettncTestChannel)
		}
	})

	assert.Contains(t, output, "KISS frame from network TNC exceeded maximum length")
	assert.Equal(t, MAX_KISS_LEN, kf.kiss_len)
}

// The TNC does eventually send its closing FEND, and the byte it used to be
// written to was one past the end of the buffer - which took the whole program
// down, from anything on the far end of the network connection.  The overlong
// frame is thrown away, and the collector is left ready for the next one.
func TestNetTNCOverlongFrameWithClosingFENDIsDiscarded(t *testing.T) {
	expectReceivedFrames(t)

	var kf = new(KISSFrame)

	var output = CaptureOutput(t, func() {
		my_kiss_rec_byte(kf, FEND, 0, nettncTestChannel)

		for range MAX_KISS_LEN + 10 {
			my_kiss_rec_byte(kf, 'x', 0, nettncTestChannel)
		}

		my_kiss_rec_byte(kf, FEND, 0, nettncTestChannel)
	})

	assert.Contains(t, output, "KISS frame from network TNC exceeded maximum length.  Discarding it.")
	assert.Equal(t, 0, kf.kiss_len)
	assert.Equal(t, KS_SEARCHING, kf.state)
	assert.Nil(t, dataLinkQueue.Remove(), "a fragment of the overlong frame was acted on")

	// And a well formed frame after it still gets through.
	for _, b := range kissFrameFor(newTestPacket(t)) {
		my_kiss_rec_byte(kf, b, 0, nettncTestChannel)
	}

	assert.NotNil(t, dataLinkQueue.Remove())
}

// With the debug option the frames are printed in both the form they arrived
// in and the form they were decoded to, so that a TNC that is not being
// understood can be looked at.
func TestNetTNCDebugPrints(t *testing.T) {
	expectReceivedFrames(t)

	var kf = new(KISSFrame)

	var output = CaptureOutput(t, func() {
		for _, b := range kissFrameFor(newTestPacket(t)) {
			my_kiss_rec_byte(kf, b, 2, nettncTestChannel)
		}
	})

	assert.Contains(t, output, "<<< Data frame from KISS client application")
	assert.Contains(t, output, "Frame content after removing KISS framing")
}

// NCHANNEL channels are attached to at start up, and the ones that are
// something else are left alone.
func TestNetTNCInitAttachesNetworkChannels(t *testing.T) {
	var orig = s_net_tncs

	t.Cleanup(func() { s_net_tncs = orig })

	var port, conns = newTestNetTNC(t.Context(), t)

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[0] = MEDIUM_RADIO
	audioConfig.chan_medium[nettncTestChannel] = MEDIUM_NETTNC
	audioConfig.nettnc_addr[nettncTestChannel] = "127.0.0.1"
	audioConfig.nettnc_port[nettncTestChannel] = port

	var output = CaptureOutput(t, func() { nettnc_init(t.Context(), audioConfig) })

	assert.Contains(t, output, fmt.Sprintf("Channel %d: Network TNC 127.0.0.1 %d", nettncTestChannel, port))

	nextTestNetTNCConn(t, conns)

	assert.NotNil(t, s_net_tncs[nettncTestChannel])
	assert.Nil(t, s_net_tncs[0], "a radio channel should not have been attached to as a network TNC")
}

// A stop that arrives while a network TNC is being connected to cuts the
// connection short, and nettnc_init goes back to its caller to tear down
// rather than exiting as though the TNC could not be reached.
func TestNetTNCInitReturnsWhenCancelled(t *testing.T) {
	var orig = s_net_tncs

	t.Cleanup(func() { s_net_tncs = orig })

	var ctx, cancel = context.WithCancel(t.Context())
	cancel()

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[nettncTestChannel] = MEDIUM_NETTNC
	audioConfig.nettnc_addr[nettncTestChannel] = "127.0.0.1"
	audioConfig.nettnc_port[nettncTestChannel] = freeTCPPort(t)

	CaptureOutput(t, func() { nettnc_init(ctx, audioConfig) })
}

// readFullFrom fills buf from conn, which a single Read is not obliged to do.
func readFullFrom(conn net.Conn, buf []byte) (int, error) {
	var got int

	for got < len(buf) {
		var n, err = conn.Read(buf[got:])

		got += n

		if err != nil {
			return got, err
		}
	}

	return got, nil
}
