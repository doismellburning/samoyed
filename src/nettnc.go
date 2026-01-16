package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Attach to Network KISS TNC(s) for NCHANNEL config file item(s).
 *
 * Description:	Called once at application start up.
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
// #include "audio.h"		// configuration.
// #include "kiss_frame.h"
// #include "ax25_pad.h"		// for AX25_MAX_PACKET_LEN
// #include "dlq.h"		// received packet queue
// void hex_dump (unsigned char *p, int len);
import "C"

import (
	"net"
	"os"
	"strconv"
	"unsafe"
)

var s_kiss_debug = 0

/*-------------------------------------------------------------------
 *
 * Name:        nettnc_init
 *
 * Purpose:      Attach to Network KISS TNC(s) for NCHANNEL config file item(s).
 *
 * Inputs:	pa              - Address of structure of type audio_s.
 *
 *		debug ? TBD
 *
 *
 * Returns:	0 for success, -1 for failure.
 *
 * Description:	Called once at direwolf application start up time.
 *		Calls nettnc_attach for each NCHANNEL configuration item.
 *
 *--------------------------------------------------------------------*/

func nettnc_init(pa *C.struct_audio_s) {

	for i := C.int(0); i < MAX_TOTAL_CHANS; i++ {

		if pa.chan_medium[i] == MEDIUM_NETTNC {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Channel %d: Network TNC %s %d\n", i, C.GoString(&pa.nettnc_addr[i][0]), pa.nettnc_port[i])
			var e = nettnc_attach(i, C.GoString(&pa.nettnc_addr[i][0]), int(pa.nettnc_port[i]))
			if e < 0 {
				os.Exit(1)
			}
		}
	}

} // end nettnc_init

/*-------------------------------------------------------------------
 *
 * Name:        nettnc_attach
 *
 * Purpose:      Attach to one Network KISS TNC.
 *
 * Inputs:	channel	- channel number from NCHANNEL configuration.
 *
 *		host	- Host name or IP address.  Often "localhost".
 *
 *		port	- TCP port number.  Typically 8001.
 *
 *		init_func - Call this function after establishing communication //
 *			with the TNC.  We put it here, so that it can be done//
 *			again automatically if the TNC disappears and we//
 *			reattach to it.//
 *			It must return 0 for success.//
 *			Can be nil if not needed.//
 *
 * Returns:	0 for success, -1 for failure.
 *
 * Description:	This starts up a thread, for each socket, which listens to the socket and
 *		dispatches the messages to the corresponding callback functions.
 *		It will also attempt to re-establish communication with the
 *		TNC if it goes away.
 *
 *--------------------------------------------------------------------*/

var s_tnc_host [MAX_TOTAL_CHANS]string
var s_tnc_port [MAX_TOTAL_CHANS]int
var s_tnc_sock [MAX_TOTAL_CHANS]net.Conn // Socket handle or file descriptor. -1 for invalid.

func nettnc_attach(channel C.int, host string, port int) int {

	Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	s_tnc_host[channel] = host
	s_tnc_port[channel] = port
	s_tnc_sock[channel] = nil

	var conn, connErr = net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))

	if connErr == nil {
		s_tnc_sock[channel] = conn
	} else {
		return -1
	}

	/*
	 * Read frames from the network TNC.
	 * If the TNC disappears, try to reestablish communication.
	 */

	go nettnc_listen_thread(channel)

	// TNC initialization if specified.

	//	if (s_tnc_init_func != nil) {
	//	  e = (*s_tnc_init_func)();
	//	  return (e);
	//	}

	return (0)

} // end nettnc_attach

/*-------------------------------------------------------------------
 *
 * Name:        nettnc_listen_thread
 *
 * Purpose:     Listen for anything from TNC and process it.
 *		Reconnect if something goes wrong and we got disconnected.
 *
 * Inputs:	arg			- Channel number.
 *		s_tnc_host[channel]	- Host & port for re-connection.
 *		s_tnc_port[channel]
 *
 * Outputs:	s_tnc_sock[channel] - File descriptor for communicating with TNC.
 *				  Will be -1 if not connected.
 *
 *--------------------------------------------------------------------*/

func nettnc_listen_thread(channel C.int) {

	Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	var kstate C.kiss_frame_t // State machine to gather a KISS frame.

	for {
		/*
		 * Re-attach to TNC if not currently attached.
		 */
		if s_tnc_sock[channel] == nil {

			text_color_set(DW_COLOR_ERROR)
			// I'm using the term "attach" here, in an attempt to
			// avoid confusion with the AX.25 connect.
			dw_printf("Attempting to reattach to network TNC...\n")

			var conn, connErr = net.Dial("tcp", net.JoinHostPort(s_tnc_host[channel], strconv.Itoa(s_tnc_port[channel])))

			if connErr == nil {
				s_tnc_sock[channel] = conn
				dw_printf("Successfully reattached to network TNC.\n")
			}
		} else {
			const NETTNCBUFSIZ = 2048
			var buf = make([]byte, NETTNCBUFSIZ)
			var n, readErr = s_tnc_sock[channel].Read(buf)

			if readErr != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Lost communication with network TNC. Will try to reattach.\n")
				s_tnc_sock[channel].Close()
				s_tnc_sock[channel] = nil
				SLEEP_SEC(5)
				continue
			}

			for j := 0; j < n; j++ {
				// Separate the byte stream into KISS frame(s) and make it
				// look like this came from a radio channel.
				my_kiss_rec_byte(&kstate, C.uchar(buf[j]), s_kiss_debug, channel)
			}
		} // s_tnc_sock != -1
	} // while (1)
} // end nettnc_listen_thread

