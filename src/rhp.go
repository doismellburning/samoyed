// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:	Provide service to other applications via the Remote
 *		Host Protocol Version 2 (RHP2), as described in
 *		https://wiki.oarc.uk/packet:white-papers:pwp-0222
 *
 * Description:	Listens on a configurable TCP port (disabled by default;
 *		set RHPPORT in configuration to enable).  Client applications
 *		connect and send/receive JSON messages framed with a 2-byte
 *		big-endian length header.
 *
 *		Supported protocol family: AX25
 *		Supported modes: dgram (UI frames), stream (connected),
 *		                 trace (decoded monitoring), raw (binary)
 *
 *		Connected-mode (stream) sockets use the same ax25_link
 *		state machine as the AGW server.  The "client" IDs used
 *		with dlq_* functions are offset by MAX_NET_CLIENTS to
 *		avoid collisions with AGW client indices.
 *
 *------------------------------------------------------------------*/

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
)

// RHP2 error codes from the spec.
const (
	rhpErrOk            = 0
	rhpErrInvalidHandle = 3
	rhpErrBadParam      = 12
)

// RHP2 status flags (STATUS message, server→client).
const (
	rhpFlagConnected = 2 // Downlink is connected
)

// RHP2 OPEN flags.
const (
	rhpOpenFlagActiveOpen = 0x80
)

// RHP2 TRACE flags.
const (
	rhpTraceFlagIncoming = 0x01
)

// rhpAuthReply is the server response to an AUTH message.
type rhpAuthReply struct {
	Type    string `json:"type"`
	ID      int    `json:"id,omitempty"`
	ErrCode int    `json:"errCode"`
	ErrText string `json:"errText"`
}

// rhpOpenReply is the server response to an OPEN message.
type rhpOpenReply struct {
	Type    string `json:"type"`
	ID      int    `json:"id,omitempty"`
	Handle  int    `json:"handle"`
	ErrCode int    `json:"errcode"`
	ErrText string `json:"errtext"`
}

// rhpCloseReply is the server response to a client-initiated CLOSE message.
type rhpCloseReply struct {
	Type    string `json:"type"`
	ID      int    `json:"id,omitempty"`
	Handle  int    `json:"handle"`
	ErrCode int    `json:"errcode"`
	ErrText string `json:"errtext"`
}

// rhpSendReply is the server response to a SEND message.
type rhpSendReply struct {
	Type    string `json:"type"`
	ID      int    `json:"id,omitempty"`
	Handle  int    `json:"handle"`
	ErrCode int    `json:"errcode"`
	ErrText string `json:"errtext"`
	Status  int    `json:"status,omitempty"`
}

// rhpAcceptMsg is the server-initiated ACCEPT notification (incoming connection).
type rhpAcceptMsg struct {
	Type   string `json:"type"`
	SeqNo  int    `json:"seqno"`
	Handle int    `json:"handle"`
	Remote string `json:"remote"`
	Local  string `json:"local"`
	Port   int    `json:"port"`
}

// rhpStatusMsg is the server-initiated STATUS notification.
type rhpStatusMsg struct {
	Type   string `json:"type"`
	SeqNo  int    `json:"seqno"`
	Handle int    `json:"handle"`
	Flags  int    `json:"flags"`
}

// rhpCloseMsg is the server-initiated CLOSE notification (link terminated).
type rhpCloseMsg struct {
	Type   string `json:"type"`
	SeqNo  int    `json:"seqno"`
	Handle int    `json:"handle"`
}

// rhpRecvMsg is the server-initiated RECV notification for connected-mode data.
type rhpRecvMsg struct {
	Type   string `json:"type"`
	SeqNo  int    `json:"seqno"`
	Handle int    `json:"handle"`
	Data   string `json:"data"`
}

// rhpTraceRecv is the server-initiated RECV notification for trace-mode frames.
type rhpTraceRecv struct {
	Type      string `json:"type"`
	SeqNo     int    `json:"seqno"`
	Handle    int    `json:"handle"`
	Action    string `json:"action"`
	Port      string `json:"port"`
	Srce      string `json:"srce"`
	Dest      string `json:"dest"`
	Ctrl      int    `json:"ctrl"`
	FrameType string `json:"frametype"`
}

// rhpRawRecv is the server-initiated RECV notification for raw-mode frames.
type rhpRawRecv struct {
	Type   string `json:"type"`
	SeqNo  int    `json:"seqno"`
	Handle int    `json:"handle"`
	Action string `json:"action"`
	Port   string `json:"port"`
	Data   string `json:"data"`
}

