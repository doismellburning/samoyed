// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// The same two platforms as kissserial_test.go, whose newTestSerialDevice this
// uses.
//go:build linux || darwin

package direwolf

// A pseudo terminal stands in for the serial port the TNC is on, as it does
// for KISS over a serial port, so none of this needs any hardware.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const serialTNCTestChannel = 4

// startTestSerialTNC attaches channel serialTNCTestChannel to the serial TNC
// on device, and starts listening to it, until the test ends.  It hands back
// a channel that is closed once the listening goroutine has finished.
//
// The port cannot be closed out from under a blocked read, so the goroutine is
// only got back by cancelling and then having the far end go away, which
// closeFarEnd does.
func startTestSerialTNC(t *testing.T, device string, recFrame frameReceiver, closeFarEnd func()) (*SerialTNC, context.CancelFunc, <-chan struct{}) {
	t.Helper()

	var st, err = NewSerialTNC(serialTNCTestChannel, device, 9600, recFrame)
	require.NoError(t, err)

	st.reattachDelay = 10 * time.Millisecond

	var ctx, cancel = context.WithCancel(t.Context())

	var done = make(chan struct{})

	go func() {
		defer close(done)

		st.listenThread(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		closeFarEnd()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("SerialTNC.listenThread did not finish")
		}
	})

	return st, cancel, done
}

// nextDLQItem waits for something to arrive on the received queue.
func nextDLQItem(t *testing.T, dlq *DataLinkQueue) *dlq_item_t {
	t.Helper()

	var item *dlq_item_t

	require.Eventually(t, func() bool {
		item = dlq.Remove()

		return item != nil
	}, 10*time.Second, 10*time.Millisecond, "the frame from the serial TNC never reached the received queue")

	return item
}

// A TNC whose port is not there cannot be attached to, and says so rather than
// leaving a channel that looks attached.
func TestSerialTNCAttachFails(t *testing.T) {
	var st, err = NewSerialTNC(serialTNCTestChannel, "/dev/there-is-no-such-serial-port", 9600, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "/dev/there-is-no-such-serial-port")
	assert.Nil(t, st)
}

// A frame from the TNC is a frame off the air as far as the rest of the
// program is concerned, so it goes on the received queue against the SCHANNEL
// number rather than the channel in the KISS frame.
func TestSerialTNCReceivedFrameReachesTheQueue(t *testing.T) {
	var dlq = NewDataLinkQueue()

	var device, tnc = newTestSerialDevice(t)

	startTestSerialTNC(t, device, dlq.RecFrame, func() { tnc.Close() })

	var pp = newTestPacket(t)

	var _, writeErr = tnc.Write(kissFrameFor(pp))
	require.NoError(t, writeErr)

	var item = nextDLQItem(t, dlq)

	assert.Equal(t, serialTNCTestChannel, item._chan)
	assert.Equal(t, dwcore.ChannelSource(dwcore.SourceSerialTNC, serialTNCTestChannel), item.src)
	assert.Equal(t, "Serial TNC", item.spectrum)
	require.NotNil(t, item.pp)
	assert.Equal(t, pp.FrameData(), item.pp.FrameData())
}

// Starting a TNC twice would have two goroutines splitting what it sends
// between them, so the second start is refused, and says so.
func TestSerialTNCStartedTwiceComplains(t *testing.T) {
	var device, tnc = newTestSerialDevice(t)

	var st, err = NewSerialTNC(serialTNCTestChannel, device, 9600, nil)
	require.NoError(t, err)

	st.started.Store(true)

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	st.Start(t.Context())

	var entry = hook.LastEntry()
	require.NotNil(t, entry, "nothing was said about starting the TNC twice")
	assert.Equal(t, logrus.ErrorLevel, entry.Level)
	assert.Contains(t, entry.Message, "started twice")

	st.closePort()
	tnc.Close()
}

// Transmitting on an SCHANNEL means handing the frame to the TNC as KISS, with
// the KISS channel set to 0 - the TNC has only the one radio.
func TestSerialTNCSendPacket(t *testing.T) {
	var device, tnc = newTestSerialDevice(t)

	var st, _, _ = startTestSerialTNC(t, device, nil, func() { tnc.Close() })

	var pp = newTestPacket(t)

	st.sendPacket(serialTNCTestChannel, pp)

	assert.Equal(t, kissFrameFor(pp), readSerialKissFrame(t, tnc))
}

// A channel with no TNC, or one whose TNC is not attached just now, has
// nowhere to send to, so the packet is dropped with a word about why - and,
// where there is no TNC to know it, which channel it was for.
func TestSerialTNCSendPacketNotAttached(t *testing.T) {
	for _, tc := range []struct {
		name    string
		st      *SerialTNC
		channel int
	}{
		{name: "no TNC", st: nil, channel: serialTNCTestChannel + 1},
		{name: "not attached", st: new(SerialTNC), channel: serialTNCTestChannel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hook = test.NewGlobal()

			t.Cleanup(hook.Reset)

			tc.st.sendPacket(tc.channel, newTestPacket(t))

			var entry = hook.LastEntry()
			require.NotNil(t, entry)
			assert.Contains(t, entry.Message, "Not attached to serial TNC")
			assert.Equal(t, tc.channel, entry.Data["channel"])
		})
	}
}

