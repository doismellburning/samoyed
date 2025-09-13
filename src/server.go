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

// #include "direwolf.h"		// Sets _WIN32_WINNT for XP API level needed by ws2tcpip.h
// #include <stdlib.h>
// #include <sys/types.h>
// #include <sys/ioctl.h>
// #include <sys/socket.h>
// #include <netinet/in.h>
// #include <errno.h>
// #include <unistd.h>
// #include <stdio.h>
// #include <assert.h>
// #include <string.h>
// #include <time.h>
// #include <ctype.h>
// #include <stddef.h>
// #include "tq.h"
// #include "ax25_pad.h"
// #include "textcolor.h"
// #include "audio.h"
// #include "server.h"
// #include "dlq.h"
import "C"

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"unsafe"
)

var client_sock[MAX_NET_CLIENTS]net.Conn
/* Socket for */
/* communication with client application. */

var enable_send_raw_to_client[MAX_NET_CLIENTS]bool
/* Should we send received packets to client app in raw form? */
/* Note that it starts as false for a new connection. */
/* the client app must send a command to enable this. */

var enable_send_monitor_to_client[MAX_NET_CLIENTS]bool
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

var debug_client C.int = 0 /* Debug option: Print information flowing from and to client. */

func server_set_debug(n C.int) {
	debug_client = n
}