// rhpDgramRecv is the server-initiated RECV notification for dgram-mode frames.
type rhpDgramRecv struct {
	Type   string `json:"type"`
	SeqNo  int    `json:"seqno"`
	Handle int    `json:"handle"`
	Port   string `json:"port"`
	Data   string `json:"data"`
}

// rhpSocket represents one virtual socket held by an RHP client.
type rhpSocket struct {
	handle int
	pfam   string // "ax25"
	mode   string // "dgram", "stream", "trace", "raw"
	port   int    // radio channel (-1 = all)
	local  string // local callsign, uppercase
	remote string // remote callsign (for stream connect), uppercase
	flags  int    // open/trace flags
	linked bool   // dlq_register_callsign has been called (stream passive)
}

// rhpClient represents one TCP connection to the RHP server.
type rhpClient struct {
	conn       net.Conn
	mu         sync.Mutex
	sockets    map[int]*rhpSocket
	nextHandle int
}

// RHPService is the top-level RHP2 server.
type RHPService struct {
	port  int
	mu    sync.Mutex
	cs    []*rhpClient
	seqno atomic.Int32
}

// NewRHPService creates a new RHP service.  If the configured port is 0
// the service is disabled (all methods are no-ops).
func NewRHPService(mc *misc_config_s) *RHPService {
	var svc = new(RHPService)
	svc.port = mc.rhp_port
	if svc.port == 0 {
		text_color_set(DW_COLOR_INFO)
		dw_printf("RHP2: Disabled.  Use RHPPORT in configuration to enable.\n")
		return svc
	}
	text_color_set(DW_COLOR_INFO)
	dw_printf("RHP2: Listening on TCP port %d.\n", svc.port)
	go svc.listen()
	return svc
}

// LinkEstablished is called when an AX.25 connection has been established.
// It sends a STATUS (connected) to the socket owner.
// For incoming connections (passive listener), it also sends an ACCEPT.
func (svc *RHPService) LinkEstablished(channel int, rhpHandle int, remote string, own string, incoming bool) {
	if svc.port == 0 {
		return
	}
	var c, sock = svc.findClientByRHPHandle(rhpHandle)
	if c == nil {
		return
	}

	if incoming {
		// The socket is a passive listener.  The connection arrived on it.
		// Update remote address on the socket.
		c.mu.Lock()
		sock.remote = strings.ToUpper(remote)
		c.mu.Unlock()

		var msg = rhpAcceptMsg{
			Type:   "accept",
			SeqNo:  svc.incSeqno(),
			Handle: sock.handle,
			Remote: remote,
			Local:  own,
			Port:   channel,
		}
		writeRHPMsg(c, msg) //nolint:errcheck
	}

	// Send STATUS: CONNECTED.
	var status = rhpStatusMsg{
		Type:   "status",
		SeqNo:  svc.incSeqno(),
		Handle: sock.handle,
		Flags:  rhpFlagConnected,
	}
	writeRHPMsg(c, status) //nolint:errcheck
}

// LinkTerminated is called when an AX.25 connection has been terminated.
// It sends a server-initiated CLOSE to the socket owner.
func (svc *RHPService) LinkTerminated(_ int, rhpHandle int, _ string, _ string, _ bool) {
	if svc.port == 0 {
		return
	}
	var c, sock = svc.findClientByRHPHandle(rhpHandle)
	if c == nil {
		return
	}

	// Remove the socket from the client's map since the link is gone.
	c.mu.Lock()
	delete(c.sockets, rhpHandle)
	c.mu.Unlock()

	var msg = rhpCloseMsg{
		Type:   "close",
		SeqNo:  svc.incSeqno(),
		Handle: sock.handle,
	}
	writeRHPMsg(c, msg) //nolint:errcheck
}

// RecConnData is called when connected data has been received from the remote station.
// It sends a RECV message to the socket owner.
func (svc *RHPService) RecConnData(_ int, rhpHandle int, _ string, _ string, _ int, data []byte) {
	if svc.port == 0 {
		return
	}
	var c, sock = svc.findClientByRHPHandle(rhpHandle)
	if c == nil {
		return
	}

	var msg = rhpRecvMsg{
		Type:   "recv",
		SeqNo:  svc.incSeqno(),
		Handle: sock.handle,
		Data:   string(data),
	}
	writeRHPMsg(c, msg) //nolint:errcheck
}

