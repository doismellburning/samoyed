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
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"time"
)

var client_sock [MAX_NET_CLIENTS]net.Conn

/* Socket for */
/* communication with client application. */

var enable_send_raw_to_client [MAX_NET_CLIENTS]bool

/* Should we send received packets to client app in raw form? */
/* Note that it starts as false for a new connection. */
/* the client app must send a command to enable this. */

var enable_send_monitor_to_client [MAX_NET_CLIENTS]bool

/* Should we send received packets to client app in monitor form? */
/* Note that it starts as false for a new connection. */
/* the client app must send a command to enable this. */

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

var debug_client int = 0 /* Debug option: Print information flowing from and to client. */

func server_set_debug(n int) {
	debug_client = n
}

func debug_print(fromto fromto_t, client int, pmsg *AGWPEMessage) {
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

	hex_dump(pmsg.Data[:pmsg.Header.DataLen])
}

/*-------------------------------------------------------------------
 *
 * Name:        server_init
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
 * Outputs:
 *
 * Description:	This starts at least two threads:
 *		  *  one to listen for a connection from client app.
 *		  *  one or more to listen for commands from client app.
 *		so the main application doesn't block while we wait for these.
 *
 *--------------------------------------------------------------------*/

func server_init(audio_config_p *audio_s, mc *misc_config_s) {
	var server_port = mc.agwpe_port /* Usually 8000 but can be changed. */

	/* TODO KG
	   #if DEBUG
	   	text_color_set(DW_COLOR_DEBUG);
	   	dw_printf ("server_init ( %d )\n", server_port);
	   	debug_a = 1;
	   #endif
	*/

	save_audio_config_p = audio_config_p

	for client := 0; client < MAX_NET_CLIENTS; client++ {
		enable_send_raw_to_client[client] = false
		enable_send_monitor_to_client[client] = false
	}

	if server_port == 0 {
		text_color_set(DW_COLOR_INFO)
		dw_printf("Disabled AGW network client port.\n")

		return
	}

	/*
	 * This waits for a client to connect and sets an available client_sock[n].
	 */
	go server_connect_listen_thread(server_port)

	/*
	 * These read messages from client when client_sock[n] is valid.
	 * Currently we start up a separate thread for each potential connection.
	 * Possible later refinement.  Start one now, others only as needed.
	 */
	for client := 0; client < MAX_NET_CLIENTS; client++ {
		go cmd_listen_thread(client)
	}
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
 * Outputs:	client_sock	- File descriptor for communicating with client app.
 *
 * Description:	Wait for connection request from client and establish
 *		communication.
 *		Note that the client can go away and come back again and
 *		re-establish communication without restarting this application.
 *
 *--------------------------------------------------------------------*/

func server_connect_listen_thread(server_port int) {
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
	    	dw_printf("Binding to port %d ... \n", server_port);
	#endif
	*/
	var listener, listenErr = net.Listen("tcp", fmt.Sprintf(":%d", server_port))
	if listenErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("connect_listen_thread: Listen failed: %s", listenErr)

		return
	}

	/* Version 1.3 - as suggested by G8BPQ. */
	/* Without this, if you kill the application then try to run it */
	/* again quickly the port number is unavailable for a while. */
	/* Don't try doing the same thing On Windows; It has a different meaning. */
	/* http://stackoverflow.com/questions/14388706/socket-options-so-reuseaddr-and-so-reuseport-how-do-they-differ-do-they-mean-t */
	if tcpListener, ok := listener.(*net.TCPListener); ok {
		file, err := tcpListener.File()
		if err == nil {
			defer file.Close()

			syscall.SetsockoptInt(int(file.Fd()), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		}
	}

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
	 	dw_printf("opened socket as fd (%d) on port (%d) for stream i/o\n", listen_sock, ntohs(sockaddr.sin_port) );
	#endif
	*/

	for {
		var client = -1
		for c := 0; c < MAX_NET_CLIENTS && client < 0; c++ {
			if client_sock[c] == nil {
				client = c
			}
		}

		if client >= 0 {
			text_color_set(DW_COLOR_INFO)
			dw_printf("Ready to accept AGW client application %d on port %d ...\n", client, server_port)

			var conn, acceptErr = listener.Accept()
			if acceptErr != nil {
				dw_printf("Accept failed: %v\n", acceptErr)
				continue
			}

			client_sock[client] = conn

			text_color_set(DW_COLOR_INFO)
			dw_printf("\nAttached to AGW client application %d...\n\n", client)

			/*
			 * The command to change this is actually a toggle, not explicit on or off.
			 * Make sure it has proper state when we get a new connection.
			 */
			enable_send_raw_to_client[client] = false
			enable_send_monitor_to_client[client] = false
		} else {
			SLEEP_SEC(1) /* wait then check again if more clients allowed. */
		}
	}
}

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

func server_send_rec_packet(channel int, pp *packet_t, fbuf []byte) {
	/*
	 * RAW format
	 */
	for client := 0; client < MAX_NET_CLIENTS; client++ {
		if enable_send_raw_to_client[client] && client_sock[client] != nil {
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

			if debug_client > 0 {
				debug_print(TO_CLIENT, client, agwpe_msg)
			}

			var _, err = agwpe_msg.Write(client_sock[client], binary.LittleEndian)
			if err != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("\nError sending message to AGW client application.  Closing connection.\n\n")
				client_sock[client].Close()
				client_sock[client] = nil
				dlq_client_cleanup(client)
			}
		}
	}

	// Application might want more human readable format.

	server_send_monitored(channel, pp, 0)
} /* end server_send_rec_packet */

