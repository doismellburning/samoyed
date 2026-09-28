// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

// An AXUDPCHANNEL talks to its peers over UDP, so these are loopback tests: a
// socket of the test's own stands in for each peer.

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const axudpTestChannel = 9

// startTestAXUDPChannel opens and starts an AXUDP channel on a free loopback
// port, routing to maps, and hands back the address to send it datagrams at.
func startTestAXUDPChannel(ctx context.Context, t *testing.T, maps []AXUDPMapEntry) (*AXUDPChannel, *net.UDPAddr) {
	t.Helper()

	var ac, err = NewAXUDPChannel(ctx, axudpTestChannel, 0, maps)
	require.NoError(t, err)

	ac.Start(ctx)

	var local, ok = ac.udpConn.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)

	return ac, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: local.Port, Zone: ""}
}

// nextReceivedFrame waits for the next frame on the received queue.
func nextReceivedFrame(t *testing.T) *dlq_item_t {
	t.Helper()

	var item *dlq_item_t

	require.Eventually(t, func() bool {
		item = dataLinkQueue.Remove()

		return item != nil
	}, 10*time.Second, 10*time.Millisecond, "the frame from the AXUDP peer never reached the received queue")

	return item
}

// A datagram from a peer is a frame off the air as far as the rest of the
// program is concerned, whether or not the peer put a checksum on it.
func TestAXUDPChannelReceivedFrameReachesTheQueue(t *testing.T) {
	for name, withCRC := range map[string]bool{"with checksum": true, "without checksum": false} {
		t.Run(name, func(t *testing.T) {
			expectReceivedFrames(t)

			var _, addr = startTestAXUDPChannel(t.Context(), t, nil)
			var peer, _ = axudpTestPeer(t)

			var pp = newTestPacket(t)
			var datagram = pp.FrameData()
			if withCRC {
				datagram = axudpAddCRC(datagram)
			}

			var _, writeErr = peer.WriteToUDP(datagram, addr)
			require.NoError(t, writeErr)

			var item = nextReceivedFrame(t)

			assert.Equal(t, axudpTestChannel, item._chan)
			assert.Equal(t, -3, item.subchan)
			assert.Equal(t, "AXUDP", item.spectrum)
			require.NotNil(t, item.pp)
			assert.Equal(t, pp.FrameData(), item.pp.FrameData())
		})
	}
}

// A datagram that is not an AX.25 frame is reported and goes no further.
func TestAXUDPChannelUndecodableDatagramIsReported(t *testing.T) {
	expectReceivedFrames(t)

	var hook = test.NewGlobal()
	t.Cleanup(hook.Reset)

	var _, addr = startTestAXUDPChannel(t.Context(), t, nil)
	var peer, _ = axudpTestPeer(t)

	var _, writeErr = peer.WriteToUDP([]byte("nonsense"), addr)
	require.NoError(t, writeErr)

	require.Eventually(t, func() bool {
		for _, e := range hook.AllEntries() {
			if e.Level == logrus.WarnLevel && e.Message == "Discarding AXUDP datagram that is not an AX.25 frame" {
				return true
			}
		}

		return false
	}, 10*time.Second, 10*time.Millisecond, "the undecodable datagram was not reported")

	assert.Nil(t, dataLinkQueue.Remove())
}

// Sending on an AXUDPCHANNEL sends the frame, checksummed, to the peer its
// destination is mapped to.
func TestAXUDPChannelSendPacket(t *testing.T) {
	var peer, peerAddr = axudpTestPeer(t)

	var ac, _ = startTestAXUDPChannel(t.Context(), t, []AXUDPMapEntry{
		{AX25Addr: "Q1TEST", Addr: peerAddr.String(), UDPAddr: peerAddr, Broadcast: false},
	})

	var pp = newTestPacket(t)

	ac.sendPacket(axudpTestChannel, pp)

	assert.Equal(t, axudpAddCRC(pp.FrameData()), axudpTestReceive(t, peer, 10*time.Second))
}

// A channel with no socket says so rather than falling over.
func TestAXUDPChannelSendPacketNoSocket(t *testing.T) {
	var ac *AXUDPChannel

	assert.NotPanics(t, func() { ac.sendPacket(axudpTestChannel, newTestPacket(t)) })
}

// Being told to stop closes the socket, even with nothing arriving on it.
func TestAXUDPChannelStopsWhenCancelled(t *testing.T) {
	var ctx, cancel = context.WithCancel(t.Context())

	var ac, _ = startTestAXUDPChannel(ctx, t, nil)

	cancel()

	require.Eventually(t, func() bool {
		return ac.udpConn.SetReadDeadline(time.Now()) != nil
	}, 10*time.Second, 10*time.Millisecond, "the AXUDP socket was not closed")
}

