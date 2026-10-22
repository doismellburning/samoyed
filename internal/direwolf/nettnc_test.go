// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const nettncTestChannel = 3

// captureLogOutput runs fn and returns what it wrote to stdout, logrus
// entries at level and above included - samoyed-direwolf sends logrus to
// stdout alongside everything else.
func captureLogOutput(t *testing.T, level logrus.Level, fn func()) string {
	t.Helper()

	return testutils.CaptureOutput(t, func() {
		var oldLogOutput = logrus.StandardLogger().Out
		var oldLevel = logrus.GetLevel()

		logrus.SetOutput(os.Stdout)
		logrus.SetLevel(level)

		defer func() {
			logrus.SetOutput(oldLogOutput)
			logrus.SetLevel(oldLevel)
		}()

		fn()
	})
}

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
// and hands back our end and the TNC's end of the connection.
func attachTestNetTNC(ctx context.Context, t *testing.T, recFrame frameReceiver) (*NetTNC, net.Conn, <-chan net.Conn) {
	t.Helper()

	var port, conns = newTestNetTNC(ctx, t)

	var nt, err = NewNetTNC(ctx, nettncTestChannel, "127.0.0.1", port, recFrame)
	require.NoError(t, err)

	// Reattaching after the TNC goes away waits between attempts; not the
	// real five seconds, though, which would make that test the slowest here.
	nt.reattachDelay = 10 * time.Millisecond

	nt.Start(ctx)

	return nt, nextTestNetTNCConn(t, conns), conns
}

// kissFrameFor wraps a packet's on-air bytes the way a KISS TNC would before
// putting them on the wire.
func kissFrameFor(pp *ax25.Packet) []byte {
	return kiss.Encapsulate(append([]byte{0}, pp.FrameData()...))
}

// A TNC that is not there cannot be attached to, and says so rather than
// leaving a channel that looks connected.
//
//nolint:paralleltest // Serial: relies on a port staying unused, which a parallel test could take.
func TestNetTNCAttachRefused(t *testing.T) {
	// A port nothing is listening on: one taken and given straight back.
	var port = testutils.UnusedPortNumber(t)

	var nt, err = NewNetTNC(t.Context(), nettncTestChannel, "127.0.0.1", port, nil)

	require.Error(t, err)
	assert.Nil(t, nt)
}

// Starting a TNC twice would have two goroutines splitting what it sends
// between them, so the second start is refused, and says so.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestNetTNCStartedTwiceComplains(t *testing.T) {
	var dlq = NewDataLinkQueue()

	var nt, tnc, _ = attachTestNetTNC(t.Context(), t, dlq.RecFrame)

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	nt.Start(t.Context())

	var entry = hook.LastEntry()
	require.NotNil(t, entry, "nothing was said about starting the TNC twice")
	assert.Equal(t, logrus.ErrorLevel, entry.Level)
	assert.Contains(t, entry.Message, "started twice")

	// Still one reader, so a frame arrives whole.
	var _, writeErr = tnc.Write(kissFrameFor(newTestPacket(t)))
	require.NoError(t, writeErr)

	require.Eventually(t, func() bool {
		return dlq.Remove() != nil
	}, 10*time.Second, 10*time.Millisecond, "the frame from the network TNC never reached the received queue")
}

// A frame from the TNC is a frame off the air as far as the rest of the
// program is concerned, so it goes on the received queue against the NCHANNEL
// number rather than the channel in the KISS frame.
func TestNetTNCReceivedFrameReachesTheQueue(t *testing.T) {
	t.Parallel()

	var dlq = NewDataLinkQueue()

	var _, tnc, _ = attachTestNetTNC(t.Context(), t, dlq.RecFrame)

	var pp = newTestPacket(t)

	// KISS channel 0, which is not the NCHANNEL number, so a frame arriving
	// on the wrong channel would show it.
	var _, writeErr = tnc.Write(kissFrameFor(pp))
	require.NoError(t, writeErr)

	var item *dlq_item_t

	require.Eventually(t, func() bool {
		item = dlq.Remove()

		return item != nil
	}, 10*time.Second, 10*time.Millisecond, "the frame from the network TNC never reached the received queue")

	assert.Equal(t, nettncTestChannel, item._chan)
	assert.Equal(t, "Network TNC", item.spectrum)
	require.NotNil(t, item.pp)
	assert.Equal(t, pp.FrameData(), item.pp.FrameData())
}

