//nolint:gochecknoglobals
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

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/metrics"
	"github.com/sirupsen/logrus"
)

const DEFAULT_IGATE_PORT = 14580

type igate_config_s struct {

	/*
	 * For logging into the IGate server.
	 */
	t2_server_name string /* Tier 2 IGate server name. */

	t2_server_port int /* Typically 14580. */

	t2_login string /* e.g. WA9XYZ-15 */
	/* Note that the ssid could be any two alphanumeric */
	/* characters not just 1 thru 15. */
	/* Could be same or different than the radio call(s). */
	/* Not sure what the consequences would be. */

	t2_passcode string /* Max. 5 digits. Could be "-1". */

	t2_filter string /* Optional filter for IS -> RF direction. */
	/* This is the "server side" filter. */
	/* A better name would be subscription or something */
	/* like that because we can only ask for more. */

	/*
	 * For transmitting.
	 */
	tx_chan int /* Radio channel for transmitting. */
	/* 0=first, etc.  -1 for none. */
	/* Presently IGate can transmit on only a single channel. */
	/* A future version might generalize this.  */
	/* Each transmit channel would have its own client side filtering. */

	tx_via string /* VIA path for transmitting third party packets. */
	/* Usual text representation.  */
	/* Must start with "," if not empty so it can */
	/* simply be inserted after the destination address. */

	max_digi_hops int /* Maximum number of digipeater hops possible for via path. */
	/* Derived from the SSID when last character of address is a digit. */
	/* e.g.  "WIDE1-1,WIDE5-2" would be 3. */
	/* This is useful to know so we can determine how many */
	/* stations we might be able to reach. */

	tx_limit_1 int /* Max. packets to transmit in 1 minute. */

	tx_limit_5 int /* Max. packets to transmit in 5 minutes. */

	igmsp int /* Number of message sender position reports to allow. */
	/* Common practice is to default to 1.  */
	/* We allow additional flexibility of 0 to disable feature */
	/* or a small number to allow more. */

	/*
	 * Receiver to IS data options.
	 */
	rx2ig_dedupe_time int /* seconds.  0 to disable. */

	/*
	 * Special SATgate mode to delay packets heard directly.
	 */
	satgate_delay int /* seconds.  0 to disable. */
}

const IGATE_TX_LIMIT_1_DEFAULT = 6
const IGATE_TX_LIMIT_1_MAX = 20

const IGATE_TX_LIMIT_5_DEFAULT = 20
const IGATE_TX_LIMIT_5_MAX = 80

const IGATE_RX2IG_DEDUPE_TIME = 0 /* Issue 85.  0 means disable dupe checking in RF>IS direction. */
/* See comments in rxToIgRemember & rxToIgAllow. */
/* Currently there is no configuration setting to change this. */

const DEFAULT_SATGATE_DELAY = 10
const MIN_SATGATE_DELAY = 5
const MAX_SATGATE_DELAY = 30

// IGate bridges the radio channels and an APRS-IS server: it passes packets
// heard on the air up to the server, and packets from the server back out over
// the air, dropping a good deal in both directions along the way.
//
// All of this was package-level state - file-scope statics in Dire Wolf that
// the port flattened into a single package, where any other file could reach
// them and two of them collided with same-named statics elsewhere.  See issue
// #674.
//
// Synchronisation is as it was: dpMutex covers the SATgate delay queue and
// nothing else.  sock, okToSend and the counters are still read and written by
// the connect, receive and delay goroutines without a lock, which #674 also
// has in its sights.
type IGate struct {
	/*
	 * What NewIGate was given.  These need to be kept around in case the
	 * connection is lost and we need to reestablish it later.
	 *
	 * audioConfig: all we care about is the number of radio channels and
	 * the radio call and SSID for each.  digiConfig: the packet filtering
	 * options.
	 */
	audioConfig *audio_s
	config      *igate_config_s
	digiConfig  *digi_config_s

	/*
	 * debugLevel	- 0  print packets FROM APRS-IS,
	 *		     establishing connection with server, and
	 *		     and anything rejected by client side filtering.
	 *		  1  plus packets sent TO server or why not.
	 *		  2  plus duplicate detection overview.
	 *		  3  plus duplicate detection details.
	 */
	debugLevel int

	dpMutex     sync.Mutex /* Critical section for delayed packet queue. */
	dpQueueHead *packet_t

	sock net.Conn

	/*
	 * After connecting to server, we want to make sure
	 * that the login sequence is sent first.
	 * This is set to true after the login is complete.
	 */
	okToSend bool

	stats igateStats

	rx2ig rx2igHistory
	ig2tx ig2txHistory
}

/*
 * Statistics for IGate function.
 * Note that the RF related counters are just a subset of what is happening on radio channels.
 *
 * TODO: should have debug option to print these occasionally.
 */