// AXUDPCHANNEL channels are opened at start up, and nothing else is - and a
// network TNC is not attached to for them, which with no address would fail
// and take the program down.
func TestAXUDPChannelInitOpensOnlyAXUDPChannels(t *testing.T) {
	var peer, peerAddr = axudpTestPeer(t)

	var audioConfig = new(AudioConfig)
	audioConfig.chan_medium[0] = MEDIUM_RADIO
	audioConfig.chan_medium[axudpTestChannel] = MEDIUM_AXUDP
	audioConfig.axudp_maps[axudpTestChannel] = []AXUDPMapEntry{
		{AX25Addr: "Q1TEST", Addr: peerAddr.String(), UDPAddr: peerAddr, Broadcast: false},
	}

	// Find a port that really is free, rather than hoping.
	var probe, _ = axudpTestPeer(t)
	audioConfig.axudp_port[axudpTestChannel] = probe.LocalAddr().(*net.UDPAddr).Port //nolint:forcetypeassert // A UDP socket has a UDP address.
	probe.Close()

	var tncs = NewNetTNCs(t.Context(), audioConfig)
	assert.Nil(t, tncs[axudpTestChannel], "an AXUDP channel should not have been attached to as a network TNC")

	var channels = NewAXUDPChannels(t.Context(), audioConfig)

	require.NotNil(t, channels[axudpTestChannel])
	assert.Nil(t, channels[0], "a radio channel should not have been opened as AXUDP")

	var pp = newTestPacket(t)

	channels[axudpTestChannel].sendPacket(axudpTestChannel, pp)

	assert.Equal(t, axudpAddCRC(pp.FrameData()), axudpTestReceive(t, peer, 10*time.Second))
}

// A packet queued for an AXUDPCHANNEL skips the radio queues and goes to the
// channel's peer.
func TestAXUDPChannelTransmitQueueSendsToItsPeers(t *testing.T) {
	var peer, peerAddr = axudpTestPeer(t)

	var ac, _ = startTestAXUDPChannel(t.Context(), t, []AXUDPMapEntry{
		{AX25Addr: "Q1TEST", Addr: peerAddr.String(), UDPAddr: peerAddr, Broadcast: false},
	})

	var audioConfig = new(AudioConfig)
	audioConfig.chan_medium[axudpTestChannel] = MEDIUM_AXUDP

	var tq = NewTransmitQueue()
	tq.Init(audioConfig)

	var channels [MAX_TOTAL_CHANS]*AXUDPChannel
	channels[axudpTestChannel] = ac

	tq.SetAXUDPChannels(channels)

	var pp = newTestPacket(t)

	var output = testutils.CaptureOutput(t, func() { tq.Append(axudpTestChannel, TQ_PRIO_1_LO, pp) })

	assert.Contains(t, output, ">ax")
	assert.Equal(t, axudpAddCRC(pp.FrameData()), axudpTestReceive(t, peer, 10*time.Second))
}

// Connected mode works over an AXUDPCHANNEL: there is no modem to seize, so
// the channel is ours at once, and data goes straight to the peer.
func TestAXUDPChannelConnectedMode(t *testing.T) {
	expectReceivedFrames(t)

	var peer, peerAddr = axudpTestPeer(t)

	var ac, _ = startTestAXUDPChannel(t.Context(), t, []AXUDPMapEntry{
		{AX25Addr: "Q1TEST", Addr: peerAddr.String(), UDPAddr: peerAddr, Broadcast: false},
	})

	var audioConfig = new(AudioConfig)
	audioConfig.chan_medium[axudpTestChannel] = MEDIUM_AXUDP

	var tq = NewTransmitQueue()
	tq.Init(audioConfig)

	var channels [MAX_TOTAL_CHANS]*AXUDPChannel
	channels[axudpTestChannel] = ac

	tq.SetAXUDPChannels(channels)

	tq.LMSeizeRequest(axudpTestChannel)

	var item = dataLinkQueue.Remove()
	require.NotNil(t, item, "the seize request was not confirmed")
	assert.Equal(t, DLQ_SEIZE_CONFIRM, item._type)
	assert.Equal(t, axudpTestChannel, item._chan)

	var pp = newTestPacket(t)

	testutils.CaptureOutput(t, func() { tq.LMDataRequest(axudpTestChannel, TQ_PRIO_1_LO, pp) })

	assert.Equal(t, axudpAddCRC(pp.FrameData()), axudpTestReceive(t, peer, 10*time.Second))
}