func debug_print(fromto fromto_t, client C.int, pmsg *AGWPEMessage) {

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

	dw_printf("%s %s %s AGWPE client application %d, total length = %d\n",
		FROMTO_PREFIX[fromto], datakind, direction, client)

	dw_printf("\tportx = %d, datakind = '%c', pid = 0x%02x\n", pmsg.Header.Portx, pmsg.Header.DataKind, pmsg.Header.PID)
	dw_printf("\tcall_from = \"%s\", call_to = \"%s\"\n", pmsg.Header.CallFrom, pmsg.Header.CallTo)
	dw_printf("\tdata_len = %d, user_reserved = %d, data =\n", pmsg.Header.DataLen, pmsg.Header.UserReserved)

	// FIXME KG hex_dump ((*C.uchar)(pmsg) + sizeof(struct agwpe_s), netle2host(pmsg.data_len_NETLE));
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

func server_init(audio_config_p *C.struct_audio_s, mc *C.struct_misc_config_s) {

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
	for client := C.int(0); client < MAX_NET_CLIENTS; client++ {
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

func server_connect_listen_thread(server_port C.int) {
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
 *		fbuf		- Address of raw received frame buffer.
 *		flen		- Length of raw received frame.
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

func server_send_rec_packet(channel C.int, pp C.packet_t, fbuf *C.uchar, flen C.int) {

	/*
	 * RAW format
	 */
	for client := C.int(0); client < MAX_NET_CLIENTS; client++ {

		if enable_send_raw_to_client[client] && client_sock[client] != nil {

			var agwpe_msg = new(AGWPEMessage)

			agwpe_msg.Header.Portx = byte(channel)

			agwpe_msg.Header.DataKind = 'K'

			var callFrom [AX25_MAX_ADDR_LEN]C.char
			C.ax25_get_addr_with_ssid(pp, AX25_SOURCE, &callFrom[0])
			// FIXME KG agwpe_msg.Header.CallFrom = callFrom

			var callTo [AX25_MAX_ADDR_LEN]C.char
			C.ax25_get_addr_with_ssid(pp, AX25_DESTINATION, &callTo[0])
			// FIXME KG agwpe_msg.Header.CallTo = callTo

			agwpe_msg.Header.DataLen = uint32(flen + 1)
			agwpe_msg.Data = make([]byte, flen + 1)

			/* Stick in extra byte for the "TNC" to use. */

			agwpe_msg.Data[0] = byte(channel) << 4 // Was 0.  Fixed in 1.8.

			copy(agwpe_msg.Data[1:], C.GoBytes(unsafe.Pointer(fbuf), flen))

			if debug_client > 0 {
				debug_print(TO_CLIENT, client, agwpe_msg)
			}

			var err = binary.Write(client_sock[client], binary.LittleEndian, agwpe_msg)

			if err != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("\nError sending message to AGW client application.  Closing connection.\n\n")
				client_sock[client].Close()
				client_sock[client] = nil
				C.dlq_client_cleanup(client)
			}
		}
	}

	// Application might want more human readable format.

	server_send_monitored(channel, pp, 0)

} /* end server_send_rec_packet */

func server_send_monitored(channel C.int, pp C.packet_t, own_xmit C.int) {
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

			var callFrom [AX25_MAX_ADDR_LEN]C.char
			C.ax25_get_addr_with_ssid(pp, AX25_SOURCE, &callFrom[0])
			// FIXME KG Copy back

			var callTo [AX25_MAX_ADDR_LEN]C.char
			C.ax25_get_addr_with_ssid(pp, AX25_DESTINATION, &callTo[0])
			// FIXME KG Copy back

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

			var desc []byte
			agwpe_msg.Header.DataKind, desc = mon_desc(pp)

			if own_xmit > 0 {
				// Should we include all own transmitted frames or only UNPROTO?
				// Discussion:  https://github.com/wb2osz/direwolf/issues/585
				if agwpe_msg.Header.DataKind != 'U' {
					break
				}
				agwpe_msg.Header.DataKind = 'T'
			}
			agwpe_msg.Data = agwpe_msg.Data + desc

			// Timestamp with [...]\r

			/* FIXME KG
			   time_t clock = time(nil);
			   struct tm *tm = localtime(&clock);		// TODO: use localtime_r ?
			   char ts[32];
			   snprintf (ts, sizeof(ts), "[%02d:%02d:%02d]\r", tm.tm_hour, tm.tm_min, tm.tm_sec);
			   strlcat ((char*)(agwpe_msg.data), ts, sizeof(agwpe_msg.data));
			*/

			// Information if any with \r.

			var pinfo *C.uchar
			   var info_len = C.ax25_get_info (pp, &pinfo);
			   var msg_data_len = len(agwpe_msg.Data);	// result length so far

			if info_len > 0 && pinfo != nil {
				// Issue 367: Use of strlcat truncated information part at any nul character.
				// Use memcpy instead to preserve binary data, e.g. NET/ROM.
				memcpy(agwpe_msg.data+msg_data_len, pinfo, info_len)
				msg_data_len += info_len
				agwpe_msg.data[msg_data_len] = '\r'
				msg_data_len++
			}

			agwpe_msg.data[msg_data_len] = 0 // add nul at end, included in length.
			msg_data_len++
			agwpe_msg.Header.data_len_NETLE = host2netle(msg_data_len)

			if debug_client {
				debug_print(TO_CLIENT, client, &agwpe_msg.Header, sizeof(agwpe_msg.Header)+netle2host(agwpe_msg.Header.data_len_NETLE))
			}

			err = SOCK_SEND(client_sock[client], &agwpe_msg, sizeof(agwpe_msg.Header)+netle2host(agwpe_msg.Header.data_len_NETLE))

			if err <= 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("\nError sending message to AGW client application %d.  Closing connection.\n\n", client)
				close(client_sock[client])
				client_sock[client] = -1
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

func mon_addrs(channel C.int, pp C.packet_t) []byte {

	var src [AX25_MAX_ADDR_LEN]C.char
	ax25_get_addr_with_ssid(pp, AX25_SOURCE, src)

	var dst [AX25_MAX_ADDR_LEN]C.char
	ax25_get_addr_with_ssid(pp, AX25_DESTINATION, dst)

	var num_digi = ax25_get_num_repeaters(pp)

	if num_digi > 0 {
		var via [AX25_MAX_REPEATERS * (AX25_MAX_ADDR_LEN + 1)]C.char // complete via path
		strlcpy(via, "", sizeof(via))

		for j := 0; j < num_digi; j++ {
			var digiaddr [AX25_MAX_ADDR_LEN]C.char

			if j != 0 {
				strlcat(via, ",", sizeof(via)) // comma if not first address
			}
			ax25_get_addr_with_ssid(pp, AX25_REPEATER_1+j, digiaddr)
			strlcat(via, digiaddr, sizeof(via))
			/*
			#if 0  // Mark each used with * as seen in UZ7HO SoundModem.
				    if (ax25_get_h(pp, AX25_REPEATER_1 + j)) {
			#else */
			// Mark only last used (i.e. the heard station) with * as in TNC-2 Monitoring format.
			if AX25_REPEATER_1+j == ax25_get_heard(pp) {
				// #endif
				strlcat(via, "*", sizeof(via))
			}

		}
		snprintf(result, result_size, " %d:Fm %s To %s Via %s ",
			channel+1, src, dst, via)
	} else {
		snprintf(result, result_size, " %d:Fm %s To %s ",
			channel+1, src, dst)
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

func mon_desc(pp C.packet_t) (byte, []byte) {
	// FIXME KG Return result too as []byte
	/* FIXME KG
	cmdres_t cr;		// command/response.
	char ignore[80];	// direwolf description.  not used here.
	int pf;			// poll/final bit.
	int ns;			// N(S) Send sequence number.
	int nr;			// N(R) Received sequence number.
	char pf_text[4];	// P or F depending on whether command or response.
	*/

	var ftype = C.ax25_frame_type(pp, &cr, ignore, &pf, &nr, &ns)

	switch cr {
	case cr_cmd:
		strcpy(pf_text, "P")
		break // P only: I, SABME, SABM, DISC
	case cr_res:
		strcpy(pf_text, "F")
		break // F only: DM, UA, FRMR
		// Either: RR, RNR, REJ, SREJ, UI, XID, TEST

	default:
		strcpy(pf_text, "PF")
		break // Not AX.25 version >= 2.0
		// APRS is often sloppy about this but it
		// is essential for connected mode.
	}

	var pinfo *C.uchar // I, UI, XID, SREJ, TEST can have information part.
	var info_len = ax25_get_info(pp, &pinfo)

	switch ftype {

	case frame_type_I:
		snprintf(result, result_size, "<I S%d R%d pid=%02X Len=%d %s=%d >", ns, nr, ax25_get_pid(pp), info_len, pf_text, pf)
		return ('I')

	case frame_type_U_UI:
		snprintf(result, result_size, "<UI pid=%02X Len=%d %s=%d >", ax25_get_pid(pp), info_len, pf_text, pf)
		return ('U')
		break

	case frame_type_S_RR:
		snprintf(result, result_size, "<RR R%d %s=%d >", nr, pf_text, pf)
		return ('S')
		break
	case frame_type_S_RNR:
		snprintf(result, result_size, "<RNR R%d %s=%d >", nr, pf_text, pf)
		return ('S')
		break
	case frame_type_S_REJ:
		snprintf(result, result_size, "<REJ R%d %s=%d >", nr, pf_text, pf)
		return ('S')
		break
	case frame_type_S_SREJ:
		snprintf(result, result_size, "<SREJ R%d %s=%d Len=%d >", nr, pf_text, pf, info_len)
		return ('S')
		break

	case frame_type_U_SABME:
		snprintf(result, result_size, "<SABME %s=%d >", pf_text, pf)
		return ('S')
		break
	case frame_type_U_SABM:
		snprintf(result, result_size, "<SABM %s=%d >", pf_text, pf)
		return ('S')
		break
	case frame_type_U_DISC:
		snprintf(result, result_size, "<DISC %s=%d >", pf_text, pf)
		return ('S')
		break
	case frame_type_U_DM:
		snprintf(result, result_size, "<DM %s=%d >", pf_text, pf)
		return ('S')
		break
	case frame_type_U_UA:
		snprintf(result, result_size, "<UA %s=%d >", pf_text, pf)
		return ('S')
		break
	case frame_type_U_FRMR:
		snprintf(result, result_size, "<FRMR %s=%d >", pf_text, pf)
		return ('S')
		break
	case frame_type_U_XID:
		snprintf(result, result_size, "<XID %s=%d Len=%d >", pf_text, pf, info_len)
		return ('S')
		break
	case frame_type_U_TEST:
		snprintf(result, result_size, "<TEST %s=%d Len=%d >", pf_text, pf, info_len)
		return ('S')
		break
	default:
	case frame_type_U:
		snprintf(result, result_size, "<U other??? >")
		return ('S')
		break
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

func server_link_established(channel C.int, client C.int, remote_call *C.char, own_call *C.char, incoming C.int) {

	/* FIXME KG
	struct {
	  struct agwpe_s Header;
	  char info[100];
	} reply;
	*/

	memset(&reply, 0, sizeof(reply))
	reply.Header.portx = channel
	reply.Header.DataKind = 'C'

	strlcpy(reply.Header.call_from, remote_call, sizeof(reply.Header.call_from))
	strlcpy(reply.Header.call_to, own_call, sizeof(reply.Header.call_to))

	// Question:  Should the via path be provided too?

	if incoming {
		// Other end initiated the connection.
		snprintf(reply.info, sizeof(reply.info), "*** CONNECTED To Station %s\r", remote_call)
	} else {
		// We started the connection.
		snprintf(reply.info, sizeof(reply.info), "*** CONNECTED With Station %s\r", remote_call)
	}
	reply.Header.data_len_NETLE = host2netle(strlen(reply.info) + 1)

	send_to_client(client, &reply)

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

func server_link_terminated(channel C.int, client C.int, remote_call *C.char, own_call *C.char, timeout C.int) {

	/* FIXME KG
	struct {
	  struct agwpe_s Header;
	  char info[100];
	} reply;
	*/

	memset(&reply, 0, sizeof(reply))
	reply.Header.portx = channel
	reply.Header.DataKind = 'd'
	strlcpy(reply.Header.call_from, remote_call, sizeof(reply.Header.call_from)) /* right order */
	strlcpy(reply.Header.call_to, own_call, sizeof(reply.Header.call_to))

	if timeout {
		snprintf(reply.info, sizeof(reply.info), "*** DISCONNECTED RETRYOUT With %s\r", remote_call)
	} else {
		snprintf(reply.info, sizeof(reply.info), "*** DISCONNECTED From Station %s\r", remote_call)
	}
	reply.Header.data_len_NETLE = host2netle(strlen(reply.info) + 1)

	send_to_client(client, &reply)

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

func server_rec_conn_data(channel *C.int, client *C.int, remote_call *C.char, own_call *C.char, pid C.int, data_ptr *C.char, data_len C.int) {

	/* FIXME KG
	struct {
	  struct agwpe_s Header;
	  char info[AX25_MAX_INFO_LEN];		// I suppose there is potential for something larger.
						// We'll cross that bridge if we ever come to it.
	} reply;
	*/

	memset(&reply.Header, 0, sizeof(reply.Header))
	reply.Header.portx = channel
	reply.Header.DataKind = 'D'
	reply.Header.pid = pid

	strlcpy(reply.Header.call_from, remote_call, sizeof(reply.Header.call_from))
	strlcpy(reply.Header.call_to, own_call, sizeof(reply.Header.call_to))

	if data_len < 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid length %d for connected data to client %d.\n", data_len, client)
		data_len = 0
	} else if data_len > AX25_MAX_INFO_LEN {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid length %d for connected data to client %d.\n", data_len, client)
		data_len = AX25_MAX_INFO_LEN
	}

	memcpy(reply.info, data_ptr, data_len)
	reply.Header.data_len_NETLE = host2netle(data_len)

	send_to_client(client, &reply)

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

func server_outstanding_frames_reply(channel C.int, client C.int, own_call *C.char, remote_call *C.char, count C.int) {

	/* FIXME KG
	struct {
	  struct agwpe_s Header;
	  int count_NETLE;
	} reply;
	*/

	memset(&reply.Header, 0, sizeof(reply.Header))

	reply.Header.portx = channel
	reply.Header.DataKind = 'Y'

	strlcpy(reply.Header.call_from, own_call, sizeof(reply.Header.call_from))
	strlcpy(reply.Header.call_to, remote_call, sizeof(reply.Header.call_to))

	reply.Header.data_len_NETLE = host2netle(4)
	reply.count_NETLE = host2netle(count)

	send_to_client(client, &reply)

} /* end server_outstanding_frames_reply */

/*-------------------------------------------------------------------
 *
 * Name:        read_from_socket
 *
 * Purpose:     Read from socket until we have desired number of bytes.
 *
 * Inputs:	fd		- file descriptor.
 *		ptr		- address where data should be placed.
 *		len		- desired number of bytes.
 *
 * Description:	Just a wrapper for the "read" system call but it should
 *		never return fewer than the desired number of bytes.
 *
 *--------------------------------------------------------------------*/

func read_from_socket(fd C.int, ptr *C.char, length C.int) C.int {
	var got_bytes = 0

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("read_from_socket (%d, %p, %d)\n", fd, ptr, len);
	#endif
	*/
	for got_bytes < len {
		var n = SOCK_RECV(fd, ptr+got_bytes, len-got_bytes)

		/* TODO KG
		#if DEBUG
			  text_color_set(DW_COLOR_DEBUG);
			  dw_printf ("read_from_socket: n = %d\n", n);
		#endif
		*/
		if n <= 0 {
			return (n)
		}

		got_bytes += n
	}
	Assert(got_bytes >= 0 && got_bytes <= len)

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("read_from_socket: return %d\n", got_bytes);
	#endif
	*/
	return (got_bytes)
}

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

func send_to_client(client C.int, reply_p unsafe.Pointer) {
	/* FIXME KG
	struct agwpe_s *ph;
	int len;
	int err;
	*/

	// FIXME KG var ph = (struct agwpe_s *) reply_p;	// Replies are often Header + other stuff.

	// FIXME KG len = sizeof(struct agwpe_s) + netle2host(ph.data_len_NETLE);

	/* Not sure what max data length might be. */

	if netle2host(ph.data_len_NETLE) < 0 || netle2host(ph.data_len_NETLE) > 4096 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid data length %d for AGW protocol message to client %d.\n", netle2host(ph.data_len_NETLE), client)
		debug_print(TO_CLIENT, client, ph, len)
	}

	if debug_client {
		debug_print(TO_CLIENT, client, ph, len)
	}

	SOCK_SEND(client_sock[client], (ph), len)
}

func cmd_listen_thread(client C.int) {

	Assert(client >= 0 && client < MAX_NET_CLIENTS)

	// FIXME KG cmd = AGWPE

	for {

		for client_sock[client] <= 0 {
			SLEEP_SEC(1) /* Not connected.  Try again later. */
		}

		var n = read_from_socket(client_sock[client], (&cmd.Header), sizeof(cmd.Header))
		if n != sizeof(cmd.Header) {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("\nError getting message header from AGW client application %d.\n", client)
			dw_printf("Tried to read %d bytes but got only %d.\n", sizeof(cmd.Header), n)
			dw_printf("Closing connection.\n\n")
			close(client_sock[client])
			client_sock[client] = -1
			dlq_client_cleanup(client)
			continue
		}

		/*
		 * Take some precautions to guard against bad data which could cause problems later.
		 */
		if cmd.Header.portx < 0 || cmd.Header.portx >= MAX_TOTAL_CHANS {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("\nInvalid port number, %d, in command '%c', from AGW client application %d.\n",
				cmd.Header.portx, cmd.Header.DataKind, client)
			cmd.Header.portx = 0 // avoid subscript out of bounds, try to keep going.
		}

		/*
		 * Call to/from fields are 10 bytes but contents must not exceed 9 characters.
		 * It's not guaranteed that unused bytes will contain 0 so we
		 * don't issue error message in this case.
		 */
		cmd.Header.call_from[sizeof(cmd.Header.call_from)-1] = 0
		cmd.Header.call_to[sizeof(cmd.Header.call_to)-1] = 0

		/*
		 * Following data must fit in available buffer.
		 * Leave room for an extra nul byte terminator at end later.
		 */

		var data_len = netle2host(cmd.Header.data_len_NETLE)

		if data_len < 0 || data_len > (sizeof(cmd.data)-1) {

			text_color_set(DW_COLOR_ERROR)
			dw_printf("\nInvalid message from AGW client application %d.\n", client)
			dw_printf("Data Length of %d is out of range.\n", data_len)

			/* This is a bad situation. */
			/* If we tried to read again, the header probably won't be there. */
			/* No point in trying to continue reading.  */

			dw_printf("Closing connection.\n\n")
			close(client_sock[client])
			client_sock[client] = -1
			dlq_client_cleanup(client)
			return (0)
		}

		cmd.data[0] = 0

		if data_len > 0 {
			var n = read_from_socket(client_sock[client], cmd.data, data_len)
			if n != data_len {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("\nError getting message data from AGW client application %d.\n", client)
				dw_printf("Tried to read %d bytes but got only %d.\n", data_len, n)
				dw_printf("Closing connection.\n\n")
				close(client_sock[client])
				client_sock[client] = -1
				dlq_client_cleanup(client)
				return (0)
			}
			if n >= 0 {
				cmd.data[n] = 0 // Tidy if we print for debug.
			}
		}

		/*
		 * print & process message from client.
		 */

		if debug_client {
			debug_print(FROM_CLIENT, client, &cmd.Header, sizeof(cmd.Header)+data_len)
		}

		switch cmd.Header.DataKind {

		case 'R': /* Request for version number */
			{
				/* FIXME KG
					struct {
					  struct agwpe_s Header;
				          int major_version_NETLE;
				          int minor_version_NETLE;
					} reply;
				*/

				memset(&reply, 0, sizeof(reply))
				reply.Header.DataKind = 'R'
				reply.Header.data_len_NETLE = host2netle(sizeof(reply.major_version_NETLE) + sizeof(reply.minor_version_NETLE))
				assert(netle2host(reply.Header.data_len_NETLE) == 8)

				// Xastir only prints this and doesn't care otherwise.
				// APRSIS32 doesn't seem to care.
				// UI-View32 wants on 2000.15 or later.

				reply.major_version_NETLE = host2netle(2005)
				reply.minor_version_NETLE = host2netle(127)

				assert(sizeof(reply) == 44)

				send_to_client(client, &reply)

			}
			break

		case 'G': /* Ask about radio ports */

			{
				/* FIXME KG
					struct {
					  struct agwpe_s Header;
				 	  char info[200];
					} reply;
				*/

				memset(&reply, 0, sizeof(reply))
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
				snprintf(reply.info, sizeof(reply.info), "%d;", count)

				for j = 0; j < MAX_TOTAL_CHANS; j++ {

					switch save_audio_config_p.chan_medium[j] {

					case MEDIUM_RADIO:
						{
							/* FIXME KG
							                // Misleading if using stdin or udp.
								        char stemp[100];
								        int a = ACHAN2ADEV(j);
								        // If I was really ambitious, some description could be provided.
								        static const char *names[8] = { "first", "second", "third", "fourth", "fifth", "sixth", "seventh", "eighth" };
							*/

							if save_audio_config_p.adev[a].num_channels == 1 {
								snprintf(stemp, sizeof(stemp), "Port%d %s soundcard mono;", j+1, names[a])
								strlcat(reply.info, stemp, sizeof(reply.info))
							} else {
								// FIXME KG snprintf (stemp, sizeof(stemp), "Port%d %s soundcard %s;", j+1, names[a], j&1 ? "right" : "left");
								strlcat(reply.info, stemp, sizeof(reply.info))
							}
						}
						break

					case MEDIUM_IGATE:
						{
							var stemp [100]C.char
							snprintf(stemp, sizeof(stemp), "Port%d Internet Gateway;", j+1)
							strlcat(reply.info, stemp, sizeof(reply.info))
						}
						break

					case MEDIUM_NETTNC:
						{
							// could elaborate with hostname, etc.
							var stemp [100]C.char
							snprintf(stemp, sizeof(stemp), "Port%d Network TNC;", j+1)
							strlcat(reply.info, stemp, sizeof(reply.info))
						}
						break

					default:
						// Only list valid channels.
						break

					} // switch
				} // for each channel

				reply.Header.data_len_NETLE = host2netle(strlen(reply.info) + 1)

				send_to_client(client, &reply)
			}
			break

		case 'g': /* Ask about capabilities of a port. */

			{
				/* FIXME KG
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

				memset(&reply, 0, sizeof(reply))

				reply.Header.portx = cmd.Header.portx /* Reply with same port number ! */
				reply.Header.DataKind = 'g'
				reply.Header.data_len_NETLE = host2netle(12)

				// YAAC asks for this.
				// Fake it to keep application happy.
				// TODO:  Supply real values instead of just faking it.

				reply.on_air_baud_rate = 0
				reply.traffic_level = 1
				reply.tx_delay = 0x19
				reply.tx_tail = 4
				reply.persist = 0xc8
				reply.slottime = 4
				reply.maxframe = 7
				reply.active_connections = 0
				reply.how_many_bytes_NETLE = host2netle(1)

				assert(sizeof(reply) == 48)

				send_to_client(client, &reply)

			}
			break

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

					        reply.Header.portx = cmd.Header.portx

					        strlcpy (reply.Header.call_from, "WB2OSZ-15 Mon,01Jan2000 01:02:03  Tue,31Dec2099 23:45:56", sizeof(reply.Header.call_from));
						// or                                                  00:00:00                00:00:00

					        strlcpy (agwpe_msg.data, ..., sizeof(agwpe_msg.data));

					        reply.Header.data_len_NETLE = host2netle(strlen(reply.info));

					        send_to_client (client, &reply);
				#endif
				*/
			}
			break

		case 'k': /* Ask to start receiving RAW AX25 frames */

			// Actually it is a toggle so we must be sure to clear it for a new connection.

			enable_send_raw_to_client[client] = !enable_send_raw_to_client[client]
			break

		case 'm': /* Ask to start receiving Monitor frames */

			// Actually it is a toggle so we must be sure to clear it for a new connection.

			enable_send_monitor_to_client[client] = !enable_send_monitor_to_client[client]
			break

		case 'V': /* Transmit UI data frame (with digipeater path) */
			{
				// Data format is:
				//	1 byte for number of digipeaters.
				//	10 bytes for each digipeater.
				//	data part of message.

				/* FIXME KG
				      	char stemp[AX25_MAX_PACKET_LEN+2];
					char *p;
					int ndigi;
					int k;


					packet_t pp;
				*/

				var pid = cmd.Header.pid
				strlcpy(stemp, cmd.Header.call_from, sizeof(stemp))
				strlcat(stemp, ">", sizeof(stemp))
				strlcat(stemp, cmd.Header.call_to, sizeof(stemp))

				cmd.data[data_len] = 0
				ndigi = cmd.data[0]
				p = cmd.data + 1

				for k := 0; k < ndigi; k++ {
					strlcat(stemp, ",", sizeof(stemp))
					strlcat(stemp, p, sizeof(stemp))
					p += 10
				}
				// At this point, p now points to info part after digipeaters.

				// Issue 527: NET/ROM routing broadcasts are binary info so we can't treat as string.
				// Originally, I just appended the information part.
				// That was fine until NET/ROM, with binary data, came along.
				// Now we set the information field after creating the packet object.

				strlcat(stemp, ":", sizeof(stemp))
				strlcat(stemp, " ", sizeof(stemp))

				//text_color_set(DW_COLOR_DEBUG);
				//dw_printf ("Transmit '%s'\n", stemp);

				var pp = ax25_from_text(stemp, 1)

				if pp == nil {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Failed to create frame from AGW 'V' message.\n")
					break
				}

				// Issue 550: Info part was one byte too long resulting in an extra nul character.
				// Original calculation was data_len-ndigi*10 but we need to subtract one
				// for first byte which is number of digipeaters.
				ax25_set_info(pp, p, data_len-ndigi*10-1)

				// Issue 527: NET/ROM routing broadcasts use PID 0xCF which was not preserved here.
				ax25_set_pid(pp, pid)

				/* This goes into the low priority queue because it is an original. */

				/* Note that the protocol has no way to set the "has been used" */
				/* bits in the digipeater fields. */

				/* This explains why the digipeating option is grayed out in */
				/* xastir when using the AGW interface.  */
				/* The current version uses only the 'V' message, not 'K' for transmitting. */

				tq_append(cmd.Header.portx, TQ_PRIO_1_LO, pp)
			}

			break

		case 'K': /* Transmit raw AX.25 frame */
			{
				// Message contains:
				//	port number for transmission.
				//	data length
				//	data which is raw ax.25 frame.
				//

				/* FIXME KG
				packet_t pp;
				alevel_t alevel;
				*/

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

				memset(&alevel, 0xff, sizeof(alevel))
				var pp = ax25_from_frame(cmd.data+1, data_len-1, alevel)

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
						ax25_get_h(pp, AX25_REPEATER_1) {
						tq_append(cmd.Header.portx, TQ_PRIO_0_HI, pp)
					} else {
						tq_append(cmd.Header.portx, TQ_PRIO_1_LO, pp)
					}
				}
			}

			break

		case 'P': /* Application Login  */

			// Silently ignore it.
			break

		case 'X': /* Register CallSign  */

			{
				/* FIXME KG
				struct {
				  struct agwpe_s Header;
				  char data;			// 1 = success, 0 = failure
				} reply;

				int ok = 1;
				*/

				// The protocol spec says it is an error to register the same one more than once.
				// Too much trouble.  Report success if the channel is valid.

				var channel = cmd.Header.portx

				// Connected mode can only be used with internal modems.

				if channel >= 0 && channel < MAX_RADIO_CHANS && save_audio_config_p.chan_medium[channel] == MEDIUM_RADIO {
					ok = 1
					dlq_register_callsign(cmd.Header.call_from, channel, client)
				} else {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("AGW protocol error.  Register callsign for invalid channel %d.\n", channel)
					ok = 0
				}

				memset(&reply, 0, sizeof(reply))
				reply.Header.DataKind = 'X'
				reply.Header.portx = cmd.Header.portx
				memcpy(reply.Header.call_from, cmd.Header.call_from, sizeof(reply.Header.call_from))
				reply.Header.data_len_NETLE = host2netle(1)
				reply.data = ok
				send_to_client(client, &reply)
			}
			break

		case 'x': /* Unregister CallSign  */

			{

				var channel = cmd.Header.portx

				// Connected mode can only be used with internal modems.

				if channel >= 0 && channel < MAX_RADIO_CHANS && save_audio_config_p.chan_medium[channel] == MEDIUM_RADIO {
					dlq_unregister_callsign(cmd.Header.call_from, channel, client)
				} else {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("AGW protocol error.  Unregister callsign for invalid channel %d.\n", channel)
				}
			}
			/* No response is expected. */
			break

		case 'C': /* Connect, Start an AX.25 Connection  */
		case 'v': /* Connect VIA, Start an AX.25 circuit thru digipeaters */
		case 'c': /* Connection with non-standard PID */

			{
				/* FIXME KG
				        struct via_info {
				          unsigned char num_digi;	// Expect to be in range 1 to 7.  Why not up to 8?
					  char dcall[7][10];
				        }
				*/

				// October 2017.  gcc ??? complained:
				//     warning: dereferencing pointer 'v' does break strict-aliasing rules
				// Try adding this attribute to get rid of the warning.
				// If this upsets your compiler, take it out.
				// Let me know.  Maybe we could put in a compiler version check here.

				/* FIXME KG
				   __attribute__((__may_alias__))
				                      *v = (struct via_info *)cmd.data;
				*/

				// FIXME KG char callsigns[AX25_MAX_ADDRS][AX25_MAX_ADDR_LEN];
				// FIXME KG int num_calls = 2;	/* 2 plus any digipeaters. */
				// FIXME KG int pid = 0xf0;		/* normal for AX.25 I frames. */
				// FIXME KG int j;

				strlcpy(callsigns[AX25_SOURCE], cmd.Header.call_from, sizeof(callsigns[AX25_SOURCE]))
				strlcpy(callsigns[AX25_DESTINATION], cmd.Header.call_to, sizeof(callsigns[AX25_DESTINATION]))

				if cmd.Header.DataKind == 'c' {
					pid = cmd.Header.pid /* non standard for NETROM, TCP/IP, etc. */
				}

				if cmd.Header.DataKind == 'v' {
					if v.num_digi >= 1 && v.num_digi <= 7 {

						if data_len != v.num_digi*10+1 && data_len != v.num_digi*10+2 {
							// I'm getting 1 more than expected from AGWterminal.
							text_color_set(DW_COLOR_ERROR)
							dw_printf("AGW client, connect via, has data len, %d when %d expected.\n", data_len, v.num_digi*10+1)
						}

						for j := 0; j < v.num_digi; j++ {
							strlcpy(callsigns[AX25_REPEATER_1+j], v.dcall[j], sizeof(callsigns[AX25_REPEATER_1+j]))
							num_calls++
						}
					} else {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("\n")
						dw_printf("AGW client, connect via, has invalid number of digipeaters = %d\n", v.num_digi)
					}
				}

				dlq_connect_request(callsigns, num_calls, cmd.Header.portx, client, pid)

			}
			break

		case 'D': /* Send Connected Data */

			{
				// FIXME KG char callsigns[AX25_MAX_ADDRS][AX25_MAX_ADDR_LEN];
				memset(callsigns, 0, sizeof(callsigns))
				const num_calls = 2 // only first 2 used.  Digipeater path
				// must be remembered from connect request.

				strlcpy(callsigns[AX25_SOURCE], cmd.Header.call_from, sizeof(callsigns[AX25_SOURCE]))
				strlcpy(callsigns[AX25_DESTINATION], cmd.Header.call_to, sizeof(callsigns[AX25_SOURCE]))

				dlq_xmit_data_request(callsigns, num_calls, cmd.Header.portx, client, cmd.Header.pid, cmd.data, netle2host(cmd.Header.data_len_NETLE))

			}
			break

		case 'd': /* Disconnect, Terminate an AX.25 Connection */

			{
				/* FIXME KG
				   char callsigns[AX25_MAX_ADDRS][AX25_MAX_ADDR_LEN];
				   memset (callsigns, 0, sizeof(callsigns));
				   const int num_calls = 2;	// only first 2 used.
				*/

				strlcpy(callsigns[AX25_SOURCE], cmd.Header.call_from, sizeof(callsigns[AX25_SOURCE]))
				strlcpy(callsigns[AX25_DESTINATION], cmd.Header.call_to, sizeof(callsigns[AX25_SOURCE]))

				dlq_disconnect_request(callsigns, num_calls, cmd.Header.portx, client)

			}
			break

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

				var pid = cmd.Header.pid
				var stemp [AX25_MAX_PACKET_LEN]C.char

				strlcpy(stemp, cmd.Header.call_from, sizeof(stemp))
				strlcat(stemp, ">", sizeof(stemp))
				strlcat(stemp, cmd.Header.call_to, sizeof(stemp))

				cmd.data[data_len] = 0

				// Issue 527: NET/ROM routing broadcasts are binary info so we can't treat as string.
				// Originally, I just appended the information part as a text string.
				// That was fine until NET/ROM, with binary data, came along.
				// Now we set the information field after creating the packet object.

				strlcat(stemp, ":", sizeof(stemp))
				strlcat(stemp, " ", sizeof(stemp))

				//text_color_set(DW_COLOR_DEBUG);
				//dw_printf ("Transmit '%s'\n", stemp);

				var pp = ax25_from_text(stemp, 1)

				if pp == nil {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Failed to create frame from AGW 'M' message.\n")
				}

				ax25_set_info(pp, cmd.data, data_len)
				// Issue 527: NET/ROM routing broadcasts use PID 0xCF which was not preserved here.
				ax25_set_pid(pp, pid)

				tq_append(cmd.Header.portx, TQ_PRIO_1_LO, pp)
			}
			break

		case 'y': /* Ask Outstanding frames waiting on a Port  */

			/* Number of frames sitting in transmit queue for specified channel. */
			{
				/* FIXME KG
				struct {
				  struct agwpe_s Header;
				  int data_NETLE;			// Little endian order.
				} reply;
				*/

				memset(&reply, 0, sizeof(reply))
				reply.Header.portx = cmd.Header.portx /* Reply with same port number */
				reply.Header.DataKind = 'y'
				reply.Header.data_len_NETLE = host2netle(4)

				var n = 0
				if cmd.Header.portx >= 0 && cmd.Header.portx < MAX_RADIO_CHANS {
					// Count both normal and expedited in transmit queue for given channel.
					n = tq_count(cmd.Header.portx, -1, "", "", 0)
				}
				reply.data_NETLE = host2netle(n)

				send_to_client(client, &reply)
			}
			break

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

				// FIXME KG char callsigns[AX25_MAX_ADDRS][AX25_MAX_ADDR_LEN];
				memset(callsigns, 0, sizeof(callsigns))
				const int num_calls = 2 // only first 2 used.

				strlcpy(callsigns[AX25_SOURCE], cmd.Header.call_from, sizeof(callsigns[AX25_SOURCE]))
				strlcpy(callsigns[AX25_DESTINATION], cmd.Header.call_to, sizeof(callsigns[AX25_SOURCE]))

				dlq_outstanding_frames_request(callsigns, num_calls, cmd.Header.portx, client)
			}
			break

		default:

			text_color_set(DW_COLOR_ERROR)
			dw_printf("--- Unexpected Command from application %d using AGW protocol:\n", client)
			debug_print(FROM_CLIENT, client, &cmd.Header, sizeof(cmd.Header)+data_len)

			break
		}
	}

} /* end send_to_client */

/* end server.c */