type igateStats struct {
	/* Most recent time connection was established. */
	/* can be used to determine elapsed connect time. */
	connectedAt time.Time

	/* Number of packets passed along to the IGate */
	/* server after filtering. */
	uplinkPackets int

	/* Total number of bytes sent to IGate server */
	/* including login, packets, and heartbeats. */
	uplinkBytes int

	/* Total number of bytes from IGate server including */
	/* packets, heartbeats, other messages. */
	downlinkBytes int

	/* Number of packets from IGate server for possible transmission. */
	/* Fewer might be transmitted due to filtering or rate limiting. */
	downlinkPackets int

	/* Number of packets passed along to radio, for the IGate function, */
	/* after filtering, rate limiting, or other restrictions. */
	/* Number of packets transmitted for beacons, digipeating, */
	/* or client applications are not included here. */
	rfXmitPackets int

	/* Number of "messages" transmitted.  Subset of above. */
	/* A "message" has the data type indicator of ":" and it is */
	/* not the special case of telemetry metadata. */
	msgCount int
}

// igate is the IGate.  Until DirewolfMain replaces it with a configured one,
// it is inert - no configuration, no connection - so that the packet paths
// which reach for it before, or without, an IGate being set up find something
// harmless rather than nil.
var igate = NewIGate(nil, nil, nil, 0)

// NewIGate returns an IGate that knows what it is meant to do but is not yet
// doing it.  start connects to the server and sets the goroutines going.
func NewIGate(audioConfig *audio_s, igateConfig *igate_config_s, digiConfig *digi_config_s, debugLevel int) *IGate {
	var ig = &IGate{ //nolint:exhaustruct_v5
		audioConfig: audioConfig,
		config:      igateConfig,
		digiConfig:  digiConfig,
		debugLevel:  debugLevel,
	}

	ig.rx2ig.reset()
	ig.ig2tx.reset()

	return ig
}

/*
 * Make some of these available for IGate statistics beacon like
 *
 *	WB2OSZ>APDW14,WIDE1-1:<IGATE,MSG_CNT=2,PKT_CNT=0,DIR_CNT=10,LOC_CNT=35,RF_CNT=45
 *
 * MSG_CNT is only "messages."   From original spec.
 * PKT_CNT is other (non-message) packets.  Followed precedent of APRSISCE32.
 */

// msgCount is how many "messages" have gone out over the air.
func (ig *IGate) msgCount() int {
	return ig.stats.msgCount
}

// pktCount is how many packets other than "messages" have gone out over the
// air.
func (ig *IGate) pktCount() int {
	return ig.stats.rfXmitPackets - ig.stats.msgCount
}

// uplinkCount is how many packets have been passed up to the server.
func (ig *IGate) uplinkCount() int {
	return ig.stats.uplinkPackets
}

// downlinkCount is how many packets have come down from the server, whether or
// not they were then transmitted.
func (ig *IGate) downlinkCount() int {
	return ig.stats.downlinkPackets
}

/*-------------------------------------------------------------------
 *
 * Name:        start
 *
 * Purpose:     One time initialization when main application starts up.
 *
 * Description:	This starts two threads:
 *
 *		  *  to establish and maintain a connection to the server.
 *		  *  to listen for packets from the server.
 *
 *		and a third, if the SATgate delay is configured, to let
 *		delayed packets continue once their time has come.
 *
 *--------------------------------------------------------------------*/

func (ig *IGate) start(ctx context.Context) {
	logrus.WithFields(logrus.Fields{
		"t2_server_name": ig.config.t2_server_name,
		"t2_server_port": ig.config.t2_server_port,
		"t2_login":       ig.config.t2_login,
		"t2_filter":      ig.config.t2_filter,
	}).Debug("igate start")

	/*
	 * Continue only if we have server name, login, and passcode.
	 */
	if len(ig.config.t2_server_name) == 0 ||
		len(ig.config.t2_login) == 0 ||
		len(ig.config.t2_passcode) == 0 {
		return
	}

	/*
	 * This connects to the server and sets ig.sock.
	 * It also sends periodic messages to say I'm still alive.
	 */

	go ig.connectThread(ctx)

	/*
	 * This reads messages from client when ig.sock is valid.
	 */

	go ig.recvThread(ctx)

	/*
	 * This lets delayed packets continue after specified amount of time.
	 */

	if ig.config.satgate_delay > 0 {
		go ig.satgateDelayThread(ctx)
	}
} /* end start */