// SendRecFrame is called from app_process_rec_packet for every received frame.
// It routes the frame to matching sockets across all connected RHP clients.
func (svc *RHPService) SendRecFrame(channel int, pp *packet_t, fbuf []byte) {
	if svc.port == 0 {
		return
	}
	if pp == nil {
		return
	}

	var src = ax25_get_addr_with_ssid(pp, AX25_SOURCE)
	var dst = ax25_get_addr_with_ssid(pp, AX25_DESTINATION)
	var info = ax25_get_info(pp)

	// Get frame type description for TRACE mode.
	var _, ftypeDesc, _, _, _, _ = ax25_frame_type(pp)

	// Get the control byte from the raw frame (after the addresses).
	var ctrl = 0
	if len(fbuf) > ax25_get_num_addr(pp)*7 {
		ctrl = int(fbuf[ax25_get_num_addr(pp)*7])
	}

	svc.mu.Lock()
	var clients = make([]*rhpClient, len(svc.cs))
	copy(clients, svc.cs)
	svc.mu.Unlock()

	for _, c := range clients {
		c.mu.Lock()
		var sockets = make([]*rhpSocket, 0, len(c.sockets))
		for _, s := range c.sockets {
			sockets = append(sockets, s)
		}
		c.mu.Unlock()

		for _, sock := range sockets {
			if !channelMatches(sock.port, channel) {
				continue
			}

			switch sock.mode {
			case "trace":
				// Deliver decoded frame metadata.
				// 0x01 = incoming frames flag; all frames here are received.
				if sock.flags&rhpTraceFlagIncoming == 0 {
					continue
				}
				var msg = rhpTraceRecv{
					Type:      "recv",
					SeqNo:     svc.incSeqno(),
					Handle:    sock.handle,
					Action:    "rcvd",
					Port:      fmt.Sprintf("%d", channel),
					Srce:      src,
					Dest:      dst,
					Ctrl:      ctrl,
					FrameType: ftypeDesc,
				}
				writeRHPMsg(c, msg) //nolint:errcheck

			case "raw":
				// Deliver raw frame bytes as base64.
				if sock.flags&rhpTraceFlagIncoming == 0 {
					continue
				}
				var msg = rhpRawRecv{
					Type:   "recv",
					SeqNo:  svc.incSeqno(),
					Handle: sock.handle,
					Action: "rcvd",
					Port:   fmt.Sprintf("%d", channel),
					Data:   base64.StdEncoding.EncodeToString(fbuf),
				}
				writeRHPMsg(c, msg) //nolint:errcheck

			case "dgram":
				// Deliver payload if destination matches (or socket is unbound).
				if sock.local != "" && !strings.EqualFold(dst, sock.local) {
					continue
				}
				var msg = rhpDgramRecv{
					Type:   "recv",
					SeqNo:  svc.incSeqno(),
					Handle: sock.handle,
					Port:   fmt.Sprintf("%d", channel),
					Data:   string(info),
				}
				writeRHPMsg(c, msg) //nolint:errcheck
			}
		}
	}
}

// incSeqno returns the next global sequence number.
func (svc *RHPService) incSeqno() int {
	return int(svc.seqno.Add(1))
}

// listen accepts incoming TCP connections.
func (svc *RHPService) listen() {
	var ln, err = net.Listen("tcp", fmt.Sprintf(":%d", svc.port))
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("RHP2: Failed to listen on port %d: %v\n", svc.port, err)
		return
	}
	defer ln.Close()

	for {
		var conn, aerr = ln.Accept()
		if aerr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("RHP2: Accept error: %v\n", aerr)
			continue
		}
		var c = &rhpClient{
			conn:       conn,
			mu:         sync.Mutex{},
			sockets:    make(map[int]*rhpSocket),
			nextHandle: 0,
		}
		svc.mu.Lock()
		svc.cs = append(svc.cs, c)
		svc.mu.Unlock()
		text_color_set(DW_COLOR_INFO)
		dw_printf("RHP2: Client connected from %s\n", conn.RemoteAddr())
		go svc.handleClient(c)
	}
}

// removeClient removes a client from the service's list.
func (svc *RHPService) removeClient(c *rhpClient) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	for i, x := range svc.cs {
		if x == c {
			svc.cs = append(svc.cs[:i], svc.cs[i+1:]...)
			return
		}
	}
}

