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
	nodesInterval int  // seconds between NODES broadcasts; default 1800.
	quality       byte // quality advertised for this node (0–255); default 192.
}

var saveNetromConfig *netrom_config_s //nolint:gochecknoglobals
var gNetromRouter *netromRouter       //nolint:gochecknoglobals
var gNetromLinkMgr *netromLinkManager //nolint:gochecknoglobals

// netrom_init initialises the NET/ROM subsystem.
func netrom_init(config *netrom_config_s) {
	if config == nil || !config.enabled {
		return
	}

	saveNetromConfig = config

	if config.ttl == 0 {
		config.ttl = NETROM_TTL_DEFAULT
	}
	if config.nodesInterval == 0 {
		config.nodesInterval = 1800
	}
	if config.quality == 0 {
		config.quality = 192
	}

	gNetromRouter = newNetromRouter()
	gNetromLinkMgr = newNetromLinkManager(config.callsign, config.alias)

	// Start periodic NODES broadcast goroutine.
	go func() {
		var ticker = time.NewTicker(time.Duration(config.nodesInterval) * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			netromSendNodes()
		}
	}()

	text_color_set(DW_COLOR_INFO)
	dw_printf("NET/ROM node %s (%s) initialised on channel %d\n", config.callsign, config.alias, config.channel)
}

// netrom_rx is called from app_process_rec_packet for frames with PID 0xCF.
func netrom_rx(fromChan int, pp *packet_t) {
	if saveNetromConfig == nil || !saveNetromConfig.enabled {
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

	gNetromLinkMgr.rxFrame(fromChan, f)
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

// netromSendNodes transmits a NODES broadcast on the configured channel.
func netromSendNodes() {
	if saveNetromConfig == nil || gNetromRouter == nil {
		return
	}

	var snap = gNetromRouter.snapshot()
	var entries = make([]netromNodesEntry, 0, len(snap))
	for _, r := range snap {
		var entry netromNodesEntry
		entry.dstCallsign = r.dstCallsign
		entry.dstAlias = netromPadAlias(r.dstAlias)
		entry.neighbor = r.neighbor
		entry.quality = r.quality
		entries = append(entries, entry)
	}

	var srcAlias = netromPadAlias(saveNetromConfig.alias)
	var payload = netromBuildRoutingBroadcast(srcAlias, entries)

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = NETROM_BROADCAST_CALLSIGN
	addrs[AX25_SOURCE] = saveNetromConfig.callsign

	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_UI, 0, AX25_PID_NETROM, payload)
	if pp == nil {
		return
	}
	tq_append(saveNetromConfig.channel, TQ_PRIO_1_LO, pp)
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