// Transmitting on an NCHANNEL means handing the frame to the TNC as KISS, with
// the KISS channel set to 0 - the TNC has only the one radio.
func TestNetTNCSendPacket(t *testing.T) {
	t.Parallel()

	var nt, tnc, _ = attachTestNetTNC(t.Context(), t, nil)

	var pp = newTestPacket(t)

	nt.sendPacket(nettncTestChannel, pp)

	require.NoError(t, tnc.SetReadDeadline(time.Now().Add(10*time.Second)))

	var want = kissFrameFor(pp)
	var got = make([]byte, len(want))

	var readErr = readFullFrom(tnc, got)
	require.NoError(t, readErr)

	assert.Equal(t, want, got)
}

// A channel with no NCHANNEL configured has nowhere to send, and says so
// rather than crashing on the nil that stands for "no TNC".
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func TestNetTNCSendPacketNoTNC(t *testing.T) {
	var nt *NetTNC

	var output = captureLogOutput(t, logrus.InfoLevel, func() { nt.sendPacket(nettncTestChannel, newTestPacket(t)) })

	assert.Contains(t, output, "Not connected to network TNC")
	assert.Contains(t, output, "channel=3")
}

// A TNC that has gone away is not connected either, even though we attached to
// it once.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func TestNetTNCSendPacketNotConnected(t *testing.T) {
	var nt = new(NetTNC)
	nt.host = "127.0.0.1"

	var output = captureLogOutput(t, logrus.InfoLevel, func() { nt.sendPacket(nettncTestChannel, newTestPacket(t)) })

	assert.Contains(t, output, "Not connected to network TNC")
	assert.Contains(t, output, "channel=3")
}

// A write that fails means the connection is no use any more, so it is given
// up and the listening goroutine left to reattach.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func TestNetTNCSendPacketWriteErrorClosesTheConnection(t *testing.T) {
	// Straight to a NetTNC rather than through NewNetTNC, so that there is no
	// listening goroutine to reattach behind the assertion below.
	var here, there = net.Pipe()

	var nt = new(NetTNC)
	nt.setSock(here)

	require.NoError(t, there.Close())

	var output = captureLogOutput(t, logrus.InfoLevel, func() { nt.sendPacket(nettncTestChannel, newTestPacket(t)) })

	assert.Contains(t, output, "Error sending packet to KISS network TNC")
	assert.Contains(t, output, "channel=3")
	assert.Nil(t, nt.getSock(), "the connection was not given up after the write failed")
}

// A TNC that stops reading leaves a packet nowhere to go once the socket
// buffers fill.  sendPacket runs on whoever is transmitting - the digipeater,
// a beacon, a client's handler - so rather than leave them waiting for ever,
// it gives up on the TNC after a while, and the listening goroutine reattaches.
func TestNetTNCSendPacketGivesUpOnATNCThatStopsReading(t *testing.T) {
	t.Parallel()

	var nt, _, conns = attachTestNetTNC(t.Context(), t, nil)

	nt.writeTimeout = 50 * time.Millisecond

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Destination] = "Q1TEST"
	addrs[ax25.Source] = "Q2TEST"

	// As big as a frame gets, so the buffers fill in fewer of them.
	var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUTEST, 0, 0, []byte(strings.Repeat("x", ax25.MaxInfoLen)))
	require.NotNil(t, pp)

	// The fake TNC never reads its end, so sooner or later a send can't go.
	var gaveUp = make(chan struct{})

	go func() {
		defer close(gaveUp)

		for nt.sendPacket(nettncTestChannel, pp) {
		}
	}()

	select {
	case <-gaveUp:
	case <-time.After(20 * time.Second):
		t.Fatal("sending to a TNC that stopped reading never gave up")
	}

	// Given up on, and attached to afresh.
	nextTestNetTNCConn(t, conns)
}

