package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Provide service to other applications via "AGW TCPIP Socket Interface".
 *
 * Input:
 *
 * Outputs:
 *
 * Description:	This provides a TCP socket for communication with a client application.
 *		It implements a subset of the AGW socket interface.
 *
 *		Commands from application recognized:
 *
 *			'R'	Request for version number.
 *				(See below for response.)
 *
 *			'G'	Ask about radio ports.
 *				(See below for response.)
 *
 *			'g'	Capabilities of a port.  (new in 0.8)
 *				(See below for response.)
 *
 *			'k'	Ask to start receiving RAW AX25 frames.
 *
 *			'm'	Ask to start receiving Monitor AX25 frames.
 *				Enables sending of U, I, S, and T messages to client app.
 *
 *			'V'	Transmit UI data frame.
 *
 *			'H'	Report recently heard stations.  Not implemented yet.
 *
 *			'K'	Transmit raw AX.25 frame.
 *
 *			'P'	Application Login.  Only checked if AGWLOGIN is configured.
 *
 *			'X'	Register CallSign
 *
 *			'x'	Unregister CallSign
 *
 *			'y'	Ask Outstanding frames waiting on a Port   (new in 1.2)
 *
 *			'Y'	How many frames waiting for transmit for a particular station (new in 1.5)
 *
 *			'C'	Connect, Start an AX.25 Connection			(new in 1.4)
 *
 *			'v'	Connect VIA, Start an AX.25 circuit thru digipeaters	(new in 1.4)
 *
 *			'c'	Connection with non-standard PID			(new in 1.4)
 *
 *			'D'	Send Connected Data					(new in 1.4)
 *
 *			'd'	Disconnect, Terminate an AX.25 Connection		(new in 1.4)
 *
 *
 *			A message is printed if any others are received.
 *
 *			TODO: Should others be implemented?
 *
 *
 *		Messages sent to client application:
 *
 *			'R'	Reply to Request for version number.
 *				Currently responds with major 1, minor 0.
 *
 *			'G'	Reply to Ask about radio ports.
 *
 *			'g'	Reply to capabilities of a port.  (new in 0.8)
 *
 *			'K'	Received AX.25 frame in raw format.
 *				(Enabled with 'k' command.)
 *
 *			'U'	Received AX.25 "UI" frames in monitor format.
 *				(Enabled with 'm' command.)
 *
 *			'I'	Received AX.25 "I" frames in monitor format.	(new in 1.6)
 *				(Enabled with 'm' command.)
 *
 *			'S'	Received AX.25 "S" and "U" (other than UI) frames in monitor format.	(new in 1.6)
 *				(Enabled with 'm' command.)
 *
 *			'T'	Own Transmitted AX.25 frames in monitor format.	(new in 1.6)
 *				(Enabled with 'm' command.)
 *
 *			'y'	Outstanding frames waiting on a Port   (new in 1.2)
 *
 *			'Y'	How many frames waiting for transmit for a particular station (new in 1.5)
 *
 *			'C'	AX.25 Connection Received		(new in 1.4)
 *
 *			'D'	Connected AX.25 Data			(new in 1.4)
 *
 *			'd'	Disconnected				(new in 1.4)
 *
 *
 *
 * References:	AGWPE TCP/IP API Tutorial
 *		http://uz7ho.org.ua/includes/agwpeapi.htm
 *
 *		It has disappeared from the original location but you can find it here:
 *		https://web.archive.org/web/20130807113413/http:/uz7ho.org.ua/includes/agwpeapi.htm
 *		https://www.on7lds.net/42/sites/default/files/AGWPEAPI.HTM
 *
 * 		Getting Started with Winsock
 *		http://msdn.microsoft.com/en-us/library/windows/desktop/bb530742(v=vs.85).aspx
 *
 *
 * Major change in 1.1:
 *
 *		Formerly a single client was allowed.
 *		Now we can have multiple concurrent clients.
 *
 *---------------------------------------------------------------*/

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// AGW_LOGIN_FIELD_LEN is the size of each of the two fields, user name and
// password, in the data of an "Application Login" frame.  Both are NUL padded.
const AGW_LOGIN_FIELD_LEN = 255

// AGWServer provides the "AGW TCPIP Socket Interface" to client applications.
//
// The main program makes one of these, and everything the interface remembers
// about its clients lives in it.  The methods the rest of the program calls to
// tell a client something do nothing on a nil server, so a caller on a path
// that runs without one - a test, or a build of the program that never started
// the interface - needs no special case of its own.
type AGWServer struct {
	// What sort of thing each channel is, which decides the channels
	// connected mode may be used on.  audio.go owns this; we only read it,
	// and it is nil in tests that do not set one up.
	audioConfigP *audio_s

	// User names and passwords, any one of which a client may send in an
	// "Application Login" frame before we honour any of its other commands.
	// Empty means no login is required.  Written once at startup, read by
	// every client's command thread thereafter.
	logins []agwpe_login_s

	// Print information flowing from and to client.  Settled by the
	// constructor, before it starts the goroutines that read it, and never
	// written again - so no lock, and none of the cost of one on a path that
	// consults it for every frame.
	debug int

	clients [MAX_NET_CLIENTS]agwClient
}

// agwClient is what the server remembers about one of its client slots.  A
// slot whose conn is nil is free for the next client to connect.
type agwClient struct {
	/* Socket for communication with client application. */
	conn net.Conn

	/* Should we send received packets to client app in raw form? */
	/* Note that it starts as false for a new connection. */
	/* the client app must send a command to enable this. */
	sendRaw bool

	/* As above, but in monitor form. */
	sendMonitor bool

	/* Has this client sent an "Application Login" that we accepted? */
	/* Only consulted when a login is required. */
	/* The connection listener and the client's own command thread both */
	/* touch this, hence the atomic. */
	loggedIn atomic.Bool

	/* Is this client exempt from having to log in at all, by having connected */
	/* from this machine?  Settled when the connection is accepted, before the */
	/* socket is published, and read again whenever the client's login state is */
	/* reset. */
	loginExempt atomic.Bool
}

/*-------------------------------------------------------------------
 *
 * Name:        NewAGWServer
 *
 * Purpose:     Set up a server to listen for connection requests from
 *		an application such as Xastir.
 *
 * Inputs:	mc.agwpe_port	- TCP port for server.
 *				  Main program has default of 8000 but allows
 *				  an alternative to be specified on the command line
 *
 *				0 means disable.  New in version 1.2.
 *
 *		debug		- "-d a" level: print the messages flowing to and
 *				  from clients.
 *
 * Outputs:
 *
 * Description:	This starts at least two threads:
 *		  *  one to listen for a connection from client app.
 *		  *  one or more to listen for commands from client app.
 *		so the main application doesn't block while we wait for these.
 *
 *--------------------------------------------------------------------*/

func NewAGWServer(ctx context.Context, audio_config_p *audio_s, mc *misc_config_s, debug int) *AGWServer {
	var server_port = mc.agwpe_port /* Usually 8000 but can be changed. */

	logrus.WithField("server_port", server_port).Debug("NewAGWServer")

	var s = new(AGWServer)
	s.audioConfigP = audio_config_p
	s.logins = mc.agwpe_logins
	s.debug = debug

	/*
	 * A new server starts with every client slot empty, and with none of the
	 * things a client can switch on switched on, because that is what the
	 * zero value of the client table is.
	 */

	if server_port == 0 {
		text_color_set(DW_COLOR_INFO)
		dw_printf("Disabled AGW network client port.\n")

		return s
	}

	if s.loginRequired() {
		text_color_set(DW_COLOR_INFO)
		dw_printf("AGW client applications must log in, unless they connect from this machine.\n")
		dw_printf("%d set(s) of credentials configured.\n", len(s.logins))
	}

	/*
	 * This waits for a client to connect and attaches it to a free slot.
	 */
	go s.connectListenThread(ctx, server_port)

	/*
	 * These read messages from client when the client's slot holds a socket.
	 * Currently we start up a separate thread for each potential connection.
	 * Possible later refinement.  Start one now, others only as needed.
	 */
	for client := range MAX_NET_CLIENTS {
		go s.cmdListenThread(ctx, client)
	}

	return s
}