// handleClient is the receive loop for one TCP connection.
func (svc *RHPService) handleClient(c *rhpClient) {
	defer func() {
		// Clean up all open sockets.
		c.mu.Lock()
		for _, sock := range c.sockets {
			svc.cleanupSocket(sock)
		}
		c.sockets = make(map[int]*rhpSocket)
		c.mu.Unlock()

		c.conn.Close()
		svc.removeClient(c)
		text_color_set(DW_COLOR_INFO)
		dw_printf("RHP2: Client disconnected.\n")
	}()

	for {
		var buf, err = readRHPMsg(c.conn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("RHP2: Read error: %v\n", err)
			}
			return
		}

		// Decode enough to get the type field.
		var envelope map[string]json.RawMessage
		var uerr = json.Unmarshal(buf, &envelope)
		if uerr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("RHP2: JSON parse error: %v\n", uerr)
			continue
		}

		var typRaw, hasType = envelope["type"]
		if !hasType {
			continue
		}
		var typStr string
		var terr = json.Unmarshal(typRaw, &typStr)
		if terr != nil {
			continue
		}

		switch strings.ToLower(typStr) {
		case "auth":
			svc.handleAuth(c, envelope)
		case "open":
			svc.handleOpen(c, envelope)
		case "close":
			svc.handleClose(c, envelope)
		case "send":
			svc.handleSend(c, envelope)
		default:
			text_color_set(DW_COLOR_ERROR)
			dw_printf("RHP2: Unknown message type %q, ignoring.\n", typStr)
		}
	}
}

// cleanupSocket releases resources held by a socket.
// Must NOT be called with c.mu held.
func (svc *RHPService) cleanupSocket(sock *rhpSocket) {
	if sock.mode != "stream" {
		return
	}
	if sock.linked {
		// Unregister callsign for passive listener.
		dlq_unregister_callsign(sock.local, sock.port, MAX_NET_CLIENTS+sock.handle)
	} else {
		// Disconnect active/established connection.
		var addrs [AX25_MAX_ADDRS]string
		addrs[AX25_SOURCE] = sock.local
		addrs[AX25_DESTINATION] = sock.remote
		dlq_disconnect_request(addrs, 2, sock.port, MAX_NET_CLIENTS+sock.handle)
	}
}

// findClientByRHPHandle searches all RHP clients for the socket with the given handle.
// Returns nil, nil if not found.
func (svc *RHPService) findClientByRHPHandle(rhpHandle int) (*rhpClient, *rhpSocket) {
	svc.mu.Lock()
	var clients = make([]*rhpClient, len(svc.cs))
	copy(clients, svc.cs)
	svc.mu.Unlock()

	for _, c := range clients {
		c.mu.Lock()
		var sock = c.sockets[rhpHandle]
		c.mu.Unlock()
		if sock != nil {
			return c, sock
		}
	}
	return nil, nil
}

// handleAuth handles the AUTH message.  We accept all connections without
// checking credentials (local/LAN clients don't require authentication per spec).
func (svc *RHPService) handleAuth(c *rhpClient, env map[string]json.RawMessage) {
	var id, hasID = optRHPInt(env, "id")

	var replyID = 0
	if hasID {
		replyID = id
	}
	var reply = rhpAuthReply{
		Type:    "authReply",
		ID:      replyID,
		ErrCode: rhpErrOk,
		ErrText: "Ok",
	}
	writeRHPMsg(c, reply) //nolint:errcheck
}