func server_send_monitored(channel int, pp *packet_t, own_xmit int) {
	/*
	 * MONITOR format - 	'I' for information frames.
	 *			'U' for unnumbered information.
	 *			'S' for supervisory and other unnumbered.
	 *
	 *			'T' for own transmitted frames.
	 */
	for client := 0; client < MAX_NET_CLIENTS; client++ {
		if enable_send_monitor_to_client[client] && client_sock[client] != nil {
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

			var pinfo = ax25_get_info(pp)
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

			if debug_client > 0 {
				debug_print(TO_CLIENT, client, agwpe_msg)
			}

			var _, err = agwpe_msg.Write(client_sock[client], binary.LittleEndian)
			if err != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("\nError sending message to AGW client application %d (%s).  Closing connection.\n\n", client, err)
				client_sock[client].Close()
				client_sock[client] = nil
				dlq_client_cleanup(client)
			}
		}
	}
} /* server_send_monitored */

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
		var via string // complete via path

		for j := 0; j < num_digi; j++ {
			if j != 0 {
				via += "," // comma if not first address
			}

			var digiaddr = ax25_get_addr_with_ssid(pp, AX25_REPEATER_1+j)
			via += digiaddr
			/*
				#if 0  // Mark each used with * as seen in UZ7HO SoundModem.
					    if (ax25_get_h(pp, AX25_REPEATER_1 + j)) {
				#else */
			// Mark only last used (i.e. the heard station) with * as in TNC-2 Monitoring format.
			if AX25_REPEATER_1+j == ax25_get_heard(pp) {
				// #endif
				via += "*"
			}
		}

		return []byte(fmt.Sprintf(" %d:Fm %s To %s Via %s ", channel+1, src, dst, via))
	} else {
		return []byte(fmt.Sprintf(" %d:Fm %s To %s ", channel+1, src, dst))
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
	var pinfo = ax25_get_info(pp)

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
 * Name:        server_link_established
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

func server_link_established(channel int, client int, remote_call string, own_call string, incoming bool) {
	var reply = new(AGWPEMessage)

	reply.Header.Portx = byte(channel)
	reply.Header.DataKind = 'C'

	copy(reply.Header.CallFrom[:], []byte(remote_call))
	copy(reply.Header.CallTo[:], []byte(own_call))

	// Question:  Should the via path be provided too?

	if incoming {
		// Other end initiated the connection.
		reply.Data = []byte(fmt.Sprintf("*** CONNECTED To Station %s\r", remote_call))
	} else {
		// We started the connection.
		reply.Data = []byte(fmt.Sprintf("*** CONNECTED With Station %s\r", remote_call))
	}

	reply.Data = append(reply.Data, 0)
	reply.Header.DataLen = uint32(len(reply.Data))

	send_to_client(client, reply)
} /* end server_link_established */

/*-------------------------------------------------------------------
 *
 * Name:        server_link_terminated
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

func server_link_terminated(channel int, client int, remote_call string, own_call string, timeout bool) {
	var reply = new(AGWPEMessage)

	reply.Header.Portx = byte(channel)
	reply.Header.DataKind = 'd'
	copy(reply.Header.CallFrom[:], []byte(remote_call))
	copy(reply.Header.CallTo[:], []byte(own_call))

	if timeout {
		reply.Data = []byte(fmt.Sprintf("*** DISCONNECTED RETRYOUT With %s\r", remote_call))
	} else {
		reply.Data = []byte(fmt.Sprintf("*** DISCONNECTED From Station %s\r", remote_call))
	}

	reply.Data = append(reply.Data, 0)
	reply.Header.DataLen = uint32(len(reply.Data))

	send_to_client(client, reply)
} /* end server_link_terminated */

/*-------------------------------------------------------------------
 *
 * Name:        server_rec_conn_data
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

func server_rec_conn_data(channel int, client int, remote_call string, own_call string, pid int, data []byte) {
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

	send_to_client(client, reply)
} /* end server_rec_conn_data */

/*-------------------------------------------------------------------
 *
 * Name:        server_outstanding_frames_reply
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

func server_outstanding_frames_reply(channel int, client int, own_call string, remote_call string, count int) {
	var reply = new(AGWPEMessage)

	reply.Header.Portx = byte(channel)
	reply.Header.DataKind = 'Y'

	copy(reply.Header.CallFrom[:], []byte(own_call))
	copy(reply.Header.CallTo[:], []byte(remote_call))

	reply.Header.DataLen = 4
	reply.Data = make([]byte, 4)
	binary.LittleEndian.PutUint32(reply.Data, uint32(count))

	send_to_client(client, reply)
} /* end server_outstanding_frames_reply */

/*-------------------------------------------------------------------
 *
 * Name:        cmd_listen_thread
 *
 * Purpose:     Wait for command messages from an application.
 *
 * Inputs:	arg		- client number, 0 .. MAX_NET_CLIENTS-1
 *
 * Outputs:	client_sock[n]	- File descriptor for communicating with client app.
 *
 * Description:	Process messages from the client application.
 *		Note that the client can go away and come back again and
 *		re-establish communication without restarting this application.
 *
 *--------------------------------------------------------------------*/

func send_to_client(client int, reply_p *AGWPEMessage) {
	if client_sock[client] == nil {
		return
	}

	var ph = reply_p.Header

	if ph.DataLen > 4096 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid data length %d for AGW protocol message to client %d.\n", ph.DataLen, client)
		debug_print(TO_CLIENT, client, reply_p)
	}

	if debug_client > 0 {
		debug_print(TO_CLIENT, client, reply_p)
	}

	reply_p.Write(client_sock[client], binary.LittleEndian)
}

