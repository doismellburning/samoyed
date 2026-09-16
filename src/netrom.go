// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"strings"
	"time"
)

// netrom_config_s holds the NET/ROM configuration parsed from the config file.
type netrom_config_s struct {
	enabled       bool
	callsign      string
	alias         string // 6-char node alias, e.g. "MYNODE".
	channel       int
	ttl           byte
	nodesInterval int // seconds between NODES broadcasts; default 1800.
	// quality (0–255; default 192) is both what we advertise for this node in
	// our own NODES broadcasts and what we assume for a link to a neighbour
	// we hear one from.
	quality    byte
	qualitySet bool // true if QUALITY was explicitly configured, even as 0.
}

var saveNetromConfig *netrom_config_s //nolint:gochecknoglobals
var gNetromRouter *netromRouter       //nolint:gochecknoglobals
var gNetromLinkMgr *netromLinkManager //nolint:gochecknoglobals

// netrom_init initialises the NET/ROM subsystem.
func netrom_init(config *netrom_config_s) {
	if config == nil || !config.enabled {
		return
	}

	if !netromChannelUsable(config.channel) {
		return
	}

	saveNetromConfig = config

	if config.ttl == 0 {
		config.ttl = NETROM_TTL_DEFAULT
	}
	if config.nodesInterval == 0 {
		config.nodesInterval = 1800
	}
	if !config.qualitySet {
		config.quality = 192
	}

	gNetromRouter = newNetromRouter()
	gNetromLinkMgr = newNetromLinkManager(config.callsign, config.alias)

	// Start periodic NODES broadcast goroutine.
	go func() {
		var ticker = time.NewTicker(time.Duration(config.nodesInterval) * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			netromNodesCycle()
		}
	}()

	text_color_set(DW_COLOR_INFO)
	dw_printf("NET/ROM node %s (%s) initialised on channel %d\n", config.callsign, config.alias, config.channel)
}

// netromChannelUsable reports whether the configured channel can actually
// carry NET/ROM, complaining if it cannot.
//
// The config parser only bounds the channel number, because chan_medium[] is
// not populated until the whole config file has been read.  By the time
// netrom_init runs it is: tq_init stores the audio config, and is reached via
// NewXmitService well before netrom_init.  A radio channel and a network TNC
// both carry AX.25 frames and so can carry NET/ROM; an IGate channel is
// APRS-IS, where NET/ROM means nothing.
func netromChannelUsable(channel int) bool {
	if save_audio_config_p == nil {
		// Nothing to validate against, so we cannot establish that the channel
		// is usable.  Refuse rather than start a node that may transmit
		// nowhere.
		text_color_set(DW_COLOR_ERROR)
		dw_printf("NET/ROM: audio configuration not available, cannot start node on channel %d.\n", channel)

		return false
	}

	switch save_audio_config_p.chan_medium[channel] {
	case MEDIUM_RADIO, MEDIUM_NETTNC:
		return true
	case MEDIUM_IGATE:
		text_color_set(DW_COLOR_ERROR)
		dw_printf("NET/ROM: channel %d is an IGate channel, which cannot carry NET/ROM.\n", channel)

		return false
	case MEDIUM_NONE:
		text_color_set(DW_COLOR_ERROR)
		dw_printf("NET/ROM: channel %d is not configured, so no NET/ROM node was started.\n", channel)
		dw_printf("Configure it as a radio channel or an NCHANNEL network TNC first.\n")

		return false
	default:
		text_color_set(DW_COLOR_ERROR)
		dw_printf("NET/ROM: channel %d has a medium that cannot carry NET/ROM.\n", channel)

		return false
	}
}

// netrom_rx is called from app_process_rec_packet for frames with PID 0xCF.
func netrom_rx(fromChan int, pp *packet_t) {
	if saveNetromConfig == nil || !saveNetromConfig.enabled {
		return
	}

	if fromChan != saveNetromConfig.channel {
		return
	}

	var info = AX25GetInfo(pp)
	if len(info) == 0 {
		return
	}

	var dst = ax25_get_addr_with_ssid(pp, AX25_DESTINATION)
	dst = strings.TrimRight(dst, " ")

	if strings.EqualFold(dst, NETROM_BROADCAST_CALLSIGN) {
		// NODES routing broadcast.
		var bc, err = netromParseRoutingBroadcast(info)
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("NET/ROM: NODES parse error: %v\n", err)

			return
		}
		var fromNeighbor = ax25_get_addr_with_ssid(pp, AX25_SOURCE)
		gNetromRouter.processNodes(bc, fromNeighbor, saveNetromConfig.quality)

		return
	}

	// Transport frames are addressed to a specific neighbour at the AX.25
	// layer. One addressed to somebody else is merely overheard: acting on
	// it would forward a copy of traffic that is already being carried
	// elsewhere, and can bounce it straight back to the sender.
	if !strings.EqualFold(dst, saveNetromConfig.callsign) {
		return
	}

	// Transport frame.
	var f, err = netromParseTransportFrame(info)
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("NET/ROM: transport frame parse error: %v\n", err)

		return
	}

	// If this frame is not for our node and TTL allows, forward it.
	if !strings.EqualFold(f.net.dst, saveNetromConfig.callsign) {
		if f.net.ttl > 1 {
			f.net.ttl--
			var route, ok = gNetromRouter.lookup(f.net.dst)
			if ok {
				var forwarded = netromRebuildTransportFrame(f)
				netromTx(fromChan, route.neighbor, forwarded)
			}
		}

		return
	}

	var fromNeighbor = ax25_get_addr_with_ssid(pp, AX25_SOURCE)
	gNetromLinkMgr.rxFrame(fromChan, fromNeighbor, f)
}

