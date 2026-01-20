package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Common code used by Serial port and network versions of KISS protocol.
 *
 * Description: The KISS TNC protocol is described in http://www.ka9q.net/papers/kiss.html
 *
 *		( An extended form, to handle multiple TNCs on a single serial port.
 *		  Not applicable for our situation.  http://he.fi/pub/oh7lzb/bpq/multi-kiss.pdf )
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
 *			* radio channel in upper nybble.
 *				(KISS doc uses "port" but I don't like that because it has too many meanings.)
 *			* command in lower nybble.
 *
 *
 *		Commands from application tp TNC:
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
 *			_5	FullDuplex	Full Duplex.  Transmit immediately without
 *						waiting for channel to be clear.
 *
 *			_6	SetHardware	TNC specific.
 *
 *			_C	XKISS extension - not supported.
 *			_E	XKISS extension - not supported.
 *
 *			FF	Return		Exit KISS mode.  Ignored.
 *
 *
 *		Messages sent to client application:
 *
 *			_0	Data Frame	Received AX.25 frame in raw format.
 *
 *			_6	SetHardware	TNC specific.
 *						Usually a response to a query.
 *
 *---------------------------------------------------------------*/

// #include "direwolf.h"
// #include <stdio.h>
// #include <unistd.h>
// #include <stdlib.h>
// #include <ctype.h>
// #include <assert.h>
// #include <string.h>
// void hex_dump (unsigned char *p, int len);
import "C"

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"unsafe"
)

const KISS_CMD_DATA_FRAME = 0
const KISS_CMD_TXDELAY = 1
const KISS_CMD_PERSISTENCE = 2
const KISS_CMD_SLOTTIME = 3
const KISS_CMD_TXTAIL = 4
const KISS_CMD_FULLDUPLEX = 5
const KISS_CMD_SET_HARDWARE = 6
const XKISS_CMD_DATA = 12 // Not supported. http://he.fi/pub/oh7lzb/bpq/multi-kiss.pdf
const XKISS_CMD_POLL = 14 // Not supported.
const KISS_CMD_END_KISS = 15

/*
 * Special characters used by SLIP protocol.
 */

const FEND = 0xC0
const FESC = 0xDB
const TFEND = 0xDC
const TFESC = 0xDD

type kiss_state_e int

const (
	KS_SEARCHING  kiss_state_e = 0 /* Looking for FEND to start KISS frame. Must be 0 so we can simply zero whole structure to initialize. */
	KS_COLLECTING kiss_state_e = 1 /* In process of collecting KISS frame. */
)

const MAX_KISS_LEN = 2048 /* Spec calls for at least 1024. */
/* Might want to make it longer to accommodate */
/* maximum packet length. */

const MAX_NOISE_LEN = 100

type kiss_frame_t struct {
	state kiss_state_e

	kiss_msg [MAX_KISS_LEN]C.uchar
	/* Leading FEND is optional. */
	/* Contains escapes and ending FEND. */
	kiss_len C.int

	noise     [MAX_NOISE_LEN]C.uchar
	noise_len C.int
}

type fromto_t int

const (
	FROM_CLIENT fromto_t = 0
	TO_CLIENT   fromto_t = 1
)

var FROMTO_PREFIX = map[fromto_t]string{
	FROM_CLIENT: "<<<",
	TO_CLIENT:   ">>>",
}

// This is used only for TCPKISS but it put in kissnet.h,
// there would be a circular dependency between the two header files.
// Each KISS TCP port has its own status block.

type kissport_status_s struct {
	pnext *kissport_status_s // To next in list.

	arg2 C.int // temp for passing second arg into
	// kissnet_listen_thread

	tcp_port C.int // default 8001

	channel C.int // Radio channel for this tcp port.
	// -1 for all.

	client_sock [MAX_NET_CLIENTS]net.Conn

	kf [MAX_NET_CLIENTS]*kiss_frame_t
	/* Accumulated KISS frame and state of decoder. */
}

var KISSUTIL = false // Dynamic replacement for the old #define

/*-------------------------------------------------------------------
 *
 * Name:        kiss_frame_init
 *
 * Purpose:     Save information about valid channels for later error checking.
 *
 * Inputs:      pa		- Address of structure of type audio_s.
 *
 *-----------------------------------------------------------------*/

func kiss_frame_init(pa *audio_s) {
	save_audio_config_p = pa
}

/*-------------------------------------------------------------------
 *
 * Name:        kiss_encapsulate
 *
 * Purpose:     Encapsulate a frame into KISS format.
 *
 * Inputs:	in	- Address of input block.
 *			  First byte is the "type indicator" with type and
 *			  channel but we don't care about that here.
 *			  If it happens to be FEND or FESC, it is escaped, like any other byte.
 *
 *			  This seems cumbersome and confusing to have this
 *			  one byte offset when encapsulating an AX.25 frame.
 *			  Maybe the type/channel byte should be passed in
 *			  as a separate argument.
 *
 *			  Note that this is "binary" data and can contain
 *			  nul (0x00) values.   Don't treat it like a text string!
 *
 *		ilen	- Number of bytes in input block.
 *
 * Outputs:	out	- Address where to place the KISS encoded representation.
 *			  The sequence is:
 *				FEND		- Magic frame separator.
 *				data		- with certain byte values replaced so
 *						  FEND will never occur here.
 *				FEND		- Magic frame separator.
 *
 * Returns:	Number of bytes in the output.
 *		Absolute max length (extremely unlikely) will be twice input plus 2.
 *
 *-----------------------------------------------------------------*/

func kiss_encapsulate(in []byte) []byte {
	var buf bytes.Buffer

	buf.WriteByte(FEND)

	for _, b := range in {
		switch b {
		case FEND:
			buf.WriteByte(FESC)
			buf.WriteByte(TFEND)
		case FESC:
			buf.WriteByte(FESC)
			buf.WriteByte(TFESC)
		default:
			buf.WriteByte(b)
		}
	}

	buf.WriteByte(FEND)

	return buf.Bytes()
}

/*-------------------------------------------------------------------
 *
 * Name:        kiss_unwrap
 *
 * Purpose:     Extract original data from a KISS frame.
 *
 * Inputs:	in	- Address of the received the KISS encoded representation.
 *			  The sequence is:
 *				FEND		- Magic frame separator, optional.
 *				data		- with certain byte values replaced so
 *						  FEND will never occur here.
 *				FEND		- Magic frame separator.
 *		ilen	- Number of bytes in input block.
 *
 * Inputs:	out	- Where to put the resulting frame without
 *			  the escapes or FEND.
 *			  First byte is the "type indicator" with type and
 *			  channel but we don't care about that here.
 *			  We treat it like any other byte with special handling
 *			  if it happens to be FESC.
 *			  Note that this is "binary" data and can contain
 *			  nul (0x00) values.   Don't treat it like a text string!
 *
 * Returns:	Number of bytes in the output.
 *
 *-----------------------------------------------------------------*/

func kiss_unwrap(in []byte) []byte {

	if len(in) < 2 {
		/* Need at least the "type indicator" byte and FEND. */
		/* Probably more. */
		text_color_set(DW_COLOR_ERROR)
		dw_printf("KISS message less than minimum length.\n")
		return []byte{}
	}

	if in[len(in)-1] == FEND {
		in = in[:len(in)-1] // Ignore last FEND
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("KISS frame should end with FEND.\n")
	}

	if in[0] == FEND {
		in = in[1:] // Skip over optional leading FEND
	}

	var escapedMode = false
	var buf bytes.Buffer
	for _, b := range in {
		if b == FEND {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("KISS frame should not have FEND in the middle.\n")
		}

		if escapedMode {
			switch b {
			case TFESC:
				buf.WriteByte(FESC)
			case TFEND:
				buf.WriteByte(FEND)
			default:
				text_color_set(DW_COLOR_ERROR)
				dw_printf("KISS protocol error.  Found 0x%02x after FESC.\n", b)
			}
			escapedMode = false
		} else if b == FESC {
			escapedMode = true
		} else {
			buf.WriteByte(b)
		}
	}

	return buf.Bytes()
} /* end kiss_unwrap */

/*-------------------------------------------------------------------
 *
 * Name:        kiss_debug_print
 *
 * Purpose:     Print message to/from client for debugging.
 *
 * Inputs:	fromto		- Direction of message.
 *		special		- Comment if not a KISS frame.
 *		pmsg		- Address of the message block.
 *		msg_len		- Length of the message.
 *
 *--------------------------------------------------------------------*/

func kiss_debug_print(fromto fromto_t, special string, pmsg []byte) {
	var direction = []string{"from", "to"}
	var prefix = []string{"<<<", ">>>"}
	var function = []string{
		"Data frame", "TXDELAY", "P", "SlotTime",
		"TXtail", "FullDuplex", "SetHardware", "Invalid 7",
		"Invalid 8", "Invalid 9", "Invalid 10", "Invalid 11",
		"Invalid 12", "Invalid 13", "Invalid 14", "Return"}

	text_color_set(DW_COLOR_DEBUG)

	if KISSUTIL {
		dw_printf("From KISS TNC:\n")
	} else {
		dw_printf("\n")
		if special == "" {

			if pmsg[0] == FEND {
				/* Skip over FEND if present. */
				pmsg = pmsg[1:]
			}

			dw_printf("%s %s %s KISS client application, channel %d, total length = %d\n",
				prefix[fromto], function[pmsg[0]&0xf], direction[fromto],
				(pmsg[0]>>4)&0xf, len(pmsg))
		} else {
			dw_printf("%s %s %s KISS client application, total length = %d\n",
				prefix[fromto], special, direction[fromto],
				len(pmsg))
		}
	}

	C.hex_dump((*C.uchar)(C.CBytes(pmsg)), C.int(len(pmsg)))
}

/*-------------------------------------------------------------------
 *
 * Name:        kiss_rec_byte
 *
 * Purpose:     Process one byte from a KISS client app.
 *
 * Inputs:	kf	- Current state of building a frame.
 *		ch	- A byte from the input stream.
 *		debug	- Activates debug output.
 *		kps	- KISS TCP port status block.
 *			  nil for pseudo terminal and serial port.
 *		client	- Client app number for TCP KISS.
 *		          Ignored for pseudo termal and serial port.
 *		sendfun	- Function to send something to the client application.
 *
 * Outputs:	kf	- Current state is updated.
 *
 * Returns:	none.
 *
 *-----------------------------------------------------------------*/

/*
 * Application might send some commands to put TNC into KISS mode.
 * For example, APRSIS32 sends something like:
 *
 *	<0x0d>
 *	<0x0d>
 *	XFLOW OFF<0x0d>
 *	FULLDUP OFF<0x0d>
 *	KISS ON<0x0d>
 *	RESTART<0x0d>
 *	<0x03><0x03><0x03>
 *	TC 1<0x0d>
 *	TN 2,0<0x0d><0x0d><0x0d>
 *	XFLOW OFF<0x0d>
 *	FULLDUP OFF<0x0d>
 *	KISS ON<0x0d>
 *	RESTART<0x0d>
 *
 * This keeps repeating over and over and over and over again if
 * it doesn't get any sort of response.
 *
 * Let's try to keep it happy by sending back a command prompt.
 */

type kiss_sendfun func(C.int, C.int, []byte, C.int, *kissport_status_s, C.int)

func kiss_rec_byte(kf *kiss_frame_t, ch C.uchar, debug C.int,
	kps *kissport_status_s, client C.int,
	sendfun kiss_sendfun) {
	// dw_printf ("kiss_frame ( %c %02x ) \n", ch, ch);

	switch kf.state {
	case KS_SEARCHING: /* Searching for starting FEND. */
		// TODO KG Also default: ?
		if ch == FEND {
			/* Start of frame.  But first print any collected noise for debugging. */

			if kf.noise_len > 0 {
				if debug > 0 {
					kiss_debug_print(FROM_CLIENT, "Rejected Noise", C.GoBytes(unsafe.Pointer(&kf.noise[0]), kf.noise_len))
				}
				kf.noise_len = 0
			}

			kf.kiss_len = 1
			kf.kiss_msg[0] = ch
			kf.state = KS_COLLECTING
			return
		}

		/* Noise to be rejected. */

		if kf.noise_len < MAX_NOISE_LEN {
			kf.noise[kf.noise_len] = ch
			kf.noise_len++
		}
		if ch == '\r' {
			if debug > 0 {
				kiss_debug_print(FROM_CLIENT, "Rejected Noise", C.GoBytes(unsafe.Pointer(&kf.noise[0]), kf.noise_len))
				kf.noise[kf.noise_len] = 0
			}

			/* Try to appease client app by sending something back. */
			if strings.EqualFold("restart\r", C.GoString((*C.char)(unsafe.Pointer(&kf.noise[0])))) ||
				strings.EqualFold("reset\r", C.GoString((*C.char)(unsafe.Pointer(&kf.noise[0])))) {
				// first 2 parameters don't matter when length is -1 indicating text.
				sendfun(0, 0, []byte("\xc0\xc0"), -1, kps, client)
			} else {
				sendfun(0, 0, []byte("\r\ncmd:"), -1, kps, client)
			}
			kf.noise_len = 0
		}
		return

	case KS_COLLECTING: /* Frame collection in progress. */
		if ch == FEND {
			/* End of frame. */

			if kf.kiss_len == 0 {
				/* Empty frame.  Starting a new one. */
				kf.kiss_msg[kf.kiss_len] = ch
				kf.kiss_len++
				return
			}
			if kf.kiss_len == 1 && kf.kiss_msg[0] == FEND {
				/* Empty frame.  Just go on collecting. */
				return
			}

			kf.kiss_msg[kf.kiss_len] = ch
			kf.kiss_len++
			if debug > 0 {
				/* As received over the wire from client app. */
				kiss_debug_print(FROM_CLIENT, "", C.GoBytes(unsafe.Pointer(&kf.kiss_msg[0]), kf.kiss_len))
			}

			var unwrapped = kiss_unwrap(C.GoBytes(unsafe.Pointer(&kf.kiss_msg[0]), kf.kiss_len))
			var ulen = len(unwrapped)

			if debug >= 2 {
				/* Append CRC to this and it goes out over the radio. */
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("\n")
				dw_printf("Packet content after removing KISS framing and any escapes:\n")
				/* Don't include the "type" indicator. */
				/* It contains the radio channel and type should always be 0 here. */
				C.hex_dump((*C.uchar)(C.CBytes(unwrapped[1:])), C.int(len(unwrapped)-1))
			}

			kiss_process_msg((*C.uchar)(C.CBytes(unwrapped)), C.int(ulen), debug, kps, client, sendfun)

			kf.state = KS_SEARCHING
			return
		}

		if kf.kiss_len < MAX_KISS_LEN {
			kf.kiss_msg[kf.kiss_len] = ch
			kf.kiss_len++
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("KISS message exceeded maximum length.\n")
		}
		return
	}
} /* end kiss_rec_byte */

/*-------------------------------------------------------------------
 *
 * Name:        kiss_process_msg
 *
 * Purpose:     Process a message from the KISS client.
 *
 * Inputs:	kiss_msg	- Kiss frame with FEND and escapes removed.
 *				  The first byte contains channel and command.
 *
 *		kiss_len	- Number of bytes including the command.
 *
 *		debug		- Debug option is selected.
 *
 *		kps		- Used only for TCP KISS.
 *				  Should be nil for pseudo terminal and serial port.
 *
 *		client		- Client app number for TCP KISS.
 *				  Should be -1 for pseudo termal and serial port.
 *
 *		sendfun		- Function to send something to the client application.
 *				  "Set Hardware" can send a response.
 *
 *-----------------------------------------------------------------*/

// This is used only by the TNC side.

func kiss_process_msg(kiss_msg *C.uchar, kiss_len C.int, debug C.int, kps *kissport_status_s, client C.int, sendfun kiss_sendfun) {
	// Temporary for now
	if KISSUTIL {
		kiss_process_msg_override(kiss_msg, kiss_len)
		return
	}

	var kiss_msg_bytes = C.GoBytes(unsafe.Pointer(kiss_msg), kiss_len)

	// New in 1.7:
	// We can have KISS TCP ports which convey only a single radio channel.
	// This is to allow operation by applications which only know how to talk to single radio TNCs.

	var channel C.int
	if kps != nil && kps.channel != -1 {
		// Ignore channel from KISS and substitute radio channel for that KISS TCP port.
		channel = kps.channel
	} else {
		// Normal case of getting radio channel from the KISS frame.
		channel = C.int(kiss_msg_bytes[0]>>4) & 0xf
	}

	var alevel alevel_t
	var cmd = kiss_msg_bytes[0] & 0xf

	switch cmd {
	case KISS_CMD_DATA_FRAME: /* 0 = Data Frame */

		// kissnet_copy clobbers first byte but we don't care
		// because we have already determined channel and command.

		kissnet_copy(kiss_msg, kiss_len, channel, C.int(cmd), kps, client)

		/* Note July 2017: There is a variant of of KISS, called SMACK, that assumes */
		/* a TNC can never have more than 8 channels.  http://symek.de/g/smack.html */
		/* It uses the MSB to indicate that a checksum is added.  I wonder if this */
		/* is why we sometimes hear about a request to transmit on channel 8.  */
		/* Should we have a message that asks the user if SMACK is being used, */
		/* and if so, turn it off in the application configuration? */
		/* Our current default is a maximum of 6 channels but it is easily */
		/* increased by changing one number and recompiling. */

		// Additional information, from Mike Playle, December 2018, for Issue #42
		//
		//	I came across this the other day with Xastir, and took a quick look.
		//	The problem is fixable without the kiss_frame.c hack, which doesn't help with Xastir anyway.
		//
		//	Workaround
		//
		//	After the kissattach command, put the interface into CRC mode "none" with a command like this:
		//
		//	# kissparms -c 1 -p radio
		//
		//	Analysis
		//
		//	The source of this behaviour is the kernel's KISS implementation:
		//
		//	https://elixir.bootlin.com/linux/v4.9/source/drivers/net/hamradio/mkiss.c#L489
		//
		//	It defaults to starting in state CRC_MODE_SMACK_TEST and ending up in mode CRC_NONE
		//	after the first two packets, which have their framing byte modified by this code in the process.
		//	It looks to me like deliberate behaviour on the kernel's part.
		//
		//	Setting the CRC mode explicitly before sending any packets stops this state machine from running.
		//
		//	Is this a bug? I don't know - that's up to you! Maybe it would make sense for Direwolf to set
		//	the CRC mode itself, or to expect this behaviour and ignore these flags on the first packets
		//	received from the Linux pty.
		//
		//	This workaround seems sound to me, though, so perhaps this is just a documentation issue.

		// Would it make sense to implement SMACK?  I don't think so.
		// Adding a checksum to the KISS data offers no benefit because it is very reliable.
		// It violates the original protocol specification which states that 16 radio channels are possible.
		// (Some times the term 'port' is used but I try to use 'channel' all the time because 'port'
		// has too many other meanings. Serial port, TCP port, ...)
		// SMACK imposes a limit of 8.  That limit might have been OK back in 1991 but not now.
		// There are people using more than 8 radio channels (using SDR not traditional radios) with direwolf.

		/* Verify that the radio channel number is valid. */
		/* Any sort of medium should be OK here. */

		if (channel < 0 || channel >= MAX_TOTAL_CHANS || save_audio_config_p.chan_medium[channel] == MEDIUM_NONE) && save_audio_config_p.chan_medium[channel] != MEDIUM_IGATE {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid transmit channel %d from KISS client app.\n", channel)
			dw_printf("\n")
			dw_printf("Are you using AX.25 for Linux?  It might be trying to use a modified\n")
			dw_printf("version of KISS which uses the channel field differently than the\n")
			dw_printf("original KISS protocol specification.  The solution might be to use\n")
			dw_printf("a command like \"kissparms -c 1 -p radio\" to set CRC none mode.\n")
			dw_printf("Another way of doing this is pre-loading the \"kiss\" kernel module with CRC disabled:\n")
			dw_printf("sudo /sbin/modprobe -q mkiss crc_force=1\n")

			dw_printf("\n")
			text_color_set(DW_COLOR_DEBUG)
			kiss_debug_print(FROM_CLIENT, "", C.GoBytes(unsafe.Pointer(kiss_msg), kiss_len))
			return
		}

		alevel = alevel_t{} //nolint:exhaustruct

		var pp = ax25_from_frame((*C.uchar)(C.CBytes(kiss_msg_bytes[1:])), kiss_len-1, alevel)
		if pp == nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("ERROR - Invalid KISS data frame from client app.\n")
		} else {
			/* How can we determine if it is an original or repeated message? */
			/* If there is at least one digipeater in the frame, AND */
			/* that digipeater has been used, it should go out quickly thru */
			/* the high priority queue. */
			/* Otherwise, it is an original for the low priority queue. */

			if ax25_get_num_repeaters(pp) >= 1 &&
				ax25_get_h(pp, AX25_REPEATER_1) > 0 {
				tq_append(channel, TQ_PRIO_0_HI, pp)
			} else {
				tq_append(channel, TQ_PRIO_1_LO, pp)
			}
		}

	case KISS_CMD_TXDELAY: /* 1 = TXDELAY */

		if kiss_len < 2 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("KISS ERROR: Missing value for TXDELAY command.\n")
			return
		}
		text_color_set(DW_COLOR_INFO)
		dw_printf("KISS protocol set TXDELAY = %d (*10mS units = %d mS), channel %d\n", kiss_msg_bytes[1], kiss_msg_bytes[1]*10, channel)
		if kiss_msg_bytes[1] < 10 || kiss_msg_bytes[1] >= 100 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Are you sure you want such an extreme value for TXDELAY?\n")
			dw_printf("Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n")
			dw_printf("section, to understand what this means.\n")
		}
		xmit_set_txdelay(channel, C.int(kiss_msg_bytes[1]))

	case KISS_CMD_PERSISTENCE: /* 2 = Persistence */

		if kiss_len < 2 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("KISS ERROR: Missing value for PERSISTENCE command.\n")
			return
		}
		text_color_set(DW_COLOR_INFO)
		dw_printf("KISS protocol set Persistence = %d, channel %d\n", kiss_msg_bytes[1], channel)
		if kiss_msg_bytes[1] < 5 || kiss_msg_bytes[1] > 250 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Are you sure you want such an extreme value for PERSIST?\n")
			dw_printf("Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n")
			dw_printf("section, to understand what this means.\n")
		}
		xmit_set_persist(channel, C.int(kiss_msg_bytes[1]))

	case KISS_CMD_SLOTTIME: /* 3 = SlotTime */

		if kiss_len < 2 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("KISS ERROR: Missing value for SLOTTIME command.\n")
			return
		}
		text_color_set(DW_COLOR_INFO)
		dw_printf("KISS protocol set SlotTime = %d (*10mS units = %d mS), channel %d\n", kiss_msg_bytes[1], kiss_msg_bytes[1]*10, channel)
		if kiss_msg_bytes[1] < 2 || kiss_msg_bytes[1] > 50 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Are you sure you want such an extreme value for SLOTTIME?\n")
			dw_printf("Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n")
			dw_printf("section, to understand what this means.\n")
		}
		xmit_set_slottime(channel, C.int(kiss_msg_bytes[1]))

	case KISS_CMD_TXTAIL: /* 4 = TXtail */
		if kiss_len < 2 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("KISS ERROR: Missing value for TXTAIL command.\n")
			return
		}

		text_color_set(DW_COLOR_INFO)
		dw_printf("KISS protocol set TXtail = %d (*10mS units = %d mS), channel %d\n", kiss_msg_bytes[1], kiss_msg_bytes[1]*10, channel)

		if kiss_msg_bytes[1] < 5 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Setting TXTAIL so low is asking for trouble.  You probably don't want to do this.\n")
			dw_printf("Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n")
			dw_printf("section, to understand what this means.\n")
		}

		xmit_set_txtail(channel, C.int(kiss_msg_bytes[1]))

	case KISS_CMD_FULLDUPLEX: /* 5 = FullDuplex */
		if kiss_len < 2 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("KISS ERROR: Missing value for FULLDUPLEX command.\n")
			return
		}
		text_color_set(DW_COLOR_INFO)
		dw_printf("KISS protocol set FullDuplex = %d, channel %d\n", kiss_msg_bytes[1], channel)
		xmit_set_fulldup(channel, C.int(kiss_msg_bytes[1]))

	case KISS_CMD_SET_HARDWARE: /* 6 = TNC specific */

		if kiss_len < 2 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("KISS ERROR: Missing value for SET HARDWARE command.\n")
			return
		}
		text_color_set(DW_COLOR_INFO)
		dw_printf("KISS protocol set hardware \"%s\", channel %d\n", kiss_msg_bytes[1:], channel)
		kiss_set_hardware(channel, kiss_msg_bytes[1:], debug, kps, client, sendfun)

	case KISS_CMD_END_KISS: /* 15 = End KISS mode, channel should be 15. */
		/* Ignore it. */
		text_color_set(DW_COLOR_INFO)
		dw_printf("KISS protocol end KISS mode - Ignored.\n")

	default:
		text_color_set(DW_COLOR_ERROR)
		dw_printf("KISS Invalid command %d\n", cmd)
		kiss_debug_print(FROM_CLIENT, "", C.GoBytes(unsafe.Pointer(kiss_msg), kiss_len))

		text_color_set(DW_COLOR_INFO)
		dw_printf("Troubleshooting tip:\n")
		dw_printf("Use \"-d kn\" option on direwolf command line to observe\n")
		dw_printf("all communication with the client application.\n")

		if cmd == XKISS_CMD_DATA || cmd == XKISS_CMD_POLL {
			dw_printf("\n")
			dw_printf("It looks like you are trying to use the \"XKISS\" protocol which is not supported.\n")
			dw_printf("Change your application settings to use standard \"KISS\" rather than some other variant.\n")
			dw_printf("If you are using Winlink Express, configure like this:\n")
			dw_printf("    Packet TNC Type:  KISS\n")
			dw_printf("    Packet TNC Model:  NORMAL      -- Using ACKMODE will cause this error.\n")
			dw_printf("\n")
		}
	}
} /* end kiss_process_msg */