/*-------------------------------------------------------------------
 *
 * Name:        debug_print
 *
 * Purpose:     Print message to/from client for debugging.
 *
 * Inputs:	fromto		- Direction of message.
 *		client		- client number, 0 .. MAX_NET_CLIENTS-1
 *		pmsg		- Address of the message block.
 *		msg_len		- Length of the message.
 *
 *--------------------------------------------------------------------*/

/*-------------------------------------------------------------------
 *
 * Name:        server_send_rec_packet
 *
 * Purpose:     Send a received packet to the client app.
 *
 * Inputs:	channel		- Channel number where packet was received.
 *				  0 = first, 1 = second if any.
 *
 *		pp		- Identifier for packet object.
 *
 *		fbuf		- Frame buffer.
 *
 *
 * Description:	Send message to client if connected.
 *		Disconnect from client, and notify user, if any error.
 *
 *		There are two different formats:
 *			RAW - the original received frame.
 *			MONITOR - human readable monitoring format.
 *
 *--------------------------------------------------------------------*/

func (s *AGWServer) SendRecPacket(channel int, pp *packet_t, fbuf []byte) {
	if s == nil {
		return
	}

	/*
	 * RAW format
	 */
	for client := range MAX_NET_CLIENTS {
		var conn = s.clientWantingRaw(client)
		if conn != nil {
			var agwpe_msg = new(AGWPEMessage)

			agwpe_msg.Header.Portx = byte(channel)

			agwpe_msg.Header.DataKind = 'K'

			var callFrom = ax25_get_addr_with_ssid(pp, AX25_SOURCE)
			copy(agwpe_msg.Header.CallFrom[:], []byte(callFrom))

			var callTo = ax25_get_addr_with_ssid(pp, AX25_DESTINATION)
			copy(agwpe_msg.Header.CallTo[:], []byte(callTo))

			agwpe_msg.Header.DataLen = uint32(len(fbuf) + 1)
			agwpe_msg.Data = make([]byte, len(fbuf)+1)

			/* Stick in extra byte for the "TNC" to use. */

			agwpe_msg.Data[0] = byte(channel) << 4 // Was 0.  Fixed in 1.8.

			copy(agwpe_msg.Data[1:], fbuf)

			if s.debug > 0 {
				s.debugPrint(TO_CLIENT, client, agwpe_msg)
			}

			var _, err = agwpe_msg.Write(conn, binary.LittleEndian)
			if err != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("\nError sending message to AGW client application.  Closing connection.\n\n")
				s.detachClient(client, conn)
			}
		}
	}

	// Application might want more human readable format.

	s.SendMonitored(channel, pp, 0)
} /* end SendRecPacket */

func (s *AGWServer) SendMonitored(channel int, pp *packet_t, own_xmit int) {
	if s == nil {
		return
	}

	/*
	 * MONITOR format - 	'I' for information frames.
	 *			'U' for unnumbered information.
	 *			'S' for supervisory and other unnumbered.
	 *
	 *			'T' for own transmitted frames.
	 */
	for client := range MAX_NET_CLIENTS {
		var conn = s.clientWantingMonitor(client)
		if conn != nil {
			var agwpe_msg = new(AGWPEMessage)

			agwpe_msg.Header.Portx = byte(channel) // datakind is added later.

			var callFrom = ax25_get_addr_with_ssid(pp, AX25_SOURCE)
			copy(agwpe_msg.Header.CallFrom[:], []byte(callFrom))

			var callTo = ax25_get_addr_with_ssid(pp, AX25_DESTINATION)
			copy(agwpe_msg.Header.CallTo[:], []byte(callTo))

			/* http://uz7ho.org.ua/includes/agwpeapi.htm#_Toc500723812 */

			/* Description mentions one CR character after timestamp but example has two. */
			/* Actual observed cases have only one. */
			/* Also need to add extra CR, CR, null at end. */
			/* The documentation example includes these 3 extra in the Len= value */
			/* but actual observed data uses only the packet info length. */

			// Documentation doesn't mention anything about including the via path.
			// In version 1.4, we add that to match observed behaviour.

			// This inconsistency was reported:
			// Direwolf:
			// [AGWE-IN] 1:Fm ZL4FOX-8 To Q7P2U2 [08:25:07]`I1*l V>/"9<}[:Barts Tracker 3.83V X
			// AGWPE:
			// [AGWE-IN] 1:Fm ZL4FOX-8 To Q7P2U2 Via WIDE3-3 [08:32:14]`I0*l V>/"98}[:Barts Tracker 3.83V X

			// Format the channel and addresses, with leading and trailing space.

			agwpe_msg.Data = mon_addrs(channel, pp)

			// Add the description with <... >

			var desc string
			agwpe_msg.Header.DataKind, desc = mon_desc(pp)

			if own_xmit > 0 {
				// Should we include all own transmitted frames or only UNPROTO?
				// Discussion:  https://github.com/wb2osz/direwolf/issues/585
				if agwpe_msg.Header.DataKind != 'U' {
					break
				}

				agwpe_msg.Header.DataKind = 'T'
			}

			agwpe_msg.Data = append(agwpe_msg.Data, []byte(desc)...)

			// Timestamp with [...]\r

			var tm = time.Now()
			var ts = tm.Format("[15:04:05]\r")
			agwpe_msg.Data = append(agwpe_msg.Data, []byte(ts)...)

			// Information if any with \r.

			var pinfo = AX25GetInfo(pp)
			var msg_data_len = len(agwpe_msg.Data) // result length so far

			if len(pinfo) > 0 {
				// Issue 367: Use of strlcat truncated information part at any nul character.
				// Use memcpy instead to preserve binary data, e.g. NET/ROM.
				agwpe_msg.Data = append(agwpe_msg.Data, pinfo...)
				msg_data_len += len(pinfo)

				agwpe_msg.Data = append(agwpe_msg.Data, '\r')
				msg_data_len++
			}

			agwpe_msg.Data = append(agwpe_msg.Data, 0) // add nul at end, included in length.
			msg_data_len++
			agwpe_msg.Header.DataLen = uint32(msg_data_len) // TODO KG Just len(Data)

			if s.debug > 0 {
				s.debugPrint(TO_CLIENT, client, agwpe_msg)
			}

			var _, err = agwpe_msg.Write(conn, binary.LittleEndian)
			if err != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("\nError sending message to AGW client application %d (%s).  Closing connection.\n\n", client, err)
				s.detachClient(client, conn)
			}
		}
	}
} /* SendMonitored */

// Next two are broken out in case they can be reused elsewhere.

// Format addresses in AGWPR monitoring format such as:
//	 1:Fm ZL4FOX-8 To Q7P2U2 Via WIDE3-3

// There is some disagreement, in the user community, about whether to:
// * follow the lead of UZ7HO SoundModem and mark all of the used addresses, or
// * follow the TNC-2 Monitoring format and mark only the last used, i.e. the station heard.

// I think my opinion (which could change) is that we should try to be consistent with TNC-2 format
// rather than continuing to propagate historical inconsistencies.

func mon_addrs(channel int, pp *packet_t) []byte {
	var src = ax25_get_addr_with_ssid(pp, AX25_SOURCE)

	var dst = ax25_get_addr_with_ssid(pp, AX25_DESTINATION)

	var num_digi = ax25_get_num_repeaters(pp)

	if num_digi > 0 {
		var via strings.Builder // complete via path

		for j := range num_digi {
			if j != 0 {
				via.WriteString(",") // comma if not first address
			}

			var digiaddr = ax25_get_addr_with_ssid(pp, AX25_REPEATER_1+j)
			via.WriteString(digiaddr)
			/*
				#if 0  // Mark each used with * as seen in UZ7HO SoundModem.
					    if (ax25_get_h(pp, AX25_REPEATER_1 + j)) {
				#else */
			// Mark only last used (i.e. the heard station) with * as in TNC-2 Monitoring format.
			if AX25_REPEATER_1+j == ax25_get_heard(pp) {
				// #endif
				via.WriteString("*")
			}
		}

		return fmt.Appendf(nil, " %d:Fm %s To %s Via %s ", channel+1, src, dst, via.String())
	} else {
		return fmt.Appendf(nil, " %d:Fm %s To %s ", channel+1, src, dst)
	}
}

