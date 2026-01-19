package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Provide service to other applications via KISS protocol via TCP socket.
 *
 * Input:
 *
 * Outputs:
 *
 * Description:	This provides a TCP socket for communication with a client application.
 *
 *		It implements the KISS TNS protocol as described in:
 *		http://www.ka9q.net/papers/kiss.html
 *
 * 		Briefly, a frame is composed of
 *
 *			* FEND (0xC0)
 *			* Contents - with special escape sequences so a 0xc0
 *				byte in the data is not taken as end of frame.
 *				as part of the data.
 *			* FEND
 *
 *		The first byte of the frame contains:
 *
 *			* port number in upper nybble.
 *			* command in lower nybble.
 *
 *
 *		Commands from application recognized:
 *
 *			_0	Data Frame	AX.25 frame in raw format.
 *
 *			_1	TXDELAY		See explanation in xmit.c.
 *
 *			_2	Persistence	"	"
 *
 *			_3 	SlotTime	"	"
 *
 *			_4	TXtail		"	"
 *						Spec says it is obsolete but Xastir
 *						sends it and we respect it.
 *
 *			_5	FullDuplex	Ignored.
 *
 *			_6	SetHardware	TNC specific.
 *
 *			FF	Return		Exit KISS mode.  Ignored.
 *
 *
 *		Messages sent to client application:
 *
 *			_0	Data Frame	Received AX.25 frame in raw format.
 *
 *
 *
 *
 * References:	Getting Started with Winsock
 *		http://msdn.microsoft.com/en-us/library/windows/desktop/bb530742(v=vs.85).aspx
 *
 * Future:	Originally we had:
 *			KISS over serial port.
 *			AGW over socket.
 *		This is the two of them munged together and we end up with duplicate code.
 *		It would have been better to separate out the transport and application layers.
 *		Maybe someday.
 *
 *---------------------------------------------------------------*/

/*
	Separate TCP ports per radio:

An increasing number of people are using multiple radios.
direwolf is capable of handling many radio channels and
provides cross-band repeating, etc.
Maybe a single stereo audio interface is used for 2 radios.

                   +------------+    tcp 8001, all channels
Radio A  --------  |            |  -------------------------- Application A
                   |  direwolf  |
Radio B  --------  |            |  -------------------------- Application B
                   +------------+    tcp 8001, all channels

The KISS protocol has a 4 bit field for the TNC port (which I prefer to
call channel because port has too many different meanings).
direwolf handles this fine.  However, most applications were written assuming
that a TNC could only talk to a single radio.  On reception, they ignore the
channel in the KISS frame.  For transmit, the channel is always set to 0.

Many people are using the work-around of two separate instances of direwolf.

                   +------------+    tcp 8001, KISS ch 0
Radio A  --------  |  direwolf  |  -------------------------- Application A
                   +------------+

                   +------------+    tcp 8002, KISS ch 0
Radio B  --------  |  direwolf  |  -------------------------- Application B
                   +------------+


Or they might be using a single application that knows how to talk to multiple
single port TNCs.  But they don't know how to multiplex multiple channels
thru a single KISS stream.

                   +------------+    tcp 8001, KISS ch 0
Radio A  --------  |  direwolf  |  ------------------------
                   +------------+                          \
                                                            -- Application
                   +------------+    tcp 8002, KISS ch 0   /
Radio B  --------  |  direwolf  |  ------------------------
                   +------------+

Using two different instances of direwolf means more complex configuration
and loss of cross-channel digipeating.  It is possible to use a stereo
audio interface but some ALSA magic is required to make it look like two
independent virtual mono interfaces.

In version 1.7, we add the capability of multiple KISS TCP ports, each for
a single radio channel.  e.g.

KISSPORT 8001 1
KISSPORT 8002 2

Now can use a single instance of direwolf.


                   +------------+    tcp 8001, KISS ch 0
Radio A  --------  |            |  -------------------------- Application A
                   |  direwolf  |
Radio B  --------  |            |  -------------------------- Application B
                   +------------+    tcp 8002, KISS ch 0

When receiving, the KISS channel is set to 0.
 - only radio channel 1 would be sent over tcp port 8001.
 - only radio channel 2 would be sent over tcp port 8001.

When transmitting, the KISS channel is ignored.
 - frames from tcp port 8001 are transmitted on radio channel 1.
 - frames from tcp port 8002 are transmitted on radio channel 2.

Of course, you could also use an application, capable of connecting to
multiple single radio TNCs.  Separate TCP ports actually go to the
same direwolf instance.

*/