/*-------------------------------------------------------------------
 *
 * Name:        connectThread
 *
 * Purpose:     Establish connection with IGate server.
 *		Send periodic heartbeat to keep keep connection active.
 *		Reconnect if something goes wrong and we got disconnected.
 *
 * Outputs:	ig.sock	- File descriptor for communicating with client app.
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

// igate_dial makes a single connection attempt to an APRS-IS server, recording
// the outcome.  Exactly one of the connect/failed-connect metrics moves per
// attempt: samoyed_igate_connects_total counts connections that were actually
// established, not attempts that were made.
func igate_dial(ctx context.Context, server_name string, server_port int) (net.Conn, error) {
	var conn, err = new(net.Dialer).DialContext(ctx, "tcp", net.JoinHostPort(server_name, strconv.Itoa(server_port)))
	if err != nil {
		metrics.RecordIgateFailedConnect()

		return nil, err
	}

	metrics.RecordIgateConnect()

	return conn, nil
}

// connectThread keeps a connection to the IGate server up until ctx is
// cancelled.
func (ig *IGate) connectThread(ctx context.Context) {
	logrus.WithField("port", ig.config.t2_server_port).Debug("igate connectThread start")
	var server_name = ig.config.t2_server_name

	/*
	 * Repeat until told to stop.
	 */

	for ctx.Err() == nil {
		/*
		 * Connect to IGate server if not currently connected.
		 */
		if ig.sock == nil {
			var conn, connErr = igate_dial(ctx, server_name, ig.config.t2_server_port)
			ig.stats.connectedAt = time.Now()

			if connErr != nil {
				text_color_set(DW_COLOR_INFO)
				dw_printf("Connect to IGate server %s failed.\n\n", server_name)
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
				 * Set ig.sock so everyone else can start using it.
				 * But make the Rx -> Internet messages wait until after login.
				 */

				ig.okToSend = false
				ig.sock = conn

				/*
				 * Send login message.
				 * Software name and version must not contain spaces.
				 */

				if !sleepSecCtx(ctx, 3) {
					return
				}

				var stemp = fmt.Sprintf("user %s pass %s vers Samoyed %s",
					ig.config.t2_login, ig.config.t2_passcode,
					SAMOYED_VERSION)
				if ig.config.t2_filter != "" {
					stemp += " filter "
					stemp += ig.config.t2_filter
				}

				ig.sendMsgToServer(stemp)

				/* Delay until it is ok to start sending packets. */

				if !sleepSecCtx(ctx, 7) {
					return
				}

				ig.okToSend = true
			}
		}

		/*
		 * If connected to IGate server, send heartbeat periodically to keep connection active.
		 */
		for range 3 {
			if ig.sock != nil && !sleepSecCtx(ctx, 10) {
				return
			}
		}

		if ig.sock != nil {
			/* This will close the socket if any error. */
			ig.sendMsgToServer("#")
		}
	}
} /* end connectThread */

/*-------------------------------------------------------------------
 *
 * Name:        sendRecPacket
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

func (ig *IGate) sendRecPacket(channel int, recv_pp *packet_t) {
	if ig.sock == nil {
		return /* Silently discard if not connected. */
	}

	if !ig.okToSend {
		return /* Login not complete. */
	}

	/* Gather statistics. */

	metrics.RecordRFReceived()

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
		ig.digiConfig.filter_str[channel][MAX_TOTAL_CHANS] != "" {
		var result, err = pfilter(channel, MAX_TOTAL_CHANS, ig.digiConfig.filter_str[channel][MAX_TOTAL_CHANS], recv_pp, true)
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("%s\n", err)
		}

		if result != 1 {
			// Is this useful troubleshooting information or just distracting noise?
			// Originally this was always printed but there was a request to add a "quiet" option to suppress this.
			// version 1.4: Instead, make the default off and activate it only with the debug igate option.
			if ig.debugLevel >= 1 {
				text_color_set(DW_COLOR_INFO)
				dw_printf("Packet from channel %d to IGate was rejected by filter: %s\n", channel, ig.digiConfig.filter_str[channel][MAX_TOTAL_CHANS])
			}

			return
		}
	}

	/*
	 * First make a copy of it because it might be modified in place.
	 */

	var pp = ax25_dup(recv_pp)

	/*
	 * Third party frames require special handling to unwrap payload.
	 */
	for ax25_get_dti(pp) == '}' {
		for n := range ax25_get_num_repeaters(pp) {
			/* includes ssid. Do we want to ignore it? */
			var via = ax25_get_addr_with_ssid(pp, n+AX25_REPEATER_1)

			if via == "TCPIP" ||
				via == "TCPXX" ||
				via == "RFONLY" ||
				via == "NOGATE" {
				if ig.debugLevel >= 1 {
					text_color_set(DW_COLOR_DEBUG)
					dw_printf("Rx IGate: Do not relay with %s in path.\n", via)
				}

				return
			}
		}

		if ig.debugLevel >= 1 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Rx IGate: Unwrap third party message.\n")
		}

		var inner_pp = ax25_unwrap_third_party(pp)
		if inner_pp == nil {
			return
		}

		pp = inner_pp
	}

	/*
	 * Do not relay packets with TCPIP, TCPXX, RFONLY, or NOGATE in the via path.
	 */
	for n := range ax25_get_num_repeaters(pp) {
		/* includes ssid. Do we want to ignore it? */
		var via = ax25_get_addr_with_ssid(pp, n+AX25_REPEATER_1)

		if via == "TCPIP" ||
			via == "TCPXX" ||
			via == "RFONLY" ||
			via == "NOGATE" {
			if ig.debugLevel >= 1 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("Rx IGate: Do not relay with %s in path.\n", via)
			}

			return
		}
	}

	/*
	 * Do not relay generic query.
	 * TODO:  Should probably block in other direction too, in case rf>is gateway did not drop.
	 */
	if ax25_get_dti(pp) == '?' {
		if ig.debugLevel >= 1 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Rx IGate: Do not relay generic query.\n")
		}

		return
	}

	/*
	 * Cut the information part at the first CR or LF.
	 * This is required because CR/LF is used as record separator when sending to server.
	 * Do NOT trim trailing spaces.
	 * Starting in 1.4 we preserve any nul characters in the information part.
	 */

	if ax25_cut_at_crlf(pp) > 0 {
		if ig.debugLevel >= 1 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Rx IGate: Truncated information part at CR.\n")
		}
	}

	var pinfo = AX25GetInfo(pp)

	/*
	 * Someone around here occasionally sends a packet with no information part.
	 */
	if len(pinfo) == 0 {
		if ig.debugLevel >= 1 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Rx IGate: Information part length is zero.\n")
		}

		return
	}

	// TODO: Should we drop raw touch tone data object type generated here?

	/*
	 * If the SATgate mode is enabled, see if it should be delayed.
	 * The rule is if we hear it directly and it has at least one
	 * digipeater so there is potential of being re-transmitted.
	 * (Digis are all unused if we are hearing it directly from source.)
	 */
	if ig.config.satgate_delay > 0 &&
		ax25_get_heard(pp) == AX25_SOURCE &&
		ax25_get_num_repeaters(pp) > 0 {
		ig.satgateDelayPacket(pp, channel)
	} else {
		ig.sendPacketToServer(pp, channel)
	}
} /* end sendRecPacket */

