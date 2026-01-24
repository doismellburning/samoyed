package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	IGate client.
 *
 * Description:	Establish connection with a tier 2 IGate server
 *		and relay packets between RF and Internet.
 *
 * References:	APRS-IS (Automatic Packet Reporting System-Internet Service)
 *		http://www.aprs-is.net/Default.aspx
 *
 *		APRS iGate properties
 *		http://wiki.ham.fi/APRS_iGate_properties
 *		(now gone but you can find a copy here:)
 *		https://web.archive.org/web/20120503201832/http://wiki.ham.fi/APRS_iGate_properties
 *
 *		Notes to iGate developers
 *		https://github.com/hessu/aprsc/blob/master/doc/IGATE-HINTS.md#igates-dropping-duplicate-packets-unnecessarily
 *
 *		SATgate mode.
 *		http://www.tapr.org/pipermail/aprssig/2016-January/045283.html
 *
 *---------------------------------------------------------------*/

// #include <stdlib.h>
// #include <netdb.h>
// #include <sys/types.h>
// #include <sys/ioctl.h>
// #include <sys/socket.h>
// #include <arpa/inet.h>
// #include <netinet/in.h>
// #include <netinet/tcp.h>
// #include <unistd.h>
// #include <stdio.h>
// #include <assert.h>
// #include <string.h>
// #include <time.h>
import "C"

import (
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"
)

const DEFAULT_IGATE_PORT = 14580

type igate_config_s struct {

	/*
	 * For logging into the IGate server.
	 */
	t2_server_name [40]C.char /* Tier 2 IGate server name. */

	t2_server_port C.int /* Typically 14580. */

	t2_login [AX25_MAX_ADDR_LEN]C.char /* e.g. WA9XYZ-15 */
	/* Note that the ssid could be any two alphanumeric */
	/* characters not just 1 thru 15. */
	/* Could be same or different than the radio call(s). */
	/* Not sure what the consequences would be. */

	t2_passcode [8]C.char /* Max. 5 digits. Could be "-1". */

	t2_filter *C.char /* Optional filter for IS -> RF direction. */
	/* This is the "server side" filter. */
	/* A better name would be subscription or something */
	/* like that because we can only ask for more. */

	/*
	 * For transmitting.
	 */
	tx_chan C.int /* Radio channel for transmitting. */
	/* 0=first, etc.  -1 for none. */
	/* Presently IGate can transmit on only a single channel. */
	/* A future version might generalize this.  */
	/* Each transmit channel would have its own client side filtering. */

	tx_via [80]C.char /* VIA path for transmitting third party packets. */
	/* Usual text representation.  */
	/* Must start with "," if not empty so it can */
	/* simply be inserted after the destination address. */

	max_digi_hops C.int /* Maximum number of digipeater hops possible for via path. */
	/* Derived from the SSID when last character of address is a digit. */
	/* e.g.  "WIDE1-1,WIDE5-2" would be 3. */
	/* This is useful to know so we can determine how many */
	/* stations we might be able to reach. */

	tx_limit_1 C.int /* Max. packets to transmit in 1 minute. */

	tx_limit_5 C.int /* Max. packets to transmit in 5 minutes. */

	igmsp C.int /* Number of message sender position reports to allow. */
	/* Common practice is to default to 1.  */
	/* We allow additional flexibility of 0 to disable feature */
	/* or a small number to allow more. */

	/*
	 * Receiver to IS data options.
	 */
	rx2ig_dedupe_time C.int /* seconds.  0 to disable. */

	/*
	 * Special SATgate mode to delay packets heard directly.
	 */
	satgate_delay C.int /* seconds.  0 to disable. */
}

const IGATE_TX_LIMIT_1_DEFAULT = 6
const IGATE_TX_LIMIT_1_MAX = 20

const IGATE_TX_LIMIT_5_DEFAULT = 20
const IGATE_TX_LIMIT_5_MAX = 80

const IGATE_RX2IG_DEDUPE_TIME = 0 /* Issue 85.  0 means disable dupe checking in RF>IS direction. */
/* See comments in rx_to_ig_remember & rx_to_ig_allow. */
/* Currently there is no configuration setting to change this. */

const DEFAULT_SATGATE_DELAY = 10
const MIN_SATGATE_DELAY = 5
const MAX_SATGATE_DELAY = 30

var dp_mutex sync.Mutex /* Critical section for delayed packet queue. */
var dp_queue_head *packet_t

var igate_sock net.Conn

/*
 * After connecting to server, we want to make sure
 * that the login sequence is sent first.
 * This is set to true after the login is complete.
 */

var ok_to_send = false

/*
 * Global stuff (to this file)
 *
 * These are set by init function and need to
 * be kept around in case connection is lost and
 * we need to reestablish the connection later.
 */

// TODO KG static struct audio_s		*save_audio_config_p;
var save_igate_config_p *igate_config_s

// TODO KG static struct digi_config_s 	*save_digi_config_p;
var s_debug C.int

/*
 * Statistics for IGate function.
 * Note that the RF related counters are just a subset of what is happening on radio channels.
 *
 * TODO: should have debug option to print these occasionally.
 */

var stats_failed_connect C.int /* Number of times we tried to connect to */
/* a server and failed.  A small number is not */
/* a bad thing.  Each name should have a bunch */
/* of addresses for load balancing and */
/* redundancy. */

var stats_connects C.int /* Number of successful connects to a server. */
/* Normally you'd expect this to be 1.  */
/* Could be larger if one disappears and we */
/* try again to find a different one. */

var stats_connect_at time.Time /* Most recent time connection was established. */
/* can be used to determine elapsed connect time. */

var stats_rf_recv_packets C.int /* Number of candidate packets from the radio. */
/* This is not the total number of AX.25 frames received */
/* over the radio; only APRS packets get this far. */

var stats_uplink_packets C.int /* Number of packets passed along to the IGate */
/* server after filtering. */

var stats_uplink_bytes int /* Total number of bytes sent to IGate server */
/* including login, packets, and heartbeats. */

var stats_downlink_bytes C.int /* Total number of bytes from IGate server including */
/* packets, heartbeats, other messages. */

var stats_downlink_packets C.int /* Number of packets from IGate server for possible transmission. */
/* Fewer might be transmitted due to filtering or rate limiting. */

var stats_rf_xmit_packets C.int /* Number of packets passed along to radio, for the IGate function, */
/* after filtering, rate limiting, or other restrictions. */
/* Number of packets transmitted for beacons, digipeating, */
/* or client applications are not included here. */

var stats_msg_cnt C.int /* Number of "messages" transmitted.  Subset of above. */
/* A "message" has the data type indicator of ":" and it is */
/* not the special case of telemetry metadata. */

/*
 * Make some of these available for IGate statistics beacon like
 *
 *	WB2OSZ>APDW14,WIDE1-1:<IGATE,MSG_CNT=2,PKT_CNT=0,DIR_CNT=10,LOC_CNT=35,RF_CNT=45
 *
 * MSG_CNT is only "messages."   From original spec.
 * PKT_CNT is other (non-message) packets.  Followed precedent of APRSISCE32.
 */

func igate_get_msg_cnt() C.int {
	return (stats_msg_cnt)
}

func igate_get_pkt_cnt() C.int {
	return (stats_rf_xmit_packets - stats_msg_cnt)
}

func igate_get_upl_cnt() C.int {
	return (stats_uplink_packets)
}

func igate_get_dnl_cnt() C.int {
	return (stats_downlink_packets)
}

/*-------------------------------------------------------------------
 *
 * Name:        igate_init
 *
 * Purpose:     One time initialization when main application starts up.
 *
 * Inputs:	p_audio_config	- Audio channel configuration.  All we care about is:
 *				  - Number of radio channels.
 *				  - Radio call and SSID for each channel.
 *
 *		p_igate_config	- IGate configuration.
 *
 *		p_digi_config	- Digipeater configuration.
 *				  All we care about here is the packet filtering options.
 *
 *		debug_level	- 0  print packets FROM APRS-IS,
 *				     establishing connection with sergver, and
 *				     and anything rejected by client side filtering.
 *				  1  plus packets sent TO server or why not.
 *				  2  plus duplicate detection overview.
 *				  3  plus duplicate detection details.
 *
 * Description:	This starts two threads:
 *
 *		  *  to establish and maintain a connection to the server.
 *		  *  to listen for packets from the server.
 *
 *--------------------------------------------------------------------*/

func igate_init(p_audio_config *audio_s, p_igate_config *igate_config_s, p_digi_config *digi_config_s, debug_level C.int) {

	s_debug = debug_level
	dp_queue_head = nil

	/* TODO KG
	#if DEBUGx
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("igate_init ( %s, %d, %s, %s, %s )\n",
					p_igate_config.t2_server_name,
					p_igate_config.t2_server_port,
					p_igate_config.t2_login,
					p_igate_config.t2_passcode,
					p_igate_config.t2_filter);
	#endif
	*/

	/*
	 * Save the arguments for later use.
	 */
	save_audio_config_p = p_audio_config
	save_igate_config_p = p_igate_config
	save_digi_config_p = p_digi_config

	stats_failed_connect = 0
	stats_connects = 0
	stats_connect_at = time.Time{}
	stats_rf_recv_packets = 0
	stats_uplink_packets = 0
	stats_uplink_bytes = 0
	stats_downlink_bytes = 0
	stats_downlink_packets = 0
	stats_rf_xmit_packets = 0
	stats_msg_cnt = 0

	rx_to_ig_init()
	ig_to_tx_init()

	/*
	 * Continue only if we have server name, login, and passcode.
	 */
	if C.strlen(&p_igate_config.t2_server_name[0]) == 0 ||
		C.strlen(&p_igate_config.t2_login[0]) == 0 ||
		C.strlen(&p_igate_config.t2_passcode[0]) == 0 {
		return
	}

	/*
	 * This connects to the server and sets igate_sock.
	 * It also sends periodic messages to say I'm still alive.
	 */

	go connect_thread()

	/*
	 * This reads messages from client when igate_sock is valid.
	 */

	go igate_recv_thread()

	/*
	 * This lets delayed packets continue after specified amount of time.
	 */

	if p_igate_config.satgate_delay > 0 {
		go satgate_delay_thread()
	}

} /* end igate_init */