// #include "direwolf.h"
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
// #include <stddef.h>
// #include "ax25_pad.h"
// void hex_dump (unsigned char *p, int len);	// This should be in a .h file.
import "C"

import (
	"fmt"
	"net"
	"syscall"
	"unsafe"
)

var s_misc_config_p *misc_config_s

// Each TCP port has its own status block.
// There is a variable number so use a linked list.

var all_ports *kissport_status_s

var kiss_debug = 0 /* Print information flowing from and to client. */

func kiss_net_set_debug(n int) {
	kiss_debug = n
}

/*-------------------------------------------------------------------
 *
 * Name:        kissnet_init
 *
 * Purpose:     Set up a server to listen for connection requests from
 *		an application such as Xastir or APRSIS32.
 *		This is called once from the main program.
 *
 * Inputs:	mc.kiss_port	- TCP port for server.
 *				0 means disable.  New in version 1.2.
 *
 * Outputs:
 *
 * Description:	This starts two threads:
 *		  *  to listen for a connection from client app.
 *		  *  to listen for commands from client app.
 *		so the main application doesn't block while we wait for these.
 *
 *--------------------------------------------------------------------*/

func kissnet_init(mc *misc_config_s) {
	s_misc_config_p = mc

	for i := range MAX_KISS_TCP_PORTS {
		if mc.kiss_port[i] != 0 {
			var kps = new(kissport_status_s)

			kps.tcp_port = mc.kiss_port[i]
			kps.channel = mc.kiss_chan[i]

			kissnet_init_one(kps)

			// Add to list.
			kps.pnext = all_ports
			all_ports = kps
		}
	}
}