/*-------------------------------------------------------------------
 *
 * Name:        kiss_set_hardware
 *
 * Purpose:     Process the "set hardware" command.
 *
 * Inputs:	channel		- channel, 0 - 15.
 *
 *		command		- All but the first byte.  e.g.  "TXBUF:99"
 *				  Case sensitive.
 *				  Will be modified so be sure caller doesn't care.
 *
 *		debug		- debug level.
 *
 *		client		- Client app number for TCP KISS.
 *				  Needed so we can send any response to the right client app.
 *				  Ignored for pseudo terminal and serial port.
 *
 *		sendfun		- Function to send something to the client application.
 *
 *				  This is the tricky part.  We can have any combination of
 *				  serial port, pseudo terminal, and multiple TCP clients.
 *				  We need to send the response to same place where query came
 *				  from.  The function is different for each class of device
 *				  and we need a client number for the TCP case because we
 *				  can have multiple TCP KISS clients at the same time.
 *
 *
 * Description:	This is new in version 1.5.  "Set hardware" was previously ignored.
 *
 *		There are times when the client app might want to send configuration
 *		commands, such as modem speed, to the KISS TNC or inquire about its
 *		current state.
 *
 *		The immediate motivation for adding this is that one application wants
 *		to know how many frames are currently in the transmit queue.  This can
 *		be used for throttling of large transmissions and performing some action
 *		after the last frame has been sent.
 *
 *		The original KISS protocol spec offers no guidance on what "Set Hardware" might look
 *		like.  I'm aware of only two, drastically different, implementations:
 *
 *		fldigi - http://www.w1hkj.com/FldigiHelp-3.22/kiss_command_page.html
 *
 *			Everything is in human readable in both directions:
 *
 *			COMMAND: [ parameter [ , parameter ... ] ]
 *
 *			Lack of a parameter, in the client to TNC direction, is a query
 *			which should generate a response in the same format.
 *
 *		    Used by applications, http://www.w1hkj.com/FldigiHelp/kiss_host_prgs_page.html
 *			- BPQ32
 *			- UIChar
 *			- YAAC
 *
 *		mobilinkd - https://raw.githubusercontent.com/mobilinkd/tnc1/tnc2/bertos/net/kiss.c
 *
 *			Single byte with the command / response code, followed by
 *			zero or more value bytes.
 *
 *		    Used by applications:
 *			- APRSdroid
 *
 *		It would be beneficial to adopt one of them rather than doing something
 *		completely different.  It might even be possible to recognize both.
 *		This might allow leveraging of other existing applications.
 *
 *		Let's start with the easy to understand human readable format.
 *
 * Commands:	(Client to TNC, with parameter(s) to set something.)
 *
 *			none yet
 *
 * Queries:	(Client to TNC, no parameters, generate a response.)
 *
 *			Query		Response		Comment
 *			-----		--------		-------
 *
 *			TNC:		TNC:DIREWOLF 9.9	9.9 represents current version.
 *
 *			TXBUF:		TXBUF:999		Number of bytes (not frames) in transmit queue.
 *
 *--------------------------------------------------------------------*/

