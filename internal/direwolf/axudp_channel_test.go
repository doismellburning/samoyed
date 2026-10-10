// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

// An AXUDP channel talks to other nodes in UDP datagrams, so everything here
// is a loopback test: sockets on 127.0.0.1 stand in for the other nodes.

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/axudp"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const axudpTestChannel = 10

// noAXUDPRoutes routes nowhere, for a channel whose test sends nothing.
func noAXUDPRoutes() axudp.Routes {
	var routes axudp.Routes

	return routes
}

// newTestAXUDPPeer listens on a free UDP port, playing another node.
func newTestAXUDPPeer(t *testing.T) *net.UDPConn {
	t.Helper()

	var pc, err = new(net.ListenConfig).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	t.Cleanup(func() { pc.Close() })

	var conn, ok = pc.(*net.UDPConn)
	require.True(t, ok)

	return conn
}

// axudpTestMap maps ax25addr to peer.
func axudpTestMap(t *testing.T, ax25addr string, peer *net.UDPConn, broadcast bool) axudp.MapEntry {
	t.Helper()

	var addr, ok = peer.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)

	return axudp.MapEntry{AX25Addr: ax25addr, Addr: addr.String(), UDPAddr: addr, Broadcast: broadcast}
}

// openTestAXUDPChannel opens channel axudpTestChannel on a free port, sending
// by routes, and starts it.
func openTestAXUDPChannel(ctx context.Context, t *testing.T, routes axudp.Routes) *AXUDPChannel {
	t.Helper()

	var ac, err = NewAXUDPChannel(ctx, axudpTestChannel, 0, routes, dataLinkQueue.RecFrame)
	require.NoError(t, err)

	t.Cleanup(func() { ac.conn.Close() })

	ac.Start(ctx)

	return ac
}

// testAXUDPChannelAddr is where a peer on 127.0.0.1 reaches ac.  The channel
// listens on every address, which it gives as [::] where there is IPv6, and
// an IPv4 socket cannot send there.
func testAXUDPChannelAddr(t *testing.T, ac *AXUDPChannel) *net.UDPAddr {
	t.Helper()

	var local, ok = ac.conn.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)

	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: local.Port, Zone: ""}
}

// readTestAXUDPDatagram waits for the next datagram to peer.
func readTestAXUDPDatagram(t *testing.T, peer *net.UDPConn) []byte {
	t.Helper()

	require.NoError(t, peer.SetReadDeadline(time.Now().Add(10*time.Second)))

	var buf = make([]byte, axudp.MaxUDPPayload)
	var n, _, err = peer.ReadFromUDP(buf)
	require.NoError(t, err, "no AXUDP datagram arrived")

	return buf[:n]
}

// assertNoTestAXUDPDatagram checks nothing arrives at peer for a moment.
func assertNoTestAXUDPDatagram(t *testing.T, peer *net.UDPConn) {
	t.Helper()

	require.NoError(t, peer.SetReadDeadline(time.Now().Add(100*time.Millisecond)))

	var _, _, err = peer.ReadFromUDP(make([]byte, axudp.MaxUDPPayload))
	assert.Error(t, err, "an AXUDP datagram arrived where none should have")
}

// A datagram from another node is a frame received on the AXUDP channel, with
// or without the checksum most AXUDP implementations append.
func TestAXUDPChannelReceivedFrameReachesTheQueue(t *testing.T) {
	for _, withCRC := range []bool{true, false} {
		t.Run(map[bool]string{true: "with CRC", false: "without CRC"}[withCRC], func(t *testing.T) {
			expectReceivedFrames(t)

			var ac = openTestAXUDPChannel(t.Context(), t, noAXUDPRoutes())
			var peer = newTestAXUDPPeer(t)

			var pp = newTestPacket(t)

			var datagram = pp.FrameData()
			if withCRC {
				datagram = axudp.AddCRC(datagram)
			}

			var _, err = peer.WriteTo(datagram, testAXUDPChannelAddr(t, ac))
			require.NoError(t, err)

			var item *dlq_item_t

			require.Eventually(t, func() bool {
				item = dataLinkQueue.Remove()

				return item != nil
			}, 10*time.Second, 10*time.Millisecond, "the frame never reached the received queue")

			assert.Equal(t, axudpTestChannel, item._chan)
			assert.Equal(t, -4, item.subchan)
			assert.Equal(t, "AXUDP", item.spectrum)
			require.NotNil(t, item.pp)
			assert.Equal(t, pp.FrameData(), item.pp.FrameData())
		})
	}
}