// Generate frame description in AGWPE monitoring format such as
//	<UI pid=F0 Len=123 >
//	<I R1 S3 pid=F0 Len=123 >
//	<RR P1 R5 >
//
// Returns:
//	'I' for information frame.
//	'U' for unnumbered information frame.
//	'S' for supervisory and other unnumbered frames.

func mon_desc(pp *packet_t) (byte, string) {
	var cr, _, pf, nr, ns, ftype = ax25_frame_type(pp)
	var pf_text string // P or F depending on whether command or response.

	switch cr {
	case cr_cmd:
		// P only: I, SABME, SABM, DISC
		pf_text = "P"
	case cr_res:
		// F only: DM, UA, FRMR
		// Either: RR, RNR, REJ, SREJ, UI, XID, TEST
		pf_text = "F"
	default:
		// Not AX.25 version >= 2.0
		// APRS is often sloppy about this but it
		// is essential for connected mode.
		pf_text = "PF"
	}

	// I, UI, XID, SREJ, TEST can have information part.
	var pinfo = AX25GetInfo(pp)

	switch ftype {
	case frame_type_I:
		return 'I', fmt.Sprintf("<I S%d R%d pid=%02X Len=%d %s=%d >", ns, nr, ax25_get_pid(pp), len(pinfo), pf_text, pf)

	case frame_type_U_UI:
		return 'U', fmt.Sprintf("<UI pid=%02X Len=%d %s=%d >", ax25_get_pid(pp), len(pinfo), pf_text, pf)

	case frame_type_S_RR:
		return 'S', fmt.Sprintf("<RR R%d %s=%d >", nr, pf_text, pf)

	case frame_type_S_RNR:
		return 'S', fmt.Sprintf("<RNR R%d %s=%d >", nr, pf_text, pf)

	case frame_type_S_REJ:
		return 'S', fmt.Sprintf("<REJ R%d %s=%d >", nr, pf_text, pf)

	case frame_type_S_SREJ:
		return 'S', fmt.Sprintf("<SREJ R%d %s=%d Len=%d >", nr, pf_text, pf, len(pinfo))

	case frame_type_U_SABME:
		return 'S', fmt.Sprintf("<SABME %s=%d >", pf_text, pf)

	case frame_type_U_SABM:
		return 'S', fmt.Sprintf("<SABM %s=%d >", pf_text, pf)

	case frame_type_U_DISC:
		return 'S', fmt.Sprintf("<DISC %s=%d >", pf_text, pf)

	case frame_type_U_DM:
		return 'S', fmt.Sprintf("<DM %s=%d >", pf_text, pf)

	case frame_type_U_UA:
		return 'S', fmt.Sprintf("<UA %s=%d >", pf_text, pf)

	case frame_type_U_FRMR:
		return 'S', fmt.Sprintf("<FRMR %s=%d >", pf_text, pf)

	case frame_type_U_XID:
		return 'S', fmt.Sprintf("<XID %s=%d Len=%d >", pf_text, pf, len(pinfo))

	case frame_type_U_TEST:
		return 'S', fmt.Sprintf("<TEST %s=%d Len=%d >", pf_text, pf, len(pinfo))

	default:
		// Also case frame_type_U:
		return 'S', "<U other??? >"
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        LinkEstablished
 *
 * Purpose:     Send notification to client app when a link has
 *		been established with another station.
 *
 *		DL-CONNECT Confirm or DL-CONNECT Indication in the protocol spec.
 *
 * Inputs:	channel		- Which radio channel.
 *
 * 		client		- Which one of potentially several clients.
 *
 *		remote_call	- Callsign[-ssid] of remote station.
 *
 *		own_call	- Callsign[-ssid] of my end.
 *
 *		incoming	- true if connection was initiated from other end.
 *				  false if this end started it.
 *
 *--------------------------------------------------------------------*/

func (s *AGWServer) LinkEstablished(channel int, client int, remote_call string, own_call string, incoming bool) {
	if s == nil {
		return
	}

	var reply = new(AGWPEMessage)

	reply.Header.Portx = byte(channel)
	reply.Header.DataKind = 'C'

	copy(reply.Header.CallFrom[:], []byte(remote_call))
	copy(reply.Header.CallTo[:], []byte(own_call))

	// Question:  Should the via path be provided too?

	if incoming {
		// Other end initiated the connection.
		reply.Data = fmt.Appendf(nil, "*** CONNECTED To Station %s\r", remote_call)
	} else {
		// We started the connection.
		reply.Data = fmt.Appendf(nil, "*** CONNECTED With Station %s\r", remote_call)
	}

	reply.Data = append(reply.Data, 0)
	reply.Header.DataLen = uint32(len(reply.Data))

	s.sendToClient(client, reply)
} /* end LinkEstablished */

/*-------------------------------------------------------------------
 *
 * Name:        LinkTerminated
 *
 * Purpose:     Send notification to client app when a link with
 *		another station has been terminated or a connection
 *		attempt failed.
 *
 *		DL-DISCONNECT Confirm or DL-DISCONNECT Indication in the protocol spec.
 *
 * Inputs:	channel		- Which radio channel.
 *
 * 		client		- Which one of potentially several clients.
 *
 *		remote_call	- Callsign[-ssid] of remote station.
 *
 *		own_call	- Callsign[-ssid] of my end.
 *
 *		timeout		- true when no answer from other station.
 *				  How do we distinguish who asked for the
 *				  termination of an existing link?
 *
 *--------------------------------------------------------------------*/

func (s *AGWServer) LinkTerminated(channel int, client int, remote_call string, own_call string, timeout bool) {
	if s == nil {
		return
	}

	var reply = new(AGWPEMessage)

	reply.Header.Portx = byte(channel)
	reply.Header.DataKind = 'd'
	copy(reply.Header.CallFrom[:], []byte(remote_call))
	copy(reply.Header.CallTo[:], []byte(own_call))

	if timeout {
		reply.Data = fmt.Appendf(nil, "*** DISCONNECTED RETRYOUT With %s\r", remote_call)
	} else {
		reply.Data = fmt.Appendf(nil, "*** DISCONNECTED From Station %s\r", remote_call)
	}

	reply.Data = append(reply.Data, 0)
	reply.Header.DataLen = uint32(len(reply.Data))

	s.sendToClient(client, reply)
} /* end LinkTerminated */

/*-------------------------------------------------------------------
 *
 * Name:        RecConnData
 *
 * Purpose:     Send received connected data to the application.
 *
 *		DL-DATA Indication in the protocol spec.
 *
 * Inputs:	channel		- Which radio channel.
 *
 * 		client		- Which one of potentially several clients.
 *
 *		remote_call	- Callsign[-ssid] of remote station.
 *
 *		own_call	- Callsign[-ssid] of my end.
 *
 *		pid		- Protocol ID from I frame.
 *
 *		data_ptr	- Pointer to a block of bytes.
 *
 *		data_len	- Number of bytes.  Could be zero.
 *
 *--------------------------------------------------------------------*/

func (s *AGWServer) RecConnData(channel int, client int, remote_call string, own_call string, pid int, data []byte) {
	if s == nil {
		return
	}

	var reply = new(AGWPEMessage)

	reply.Header.Portx = byte(channel)
	reply.Header.DataKind = 'D'
	reply.Header.PID = byte(pid)

	copy(reply.Header.CallFrom[:], []byte(remote_call))
	copy(reply.Header.CallTo[:], []byte(own_call))

	if len(data) > AX25_MAX_INFO_LEN {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid length %d for connected data to client %d.\n", len(data), client)
		data = data[:AX25_MAX_INFO_LEN]
	}

	reply.Data = make([]byte, len(data))
	copy(reply.Data, data)
	reply.Header.DataLen = uint32(len(data))

	s.sendToClient(client, reply)
} /* end RecConnData */

/*-------------------------------------------------------------------
 *
 * Name:        OutstandingFramesReply
 *
 * Purpose:     Send 'Y' Outstanding frames for connected data to the application.
 *
 * Inputs:	channel		- Which radio channel.
 *
 * 		client		- Which one of potentially several clients.
 *
 *		own_call	- Callsign[-ssid] of my end.
 *
 *		remote_call	- Callsign[-ssid] of remote station.
 *
 *		count		- Number of frames sent from the application but
 *				  not yet received by the other station.
 *
 *--------------------------------------------------------------------*/

func (s *AGWServer) OutstandingFramesReply(channel int, client int, own_call string, remote_call string, count int) {
	if s == nil {
		return
	}

	var reply = new(AGWPEMessage)

	reply.Header.Portx = byte(channel)
	reply.Header.DataKind = 'Y'

	copy(reply.Header.CallFrom[:], []byte(own_call))
	copy(reply.Header.CallTo[:], []byte(remote_call))

	reply.Header.DataLen = 4
	reply.Data = make([]byte, 4)
	binary.LittleEndian.PutUint32(reply.Data, uint32(count))

	s.sendToClient(client, reply)
} /* end OutstandingFramesReply */

// clientConn returns the socket currently attached to client, or nil if
// nothing is.
func (s *AGWServer) clientConn(client int) net.Conn {
	return s.clients[client].conn
}

// clientWantingRaw returns client's socket if it has asked to be sent received
// packets in raw form, and nil otherwise.
func (s *AGWServer) clientWantingRaw(client int) net.Conn {
	if !s.clients[client].sendRaw {
		return nil
	}

	return s.clients[client].conn
}

// clientWantingMonitor returns client's socket if it has asked to be sent
// received packets in monitor form, and nil otherwise.
func (s *AGWServer) clientWantingMonitor(client int) net.Conn {
	if !s.clients[client].sendMonitor {
		return nil
	}

	return s.clients[client].conn
}

// findFreeClient returns the index of the first client slot with no socket
// attached, or -1 if all are in use.
func (s *AGWServer) findFreeClient() int {
	for c := range MAX_NET_CLIENTS {
		if s.clients[c].conn == nil {
			return c
		}
	}

	return -1
}

func (s *AGWServer) debugPrint(fromto fromto_t, client int, pmsg *AGWPEMessage) {
	var direction, datakind string

	switch fromto {
	case FROM_CLIENT:
		direction = "from" /* from the client application */

		switch pmsg.Header.DataKind {
		case 'P':
			datakind = "Application Login"
		case 'X':
			datakind = "Register CallSign"
		case 'x':
			datakind = "Unregister CallSign"
		case 'G':
			datakind = "Ask Port Information"
		case 'm':
			datakind = "Enable Reception of Monitoring Frames"
		case 'R':
			datakind = "AGWPE Version Info"
		case 'g':
			datakind = "Ask Port Capabilities"
		case 'H':
			datakind = "Callsign Heard on a Port"
		case 'y':
			datakind = "Ask Outstanding frames waiting on a Port"
		case 'Y':
			datakind = "Ask Outstanding frames waiting for a connection"
		case 'M':
			datakind = "Send UNPROTO Information"
		case 'C':
			datakind = "Connect, Start an AX.25 Connection"
		case 'D':
			datakind = "Send Connected Data"
		case 'd':
			datakind = "Disconnect, Terminate an AX.25 Connection"
		case 'v':
			datakind = "Connect VIA, Start an AX.25 circuit thru digipeaters"
		case 'V':
			datakind = "Send UNPROTO VIA"
		case 'c':
			datakind = "Non-Standard Connections, Connection with PID"
		case 'K':
			datakind = "Send data in raw AX.25 format"
		case 'k':
			datakind = "Activate reception of Frames in raw format"
		default:
			datakind = "**INVALID**"
		}

	case TO_CLIENT:
		direction = "to"

		switch pmsg.Header.DataKind {
		case 'R':
			datakind = "Version Number"
		case 'X':
			datakind = "Callsign Registration"
		case 'G':
			datakind = "Port Information"
		case 'g':
			datakind = "Capabilities of a Port"
		case 'y':
			datakind = "Frames Outstanding on a Port"
		case 'Y':
			datakind = "Frames Outstanding on a Connection"
		case 'H':
			datakind = "Heard Stations on a Port"
		case 'C':
			datakind = "AX.25 Connection Received"
		case 'D':
			datakind = "Connected AX.25 Data"
		case 'd':
			datakind = "Disconnected"
		case 'I':
			datakind = "Monitored Connected Information"
		case 'S':
			datakind = "Monitored Supervisory Information"
		case 'U':
			datakind = "Monitored Unproto Information"
		case 'T':
			datakind = "Monitoring Own Information"
		case 'K':
			datakind = "Monitored Information in Raw Format"
		default:
			datakind = "**INVALID**"
		}
	default:
		panic(fmt.Sprintf("Unknown fromto: %v", fromto))
	}

	text_color_set(DW_COLOR_DEBUG)
	dw_printf("\n")

	dw_printf("%s %s %s AGWPE client application %d\n",
		FROMTO_PREFIX[fromto], datakind, direction, client)

	dw_printf("\tportx = %d, datakind = '%c', pid = 0x%02x\n", pmsg.Header.Portx, pmsg.Header.DataKind, pmsg.Header.PID)
	dw_printf("\tcall_from = \"%s\", call_to = \"%s\"\n", pmsg.Header.CallFrom, pmsg.Header.CallTo)
	dw_printf("\tdata_len = %d, user_reserved = %d, data =\n", pmsg.Header.DataLen, pmsg.Header.UserReserved)

	HexDump(pmsg.Data[:pmsg.Header.DataLen])
}

// connectedModeAllowed reports whether AX.25 connected mode is allowed on portx.
// Connected mode is supported for MEDIUM_RADIO channels and MEDIUM_NETTNC channels.
// When there is no audio configuration to consult (e.g. in unit tests), only
// channels < MAX_RADIO_CHANS are permitted, preserving the previous behaviour.
func (s *AGWServer) connectedModeAllowed(portx byte) bool {
	if int(portx) >= MAX_TOTAL_CHANS {
		return false
	}

	if s == nil || s.audioConfigP == nil {
		return int(portx) < MAX_RADIO_CHANS
	}

	var m = s.audioConfigP.chan_medium[portx]

	return m == MEDIUM_RADIO || m == MEDIUM_NETTNC
}

/*-------------------------------------------------------------------
 *
 * Name:        connect_listen_thread
 *
 * Purpose:     Wait for a connection request from an application.
 *
 * Inputs:	arg		- TCP port for server.
 *				  Main program has default of 8000 but allows
 *				  an alternative to be specified on the command line
 *
 * Outputs:	The accepted connection is attached to a free client slot.
 *
 * Description:	Wait for connection request from client and establish
 *		communication.
 *		Note that the client can go away and come back again and
 *		re-establish communication without restarting this application.
 *
 *--------------------------------------------------------------------*/

func (s *AGWServer) connectListenThread(ctx context.Context, server_port int) {
	logrus.WithField("port", server_port).Debug("Binding to port")
	var listener, listenErr = new(net.ListenConfig).Listen(ctx, "tcp", fmt.Sprintf(":%d", server_port))
	if listenErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("connect_listen_thread: Listen failed: %s", listenErr)

		return
	}

	// Dire Wolf set SO_REUSEADDR here (its version 1.3, as suggested by
	// G8BPQ) so that restarting the application straight away could bind the
	// port again.  Go's net package already sets it on every Unix TCP
	// listener, and the way we were setting it - TCPListener.File, then
	// setsockopt on the duplicate - has a sting in the tail: File puts the
	// underlying socket into blocking mode, which takes it out of the
	// runtime's poller, and Close then no longer interrupts a goroutine
	// waiting in Accept.  That is exactly what stopping needs it to do.

	logrus.WithField("port", server_port).Debug("opened socket for stream i/o")

	// Accept below blocks until a client turns up, which may be never, so
	// closing the listener is what gets us back when we are asked to stop.
	// It also gives the port up rather than holding it until the process
	// exits, which is what lets a test start a server and then stop it.
	defer closeOnDone(ctx, listener)()

	for ctx.Err() == nil {
		var client = s.findFreeClient()

		if client >= 0 {
			text_color_set(DW_COLOR_INFO)
			dw_printf("Ready to accept AGW client application %d on port %d ...\n", client, server_port)

			var conn, acceptErr = listener.Accept()
			if acceptErr != nil {
				if ctx.Err() != nil {
					return // We closed the listener ourselves on the way out.
				}

				dw_printf("Accept failed: %v\n", acceptErr)

				continue
			}

			if ctx.Err() != nil {
				// Cancelled while this connection sat in the accept queue:
				// the kernel completes a connection whether or not anybody
				// is still listening.  Hang up rather than attach a client
				// nothing will ever read from.
				conn.Close()

				return
			}

			s.clientAccepted(client, conn)

			text_color_set(DW_COLOR_INFO)
			dw_printf("\nAttached to AGW client application %d...\n\n", client)
		} else if !sleepSecCtx(ctx, 1) { /* wait then check again if more clients allowed. */
			return
		}
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        cmdListenThread
 *
 * Purpose:     Wait for command messages from an application.
 *
 * Inputs:	arg		- client number, 0 .. MAX_NET_CLIENTS-1
 *
 * Outputs:	The client's slot is emptied when its connection goes away.
 *
 * Description:	Process messages from the client application.
 *		Note that the client can go away and come back again and
 *		re-establish communication without restarting this application.
 *
 *--------------------------------------------------------------------*/

func (s *AGWServer) sendToClient(client int, reply_p *AGWPEMessage) {
	var conn = s.clientConn(client)
	if conn == nil {
		return
	}

	var ph = reply_p.Header

	if ph.DataLen > 4096 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid data length %d for AGW protocol message to client %d.\n", ph.DataLen, client)
		s.debugPrint(TO_CLIENT, client, reply_p)
	}

	if s.debug > 0 {
		s.debugPrint(TO_CLIENT, client, reply_p)
	}

	reply_p.Write(conn, binary.LittleEndian)
}

// detachClient hangs up on a client, gives its slot back, and tells the data
// link machinery that it has gone.  The slot is only cleared if it still holds
// conn, so a newer connection that has already been accepted into it isn't
// clobbered by a thread on its way out.
func (s *AGWServer) detachClient(client int, conn net.Conn) {
	conn.Close()

	if s.clients[client].conn == conn {
		s.clients[client].conn = nil
	}

	dlq_client_cleanup(client)
}

// readCommandData reads an AGW message's data part, if it has one, into
// cmd.Data.  It returns how many bytes it read, so a caller can tell a short
// read from a complete one, and reads nothing at all for a message whose
// header says it has no data.
func readCommandData(conn net.Conn, cmd *AGWPEMessage) (int, error) {
	if cmd.Header.DataLen == 0 {
		return 0, nil
	}

	var b = make([]byte, cmd.Header.DataLen)

	var n, readErr = conn.Read(b)
	if readErr != nil {
		return n, readErr
	}

	cmd.Data = b[:n]

	return n, nil
}

func (s *AGWServer) cmdListenThread(ctx context.Context, client int) {
	Assert(client >= 0 && client < MAX_NET_CLIENTS)

	for ctx.Err() == nil {
		for s.clientConn(client) == nil {
			if !sleepSecCtx(ctx, 1) { /* Not connected.  Try again later. */
				return
			}
		}

		var cmd = new(AGWPEMessage)

		var conn = s.clientConn(client)
		if conn == nil {
			continue // It went away between the check above and here.
		}

		// A client that says nothing, or sends a header and then stops
		// halfway through its data, leaves the reads below blocked
		// indefinitely, so closing its socket is what gets us back.  It stays
		// armed until the whole message has been read, since either read can
		// be the one that never returns.
		var stopClose = closeOnDone(ctx, conn)

		var readErr = binary.Read(conn, binary.LittleEndian, &cmd.Header)

		if readErr != nil {
			stopClose()

			if ctx.Err() != nil {
				// Hang up rather than leave a client attached to a thread
				// that has gone, and with connected mode still believing it
				// is there.
				s.detachClient(client, conn)

				return
			}

			text_color_set(DW_COLOR_ERROR)
			dw_printf("\nError getting message header from AGW client application %d: %s\n", client, readErr)
			dw_printf("Closing connection.\n\n")
			s.detachClient(client, conn)

			continue
		}

		/*
		 * Take some precautions to guard against bad data which could cause problems later.
		 */
		if cmd.Header.Portx >= MAX_TOTAL_CHANS {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("\nInvalid port number, %d, in command '%c', from AGW client application %d.\n",
				cmd.Header.Portx, cmd.Header.DataKind, client)
			cmd.Header.Portx = 0 // avoid subscript out of bounds, try to keep going.
		}

		/*
		 * Call to/from fields are 10 bytes but contents must not exceed 9 characters.
		 * It's not guaranteed that unused bytes will contain 0 so we
		 * don't issue error message in this case.
		 */
		cmd.Header.CallFrom[len(cmd.Header.CallFrom)-1] = 0
		cmd.Header.CallTo[len(cmd.Header.CallTo)-1] = 0

		var n, dataErr = readCommandData(conn, cmd)

		stopClose()

		if ctx.Err() != nil {
			s.detachClient(client, conn) // As above.

			return
		}

		if n != int(cmd.Header.DataLen) || dataErr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("\nError getting message data from AGW client application %d: %s\n", client, dataErr)
			dw_printf("Tried to read %d bytes, got %d.\n", cmd.Header.DataLen, n)
			dw_printf("Closing connection.\n\n")
			s.detachClient(client, conn)

			return
		}

		/*
		 * print & process message from client.
		 */

		if s.debug > 0 {
			s.debugPrint(FROM_CLIENT, client, cmd)
		}

		s.handleClientCommand(client, cmd)
	}
} /* end cmdListenThread */

// clientAccepted takes on a newly accepted connection, putting the per-client
// state into the shape a new client should find it in.
func (s *AGWServer) clientAccepted(client int, conn net.Conn) {
	/*
	 * The command to change these is actually a toggle, not explicit on or off.
	 * Make sure they have proper state when we get a new connection.
	 */
	s.clients[client].sendRaw = false
	s.clients[client].sendMonitor = false

	/*
	 * Whoever had this slot before does not vouch for whoever has it now, so a
	 * client that has to log in starts logged out.
	 */
	s.clients[client].loginExempt.Store(agwClientIsLocal(conn))
	s.clientLoggedOut(client)

	/*
	 * Publish the socket last.  cmdListenThread is already watching this slot
	 * for one, and acts on whatever it finds the moment one appears, so
	 * anything a command is judged against has to be in place first -
	 * otherwise a client that gets in quickly enough is judged against the
	 * state the previous holder of the slot left, and a remote one could find
	 * itself logged in on the strength of somebody else's login.
	 */
	s.clients[client].conn = conn
}

// clientLoggedOut puts a client back to where one that has not logged in
// starts: needing to log in, unless it is exempt by having connected from this
// machine.  A client on this machine never has to log in, so a login attempt it
// gets wrong does not take anything away from it.
func (s *AGWServer) clientLoggedOut(client int) {
	s.clients[client].loggedIn.Store(s.clients[client].loginExempt.Load())
}

// agwClientIsLocal reports whether a client connected from the machine we are
// running on.
//
// AGWPE's security settings are about which other machines may reach it, and
// its documentation says a login "should not bother applications running on the
// same machine where AGWPE is executing", so we exempt those too.  Note that
// this means every user of a shared machine is exempt; see the "Put a password
// on the AGW port" section of the documentation.
//
// A loopback source address really does mean this machine: the kernel will not
// accept one that arrived over a network interface.
func agwClientIsLocal(conn net.Conn) bool {
	if conn == nil {
		return false
	}

	var addr, ok = conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return false /* Not something we can judge, so make it log in. */
	}

	return addr.IP.IsLoopback()
}

