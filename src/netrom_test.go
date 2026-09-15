// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"
)

// netromTestFrameTo builds an AX.25 UI frame carrying a NET/ROM transport
// frame, addressed over the air to axDst and sent by axSrc.
func netromTestFrameTo(t *testing.T, axSrc, axDst string, payload []byte) *packet_t {
	t.Helper()

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = axDst
	addrs[AX25_SOURCE] = axSrc

	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_UI, 0, AX25_PID_NETROM, payload)
	if pp == nil {
		t.Fatal("failed to build test AX.25 UI frame")
	}

	return pp
}

// netromQueuedFrames reports how many frames are waiting to be transmitted on
// channel 0, which is where netromTx puts anything it decides to send.
func netromQueuedFrames() int {
	return tq_count(0, TQ_PRIO_1_LO, "", "", false)
}

// TestNetromRxIgnoresOverheardTransportFrame is the regression test for a
// frame amplifier: netrom_rx used to act on any NET/ROM transport frame it
// could decode, without checking who it was addressed to over the air. On a
// shared channel every node in earshot would forward a copy of traffic
// already being carried by the node it was actually sent to — and could send
// it straight back where it came from.
func TestNetromRxIgnoresOverheardTransportFrame(t *testing.T) {
	setupNetromTestEnv(t)

	// A route exists to the destination, so forwarding is possible and only
	// the addressing check can prevent it.
	gNetromRouter.routes[testNodeCallQ4] = &netromRouteEntry{
		dstCallsign: testNodeCallQ4,
		dstAlias:    "QNODE4",
		neighbor:    testNodeCallQ2,
		quality:     200,
		obsCount:    netromObsCountInit,
	}

	// Q2TEST → Q3TEST over the air; we are Q1TEST and merely overhear it.
	var payload = netromBuildInfo(testNodeCallQ4, testNodeCallQ3, NETROM_TTL_DEFAULT,
		0x11, 0x22, 0, 0, false, false, false, []byte("not ours"))
	netrom_rx(0, netromTestFrameTo(t, testNodeCallQ2, testNodeCallQ3, payload))

	if got := netromQueuedFrames(); got != 0 {
		t.Errorf("expected an overheard frame to be ignored, but %d frame(s) were queued for transmission", got)
	}
}

// TestNetromRxForwardsFrameAddressedToUs is the positive control for the
// check above: a frame genuinely sent to us for onward delivery must still be
// forwarded.
func TestNetromRxForwardsFrameAddressedToUs(t *testing.T) {
	setupNetromTestEnv(t)

	gNetromRouter.routes[testNodeCallQ4] = &netromRouteEntry{
		dstCallsign: testNodeCallQ4,
		dstAlias:    "QNODE4",
		neighbor:    testNodeCallQ2,
		quality:     200,
		obsCount:    netromObsCountInit,
	}

	var payload = netromBuildInfo(testNodeCallQ4, testNodeCallQ3, NETROM_TTL_DEFAULT,
		0x11, 0x22, 0, 0, false, false, false, []byte("please forward"))
	netrom_rx(0, netromTestFrameTo(t, testNodeCallQ2, testNodeCallQ1, payload))

	if got := netromQueuedFrames(); got != 1 {
		t.Errorf("expected a frame addressed to us to be forwarded once, got %d queued", got)
	}
}

// TestNetromNodesCyclePrunesRoutesViaSilentNeighbor is the regression test
// for a black hole. Aging used to happen only while handling a received NODES
// broadcast, and only for routes learned from the neighbour that sent it — so
// a neighbour that went off the air, sending nothing to age its routes with,
// kept them forever and everything addressed through it vanished.
func TestNetromNodesCyclePrunesRoutesViaSilentNeighbor(t *testing.T) {
	setupNetromTestEnv(t)

	var bc = makeTestBroadcast("QNODE2", []netromNodesEntry{
		{dstCallsign: testNodeCallQ3, dstAlias: netromPadAlias("QNODE3"), neighbor: testNodeCallQ2, quality: 200},
	})
	gNetromRouter.processNodes(bc, testNodeCallQ2, 200)

	if _, ok := gNetromRouter.lookup(testNodeCallQ3); !ok {
		t.Fatal("expected a route to Q3TEST after the broadcast")
	}

	// Q2TEST now goes silent. Only our own broadcast cycles pass.
	for range netromObsCountInit {
		netromNodesCycle()
	}

	if _, ok := gNetromRouter.lookup(testNodeCallQ3); ok {
		t.Error("a route via a neighbour that stopped broadcasting should expire on our own cycles")
	}
}