func cmd_listen_thread(client int) {
	Assert(client >= 0 && client < MAX_NET_CLIENTS)

	for {
		for client_sock[client] == nil {
			SLEEP_SEC(1) /* Not connected.  Try again later. */
		}

		var cmd = new(AGWPEMessage)

		var readErr = binary.Read(client_sock[client], binary.LittleEndian, cmd.Header)
		if readErr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("\nError getting message header from AGW client application %d: %s\n", client, readErr)
			dw_printf("Closing connection.\n\n")
			client_sock[client].Close()
			client_sock[client] = nil
			dlq_client_cleanup(client)

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

		if cmd.Header.DataLen > 0 {
			var b = make([]byte, cmd.Header.DataLen)
			var n, readErr = client_sock[client].Read(b)

			if n != int(cmd.Header.DataLen) || readErr != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("\nError getting message data from AGW client application %d: %s\n", client, readErr)
				dw_printf("Tried to read %d bytes, got %d.\n", cmd.Header.DataLen, n)
				dw_printf("Closing connection.\n\n")
				client_sock[client].Close()
				client_sock[client] = nil
				dlq_client_cleanup(client)

				return
			}

			cmd.Data = b

			if n >= 0 {
				cmd.Data = append(cmd.Data, 0) // Tidy if we print for debug.
			}
		}

		/*
		 * print & process message from client.
		 */

		if debug_client > 0 {
			debug_print(FROM_CLIENT, client, cmd)
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

				send_to_client(client, reply)
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

				var count = 0

				for j := 0; j < MAX_TOTAL_CHANS; j++ {
					if save_audio_config_p.chan_medium[j] == MEDIUM_RADIO ||
						save_audio_config_p.chan_medium[j] == MEDIUM_IGATE ||
						save_audio_config_p.chan_medium[j] == MEDIUM_NETTNC {
						count++
					}
				}

				var info = fmt.Sprintf("%d;", count)

				for j := 0; j < MAX_TOTAL_CHANS; j++ {
					switch save_audio_config_p.chan_medium[j] {
					case MEDIUM_RADIO:
						// Misleading if using stdin or udp.
						var a = ACHAN2ADEV(j)
						// If I was really ambitious, some description could be provided.
						var names = []string{"first", "second", "third", "fourth", "fifth", "sixth", "seventh", "eighth"}

						if save_audio_config_p.adev[a].num_channels == 1 {
							info += fmt.Sprintf("Port%d %s soundcard mono;", j+1, names[a])
						} else {
							var lr = "left"
							if j&1 > 0 {
								lr = "right"
							}

							info += fmt.Sprintf("Port%d %s soundcard %s;", j+1, names[a], lr)
						}

					case MEDIUM_IGATE:
						info += fmt.Sprintf("Port%d Internet Gateway;", j+1)

					case MEDIUM_NETTNC:
						// could elaborate with hostname, etc.
						info += fmt.Sprintf("Port%d Network TNC;", j+1)

					default:
						// Only list valid channels.
					} // switch
				} // for each channel

				reply.Data = []byte(info)
				reply.Header.DataLen = uint32(len(reply.Data))

				send_to_client(client, reply)
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

			send_to_client(client, reply)

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
			enable_send_raw_to_client[client] = !enable_send_raw_to_client[client]

		case 'm': /* Ask to start receiving Monitor frames */
			// Actually it is a toggle so we must be sure to clear it for a new connection.
			enable_send_monitor_to_client[client] = !enable_send_monitor_to_client[client]

		case 'V': /* Transmit UI data frame (with digipeater path) */
			{
				// Data format is:
				//	1 byte for number of digipeaters.
				//	10 bytes for each digipeater.
				//	data part of message.
				var pid = cmd.Header.PID
				var stemp = ByteArrayToString(cmd.Header.CallFrom[:])
				stemp += ">"
				stemp += ByteArrayToString(cmd.Header.CallTo[:])

				var ndigi = int(cmd.Data[0])

				for k := 0; k < ndigi; k++ {
					var offset = 1 + 10*k
					stemp += "," + string(cmd.Data[offset:offset+10])
				}
				// At this point, p now points to info part after digipeaters.

				// Issue 527: NET/ROM routing broadcasts are binary info so we can't treat as string.
				// Originally, I just appended the information part.
				// That was fine until NET/ROM, with binary data, came along.
				// Now we set the information field after creating the packet object.

				stemp += ": "

				//text_color_set(DW_COLOR_DEBUG);
				//dw_printf ("Transmit '%s'\n", stemp);

				var pp = ax25_from_text(stemp, true)

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
				var alevel alevel_t
				var pp = ax25_from_frame(cmd.Data[1:cmd.Header.DataLen], alevel)

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

		case 'P': /* Application Login  */

			// Silently ignore it.

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

				// Connected mode can only be used with internal modems.

				if channel < MAX_RADIO_CHANS && save_audio_config_p.chan_medium[channel] == MEDIUM_RADIO {
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

				send_to_client(client, reply)
			}

		case 'x': /* Unregister CallSign  */
			var channel = int(cmd.Header.Portx)

			// Connected mode can only be used with internal modems.

			if channel < MAX_RADIO_CHANS && save_audio_config_p.chan_medium[channel] == MEDIUM_RADIO {
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
					var v struct {
						num_digi byte
						dcall    [7][10]byte
					}

					binary.Decode(cmd.Data, binary.LittleEndian, v) // TODO KG Explicitly check err?

					if v.num_digi >= 1 && v.num_digi <= 7 {
						if cmd.Header.DataLen != uint32(v.num_digi)*10+1 && cmd.Header.DataLen != uint32(v.num_digi)*10+2 {
							// I'm getting 1 more than expected from AGWterminal.
							text_color_set(DW_COLOR_ERROR)
							dw_printf("AGW client, connect via, has data len, %d when %d expected.\n", cmd.Header.DataLen, v.num_digi*10+1)
						}

						for j := byte(0); j < v.num_digi; j++ {
							callsigns[AX25_REPEATER_1+j] = ByteArrayToString(v.dcall[j][:])
							num_calls++
						}
					} else {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("\n")
						dw_printf("AGW client, connect via, has invalid number of digipeaters = %d\n", v.num_digi)
					}
				}

				dlq_connect_request(callsigns, num_calls, int(cmd.Header.Portx), client, int(pid))
			}

		case 'D': /* Send Connected Data */
			{
				var callsigns [AX25_MAX_ADDRS]string
				const num_calls = 2 // only first 2 used.  Digipeater path must be remembered from connect request.

				callsigns[AX25_SOURCE] = ByteArrayToString(cmd.Header.CallFrom[:])
				callsigns[AX25_DESTINATION] = ByteArrayToString(cmd.Header.CallTo[:])

				dlq_xmit_data_request(callsigns, num_calls, int(cmd.Header.Portx), client, int(cmd.Header.PID), cmd.Data)
			}

		case 'd': /* Disconnect, Terminate an AX.25 Connection */
			{
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

				var pp = ax25_from_text(stemp, true)

				if pp == nil {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Failed to create frame from AGW 'M' message.\n")
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

				send_to_client(client, reply)
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
				var callsigns [AX25_MAX_ADDRS]string
				const num_calls = 2 // only first 2 used.

				callsigns[AX25_SOURCE] = ByteArrayToString(cmd.Header.CallFrom[:])
				callsigns[AX25_DESTINATION] = ByteArrayToString(cmd.Header.CallTo[:])

				dlq_outstanding_frames_request(callsigns, num_calls, int(cmd.Header.Portx), client)
			}

		default:
			text_color_set(DW_COLOR_ERROR)
			dw_printf("--- Unexpected Command from application %d using AGW protocol:\n", client)
			debug_print(FROM_CLIENT, client, cmd)

		}
	}
} /* end send_to_client */