/*-------------------------------------------------------------------
 *
 * Name:        my_kiss_rec_byte
 *
 * Purpose:     Process one byte from a KISS network TNC.
 *
 * Inputs:	kf	- Current state of building a frame.
 *		b	- A byte from the input stream.
 *		debug	- Activates debug output.
 *		channel_overide - Set incoming channel number to the NCHANNEL
 *				number rather than the channel in the KISS frame.
 *
 * Outputs:	kf	- Current state is updated.
 *
 * Returns:	none.
 *
 * Description:	This is a simplified version of kiss_rec_byte used
 *		for talking to KISS client applications.  It already has
 *		too many special cases and I don't want to make it worse.
 *		This also needs to make the packet look like it came from
 *		a radio channel, not from a client app.
 *
 *-----------------------------------------------------------------*/

func my_kiss_rec_byte(kf *C.kiss_frame_t, b C.uchar, debug int, channel_override C.int) {

	//dw_printf ("my_kiss_rec_byte ( %c %02x ) \n", b, b);

	switch kf.state {

	/* Searching for starting FEND. */
	default: // Includes KS_SEARCHING

		if b == C.FEND {

			/* Start of frame.  */

			kf.kiss_len = 0
			kf.kiss_msg[kf.kiss_len] = b
			kf.kiss_len++
			kf.state = KS_COLLECTING
			return
		}
		return

	case KS_COLLECTING: /* Frame collection in progress. */

		if b == C.FEND {

			/* End of frame. */

			if kf.kiss_len == 0 {
				/* Empty frame.  Starting a new one. */
				kf.kiss_msg[kf.kiss_len] = b
				kf.kiss_len++
				return
			}
			if kf.kiss_len == 1 && kf.kiss_msg[0] == C.FEND {
				/* Empty frame.  Just go on collecting. */
				return
			}

			kf.kiss_msg[kf.kiss_len] = b
			kf.kiss_len++
			if debug > 0 {
				/* As received over the wire from network TNC. */
				// May include escapted characters.  What about FEND?
				// FIXME: make it say Network TNC.
				kiss_debug_print(FROM_CLIENT, "", C.GoBytes(unsafe.Pointer(&kf.kiss_msg[0]), kf.kiss_len))
			}

			var unwrapped = kiss_unwrap(C.GoBytes(unsafe.Pointer(&kf.kiss_msg[0]), kf.kiss_len))

			if debug >= 2 {
				/* Append CRC to this and it goes out over the radio. */
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("\n")
				dw_printf("Frame content after removing KISS framing and any escapes:\n")
				/* Don't include the "type" indicator. */
				/* It contains the radio channel and type should always be 0 here. */
				C.hex_dump((*C.uchar)(C.CBytes(unwrapped[1:])), C.int(len(unwrapped[1:])))
			}

			// Convert to packet object and send to received packet queue.
			// Note that we use channel associated with the network TNC, not channel in KISS frame.

			var subchan C.int = -3
			var slice C.int = 0
			var alevel C.alevel_t
			var pp = ax25_from_frame((*C.uchar)(C.CBytes(unwrapped[1:])), C.int(len(unwrapped[1:])), alevel)

			if pp != nil {
				var fec_type C.fec_type_t = C.fec_type_none
				var retries C.retry_t

				var spectrum = C.CString("Network TNC")
				C.dlq_rec_frame(channel_override, subchan, slice, pp, alevel, fec_type, retries, spectrum)
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Failed to create packet object for KISS frame from channel %d network TNC.\n", channel_override)
			}

			kf.state = KS_SEARCHING
			return
		}

		if kf.kiss_len < C.MAX_KISS_LEN {
			kf.kiss_msg[kf.kiss_len] = b
			kf.kiss_len++
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("KISS frame from network TNC exceeded maximum length.\n")
		}
		return
	}
} /* end my_kiss_rec_byte */

/*-------------------------------------------------------------------
 *
 * Name:	nettnc_send_packet
 *
 * Purpose:	Send packet to a KISS network TNC.
 *
 * Inputs:	channel	- Channel number from NCHANNEL configuration.
 *		pp	- Packet object.
 *		b	- A byte from the input stream.
 *
 * Outputs:	Packet is converted to KISS and send to network TNC.
 *
 * Returns:	none.
 *
 * Description:	This does not free the packet object; caller is responsible.
 *
 *-----------------------------------------------------------------*/

func nettnc_send_packet(channel C.int, pp C.packet_t) {

	// First, get the on-air frame format from packet object.
	// Prepend 0 byte for KISS command and channel.

	var fbuf = ax25_get_frame_data_ptr(pp)
	var flen = ax25_get_frame_len(pp)

	var frame_buff = []byte{0} // For now, set channel to 0.
	frame_buff = append(frame_buff, C.GoBytes(unsafe.Pointer(fbuf), flen)...)

	// Next, encapsulate into KISS frame with surrounding FENDs and any escapes.

	var kiss_buff = kiss_encapsulate(frame_buff)

	var _, err = s_tnc_sock[channel].Write(kiss_buff)
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("\nError %d sending packet to KISS Network TNC for channel %d.  Closing connection.\n\n", err, channel)
		s_tnc_sock[channel].Close()
		s_tnc_sock[channel] = nil
	}

	// Do not free packet object;  caller will take care of it.

} /* end nettnc_send_packet */