// TestNetromNodesEntriesAdvertisesSelf is the regression test for a node that
// nobody could reach: NODES broadcasts used to contain only what we had
// learned from others, never this node, so neighbours never learned a route
// to the very station they were hearing.
func TestNetromNodesEntriesAdvertisesSelf(t *testing.T) {
	setupNetromTestEnv(t)
	saveNetromConfig.quality = 210

	var entries = netromNodesEntries()

	if len(entries) == 0 {
		t.Fatal("expected a NODES broadcast to contain at least this node")
	}
	var self = entries[0]
	if self.dstCallsign != testNodeCallQ1 {
		t.Errorf("first entry: got %q, want this node %q", self.dstCallsign, testNodeCallQ1)
	}
	if self.neighbor != testNodeCallQ1 {
		t.Errorf("self entry neighbour: got %q, want this node itself", self.neighbor)
	}
	if self.quality != 210 {
		t.Errorf("self entry quality: got %d, want the configured 210", self.quality)
	}
}

// TestNetromNodesEntriesDoesNotDuplicateSelf checks that a route to ourselves
// learned from somebody else does not produce a second self entry.
func TestNetromNodesEntriesDoesNotDuplicateSelf(t *testing.T) {
	setupNetromTestEnv(t)

	gNetromRouter.routes[testNodeCallQ1] = &netromRouteEntry{
		dstCallsign: testNodeCallQ1,
		dstAlias:    "QNODE1",
		neighbor:    testNodeCallQ2,
		quality:     100,
		obsCount:    netromObsCountInit,
	}

	var entries = netromNodesEntries()

	var count = 0
	for _, e := range entries {
		if e.dstCallsign == testNodeCallQ1 {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one entry for this node, got %d", count)
	}
}

// TestNetromNodesPayloadsChunksLargeTable is the regression test for a
// routing table that outgrew its frame: every entry used to go into a single
// payload, so past about 97 routes the frame exceeded what an AX.25
// information field can hold and was truncated mid-entry on every cycle.
func TestNetromNodesPayloadsChunksLargeTable(t *testing.T) {
	var entries = make([]netromNodesEntry, 0, 250)
	for i := range 250 {
		var entry netromNodesEntry
		entry.dstCallsign = "Q1TEST"
		entry.dstAlias = netromPadAlias("QNODE1")
		entry.neighbor = "Q2TEST"
		entry.quality = byte(i)
		entries = append(entries, entry)
	}

	var payloads = netromNodesPayloads(netromPadAlias("QNODE1"), entries)

	var total = 0
	for i, p := range payloads {
		if len(p) > AX25_MAX_INFO_LEN {
			t.Errorf("payload %d is %d bytes, over the %d byte AX.25 limit", i, len(p), AX25_MAX_INFO_LEN)
		}

		var bc, err = netromParseRoutingBroadcast(p)
		if err != nil {
			t.Fatalf("payload %d does not parse back: %v", i, err)
		}
		if len(bc.entries) > netromNodesPerFrame {
			t.Errorf("payload %d carries %d entries, over the %d per frame limit",
				i, len(bc.entries), netromNodesPerFrame)
		}
		total += len(bc.entries)
	}

	if total != len(entries) {
		t.Errorf("chunking lost entries: %d survived the round trip, want %d", total, len(entries))
	}
}

// TestNetromNodesPayloadsEmptyTableStillAnnounces checks that a node with an
// empty routing table still sends a broadcast, since that broadcast is how
// neighbours learn it exists.
func TestNetromNodesPayloadsEmptyTableStillAnnounces(t *testing.T) {
	var payloads = netromNodesPayloads(netromPadAlias("QNODE1"), nil)

	if len(payloads) != 1 {
		t.Fatalf("expected one payload for an empty table, got %d", len(payloads))
	}
	var _, err = netromParseRoutingBroadcast(payloads[0])
	if err != nil {
		t.Errorf("empty-table payload does not parse: %v", err)
	}
}

// --- netrom_init channel medium validation ---

// netromInitTestConfig returns a NET/ROM config for channel, with a NODES
// interval long enough that the broadcast goroutine never fires mid-test.
func netromInitTestConfig(channel int) *netrom_config_s {
	var config = new(netrom_config_s)
	config.enabled = true
	config.callsign = testNodeCallQ1
	config.alias = "QNODE1"
	config.channel = channel
	config.nodesInterval = 86400

	return config
}

// netromInitTestSetup points the audio config at a single channel of the given
// medium, and restores every global netrom_init touches afterwards.
func netromInitTestSetup(t *testing.T, channel int, medium medium_e) {
	t.Helper()

	var prevAudio = save_audio_config_p
	var prevConfig = saveNetromConfig
	var prevRouter = gNetromRouter
	var prevLinkMgr = gNetromLinkMgr

	t.Cleanup(func() {
		save_audio_config_p = prevAudio
		saveNetromConfig = prevConfig
		gNetromRouter = prevRouter
		gNetromLinkMgr = prevLinkMgr
	})

	saveNetromConfig = nil
	gNetromRouter = nil
	gNetromLinkMgr = nil

	var audio = new(audio_s)
	audio.chan_medium[channel] = medium
	save_audio_config_p = audio
}

// TestNetromInitRejectsUnusableChannel covers the channels a NET/ROM node must
// refuse to start on.  The config parser only bounds the channel number, since
// chan_medium[] is not populated while the config file is still being read, so
// this is the check that actually keeps a node off a channel that cannot carry
// it.
func TestNetromInitRejectsUnusableChannel(t *testing.T) {
	var tests = []struct {
		name   string
		medium medium_e
	}{
		{"unconfigured channel", MEDIUM_NONE},
		{"igate channel", MEDIUM_IGATE},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			netromInitTestSetup(t, 0, tt.medium)

			netrom_init(netromInitTestConfig(0))

			if saveNetromConfig != nil {
				t.Error("no NET/ROM config should have been stored")
			}
			if gNetromRouter != nil || gNetromLinkMgr != nil {
				t.Error("no NET/ROM state should have been created")
			}
		})
	}
}