/*-------------------------------------------------------------------
 *
 * Name:        sendPacketToServer
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

func (ig *IGate) sendPacketToServer(pp *packet_t, channel int) {
	var pinfo = AX25GetInfo(pp)

	/*
	 * We will often see the same packet multiple times close together due to digipeating.
	 * The consensus seems to be that we should just send the first and drop the later duplicates.
	 * There is some dissent on this issue. http://www.tapr.org/pipermail/aprssig/2016-July/045907.html
	 * There could be some value to sending them all to provide information about digipeater paths.
	 * However, the servers should drop all duplicates so we wasting everyone's time but sending duplicates.
	 * If you feel strongly about this issue, you could remove the following section.
	 * Currently rxToIgAllow only checks for recent duplicates.
	 */

	if !ig.rxToIgAllow(pp) {
		if ig.debugLevel >= 1 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Rx IGate: Drop duplicate of same packet seen recently.\n")
		}

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

	var msg = AX25FormatAddrs(pp)

	msg = strings.TrimRight(msg, ":") /* Remove trailing ":" */

	if ig.config.tx_chan >= 0 {
		msg += ",qAR,"
	} else {
		msg += ",qAO," // new for version 1.4.
	}

	var mycall = ig.audioConfig.mycall[0]
	if channel >= 0 {
		mycall = ig.audioConfig.mycall[channel]
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

	msg += string(pinfo)

	// TODO KG Check against IGATE_MAX_MSG size?

	ig.sendMsgToServer(msg)

	ig.stats.uplinkPackets++
	metrics.RecordUplink()

	/*
	 * Remember what was sent to avoid duplicates in near future.
	 */
	ig.rxToIgRemember(pp)
} /* end sendPacketToServer */

/*-------------------------------------------------------------------
 *
 * Name:        sendMsgToServer
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

func (ig *IGate) sendMsgToServer(imsg string) {
	if ig.sock == nil {
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

	if ig.debugLevel >= 1 {
		text_color_set(DW_COLOR_XMIT)
		dw_printf("[rx>ig] ")
		AX25SafePrint([]byte(imsg), false)
		dw_printf("\n")
	}

	imsg += "\r\n"

	ig.stats.uplinkBytes += len(imsg)

	var _, err = ig.sock.Write([]byte(imsg)) // TODO KG Should imsg just be a []byte?
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("\nError sending to IGate server.  Closing connection.\n\n")
		ig.sock.Close()
		ig.sock = nil
	}
} /* end sendMsgToServer */

/*-------------------------------------------------------------------
 *
 * Name:        get1ch
 *
 * Purpose:     Read one byte from socket.
 *
 * Inputs:	ig.sock	- file handle for socket.
 *
 * Returns:	One byte from stream.
 *		Waits and tries again later if any error.
 *
 *
 *--------------------------------------------------------------------*/