// A network TNC is somebody else's process, and it can restart.  Losing the
// connection is not losing the channel: we attach again and carry on.
func TestNetTNCReattachesAfterTheTNCGoesAway(t *testing.T) {
	t.Parallel()

	var dlq = NewDataLinkQueue()

	var _, tnc, conns = attachTestNetTNC(t.Context(), t, dlq.RecFrame)

	require.NoError(t, tnc.Close())

	var reattached = nextTestNetTNCConn(t, conns)

	var pp = newTestPacket(t)

	var _, writeErr = reattached.Write(kissFrameFor(pp))
	require.NoError(t, writeErr)

	require.Eventually(t, func() bool {
		return dlq.Remove() != nil
	}, 10*time.Second, 10*time.Millisecond, "nothing was heard on the reattached connection")
}

// A TNC that goes away part way through a frame takes that frame with it.
// What it had sent of it must not be glued to the start of whatever the
// reattached connection sends, which would turn the new connection's opening
// FEND into the close of a truncated frame and its first whole frame into
// noise.
func TestNetTNCReattachDiscardsAPartialFrame(t *testing.T) {
	t.Parallel()

	var dlq = NewDataLinkQueue()

	var _, tnc, conns = attachTestNetTNC(t.Context(), t, dlq.RecFrame)

	var pp = newTestPacket(t)
	var frame = kissFrameFor(pp)

	var _, partialErr = tnc.Write(frame[:len(frame)-5])
	require.NoError(t, partialErr)

	// TCP delivers the partial frame ahead of the end of the stream, so it is
	// read and collected before the listener notices the TNC has gone.
	require.NoError(t, tnc.Close())

	var reattached = nextTestNetTNCConn(t, conns)

	var _, writeErr = reattached.Write(frame)
	require.NoError(t, writeErr)

	var item *dlq_item_t

	require.Eventually(t, func() bool {
		item = dlq.Remove()

		return item != nil
	}, 10*time.Second, 10*time.Millisecond, "nothing was heard on the reattached connection")

	require.NotNil(t, item.pp)
	assert.Equal(t, pp.FrameData(), item.pp.FrameData(), "the frame heard was not the one the reattached TNC sent")
	assert.Nil(t, dlq.Remove(), "more than one frame was heard")
}

// The channel goes away with the rest of the program: a cancelled context
// stops the listening goroutine and hangs up on the TNC, rather than leaving a
// goroutine blocked on a read of it forever.
func TestNetTNCStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	var ctx, cancel = context.WithCancel(t.Context())

	var _, tnc, _ = attachTestNetTNC(ctx, t, nil)

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
	t.Parallel()

	var dlq = NewDataLinkQueue()

	var pp = newTestPacket(t)

	var kf = new(kiss.Collector)

	for _, b := range append([]byte("cmd:\r\n"), kissFrameFor(pp)...) {
		tncRecByte(kf, b, 0, nettncTestChannel, netTNC(), dlq.RecFrame)
	}

	var item = dlq.Remove()
	require.NotNil(t, item, "the frame after the noise was not decoded")
	assert.Equal(t, pp.FrameData(), item.pp.FrameData())
}

// FENDs with nothing between them are how some TNCs idle, and are not frames.
func TestNetTNCEmptyFramesAreNotFrames(t *testing.T) {
	t.Parallel()

	var dlq = NewDataLinkQueue()

	var kf = new(kiss.Collector)

	for _, b := range []byte{kiss.FEND, kiss.FEND, kiss.FEND, kiss.FEND} {
		tncRecByte(kf, b, 0, nettncTestChannel, netTNC(), dlq.RecFrame)
	}

	assert.Nil(t, dlq.Remove(), "an empty KISS frame was taken for a received frame")
}