/*-------------------------------------------------------------------
 *
 * Name:        connnect_thread
 *
 * Purpose:     Establish connection with IGate server.
 *		Send periodic heartbeat to keep keep connection active.
 *		Reconnect if something goes wrong and we got disconnected.
 *
 * Outputs:	igate_sock	- File descriptor for communicating with client app.
 *				  Will be -1 if not connected.
 *
 * References:	TCP client example.
 *		http://msdn.microsoft.com/en-us/library/windows/desktop/ms737591(v=vs.85).aspx
 *
 *		Linux IPv6 HOWTO
 *		http://www.tldp.org/HOWTO/Linux+IPv6-HOWTO/
 *
 *--------------------------------------------------------------------*/

const MAX_HOSTS = 50

func connect_thread() {

	/* TODO KG
	#if DEBUGx
		text_color_set(DW_COLOR_DEBUG);
	        dw_printf ("DEBUG: igate connect_thread start, port = %d = '%s'\n", save_igate_config_p.t2_server_port, server_port_str);
	#endif
	*/

	var server_name = C.GoString(&save_igate_config_p.t2_server_name[0])

	/*
	 * Repeat forever.
	 */

	for {

		/*
		 * Connect to IGate server if not currently connected.
		 */

		if igate_sock == nil {
			var conn, connErr = net.Dial("tcp", net.JoinHostPort(server_name, strconv.Itoa(int(save_igate_config_p.t2_server_port))))
			stats_connects++
			stats_connect_at = time.Now()

			if connErr != nil {
				text_color_set(DW_COLOR_INFO)
				dw_printf("Connect to IGate server %s failed.\n\n", server_name)
				stats_failed_connect++
			} else {

				/* Success. */

				text_color_set(DW_COLOR_INFO)
				dw_printf("\nNow connected to IGate server %s\n", server_name)
				if strings.Contains(server_name, ":") {
					dw_printf("Check server status here http://[%s]:14501\n\n", server_name)
				} else {
					dw_printf("Check server status here http://%s:14501\n\n", server_name)
				}

				/*
				 * Set igate_sock so everyone else can start using it.
				 * But make the Rx -> Internet messages wait until after login.
				 */

				ok_to_send = false
				igate_sock = conn

				/*
				 * Send login message.
				 * Software name and version must not contain spaces.
				 */

				SLEEP_SEC(3)
				var stemp = fmt.Sprintf("user %s pass %s vers Dire-Wolf %d.%d",
					C.GoString(&save_igate_config_p.t2_login[0]), C.GoString(&save_igate_config_p.t2_passcode[0]),
					C.MAJOR_VERSION, C.MINOR_VERSION)
				if save_igate_config_p.t2_filter != nil {
					stemp += " filter "
					stemp += C.GoString(save_igate_config_p.t2_filter)
				}
				send_msg_to_server(stemp)

				/* Delay until it is ok to start sending packets. */

				SLEEP_SEC(7)
				ok_to_send = true
			}
		}

		/*
		 * If connected to IGate server, send heartbeat periodically to keep connection active.
		 */
		if igate_sock != nil {
			SLEEP_SEC(10)
		}
		if igate_sock != nil {
			SLEEP_SEC(10)
		}
		if igate_sock != nil {
			SLEEP_SEC(10)
		}

		if igate_sock != nil {
			/* This will close the socket if any error. */
			send_msg_to_server("#")
		}
	}
} /* end connnect_thread */

/*-------------------------------------------------------------------
 *
 * Name:        igate_send_rec_packet
 *
 * Purpose:     Send a packet to the IGate server
 *
 * Inputs:	channel	- Radio channel it was received on.
 *			  This is required for the RF>IS filtering.
 *		          Beaconing (sendto=ig, chan=-1) and a client app sending
 *			  to ICHANNEL should bypass the filtering.
 *
 *		recv_pp	- Pointer to packet object.
 *			  *** CALLER IS RESPONSIBLE FOR DELETING IT! **
 *
 *
 * Description:	Send message to IGate Server if connected.
 *
 * Assumptions:	(1) Caller has already verified it is an APRS packet.
 *		i.e. control = 3 for UI frame, protocol id = 0xf0 for no layer 3
 *
 *		(2) This is being called only for packets received with
 *		a correct CRC.  We don't want to propagate corrupted data.
 *
 *--------------------------------------------------------------------*/

const IGATE_MAX_MSG = 512 /* "All 'packets' sent to APRS-IS must be in the TNC2 format terminated */
/* by a carriage return, line feed sequence. No line may exceed 512 bytes */
/* including the CR/LF sequence." */

func igate_send_rec_packet(channel C.int, recv_pp *packet_t) {

	if igate_sock == nil {
		return /* Silently discard if not connected. */
	}

	if !ok_to_send {
		return /* Login not complete. */
	}

	/* Gather statistics. */

	stats_rf_recv_packets++

	/*
	 * Check for filtering from specified channel to the IGate server.
	 *
	 * Should we do this after unwrapping the payload from a third party packet?
	 * In my experience, third party packets have only been seen coming from IGates.
	 * In that case, the payload will have TCPIP in the path and it will be dropped.
	 */

	// Apply RF>IS filtering only if it same from a radio channel.
	// Beacon will be channel -1.
	// Client app to ICHANNEL is outside of radio channel range.

	if channel >= 0 && channel < MAX_TOTAL_CHANS && // in radio channel range
		save_digi_config_p.filter_str[channel][MAX_TOTAL_CHANS] != "" {

		if pfilter(channel, MAX_TOTAL_CHANS, C.CString(save_digi_config_p.filter_str[channel][MAX_TOTAL_CHANS]), recv_pp, 1) != 1 {

			// Is this useful troubleshooting information or just distracting noise?
			// Originally this was always printed but there was a request to add a "quiet" option to suppress this.
			// version 1.4: Instead, make the default off and activate it only with the debug igate option.

			if s_debug >= 1 {
				text_color_set(DW_COLOR_INFO)
				dw_printf("Packet from channel %d to IGate was rejected by filter: %s\n", channel, save_digi_config_p.filter_str[channel][MAX_TOTAL_CHANS])
			}
			return
		}
	}

	/*
	 * First make a copy of it because it might be modified in place.
	 */

	var pp = ax25_dup(recv_pp)
	Assert(pp != nil)

	/*
	 * Third party frames require special handling to unwrap payload.
	 */
	for ax25_get_dti(pp) == '}' {

		for n := C.int(0); n < ax25_get_num_repeaters(pp); n++ {
			var _via [AX25_MAX_ADDR_LEN]C.char /* includes ssid. Do we want to ignore it? */

			ax25_get_addr_with_ssid(pp, n+AX25_REPEATER_1, &_via[0])

			var via = C.GoString(&_via[0])

			if via == "TCPIP" ||
				via == "TCPXX" ||
				via == "RFONLY" ||
				via == "NOGATE" {

				if s_debug >= 1 {
					text_color_set(DW_COLOR_DEBUG)
					dw_printf("Rx IGate: Do not relay with %s in path.\n", via)
				}

				ax25_delete(pp)
				return
			}
		}

		if s_debug >= 1 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Rx IGate: Unwrap third party message.\n")
		}

		var inner_pp = ax25_unwrap_third_party(pp)
		if inner_pp == nil {
			ax25_delete(pp)
			return
		}
		ax25_delete(pp)
		pp = inner_pp
	}

	/*
	 * Do not relay packets with TCPIP, TCPXX, RFONLY, or NOGATE in the via path.
	 */
	for n := C.int(0); n < ax25_get_num_repeaters(pp); n++ {
		var _via [AX25_MAX_ADDR_LEN]C.char /* includes ssid. Do we want to ignore it? */

		ax25_get_addr_with_ssid(pp, n+AX25_REPEATER_1, &_via[0])

		var via = C.GoString(&_via[0])

		if via == "TCPIP" ||
			via == "TCPXX" ||
			via == "RFONLY" ||
			via == "NOGATE" {

			if s_debug >= 1 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("Rx IGate: Do not relay with %s in path.\n", via)
			}

			ax25_delete(pp)
			return
		}
	}

	/*
	 * Do not relay generic query.
	 * TODO:  Should probably block in other direction too, in case rf>is gateway did not drop.
	 */
	if ax25_get_dti(pp) == '?' {
		if s_debug >= 1 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Rx IGate: Do not relay generic query.\n")
		}
		ax25_delete(pp)
		return
	}

	/*
	 * Cut the information part at the first CR or LF.
	 * This is required because CR/LF is used as record separator when sending to server.
	 * Do NOT trim trailing spaces.
	 * Starting in 1.4 we preserve any nul characters in the information part.
	 */

	if ax25_cut_at_crlf(pp) > 0 {
		if s_debug >= 1 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Rx IGate: Truncated information part at CR.\n")
		}
	}

	var pinfo *C.uchar
	var info_len = ax25_get_info(pp, &pinfo)

	/*
	 * Someone around here occasionally sends a packet with no information part.
	 */
	if info_len == 0 {

		if s_debug >= 1 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Rx IGate: Information part length is zero.\n")
		}
		ax25_delete(pp)
		return
	}

	// TODO: Should we drop raw touch tone data object type generated here?

	/*
	 * If the SATgate mode is enabled, see if it should be delayed.
	 * The rule is if we hear it directly and it has at least one
	 * digipeater so there is potential of being re-transmitted.
	 * (Digis are all unused if we are hearing it directly from source.)
	 */
	if save_igate_config_p.satgate_delay > 0 &&
		ax25_get_heard(pp) == AX25_SOURCE &&
		ax25_get_num_repeaters(pp) > 0 {

		satgate_delay_packet(pp, channel)
	} else {
		send_packet_to_server(pp, channel)
	}

} /* end igate_send_rec_packet */