// A datagram that is not an AX.25 frame is dropped, and the channel carries on.
func TestAXUDPChannelUndecodableDatagramIsDropped(t *testing.T) {
	expectReceivedFrames(t)

	var ac = openTestAXUDPChannel(t.Context(), t, noAXUDPRoutes())
	var peer = newTestAXUDPPeer(t)

	var _, err = peer.WriteTo([]byte("junk"), testAXUDPChannelAddr(t, ac))
	require.NoError(t, err)

	var pp = newTestPacket(t)
	_, err = peer.WriteTo(pp.FrameData(), testAXUDPChannelAddr(t, ac))
	require.NoError(t, err)

	var item *dlq_item_t

	require.Eventually(t, func() bool {
		item = dataLinkQueue.Remove()

		return item != nil
	}, 10*time.Second, 10*time.Millisecond, "the frame after the junk never reached the received queue")

	assert.Equal(t, pp.FrameData(), item.pp.FrameData())
	assert.Nil(t, dataLinkQueue.Remove(), "the junk was taken for a frame")
}

// Sending goes to the node mapped for the destination, checksum appended.
func TestAXUDPChannelSendPacket(t *testing.T) {
	var peer = newTestAXUDPPeer(t)
	var other = newTestAXUDPPeer(t)

	var routes = noAXUDPRoutes()
	routes.Maps = []axudp.MapEntry{
		axudpTestMap(t, "Q1TEST", peer, false),
		axudpTestMap(t, "Q3TEST", other, false),
	}

	var ac = openTestAXUDPChannel(t.Context(), t, routes)

	var pp = newTestPacket(t) // To Q1TEST.

	ac.sendPacket(axudpTestChannel, pp)

	var frame, ok = axudp.StripCRC(readTestAXUDPDatagram(t, peer))
	require.True(t, ok, "the datagram's checksum is wrong or missing")
	assert.Equal(t, pp.FrameData(), frame)

	assertNoTestAXUDPDatagram(t, other)
}

// A frame for a broadcast address goes to every node marked for broadcasts.
func TestAXUDPChannelSendBroadcast(t *testing.T) {
	var peers = []*net.UDPConn{newTestAXUDPPeer(t), newTestAXUDPPeer(t)}
	var unmarked = newTestAXUDPPeer(t)

	var routes = noAXUDPRoutes()
	routes.Broadcast = []string{"NODES"}
	routes.Maps = []axudp.MapEntry{
		axudpTestMap(t, "Q1TEST", peers[0], true),
		axudpTestMap(t, "Q2TEST", peers[1], true),
		axudpTestMap(t, "Q3TEST", unmarked, false),
	}

	var ac = openTestAXUDPChannel(t.Context(), t, routes)

	var pp = ax25.FromText("Q3TEST>NODES:hello", true)
	require.NotNil(t, pp)

	ac.sendPacket(axudpTestChannel, pp)

	for i, peer := range peers {
		var frame, ok = axudp.StripCRC(readTestAXUDPDatagram(t, peer))
		require.True(t, ok, "peer %d", i)
		assert.Equal(t, pp.FrameData(), frame, "peer %d", i)
	}

	assertNoTestAXUDPDatagram(t, unmarked)
}

// A frame for somewhere nobody is mapped to goes nowhere, and says so.
func TestAXUDPChannelSendUnrouted(t *testing.T) {
	var ac = openTestAXUDPChannel(t.Context(), t, noAXUDPRoutes())

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	ac.sendPacket(axudpTestChannel, newTestPacket(t))

	var entry = hook.LastEntry()
	require.NotNil(t, entry)
	assert.Equal(t, logrus.WarnLevel, entry.Level)
	assert.Equal(t, "Q1TEST", entry.Data["dest"])
}

// A channel that could not be opened discards what is sent on it.
func TestAXUDPChannelSendPacketNoChannel(t *testing.T) {
	var ac *AXUDPChannel

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	ac.sendPacket(axudpTestChannel, newTestPacket(t))

	var entry = hook.LastEntry()
	require.NotNil(t, entry)
	assert.Equal(t, logrus.ErrorLevel, entry.Level)
}

// Starting twice would split what arrives between two readers.
func TestAXUDPChannelStartedTwiceComplains(t *testing.T) {
	var ac = openTestAXUDPChannel(t.Context(), t, noAXUDPRoutes())

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	ac.Start(t.Context())

	var entry = hook.LastEntry()
	require.NotNil(t, entry)
	assert.Contains(t, entry.Message, "started twice")
}

// Cancelling closes the socket, freeing the port.
func TestAXUDPChannelStopsWhenCancelled(t *testing.T) {
	var ctx, cancel = context.WithCancel(t.Context())

	var ac = openTestAXUDPChannel(ctx, t, noAXUDPRoutes())
	var addr = ac.conn.LocalAddr().String()

	cancel()

	require.Eventually(t, func() bool {
		var pc, err = new(net.ListenConfig).ListenPacket(t.Context(), "udp", addr)
		if err != nil {
			return false
		}

		pc.Close()

		return true
	}, 10*time.Second, 10*time.Millisecond, "the AXUDP socket was not closed when the channel was stopped")
}