// get1ch returns the next byte from the IGate server.  It reports false
// instead if ctx was cancelled, in which case there is no byte and the caller
// should stop.
func (ig *IGate) get1ch(ctx context.Context) (byte, bool) {
	for ctx.Err() == nil {
		for ig.sock == nil {
			if !sleepSecCtx(ctx, 5) { /* Not connected.  Try again later. */
				return 0, false
			}
		}

		/* Just get one byte at a time. */
		// TODO: might read complete packets and unpack from own buffer
		// rather than using a system call for each byte.

		var conn = ig.sock
		if conn == nil {
			continue // It went away between the check above and here.
		}

		// A server with nothing to say leaves the read below blocked, so
		// closing the socket is what gets us back when we are asked to stop.
		var stopClose = closeOnDone(ctx, conn)

		var ch = make([]byte, 1)
		var n, _ = conn.Read(ch)

		stopClose()

		if ctx.Err() != nil {
			// Ours to close: nothing will read from it again.
			conn.Close()

			if ig.sock == conn {
				ig.sock = nil
			}

			return 0, false
		}

		if n == 1 {
			if logrus.IsLevelEnabled(logrus.TraceLevel) {
				logrus.WithField("ch", fmt.Sprintf("%02x", ch[0])).Trace("get1ch")
			}

			return ch[0], true
		}

		text_color_set(DW_COLOR_ERROR)
		dw_printf("\nError reading from IGate server.  Closing connection.\n\n")
		conn.Close()

		if ig.sock == conn {
			ig.sock = nil
		}
	}

	return 0, false
} /* end get1ch */

/*-------------------------------------------------------------------
 *
 * Name:        recvThread
 *
 * Purpose:     Wait for messages from IGate Server.
 *
 * Outputs:	ig.sock	- File descriptor for communicating with client app.
 *
 * Description:	Process messages from the IGate server.
 *
 *--------------------------------------------------------------------*/