/*-------------------------------------------------------------------
 *
 * Name:        send_packet_to_server
 *
 * Purpose:     Convert to text and send to the IGate server.
 *
 * Inputs:	pp 	- Packet object.
 *
 *		channel	- Radio channel where it was received.
 *				This will be -1 if from a beacon with sendto=ig
 *				so be careful if using as subscript.
 *
 * Description:	Duplicate detection is handled here.
 *		Suppress if same was sent recently.
 *
 *--------------------------------------------------------------------*/

func send_packet_to_server(pp *packet_t, channel C.int) {

	var pinfo *C.uchar
	ax25_get_info(pp, &pinfo)

	/*
	 * We will often see the same packet multiple times close together due to digipeating.
	 * The consensus seems to be that we should just send the first and drop the later duplicates.
	 * There is some dissent on this issue. http://www.tapr.org/pipermail/aprssig/2016-July/045907.html
	 * There could be some value to sending them all to provide information about digipeater paths.
	 * However, the servers should drop all duplicates so we wasting everyone's time but sending duplicates.
	 * If you feel strongly about this issue, you could remove the following section.
	 * Currently rx_to_ig_allow only checks for recent duplicates.
	 */

	if !rx_to_ig_allow(pp) {
		if s_debug >= 1 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Rx IGate: Drop duplicate of same packet seen recently.\n")
		}
		ax25_delete(pp)
		return
	}

	/*
	 * Finally, append ",qAR," and my call to the path.
	 */

	/*
	 * It seems that the specification has changed recently.
	 * http://www.tapr.org/pipermail/aprssig/2016-December/046456.html
	 *
	 * We can see the history at the Internet Archive Wayback Machine.
	 *
	 * http://www.aprs-is.net/Connecting.aspx
	 *	captured Oct 19, 2016:
	 *		... Only the qAR construct may be generated by a client (IGate) on APRS-IS.
	 * 	Captured Dec 1, 2016:
	 *		... Only the qAR and qAO constructs may be generated by a client (IGate) on APRS-IS.
	 *
	 * http://www.aprs-is.net/q.aspx
	 *	Captured April 23, 2016:
	 *		(no mention of client generating qAO.)
	 *	Captured July 19, 2016:
	 *		qAO - (letter O) Packet is placed on APRS-IS by a receive-only IGate from RF.
	 *		The callSSID following the qAO is the callSSID of the IGate. Note that receive-only
	 *		IGates are discouraged on standard APRS frequencies. Please consider a bidirectional
	 *		IGate that only gates to RF messages for stations heard directly.
	 */

	var _msg [IGATE_MAX_MSG]C.char
	ax25_format_addrs(pp, &_msg[0])

	var msg = C.GoString(&_msg[0])

	msg = strings.TrimRight(msg, ":") /* Remove trailing ":" */

	if save_igate_config_p.tx_chan >= 0 {
		msg += ",qAR,"
	} else {
		msg += ",qAO," // new for version 1.4.
	}

	var mycall = save_audio_config_p.mycall[0]
	if channel >= 0 {
		mycall = save_audio_config_p.mycall[channel]
	}
	msg += mycall
	msg += ":"

	// It was reported that APRS packets, containing a nul byte in the information part,
	// are being truncated.  https://github.com/wb2osz/direwolf/issues/84
	//
	// One might argue that the packets are invalid and the proper behavior would be
	// to simply discard them, the same way we do if the CRC is bad.  One might argue
	// that we should simply pass along whatever we receive even if we don't like it.
	// We really shouldn't modify it and make the situation even worse.
	//
	// Chapter 5 of the APRS spec ( http://www.aprs.org/doc/APRS101.PDF ) says:
	//
	// 	"The comment may contain any printable ASCII characters (except | and ~,
	// 	which are reserved for TNC channel switching)."
	//
	// "Printable" would exclude character values less than space (00100000), e.g.
	// tab, carriage return, line feed, nul.  Sometimes we see carriage return
	// (00001010) at the end of APRS packets.   This would be in violation of the
	// specification.
	//
	// The MIC-E position format can have non printable characters (0x1c ... 0x1f, 0x7f)
	// in the information part.  An unfortunate decision, but it is not in the comment part.
	//
	// The base 91 telemetry format (http://he.fi/doc/aprs-base91-comment-telemetry.txt ),
	// which is not part of the APRS spec, uses the | character in the comment to delimit encoded
	// telemetry data.   This would be in violation of the original spec.  No one cares.
	//
	// The APRS Spec Addendum 1.2 Proposals ( http://www.aprs.org/aprs12/datum.txt)
	// adds use of UTF-8 (https://en.wikipedia.org/wiki/UTF-8 )for the free form text in
	// messages and comments. It can't be used in the fixed width fields.
	//
	// Non-ASCII characters are represented by multi-byte sequences.  All bytes in these
	// multi-byte sequences have the most significant bit set to 1.  Using UTF-8 would not
	// add any nul (00000000) bytes to the stream.
	//
	// Based on all of that, we would not expect to see a nul character in the information part.
	//
	// There are two known cases where we can have a nul character value.
	//
	// * The Kenwood TM-D710A sometimes sends packets like this:
	//
	// 	VA3AJ-9>T2QU6X,VE3WRC,WIDE1,K8UNS,WIDE2*:4P<0x00><0x0f>4T<0x00><0x0f>4X<0x00><0x0f>4\<0x00>`nW<0x1f>oS8>/]"6M}driving fast=
	// 	K4JH-9>S5UQ6X,WR4AGC-3*,WIDE1*:4P<0x00><0x0f>4T<0x00><0x0f>4X<0x00><0x0f>4\<0x00>`jP}l"&>/]"47}QRV from the EV =
	//
	//   Notice that the data type indicator of "4" is not valid.  If we remove
	//   4P<0x00><0x0f>4T<0x00><0x0f>4X<0x00><0x0f>4\<0x00>   we are left with a good MIC-E format.
	//   This same thing has been observed from others and is intermittent.
	//
	// * AGW Tracker can send UTF-16 if an option is selected.  This can introduce nul bytes.
	//   This is wrong, it should be using UTF-8.
	//
	// Rather than using strlcat here, we need to use memcpy and maintain our
	// own lengths, being careful to avoid buffer overflow.

	// KG Go strings can contain null bytes, so we're all good!
	// (Except I'm not convinced everything is correct here with type conversions...)

	msg += C.GoString((*C.char)(unsafe.Pointer(pinfo)))

	// TODO KG Check against IGATE_MAX_MSG size?

	send_msg_to_server(msg)
	stats_uplink_packets++

	/*
	 * Remember what was sent to avoid duplicates in near future.
	 */
	rx_to_ig_remember(pp)

	ax25_delete(pp)
} /* end send_packet_to_server */

/*-------------------------------------------------------------------
 *
 * Name:        send_msg_to_server
 *
 * Purpose:     Send something to the IGate server.
 *		This one function should be used for login, heartbeats,
 *		and packets.
 *
 * Inputs:	imsg	- Message.  We will add CR/LF here.
 *
 *		imsg_len - Length of imsg in bytes.
 *			  It could contain nul characters so we can't
 *			  use the normal C string functions.
 *
 * Description:	Send message to IGate Server if connected.
 *		Disconnect from server, and notify user, if any error.
 *		Should use a word other than message because that has
 *		a specific meaning for APRS.
 *
 *--------------------------------------------------------------------*/

func send_msg_to_server(imsg string) {

	if igate_sock == nil {
		return /* Silently discard if not connected. */
	}

	// TODO KG Truncate if > IGATE_MAX_MSG?
	/*
		if len(imsg)+2 > IGATE_MAX_MSG {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Rx IGate: Too long. Truncating.\n")
			stemp_len = IGATE_MAX_MSG - 2
		}
	*/

	if s_debug >= 1 {
		text_color_set(DW_COLOR_XMIT)
		dw_printf("[rx>ig] ")
		ax25_safe_print(C.CString(imsg), C.int(len(imsg)), 0)
		dw_printf("\n")
	}

	imsg += "\r\n"

	stats_uplink_bytes += len(imsg)

	var _, err = igate_sock.Write([]byte(imsg)) // TODO KG Should imsg just be a []byte?

	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("\nError sending to IGate server.  Closing connection.\n\n")
		igate_sock.Close()
		igate_sock = nil
	}
} /* end send_msg_to_server */