func kissnet_init_one(kps *kissport_status_s) {
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("kissnet_init ( tcp port %d, radio chan = %d )\n", kps.tcp_port, kps.chan);
	#endif
	*/

	for client := range MAX_NET_CLIENTS {
		kps.client_sock[client] = nil
		kps.kf[client] = new(kiss_frame_t)
	}

	if kps.tcp_port == 0 {
		text_color_set(DW_COLOR_INFO)
		dw_printf("Disabled KISS network client port.\n")
		return
	}

	/*
	 * This waits for a client to connect and sets client_sock[n].
	 */
	go connect_listen_thread(kps)

	/*
	 * These read messages from client when client_sock[n] is valid.
	 * Currently we start up a separate thread for each potential connection.
	 * Possible later refinement.  Start one now, others only as needed.
	 */
	for client := C.int(0); client < MAX_NET_CLIENTS; client++ {
		go kissnet_listen_thread(kps, client)
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        connect_listen_thread
 *
 * Purpose:     Wait for a connection request from an application.
 *
 * Inputs:	arg		- KISS port status block.
 *
 * Outputs:	client_sock	- File descriptor for communicating with client app.
 *
 * Description:	Wait for connection request from client and establish
 *		communication.
 *		Note that the client can go away and come back again and
 *		re-establish communication without restarting this application.
 *
 *--------------------------------------------------------------------*/

func connect_listen_thread(kps *kissport_status_s) {
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf("Binding to port %d ... \n", kps.tcp_port);
	#endif
	*/

	var listener, listenErr = net.Listen("tcp", fmt.Sprintf(":%d", kps.tcp_port))
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
	// TODO KG Test this
	// Set SO_REUSEADDR equivalent (handled automatically by Go's net package)
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
	 	dw_printf("opened KISS TCP socket as fd (%d) on port (%d) for stream i/o\n", listen_sock, ntohs(sockaddr.sin_port) );
	#endif
	*/

	for {
		var client = -1
		for c := 0; c < MAX_NET_CLIENTS && client < 0; c++ {
			if kps.client_sock[c] == nil {
				client = c
			}
		}

		if client >= 0 {
			text_color_set(DW_COLOR_INFO)
			if kps.channel == -1 {
				dw_printf("Ready to accept KISS TCP client application %d on port %d ...\n", client, kps.tcp_port)
			} else {
				dw_printf("Ready to accept KISS TCP client application %d on port %d (radio channel %d) ...\n", client, kps.tcp_port, kps.channel)
			}

			var conn, acceptErr = listener.Accept()
			if acceptErr != nil {
				dw_printf("Accept failed: %v\n", acceptErr)
				continue
			}

			kps.client_sock[client] = conn

			text_color_set(DW_COLOR_INFO)
			if kps.channel == -1 {
				dw_printf("\nAttached to KISS TCP client application %d on port %d ...\n\n", client, kps.tcp_port)
			} else {
				dw_printf("\nAttached to KISS TCP client application %d on port %d (radio channel %d) ...\n\n", client, kps.tcp_port, kps.channel)
			}

			// Reset the state and buffer.
			for i := range len(kps.kf) {
				kps.kf[i] = new(kiss_frame_t)
			}
		} else {
			SLEEP_SEC(1) /* wait then check again if more clients allowed. */
		}
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        kissnet_send_rec_packet
 *
 * Purpose:     Send a packet, received over the radio, to the client app.
 *
 * Inputs:	chan		- Channel number where packet was received.
 *				  0 = first, 1 = second if any.
 *
// TODO: add kiss_cmd
 *
 *		fbuf		- Address of raw received frame buffer
 *				  or a text string.
 *
 *		kiss_cmd	- Usually KISS_CMD_DATA_FRAME but we can also have
 *				  KISS_CMD_SET_HARDWARE when responding to a query.
 *
 *		flen		- Number of bytes for AX.25 frame.
 *				  When called from kiss_rec_byte, flen will be -1
 *				  indicating a text string rather than frame content.
 *				  This is used to fake out an application that thinks
 *				  it is using a traditional TNC and tries to put it
 *				  into KISS mode.
 *
 *		onlykps		- KISS TCP status block pointer or NULL.
 *
 *		onlyclient	- It is possible to have more than client attached
 *				  at the same time with TCP KISS.
 *				  Starting with version 1.7 we can have multiple TCP ports.
 *				  When a frame is received from the radio we normally want it
 *				  to go to all of the clients.
 *				  In this case specify NULL for onlykps and -1 tcp client.
 *				  When responding to a command from the client, we want
 *				  to send only to that one client app.  In this case
 *				  a non NULL kps and onlyclient >= 0.
 *
 * Description:	Send message to client(s) if connected.
 *		Disconnect from client, and notify user, if any error.
 *
 *--------------------------------------------------------------------*/

func kissnet_send_rec_packet(channel C.int, kiss_cmd C.int, fbuf []byte, flen C.int,
	onlykps *kissport_status_s, onlyclient C.int) {
	// Something received over the radio would normally be sent to all attached clients.
	// However, there are times we want to send a response only to a particular client.
	// In the case of a serial port or pseudo terminal, there is only one potential client.
	// so the response would be sent to only one place.  A new parameter has been added for this.

	for kps := all_ports; kps != nil; kps = kps.pnext {
		if onlykps == nil || kps == onlykps {
			for client := C.int(0); client < MAX_NET_CLIENTS; client++ {
				if onlyclient == -1 || client == onlyclient {
					if kps.client_sock[client] != nil {
						var kiss_buff []byte
						if flen < 0 {
							// A client app might think it is attached to a traditional TNC.
							// It might try sending commands over and over again trying to get the TNC into KISS mode.
							// We recognize this attempt and send it something to keep it happy.

							text_color_set(DW_COLOR_ERROR)
							dw_printf("KISS TCP: Something unexpected from client application.\n")
							dw_printf("Is client app treating this like an old TNC with command mode?\n")
							dw_printf("This can be caused by the application sending commands to put a\n")
							dw_printf("traditional TNC into KISS mode.  It is usually a harmless warning.\n")
							dw_printf("For best results, configure for a KISS-only TNC to avoid this.\n")
							dw_printf("In the case of APRSISCE/32, use \"Simply(KISS)\" rather than \"KISS.\"\n")

							flen = C.int(len(fbuf))
							if kiss_debug > 0 {
								kiss_debug_print(TO_CLIENT, "Fake command prompt", fbuf)
							}
							kiss_buff = fbuf
						} else {
							var stemp []byte

							// New in 1.7.
							// Previously all channels were sent to everyone.
							// We now have tcp ports which carry only a single radio channel.
							// The application will see KISS channel 0 regardless of the radio channel.

							if kps.channel == -1 { //nolint:staticcheck
								// Normal case, all channels.
								stemp = []byte{byte((channel << 4) | kiss_cmd)}
							} else if kps.channel == channel {
								// Single radio channel for this port.  Application sees 0.
								stemp = []byte{byte((0 << 4) | kiss_cmd)}
							} else {
								// Skip it.
								continue
							}

							stemp = append(stemp, fbuf...)

							if kiss_debug >= 2 {
								/* AX.25 frame with the CRC removed. */
								text_color_set(DW_COLOR_DEBUG)
								dw_printf("\n")
								dw_printf("Packet content before adding KISS framing and any escapes:\n")
								C.hex_dump((*C.uchar)(C.CBytes(fbuf)), flen)
							}

							kiss_buff = kiss_encapsulate(stemp)

							/* This has the escapes and the surrounding FENDs. */

							if kiss_debug > 0 {
								kiss_debug_print(TO_CLIENT, "", kiss_buff)
							}
						}

						var _, err = kps.client_sock[client].Write(kiss_buff)
						if err != nil {
							text_color_set(DW_COLOR_ERROR)
							dw_printf("\nError %s sending message to KISS client application %d on port %d.  Closing connection.\n\n", err, client, kps.tcp_port)
							kps.client_sock[client].Close()
							kps.client_sock[client] = nil
						}
					} // frame length >= 0
				} // if all clients or the one specifie
			} // for each client on the tcp port
		} // if all ports or the one specified
	} // for each tcp port
} /* end kissnet_send_rec_packet */

/*-------------------------------------------------------------------
 *
 * Name:        kissnet_copy
 *
 * Purpose:     Send data from one network KISS client to all others.
 *
 * Inputs:	in_msg		- KISS frame data without the framing or escapes.
 *				  The first byte is channel and command (should be data).
 *
 *		in_len 		- Number of bytes in above.
 *
 *		chan		- Channel.  Use this instead of first byte of in_msg.
 *
 *		cmd		- KISS command nybble.
 *				  Should be 0 because I'm expecting this only for data.
 *
 *		from_client	- Number of network (TCP) client instance.
 *				  Should be 0, 1, 2, ...
 *
 *
 * Global In:	kiss_copy	- From misc. configuration.
 *				  This enables the feature.
 *
 *
 * Description:	Send message to any attached network KISS clients, other than the one where it came from.
 *		Enable this by putting KISSCOPY in the configuration file.
 *		Note that this applies only to network (TCP) KISS clients, not serial port, or pseudo terminal.
 *
 *
 *--------------------------------------------------------------------*/

func kissnet_copy(in_msg *C.uchar, in_len C.int, channel C.int, cmd C.int, from_kps *kissport_status_s, from_client C.int) {
	var msg = C.GoBytes(unsafe.Pointer(in_msg), in_len)
	if s_misc_config_p.kiss_copy > 0 {
		for kps := all_ports; kps != nil; kps = kps.pnext {
			for client := C.int(0); client < MAX_NET_CLIENTS; client++ {
				// To all but origin.
				if !(kps == from_kps && client == from_client) { //nolint:staticcheck
					if kps.client_sock[client] != nil {
						if kps.channel == -1 || kps.channel == channel {
							// Two different cases here:
							//  - The TCP port allows all channels, or
							//  - The TCP port allows only one channel.  In this case set KISS channel to 0.

							if kps.channel == -1 {
								msg[0] = byte((channel << 4) | cmd)
							} else {
								msg[0] = byte(0 | cmd) // set channel to zero.
							}

							var kiss_buff = kiss_encapsulate(msg)

							/* This has the escapes and the surrounding FENDs. */

							if kiss_debug > 0 {
								kiss_debug_print(TO_CLIENT, "", kiss_buff)
							}

							var _, err = kps.client_sock[client].Write(kiss_buff)
							if err != nil {
								text_color_set(DW_COLOR_ERROR)
								dw_printf("\nError %s copying message to KISS TCP port %d client %d application.  Closing connection.\n\n", err, kps.tcp_port, client)
								kps.client_sock[client].Close()
								kps.client_sock[client] = nil
							}
						} // Channel is allowed on this port.
					} // socket is open
				} // if origin and destination different.
			} // loop over all KISS network clients for one port.
		} // loop over all KISS TCP ports
	} // Feature enabled.
} /* end kissnet_copy */

/*-------------------------------------------------------------------
 *
 * Name:        kissnet_listen_thread
 *
 * Purpose:     Wait for KISS messages from an application.
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

/* Return one byte (value 0 - 255) */

func kiss_get(kps *kissport_status_s, client int) byte {
	for {
		for kps.client_sock[client] == nil {
			SLEEP_SEC(1) /* Not connected.  Try again later. */
		}

		/* Just get one byte at a time. */

		var c = kps.client_sock[client]
		var ch = make([]byte, 1)
		var n, _ = c.Read(ch)

		if n == 1 {
			/* TODO KG
			#if DEBUG9
				    dw_printf (log_fp, "%02x %c %c", ch,
						isprint(ch) ? ch : '.' ,
						(isupper(ch>>1) || isdigit(ch>>1) || (ch>>1) == ' ') ? (ch>>1) : '.');
				    if (ch == FEND) fprintf (log_fp, "  FEND");
				    if (ch == FESC) fprintf (log_fp, "  FESC");
				    if (ch == TFEND) fprintf (log_fp, "  TFEND");
				    if (ch == TFESC) fprintf (log_fp, "  TFESC");
				    if (ch == '\r') fprintf (log_fp, "  CR");
				    if (ch == '\n') fprintf (log_fp, "  LF");
				    fprintf (log_fp, "\n");
				    if (ch == FEND) fflush (log_fp);
			#endif
			*/
			return (ch[0])
		}

		text_color_set(DW_COLOR_ERROR)
		dw_printf("\nKISS client application %d on TCP port %d has gone away.\n\n", client, kps.tcp_port)
		c.Close()
		kps.client_sock[client] = nil
	}
}

func kissnet_listen_thread(kps *kissport_status_s, client C.int) {
	Assert(client >= 0 && client < MAX_NET_CLIENTS)

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("kissnet_listen_thread ( tcp_port = %d, client = %d, socket fd = %d )\n", kps.tcp_port, client, kps.client_sock[client]);
	#endif
	*/

	// So why is kissnet_send_rec_packet mentioned here for incoming from the client app?
	// The logic exists for the serial port case where the client might think it is
	// attached to a traditional TNC.  It might try sending commands over and over again
	// trying to get the TNC into KISS mode.  To keep it happy, we recognize this attempt
	// and send it something to keep it happy.
	// In the case of a serial port or pseudo terminal, there is only one potential client
	// so the response would be sent to only one place.
	// Starting in version 1.5, this now can have multiple attached clients.  We wouldn't
	// want to send the response to all of them.   Actually, we should be providing only
	// "Simply KISS" as some call it.

	for {
		var ch = kiss_get(kps, int(client))
		kiss_rec_byte(kps.kf[client], C.uchar(ch), C.int(kiss_debug), kps, client, kissnet_send_rec_packet)
	}
} /* end kissnet_listen_thread */

/* end kissnet.c */