// handleOpen handles the OPEN message (create a virtual socket).
func (svc *RHPService) handleOpen(c *rhpClient, env map[string]json.RawMessage) {
	var id, hasID = optRHPInt(env, "id")
	var pfam = strings.ToLower(optRHPStr(env, "pfam"))
	var mode = strings.ToLower(optRHPStr(env, "mode"))
	var portStr = optRHPStr(env, "port")
	var local = strings.ToUpper(optRHPStr(env, "local"))
	var remote = strings.ToUpper(optRHPStr(env, "remote"))
	var flags, _ = optRHPInt(env, "flags")

	var replyID = 0
	if hasID {
		replyID = id
	}

	sendReply := func(handle, code int, text string) {
		var r = rhpOpenReply{
			Type:    "openReply",
			ID:      replyID,
			Handle:  handle,
			ErrCode: code,
			ErrText: text,
		}
		writeRHPMsg(c, r) //nolint:errcheck
	}

	if pfam != "ax25" {
		sendReply(0, rhpErrBadParam, fmt.Sprintf("unsupported protocol family %q", pfam))
		return
	}

	switch mode {
	case "dgram", "stream", "trace", "raw":
		// OK
	default:
		sendReply(0, rhpErrBadParam, fmt.Sprintf("unsupported mode %q", mode))
		return
	}

	// Parse port (channel).  Empty or "-1" means all channels.
	var channel = -1
	if portStr != "" && portStr != "-1" {
		var n int
		var _, serr = fmt.Sscanf(portStr, "%d", &n)
		if serr == nil {
			channel = n
		}
	}

	// Allocate a handle for this socket.
	c.mu.Lock()
	var handle = c.nextHandle
	c.nextHandle++
	var sock = &rhpSocket{
		handle: handle,
		pfam:   pfam,
		mode:   mode,
		port:   channel,
		local:  local,
		remote: remote,
		flags:  flags,
		linked: false,
	}
	c.sockets[handle] = sock
	c.mu.Unlock()

	// For stream mode: initiate or register for connections.
	if mode == "stream" {
		if flags&rhpOpenFlagActiveOpen != 0 {
			// Active open: connect to remote station.
			if local == "" || remote == "" {
				c.mu.Lock()
				delete(c.sockets, handle)
				c.mu.Unlock()
				sendReply(0, rhpErrBadParam, "stream active open requires local and remote")
				return
			}
			if channel < 0 {
				c.mu.Lock()
				delete(c.sockets, handle)
				c.mu.Unlock()
				sendReply(0, rhpErrBadParam, "stream active open requires a specific port")
				return
			}
			var addrs [AX25_MAX_ADDRS]string
			addrs[AX25_SOURCE] = local
			addrs[AX25_DESTINATION] = remote
			dlq_connect_request(addrs, 2, channel, MAX_NET_CLIENTS+handle, AX25_PID_NO_LAYER_3)
		} else {
			// Passive open: listen for incoming connections on local callsign.
			if local == "" {
				c.mu.Lock()
				delete(c.sockets, handle)
				c.mu.Unlock()
				sendReply(0, rhpErrBadParam, "stream passive open requires local callsign")
				return
			}
			if channel < 0 {
				c.mu.Lock()
				delete(c.sockets, handle)
				c.mu.Unlock()
				sendReply(0, rhpErrBadParam, "stream passive open requires a specific port")
				return
			}
			dlq_register_callsign(local, channel, MAX_NET_CLIENTS+handle)
			c.mu.Lock()
			sock.linked = true
			c.mu.Unlock()
		}
	}

	sendReply(handle, rhpErrOk, "ok")
}

// handleClose handles the CLOSE message from a client.
func (svc *RHPService) handleClose(c *rhpClient, env map[string]json.RawMessage) {
	var id, hasID = optRHPInt(env, "id")
	var handle, hasHandle = optRHPInt(env, "handle")

	var replyID = 0
	if hasID {
		replyID = id
	}

	sendReply := func(h, code int, text string) {
		var r = rhpCloseReply{
			Type:    "closeReply",
			ID:      replyID,
			Handle:  h,
			ErrCode: code,
			ErrText: text,
		}
		writeRHPMsg(c, r) //nolint:errcheck
	}

	if !hasHandle {
		sendReply(0, rhpErrBadParam, "missing handle")
		return
	}

	c.mu.Lock()
	var sock, ok = c.sockets[handle]
	if !ok {
		c.mu.Unlock()
		sendReply(handle, rhpErrInvalidHandle, "invalid handle")
		return
	}
	delete(c.sockets, handle)
	c.mu.Unlock()

	svc.cleanupSocket(sock)
	sendReply(handle, rhpErrOk, "Ok")
}