/*-------------------------------------------------------------------
 *
 * Name:        get1ch
 *
 * Purpose:     Read one byte from socket.
 *
 * Inputs:	igate_sock	- file handle for socket.
 *
 * Returns:	One byte from stream.
 *		Waits and tries again later if any error.
 *
 *
 *--------------------------------------------------------------------*/

func get1ch() byte {

	for {

		for igate_sock == nil {
			SLEEP_SEC(5) /* Not connected.  Try again later. */
		}

		/* Just get one byte at a time. */
		// TODO: might read complete packets and unpack from own buffer
		// rather than using a system call for each byte.

		var ch = make([]byte, 1)
		var n, _ = igate_sock.Read(ch)

		if n == 1 {
			/* TODO KG
			#if DEBUG9
				    dw_printf (log_fp, "%02x %c %c", ch,
						isprint(ch) ? ch : '.' ,
						(isupper(ch>>1) || isdigit(ch>>1) || (ch>>1) == ' ') ? (ch>>1) : '.');
				    if (ch == '\r') fprintf (log_fp, "  CR");
				    if (ch == '\n') fprintf (log_fp, "  LF");
				    fprintf (log_fp, "\n");
			#endif
			*/
			return (ch[0])
		}

		text_color_set(DW_COLOR_ERROR)
		dw_printf("\nError reading from IGate server.  Closing connection.\n\n")
		igate_sock.Close()
		igate_sock = nil
	}

} /* end get1ch */

/*-------------------------------------------------------------------
 *
 * Name:        igate_recv_thread
 *
 * Purpose:     Wait for messages from IGate Server.
 *
 * Outputs:	igate_sock	- File descriptor for communicating with client app.
 *
 * Description:	Process messages from the IGate server.
 *
 *--------------------------------------------------------------------*/

func igate_recv_thread() {

	/* TODO KG
	#if DEBUGx
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("igate_recv_thread ( socket = %d )\n", igate_sock);
	#endif
	*/

	for {
		var message []byte

		for {
			var ch = get1ch()
			stats_downlink_bytes++

			// I never expected to see a nul character but it can happen.
			// If found, change it to <0x00> and ax25_from_text will change it back to a single byte.
			// Along the way we can use the normal C string handling.

			if ch == 0 {
				message = append(message, []byte("<0x00>")...)
			} else {
				message = append(message, ch)
			}

			if ch == '\n' {
				break
			}
		}

		/*
		 * We have a complete message terminated by LF.
		 *
		 * Remove CR LF from end.
		 * This is a record separator for the protocol, not part of the data.
		 * Should probably have an error if we don't have this.
		 */
		message = bytes.TrimRight(message, "\n\r")

		/*
		 * I've seen a case where the original RF packet had a trailing CR but
		 * after someone else sent it to the server and it came back to me, that
		 * CR was now a trailing space.
		 *
		 * At first I was tempted to trim a trailing space as well.
		 * By fixing this one case it might corrupt the data in other cases.
		 * We compensate for this by ignoring trailing spaces when performing
		 * the duplicate detection and removal.
		 *
		 * We need to transmit exactly as we get it.
		 */

		/*
		 * I've also seen a multiple trailing spaces like this.
		 * Notice how safe_print shows a trailing space in hexadecimal to make it obvious.
		 *
		 * W1CLA-1>APVR30,TCPIP*,qAC,T2TOKYO3:;IRLP-4942*141503z4218.46NI07108.24W0446325-146IDLE    <0x20>
		 */

		if len(message) == 0 {
			/*
			 * Discard if zero length.
			 */
		} else if message[0] == '#' {
			/*
			 * Heartbeat or other control message.
			 *
			 * Print only if within seconds of logging in.
			 * That way we can see login confirmation but not
			 * be bothered by the heart beat messages.
			 */

			if !ok_to_send {
				text_color_set(DW_COLOR_REC)
				dw_printf("[ig] ")
				ax25_safe_print((*C.char)(C.CBytes(message)), C.int(len(message)), 0)
				dw_printf("\n")
			}
		} else {
			/*
			 * Convert to third party packet and transmit.
			 *
			 * Future: might have ability to configure multiple transmit
			 * channels, each with own client side filtering and via path.
			 * If so, loop here over all configured channels.
			 */
			text_color_set(DW_COLOR_REC)
			dw_printf("\n[ig>tx] ") // formerly just [ig]
			ax25_safe_print((*C.char)(C.CBytes(message)), C.int(len(message)), 0)
			dw_printf("\n")

			if bytes.Contains(message, []byte{0}) {
				// Invalid.  Either drop it or pass it along as-is.  Don't change.

				text_color_set(DW_COLOR_ERROR)
				dw_printf("'nul' character found in packet from IS.  This should never happen.\n")
				dw_printf("The source station is probably transmitting with defective software.\n")

				//if (strcmp((char*)pinfo, "4P") == 0) {
				//  dw_printf("The TM-D710 will do this intermittently.  A firmware upgrade is needed to fix it.\n");
				//}
			}

			/*
			 * Record that we heard from the source address.
			 */
			mheard_save_is((*C.char)(C.CBytes(message)))

			stats_downlink_packets++

			/*
			 * Possibly transmit if so configured.
			 */
			var to_chan = save_igate_config_p.tx_chan

			if to_chan >= 0 {
				maybe_xmit_packet_from_igate(message, to_chan)
			}

			/*
			 * New in 1.7:  If ICHANNEL was specified, send packet to client app as specified channel.
			 */
			if save_audio_config_p.igate_vchannel >= 0 {

				var ichan = save_audio_config_p.igate_vchannel

				// My original poorly thoughtout idea was to parse it into a packet object,
				// using the non-strict option, and send to the client app.
				//
				// A lot of things can go wrong with that approach.

				// (1)  Up to 8 digipeaters are allowed in radio format.
				//      There is a potential of finding a larger number here.
				//
				// (2)  The via path can have names that are not valid in the radio format.
				//      e.g.  qAC, T2HAKATA, N5JXS-F1.
				//      Non-strict parsing would force uppercase, truncate names too long,
				//      and drop unacceptable SSIDs.
				//
				// (3) The source address could be invalid for the RF address format.
				//     e.g.  WHO-IS>APJIW4,TCPIP*,qAC,AE5PL-JF::ZL1JSH-9 :Charles Beadfield/New Zealand{583
				//     That is essential information that we absolutely need to preserve.
				//
				// I think the only correct solution is to apply a third party header
				// wrapper so the original contents are preserved.  This will be a little
				// more work for the application developer.  Search for ":}" and use only
				// the part after that.  At this point, I don't see any value in encoding
				// information in the source/destination so I will just use "X>X:}" as a prefix

				var stemp = append([]byte("X>X:}"), message...)

				var pp3 = ax25_from_text((*C.char)(C.CBytes(stemp)), 0) // 0 means not strict
				if pp3 != nil {

					var alevel alevel_t
					alevel.mark = -2 // FIXME: Do we want some other special case?
					alevel.space = -2

					var subchan C.int = -2 // FIXME: -1 is special case for APRStt.
					// See what happens with -2 and follow up on this.
					// Do we need something else here?
					var slice C.int = 0
					var fec_type fec_type_t = fec_type_none
					var spectrum = C.CString("APRS-IS")
					dlq_rec_frame(C.int(ichan), subchan, slice, pp3, alevel, fec_type, RETRY_NONE, spectrum)
				} else {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("ICHANNEL %d: Could not parse message from APRS-IS server.\n", ichan)
					dw_printf("%s\n", message)
				}
			} // end ICHANNEL option
		}
	} /* while (1) */
} /* end igate_recv_thread */

/*-------------------------------------------------------------------
 *
 * Name:        satgate_delay_packet
 *
 * Purpose:     Put packet into holding area for a while rather than
 *		sending it immediately to the IS server.
 *
 * Inputs:	pp	- Packet object.
 *
 *		channel	- Radio channel where received.
 *
 * Outputs:	Appended to queue.
 *
 * Description:	If we hear a packet directly and the same one digipeated,
 *		we only send the first to the APRS IS due to duplicate removal.
 *		It may be desirable to favor the digipeated packet over the
 *		original.  For this situation, we have an option which delays
 *		a packet if we hear it directly and the via path is not empty.
 *		We know we heard it directly if none of the digipeater
 *		addresses have been used.
 *		This way the digipeated packet will go first.
 *		The original is sent about 10 seconds later.
 *		Duplicate removal will drop the original if there is no
 *		corresponding digipeated version.
 *
 *
 *		This was an idea that came up in one of the discussion forums.
 *		I rushed in without thinking about it very much.
 *
 * 		In retrospect, I don't think this was such a good idea.
 *		It would be of value only if there is no other IGate nearby
 *		that would report on the original transmission.
 *		I wonder if anyone would notice if this silently disappeared.
 *
 *--------------------------------------------------------------------*/

func satgate_delay_packet(pp *packet_t, channel C.int) {

	//if (s_debug >= 1) {
	text_color_set(DW_COLOR_INFO)
	dw_printf("Rx IGate: SATgate mode, delay packet heard directly.\n")
	//}

	ax25_set_release_time(pp, C.double(float64(time.Now().UnixNano())/1e9)+C.double(save_igate_config_p.satgate_delay))
	//TODO: save channel too.

	dp_mutex.Lock()

	var pnext, plast *packet_t
	if dp_queue_head == nil {
		dp_queue_head = pp
	} else {
		plast = dp_queue_head
		for {
			pnext = ax25_get_nextp(plast)
			if pnext == nil {
				break
			}
			plast = pnext
		}
		ax25_set_nextp(plast, pp)
	}

	dp_mutex.Unlock()
} /* end satgate_delay_packet */