func kiss_set_hardware(channel C.int, command []byte, debug C.int, kps *kissport_status_s, client C.int, sendfun kiss_sendfun) {
	var cmd, value, found = bytes.Cut(command, []byte{':'})

	if found {
		if bytes.Equal(cmd, []byte("TNC")) { /* TNC - Identify software version. */
			if len(value) > 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("KISS Set Hardware TNC: Did not expect a parameter.\n")
			}

			var response = fmt.Sprintf("DIREWOLF %d.%d", C.MAJOR_VERSION, C.MINOR_VERSION)
			sendfun(channel, KISS_CMD_SET_HARDWARE, []byte(response), C.int(len(response)), kps, client)
		} else if bytes.Equal(cmd, []byte("TXBUF")) { /* TXBUF - Number of bytes in transmit queue. */
			if len(value) > 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("KISS Set Hardware TXBUF: Did not expect a parameter.\n")
			}

			var n = tq_count(channel, -1, C.CString(""), C.CString(""), 1)
			var response = fmt.Sprintf("TXBUF:%d", n)
			sendfun(channel, KISS_CMD_SET_HARDWARE, []byte(response), C.int(len(response)), kps, client)
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("KISS Set Hardware unrecognized command: %s.\n", cmd)
		}
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("KISS Set Hardware \"%s\" expected the form COMMAND:[parameter[,parameter...]]\n", command)
	}
} /* end kiss_set_hardware */