// TestNetromInitRejectsChannelWithoutAudioConfig guards the case where the
// audio config has not been stored at all: there is then nothing to validate
// the channel against, and starting a node that might transmit nowhere is
// worse than not starting one.
func TestNetromInitRejectsChannelWithoutAudioConfig(t *testing.T) {
	netromInitTestSetup(t, 0, MEDIUM_RADIO)
	save_audio_config_p = nil

	netrom_init(netromInitTestConfig(0))

	if saveNetromConfig != nil || gNetromRouter != nil || gNetromLinkMgr != nil {
		t.Error("expected no NET/ROM node without an audio configuration")
	}
}

// TestNetromInitAcceptsUsableChannel covers the two media that carry AX.25
// frames and so can carry NET/ROM.  MEDIUM_NETTNC is the one that matters for
// reaching an AXUDP link, via an NCHANNEL pointed at samoyed-axudp, and it
// lives above MAX_RADIO_CHANS.
func TestNetromInitAcceptsUsableChannel(t *testing.T) {
	var tests = []struct {
		name    string
		channel int
		medium  medium_e
	}{
		{"radio channel", 0, MEDIUM_RADIO},
		{"network tnc channel", MAX_RADIO_CHANS, MEDIUM_NETTNC},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			netromInitTestSetup(t, tt.channel, tt.medium)

			netrom_init(netromInitTestConfig(tt.channel))

			if saveNetromConfig == nil {
				t.Fatal("expected the NET/ROM config to be stored")
			}
			if saveNetromConfig.channel != tt.channel {
				t.Errorf("channel = %d, want %d", saveNetromConfig.channel, tt.channel)
			}
			if gNetromRouter == nil || gNetromLinkMgr == nil {
				t.Error("expected NET/ROM router and link manager to be created")
			}
		})
	}
}