/*-------------------------------------------------------------------
 *
 * Name:        satgate_delay_thread
 *
 * Purpose:     Release packet when specified release time has arrived.
 *
 * Inputs:	dp_queue_head	- Queue of packets.
 *
 * Outputs:	Sent to APRS IS.
 *
 * Description:	For simplicity we'll just poll each second.
 *		Release the packet when its time has arrived.
 *
 *--------------------------------------------------------------------*/

func satgate_delay_thread() {
	var channel C.int = 0 // TODO:  get receive channel somehow.
	// only matters if multi channel with different names.

	for {
		SLEEP_SEC(1)

		/* Don't need critical region just to peek */

		if dp_queue_head != nil {

			var now = C.double(float64(time.Now().UnixNano()) / 1e9)

			var release_time = ax25_get_release_time(dp_queue_head)

			if now > release_time {
				dp_mutex.Lock()

				var pp = dp_queue_head
				dp_queue_head = ax25_get_nextp(pp)

				dp_mutex.Unlock()
				ax25_set_nextp(pp, nil)

				send_packet_to_server(pp, channel)
			}
		} /* if something in queue */
	} /* while (1) */
} /* end satgate_delay_thread */

/*-------------------------------------------------------------------
 *
 * Name:        maybe_xmit_packet_from_igate
 *
 * Purpose:     Convert text string, from IGate server, to third party
 *		packet and send to transmit queue if appropriate.
 *
 * Inputs:	message		- As sent by the server.
 *				  Any trailing CRLF should have been removed.
 *				  Typical examples:
 *
 *				KA1BTK-5>APDR13,TCPIP*,qAC,T2IRELAND:=4237.62N/07040.68W$/A=-00054 http://aprsdroid.org/
 *				N1HKO-10>APJI40,TCPIP*,qAC,N1HKO-JS:<IGATE,MSG_CNT=0,LOC_CNT=0
 *				K1RI-2>APWW10,WIDE1-1,WIDE2-1,qAS,K1RI:/221700h/9AmA<Ct3_ sT010/002g005t045r000p023P020h97b10148
 *				KC1BOS-2>T3PQ3S,WIDE1-1,WIDE2-1,qAR,W1TG-1:`c)@qh\>/"50}TinyTrak4 Mobile
 *
 *				  This is interesting because the source is not a valid AX.25 address.
 *				  Non-RF stations can have 2 alphanumeric characters for SSID.
 *				  In this example, the WHO-IS server is responding to a message.
 *
 *				WHO-IS>APJIW4,TCPIP*,qAC,AE5PL-JF::ZL1JSH-9 :Charles Beadfield/New Zealand{583
 *
 *
 *				  Notice how the final digipeater address, in the header, might not
 *				  be a valid AX.25 address.  We see a 9 character address
 *				  (with no ssid) and an ssid of two letters.
 *				  We don't care because we end up discarding them before
 *				  repackaging to go over the radio.
 *
 *				  The "q construct"  ( http://www.aprs-is.net/q.aspx ) provides
 *				  a clue about the journey taken. "qAX" means that the station sending
 *				  the packet to the server did not login properly as a ham radio
 *				  operator so we don't want to put this on to RF.
 *
 *		to_chan		- Radio channel for transmitting.
 *
 *--------------------------------------------------------------------*/

// It is unforunate that the : data type indicator (DTI) was overloaded with
// so many different meanings.  Simply looking at the DTI is not adequate for
// determining whether a packet is a message.
// We need to exclude the other special cases of telemetry metadata,
// bulletins, and weather bulletins.

func is_message_message(infop string) bool {
	if !strings.HasPrefix(infop, ":") {
		return false
	}

	if len(infop) < 11 {
		return false // too short for : addressee :
	}

	if len(infop) >= 16 {
		switch infop[10:16] {
		case ":PARM.", ":UNIT.", ":EQNS.", ":BITS.":
			return false
		}
	}

	if len(infop) >= 4 {
		switch infop[1:4] {
		case "BLN", "NWS", "SKY", "CWA", "BOM":
			return false
		}
	}

	return true // message, including ack, rej
}

func maybe_xmit_packet_from_igate(message []byte, to_chan C.int) {

	Assert(to_chan >= 0 && to_chan < MAX_TOTAL_CHANS)

	/*
	 * Try to parse it into a packet object; we need this for the packet filtering.
	 *
	 * We use the non-strict option because there the via path can have:
	 *	- station names longer than 6.
	 *	- alphanumeric SSID.
	 *	- lower case for "q constructs.
	 * We don't care about any of those because the via path will be discarded anyhow.
	 *
	 * The other issue, that I did not think of originally, is that the "source"
	 * address might not conform to AX.25 restrictions when it originally came
	 * from a non-RF source.  For example an APRS "message" might be sent to the
	 * "WHO-IS" server, and the reply message would have that for the source address.
	 *
	 * Originally, I used the source address from the packet object but that was
	 * missing the alphanumeric SSID.  This needs to be done differently.
	 *
	 * Potential Bug:  Up to 8 digipeaters are allowed in radio format.
	 * Is there a possibility of finding a larger number here?
	 */
	var pp3 = ax25_from_text((*C.char)(C.CBytes(message)), 0)
	if pp3 == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Tx IGate: Could not parse message from server.\n")
		dw_printf("%s\n", message)
		return
	}

	// Issue 408: The source address might not be valid AX.25 because it
	// came from a non-RF station.  e.g.  some server responding to a message.
	// We need to take source address from original rather than extracting it
	// from the packet object.

	var src, _, _ = bytes.Cut(message, []byte(">"))

	/*
	 * Drop if path contains:
	 *	NOGATE or RFONLY - means IGate should not pass them.
	 *	TCPXX or qAX - means it came from somewhere that did not identify itself correctly.
	 */
	for n := C.int(0); n < ax25_get_num_repeaters(pp3); n++ {
		var _via [AX25_MAX_ADDR_LEN]C.char /* includes ssid. Do we want to ignore it? */

		ax25_get_addr_with_ssid(pp3, n+AX25_REPEATER_1, &_via[0])

		var via = C.GoString(&_via[0])

		if via == "qAX" || // qAX deprecated. http://www.aprs-is.net/q.aspx
			via == "TCPXX" || // TCPXX deprecated.
			via == "RFONLY" ||
			via == "NOGATE" {

			if s_debug >= 1 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("Tx IGate: Do not transmit with %s in path.\n", via)
			}

			ax25_delete(pp3)
			return
		}
	}

	/*
	 * Apply our own packet filtering if configured.
	 * Do we want to do this before or after removing the VIA path?
	 * I suppose by doing it first, we have the possibility of
	 * filtering by stations along the way or the q construct.
	 */

	Assert(to_chan >= 0 && to_chan < MAX_TOTAL_CHANS)

	/*
	 * We have a rather strange special case here.
	 * If we recently transmitted a 'message' from some station,
	 * send the position of the message sender when it comes along later.
	 *
	 * Some refer to this as a "courtesy posit report" but I don't
	 * think that is an official term.
	 *
	 * If we have a position report, look up the sender and see if we should
	 * bypass the normal filtering.
	 *
	 * Reference:  https://www.aprs-is.net/IGating.aspx
	 *
	 *	"Passing all message packets also includes passing the sending station's position
	 *	along with the message. When APRS-IS was small, we did this using historical position
	 *	packets. This has become problematic as it introduces historical data on to RF.
	 *	The IGate should note the station(s) it has gated messages to RF for and pass
	 *	the next position packet seen for that station(s) to RF."
	 */

	// TODO: Not quite this simple.  Should have a function to check for position.
	// $ raw gps could be a position.  @ could be weather data depending on symbol.

	var _pinfo *C.uchar
	var info_len = ax25_get_info(pp3, (&_pinfo))
	var pinfo = C.GoBytes(unsafe.Pointer(_pinfo), info_len)

	var msp_special_case = false

	if info_len >= 1 && bytes.ContainsAny(pinfo[0:1], "!=/@'`") {

		var n = mheard_get_msp((*C.char)(C.CBytes(src)))

		if n > 0 {

			msp_special_case = true

			if s_debug >= 1 {
				text_color_set(DW_COLOR_INFO)
				dw_printf("Special case, allow position from message sender %s, %d remaining.\n", src, n-1)
			}

			mheard_set_msp((*C.char)(C.CBytes(src)), n-1)
		}
	}

	if !msp_special_case {

		if save_digi_config_p.filter_str[MAX_TOTAL_CHANS][to_chan] != "" {

			if pfilter(MAX_TOTAL_CHANS, to_chan, C.CString(save_digi_config_p.filter_str[MAX_TOTAL_CHANS][to_chan]), pp3, 1) != 1 {

				// Previously there was a debug message here about the packet being dropped by filtering.
				// This is now handled better by the "-df" command line option for filtering details.

				ax25_delete(pp3)
				return
			}
		}
	}

	/*
	 * We want to discard the via path, as received from the APRS-IS, then
	 * replace it with TCPIP and our own call, marked as used.
	 *
	 *
	 * For example, we might get something like this from the server.
	 *	K1USN-1>APWW10,TCPIP*,qAC,N5JXS-F1:T#479,100,048,002,500,000,10000000
	 *
	 * We want to transform it to this before wrapping it as third party traffic.
	 *	K1USN-1>APWW10,TCPIP,mycall*:T#479,100,048,002,500,000,10000000
	 */

	/*
	 * These are typical examples where we see TCPIP*,qAC,<server>
	 *
	 *	N3LLO-4>APRX28,TCPIP*,qAC,T2NUENGLD:T#474,21.4,0.3,114.0,4.0,0.0,00000000
	 *	N1WJO>APWW10,TCPIP*,qAC,T2MAINE:)147.120!4412.27N/07033.27WrW1OCA repeater136.5 Tone Norway Me
	 *	AB1OC-10>APWW10,TCPIP*,qAC,T2IAD2:=4242.70N/07135.41W#(Time 0:00:00)!INSERVICE!!W60!
	 *
	 * But sometimes we get a different form:
	 *
	 *	N1YG-1>T1SY9P,WIDE1-1,WIDE2-2,qAR,W2DAN-15:'c&<0x7f>l <0x1c>-/>
	 *	W1HS-8>TSSP9T,WIDE1-1,WIDE2-1,qAR,N3LLO-2:`d^Vl"W>/'"85}|*&%_'[|!wLK!|3
	 *	N1RCW-1>APU25N,MA2-2,qAR,KA1VCQ-1:=4140.41N/07030.21W-Home Station/Fill-in Digi {UIV32N}
	 *	N1IEJ>T4PY3U,W1EMA-1,WIDE1*,WIDE2-2,qAR,KD1KE:`a5"l!<0x7f>-/]"4f}Retired & Busy=
	 *
	 * Oh!  They have qAR rather than qAC.  What does that mean?
	 * From  http://www.aprs-is.net/q.aspx
	 *
	 *	qAC - Packet was received from the client directly via a verified connection (FROMCALL=login).
	 *		The callSSID following the qAC is the server's callsign-SSID.
	 *
	 *	qAR - Packet was received directly (via a verified connection) from an IGate using the ,I construct.
	 *		The callSSID following the qAR it the callSSID of the IGate.
	 *
	 * What is the ",I" construct?
	 * Do we care here?
	 * Is it something new and improved that we should be using in the other direction?
	 */

	var dest [AX25_MAX_ADDR_LEN]C.char /* Destination field. */
	ax25_get_addr_with_ssid(pp3, AX25_DESTINATION, &dest[0])
	var payload = fmt.Sprintf("%s>%s,TCPIP,%s*:%s", string(src), C.GoString(&dest[0]), save_audio_config_p.mycall[to_chan], pinfo)

	/* TODO KG
	#if DEBUGx
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("Tx IGate: DEBUG payload=%s\n", payload);
	#endif
	*/

	/*
	 * Encapsulate for sending over radio if no reason to drop it.
	 */

	/*
	 * We don't want to suppress duplicate "messages" within a short time period.
	 * Suppose we transmitted a "message" for some station and it did not respond with an ack.
	 * 25 seconds later the sender retries.  Wouldn't we want to pass along that retry?
	 *
	 * "Messages" get preferential treatment because they are high value and very rare.
	 *	-> Bypass the duplicate suppression.
	 *	-> Raise the rate limiting value.
	 */
	if ig_to_tx_allow(pp3, to_chan) {
		var radio = fmt.Sprintf("%s>%s%d%d%s:}%s",
			save_audio_config_p.mycall[to_chan],
			APP_TOCALL, C.MAJOR_VERSION, C.MINOR_VERSION,
			C.GoString(&save_igate_config_p.tx_via[0]),
			payload)

		var pradio = ax25_from_text(C.CString(radio), 1)
		if pradio != nil {

			/* TODO KG
			#if ITEST
				    text_color_set(DW_COLOR_XMIT);
				    dw_printf ("Xmit: %s\n", radio);
				    ax25_delete (pradio);
			#else
			*/
			/* This consumes packet so don't reference it again! */
			tq_append(to_chan, TQ_PRIO_1_LO, pradio)
			// TODO KG #endif
			stats_rf_xmit_packets++ // Any type of packet.

			if is_message_message(string(pinfo)) {

				// We transmitted a "message."  Telemetry metadata is excluded.
				// Remember to pass along address of the sender later.

				stats_msg_cnt++ // Update statistics.

				mheard_set_msp((*C.char)(C.CBytes(src)), save_igate_config_p.igmsp)
			}

			ig_to_tx_remember(pp3, save_igate_config_p.tx_chan, 0) // correct. version before encapsulating it.
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Received invalid packet from IGate.\n")
			dw_printf("%s\n", payload)
			dw_printf("Will not attempt to transmit third party packet.\n")
			dw_printf("%s\n", radio)
		}

	}

	ax25_delete(pp3)

} /* end maybe_xmit_packet_from_igate */