// A port that has gone away cannot be written to, so it is given up rather
// than written to again.  The closing is left to the listening goroutine,
// which may be blocked reading the port.
func TestSerialTNCSendPacketWriteErrorGivesUpThePort(t *testing.T) {
	var device, tnc = newTestSerialDevice(t)

	var st, err = NewSerialTNC(serialTNCTestChannel, device, 9600, nil)
	require.NoError(t, err)

	t.Cleanup(st.closePort)

	require.NoError(t, tnc.Close())

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	st.sendPacket(serialTNCTestChannel, newTestPacket(t))

	var entry = hook.LastEntry()
	require.NotNil(t, entry)
	assert.Contains(t, entry.Message, "Error sending packet to serial TNC")

	var fd, failed = st.port()
	assert.NotNil(t, fd, "the sender closed the port, which only the listener may do")
	assert.True(t, failed, "the serial port was not given up after the write error")

	st.sendPacket(serialTNCTestChannel, newTestPacket(t))

	assert.Contains(t, hook.LastEntry().Message, "Not attached to serial TNC")
}

// A serial TNC can go away - a USB serial device unplugged, say - and come
// back.  Losing it should not lose the channel: the listener reattaches, and
// frames flow again.
func TestSerialTNCReattachesAfterTheTNCGoesAway(t *testing.T) {
	var dlq = NewDataLinkQueue()

	// The listener reopens the TNC by name, so a link that can be pointed at
	// a second pseudo terminal stands in for the device coming back.
	var device = filepath.Join(t.TempDir(), "tnc")

	var first, firstTNC = newTestSerialDevice(t)
	require.NoError(t, os.Symlink(first, device))

	var second, secondTNC = newTestSerialDevice(t)

	var st, _, _ = startTestSerialTNC(t, device, dlq.RecFrame, func() { secondTNC.Close() })

	var firstPort, _ = st.port()

	var next = device + ".next"
	require.NoError(t, os.Symlink(second, next))
	require.NoError(t, os.Rename(next, device))

	require.NoError(t, firstTNC.Close())

	// Until the listener has reopened the device there is nothing on the far
	// end of the second terminal, and macOS refuses writes to it outright.
	require.Eventually(t, func() bool {
		var port, _ = st.port()

		return port != nil && port != firstPort
	}, 10*time.Second, 10*time.Millisecond, "the listener never reattached to the serial TNC")

	var pp = newTestPacket(t)

	var _, writeErr = secondTNC.Write(kissFrameFor(pp))
	require.NoError(t, writeErr)

	var item = nextDLQItem(t, dlq)

	assert.Equal(t, serialTNCTestChannel, item._chan)
	assert.Equal(t, pp.FrameData(), item.pp.FrameData())
}

// Cancelling stops the listener at its next look - once the read it is
// blocked in returns - and it lets go of the port on the way out.
func TestSerialTNCStopsWhenCancelled(t *testing.T) {
	var device, tnc = newTestSerialDevice(t)

	var st, cancel, done = startTestSerialTNC(t, device, nil, func() { tnc.Close() })

	cancel()

	// Something to read gets the listener out of its read.
	var _, writeErr = tnc.Write([]byte{0})
	require.NoError(t, writeErr)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SerialTNC.listenThread did not stop when cancelled")
	}

	var fd, _ = st.port()
	assert.Nil(t, fd, "the port was not closed when the listener stopped")
}

// Each SCHANNEL in the configuration gets a serial TNC attached to it, and no
// other channel does.
func TestNewSerialTNCsAttachesSerialChannels(t *testing.T) {
	var dlq = NewDataLinkQueue()

	var device, tnc = newTestSerialDevice(t)

	var audioConfig = new(RadioConfig)
	audioConfig.chan_medium[0] = MEDIUM_RADIO
	audioConfig.chan_medium[serialTNCTestChannel] = MediumSerialTNC
	audioConfig.serialTNCDevice[serialTNCTestChannel] = device
	audioConfig.serialTNCBaud[serialTNCTestChannel] = 9600

	var ctx, cancel = context.WithCancel(t.Context())

	var tncs = NewSerialTNCs(ctx, audioConfig, dlq.RecFrame)

	// The listener cannot be interrupted mid-read, so stop it, and let go of
	// the port, by hanging up the far end once it has been cancelled.
	t.Cleanup(func() {
		cancel()
		tnc.Close()
	})

	require.NotNil(t, tncs[serialTNCTestChannel])

	for channel, st := range tncs {
		if channel != serialTNCTestChannel {
			assert.Nil(t, st, "channel %d should not have a serial TNC", channel)
		}
	}

	// Started, too: what the TNC says reaches the received queue.
	var _, writeErr = tnc.Write(kissFrameFor(newTestPacket(t)))
	require.NoError(t, writeErr)

	assert.Equal(t, serialTNCTestChannel, nextDLQItem(t, dlq)._chan)
}