func (ig *IGate) recvThread(ctx context.Context) {
	logrus.Debug("igate recvThread")

	for ctx.Err() == nil {
		var message []byte

		for {
			var ch, ok = ig.get1ch(ctx)
			if !ok {
				return // Cancelled.
			}

			ig.stats.downlinkBytes++

			// I never expected to see a nul character but it can happen.
			// If found, change it to <0x00> and AX25FromText will change it back to a single byte.
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
			if !ig.okToSend {
				text_color_set(DW_COLOR_REC)
				dw_printf("[ig] ")
				AX25SafePrint(message, false)
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
			AX25SafePrint(message, false)
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
			mheardDB.SaveIS(string(message))

			ig.stats.downlinkPackets++
			metrics.RecordDownlink()

			/*
			 * Possibly transmit if so configured.
			 */
			var to_chan = ig.config.tx_chan

			if to_chan >= 0 {
				ig.maybeXmitPacketFromIGate(message, to_chan)
			}

			/*
			 * New in 1.7:  If ICHANNEL was specified, send packet to client app as specified channel.
			 */
			if ig.audioConfig.igate_vchannel >= 0 {
				var ichan = ig.audioConfig.igate_vchannel

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

				var pp3 = AX25FromText(string(stemp), false)
				if pp3 != nil {
					var alevel ALevel
					alevel.mark = -2 // FIXME: Do we want some other special case?
					alevel.space = -2

					var subchan = -2 // FIXME: -1 is special case for APRStt.
					// See what happens with -2 and follow up on this.
					// Do we need something else here?
					var slice = 0
					var fec_type = fec_type_none
					var spectrum = "APRS-IS"
					dataLinkQueue.RecFrame(ichan, subchan, slice, pp3, alevel, fec_type, RETRY_NONE, spectrum)
				} else {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("ICHANNEL %d: Could not parse message from APRS-IS server.\n", ichan)
					dw_printf("%s\n", message)
				}
			} // end ICHANNEL option
		}
	} /* while (1) */
} /* end recvThread */

/*-------------------------------------------------------------------
 *
 * Name:        satgateDelayPacket
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

func (ig *IGate) satgateDelayPacket(pp *packet_t, channel int) { //nolint:unparam
	//if (ig.debugLevel >= 1) {
	text_color_set(DW_COLOR_INFO)
	dw_printf("Rx IGate: SATgate mode, delay packet heard directly.\n")
	//}

	ax25_set_release_time(pp, time.Now().Add(time.Duration(ig.config.satgate_delay)*time.Second))
	//TODO: save channel too.

	ig.dpMutex.Lock()

	var pnext, plast *packet_t

	if ig.dpQueueHead == nil {
		ig.dpQueueHead = pp
	} else {
		plast = ig.dpQueueHead
		for {
			pnext = ax25_get_nextp(plast)
			if pnext == nil {
				break
			}

			plast = pnext
		}

		ax25_set_nextp(plast, pp)
	}

	ig.dpMutex.Unlock()
} /* end satgateDelayPacket */

/*-------------------------------------------------------------------
 *
 * Name:        satgateDelayThread
 *
 * Purpose:     Release packet when specified release time has arrived.
 *
 * Inputs:	ig.dpQueueHead	- Queue of packets.
 *
 * Outputs:	Sent to APRS IS.
 *
 * Description:	For simplicity we'll just poll each second.
 *		Release the packet when its time has arrived.
 *
 *--------------------------------------------------------------------*/

func (ig *IGate) satgateDelayThread(ctx context.Context) {
	var channel = 0 // TODO:  get receive channel somehow.
	// only matters if multi channel with different names.

	for {
		if !sleepSecCtx(ctx, 1) {
			return
		}

		/* Don't need critical region just to peek */

		if ig.dpQueueHead != nil {
			var release_time = ax25_get_release_time(ig.dpQueueHead)

			if time.Now().After(release_time) {
				ig.dpMutex.Lock()

				var pp = ig.dpQueueHead
				ig.dpQueueHead = ax25_get_nextp(pp)

				ig.dpMutex.Unlock()
				ax25_set_nextp(pp, nil)

				ig.sendPacketToServer(pp, channel)
			}
		} /* if something in queue */
	} /* until cancelled */
} /* end satgateDelayThread */

/*-------------------------------------------------------------------
 *
 * Name:        maybeXmitPacketFromIGate
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

func (ig *IGate) maybeXmitPacketFromIGate(message []byte, to_chan int) {
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
	var pp3 = AX25FromText(string(message), false)
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
	for n := range ax25_get_num_repeaters(pp3) {
		/* includes ssid. Do we want to ignore it? */
		var via = ax25_get_addr_with_ssid(pp3, n+AX25_REPEATER_1)

		// "QAX" rather than "qAX": the addresses come back from the parser
		// upper-cased, whatever case they arrived in, so the q construct
		// never matched its own spelling and a packet from a station that
		// did not identify itself properly went out over the air.
		if via == "QAX" || // qAX deprecated. http://www.aprs-is.net/q.aspx
			via == "TCPXX" || // TCPXX deprecated.
			via == "RFONLY" ||
			via == "NOGATE" {
			if ig.debugLevel >= 1 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("Tx IGate: Do not transmit with %s in path.\n", via)
			}

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

	var pinfo = AX25GetInfo(pp3)

	var msp_special_case = false

	if len(pinfo) >= 1 && bytes.ContainsAny(pinfo[0:1], "!=/@'`") {
		var n = mheardDB.GetMSP(string(src))

		if n > 0 {
			msp_special_case = true

			if ig.debugLevel >= 1 {
				text_color_set(DW_COLOR_INFO)
				dw_printf("Special case, allow position from message sender %s, %d remaining.\n", src, n-1)
			}

			mheardDB.SetMSP(string(src), n-1)
		}
	}

	if !msp_special_case {
		if ig.digiConfig.filter_str[MAX_TOTAL_CHANS][to_chan] != "" {
			var result, err = pfilter(MAX_TOTAL_CHANS, to_chan, ig.digiConfig.filter_str[MAX_TOTAL_CHANS][to_chan], pp3, true)
			if err != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("%s\n", err)
			}

			if result != 1 {
				// Previously there was a debug message here about the packet being dropped by filtering.
				// This is now handled better by the "-df" command line option for filtering details.

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

	/* Destination field. */
	var dest = ax25_get_addr_with_ssid(pp3, AX25_DESTINATION)
	var payload = fmt.Sprintf("%s>%s,TCPIP,%s*:%s", string(src), dest, ig.audioConfig.mycall[to_chan], pinfo)

	logrus.WithField("payload", payload).Debug("Tx IGate")

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
	if ig.igToTxAllow(pp3, to_chan) {
		var radio = fmt.Sprintf("%s>%s%d%d%s:}%s",
			ig.audioConfig.mycall[to_chan],
			APP_TOCALL, MAJOR_VERSION, MINOR_VERSION,
			ig.config.tx_via,
			payload)

		var pradio = AX25FromText(radio, true)
		if pradio != nil {
			/* This consumes packet so don't reference it again! */
			transmitQueue.Append(to_chan, TQ_PRIO_1_LO, pradio)
			ig.stats.rfXmitPackets++ // Any type of packet.
			metrics.RecordRFTransmitted()

			if is_message_message(string(pinfo)) {
				// We transmitted a "message."  Telemetry metadata is excluded.
				// Remember to pass along address of the sender later.
				ig.stats.msgCount++ // Update statistics.

				mheardDB.SetMSP(string(src), ig.config.igmsp)
			}

			ig.igToTxRemember(pp3, ig.config.tx_chan, 0) // correct. version before encapsulating it.
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Received invalid packet from IGate.\n")
			dw_printf("%s\n", payload)
			dw_printf("Will not attempt to transmit third party packet.\n")
			dw_printf("%s\n", radio)
		}
	}
} /* end maybeXmitPacketFromIGate */

/*-------------------------------------------------------------------
 *
 * Name:        rxToIgRemember
 *
 * Purpose:     Keep a record of packets sent to the IGate server
 *		so we don't send duplicates within some set amount of time.
 *
 * Inputs:	pp	- Pointer to a packet object.
 *
 *-------------------------------------------------------------------
 *
 * Name:	rxToIgAllow
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
 *		rxToIgRemember must be called for every packet sent to the server.
 *
 *		rxToIgAllow decides whether this should be allowed thru
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

// rx2igEntry is one packet the IGate passed up to the server.  Rather than
// storing the whole packet we keep only a CRC of it, which is all the
// duplicate check needs.
type rx2igEntry struct {
	timeStamp time.Time
	checksum  int
}

// rx2igHistory is a ring of the last RX2IG_HISTORY_MAX of those, oldest
// overwritten first.
type rx2igHistory struct {
	entries    [RX2IG_HISTORY_MAX]rx2igEntry
	insertNext int
}

func (h *rx2igHistory) reset() {
	*h = rx2igHistory{} //nolint:exhaustruct_v5
}

func (ig *IGate) rxToIgRemember(pp *packet_t) {
	// No need to save the information if we are not doing duplicate checking.
	if ig.config.rx2ig_dedupe_time == 0 {
		return
	}

	ig.rx2ig.entries[ig.rx2ig.insertNext].timeStamp = time.Now()
	ig.rx2ig.entries[ig.rx2ig.insertNext].checksum = int(ax25_dedupe_crc(pp))

	if ig.debugLevel >= 3 {
		var src = ax25_get_addr_with_ssid(pp, AX25_SOURCE)
		var dest = ax25_get_addr_with_ssid(pp, AX25_DESTINATION)
		var pinfo = AX25GetInfo(pp)

		text_color_set(DW_COLOR_DEBUG)
		dw_printf("rx_to_ig_remember [%d] = %s %d \"%s>%s:%s\"\n",
			ig.rx2ig.insertNext,
			ig.rx2ig.entries[ig.rx2ig.insertNext].timeStamp.String(),
			ig.rx2ig.entries[ig.rx2ig.insertNext].checksum,
			src, dest, string(pinfo))
	}

	ig.rx2ig.insertNext++
	if ig.rx2ig.insertNext >= RX2IG_HISTORY_MAX {
		ig.rx2ig.insertNext = 0
	}
}

func (ig *IGate) rxToIgAllow(pp *packet_t) bool {
	var crc = ax25_dedupe_crc(pp)
	var now = time.Now()

	if ig.debugLevel >= 2 {
		var src = ax25_get_addr_with_ssid(pp, AX25_SOURCE)
		var dest = ax25_get_addr_with_ssid(pp, AX25_DESTINATION)
		var pinfo = AX25GetInfo(pp)

		text_color_set(DW_COLOR_DEBUG)
		dw_printf("rx_to_ig_allow? %d \"%s>%s:%s\"\n", crc, src, dest, string(pinfo))
	}

	// Do we have duplicate checking at all in the RF>IS direction?

	if ig.config.rx2ig_dedupe_time == 0 {
		if ig.debugLevel >= 2 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("rx_to_ig_allow? YES, no dedupe checking\n")
		}

		return true
	}

	// Yes, check for duplicates within certain time.

	for j := range RX2IG_HISTORY_MAX {
		if ig.rx2ig.entries[j].checksum == int(crc) && !ig.rx2ig.entries[j].timeStamp.Before(now.Add(-time.Duration(ig.config.rx2ig_dedupe_time)*time.Second)) {
			if ig.debugLevel >= 2 {
				text_color_set(DW_COLOR_DEBUG)
				// could be multiple entries and this might not be the most recent.
				dw_printf("rx_to_ig_allow? NO. Seen %d seconds ago.\n", int(time.Since(ig.rx2ig.entries[j].timeStamp).Seconds()))
			}

			return false
		}
	}

	if ig.debugLevel >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("rx_to_ig_allow? YES\n")
	}

	return true
} /* end rxToIgAllow */

/*-------------------------------------------------------------------
 *
 * Name:        igToTxRemember
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
 * Name:	igToTxAllow
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
 *		igToTxRemember must be called for every packet, from the IGate
 *		server, sent to the radio transmitter.
 *
 *		igToTxAllow decides whether this should be allowed thru
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

The CRC differs because sendRecPacket makes a private copy of the packet and
calls ax25_cut_at_crlf on the copy before rxToIgRemember computes the checksum,
so that path sees info without the CR.  The digipeater receives the original packet
(CR still present) and dedupe_remember -> igToTxRemember -> ax25_dedupe_crc sees
the CR.  At the time this log was captured, ax25_dedupe_crc did not strip trailing
CR/LF/space, so the two paths produced different checksums for what looks like the
same content.  ax25_dedupe_crc now strips trailing CR, LF, and space before hashing,
so the checksums agree and cross-suppression between the Rx IGate and the digipeater
works correctly.

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

// ig2txEntry is one packet that went out over the air.  channel is which
// radio channel it went out on - duplicate detection is separate for each -
// and bydigi says whether the digipeater sent it rather than the IGate, which
// matters because the transmit rate limits are the IGate's alone.
type ig2txEntry struct {
	timeStamp time.Time
	checksum  int
	channel   int
	bydigi    int
}

// ig2txHistory is a ring of the last IG2TX_HISTORY_MAX of those, oldest
// overwritten first.  mu guards the rest: the digipeater remembers into it from
// the receive thread, APRStt object reports from the audio thread that decoded
// them, and the IGate's APRS-IS to RF path remembers and consults it from its
// own goroutine.
type ig2txHistory struct {
	mu         sync.Mutex
	entries    [IG2TX_HISTORY_MAX]ig2txEntry
	insertNext int
}

func (h *ig2txHistory) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries = [IG2TX_HISTORY_MAX]ig2txEntry{}
	h.insertNext = 0

	for n := range h.entries {
		// Not a channel, so an empty slot is not a match for channel 0.
		h.entries[n].channel = 0xff
	}
}

func (ig *IGate) igToTxRemember(pp *packet_t, channel int, bydigi int) {
	var now = time.Now()
	var crc = ax25_dedupe_crc(pp)

	ig.ig2tx.mu.Lock()
	defer ig.ig2tx.mu.Unlock()

	if ig.debugLevel >= 3 {
		var src = ax25_get_addr_with_ssid(pp, AX25_SOURCE)
		var dest = ax25_get_addr_with_ssid(pp, AX25_DESTINATION)
		var pinfo = AX25GetInfo(pp)

		text_color_set(DW_COLOR_DEBUG)
		dw_printf("ig_to_tx_remember [%d] = ch%d d%d %s %d \"%s>%s:%s\"\n",
			ig.ig2tx.insertNext,
			channel, bydigi,
			now.String(), crc,
			src, dest, string(pinfo))
	}

	ig.ig2tx.entries[ig.ig2tx.insertNext].timeStamp = now
	ig.ig2tx.entries[ig.ig2tx.insertNext].checksum = int(crc)
	ig.ig2tx.entries[ig.ig2tx.insertNext].channel = channel
	ig.ig2tx.entries[ig.ig2tx.insertNext].bydigi = bydigi

	ig.ig2tx.insertNext++
	if ig.ig2tx.insertNext >= IG2TX_HISTORY_MAX {
		ig.ig2tx.insertNext = 0
	}
}

func (ig *IGate) igToTxAllow(pp *packet_t, channel int) bool {
	var crc = ax25_dedupe_crc(pp)
	var now = time.Now()

	var pinfo = AX25GetInfo(pp)

	if ig.debugLevel >= 2 {
		var src = ax25_get_addr_with_ssid(pp, AX25_SOURCE)
		var dest = ax25_get_addr_with_ssid(pp, AX25_DESTINATION)

		text_color_set(DW_COLOR_DEBUG)
		dw_printf("ig_to_tx_allow? ch%d %d \"%s>%s:%s\"\n", channel, crc, src, dest, string(pinfo))
	}

	ig.ig2tx.mu.Lock()
	defer ig.ig2tx.mu.Unlock()

	/* Consider transmissions on this channel only by either digi or IGate. */

	for j := range IG2TX_HISTORY_MAX {
		if ig.ig2tx.entries[j].checksum == int(crc) && ig.ig2tx.entries[j].channel == channel && !ig.ig2tx.entries[j].timeStamp.Before(now.Add(-IG2TX_DEDUPE_TIME)) {
			/* We have a duplicate within some time period. */
			if is_message_message(string(pinfo)) {
				/* I think I want to avoid the duplicate suppression for "messages." */
				/* Suppose we transmit a message from station X and it doesn't get an ack back. */
				/* Station X then sends exactly the same thing 20 seconds later.  */
				/* We don't want to suppress the retry. */
				if ig.debugLevel >= 2 {
					text_color_set(DW_COLOR_DEBUG)
					dw_printf("ig_to_tx_allow? Yes for duplicate message sent %d seconds ago. bydigi=%d\n", int(time.Since(ig.ig2tx.entries[j].timeStamp).Seconds()), ig.ig2tx.entries[j].bydigi)
				}
			} else {
				/* Normal (non-message) case. */
				if ig.debugLevel >= 2 {
					text_color_set(DW_COLOR_DEBUG)
					// could be multiple entries and this might not be the most recent.
					dw_printf("ig_to_tx_allow? NO. Duplicate sent %d seconds ago. bydigi=%d\n", int(time.Since(ig.ig2tx.entries[j].timeStamp).Seconds()), ig.ig2tx.entries[j].bydigi)
				}

				text_color_set(DW_COLOR_INFO)
				dw_printf("Tx IGate: Drop duplicate packet transmitted recently.\n")

				return false
			}
		}
	}

	/* IGate transmit counts must not include digipeater transmissions. */

	var count_1 = 0
	var count_5 = 0

	for j := range IG2TX_HISTORY_MAX {
		if ig.ig2tx.entries[j].channel == channel && ig.ig2tx.entries[j].bydigi == 0 {
			if !ig.ig2tx.entries[j].timeStamp.Before(time.Now().Add(-60 * time.Second)) {
				count_1++
			}

			if !ig.ig2tx.entries[j].timeStamp.Before(time.Now().Add(-300 * time.Second)) {
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

	var increase_limit = 1
	if is_message_message(string(pinfo)) {
		increase_limit = 3
	}

	if count_1 >= ig.config.tx_limit_1*increase_limit {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Tx IGate: Already transmitted maximum of %d packets in 1 minute.\n", ig.config.tx_limit_1)

		return false
	}

	if count_5 >= ig.config.tx_limit_5*increase_limit {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Tx IGate: Already transmitted maximum of %d packets in 5 minutes.\n", ig.config.tx_limit_5)

		return false
	}

	if ig.debugLevel >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("ig_to_tx_allow? YES\n")
	}

	return true
} /* end igToTxAllow */

/* end igate.c */