// A frame too short to be AX.25 cannot be made into a packet, and is reported
// rather than passed on as something the rest of the program has to cope with.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func TestNetTNCUndecodableFrameIsReported(t *testing.T) {
	var dlq = NewDataLinkQueue()

	var kf = new(kiss.Collector)

	var output = captureLogOutput(t, logrus.InfoLevel, func() {
		for _, b := range kiss.Encapsulate([]byte{0, 'n', 'o', 't', ' ', 'a', 'x', '2', '5'}) {
			tncRecByte(kf, b, 0, nettncTestChannel, netTNC(), dlq.RecFrame)
		}
	})

	assert.Contains(t, output, "Failed to create packet object for KISS frame")
	assert.Contains(t, output, "channel=3")
	assert.Nil(t, dlq.Remove())
}

// FEND FESC FEND is a frame, but escapes nothing, so there is no type byte
// once it is unescaped.  Three bytes from the far end of the network
// connection used to take the whole program down, looking for one.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func TestNetTNCFrameEmptyOnceUnescapedIsReported(t *testing.T) {
	var dlq = NewDataLinkQueue()

	var kf = new(kiss.Collector)

	var output = captureLogOutput(t, logrus.InfoLevel, func() {
		for _, b := range []byte{kiss.FEND, kiss.FESC, kiss.FEND} {
			tncRecByte(kf, b, 2, nettncTestChannel, netTNC(), dlq.RecFrame)
		}
	})

	assert.Contains(t, output, "nothing in it")
	assert.Nil(t, dlq.Remove())
}

// A TNC that never sends a FEND would otherwise fill the frame buffer without
// limit, so the collecting stops at the maximum and says so - once, not once
// for every byte past it.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func TestNetTNCOverlongFrameIsReported(t *testing.T) {
	var kf = new(kiss.Collector)

	var output = captureLogOutput(t, logrus.InfoLevel, func() {
		tncRecByte(kf, kiss.FEND, 0, nettncTestChannel, netTNC(), nil)

		for range kiss.MaxFrameLen + 10 {
			tncRecByte(kf, 'x', 0, nettncTestChannel, netTNC(), nil)
		}
	})

	assert.Equal(t, 1, strings.Count(output, "KISS frame exceeded maximum length"))
}

// The TNC does eventually send its closing FEND, and the byte it used to be
// written to was one past the end of the buffer - which took the whole program
// down, from anything on the far end of the network connection.  The overlong
// frame is thrown away, and the collector is left ready for the next one.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func TestNetTNCOverlongFrameWithClosingFENDIsDiscarded(t *testing.T) {
	var dlq = NewDataLinkQueue()

	var kf = new(kiss.Collector)

	var output = captureLogOutput(t, logrus.InfoLevel, func() {
		tncRecByte(kf, kiss.FEND, 0, nettncTestChannel, netTNC(), dlq.RecFrame)

		for range kiss.MaxFrameLen + 10 {
			tncRecByte(kf, 'x', 0, nettncTestChannel, netTNC(), dlq.RecFrame)
		}

		tncRecByte(kf, kiss.FEND, 0, nettncTestChannel, netTNC(), dlq.RecFrame)
	})

	assert.Contains(t, output, "KISS frame exceeded maximum length; discarding it")
	assert.Nil(t, dlq.Remove(), "a fragment of the overlong frame was acted on")

	// And a well formed frame after it still gets through.
	for _, b := range kissFrameFor(newTestPacket(t)) {
		tncRecByte(kf, b, 0, nettncTestChannel, netTNC(), dlq.RecFrame)
	}

	assert.NotNil(t, dlq.Remove())
}

// With the debug option the frames are printed in both the form they arrived
// in and the form they were decoded to, so that a TNC that is not being
// understood can be looked at.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func TestNetTNCDebugPrints(t *testing.T) {
	var kf = new(kiss.Collector)

	var output = captureLogOutput(t, logrus.DebugLevel, func() {
		for _, b := range kissFrameFor(newTestPacket(t)) {
			tncRecByte(kf, b, 2, nettncTestChannel, netTNC(), nil)
		}
	})

	assert.Contains(t, output, `direction=from function="Data frame"`)
	assert.Contains(t, output, "Frame content after removing KISS framing")
}