// loginRequired reports whether a client has to log in before we honour any of
// its other commands.
func (s *AGWServer) loginRequired() bool {
	return len(s.logins) > 0
}

// matchLogin looks for credentials from an "Application Login" frame among
// those configured, and returns the configured user name that matched.
//
// Every set is compared, with no early exit, so how long this takes says
// nothing about which user names exist.  The name it hands back is our own
// configured text rather than the client's, so it is safe to print.
func (s *AGWServer) matchLogin(user string, password string) (string, bool) {
	var matched string
	var accepted bool

	for _, login := range s.logins {
		/* Constant time so a password can't be guessed a character at a time. */
		var userOK = subtle.ConstantTimeCompare([]byte(user), []byte(login.user)) == 1
		var passwordOK = subtle.ConstantTimeCompare([]byte(password), []byte(login.password)) == 1

		if userOK && passwordOK {
			matched = login.user
			accepted = true
		}
	}

	return matched, accepted
}

// parseAGWLogin splits the data of an "Application Login" frame into its user
// name and password.  Each is a fixed AGW_LOGIN_FIELD_LEN byte field, so
// anything shorter is not a login we can check.  Anything longer is somebody
// else's idea of the frame, and the two fields we want are still where the
// protocol says they are.
func parseAGWLogin(data []byte) (string, string, bool) {
	if len(data) < 2*AGW_LOGIN_FIELD_LEN {
		return "", "", false
	}

	return agwLoginField(data[:AGW_LOGIN_FIELD_LEN]),
		agwLoginField(data[AGW_LOGIN_FIELD_LEN : 2*AGW_LOGIN_FIELD_LEN]),
		true
}