/*-------------------------------------------------------------------
 *
 * Name:        rx_to_ig_remember
 *
 * Purpose:     Keep a record of packets sent to the IGate server
 *		so we don't send duplicates within some set amount of time.
 *
 * Inputs:	pp	- Pointer to a packet object.
 *
 *-------------------------------------------------------------------
 *
 * Name:	rx_to_ig_allow
 *
 * Purpose:	Check whether this is a duplicate of another
 *		recently received from RF and sent to the Server
 *
 * Input:	pp	- Pointer to packet object.
 *
 * Returns:	True if it is OK to send.
 *
 *-------------------------------------------------------------------
 *
 * Description: These two functions perform the final stage of filtering
 *		before sending a received (from radio) packet to the IGate server.
 *
 *		rx_to_ig_remember must be called for every packet sent to the server.
 *
 *		rx_to_ig_allow decides whether this should be allowed thru
 *		based on recent activity.  We will drop the packet if it is a
 *		duplicate of another sent recently.
 *
 *		Rather than storing the entire packet, we just keep a CRC to
 *		reduce memory and processing requirements.  We do the same in
 *		the digipeater function to suppress duplicates.
 *
 *		There is a 1 / 65536 chance of getting a false positive match
 *		which is good enough for this application.
 *
 *
 * Original thinking:
 *
 *		Occasionally someone will get on one of the discussion groups and say:
 *		I don't think my IGate is working.  I look at packets, from local stations,
 *		on aprs.fi or findu.com, and they are always through some other IGate station,
 *		never mine.
 *		Then someone has to explain, this is not a valid strategy for analyzing
 *		everything going thru the network.   The APRS-IS servers drop duplicate
 *		packets (ignoring the via path) within a 30 second period.  If some
 *		other IGate gets the same thing there a millisecond faster than you,
 *		the one you send is discarded.
 *		In this scenario, it would make sense to perform additional duplicate
 *		suppression before forwarding RF packets to the Server.
 *		I don't recall if I saw some specific recommendation to do this or if
 *		it just seemed like the obvious thing to do to avoid sending useless
 *		stuff that would just be discarded anyhow.  It seems others came to the
 *		same conclusion.  http://www.tapr.org/pipermail/aprssig/2016-July/045907.html
 *
 * Version 1.5:	Rethink strategy.
 *
 *		Issue 85, https://github.com/wb2osz/direwolf/issues/85 ,
 *		got me thinking about this some more.  Sending more information will
 *		allow the APRS-IS servers to perform future additional network analysis.
 *		To make a long story short, the RF>IS direction duplicate checking
 *		is now disabled.   The code is still there in case I change my mind
 *		and want to add a configuration option to allow it.  The dedupe
 *		time is set to 0 which means don't do the checking.
 *
 *--------------------------------------------------------------------*/

const RX2IG_HISTORY_MAX = 30 /* Remember the last 30 sent to IGate server. */

var rx2ig_insert_next C.int
var rx2ig_time_stamp [RX2IG_HISTORY_MAX]time.Time
var rx2ig_checksum [RX2IG_HISTORY_MAX]C.ushort

func rx_to_ig_init() {
	for n := 0; n < RX2IG_HISTORY_MAX; n++ {
		rx2ig_time_stamp[n] = time.Time{}
		rx2ig_checksum[n] = 0
	}
	rx2ig_insert_next = 0
}

func rx_to_ig_remember(pp *packet_t) {

	// No need to save the information if we are not doing duplicate checking.

	if save_igate_config_p.rx2ig_dedupe_time == 0 {
		return
	}

	rx2ig_time_stamp[rx2ig_insert_next] = time.Now()
	rx2ig_checksum[rx2ig_insert_next] = ax25_dedupe_crc(pp)

	if s_debug >= 3 {
		var src [AX25_MAX_ADDR_LEN]C.char
		var dest [AX25_MAX_ADDR_LEN]C.char
		var pinfo *C.uchar

		ax25_get_addr_with_ssid(pp, AX25_SOURCE, &src[0])
		ax25_get_addr_with_ssid(pp, AX25_DESTINATION, &dest[0])
		ax25_get_info(pp, &pinfo)

		text_color_set(DW_COLOR_DEBUG)
		dw_printf("rx_to_ig_remember [%d] = %s %d \"%s>%s:%s\"\n",
			rx2ig_insert_next,
			rx2ig_time_stamp[rx2ig_insert_next].String(),
			rx2ig_checksum[rx2ig_insert_next],
			C.GoString(&src[0]), C.GoString(&dest[0]), C.GoString((*C.char)(unsafe.Pointer(pinfo))))
	}

	rx2ig_insert_next++
	if rx2ig_insert_next >= RX2IG_HISTORY_MAX {
		rx2ig_insert_next = 0
	}
}