// NCHANNEL channels are attached to at start up, and the ones that are
// something else are left alone.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func TestNetTNCInitAttachesNetworkChannels(t *testing.T) {
	var port, conns = newTestNetTNC(t.Context(), t)

	var audioConfig = new(RadioConfig)
	audioConfig.chan_medium[0] = MEDIUM_RADIO
	audioConfig.chan_medium[nettncTestChannel] = MEDIUM_NETTNC
	audioConfig.nettnc_addr[nettncTestChannel] = "127.0.0.1"
	audioConfig.nettnc_port[nettncTestChannel] = port

	var tncs [MAX_TOTAL_CHANS]*NetTNC

	var output = captureLogOutput(t, logrus.InfoLevel, func() { tncs = NewNetTNCs(t.Context(), audioConfig, nil) })

	assert.Contains(t, output, fmt.Sprintf(`msg="Network TNC" channel=%d host=127.0.0.1 port=%d`, nettncTestChannel, port))

	nextTestNetTNCConn(t, conns)

	assert.NotNil(t, tncs[nettncTestChannel])
	assert.Nil(t, tncs[0], "a radio channel should not have been attached to as a network TNC")
}

// A stop that arrives while a network TNC is being connected to cuts the
// connection short, and NewNetTNCs goes back to its caller to tear down
// rather than exiting as though the TNC could not be reached.
//
//nolint:paralleltest // Serial: captures os.Stdout and relies on a port staying unused, which a parallel test could take.
func TestNetTNCInitReturnsWhenCancelled(t *testing.T) {
	var ctx, cancel = context.WithCancel(t.Context())
	cancel()

	var audioConfig = new(RadioConfig)
	audioConfig.chan_medium[nettncTestChannel] = MEDIUM_NETTNC
	audioConfig.nettnc_addr[nettncTestChannel] = "127.0.0.1"
	audioConfig.nettnc_port[nettncTestChannel] = testutils.UnusedPortNumber(t)

	var tncs [MAX_TOTAL_CHANS]*NetTNC

	testutils.CaptureOutput(t, func() { tncs = NewNetTNCs(ctx, audioConfig, nil) })

	assert.Nil(t, tncs[nettncTestChannel], "a connection cut short should not have been attached")
}

// readFullFrom fills buf from conn, which a single Read is not obliged to do.
func readFullFrom(conn net.Conn, buf []byte) error {
	var got int

	for got < len(buf) {
		var n, err = conn.Read(buf[got:])

		got += n

		if err != nil {
			return err
		}
	}

	return nil
}

// A packet queued for an NCHANNEL channel skips the radio queues and goes to
// the network TNC the transmit queue was handed.
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func TestNetTNCTransmitQueueSendsToItsTNCs(t *testing.T) {
	var nt, tnc, _ = attachTestNetTNC(t.Context(), t, nil)

	var audioConfig = new(RadioConfig)
	audioConfig.chan_medium[nettncTestChannel] = MEDIUM_NETTNC

	var tncs [MAX_TOTAL_CHANS]*NetTNC
	tncs[nettncTestChannel] = nt

	var tq = NewTransmitQueue(audioConfig, tncs, [MAX_TOTAL_CHANS]*SerialTNC{}, [MAX_TOTAL_CHANS]*AXUDPChannel{}, nil)

	var pp = newTestPacket(t)

	testutils.CaptureOutput(t, func() { tq.Append(nettncTestChannel, TQ_PRIO_1_LO, pp) })

	require.NoError(t, tnc.SetReadDeadline(time.Now().Add(10*time.Second)))

	var want = kissFrameFor(pp)
	var got = make([]byte, len(want))

	var readErr = readFullFrom(tnc, got)
	require.NoError(t, readErr)

	assert.Equal(t, want, got)
	assert.Nil(t, tq.Peek(nettncTestChannel, TQ_PRIO_1_LO), "the packet should not have been queued for a radio")
}