// Two channels cannot share a port, and the second saying so is better than
// silently hearing nothing.
func TestAXUDPChannelPortInUse(t *testing.T) {
	var ac = openTestAXUDPChannel(t.Context(), t, noAXUDPRoutes())

	var port = testAXUDPChannelAddr(t, ac).Port

	var second, err = NewAXUDPChannel(t.Context(), axudpTestChannel+1, port, noAXUDPRoutes(), dataLinkQueue.RecFrame)
	require.Error(t, err)
	assert.Nil(t, second)
}

// The transmit queue hands a packet for an AXUDP channel to that channel.
func TestAXUDPChannelTransmitQueue(t *testing.T) {
	var peer = newTestAXUDPPeer(t)

	var routes = noAXUDPRoutes()
	routes.Maps = []axudp.MapEntry{axudpTestMap(t, "Q1TEST", peer, false)}

	var ac = openTestAXUDPChannel(t.Context(), t, routes)

	var audio = new(RadioConfig)
	audio.chan_medium[axudpTestChannel] = MEDIUM_AXUDP

	var channels [MAX_TOTAL_CHANS]*AXUDPChannel
	channels[axudpTestChannel] = ac

	var tq = NewTransmitQueue()
	tq.Init(audio)
	tq.SetAXUDPChannels(channels)

	var pp = newTestPacket(t)
	var want = pp.FrameData()

	tq.Append(axudpTestChannel, TQ_PRIO_1_LO, pp)

	var frame, ok = axudp.StripCRC(readTestAXUDPDatagram(t, peer))
	require.True(t, ok)
	assert.Equal(t, want, frame)
}

// Connected mode works over AXUDP as it does over a network TNC: a frame from
// the data link goes straight out, and a seize is confirmed at once, there
// being no radio channel to wait for.
func TestAXUDPChannelConnectedMode(t *testing.T) {
	expectReceivedFrames(t)

	var peer = newTestAXUDPPeer(t)

	var routes = noAXUDPRoutes()
	routes.Maps = []axudp.MapEntry{axudpTestMap(t, "Q1TEST", peer, false)}

	var ac = openTestAXUDPChannel(t.Context(), t, routes)

	var audio = new(RadioConfig)
	audio.chan_medium[axudpTestChannel] = MEDIUM_AXUDP

	var channels [MAX_TOTAL_CHANS]*AXUDPChannel
	channels[axudpTestChannel] = ac

	var tq = NewTransmitQueue()
	tq.Init(audio)
	tq.SetAXUDPChannels(channels)
	tq.SetSeizeConfirm(dataLinkQueue.SeizeConfirm)

	var pp = newTestPacket(t)
	var want = pp.FrameData()

	tq.LMDataRequest(axudpTestChannel, TQ_PRIO_1_LO, pp)

	var frame, ok = axudp.StripCRC(readTestAXUDPDatagram(t, peer))
	require.True(t, ok)
	assert.Equal(t, want, frame)

	tq.LMSeizeRequest(axudpTestChannel)

	var item = dataLinkQueue.Remove()
	require.NotNil(t, item, "the seize was not confirmed")
	assert.Equal(t, DLQ_SEIZE_CONFIRM, item._type)
	assert.Equal(t, axudpTestChannel, item._chan)
}

// Anyone who can reach the port can send junk, so turning it away is not
// worth a warning each time.
func TestAXUDPChannelJunkIsNotWarnedAbout(t *testing.T) {
	expectReceivedFrames(t)

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	var ac = openTestAXUDPChannel(t.Context(), t, noAXUDPRoutes())

	for _, junk := range [][]byte{[]byte("x"), make([]byte, ax25.MaxPacketLen+1)} {
		ac.receive(junk, new(net.UDPAddr))
	}

	for _, entry := range hook.AllEntries() {
		assert.Greater(t, entry.Level, logrus.WarnLevel, "logged at %s: %s", entry.Level, entry.Message)
	}

	assert.Nil(t, dataLinkQueue.Remove())
}

// Sending on a channel whose socket has been closed on the way out is not an
// error worth reporting.
func TestAXUDPChannelSendAfterCloseIsQuiet(t *testing.T) {
	var peer = newTestAXUDPPeer(t)

	var routes = noAXUDPRoutes()
	routes.Maps = []axudp.MapEntry{axudpTestMap(t, "Q1TEST", peer, false)}

	// Not started: a listener would see the close too, and say so at Error
	// whenever it got round to it, which is not what is under test.
	var ac, err = NewAXUDPChannel(t.Context(), axudpTestChannel, 0, routes, dataLinkQueue.RecFrame)
	require.NoError(t, err)
	require.NoError(t, ac.conn.Close())

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	ac.sendPacket(axudpTestChannel, newTestPacket(t))

	for _, entry := range hook.AllEntries() {
		assert.Greater(t, entry.Level, logrus.WarnLevel, "logged at %s: %s", entry.Level, entry.Message)
	}
}