func rx_to_ig_allow(pp *packet_t) bool {
	var crc = ax25_dedupe_crc(pp)
	var now = time.Now()

	if s_debug >= 2 {
		var src [AX25_MAX_ADDR_LEN]C.char
		var dest [AX25_MAX_ADDR_LEN]C.char
		var pinfo *C.uchar

		ax25_get_addr_with_ssid(pp, AX25_SOURCE, &src[0])
		ax25_get_addr_with_ssid(pp, AX25_DESTINATION, &dest[0])
		ax25_get_info(pp, &pinfo)

		text_color_set(DW_COLOR_DEBUG)
		dw_printf("rx_to_ig_allow? %d \"%s>%s:%s\"\n", crc, C.GoString(&src[0]), C.GoString(&dest[0]), C.GoString((*C.char)(unsafe.Pointer(pinfo))))
	}

	// Do we have duplicate checking at all in the RF>IS direction?

	if save_igate_config_p.rx2ig_dedupe_time == 0 {
		if s_debug >= 2 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("rx_to_ig_allow? YES, no dedupe checking\n")
		}
		return true
	}

	// Yes, check for duplicates within certain time.

	for j := 0; j < RX2IG_HISTORY_MAX; j++ {
		if rx2ig_checksum[j] == crc && !rx2ig_time_stamp[j].Before(now.Add(-time.Duration(save_igate_config_p.rx2ig_dedupe_time)*time.Second)) {
			if s_debug >= 2 {
				text_color_set(DW_COLOR_DEBUG)
				// could be multiple entries and this might not be the most recent.
				dw_printf("rx_to_ig_allow? NO. Seen %d seconds ago.\n", int(time.Since(rx2ig_time_stamp[j]).Seconds()))
			}
			return false
		}
	}

	if s_debug >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("rx_to_ig_allow? YES\n")
	}
	return true

} /* end rx_to_ig_allow */

/*-------------------------------------------------------------------
 *
 * Name:        ig_to_tx_remember
 *
 * Purpose:     Keep a record of packets sent from IGate server to radio transmitter
 *		so we don't send duplicates within some set amount of time.
 *
 * Inputs:	pp	- Pointer to a packet object.
 *
 *		channel	- Channel number where it is being transmitted.
 *			  Duplicate detection needs to be separate for each radio channel.
 *
 *		bydigi	- True if transmitted by digipeater function.  False for IGate.
 *			  Why do we care about digpeating here?  See discussion below.
 *
 *------------------------------------------------------------------------------
 *
 * Name:	ig_to_tx_allow
 *
 * Purpose:	Check whether this is a duplicate of another sent recently
 *		or if we exceed the transmit rate limits.
 *
 * Input:	pp	- Pointer to packet object.
 *
 *		channel	- Radio channel number where we want to transmit.
 *
 * Returns:	True if it is OK to send.
 *
 *------------------------------------------------------------------------------
 *
 * Description: These two functions perform the final stage of filtering
 *		before sending a packet from the IGate server to the radio.
 *
 *		ig_to_tx_remember must be called for every packet, from the IGate
 *		server, sent to the radio transmitter.
 *
 *		ig_to_tx_allow decides whether this should be allowed thru
 *		based on recent activity.  We will drop the packet if it is a
 *		duplicate of another sent recently.
 *
 *		This is the essentially the same as the pair of functions
 *		above, for RF to IS, with one additional restriction.
 *
 *		The typical residential Internet connection is around 10,000
 *		to 50,000 times faster than the radio links we are using.  It would
 *		be easy to completely saturate the radio channel if we are
 *		not careful.
 *
 *		Besides looking for duplicates, this will also tabulate the
 *		number of packets sent during the past minute and past 5
 *		minutes and stop sending if a limit is reached.
 *
 * More Discussion:
 *
 *		Consider the following example.
 *		I hear a packet from W1TG-1 three times over the radio then get the
 *		(almost) same thing twice from APRS-IS.
 *
 *
 *		Digipeater N3LEE-10 audio level = 23(10/6)   [NONE]   __|||||||
 *		[0.5] W1TG-1>APU25N,N3LEE-10*,WIDE2-1:<IGATE,MSG_CNT=30,LOC_CNT=61<0x0d>
 *		Station Capabilities, Ambulance, UIview 32 bit apps
 *		IGATE,MSG_CNT=30,LOC_CNT=61
 *
 *		[0H] W1TG-1>APU25N,N3LEE-10,WB2OSZ-14*:<IGATE,MSG_CNT=30,LOC_CNT=61<0x0d>
 *
 *		Digipeater WIDE2 (probably N3LEE-4) audio level = 22(10/6)   [NONE]   __|||||||
 *		[0.5] W1TG-1>APU25N,N3LEE-10,N3LEE-4,WIDE2*:<IGATE,MSG_CNT=30,LOC_CNT=61<0x0d>
 *		Station Capabilities, Ambulance, UIview 32 bit apps
 *		IGATE,MSG_CNT=30,LOC_CNT=61
 *
 *		Digipeater WIDE2 (probably AB1OC-10) audio level = 31(14/11)   [SINGLE]   ____:____
 *		[0.4] W1TG-1>APU25N,N3LEE-10,AB1OC-10,WIDE2*:<IGATE,MSG_CNT=30,LOC_CNT=61<0x0d>
 *		Station Capabilities, Ambulance, UIview 32 bit apps
 *		IGATE,MSG_CNT=30,LOC_CNT=61
 *
 *		[ig] W1TG-1>APU25N,WIDE2-2,qAR,W1GLO-11:<IGATE,MSG_CNT=30,LOC_CNT=61
 *		[0L] WB2OSZ-14>APDW13,WIDE1-1:}W1TG-1>APU25N,TCPIP,WB2OSZ-14*:<IGATE,MSG_CNT=30,LOC_CNT=61
 *
 *		[ig] W1TG-1>APU25N,K1FFK,WIDE2*,qAR,WB2ZII-15:<IGATE,MSG_CNT=30,LOC_CNT=61<0x20>
 *		[0L] WB2OSZ-14>APDW13,WIDE1-1:}W1TG-1>APU25N,TCPIP,WB2OSZ-14*:<IGATE,MSG_CNT=30,LOC_CNT=61<0x20>
 *
 *
 *		The first one gets retransmitted by digipeating.
 *
 *		Why are we getting the same thing twice from APRS-IS?  Shouldn't remove duplicates?
 *		Look closely.  The original packet, on RF, had a CR character at the end.
 *		At first I thought duplicate removal was broken but it turns out they
 *		are not exactly the same.
 *
 *		>>> The receive IGate spec says a packet should be cut at a CR. <<<
 *
 *		In one case it is removed as expected   In another case, it is replaced by a trailing
 *		space character.  Maybe someone thought non printable characters should be
 *		replaced by spaces???  (I have since been told someone thought it would be a good
 *		idea to replace unprintable characters with spaces.  How's that working out for MIC-E position???)
 *
 *		At first I was tempted to remove any trailing spaces to make up for the other
 *		IGate adding it.  Two wrongs don't make a right.   Trailing spaces are not that
 *		rare and removing them would corrupt the data.  My new strategy is for
 *		the duplicate detection compare to ignore trailing space, CR, and LF.
 *
 *		We already transmitted the same thing by the digipeater function so this should
 *		also go into memory for avoiding duplicates out of the transmit IGate.
 *
 * Future:
 *		Should the digipeater function avoid transmitting something if it
 *		was recently transmitted by the IGate function?
 *		This code is pretty much the same as dedupe.c. Maybe it could all
 *		be combined into one.  Need to ponder this some more.
 *
 *--------------------------------------------------------------------*/