// handleSend handles the SEND message from a client.
func (svc *RHPService) handleSend(c *rhpClient, env map[string]json.RawMessage) {
	var id, hasID = optRHPInt(env, "id")
	var handle, hasHandle = optRHPInt(env, "handle")
	var data = optRHPStr(env, "data")

	var replyID = 0
	if hasID {
		replyID = id
	}

	sendReply_ := func(h, code, status int, text string) {
		var r = rhpSendReply{
			Type:    "sendReply",
			ID:      replyID,
			Handle:  h,
			ErrCode: code,
			ErrText: text,
			Status:  status,
		}
		writeRHPMsg(c, r) //nolint:errcheck
	}

	if !hasHandle {
		sendReply_(0, rhpErrBadParam, 0, "missing handle")
		return
	}

	c.mu.Lock()
	var sock, ok = c.sockets[handle]
	c.mu.Unlock()
	if !ok {
		sendReply_(handle, rhpErrInvalidHandle, 0, "invalid handle")
		return
	}

	var payload = []byte(data)

	switch sock.mode {
	case "dgram":
		// Determine source and destination callsigns.
		var src = sock.local
		var dst = sock.remote
		// Allow per-send override of remote from the SEND message fields.
		var remoteOverride = strings.ToUpper(optRHPStr(env, "remote"))
		if remoteOverride != "" {
			dst = remoteOverride
		}
		if src == "" || dst == "" {
			sendReply_(handle, rhpErrBadParam, 0, "dgram send requires local and remote callsigns")
			return
		}
		var channel = sock.port
		if channel < 0 {
			channel = 0 // default to channel 0 if "all channels"
		}
		var addrs [AX25_MAX_ADDRS]string
		addrs[AX25_DESTINATION] = dst
		addrs[AX25_SOURCE] = src
		var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_UI, 0, AX25_PID_NO_LAYER_3, payload)
		if pp == nil {
			sendReply_(handle, rhpErrBadParam, 0, "failed to build UI frame")
			return
		}
		tq_append(channel, TQ_PRIO_1_LO, pp)
		sendReply_(handle, rhpErrOk, 0, "Ok")

	case "stream":
		if sock.local == "" || sock.remote == "" {
			sendReply_(handle, rhpErrBadParam, 0, "stream send requires established connection")
			return
		}
		var channel = sock.port
		if channel < 0 {
			channel = 0
		}
		var addrs [AX25_MAX_ADDRS]string
		addrs[AX25_SOURCE] = sock.local
		addrs[AX25_DESTINATION] = sock.remote
		dlq_xmit_data_request(addrs, 2, channel, MAX_NET_CLIENTS+handle, AX25_PID_NO_LAYER_3, payload)
		sendReply_(handle, rhpErrOk, rhpFlagConnected, "Ok")

	default:
		sendReply_(handle, rhpErrBadParam, 0, "send not supported on this socket mode")
	}
}

// --- Message framing -------------------------------------------------------

// readRHPMsg reads one RHP2 message: 2-byte BE length followed by that many bytes.
func readRHPMsg(conn net.Conn) ([]byte, error) {
	var lenBuf [2]byte
	var _, rerr = io.ReadFull(conn, lenBuf[:])
	if rerr != nil {
		return nil, rerr
	}
	var n = binary.BigEndian.Uint16(lenBuf[:])
	if n == 0 {
		return []byte{}, nil
	}
	var body = make([]byte, n)
	var _, berr = io.ReadFull(conn, body)
	if berr != nil {
		return nil, berr
	}
	return body, nil
}

// writeRHPMsg serialises v as JSON and sends it with a 2-byte BE length header.
// The caller must hold no locks that would block c.conn.Write.
//
//nolint:errchkjson // v is always a concrete struct defined in this file
func writeRHPMsg(c *rhpClient, v any) error {
	var data, merr = json.Marshal(v)
	if merr != nil {
		return merr
	}
	if len(data) > 65535 {
		return fmt.Errorf("RHP2: message too large (%d bytes)", len(data))
	}
	var frame = make([]byte, 2+len(data))
	binary.BigEndian.PutUint16(frame[:2], uint16(len(data)))
	copy(frame[2:], data)
	c.mu.Lock()
	defer c.mu.Unlock()
	var _, werr = c.conn.Write(frame)
	return werr
}

// --- Message field helpers --------------------------------------------------

func optRHPInt(env map[string]json.RawMessage, key string) (int, bool) {
	var raw, ok = env[key]
	if !ok {
		return 0, false
	}
	var n int
	var err = json.Unmarshal(raw, &n)
	if err != nil {
		return 0, false
	}
	return n, true
}

func optRHPStr(env map[string]json.RawMessage, key string) string {
	var raw, ok = env[key]
	if !ok {
		return ""
	}
	var s string
	var err = json.Unmarshal(raw, &s)
	if err != nil {
		return ""
	}
	return s
}

// channelMatches returns true if sockPort matches the received channel.
// sockPort == -1 means "all channels".
func channelMatches(sockPort, channel int) bool {
	return sockPort == -1 || sockPort == channel
}