// agwLoginField takes the text out of one field of an "Application Login"
// frame.  The protocol describes each as "ended with 0x00 filled till 255
// bytes", so the value stops at the first NUL; what a client leaves in the pad
// after it is not part of the value, and is not necessarily NUL.
func agwLoginField(field []byte) string {
	var before, _, ok = bytes.Cut(field, []byte{0})
	if !ok {
		return string(field) /* No terminator, so the whole field it is. */
	}

	return string(before)
}

// handleClientLogin processes an "Application Login" frame.
//
// The protocol has no reply for this, so a client that gets it wrong finds out
// only by having its later commands ignored.  That is also what happens if no
// login is configured and one is sent anyway.
//
// Any attempt that does not succeed leaves the client logged out, whether the
// credentials were wrong or the frame was not one we could read, so a client
// that has logged in cannot pass the socket on to one that cannot.  A client on
// this machine, which never had to log in, keeps its exemption either way.
func (s *AGWServer) handleClientLogin(client int, cmd *AGWPEMessage) {
	if !s.loginRequired() {
		return /* Nothing to check it against. */
	}

	var user, password, ok = parseAGWLogin(cmd.Data)
	if !ok {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("AGW client application %d sent a malformed login: expected %d bytes of data, got %d.\n",
			client, 2*AGW_LOGIN_FIELD_LEN, len(cmd.Data))
		s.clientLoggedOut(client)

		return
	}

	var matched, accepted = s.matchLogin(user, password)
	if !accepted {
		/* The user name is not echoed back; it is whatever the other end chose to send. */
		text_color_set(DW_COLOR_ERROR)
		dw_printf("AGW client application %d sent an incorrect user name or password.  Its commands will be ignored.\n", client)
		s.clientLoggedOut(client)

		return
	}

	s.clients[client].loggedIn.Store(true)

	text_color_set(DW_COLOR_INFO)
	dw_printf("AGW client application %d logged in as \"%s\".\n", client, matched)
}