/*
Here is another complete example, with the "-diii" debugging option to show details.


We receive the signal directly from the source: (zzz.log 1011)

	N1ZKO-7 audio level = 33(16/10)   [NONE]   ___||||||
	[0.5] N1ZKO-7>T2TS7X,WIDE1-1,WIDE2-1:`c6wl!i[/>"4]}[scanning]=<0x0d>
	MIC-E, Human, Kenwood TH-D72, In Service
	N 42 43.7800, W 071 26.9100, 0 MPH, course 177, alt 230 ft
	[scanning]

We did not send it to the IS server recently.

	Rx IGate: Truncated information part at CR.
	rx_to_ig_allow? 57185 "N1ZKO-7>T2TS7X:`c6wl!i[/>"4]}[scanning]="
	rx_to_ig_allow? YES

Send it now and remember that fact.

	[rx>ig] N1ZKO-7>T2TS7X,WIDE1-1,WIDE2-1,qAR,WB2OSZ-14:`c6wl!i[/>"4]}[scanning]=
	rx_to_ig_remember [21] = 1447683040 57185 "N1ZKO-7>T2TS7X:`c6wl!i[/>"4]}[scanning]="

Digipeat it.  Notice how it has a trailing CR.
TODO:  Why is the CRC different?  Content looks the same.

	ig_to_tx_remember [38] = ch0 d1 1447683040 27598 "N1ZKO-7>T2TS7X:`c6wl!i[/>"4]}[scanning]="
	[0H] N1ZKO-7>T2TS7X,WB2OSZ-14*,WIDE2-1:`c6wl!i[/>"4]}[scanning]=<0x0d>

Now we hear it again, thru a digipeater.
Not sure who.   Was it UNCAN or was it someone else who doesn't use tracing?
See my rant in the User Guide about this.

	Digipeater WIDE2 (probably UNCAN) audio level = 30(15/10)   [NONE]   __|||::__
	[0.4] N1ZKO-7>T2TS7X,KB1POR-2,UNCAN,WIDE2*:`c6wl!i[/>"4]}[scanning]=<0x0d>
	MIC-E, Human, Kenwood TH-D72, In Service
	N 42 43.7800, W 071 26.9100, 0 MPH, course 177, alt 230 ft
	[scanning]

Was sent to server recently so don't do it again.

	Rx IGate: Truncated information part at CR.
	rx_to_ig_allow? 57185 "N1ZKO-7>T2TS7X:`c6wl!i[/>"4]}[scanning]="
	rx_to_ig_allow? NO. Seen 1 seconds ago.
	Rx IGate: Drop duplicate of same packet seen recently.

We hear it a third time, by a different digipeater.

	Digipeater WIDE1 (probably N3LEE-10) audio level = 23(12/6)   [NONE]   __|||||||
	[0.5] N1ZKO-7>T2TS7X,N3LEE-10,WIDE1*,WIDE2-1:`c6wl!i[/>"4]}[scanning]=<0x0d>
	MIC-E, Human, Kenwood TH-D72, In Service
	N 42 43.7800, W 071 26.9100, 0 MPH, course 177, alt 230 ft
	[scanning]

It's a duplicate, so don't send to server.

	Rx IGate: Truncated information part at CR.
	rx_to_ig_allow? 57185 "N1ZKO-7>T2TS7X:`c6wl!i[/>"4]}[scanning]="
	rx_to_ig_allow? NO. Seen 2 seconds ago.
	Rx IGate: Drop duplicate of same packet seen recently.
	Digipeater: Drop redundant packet to channel 0.

The server sends it to us.
NOTICE: The CR at the end has been replaced by a space.

	[ig>tx] N1ZKO-7>T2TS7X,K1FFK,WA2MJM-15*,qAR,WB2ZII-15:`c6wl!i[/>"4]}[scanning]=<0x20>

Should we transmit it?
No, we sent it recently by the digipeating function (note "bydigi=1").

	DEBUG:  ax25_dedupe_crc ignoring trailing space.
	ig_to_tx_allow? ch0 27598 "N1ZKO-7>T2TS7X:`c6wl!i[/>"4]}[scanning]= "
	ig_to_tx_allow? NO. Sent 4 seconds ago. bydigi=1
	Tx IGate: Drop duplicate packet transmitted recently.
	[0L] WB2OSZ-14>APDW13,WIDE1-1:}W1AST>TRPR4T,TCPIP,WB2OSZ-14*:`d=Ml!3>/"4N}
	[rx>ig] #
*/

const IG2TX_DEDUPE_TIME = 60 * time.Second /* Do not send duplicate within 60 seconds. */
const IG2TX_HISTORY_MAX = 50               /* Remember the last 50 sent from server to radio. */

/* Ideally this should be a critical region because */
/* it is being written by two threads but I'm not that concerned. */

var ig2tx_insert_next C.int
var ig2tx_time_stamp [IG2TX_HISTORY_MAX]time.Time
var ig2tx_checksum [IG2TX_HISTORY_MAX]C.ushort
var ig2tx_chan [IG2TX_HISTORY_MAX]C.int
var ig2tx_bydigi [IG2TX_HISTORY_MAX]C.int

func ig_to_tx_init() {
	for n := 0; n < IG2TX_HISTORY_MAX; n++ {
		ig2tx_time_stamp[n] = time.Time{}
		ig2tx_checksum[n] = 0
		ig2tx_chan[n] = 0xff
		ig2tx_bydigi[n] = 0
	}
	ig2tx_insert_next = 0
}

func ig_to_tx_remember(pp *packet_t, channel C.int, bydigi C.int) {
	var now = time.Now()
	var crc = ax25_dedupe_crc(pp)

	if s_debug >= 3 {
		var src [AX25_MAX_ADDR_LEN]C.char
		var dest [AX25_MAX_ADDR_LEN]C.char
		var pinfo *C.uchar

		ax25_get_addr_with_ssid(pp, AX25_SOURCE, &src[0])
		ax25_get_addr_with_ssid(pp, AX25_DESTINATION, &dest[0])
		ax25_get_info(pp, &pinfo)

		text_color_set(DW_COLOR_DEBUG)
		dw_printf("ig_to_tx_remember [%d] = ch%d d%d %s %d \"%s>%s:%s\"\n",
			ig2tx_insert_next,
			channel, bydigi,
			now.String(), crc,
			C.GoString(&src[0]), C.GoString(&dest[0]), C.GoString((*C.char)(unsafe.Pointer(pinfo))))
	}

	ig2tx_time_stamp[ig2tx_insert_next] = now
	ig2tx_checksum[ig2tx_insert_next] = crc
	ig2tx_chan[ig2tx_insert_next] = channel
	ig2tx_bydigi[ig2tx_insert_next] = bydigi

	ig2tx_insert_next++
	if ig2tx_insert_next >= IG2TX_HISTORY_MAX {
		ig2tx_insert_next = 0
	}
}

func ig_to_tx_allow(pp *packet_t, channel C.int) bool {
	var crc = ax25_dedupe_crc(pp)
	var now = time.Now()

	var pinfo *C.uchar
	ax25_get_info(pp, &pinfo)

	if s_debug >= 2 {
		var src [AX25_MAX_ADDR_LEN]C.char
		var dest [AX25_MAX_ADDR_LEN]C.char

		ax25_get_addr_with_ssid(pp, AX25_SOURCE, &src[0])
		ax25_get_addr_with_ssid(pp, AX25_DESTINATION, &dest[0])

		text_color_set(DW_COLOR_DEBUG)
		dw_printf("ig_to_tx_allow? ch%d %d \"%s>%s:%s\"\n", channel, crc, C.GoString(&src[0]), C.GoString(&dest[0]), C.GoString((*C.char)(unsafe.Pointer(pinfo))))
	}

	/* Consider transmissions on this channel only by either digi or IGate. */

	for j := 0; j < IG2TX_HISTORY_MAX; j++ {
		if ig2tx_checksum[j] == crc && ig2tx_chan[j] == channel && !ig2tx_time_stamp[j].Before(now.Add(-IG2TX_DEDUPE_TIME)) {

			/* We have a duplicate within some time period. */

			if is_message_message(C.GoString((*C.char)(unsafe.Pointer(pinfo)))) {

				/* I think I want to avoid the duplicate suppression for "messages." */
				/* Suppose we transmit a message from station X and it doesn't get an ack back. */
				/* Station X then sends exactly the same thing 20 seconds later.  */
				/* We don't want to suppress the retry. */

				if s_debug >= 2 {
					text_color_set(DW_COLOR_DEBUG)
					dw_printf("ig_to_tx_allow? Yes for duplicate message sent %d seconds ago. bydigi=%d\n", int(time.Since(ig2tx_time_stamp[j]).Seconds()), ig2tx_bydigi[j])
				}
			} else {

				/* Normal (non-message) case. */

				if s_debug >= 2 {
					text_color_set(DW_COLOR_DEBUG)
					// could be multiple entries and this might not be the most recent.
					dw_printf("ig_to_tx_allow? NO. Duplicate sent %d seconds ago. bydigi=%d\n", int(time.Since(ig2tx_time_stamp[j]).Seconds()), ig2tx_bydigi[j])
				}

				text_color_set(DW_COLOR_INFO)
				dw_printf("Tx IGate: Drop duplicate packet transmitted recently.\n")
				return false
			}
		}
	}

	/* IGate transmit counts must not include digipeater transmissions. */

	var count_1 C.int = 0
	var count_5 C.int = 0
	for j := 0; j < IG2TX_HISTORY_MAX; j++ {
		if ig2tx_chan[j] == channel && ig2tx_bydigi[j] == 0 {
			if !ig2tx_time_stamp[j].Before(time.Now().Add(-60 * time.Second)) {
				count_1++
			}
			if !ig2tx_time_stamp[j].Before(time.Now().Add(-300 * time.Second)) {
				count_5++
			}
		}
	}

	/* "Messages" (special APRS data type ":") are intentional and more */
	/* important than all of the other mostly repetitive useless junk */
	/* flowing thru here.  */
	/* It would be unfortunate to discard a message because we already */
	/* hit our limit.  I don't want to completely eliminate limiting for */
	/* messages, in case something goes terribly wrong, but we can triple */
	/* the normal limit for them. */

	var increase_limit C.int = 1
	if is_message_message(C.GoString((*C.char)(unsafe.Pointer(pinfo)))) {
		increase_limit = 3
	}

	if count_1 >= save_igate_config_p.tx_limit_1*increase_limit {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Tx IGate: Already transmitted maximum of %d packets in 1 minute.\n", save_igate_config_p.tx_limit_1)
		return false
	}
	if count_5 >= save_igate_config_p.tx_limit_5*increase_limit {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Tx IGate: Already transmitted maximum of %d packets in 5 minutes.\n", save_igate_config_p.tx_limit_5)
		return false
	}

	if s_debug >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("ig_to_tx_allow? YES\n")
	}

	return true

} /* end ig_to_tx_allow */

/* end igate.c */