// netromConfigTTL returns the TTL to use for outbound frames, falling back to
// NETROM_TTL_DEFAULT if the subsystem has not been initialised yet.
func netromConfigTTL() byte {
	if saveNetromConfig != nil && saveNetromConfig.ttl > 0 {
		return saveNetromConfig.ttl
	}

	return NETROM_TTL_DEFAULT
}

// netromTx transmits a NET/ROM payload as an AX.25 UI frame to a neighbor.
func netromTx(channel int, neighbor string, payload []byte) {
	if saveNetromConfig == nil {
		return
	}

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = neighbor
	addrs[AX25_SOURCE] = saveNetromConfig.callsign

	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_UI, 0, AX25_PID_NETROM, payload)
	if pp == nil {
		return
	}
	tq_append(channel, TQ_PRIO_1_LO, pp)
}

// netromNodesCycle is one turn of the periodic NODES work: age the routing
// table, then broadcast what is left of it.
//
// The aging belongs here, on our own clock, rather than in the handling of a
// received broadcast: a neighbour that has gone off the air sends nothing to
// age its routes with, so driving expiry from received broadcasts alone left
// its routes — and the traffic using them — in place for good.
func netromNodesCycle() {
	if gNetromRouter == nil {
		return
	}

	gNetromRouter.tick()
	netromSendNodes()
}

// netromSendNodes transmits a NODES broadcast on the configured channel.
func netromSendNodes() {
	if saveNetromConfig == nil || gNetromRouter == nil {
		return
	}

	var srcAlias = netromPadAlias(saveNetromConfig.alias)

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = NETROM_BROADCAST_CALLSIGN
	addrs[AX25_SOURCE] = saveNetromConfig.callsign

	// One frame per chunk: the whole table rarely fits in a single one, and
	// a payload past the AX.25 limit would be truncated mid-entry.
	for _, payload := range netromNodesPayloads(srcAlias, netromNodesEntries()) {
		var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_UI, 0, AX25_PID_NETROM, payload)
		if pp == nil {
			continue
		}
		tq_append(saveNetromConfig.channel, TQ_PRIO_1_LO, pp)
	}
}

// netromNodesEntries builds the entry list for a NODES broadcast: this node
// first, then everything we know how to reach.
//
// Advertising ourselves is what lets neighbours route to us at all — without
// it a node is invisible to the very stations that can hear it.
func netromNodesEntries() []netromNodesEntry {
	var snap = gNetromRouter.snapshot()
	var entries = make([]netromNodesEntry, 0, len(snap)+1)

	var self netromNodesEntry
	self.dstCallsign = saveNetromConfig.callsign
	self.dstAlias = netromPadAlias(saveNetromConfig.alias)
	self.neighbor = saveNetromConfig.callsign
	self.quality = saveNetromConfig.quality
	entries = append(entries, self)

	for _, r := range snap {
		if strings.EqualFold(r.dstCallsign, saveNetromConfig.callsign) {
			continue
		}
		var entry netromNodesEntry
		entry.dstCallsign = r.dstCallsign
		entry.dstAlias = netromPadAlias(r.dstAlias)
		entry.neighbor = r.neighbor
		entry.quality = r.quality
		entries = append(entries, entry)
	}

	return entries
}

// netromRebuildTransportFrame re-encodes a (possibly modified) transport frame.
// Used when forwarding frames with a decremented TTL.
func netromRebuildTransportFrame(f *netromTransportFrame) []byte {
	switch f.opcode {
	case netromOpcodeInfo:
		return netromBuildInfo(
			f.net.dst, f.net.src, f.net.ttl,
			f.cktIdx, f.cktID, f.txSeq, f.rxSeq,
			f.flags&netromFlagChoke != 0,
			f.flags&netromFlagNAK != 0,
			f.flags&netromFlagMore != 0,
			f.info,
		)
	case netromOpcodeInfoAck:
		return netromBuildInfoAck(
			f.net.dst, f.net.src, f.net.ttl,
			f.cktIdx, f.cktID, f.rxSeq,
			f.flags&netromFlagChoke != 0,
			f.flags&netromFlagNAK != 0,
		)
	case netromOpcodeConnect:
		return netromBuildConnect(
			f.net.dst, f.net.src, f.net.ttl,
			f.cktIdx, f.cktID,
			f.origIdx, f.origID,
			f.origCallsign, f.origAlias,
			f.dstCallsign, f.dstAlias,
			f.windowSize,
		)
	case netromOpcodeConnAck:
		return netromBuildConnAck(
			f.net.dst, f.net.src, f.net.ttl,
			f.cktIdx, f.cktID,
			f.acceptIdx, f.acceptID,
			f.windowSize,
			f.flags&netromFlagChoke != 0,
		)
	case netromOpcodeDisconnect:
		return netromBuildDisconnect(
			f.net.dst, f.net.src, f.net.ttl,
			f.cktIdx, f.cktID, f.rxSeq,
		)
	case netromOpcodeDiscAck:
		return netromBuildDiscAck(
			f.net.dst, f.net.src, f.net.ttl,
			f.cktIdx, f.cktID,
		)
	default:
		return nil
	}
}