func (s *AGWServer) handleClientCommand(client int, cmd *AGWPEMessage) {
	if cmd.Header.DataKind == 'P' { /* Application Login */
		s.handleClientLogin(client, cmd)

		return
	}

	if s.loginRequired() && !s.clients[client].loggedIn.Load() {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("AGW client application %d sent command '%c' without logging in first.  Ignored.\n",
			client, cmd.Header.DataKind)

		return
	}

	switch cmd.Header.DataKind {
	case 'R': /* Request for version number */
		{
			var reply = new(AGWPEMessage)

			reply.Header.DataKind = 'R'
			reply.Header.DataLen = 8
			reply.Data = make([]byte, 8)

			// Xastir only prints this and doesn't care otherwise.
			// APRSIS32 doesn't seem to care.
			// UI-View32 wants on 2000.15 or later.

			binary.LittleEndian.PutUint32(reply.Data[0:4], 2005) // Major version
			binary.LittleEndian.PutUint32(reply.Data[4:8], 127)  // Minor version

			s.sendToClient(client, reply)
		}

	case 'G': /* Ask about radio ports */
		{
			var reply = new(AGWPEMessage)

			reply.Header.DataKind = 'G'

			// Xastir only prints this and doesn't care otherwise.
			// YAAC uses this to identify available channels.

			// The interface manual wants the first to be "Port1"
			// so channel 0 corresponds to "Port1."
			// We can have gaps in the numbering.
			// I wonder what applications will think about that.

			// No other place cares about total number.

			// A server with no audio configuration - one in a test that did
			// not set one up - has nothing to describe.  Standing in an empty
			// configuration reports no ports, which is the truth of it, where
			// reaching through the nil pointer would take the program out.
			var cfg = s.audioConfigP
			if cfg == nil {
				cfg = new(audio_s)
			}

			var count = 0

			for j := range MAX_TOTAL_CHANS {
				if cfg.chan_medium[j] == MEDIUM_RADIO ||
					cfg.chan_medium[j] == MEDIUM_IGATE ||
					cfg.chan_medium[j] == MEDIUM_NETTNC {
					count++
				}
			}

			var info strings.Builder
			fmt.Fprintf(&info, "%d;", count)

			for j := range MAX_TOTAL_CHANS {
				switch cfg.chan_medium[j] {
				case MEDIUM_RADIO:
					// Misleading if using stdin or udp.
					var a = ACHAN2ADEV(j)
					// If I was really ambitious, some description could be provided.
					var names = []string{"first", "second", "third", "fourth", "fifth", "sixth", "seventh", "eighth"}

					if cfg.adev[a].num_channels == 1 {
						fmt.Fprintf(&info, "Port%d %s soundcard mono;", j+1, names[a])
					} else {
						var lr = "left"
						if j&1 > 0 {
							lr = "right"
						}

						fmt.Fprintf(&info, "Port%d %s soundcard %s;", j+1, names[a], lr)
					}

				case MEDIUM_IGATE:
					fmt.Fprintf(&info, "Port%d Internet Gateway;", j+1)

				case MEDIUM_NETTNC:
					// could elaborate with hostname, etc.
					fmt.Fprintf(&info, "Port%d Network TNC;", j+1)

				default:
					// Only list valid channels.
				} // switch
			} // for each channel

			reply.Data = []byte(info.String())
			reply.Header.DataLen = uint32(len(reply.Data))

			s.sendToClient(client, reply)
		}

	case 'g': /* Ask about capabilities of a port. */
		/*
				struct {
				  struct agwpe_s Header;
			 	  unsigned char on_air_baud_rate; 	// 0=1200, 1=2400, 2=4800, 3=9600, ...
				  unsigned char traffic_level;		// 0xff if not in autoupdate mode
				  unsigned char tx_delay;
				  unsigned char tx_tail;
				  unsigned char persist;
				  unsigned char slottime;
				  unsigned char maxframe;
				  unsigned char active_connections;
				  int how_many_bytes_NETLE;
				} reply;
		*/
		var reply = new(AGWPEMessage)

		reply.Header.Portx = cmd.Header.Portx /* Reply with same port number ! */
		reply.Header.DataKind = 'g'
		reply.Header.DataLen = 12

		// YAAC asks for this.
		// Fake it to keep application happy.
		// TODO:  Supply real values instead of just faking it.

		reply.Data = make([]byte, 12)
		reply.Data[0] = 0                                  // on_air_baud_rate
		reply.Data[1] = 1                                  // traffic_level
		reply.Data[2] = 0x19                               // tx_delay
		reply.Data[3] = 4                                  // tx_tail
		reply.Data[4] = 0xc8                               // persist
		reply.Data[5] = 4                                  // slottime
		reply.Data[6] = 7                                  // maxframe
		reply.Data[7] = 0                                  // active_connections
		binary.LittleEndian.PutUint32(reply.Data[8:12], 1) // how_many_bytes

		s.sendToClient(client, reply)

	case 'H': /* Ask about recently heard stations on given port. */
		/* This should send back 20 'H' frames for the most recently heard stations. */
		/* If there are less available, empty frames are sent to make a total of 20. */
		/* Each contains the first and last heard times. */
		{
			/*
				#if 0						// Currently, this information is not being collected.
						struct {
						  struct agwpe_s Header;
					 	  char info[100];
						} reply;


					        memset (&reply.Header, 0, sizeof(reply.Header));
					        reply.Header.DataKind = 'H';

						// TODO:  Implement properly.

					        reply.Header.Portx = cmd.Header.Portx

					        strlcpy (reply.Header.call_from, "WB2OSZ-15 Mon,01Jan2000 01:02:03  Tue,31Dec2099 23:45:56", sizeof(reply.Header.call_from));
						// or                                                  00:00:00                00:00:00

					        strlcpy (agwpe_msg.data, ..., sizeof(agwpe_msg.data));

					        reply.Header.data_len_NETLE = host2netle(strlen(reply.info));

					        send_to_client (client, &reply);
				#endif
			*/
		}

	case 'k': /* Ask to start receiving RAW AX25 frames */
		// Actually it is a toggle so we must be sure to clear it for a new connection.
		s.clients[client].sendRaw = !s.clients[client].sendRaw

	case 'm': /* Ask to start receiving Monitor frames */
		// Actually it is a toggle so we must be sure to clear it for a new connection.
		s.clients[client].sendMonitor = !s.clients[client].sendMonitor

	case 'V': /* Transmit UI data frame (with digipeater path) */
		{
			// Data format is:
			//	1 byte for number of digipeaters.
			//	10 bytes for each digipeater.
			//	data part of message.
			var pid = cmd.Header.PID
			var stemp strings.Builder
			stemp.WriteString(ByteArrayToString(cmd.Header.CallFrom[:]))
			stemp.WriteString(">")
			stemp.WriteString(ByteArrayToString(cmd.Header.CallTo[:]))

			if len(cmd.Data) < 1 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("AGW 'V' message too short to contain digipeater count.\n")

				break
			}

			var ndigi = int(cmd.Data[0])

			if len(cmd.Data) < 1+10*ndigi {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("AGW 'V' message too short for %d digipeaters.\n", ndigi)

				break
			}

			for k := range ndigi {
				var offset = 1 + 10*k
				stemp.WriteString("," + string(cmd.Data[offset:offset+10]))
			}
			// At this point, p now points to info part after digipeaters.

			// Issue 527: NET/ROM routing broadcasts are binary info so we can't treat as string.
			// Originally, I just appended the information part.
			// That was fine until NET/ROM, with binary data, came along.
			// Now we set the information field after creating the packet object.

			stemp.WriteString(": ")

			//text_color_set(DW_COLOR_DEBUG);
			//dw_printf ("Transmit '%s'\n", stemp);

			var pp = AX25FromText(stemp.String(), true)

			if pp == nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Failed to create frame from AGW 'V' message.\n")

				break
			}

			var data = cmd.Data[1+10*ndigi:]
			ax25_set_info(pp, data)

			// Issue 527: NET/ROM routing broadcasts use PID 0xCF which was not preserved here.
			ax25_set_pid(pp, pid)

			/* This goes into the low priority queue because it is an original. */

			/* Note that the protocol has no way to set the "has been used" */
			/* bits in the digipeater fields. */

			/* This explains why the digipeating option is grayed out in */
			/* xastir when using the AGW interface.  */
			/* The current version uses only the 'V' message, not 'K' for transmitting. */

			tq_append(int(cmd.Header.Portx), TQ_PRIO_1_LO, pp)
		}

	case 'K': /* Transmit raw AX.25 frame */
		{
			// Message contains:
			//	port number for transmission.
			//	data length
			//	data which is raw ax.25 frame.
			//

			// Bug fix in version 1.1:
			//
			// The first byte of data is described as:
			//
			// 		the "TNC" to use
			//		00=Port 1
			//		16=Port 2
			//
			// The seems to be redundant; we already a port number in the header.
			// Anyhow, the original code here added one to cmd.data to get the
			// first byte of the frame.  Unfortunately, it did not subtract one from
			// cmd.Header.data_len so we ended up sending an extra byte.

			// TODO: Right now I just use the port (channel) number in the header.
			// What if the second one is inconsistent?
			// - Continue to ignore port number at beginning of data?
			// - Use second one instead?
			// - Error message if a mismatch?
			if cmd.Header.DataLen < 1 || int(cmd.Header.DataLen) > len(cmd.Data) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("AGW 'K' message has invalid data length %d.\n", cmd.Header.DataLen)

				break
			}

			var alevel ALevel
			var pp = AX25FromFrame(cmd.Data[1:cmd.Header.DataLen], alevel)

			if pp == nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Failed to create frame from AGW 'K' message.\n")
			} else {
				/* How can we determine if it is an original or repeated message? */
				/* If there is at least one digipeater in the frame, AND */
				/* that digipeater has been used, it should go out quickly thru */
				/* the high priority queue. */
				/* Otherwise, it is an original for the low priority queue. */
				if ax25_get_num_repeaters(pp) >= 1 &&
					ax25_get_h(pp, AX25_REPEATER_1) > 0 {
					tq_append(int(cmd.Header.Portx), TQ_PRIO_0_HI, pp)
				} else {
					tq_append(int(cmd.Header.Portx), TQ_PRIO_1_LO, pp)
				}
			}
		}

	case 'X': /* Register CallSign  */
		{
			/*
				struct {
				  struct agwpe_s Header;
				  char data;			// 1 = success, 0 = failure
				} reply;
			*/
			var ok byte

			// The protocol spec says it is an error to register the same one more than once.
			// Too much trouble.  Report success if the channel is valid.

			var channel = int(cmd.Header.Portx)

			if s.connectedModeAllowed(cmd.Header.Portx) {
				ok = 1

				dlq_register_callsign(ByteArrayToString(cmd.Header.CallFrom[:]), channel, client)
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("AGW protocol error.  Register callsign for invalid channel %d.\n", channel)

				ok = 0
			}

			var reply = new(AGWPEMessage)
			reply.Header.DataKind = 'X'
			reply.Header.Portx = cmd.Header.Portx
			copy(reply.Header.CallFrom[:], cmd.Header.CallFrom[:])
			reply.Header.DataLen = 1
			reply.Data = []byte{ok}

			s.sendToClient(client, reply)
		}

	case 'x': /* Unregister CallSign  */
		var channel = int(cmd.Header.Portx)

		if s.connectedModeAllowed(cmd.Header.Portx) {
			dlq_unregister_callsign(ByteArrayToString(cmd.Header.CallFrom[:]), channel, client)
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("AGW protocol error.  Unregister callsign for invalid channel %d.\n", channel)
		}
	/* No response is expected. */

	case 'C', 'v', 'c':
		/* C: Connect, Start an AX.25 Connection  */
		/* v: Connect VIA, Start an AX.25 circuit thru digipeaters */
		/* c: Connection with non-standard PID */
		{
			if !s.connectedModeAllowed(cmd.Header.Portx) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("AGW connect command on unsupported channel %d ignored.\n", cmd.Header.Portx)

				break
			}
			/*
				        struct via_info {
				          unsigned char num_digi;	// Expect to be in range 1 to 7.  Why not up to 8?
					  char dcall[7][10];
				        }
			*/
			var callsigns [AX25_MAX_ADDRS]string
			callsigns[AX25_SOURCE] = ByteArrayToString(cmd.Header.CallFrom[:])
			callsigns[AX25_DESTINATION] = ByteArrayToString(cmd.Header.CallTo[:])

			var pid byte = 0xf0 /* normal for AX.25 I frames. */
			if cmd.Header.DataKind == 'c' {
				pid = cmd.Header.PID /* non standard for NETROM, TCP/IP, etc. */
			}

			var num_calls = 2 /* 2 plus any digipeaters. */

			if cmd.Header.DataKind == 'v' {
				if len(cmd.Data) < 1 {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("\n")
					dw_printf("AGW client, connect via, has invalid payload: too short\n")

					break
				}

				var numDigi = int(cmd.Data[0])

				if numDigi >= 1 && numDigi <= 7 {
					var expectedLen = uint32(numDigi)*10 + 1
					if cmd.Header.DataLen != expectedLen && cmd.Header.DataLen != expectedLen+1 {
						// I'm getting 1 more than expected from AGWterminal.
						text_color_set(DW_COLOR_ERROR)
						dw_printf("AGW client, connect via, has data len, %d when %d expected.\n", cmd.Header.DataLen, expectedLen)
					}

					if len(cmd.Data) < 1+10*numDigi {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("\n")
						dw_printf("AGW client, connect via, payload too short for %d digipeaters.\n", numDigi)

						break
					}

					for j := range numDigi {
						callsigns[AX25_REPEATER_1+j] = ByteArrayToString(cmd.Data[1+10*j : 1+10*j+10])
						num_calls++
					}
				} else {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("\n")
					dw_printf("AGW client, connect via, has invalid number of digipeaters = %d\n", numDigi)

					break
				}
			}

			dlq_connect_request(callsigns, num_calls, int(cmd.Header.Portx), client, int(pid))
		}

	case 'D': /* Send Connected Data */
		{
			if !s.connectedModeAllowed(cmd.Header.Portx) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("AGW 'D' command on unsupported channel %d ignored.\n", cmd.Header.Portx)

				break
			}

			if int(cmd.Header.DataLen) > len(cmd.Data) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("AGW 'D' message has invalid data length %d.\n", cmd.Header.DataLen)

				break
			}

			var callsigns [AX25_MAX_ADDRS]string
			const num_calls = 2 // only first 2 used.  Digipeater path must be remembered from connect request.

			callsigns[AX25_SOURCE] = ByteArrayToString(cmd.Header.CallFrom[:])
			callsigns[AX25_DESTINATION] = ByteArrayToString(cmd.Header.CallTo[:])

			dlq_xmit_data_request(callsigns, num_calls, int(cmd.Header.Portx), client, int(cmd.Header.PID), cmd.Data[:cmd.Header.DataLen])
		}

	case 'd': /* Disconnect, Terminate an AX.25 Connection */
		{
			if !s.connectedModeAllowed(cmd.Header.Portx) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("AGW 'd' command on unsupported channel %d ignored.\n", cmd.Header.Portx)

				break
			}

			var callsigns [AX25_MAX_ADDRS]string
			const num_calls = 2 // only first 2 used.

			callsigns[AX25_SOURCE] = ByteArrayToString(cmd.Header.CallFrom[:])
			callsigns[AX25_DESTINATION] = ByteArrayToString(cmd.Header.CallTo[:])

			dlq_disconnect_request(callsigns, num_calls, int(cmd.Header.Portx), client)
		}

	case 'M': /* Send UNPROTO Information (no digipeater path) */
		/*
					Added in version 1.3.
					This is the same as 'V' except there is no provision for digipeaters.
					TODO: combine 'V' and 'M' into one case.
					AGWterminal sends this for beacon or ask QRA.

					<<< Send UNPROTO Information from AGWPE client application 0, total length = 253
					        portx = 0, datakind = 'M', pid = 0x00
					        call_from = "WB2OSZ-15", call_to = "BEACON"
					        data_len = 217, user_reserved = 556, data =
					  000:  54 68 69 73 20 76 65 72 73 69 6f 6e 20 75 73 65  This version use
					   ...

					<<< Send UNPROTO Information from AGWPE client application 0, total length = 37
					        portx = 0, datakind = 'M', pid = 0x00
					        call_from = "WB2OSZ-15", call_to = "QRA"
					        data_len = 1, user_reserved = 31759424, data =
					  000:  0d                                               .
			                                          .

					There is also a report of it coming from UISS.

					<<< Send UNPROTO Information from AGWPE client application 0, total length = 50
						portx = 0, port_hi_reserved = 0
						datakind = 77 = 'M', kind_hi = 0
						call_from = "JH4XSY", call_to = "APRS"
						data_len = 14, user_reserved = 0, data =
					  000:  21 22 3c 43 2e 74 71 6c 48 72 71 21 21 5f        !"<C.tqlHrq!!_
		*/
		{
			var pid = cmd.Header.PID
			var stemp = ByteArrayToString(cmd.Header.CallFrom[:]) + ">" + ByteArrayToString(cmd.Header.CallTo[:]) + ": "

			// Issue 527: NET/ROM routing broadcasts are binary info so we can't treat as string.
			// Originally, I just appended the information part as a text string.
			// That was fine until NET/ROM, with binary data, came along.
			// Now we set the information field after creating the packet object.

			//text_color_set(DW_COLOR_DEBUG);
			//dw_printf ("Transmit '%s'\n", stemp);

			var pp = AX25FromText(stemp, true)

			if pp == nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Failed to create frame from AGW 'M' message.\n")

				break
			}

			ax25_set_info(pp, cmd.Data)
			// Issue 527: NET/ROM routing broadcasts use PID 0xCF which was not preserved here.
			ax25_set_pid(pp, pid)

			tq_append(int(cmd.Header.Portx), TQ_PRIO_1_LO, pp)
		}

	case 'y': /* Ask Outstanding frames waiting on a Port  */
		/* Number of frames sitting in transmit queue for specified channel. */
		{
			/*
				struct {
				  struct agwpe_s Header;
				  int data_NETLE;			// Little endian order.
				} reply;
			*/
			var reply = new(AGWPEMessage)

			reply.Header.Portx = cmd.Header.Portx /* Reply with same port number */
			reply.Header.DataKind = 'y'
			reply.Header.DataLen = 4

			var n = 0
			if cmd.Header.Portx < MAX_RADIO_CHANS {
				// Count both normal and expedited in transmit queue for given channel.
				n = tq_count(int(cmd.Header.Portx), -1, "", "", false)
			}

			reply.Data = make([]byte, 4)
			binary.LittleEndian.PutUint32(reply.Data, uint32(n))

			s.sendToClient(client, reply)
		}

	case 'Y': /* How Many Outstanding frames wait for tx for a particular station  */
		// This is different than the above 'y' because this refers to a specific
		// link in connected mode.

		// This would be useful for a couple different purposes.

		// When sending bulk data, we want to keep a fair amount queued up to take
		// advantage of large window sizes (MAXFRAME, EMAXFRAME).  On the other
		// hand we don't want to get TOO far ahead when transferring a large file.

		// Before disconnecting from another station, it would be good to know
		// that it actually received the last message we sent.  For this reason,
		// I think it would be good for this to include information frames that were
		// transmitted but not yet acknowledged.
		// You could say that a particular frame is still waiting to be sent even
		// if was already sent because it could be sent again if lost previously.

		// The documentation is inconsistent about the address order.
		// One place says "callfrom" is my callsign and "callto" is the other guy.
		// That would make sense.  We are asking about frames going to the other guy.

		// But another place says it depends on who initiated the connection.
		//
		//	"If we started the connection CallFrom=US and CallTo=THEM
		//	If the other end started the connection CallFrom=THEM and CallTo=US"
		//
		// The response description says nothing about the order; it just mentions two addresses.
		// If you are writing a client or server application, the order would
		// be clear but right here it could be either case.
		//
		// Another version of the documentation mentioned the source address being optional.
		//

		// The only way to get this information is from inside the data link state machine.
		// We will send a request to it and the result coming out will be used to
		// send the reply back to the client application.
		{
			if !s.connectedModeAllowed(cmd.Header.Portx) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("AGW 'Y' command on unsupported channel %d ignored.\n", cmd.Header.Portx)

				break
			}

			var callsigns [AX25_MAX_ADDRS]string
			const num_calls = 2 // only first 2 used.

			callsigns[AX25_SOURCE] = ByteArrayToString(cmd.Header.CallFrom[:])
			callsigns[AX25_DESTINATION] = ByteArrayToString(cmd.Header.CallTo[:])

			dlq_outstanding_frames_request(callsigns, num_calls, int(cmd.Header.Portx), client)
		}

	default:
		text_color_set(DW_COLOR_ERROR)
		dw_printf("--- Unexpected Command from application %d using AGW protocol:\n", client)
		s.debugPrint(FROM_CLIENT, client, cmd)
	}
} /* end handleClientCommand */
