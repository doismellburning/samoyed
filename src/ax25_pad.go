package direwolf

/*------------------------------------------------------------------
 *
 * Name:	ax25_pad
 *
 * Purpose:	Packet assembler and disasembler.
 *
 *		This was written when I was only concerned about APRS which
 *		uses only UI frames.  ax25_pad2.c, added years later, has
 *		functions for dealing with other types of frames.
 *
 *   		We can obtain AX.25 packets from different sources:
 *
 *		(a) from an HDLC frame.
 *		(b) from text representation.
 *		(c) built up piece by piece.
 *
 *		We also want to use a packet in different ways:
 *
 *		(a) transmit as an HDLC frame.
 *		(b) print in human-readable text.
 *		(c) take it apart piece by piece.
 *
 *		Looking at the more general case, we also want to modify
 *		an existing packet.  For instance an APRS repeater might
 *		want to change "WIDE2-2" to "WIDE2-1" and retransmit it.
 *
 *
 * Description:
 *
 *
 *	APRS uses only UI frames.
 *	Each starts with 2-10 addresses (14-70 octets):
 *
 *	* Destination Address  (note: opposite order in printed format)
 *
 *	* Source Address
 *
 *	* 0-8 Digipeater Addresses  (Could there ever be more as a result of
 *					digipeaters inserting their own call for
 *					the tracing feature?
 *					NO.  The limit is 8 when transmitting AX.25 over the
 *					radio.
 *					Communication with an IGate server could
 *					have a longer VIA path but that is only in text form,
 *					not as an AX.25 frame.)
 *
 *	Each address is composed of:
 *
 *	* 6 upper case letters or digits, blank padded.
 *		These are shifted left one bit, leaving the LSB always 0.
 *
 *	* a 7th octet containing the SSID and flags.
 *		The LSB is always 0 except for the last octet of the address field.
 *
 *	The final octet of the Destination has the form:
 *
 *		C R R SSID 0, where,
 *
 *			C = command/response = 1
 *			R R = Reserved = 1 1
 *			SSID = substation ID
 *			0 = zero
 *
 *		The AX.25 spec states that the RR bits should be 11 if not used.
 *		There are a couple documents talking about possible uses for APRS.
 *		I'm ignoring them for now.
 *		http://www.aprs.org/aprs12/preemptive-digipeating.txt
 *		http://www.aprs.org/aprs12/RR-bits.txt
 *
 *		I don't recall why I originally set the source & destination C bits both to 1.
 *		Reviewing this 5 years later, after spending more time delving into the
 *		AX.25 spec, I think it should be 1 for destination and 0 for source.
 *		In practice you see all four combinations being used by APRS stations
 *		and everyone apparently ignores them for APRS.  They do make a big
 *		difference for connected mode.
 *
 *	The final octet of the Source has the form:
 *
 *		C R R SSID 0, where,
 *
 *			C = command/response = 0
 *			R R = Reserved = 1 1
 *			SSID = substation ID
 *			0 = zero (or 1 if no repeaters)
 *
 *	The final octet of each repeater has the form:
 *
 *		H R R SSID 0, where,
 *
 *			H = has-been-repeated = 0 initially.
 *				Set to 1 after this address has been used.
 *			R R = Reserved = 1 1
 *			SSID = substation ID
 *			0 = zero (or 1 if last repeater in list)
 *
 *		A digipeater would repeat this frame if it finds its address
 *		with the "H" bit set to 0 and all earlier repeater addresses
 *		have the "H" bit set to 1.
 *		The "H" bit would be set to 1 in the repeated frame.
 *
 *	In standard monitoring format, an asterisk is displayed after the last
 *	digipeater with the "H" bit set.  That indicates who you are hearing
 *	over the radio.
 *	(That is if digipeaters update the via path properly.  Some don't so
 *	we don't know who we are hearing.  This is discussed in the User Guide.)
 *	No asterisk means the source is being heard directly.
 *
 *	Example, if we can hear all stations involved,
 *
 *		SRC>DST,RPT1,RPT2,RPT3:		-- we heard SRC
 *		SRC>DST,RPT1*,RPT2,RPT3:	-- we heard RPT1
 *		SRC>DST,RPT1,RPT2*,RPT3:	-- we heard RPT2
 *		SRC>DST,RPT1,RPT2,RPT3*:	-- we heard RPT3
 *
 *
 *	Next we have:
 *
 *	* One byte Control Field 	- APRS uses 3 for UI frame
 *					   The more general AX.25 frame can have two.
 *
 *	* One byte Protocol ID 		- APRS uses 0xf0 for no layer 3
 *
 *	Finally the Information Field of 1-256 bytes.
 *
 *	And, of course, the 2 byte CRC.
 *
 * 	The descriptions above, for the C, H, and RR bits, are for APRS usage.
 *	When operating as a KISS TNC we just pass everything along and don't
 *	interpret or change them.
 *
 *
 * Constructors: ax25_init		- Clear everything.
 *		ax25_from_text		- Tear apart a text string
 *		ax25_from_frame		- Tear apart an AX.25 frame.
 *					  Must be called before any other function.
 *
 * Get methods:	....			- Extract destination, source, or digipeater
 *					  address from frame.
 *
 * Assumptions:	CRC has already been verified to be correct.
 *
 *------------------------------------------------------------------*/

// #define AX25_PAD_C		/* this will affect behavior of ax25_pad.h */
// #include "direwolf.h"
// #include <stdlib.h>
// #include <string.h>
// #include <stdio.h>
// #include <ctype.h>
// #include "regex.h"
// #include "ax25_pad.h"
// #include "fcs_calc.h"
// void hex_dump (unsigned char *p, int len);
import "C"

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unsafe"
)

type ax25_frame_type_t C.ax25_frame_type_t

/*
 * Accumulate statistics.
 * If ax25_new_count gets much larger than ax25_delete_count plus the size of
 * the transmit queue we have a memory leak.
 */

var ax25_new_count = 0
var ax25_delete_count = 0
var last_seq_num C.int = 0

// Runtime replacement for DECAMAIN define
var DECODE_APRS_UTIL = false

const MAGIC = C.MAGIC

func CLEAR_LAST_ADDR_FLAG(this_p C.packet_t) {
	this_p.frame_data[this_p.num_addr*7-1] &= ^(C.uchar(C.SSID_LAST_MASK))
}

func SET_LAST_ADDR_FLAG(this_p C.packet_t) {
	this_p.frame_data[this_p.num_addr*7-1] |= C.SSID_LAST_MASK
}

func isxdigit(b byte) bool {
	return slices.Contains([]byte("0123456789abcdefABCDEF"), b)
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_new
 *
 * Purpose:	Allocate memory for a new packet object.
 *
 * Returns:	Identifier for a new packet object.
 *		In the current implementation this happens to be a pointer.
 *
 *------------------------------------------------------------------------------*/

func ax25_new() C.packet_t {

	/* TODO KG
	#if DEBUG
	        text_color_set(DW_COLOR_DEBUG);
	        dw_printf ("ax25_new(): before alloc, new=%d, delete=%d\n", ax25_new_count, ax25_delete_count);
	#endif
	*/

	last_seq_num++
	ax25_new_count++

	/*
	 * check for memory leak.
	 */

	// version 1.4 push up the threshold.   We could have considerably more with connected mode.

	//if (ax25_new_count > ax25_delete_count + 100) {
	if ax25_new_count > ax25_delete_count+256 {

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Report to WB2OSZ - Memory leak for packet objects.  new=%d, delete=%d\n", ax25_new_count, ax25_delete_count)
	}

	var this_p = (C.packet_t)(C.calloc(C.sizeof_struct_packet_s, 1))

	Assert(this_p != nil)

	this_p.magic1 = MAGIC
	this_p.seq = last_seq_num
	this_p.magic2 = MAGIC
	this_p.num_addr = (-1)

	return (this_p)
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_delete
 *
 * Purpose:	Destroy a packet object, freeing up memory it was using.
 *
 *------------------------------------------------------------------------------*/

func ax25_delete(this_p C.packet_t) {
	/* TODO KG
	#if DEBUG
	        text_color_set(DW_COLOR_DEBUG);
	        dw_printf ("ax25_delete(): before free, new=%d, delete=%d\n", ax25_new_count, ax25_delete_count);
	#endif
	*/

	if this_p == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR - nil pointer passed to ax25_delete.\n")
		return
	}

	ax25_delete_count++

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	this_p.magic1 = 0
	this_p.magic1 = 0

	//memset (this_p, 0, sizeof (struct packet_s));
	C.free(unsafe.Pointer(this_p))
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_from_text
 *
 * Purpose:	Parse a frame in human-readable monitoring format and change
 *		to internal representation.
 *
 * Input:	monitor	- "TNC-2" monitor format for packet.  i.e.
 *				source>dest[,repeater1,repeater2,...]:information
 *
 *			The information part can have non-printable characters
 *			in the form of <0xff>.  This will be converted to single
 *			bytes.  e.g.  <0x0d> is carriage return.
 *			In version 1.4H we will allow nul characters which means
 *			we have to maintain a length rather than using strlen().
 *			I maintain that it violates the spec but want to handle it
 *			because it does happen and we want to preserve it when
 *			acting as an IGate rather than corrupting it.
 *
 *		strict	- True to enforce rules for packets sent over the air.
 *			  False to be more lenient for packets from IGate server.
 *
 *			  Packets from an IGate server can have longer
 *		 	  addresses after qAC.  Up to 9 observed so far.
 *			  The SSID can be 2 alphanumeric characters, not just 1 to 15.
 *
 *			  We can just truncate the name because we will only
 *			  end up discarding it.    TODO:  check on this.  WRONG! FIXME
 *
 * Returns:	Pointer to new packet object in the current implementation.
 *
 * Outputs:	Use the "get" functions to retrieve information in different ways.
 *
 * Evolution:	Originally this was written to handle only valid RF packets.
 *		There are other places where the rules are not as strict.
 *		Using decode_aprs with raw data seen on aprs.fi.  e.g.
 *			EL-CA2JOT>RXTLM-1,TCPIP,qAR,CA2JOT::EL-CA2JOT:UNIT....
 *			EA4YR>APBM1S,TCPIP*,qAS,BM2142POS:@162124z...
 *		* Source addr might not comply to RF format.
 *		* The q-construct has lower case.
 *		* Tier-2 server name might not comply to RF format.
 *		We have the same issue with the encapsulated part of a third-party packet.
 *			WB2OSZ-5>APDW17,WIDE1-1,WIDE2-1:}WHO-IS>APJIW4,TCPIP,WB2OSZ-5*::WB2OSZ-7 :ack0
 *
 *		We need a way to keep and retrieve the original name.
 *		This gets a little messy because the packet object is in the on air frame format.
 *
 *------------------------------------------------------------------------------*/

func ax25_from_text(monitor *C.char, strict C.int) C.packet_t {

	/*
	 * Tearing it apart is destructive so make our own copy first.
	 */

	// text_color_set(DW_COLOR_DEBUG);
	// dw_printf ("DEBUG: ax25_from_text ('%s', %d)\n", monitor, strict);
	// fflush(stdout); sleep(1);

	var this_p = ax25_new()

	/* Is it possible to have a nul character (zero byte) in the */
	/* information field of an AX.25 frame? */
	/* At this point, we have a normal C string. */
	/* It is possible that will convert <0x00> to a nul character later. */
	/* There we need to maintain a separate length and not use normal C string functions. */

	var stuff = C.GoBytes(unsafe.Pointer(monitor), C.int(C.strlen(monitor)))

	/*
	 * Initialize the packet structure with two addresses and control/pid
	 * for APRS.
	 */
	C.memset(unsafe.Pointer(&this_p.frame_data[AX25_DESTINATION*7]), ' '<<1, 6)
	this_p.frame_data[AX25_DESTINATION*7+6] = C.SSID_H_MASK | C.SSID_RR_MASK

	C.memset(unsafe.Pointer(&this_p.frame_data[AX25_SOURCE*7]), ' '<<1, 6)
	this_p.frame_data[AX25_SOURCE*7+6] = C.SSID_RR_MASK | C.SSID_LAST_MASK

	this_p.frame_data[14] = C.AX25_UI_FRAME
	this_p.frame_data[15] = C.AX25_PID_NO_LAYER_3

	this_p.frame_len = 7 + 7 + 1 + 1
	this_p.num_addr = (-1)
	ax25_get_num_addr(this_p) // when num_addr is -1, this sets it properly.
	Assert(this_p.num_addr == 2)

	/*
	 * Separate the addresses from the rest.
	 */
	var pinfo []byte
	var colonFound bool
	stuff, pinfo, colonFound = bytes.Cut(stuff, []byte{':'})

	if !colonFound {
		ax25_delete(this_p)
		return (nil)
	}

	/*
	 * Separate the addresses.
	 * Note that source and destination order is swappped.
	 */

	/*
	 * Source address.
	 */

	var pa []byte
	var found bool

	pa, stuff, found = bytes.Cut(stuff, []byte{'>'})
	if !found {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Failed to create packet from text.  No source address\n")
		ax25_delete(this_p)
		return (nil)
	}

	var ssid_temp, heard_temp C.int
	var atemp [AX25_MAX_ADDR_LEN]C.char

	if ax25_parse_addr(AX25_SOURCE, C.CString(string(pa)), strict, &atemp[0], &ssid_temp, &heard_temp) == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Failed to create packet from text.  Bad source address\n")
		ax25_delete(this_p)
		return (nil)
	}

	ax25_set_addr(this_p, AX25_SOURCE, &atemp[0])
	ax25_set_h(this_p, AX25_SOURCE) // c/r in this position
	ax25_set_ssid(this_p, AX25_SOURCE, ssid_temp)

	/*
	 * Destination address.
	 */

	pa, stuff, _ = bytes.Cut(stuff, []byte{','})
	// Note: if no comma found, pa contains the destination and stuff is empty (no digipeaters)

	if ax25_parse_addr(AX25_DESTINATION, C.CString(string(pa)), strict, &atemp[0], &ssid_temp, &heard_temp) == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Failed to create packet from text.  Bad destination address\n")
		ax25_delete(this_p)
		return (nil)
	}

	ax25_set_addr(this_p, AX25_DESTINATION, &atemp[0])
	ax25_set_h(this_p, AX25_DESTINATION) // c/r in this position
	ax25_set_ssid(this_p, AX25_DESTINATION, ssid_temp)

	/*
	 * VIA path.
	 */

	// Originally this used strtok_r.
	// strtok considers all adjacent delimiters to be a single delimiter.
	// This is handy for varying amounts of whitespace.
	// It will never return a zero length string.
	// All was good until this bizarre case came along:

	//	AISAT-1>CQ,,::CQ-0     :From  AMSAT INDIA & Exseed Space |114304|48|45|42{962

	// Apparently there are two digipeater fields but they are empty.
	// When we parsed this text representation, the extra commas were ignored rather
	// than pointed out as being invalid.

	// Use strsep instead.  This does not collapse adjacent delimiters.

	for len(stuff) > 0 && this_p.num_addr < AX25_MAX_ADDRS {
		pa, stuff, found = bytes.Cut(stuff, []byte{','})

		var k = this_p.num_addr

		// printf ("DEBUG: get digi loop, num addr = %d, address = '%s'\n", k, pa);// FIXME

		// Hack for q construct, from APRS-IS, so it does not cause panic later.

		if strict == 0 && len(pa) >= 3 && pa[0] == 'q' && pa[1] == 'A' {
			pa[0] = 'Q'
			pa[2] = byte(unicode.ToUpper(rune(pa[2])))
		}

		if ax25_parse_addr(k, C.CString(string(pa)), strict, &atemp[0], &ssid_temp, &heard_temp) == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Failed to create packet from text.  Bad digipeater address\n")
			ax25_delete(this_p)
			return (nil)
		}

		ax25_set_addr(this_p, k, &atemp[0])
		ax25_set_ssid(this_p, k, ssid_temp)

		// Does it have an "*" at the end?
		// TODO: Complain if more than one "*".
		// Could also check for all has been repeated bits are adjacent.

		if heard_temp != 0 {
			for ; k >= AX25_REPEATER_1; k-- {
				ax25_set_h(this_p, k)
			}
		}

		// If no comma was found, this was the last digipeater
		if !found {
			break
		}
	}

	/*
	 * Finally, process the information part.
	 *
	 * Translate hexadecimal values like <0xff> to single bytes.
	 * MIC-E format uses 5 different non-printing characters.
	 * We might want to manually generate UTF-8 characters such as degree.
	 */

	//#define DEBUG14H 1

	/*
	   #if DEBUG14H
	   	text_color_set(DW_COLOR_DEBUG);
	   	dw_printf ("BEFORE: %s\nSAFE:   ", pinfo);
	   	ax25_safe_print (pinfo, -1, 0);
	   	dw_printf ("\n");
	   #endif
	*/

	var info_len = 0
	var info_part [AX25_MAX_INFO_LEN + 1]C.char
	for len(pinfo) > 0 && info_len < AX25_MAX_INFO_LEN {
		if len(pinfo) >= 6 &&
			pinfo[0] == '<' &&
			pinfo[1] == '0' &&
			pinfo[2] == 'x' &&
			isxdigit(pinfo[3]) &&
			isxdigit(pinfo[4]) &&
			pinfo[5] == '>' {

			var hexVal, _ = strconv.ParseInt(string(pinfo[3:5]), 16, 64)
			info_part[info_len] = C.char(hexVal)
			info_len++
			pinfo = pinfo[6:]
		} else {
			info_part[info_len] = C.char(pinfo[0])
			info_len++
			pinfo = pinfo[1:]
		}
	}
	info_part[info_len] = 0

	/*
		#if DEBUG14H
			text_color_set(DW_COLOR_DEBUG);
			dw_printf ("AFTER:  %s\nSAFE:   ", info_part);
			ax25_safe_print (info_part, info_len, 0);
			dw_printf ("\n");
		#endif
	*/

	/*
	 * Append the info part.
	 */
	C.memcpy(unsafe.Add(unsafe.Pointer(&this_p.frame_data[0]), this_p.frame_len), unsafe.Pointer(&info_part[0]), C.size_t(info_len))
	this_p.frame_len += C.int(info_len)

	return (this_p)
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_from_frame
 *
 * Purpose:	Split apart an HDLC frame to components.
 *
 * Inputs:	fbuf	- Pointer to beginning of frame.
 *
 *		flen	- Length excluding the two FCS bytes.
 *
 *		alevel	- Audio level of received signal.
 *			  Maximum range 0 - 100.
 *			  -1 might be used when not applicable.
 *
 * Returns:	Pointer to new packet object or nil if error.
 *
 * Outputs:	Use the "get" functions to retrieve information in different ways.
 *
 *------------------------------------------------------------------------------*/

func ax25_from_frame(fbuf *C.uchar, flen C.int, alevel C.alevel_t) C.packet_t {

	/*
	 * First make sure we have an acceptable length:
	 *
	 *	We are not concerned with the FCS (CRC) because someone else checked it.
	 *
	 * Is is possible to have zero length for info?
	 *
	 * In the original version, assuming APRS, the answer was no.
	 * We always had at least 3 octets after the address part:
	 * control, protocol, and first byte of info part for data type.
	 *
	 * In later versions, this restriction was relaxed so other
	 * variations of AX.25 could be used.  Now the minimum length
	 * is 7+7 for addresses plus 1 for control.
	 *
	 */

	if flen < C.AX25_MIN_PACKET_LEN || flen > C.AX25_MAX_PACKET_LEN {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Frame length %d not in allowable range of %d to %d.\n", flen, C.AX25_MIN_PACKET_LEN, C.AX25_MAX_PACKET_LEN)
		return (nil)
	}

	var this_p = ax25_new()

	/* Copy the whole thing intact. */

	C.memcpy(unsafe.Pointer(&this_p.frame_data[0]), unsafe.Pointer(fbuf), C.size_t(flen))
	this_p.frame_data[flen] = 0
	this_p.frame_len = flen

	/* Find number of addresses. */

	this_p.num_addr = (-1)
	ax25_get_num_addr(this_p)

	return (this_p)
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_dup
 *
 * Purpose:	Make a copy of given packet object.
 *
 * Inputs:	copy_from	- Existing packet object.
 *
 * Returns:	Pointer to new packet object or nil if error.
 *
 *
 *------------------------------------------------------------------------------*/

func ax25_dup(copy_from C.packet_t) C.packet_t {

	var this_p = ax25_new()
	Assert(this_p != nil)

	var save_seq = this_p.seq

	C.memcpy(unsafe.Pointer(this_p), unsafe.Pointer(copy_from), C.sizeof_struct_packet_s)
	this_p.seq = save_seq

	return (this_p)

}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_parse_addr
 *
 * Purpose:	Parse address with optional ssid.
 *
 * Inputs:	position	- AX25_DESTINATION, AX25_SOURCE, AX25_REPEATER_1...
 *				  Used for more specific error message.  -1 if not used.
 *
 *		in_addr		- Input such as "WB2OSZ-15*"
 *
 * 		strict		- 1 (true) for strict checking (6 characters, no lower case,
 *				  SSID must be in range of 0 to 15).
 *				  Strict is appropriate for packets sent
 *				  over the radio.  Communication with IGate
 *				  allows lower case (e.g. "qAR") and two
 *				  alphanumeric characters for the SSID.
 *				  We also get messages like this from a server.
 *					KB1POR>APU25N,TCPIP*,qAC,T2NUENGLD:...
 *					K1BOS-B>APOSB,TCPIP,WR2X-2*:...
 *
 *				  2 (extra true) will complain if * is found at end.
 *
 * Outputs:	out_addr	- Address without any SSID.
 *				  Must be at least AX25_MAX_ADDR_LEN bytes.
 *
 *		out_ssid	- Numeric value of SSID.
 *
 *		out_heard	- True if "*" found.
 *
 * Returns:	True (1) if OK, false (0) if any error.
 *		When 0, out_addr, out_ssid, and out_heard are undefined.
 *
 *
 *------------------------------------------------------------------------------*/

var position_name = [1 + AX25_MAX_ADDRS]string{
	"", "Destination ", "Source ",
	"Digi1 ", "Digi2 ", "Digi3 ", "Digi4 ",
	"Digi5 ", "Digi6 ", "Digi7 ", "Digi8 "}

//export ax25_parse_addr
func ax25_parse_addr(position C.int, _in_addr *C.char, strict C.int, _out_addr *C.char, out_ssid *C.int, out_heard *C.int) C.int {

	var in_addr = C.GoString(_in_addr)

	*_out_addr = 0
	*out_ssid = 0
	*out_heard = 0

	// dw_printf ("ax25_parse_addr in: position=%d, '%s', strict=%d\n", position, in_addr, strict);

	if position < -1 {
		position = -1
	}
	if position > C.AX25_REPEATER_8 {
		position = C.AX25_REPEATER_8
	}
	position++ /* Adjust for position_name above. */

	if len(in_addr) == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("%sAddress \"%s\" is empty.\n", position_name[position], in_addr)
		return 0
	}

	if strict != 0 && len(in_addr) >= 2 && strings.HasPrefix(in_addr, "qA") {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("%sAddress \"%s\" is a \"q-construct\" used for communicating with\n", position_name[position], in_addr)
		dw_printf("APRS Internet Servers.  It should never appear when going over the radio.\n")
	}

	// dw_printf ("ax25_parse_addr in: %s\n", in_addr);

	var maxlen = IfThenElse(strict != 0, 6, (AX25_MAX_ADDR_LEN - 1))
	var out_addr string
	for i, p := range in_addr {
		if p == '-' || p == '*' {
			break
		}

		if i >= maxlen {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("%sAddress is too long. \"%s\" has more than %d characters.\n", position_name[position], in_addr, maxlen)
			return 0
		}

		if !unicode.IsLetter(p) && !unicode.IsNumber(p) {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("%sAddress, \"%s\" contains character other than letter or digit in character position %d.\n", position_name[position], in_addr, i)
			return 0
		}

		out_addr += string(p)

		if DECODE_APRS_UTIL {
			// Hack when running in decode_aprs utility
			// Exempt the "qA..." case because it was already mentioned.

			if strict != 0 && unicode.IsLower(p) && !strings.HasPrefix(in_addr, "qA") {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("%sAddress has lower case letters. \"%s\" must be all upper case.\n", position_name[position], in_addr)
			}
		} else {
			if strict != 0 && unicode.IsLower(p) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("%sAddress has lower case letters. \"%s\" must be all upper case.\n", position_name[position], in_addr)
				return 0
			}
		}
	}
	C.strcpy(_out_addr, C.CString(out_addr))

	// Chomp
	in_addr = in_addr[len(out_addr):]

	var sstr string
	if len(in_addr) > 0 && in_addr[0] == '-' {
		in_addr = in_addr[1:]
		for i, p := range in_addr {
			if !unicode.IsLetter(p) && !unicode.IsNumber(p) {
				break
			}
			if i >= 2 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("%sSSID is too long. SSID part of \"%s\" has more than 2 characters.\n", position_name[position], in_addr)
				return 0
			}
			sstr += string(p)
			if strict != 0 && !unicode.IsDigit(p) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("%sSSID must be digits. \"%s\" has letters in SSID.\n", position_name[position], in_addr)
				return 0
			}
		}
		var k, kErr = strconv.Atoi(sstr)
		if kErr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("%sMalformed SSID: \"%s\" could not be parsed.\n", position_name[position], in_addr)
			return 0
		}
		if k < 0 || k > 15 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("%sSSID out of range. SSID of \"%s\" not in range of 0 to 15.\n", position_name[position], in_addr)
			return 0
		}
		*out_ssid = C.int(k)

		// Chomp
		in_addr = in_addr[len(sstr):]
	}

	if len(in_addr) > 0 && in_addr[0] == '*' {
		*out_heard = 1
		if strict == 2 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("\"*\" is not allowed at end of address \"%s\" here.\n", in_addr)
			return 0
		}
		in_addr = in_addr[1:]
	}

	if len(in_addr) != 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid character \"%c\" found in %saddress \"%s\".\n", in_addr[0], position_name[position], in_addr)
		return 0
	}

	// dw_printf ("ax25_parse_addr out: '%s' %d %d\n", out_addr, *out_ssid, *out_heard);

	return (1)

} /* end ax25_parse_addr */

/*-------------------------------------------------------------------
 *
 * Name:        ax25_check_addresses
 *
 * Purpose:     Check addresses of given packet and print message if any issues.
 *		We call this when receiving and transmitting.
 *
 * Inputs:	pp	- packet object pointer.
 *
 * Errors:	Print error message.
 *
 * Returns:	1 for all valid.  0 if not.
 *
 * Examples:	I was surprised to get this from an APRS-IS server with
 *		a lower case source address.
 *
 *			n1otx>APRS,TCPIP*,qAC,THIRD:@141335z4227.48N/07111.73W_348/005g014t044r000p000h60b10075.wview_5_20_2
 *
 *		I haven't gotten to the bottom of this yet but it sounds
 *		like "q constructs" are somehow getting on to the air when
 *		they should only appear in conversations with IGate servers.
 *
 *			https://groups.yahoo.com/neo/groups/direwolf_packet/conversations/topics/678
 *
 *			WB0VGI-7>APDW12,W0YC-5*,qAR,AE0RF-10:}N0DZQ-10>APWW10,TCPIP,WB0VGI-7*:;145.230MN*080306z4607.62N/09230.58WrKE0ACL/R 145.230- T146.2 (Pine County ARES)
 *
 * Typical result:
 *
 *			Digipeater WIDE2 (probably N3LEE-4) audio level = 28(10/6)   [NONE]   __|||||||
 *			[0.5] VE2DJE-9>P_0_P?,VE2PCQ-3,K1DF-7,N3LEE-4,WIDE2*:'{S+l <0x1c>>/
 *			Invalid character "_" in MIC-E destination/latitude.
 *			Invalid character "_" in MIC-E destination/latitude.
 *			Invalid character "?" in MIC-E destination/latitude.
 *			Invalid MIC-E N/S encoding in 4th character of destination.
 *			Invalid MIC-E E/W encoding in 6th character of destination.
 *			MIC-E, normal car (side view), Unknown manufacturer, Returning
 *			N 00 00.0000, E 005 55.1500, 0 MPH
 *			Invalid character "_" found in Destination address "P_0_P?".
 *
 *			*** The origin and journey of this packet should receive some scrutiny. ***
 *
 *--------------------------------------------------------------------*/

func ax25_check_addresses(pp C.packet_t) C.int {

	var all_ok = true
	for n := C.int(0); n < ax25_get_num_addr(pp); n++ {
		var addr [AX25_MAX_ADDR_LEN]C.char
		ax25_get_addr_with_ssid(pp, n, &addr[0])

		var ignore1 [AX25_MAX_ADDR_LEN]C.char
		var ignore2, ignore3 C.int
		all_ok = all_ok && (ax25_parse_addr(n, &addr[0], 1, &ignore1[0], &ignore2, &ignore3) != 0)
	}

	if !all_ok {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("\n")
		dw_printf("*** The origin and journey of this packet should receive some scrutiny. ***\n")
		dw_printf("\n")
	}

	return C.int(IfThenElse(all_ok, 1, 0))
} /* end ax25_check_addresses */

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_unwrap_third_party
 *
 * Purpose:	Unwrap a third party message from the header.
 *
 * Inputs:	copy_from	- Existing packet object.
 *
 * Returns:	Pointer to new packet object or nil if error.
 *
 * Example:	Input:		A>B,C:}D>E,F:info
 *		Output:		D>E,F:info
 *
 *------------------------------------------------------------------------------*/

func ax25_unwrap_third_party(from_pp C.packet_t) C.packet_t {

	if ax25_get_dti(from_pp) != '}' {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error: ax25_unwrap_third_party: wrong data type.\n")
		return (nil)
	}

	var info_p *C.uchar
	ax25_get_info(from_pp, &info_p)

	// Want strict because addresses should conform to AX.25 here.
	// That's not the case for something from an Internet Server.

	var result_pp = ax25_from_text((*C.char)(unsafe.Add(unsafe.Pointer(info_p), 1)), 1)

	return (result_pp)
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_set_addr
 *
 * Purpose:	Add or change an address.
 *
 * Inputs:	n	- Index of address.   Use the symbols
 *			  AX25_DESTINATION, AX25_SOURCE, AX25_REPEATER1, etc.
 *
 *			  Must be either an existing address or one greater
 *			  than the final which causes a new one to be added.
 *
 *		ad	- Address with optional dash and substation id.
 *
 * Assumption:	ax25_from_text or ax25_from_frame was called first.
 *
 * TODO:  	ax25_from_text could use this.
 *
 * Returns:	None.
 *
 *------------------------------------------------------------------------------*/

func ax25_set_addr(this_p C.packet_t, n C.int, ad *C.char) {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)
	Assert(n >= 0 && n < AX25_MAX_ADDRS)

	//dw_printf ("ax25_set_addr (%d, %s) num_addr=%d\n", n, ad, this_p.num_addr);

	if C.strlen(ad) == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Set address error!  Station address for position %d is empty!\n", n)
	}

	if n >= 0 && n < this_p.num_addr {

		//dw_printf ("ax25_set_addr , existing case\n");
		/*
		 * Set existing address position.
		 */

		// Why aren't we setting 'strict' here?
		// Messages from IGate have q-constructs.
		// We use this to parse it and later remove unwanted parts.

		var atemp [AX25_MAX_ADDR_LEN]C.char
		var ssid_temp, heard_temp C.int
		ax25_parse_addr(n, ad, 0, &atemp[0], &ssid_temp, &heard_temp)

		C.memset(unsafe.Pointer(&this_p.frame_data[n*7]), ' '<<1, 6)

		for i := C.int(0); i < 6 && atemp[i] != 0; i++ {
			this_p.frame_data[n*7+i] = C.uchar(atemp[i] << 1)
		}
		ax25_set_ssid(this_p, n, ssid_temp)
	} else if n == this_p.num_addr {

		//dw_printf ("ax25_set_addr , appending case\n");
		/*
		 * One beyond last position, process as insert.
		 */

		ax25_insert_addr(this_p, n, ad)
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error, ax25_set_addr, bad position %d for '%s'\n", n, C.GoString(ad))
	}

	//dw_printf ("------\n");
	//dw_printf ("dump after ax25_set_addr (%d, %s)\n", n, ad);
	//ax25_hex_dump (this_p);
	//dw_printf ("------\n");
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_insert_addr
 *
 * Purpose:	Insert address at specified position, shifting others up one
 *		position.
 *		This is used when a digipeater wants to insert its own call
 *		for tracing purposes.
 *		For example:
 *			W1ABC>TEST,WIDE3-3
 *		Would become:
 *			W1ABC>TEST,WB2OSZ-1*,WIDE3-2
 *
 * Inputs:	n	- Index of address.   Use the symbols
 *			  AX25_DESTINATION, AX25_SOURCE, AX25_REPEATER1, etc.
 *
 *		ad	- Address with optional dash and substation id.
 *
 * Bugs:	Little validity or bounds checking is performed.  Be careful.
 *
 * Assumption:	ax25_from_text or ax25_from_frame was called first.
 *
 * Returns:	None.
 *
 *
 *------------------------------------------------------------------------------*/

func ax25_insert_addr(this_p C.packet_t, n C.int, ad *C.char) {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)
	Assert(n >= AX25_REPEATER_1 && n < AX25_MAX_ADDRS)

	//dw_printf ("ax25_insert_addr (%d, %s)\n", n, ad);

	if C.strlen(ad) == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Set address error!  Station address for position %d is empty!\n", n)
	}

	/* Don't do it if we already have the maximum number. */
	/* Should probably return success/fail code but currently the caller doesn't care. */

	if this_p.num_addr >= AX25_MAX_ADDRS {
		return
	}

	CLEAR_LAST_ADDR_FLAG(this_p)

	this_p.num_addr++

	C.memmove(unsafe.Pointer(&this_p.frame_data[(n+1)*7]), unsafe.Pointer(&this_p.frame_data[n*7]), C.size_t(this_p.frame_len-(n*7)))
	C.memset(unsafe.Pointer(&this_p.frame_data[n*7]), ' '<<1, 6)
	this_p.frame_len += 7
	this_p.frame_data[n*7+6] = C.SSID_RR_MASK

	SET_LAST_ADDR_FLAG(this_p)

	// Why aren't we setting 'strict' here?
	// Messages from IGate have q-constructs.
	// We use this to parse it and later remove unwanted parts.

	var atemp [AX25_MAX_ADDR_LEN]C.char
	var ssid_temp, heard_temp C.int
	ax25_parse_addr(n, ad, 0, &atemp[0], &ssid_temp, &heard_temp)
	C.memset(unsafe.Pointer(&this_p.frame_data[n*7]), ' '<<1, 6)
	for i := C.int(0); i < 6 && atemp[i] != 0; i++ {
		this_p.frame_data[n*7+i] = C.uchar(atemp[i] << 1)
	}

	ax25_set_ssid(this_p, n, ssid_temp)

	// Sanity check after messing with number of addresses.

	var expect = this_p.num_addr
	this_p.num_addr = (-1)
	if expect != ax25_get_num_addr(this_p) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error ax25_remove_addr expect %d, actual %d\n", expect, this_p.num_addr)
	}
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_remove_addr
 *
 * Purpose:	Remove address at specified position, shifting others down one position.
 *		This is used when we want to remove something from the digipeater list.
 *
 * Inputs:	n	- Index of address.   Use the symbols
 *			  AX25_REPEATER1, AX25_REPEATER2, etc.
 *
 * Bugs:	Little validity or bounds checking is performed.  Be careful.
 *
 * Assumption:	ax25_from_text or ax25_from_frame was called first.
 *
 * Returns:	None.
 *
 *
 *------------------------------------------------------------------------------*/

func ax25_remove_addr(this_p C.packet_t, n C.int) {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)
	Assert(n >= AX25_REPEATER_1 && n < AX25_MAX_ADDRS)

	/* Shift those beyond to fill this position. */

	CLEAR_LAST_ADDR_FLAG(this_p)

	this_p.num_addr--

	C.memmove(unsafe.Pointer(&this_p.frame_data[n*7]), unsafe.Pointer(&this_p.frame_data[(n+1)*7]), C.size_t(this_p.frame_len-((n+1)*7)))
	this_p.frame_len -= 7
	SET_LAST_ADDR_FLAG(this_p)

	// Sanity check after messing with number of addresses.

	var expect = this_p.num_addr
	this_p.num_addr = (-1)
	if expect != ax25_get_num_addr(this_p) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error ax25_remove_addr expect %d, actual %d\n", expect, this_p.num_addr)
	}

}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_get_num_addr
 *
 * Purpose:	Return number of addresses in current packet.
 *
 * Assumption:	ax25_from_text or ax25_from_frame was called first.
 *
 * Returns:	Number of addresses in the current packet.
 *		Should be in the range of 2 .. AX25_MAX_ADDRS.
 *
 * Version 0.9:	Could be zero for a non AX.25 frame in KISS mode.
 *
 *------------------------------------------------------------------------------*/

func ax25_get_num_addr(this_p C.packet_t) C.int {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	/* Use cached value if already set. */

	if this_p.num_addr >= 0 {
		return (this_p.num_addr)
	}

	/* Otherwise, determine the number ofaddresses. */

	this_p.num_addr = 0 /* Number of addresses extracted. */

	var addr_bytes C.int = 0
	for a := C.int(0); a < this_p.frame_len && addr_bytes == 0; a++ {
		if this_p.frame_data[a]&C.SSID_LAST_MASK != 0 {
			addr_bytes = a + 1
		}
	}

	if addr_bytes%7 == 0 {
		var addrs = addr_bytes / 7
		if addrs >= C.AX25_MIN_ADDRS && addrs <= AX25_MAX_ADDRS {
			this_p.num_addr = addrs
		}
	}

	return (this_p.num_addr)
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_get_num_repeaters
 *
 * Purpose:	Return number of repeater addresses in current packet.
 *
 * Assumption:	ax25_from_text or ax25_from_frame was called first.
 *
 * Returns:	Number of addresses in the current packet - 2.
 *		Should be in the range of 0 .. AX25_MAX_ADDRS - 2.
 *
 *------------------------------------------------------------------------------*/

func ax25_get_num_repeaters(this_p C.packet_t) C.int {
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	if this_p.num_addr >= 2 {
		return (this_p.num_addr - 2)
	}

	return (0)
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_get_addr_with_ssid
 *
 * Purpose:	Return specified address with any SSID in current packet.
 *
 * Inputs:	n	- Index of address.   Use the symbols
 *			  AX25_DESTINATION, AX25_SOURCE, AX25_REPEATER1, etc.
 *
 * Outputs:	station - String representation of the station, including the SSID.
 *			e.g.  "WB2OSZ-15"
 *			  Usually variables will be AX25_MAX_ADDR_LEN bytes
 *			  but 10 would be adequate.
 *
 * Bugs:	No bounds checking is performed.  Be careful.
 *
 * Assumption:	ax25_from_text or ax25_from_frame was called first.
 *
 * Returns:	Character string in usual human readable format,
 *
 *
 *------------------------------------------------------------------------------*/

func ax25_get_addr_with_ssid(this_p C.packet_t, n C.int, _station *C.char) {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	if n < 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error detected in ax25_get_addr_with_ssid.\n")
		dw_printf("Address index, %d, is less than zero.\n", n)
		C.strcpy(_station, C.CString("??????"))
		return
	}

	if n >= this_p.num_addr {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error detected in ax25_get_addr_with_ssid.\n")
		dw_printf("Address index, %d, is too large for number of addresses, %d.\n", n, this_p.num_addr)
		C.strcpy(_station, C.CString("??????"))
		return
	}

	// At one time this would stop at the first space, on the assumption we would have only trailing spaces.
	// Then there was a forum discussion where someone encountered the address " WIDE2" with a leading space.
	// In that case, we would have returned a zero length string here.
	// Now we return exactly what is in the address field and trim trailing spaces.
	// This will provide better information for troubleshooting.

	var station string
	for i := C.int(0); i < 6; i++ {
		station += string((this_p.frame_data[n*7+i] >> 1) & 0x7f)
	}

	if strings.Contains(station, "\000") {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Station address \"%s\" contains nul character.  AX.25 protocol requires trailing ASCII spaces when less than 6 characters.\n", station)
	}

	station = strings.TrimRight(station, " ")

	if len(station) == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Station address, in position %d, is empty!  This is not a valid AX.25 frame.\n", n)
	}

	var ssid = ax25_get_ssid(this_p, n)
	if ssid != 0 {
		station += fmt.Sprintf("-%d", ssid)
	}

	C.strcpy(_station, C.CString(station))
} /* end ax25_get_addr_with_ssid */

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_get_addr_no_ssid
 *
 * Purpose:	Return specified address WITHOUT any SSID.
 *
 * Inputs:	n	- Index of address.   Use the symbols
 *			  AX25_DESTINATION, AX25_SOURCE, AX25_REPEATER1, etc.
 *
 * Outputs:	station - String representation of the station, WITHOUT the SSID.
 *			e.g.  "WB2OSZ"
 *			  Usually variables will be AX25_MAX_ADDR_LEN bytes
 *			  but 7 would be adequate.
 *
 * Bugs:	No bounds checking is performed.  Be careful.
 *
 * Assumption:	ax25_from_text or ax25_from_frame was called first.
 *
 * Returns:	Character string in usual human readable format,
 *
 *
 *------------------------------------------------------------------------------*/

func ax25_get_addr_no_ssid(this_p C.packet_t, n C.int, _station *C.char) {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	if n < 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error detected in ax25_get_addr_no_ssid.\n")
		dw_printf("Address index, %d, is less than zero.\n", n)
		C.strcpy(_station, C.CString("??????"))
		return
	}

	if n >= this_p.num_addr {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error detected in ax25_get_no_with_ssid.\n")
		dw_printf("Address index, %d, is too large for number of addresses, %d.\n", n, this_p.num_addr)
		C.strcpy(_station, C.CString("??????"))
		return
	}

	// At one time this would stop at the first space, on the assumption we would have only trailing spaces.
	// Then there was a forum discussion where someone encountered the address " WIDE2" with a leading space.
	// In that case, we would have returned a zero length string here.
	// Now we return exactly what is in the address field and trim trailing spaces.
	// This will provide better information for troubleshooting.

	var station string
	for i := C.int(0); i < 6; i++ {
		station += string((this_p.frame_data[n*7+i] >> 1) & 0x7f)
	}

	station = strings.TrimRight(station, " ")

	if len(station) == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Station address, in position %d, is empty!  This is not a valid AX.25 frame.\n", n)
	}

	C.strcpy(_station, C.CString(station))

} /* end ax25_get_addr_no_ssid */

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_get_ssid
 *
 * Purpose:	Return SSID of specified address in current packet.
 *
 * Inputs:	n	- Index of address.   Use the symbols
 *			  AX25_DESTINATION, AX25_SOURCE, AX25_REPEATER1, etc.
 *
 * Assumption:	ax25_from_text or ax25_from_frame was called first.
 *
 * Returns:	Substation id, as integer 0 .. 15.
 *
 *------------------------------------------------------------------------------*/

func ax25_get_ssid(this_p C.packet_t, n C.int) C.int {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	if n >= 0 && n < this_p.num_addr {
		return C.int((this_p.frame_data[n*7+6] & C.SSID_SSID_MASK) >> C.SSID_SSID_SHIFT)
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error: ax25_get_ssid(%d), num_addr=%d\n", n, this_p.num_addr)
		return (0)
	}
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_set_ssid
 *
 * Purpose:	Set the SSID of specified address in current packet.
 *
 * Inputs:	n	- Index of address.   Use the symbols
 *			  AX25_DESTINATION, AX25_SOURCE, AX25_REPEATER1, etc.
 *
 *		ssid	- New SSID.  Must be in range of 0 to 15.
 *
 * Assumption:	ax25_from_text or ax25_from_frame was called first.
 *
 * Bugs:	Rewrite to keep call and SSID separate internally.
 *
 *------------------------------------------------------------------------------*/

func ax25_set_ssid(this_p C.packet_t, n C.int, ssid C.int) {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	if n >= 0 && n < this_p.num_addr {
		this_p.frame_data[n*7+6] = (this_p.frame_data[n*7+6] & ^(C.uchar(C.SSID_SSID_MASK))) |
			C.uchar((ssid<<C.SSID_SSID_SHIFT)&C.SSID_SSID_MASK)
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error: ax25_set_ssid(%d,%d), num_addr=%d\n", n, ssid, this_p.num_addr)
	}
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_get_h
 *
 * Purpose:	Return "has been repeated" flag of specified address in current packet.
 *
 * Inputs:	n	- Index of address.   Use the symbols
 *			  AX25_DESTINATION, AX25_SOURCE, AX25_REPEATER1, etc.
 *
 * Bugs:	No bounds checking is performed.  Be careful.
 *
 * Assumption:	ax25_from_text or ax25_from_frame was called first.
 *
 * Returns:	True or false.
 *
 *------------------------------------------------------------------------------*/

func ax25_get_h(this_p C.packet_t, n C.int) C.int {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)
	Assert(n >= 0 && n < this_p.num_addr)

	if n >= 0 && n < this_p.num_addr {
		return C.int((this_p.frame_data[n*7+6] & SSID_H_MASK) >> SSID_H_SHIFT)
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error: ax25_get_h(%d), num_addr=%d\n", n, this_p.num_addr)
		return (0)
	}
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_set_h
 *
 * Purpose:	Set the "has been repeated" flag of specified address in current packet.
 *
 * Inputs:	n	- Index of address.   Use the symbols
 *			 Should be in range of AX25_REPEATER_1 .. AX25_REPEATER_8.
 *
 * Bugs:	No bounds checking is performed.  Be careful.
 *
 * Assumption:	ax25_from_text or ax25_from_frame was called first.
 *
 * Returns:	None
 *
 *------------------------------------------------------------------------------*/

func ax25_set_h(this_p C.packet_t, n C.int) {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	if n >= 0 && n < this_p.num_addr {
		this_p.frame_data[n*7+6] |= SSID_H_MASK
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error: ax25_set_hd(%d), num_addr=%d\n", n, this_p.num_addr)
	}
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_get_heard
 *
 * Purpose:	Return index of the station that we heard.
 *
 * Inputs:	none
 *
 *
 * Assumption:	ax25_from_text or ax25_from_frame was called first.
 *
 * Returns:	If any of the digipeaters have the has-been-repeated bit set,
 *		return the index of the last one.  Otherwise return index for source.
 *
 *------------------------------------------------------------------------------*/

func ax25_get_heard(this_p C.packet_t) C.int {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	var result C.int = AX25_SOURCE

	for i := C.int(AX25_REPEATER_1); i < ax25_get_num_addr(this_p); i++ {

		if ax25_get_h(this_p, i) != 0 {
			result = i
		}
	}
	return (result)
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_get_first_not_repeated
 *
 * Purpose:	Return index of the first repeater that does NOT have the
 *		"has been repeated" flag set or -1 if none.
 *
 * Inputs:	none
 *
 *
 * Assumption:	ax25_from_text or ax25_from_frame was called first.
 *
 * Returns:	In range of X25_REPEATER_1 .. X25_REPEATER_8 or -1 if none.
 *
 *------------------------------------------------------------------------------*/

func ax25_get_first_not_repeated(this_p C.packet_t) C.int {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	for i := C.int(AX25_REPEATER_1); i < ax25_get_num_addr(this_p); i++ {

		if ax25_get_h(this_p, i) == 0 {
			return (i)
		}
	}
	return (-1)
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_get_rr
 *
 * Purpose:	Return the two reserved "RR" bits in the specified address field.
 *
 * Inputs:	pp	- Packet object.
 *
 *		n	- Index of address.   Use the symbols
 *			  AX25_DESTINATION, AX25_SOURCE, AX25_REPEATER1, etc.
 *
 * Returns:	0, 1, 2, or 3.
 *
 *------------------------------------------------------------------------------*/

func ax25_get_rr(this_p C.packet_t, n C.int) C.int {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)
	Assert(n >= 0 && n < this_p.num_addr)

	if n >= 0 && n < this_p.num_addr {
		return C.int((this_p.frame_data[n*7+6] & SSID_RR_MASK) >> SSID_RR_SHIFT)
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error: ax25_get_rr(%d), num_addr=%d\n", n, this_p.num_addr)
		return (0)
	}
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_get_info
 *
 * Purpose:	Obtain Information part of current packet.
 *
 * Inputs:	this_p	- Packet object pointer.
 *
 * Outputs:	paddr	- Starting address of information part is returned here.
 *
 * Assumption:	ax25_from_text or ax25_from_frame was called first.
 *
 * Returns:	Number of octets in the Information part.
 *		Should be in the range of AX25_MIN_INFO_LEN .. AX25_MAX_INFO_LEN.
 *
 *------------------------------------------------------------------------------*/

func ax25_get_info(this_p C.packet_t, paddr **C.uchar) C.int {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	var info_ptr *C.uchar
	var info_len C.int

	if this_p.num_addr >= 2 {

		/* AX.25 */

		info_ptr = &this_p.frame_data[ax25_get_info_offset(this_p)]
		info_len = ax25_get_num_info(this_p)
	} else {

		/* Not AX.25.  Treat Whole packet as info. */

		info_ptr = &this_p.frame_data[0]
		info_len = this_p.frame_len
	}

	/* Add nul character in case caller treats as printable string. */

	Assert(info_len >= 0)

	*(*C.uchar)(unsafe.Add(unsafe.Pointer(info_ptr), info_len)) = 0

	*paddr = info_ptr
	return (info_len)

} /* end ax25_get_info */

func ax25_set_info(this_p C.packet_t, new_info_ptr *C.uchar, new_info_len C.int) {
	var old_info_ptr *C.uchar
	var old_info_len = ax25_get_info(this_p, &old_info_ptr)
	this_p.frame_len -= old_info_len

	if new_info_len < 0 {
		new_info_len = 0
	}

	if new_info_len > AX25_MAX_INFO_LEN {
		new_info_len = AX25_MAX_INFO_LEN
	}

	C.memcpy(unsafe.Pointer(old_info_ptr), unsafe.Pointer(new_info_ptr), C.size_t(new_info_len))
	this_p.frame_len += new_info_len
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_cut_at_crlf
 *
 * Purpose:	Truncate the information part at the first CR or LF.
 *		This is used for the RF>IS IGate function.
 *		CR/LF is used as record separator so we must remove it
 *		before packaging up packet to sending to server.
 *
 * Inputs:	this_p	- Packet object pointer.
 *
 * Outputs:	Packet is modified in place.
 *
 * Returns:	Number of characters removed from the end.
 *		0 if not changed.
 *
 * Assumption:	ax25_from_text or ax25_from_frame was called first.
 *
 *------------------------------------------------------------------------------*/

func ax25_cut_at_crlf(this_p C.packet_t) C.int {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	var info_ptr *C.uchar
	var info_len = ax25_get_info(this_p, &info_ptr)
	var info = C.GoBytes(unsafe.Pointer(info_ptr), info_len)

	// Can't use strchr because there is potential of nul character.

	for j := C.int(0); j < info_len; j++ {

		if info[j] == '\r' || info[j] == '\n' {

			var chop = info_len - j

			this_p.frame_len -= chop
			return (chop)
		}
	}

	return (0)
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_get_dti
 *
 * Purpose:	Get Data Type Identifier from Information part.
 *
 * Inputs:	None.
 *
 * Assumption:	ax25_from_text or ax25_from_frame was called first.
 *
 * Returns:	First byte from the information part.
 *
 *------------------------------------------------------------------------------*/

func ax25_get_dti(this_p C.packet_t) C.int {
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	if this_p.num_addr >= 2 {
		return C.int(this_p.frame_data[ax25_get_info_offset(this_p)])
	}
	return (' ')
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_set_nextp
 *
 * Purpose:	Set next packet object in queue.
 *
 * Inputs:	this_p		- Current packet object.
 *
 *		next_p		- pointer to next one
 *
 * Description:	This is used to build a linked list for a queue.
 *
 *------------------------------------------------------------------------------*/

func ax25_set_nextp(this_p C.packet_t, next_p C.packet_t) {
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	this_p.nextp = next_p
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_get_nextp
 *
 * Purpose:	Obtain next packet object in queue.
 *
 * Inputs:	Packet object.
 *
 * Returns:	Following object in queue or nil.
 *
 *------------------------------------------------------------------------------*/

func ax25_get_nextp(this_p C.packet_t) C.packet_t {
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	return (this_p.nextp)
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_set_release_time
 *
 * Purpose:	Set release time
 *
 * Inputs:	this_p		- Current packet object.
 *
 *		release_time	- Time as returned by dtime_monotonic().
 *
 *------------------------------------------------------------------------------*/

func ax25_set_release_time(this_p C.packet_t, release_time C.double) {
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	this_p.release_time = release_time
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_get_release_time
 *
 * Purpose:	Get release time.
 *
 *------------------------------------------------------------------------------*/

func ax25_get_release_time(this_p C.packet_t) C.double {
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	return (this_p.release_time)
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_set_modulo
 *
 * Purpose:	Set modulo value for I and S frame sequence numbers.
 *
 *------------------------------------------------------------------------------*/

func ax25_set_modulo(this_p C.packet_t, modulo C.int) {
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	this_p.modulo = modulo
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_get_modulo
 *
 * Purpose:	Get modulo value for I and S frame sequence numbers.
 *
 * Returns:	8 or 128 if known.
 *		0 if unknown.
 *
 *------------------------------------------------------------------------------*/

func ax25_get_modulo(this_p C.packet_t) C.int {
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	return (this_p.modulo)
}

/*------------------------------------------------------------------
 *
 * Function:	ax25_format_addrs
 *
 * Purpose:	Format all the addresses suitable for printing.
 *
 *		The AX.25 spec refers to this as "Source Path Header" - "TNC-2" Format
 *
 * Inputs:	Current packet.
 *
 * Outputs:	result	- All addresses combined into a single string of the form:
 *
 *				"Source > Destination [ , repeater ... ] :"
 *
 *			An asterisk is displayed after the last digipeater
 *			with the "H" bit set.  e.g.  If we hear RPT2,
 *
 *			SRC>DST,RPT1,RPT2*,RPT3:
 *
 *			No asterisk means the source is being heard directly.
 *			Needs to be 101 characters to avoid overflowing.
 *			(Up to 100 characters + \0)
 *
 * Errors:	No error checking so caller needs to be careful.
 *
 *
 *------------------------------------------------------------------*/

// TODO: max len for result.  buffer overflow?

func ax25_format_addrs(this_p C.packet_t, result *C.char) {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)
	*result = 0

	/* New in 0.9. */
	/* Don't get upset if no addresses.  */
	/* This will allow packets that do not comply to AX.25 format. */

	if this_p.num_addr == 0 {
		return
	}

	var stemp [AX25_MAX_ADDR_LEN]C.char
	ax25_get_addr_with_ssid(this_p, AX25_SOURCE, &stemp[0])
	// FIXME:  For ALL strcat: Pass in sizeof result and use strlcat.
	C.strcat(result, &stemp[0])
	C.strcat(result, C.CString(">"))

	ax25_get_addr_with_ssid(this_p, AX25_DESTINATION, &stemp[0])
	C.strcat(result, &stemp[0])

	var heard = ax25_get_heard(this_p)

	for i := C.int(AX25_REPEATER_1); i < this_p.num_addr; i++ {
		ax25_get_addr_with_ssid(this_p, i, &stemp[0])
		C.strcat(result, C.CString(","))
		C.strcat(result, &stemp[0])
		if i == heard {
			C.strcat(result, C.CString("*"))
		}
	}

	C.strcat(result, C.CString(":"))

	// dw_printf ("DEBUG ax25_format_addrs, num_addr = %d, result = '%s'\n", this_p.num_addr, result);
}

/*------------------------------------------------------------------
 *
 * Function:	ax25_format_via_path
 *
 * Purpose:	Format via path addresses suitable for printing.
 *
 * Inputs:	Current packet.
 *
 *		result_size	- Number of bytes available for result.
 *				  We can have up to 8 addresses x 9 characters
 *				  plus 7 commas, possible *, and nul = 81 minimum.
 *
 * Outputs:	result	- Digipeater field addresses combined into a single string of the form:
 *
 *				"repeater, repeater ..."
 *
 *			An asterisk is displayed after the last digipeater
 *			with the "H" bit set.  e.g.  If we hear RPT2,
 *
 *			RPT1,RPT2*,RPT3
 *
 *			No asterisk means the source is being heard directly.
 *
 *------------------------------------------------------------------*/

func ax25_format_via_path(this_p C.packet_t, _result *C.char, result_size C.size_t) {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	/* Don't get upset if no addresses.  */
	/* This will allow packets that do not comply to AX.25 format. */

	if this_p.num_addr == 0 {
		return
	}

	var heard = ax25_get_heard(this_p)
	var result string

	for i := C.int(AX25_REPEATER_1); i < this_p.num_addr; i++ {
		if i > AX25_REPEATER_1 {
			result += ","
		}
		var stemp [AX25_MAX_ADDR_LEN]C.char
		ax25_get_addr_with_ssid(this_p, i, &stemp[0])
		result += C.GoString(&stemp[0])
		if i == heard {
			result += "*"
		}
	}

	C.strcpy(_result, C.CString(result))
} /* end ax25_format_via_path */

/*------------------------------------------------------------------
 *
 * Function:	ax25_pack
 *
 * Purpose:	Put all the pieces into format ready for transmission.
 *
 * Inputs:	this_p	- pointer to packet object.
 *
 * Outputs:	result		- Frame buffer, AX25_MAX_PACKET_LEN bytes.
 *				Should also have two extra for FCS to be
 *				added later.
 *
 * Returns:	Number of octets in the frame buffer.
 *		Does NOT include the extra 2 for FCS.
 *
 * Errors:	Returns -1.
 *
 *------------------------------------------------------------------*/

func ax25_pack(this_p C.packet_t, result *C.uchar) C.int {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	Assert(this_p.frame_len >= 0 && this_p.frame_len <= AX25_MAX_PACKET_LEN)

	C.memcpy(unsafe.Pointer(result), unsafe.Pointer(&this_p.frame_data[0]), C.size_t(this_p.frame_len))

	return (this_p.frame_len)
}

/*------------------------------------------------------------------
 *
 * Function:	ax25_frame_type
 *
 * Purpose:	Extract the type of frame.
 *		This is derived from the control byte(s) but
 *		is an enumerated type for easier handling.
 *
 * Inputs:	this_p	- pointer to packet object.
 *
 * Outputs:	desc	- Text description such as "I frame" or
 *			  "U frame SABME".
 *			  Supply 56 bytes to be safe.
 *
 *		cr	- Command or response?
 *
 *		pf	- P/F - Poll/Final or -1 if not applicable
 *
 *		nr	- N(R) - receive sequence or -1 if not applicable.
 *
 *		ns	- N(S) - send sequence or -1 if not applicable.
 *
 * Returns:	Frame type from  enum ax25_frame_type_e.
 *
 *------------------------------------------------------------------*/

// TODO: need someway to ensure caller allocated enough space.
// Should pass in as parameter.
const DESC_SIZ = 56

func ax25_frame_type(this_p C.packet_t, cr *C.cmdres_t, desc *C.char, pf *C.int, nr *C.int, ns *C.int) ax25_frame_type_t {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	C.strcpy(desc, C.CString("????"))
	*cr = cr_11
	*pf = -1
	*nr = -1
	*ns = -1

	// U frames are always one control byte.
	var c = ax25_get_control(this_p)
	if c < 0 {
		C.strcpy(desc, C.CString("Not AX.25"))
		return (frame_not_AX25)
	}

	/*
	 * TERRIBLE HACK :-(  for display purposes.
	 *
	 * I and S frames can have 1 or 2 control bytes but there is
	 * no good way to determine this without dipping into the data
	 * link state machine.  Can we guess?
	 *
	 * S frames have no protocol id or information so if there is one
	 * more byte beyond the control field, we could assume there are
	 * two control bytes.
	 *
	 * For I frames, the protocol id will usually be 0xf0.  If we find
	 * that as the first byte of the information field, it is probably
	 * the pid and not part of the information.  Ditto for segments 0x08.
	 * Not fool proof but good enough for troubleshooting text out.
	 *
	 * If we have a link to the peer station, this will be set properly
	 * before it needs to be used for other reasons.
	 *
	 * Setting one of the RR bits (find reference!) is sounding better and better.
	 * It's in common usage so I should lobby to get that in the official protocol spec.
	 */

	if this_p.modulo == 0 && (c&3) == 1 && ax25_get_c2(this_p) != -1 {
		this_p.modulo = C.modulo_128
	} else if this_p.modulo == 0 && (c&1) == 0 && this_p.frame_data[ax25_get_info_offset(this_p)] == 0xF0 {
		this_p.modulo = C.modulo_128
	} else if this_p.modulo == 0 && (c&1) == 0 && this_p.frame_data[ax25_get_info_offset(this_p)] == 0x08 { // same for segments
		this_p.modulo = C.modulo_128
	}

	var c2 C.int // I & S frames can have second Control byte.
	if this_p.modulo == C.modulo_128 {
		c2 = ax25_get_c2(this_p)
	}

	var dst_c = this_p.frame_data[AX25_DESTINATION*7+6] & SSID_H_MASK
	var src_c = this_p.frame_data[AX25_SOURCE*7+6] & SSID_H_MASK

	var cr_text string
	var pf_text string

	if dst_c != 0 {
		if src_c != 0 {
			*cr = cr_11
			cr_text = "cc=11"
			pf_text = "p/f"
		} else {
			*cr = cr_cmd
			cr_text = "cmd"
			pf_text = "p"
		}
	} else {
		if src_c != 0 {
			*cr = cr_res
			cr_text = "res"
			pf_text = "f"
		} else {
			*cr = cr_00
			cr_text = "cc=00"
			pf_text = "p/f"
		}
	}

	if (c & 1) == 0 {

		// Information 			rrr p sss 0		or	sssssss 0  rrrrrrr p

		if this_p.modulo == C.modulo_128 {
			*ns = (c >> 1) & 0x7f
			*pf = c2 & 1
			*nr = (c2 >> 1) & 0x7f
		} else {
			*ns = (c >> 1) & 7
			*pf = (c >> 4) & 1
			*nr = (c >> 5) & 7
		}

		//snprintf (desc, DESC_SIZ, "I %s, n(s)=%d, n(r)=%d, %s=%d", cr_text, *ns, *nr, pf_text, *pf);
		C.strcpy(desc, C.CString(fmt.Sprintf("I %s, n(s)=%d, n(r)=%d, %s=%d, pid=0x%02x", cr_text, *ns, *nr, pf_text, *pf, ax25_get_pid(this_p))))
		return (frame_type_I)
	} else if (c & 2) == 0 {

		// Supervisory			rrr p/f ss 0 1		or	0000 ss 0 1  rrrrrrr p/f

		if this_p.modulo == C.modulo_128 {
			*pf = c2 & 1
			*nr = (c2 >> 1) & 0x7f
		} else {
			*pf = (c >> 4) & 1
			*nr = (c >> 5) & 7
		}

		// The exhaustive linter is wrong about exhaustiveness(!)
		switch (c >> 2) & 3 { //nolint:exhaustive
		case 0:
			C.strcpy(desc, C.CString(fmt.Sprintf("RR %s, n(r)=%d, %s=%d", cr_text, *nr, pf_text, *pf)))
			return (frame_type_S_RR)
		case 1:
			C.strcpy(desc, C.CString(fmt.Sprintf("RNR %s, n(r)=%d, %s=%d", cr_text, *nr, pf_text, *pf)))
			return (frame_type_S_RNR)
		case 2:
			C.strcpy(desc, C.CString(fmt.Sprintf("REJ %s, n(r)=%d, %s=%d", cr_text, *nr, pf_text, *pf)))
			return (frame_type_S_REJ)
		case 3:
			C.strcpy(desc, C.CString(fmt.Sprintf("SREJ %s, n(r)=%d, %s=%d", cr_text, *nr, pf_text, *pf)))
			return (frame_type_S_SREJ)
		}
	} else {

		// Unnumbered			mmm p/f mm 1 1

		*pf = (c >> 4) & 1

		switch c & 0xef {

		case 0x6f:
			C.strcpy(desc, C.CString(fmt.Sprintf("SABME %s, %s=%d", cr_text, pf_text, *pf)))
			return (frame_type_U_SABME)
		case 0x2f:
			C.strcpy(desc, C.CString(fmt.Sprintf("SABM %s, %s=%d", cr_text, pf_text, *pf)))
			return (frame_type_U_SABM)
		case 0x43:
			C.strcpy(desc, C.CString(fmt.Sprintf("DISC %s, %s=%d", cr_text, pf_text, *pf)))
			return (frame_type_U_DISC)
		case 0x0f:
			C.strcpy(desc, C.CString(fmt.Sprintf("DM %s, %s=%d", cr_text, pf_text, *pf)))
			return (frame_type_U_DM)
		case 0x63:
			C.strcpy(desc, C.CString(fmt.Sprintf("UA %s, %s=%d", cr_text, pf_text, *pf)))
			return (frame_type_U_UA)
		case 0x87:
			C.strcpy(desc, C.CString(fmt.Sprintf("FRMR %s, %s=%d", cr_text, pf_text, *pf)))
			return (frame_type_U_FRMR)
		case 0x03:
			C.strcpy(desc, C.CString(fmt.Sprintf("UI %s, %s=%d", cr_text, pf_text, *pf)))
			return (frame_type_U_UI)
		case 0xaf:
			C.strcpy(desc, C.CString(fmt.Sprintf("XID %s, %s=%d", cr_text, pf_text, *pf)))
			return (frame_type_U_XID)
		case 0xe3:
			C.strcpy(desc, C.CString(fmt.Sprintf("TEST %s, %s=%d", cr_text, pf_text, *pf)))
			return (frame_type_U_TEST)
		default:
			C.strcpy(desc, C.CString("U other???"))
			return (frame_type_U)
		}
	}

	// Should be unreachable but compiler doesn't realize that.
	// Here only to suppress "warning: control reaches end of non-void function"

	return (frame_not_AX25)

} /* end ax25_frame_type */

/*------------------------------------------------------------------
 *
 * Function:	ax25_hex_dump
 *
 * Purpose:	Print out packet in hexadecimal for debugging.
 *
 * Inputs:	fptr		- Pointer to frame data.
 *
 *		flen		- Frame length, bytes.  Does not include CRC.
 *
 *------------------------------------------------------------------*/

/* Text description of control octet. */
// FIXME:  this is wrong.  It doesn't handle modulo 128.

// TODO: use ax25_frame_type() instead.

func ctrl_to_text(c C.int, out *C.char, outsiz C.size_t) {
	if (c & 1) == 0 {
		C.strcpy(out, C.CString(fmt.Sprintf("I frame: n(r)=%d, p=%d, n(s)=%d", (c>>5)&7, (c>>4)&1, (c>>1)&7)))
	} else if (c & 0xf) == 0x01 {
		C.strcpy(out, C.CString(fmt.Sprintf("S frame RR: n(r)=%d, p/f=%d", (c>>5)&7, (c>>4)&1)))
	} else if (c & 0xf) == 0x05 {
		C.strcpy(out, C.CString(fmt.Sprintf("S frame RNR: n(r)=%d, p/f=%d", (c>>5)&7, (c>>4)&1)))
	} else if (c & 0xf) == 0x09 {
		C.strcpy(out, C.CString(fmt.Sprintf("S frame REJ: n(r)=%d, p/f=%d", (c>>5)&7, (c>>4)&1)))
	} else if (c & 0xf) == 0x0D {
		C.strcpy(out, C.CString(fmt.Sprintf("S frame sREJ: n(r)=%d, p/f=%d", (c>>5)&7, (c>>4)&1)))
	} else if (c & 0xef) == 0x6f {
		C.strcpy(out, C.CString(fmt.Sprintf("U frame SABME: p=%d", (c>>4)&1)))
	} else if (c & 0xef) == 0x2f {
		C.strcpy(out, C.CString(fmt.Sprintf("U frame SABM: p=%d", (c>>4)&1)))
	} else if (c & 0xef) == 0x43 {
		C.strcpy(out, C.CString(fmt.Sprintf("U frame DISC: p=%d", (c>>4)&1)))
	} else if (c & 0xef) == 0x0f {
		C.strcpy(out, C.CString(fmt.Sprintf("U frame DM: f=%d", (c>>4)&1)))
	} else if (c & 0xef) == 0x63 {
		C.strcpy(out, C.CString(fmt.Sprintf("U frame UA: f=%d", (c>>4)&1)))
	} else if (c & 0xef) == 0x87 {
		C.strcpy(out, C.CString(fmt.Sprintf("U frame FRMR: f=%d", (c>>4)&1)))
	} else if (c & 0xef) == 0x03 {
		C.strcpy(out, C.CString(fmt.Sprintf("U frame UI: p/f=%d", (c>>4)&1)))
	} else if (c & 0xef) == 0xAF {
		C.strcpy(out, C.CString(fmt.Sprintf("U frame XID: p/f=%d", (c>>4)&1)))
	} else if (c & 0xef) == 0xe3 {
		C.strcpy(out, C.CString(fmt.Sprintf("U frame TEST: p/f=%d", (c>>4)&1)))
	} else {
		C.strcpy(out, C.CString(fmt.Sprintf("Unknown frame type for control = 0x%02x", c)))
	}
}

/* Text description of protocol id octet. */

const PID_TEXT_SIZE = 80

func pid_to_text(p C.int, out *C.char) {

	if (p & 0x30) == 0x10 {
		C.strcpy(out, C.CString("AX.25 layer 3 implemented."))
	} else if (p & 0x30) == 0x20 {
		C.strcpy(out, C.CString("AX.25 layer 3 implemented."))
	} else if p == 0x01 {
		C.strcpy(out, C.CString("ISO 8208/CCITT X.25 PLP"))
	} else if p == 0x06 {
		C.strcpy(out, C.CString("Compressed TCP/IP packet. Van Jacobson (RFC 1144)"))
	} else if p == 0x07 {
		C.strcpy(out, C.CString("Uncompressed TCP/IP packet. Van Jacobson (RFC 1144)"))
	} else if p == 0x08 {
		C.strcpy(out, C.CString("Segmentation fragment"))
	} else if p == 0xC3 {
		C.strcpy(out, C.CString("TEXNET datagram protocol"))
	} else if p == 0xC4 {
		C.strcpy(out, C.CString("Link Quality Protocol"))
	} else if p == 0xCA {
		C.strcpy(out, C.CString("Appletalk"))
	} else if p == 0xCB {
		C.strcpy(out, C.CString("Appletalk ARP"))
	} else if p == 0xCC {
		C.strcpy(out, C.CString("ARPA Internet Protocol"))
	} else if p == 0xCD {
		C.strcpy(out, C.CString("ARPA Address resolution"))
	} else if p == 0xCE {
		C.strcpy(out, C.CString("FlexNet"))
	} else if p == 0xCF {
		C.strcpy(out, C.CString("NET/ROM"))
	} else if p == 0xF0 {
		C.strcpy(out, C.CString("No layer 3 protocol implemented."))
	} else if p == 0xFF {
		C.strcpy(out, C.CString("Escape character. Next octet contains more Level 3 protocol information."))
	} else {
		C.strcpy(out, C.CString(fmt.Sprintf("Unknown protocol id = 0x%02x", p)))
	}
}

func ax25_hex_dump(this_p C.packet_t) {
	var fptr = this_p.frame_data
	var flen = this_p.frame_len

	if this_p.num_addr >= C.AX25_MIN_ADDRS && this_p.num_addr <= AX25_MAX_ADDRS {

		var c = fptr[this_p.num_addr*7]
		var p = fptr[this_p.num_addr*7+1]

		var cp_text [120]C.char
		ctrl_to_text(C.int(c), &cp_text[0], C.size_t(len(cp_text))) // TODO: use ax25_frame_type() instead.

		if (c&0x01) == 0 || /* I   xxxx xxx0 */
			c == 0x03 || c == 0x13 { /* UI  000x 0011 */

			var pid_text [PID_TEXT_SIZE]C.char

			pid_to_text(C.int(p), &pid_text[0])

			C.strcat(&cp_text[0], C.CString(", "))
			C.strcat(&cp_text[0], &pid_text[0])

		}

		var l_text [20]C.char
		C.strcpy(&l_text[0], C.CString(fmt.Sprintf(", length = %d", flen)))
		C.strcat(&cp_text[0], &l_text[0])

		dw_printf("%s\n", C.GoString(&cp_text[0]))
	}

	// Address fields must be only upper case letters and digits.
	// If less than 6 characters, trailing positions are filled with ASCII space.
	// Using all zero bits in one of these 6 positions is wrong.
	// Any non printable characters will be printed as "." here.

	dw_printf(" dest    %c%c%c%c%c%c %2d c/r=%d res=%d last=%d\n",
		IfThenElse(unicode.IsPrint(rune(fptr[0]>>1)), fptr[0]>>1, '.'),
		IfThenElse(unicode.IsPrint(rune(fptr[1]>>1)), fptr[1]>>1, '.'),
		IfThenElse(unicode.IsPrint(rune(fptr[2]>>1)), fptr[2]>>1, '.'),
		IfThenElse(unicode.IsPrint(rune(fptr[3]>>1)), fptr[3]>>1, '.'),
		IfThenElse(unicode.IsPrint(rune(fptr[4]>>1)), fptr[4]>>1, '.'),
		IfThenElse(unicode.IsPrint(rune(fptr[5]>>1)), fptr[5]>>1, '.'),
		(fptr[6]&SSID_SSID_MASK)>>SSID_SSID_SHIFT,
		(fptr[6]&SSID_H_MASK)>>SSID_H_SHIFT,
		(fptr[6]&SSID_RR_MASK)>>SSID_RR_SHIFT,
		fptr[6]&SSID_LAST_MASK)

	dw_printf(" source  %c%c%c%c%c%c %2d c/r=%d res=%d last=%d\n",
		IfThenElse(unicode.IsPrint(rune(fptr[7]>>1)), fptr[7]>>1, '.'),
		IfThenElse(unicode.IsPrint(rune(fptr[8]>>1)), fptr[8]>>1, '.'),
		IfThenElse(unicode.IsPrint(rune(fptr[9]>>1)), fptr[9]>>1, '.'),
		IfThenElse(unicode.IsPrint(rune(fptr[10]>>1)), fptr[10]>>1, '.'),
		IfThenElse(unicode.IsPrint(rune(fptr[11]>>1)), fptr[11]>>1, '.'),
		IfThenElse(unicode.IsPrint(rune(fptr[12]>>1)), fptr[12]>>1, '.'),
		(fptr[13]&SSID_SSID_MASK)>>SSID_SSID_SHIFT,
		(fptr[13]&SSID_H_MASK)>>SSID_H_SHIFT,
		(fptr[13]&SSID_RR_MASK)>>SSID_RR_SHIFT,
		fptr[13]&SSID_LAST_MASK)

	for n := C.int(2); n < this_p.num_addr; n++ {

		dw_printf(" digi %d  %c%c%c%c%c%c %2d   h=%d res=%d last=%d\n",
			n-1,
			IfThenElse(unicode.IsPrint(rune(fptr[n*7+0]>>1)), fptr[n*7+0]>>1, '.'),
			IfThenElse(unicode.IsPrint(rune(fptr[n*7+1]>>1)), fptr[n*7+1]>>1, '.'),
			IfThenElse(unicode.IsPrint(rune(fptr[n*7+2]>>1)), fptr[n*7+2]>>1, '.'),
			IfThenElse(unicode.IsPrint(rune(fptr[n*7+3]>>1)), fptr[n*7+3]>>1, '.'),
			IfThenElse(unicode.IsPrint(rune(fptr[n*7+4]>>1)), fptr[n*7+4]>>1, '.'),
			IfThenElse(unicode.IsPrint(rune(fptr[n*7+5]>>1)), fptr[n*7+5]>>1, '.'),
			(fptr[n*7+6]&SSID_SSID_MASK)>>SSID_SSID_SHIFT,
			(fptr[n*7+6]&SSID_H_MASK)>>SSID_H_SHIFT,
			(fptr[n*7+6]&SSID_RR_MASK)>>SSID_RR_SHIFT,
			fptr[n*7+6]&SSID_LAST_MASK)

	}

	C.hex_dump(&fptr[0], flen)

} /* end ax25_hex_dump */

/*------------------------------------------------------------------
 *
 * Function:	ax25_is_aprs
 *
 * Purpose:	Is this packet APRS format?
 *
 * Inputs:	this_p	- pointer to packet object.
 *
 * Returns:	True if this frame has the proper control
 *		octets for an APRS packet.
 *			control		3 for UI frame
 *			protocol id	0xf0 for no layer 3
 *
 *
 * Description:	Dire Wolf should be able to act as a KISS TNC for
 *		any type of AX.25 activity.  However, there are other
 *		places where we want to process only APRS.
 *		(e.g. digipeating and IGate.)
 *
 *------------------------------------------------------------------*/

func ax25_is_aprs(this_p C.packet_t) C.int {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	if this_p.frame_len == 0 {
		return (0)
	}

	var ctrl = ax25_get_control(this_p)
	var pid = ax25_get_pid(this_p)

	var is_aprs = this_p.num_addr >= 2 && ctrl == C.AX25_UI_FRAME && pid == C.AX25_PID_NO_LAYER_3

	return C.int(IfThenElse(is_aprs, 1, 0))
}

/*------------------------------------------------------------------
 *
 * Function:	ax25_is_null_frame
 *
 * Purpose:	Is this packet structure empty?
 *
 * Inputs:	this_p	- pointer to packet object.
 *
 * Returns:	True if frame data length is 0.
 *
 * Description:	This is used when we want to wake up the
 *		transmit queue processing thread but don't
 *		want to transmit a frame.
 *
 *------------------------------------------------------------------*/

func ax25_is_null_frame(this_p C.packet_t) C.int {

	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	var is_null = this_p.frame_len == 0

	return C.int(IfThenElse(is_null, 1, 0))
}

/*------------------------------------------------------------------
*
* Function:	ax25_get_control
		ax25_get_c2
*
* Purpose:	Get Control field from packet.
*
* Inputs:	this_p	- pointer to packet object.
*
* Returns:	APRS uses AX25_UI_FRAME.
*		This could also be used in other situations.
*
*------------------------------------------------------------------*/

func ax25_get_control(this_p C.packet_t) C.int {
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	if this_p.frame_len == 0 {
		return (-1)
	}

	if this_p.num_addr >= 2 {
		return C.int(this_p.frame_data[ax25_get_control_offset(this_p)])
	}
	return (-1)
}

func ax25_get_c2(this_p C.packet_t) C.int {
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	if this_p.frame_len == 0 {
		return (-1)
	}

	if this_p.num_addr >= 2 {
		var offset2 = ax25_get_control_offset(this_p) + 1

		if offset2 < this_p.frame_len {
			return C.int(this_p.frame_data[offset2])
		} else {
			return (-1) /* attempt to go beyond the end of frame. */
		}
	}
	return (-1) /* not AX.25 */
}

/*------------------------------------------------------------------
 *
 * Function:	ax25_set_pid
 *
 * Purpose:	Set protocol ID in packet.
 *
 * Inputs:	this_p	- pointer to packet object.
 *
 *		pid - usually 0xF0 for APRS or 0xCF for NET/ROM.
 *
 * AX.25:	"The Protocol Identifier (PID) field appears in information
 *		 frames (I and UI) only. It identifies which kind of
 *		 Layer 3 protocol, if any, is in use."
 *
 *------------------------------------------------------------------*/

func ax25_set_pid(this_p C.packet_t, pid C.int) {
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	// Some applications set this to 0 which is an error.
	// Change 0 to 0xF0 meaning no layer 3 protocol.

	if pid == 0 {
		pid = C.AX25_PID_NO_LAYER_3
	}

	// Sanity check: is it I or UI frame?

	if this_p.frame_len == 0 {
		return
	}

	var cr C.cmdres_t // command or response.
	var description [64]C.char
	var pf C.int     // Poll/Final.
	var nr, ns C.int // Sequence numbers.

	var frame_type = ax25_frame_type(this_p, &cr, &description[0], &pf, &nr, &ns)

	if frame_type != frame_type_I && frame_type != frame_type_U_UI {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ax25_set_pid(0x%2x): Packet type is not I or UI.\n", pid)
		return
	}

	// TODO: handle 2 control byte case.
	if this_p.num_addr >= 2 {
		this_p.frame_data[ax25_get_pid_offset(this_p)] = C.uchar(pid)
	}
}

/*------------------------------------------------------------------
 *
 * Function:	ax25_get_pid
 *
 * Purpose:	Get protocol ID from packet.
 *
 * Inputs:	this_p	- pointer to packet object.
 *
 * Returns:	APRS uses 0xf0 for no layer 3.
 *		This could also be used in other situations.
 *
 * AX.25:	"The Protocol Identifier (PID) field appears in information
 *		 frames (I and UI) only. It identifies which kind of
 *		 Layer 3 protocol, if any, is in use."
 *
 *------------------------------------------------------------------*/

func ax25_get_pid(this_p C.packet_t) C.int {
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	// TODO: handle 2 control byte case.
	// TODO: sanity check: is it I or UI frame?

	if this_p.frame_len == 0 {
		return (-1)
	}

	if this_p.num_addr >= 2 {
		return C.int(this_p.frame_data[ax25_get_pid_offset(this_p)])
	}
	return (-1)
}

/*------------------------------------------------------------------
 *
 * Function:	ax25_get_frame_len
 *
 * Purpose:	Get length of frame.
 *
 * Inputs:	this_p	- pointer to packet object.
 *
 * Returns:	Number of octets in the frame buffer.
 *		Does NOT include the extra 2 for FCS.
 *
 *------------------------------------------------------------------*/

func ax25_get_frame_len(this_p C.packet_t) C.int {
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	Assert(this_p.frame_len >= 0 && this_p.frame_len <= AX25_MAX_PACKET_LEN)

	return (this_p.frame_len)

} /* end ax25_get_frame_len */

func ax25_get_frame_data_ptr(this_p C.packet_t) *C.uchar {
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	return (&this_p.frame_data[0])

} /* end ax25_get_frame_data_ptr */

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_dedupe_crc
 *
 * Purpose:	Calculate a checksum for the packet source, destination, and
 *		information but NOT the digipeaters.
 *		This is used for duplicate detection in the digipeater
 *		and IGate algorithms.
 *
 * Input:	pp	- Pointer to packet object.
 *
 * Returns:	Value which will be the same for a duplicate but very unlikely
 *		to match a non-duplicate packet.
 *
 * Description:	For detecting duplicates, we need to look
 *			+ source station
 *			+ destination
 *			+ information field
 *		but NOT the changing list of digipeaters.
 *
 *		Typically, only a checksum is kept to reduce memory
 *		requirements and amount of compution for comparisons.
 *		There is a very very small probability that two unrelated
 *		packets will result in the same checksum, and the
 *		undesired dropping of the packet.
 *
 *		There is a 1 / 65536 chance of getting a false positive match
 *		which is good enough for this application.
 *		We could reduce that with a 32 bit CRC instead of reusing
 *		code from the AX.25 frame CRC calculation.
 *
 * Version 1.3:	We exclude any trailing CR/LF at the end of the info part
 *		so we can detect duplicates that are received only over the
 *		air and those which have gone thru an IGate where the process
 *		removes any trailing CR/LF.   Example:
 *
 *		Original via RF only:
 *		W1TG-1>APU25N,N3LEE-10*,WIDE2-1:<IGATE,MSG_CNT=30,LOC_CNT=61<0x0d>
 *
 *		When we get the same thing via APRS-IS:
 *		W1TG-1>APU25N,K1FFK,WIDE2*,qAR,WB2ZII-15:<IGATE,MSG_CNT=30,LOC_CNT=61
 *
 *		(Actually there is a trailing space.  Maybe some systems
 *		change control characters to space???)
 *		Hmmmm.  I guess we should ignore trailing space as well for
 *		duplicate detection and suppression.
 *
 *------------------------------------------------------------------------------*/

func ax25_dedupe_crc(pp C.packet_t) C.ushort {

	var src [AX25_MAX_ADDR_LEN]C.char
	ax25_get_addr_with_ssid(pp, AX25_SOURCE, &src[0])

	var dest [AX25_MAX_ADDR_LEN]C.char
	ax25_get_addr_with_ssid(pp, AX25_DESTINATION, &dest[0])

	var pinfo *C.uchar
	var info_len = ax25_get_info(pp, &pinfo)
	var info = C.GoBytes(unsafe.Pointer(pinfo), info_len)

	for info_len >= 1 && (info[info_len-1] == '\r' ||
		info[info_len-1] == '\n' ||
		info[info_len-1] == ' ') {

		// Temporary for debugging!

		//  if (pinfo[info_len-1] == ' ') {
		//    text_color_set(DW_COLOR_ERROR);
		//    dw_printf ("DEBUG:  ax25_dedupe_crc ignoring trailing space.\n");
		//  }

		info_len--
	}

	var crc C.ushort = 0xffff
	crc = crc16((*C.uchar)(unsafe.Pointer(&src[0])), C.int(C.strlen(&src[0])), crc)
	crc = crc16((*C.uchar)(unsafe.Pointer(&dest[0])), C.int(C.strlen(&dest[0])), crc)
	crc = crc16(pinfo, info_len, crc)

	return (crc)
}

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_m_m_crc
 *
 * Purpose:	Calculate a checksum for the packet.
 *		This is used for the multimodem duplicate detection.
 *
 * Input:	pp	- Pointer to packet object.
 *
 * Returns:	Value which will be the same for a duplicate but very unlikely
 *		to match a non-duplicate packet.
 *
 * Description:	For detecting duplicates, we need to look the entire packet.
 *
 *		Typically, only a checksum is kept to reduce memory
 *		requirements and amount of compution for comparisons.
 *		There is a very very small probability that two unrelated
 *		packets will result in the same checksum, and the
 *		undesired dropping of the packet.

 *------------------------------------------------------------------------------*/

func ax25_m_m_crc(pp C.packet_t) C.ushort {

	// TODO: I think this can be more efficient by getting the packet content pointer instead of copying.
	var fbuf [AX25_MAX_PACKET_LEN]C.uchar
	var flen = ax25_pack(pp, &fbuf[0])

	var crc C.ushort = 0xffff
	crc = crc16(&fbuf[0], flen, crc)

	return (crc)
}

/*------------------------------------------------------------------
 *
 * Function:	ax25_safe_print
 *
 * Purpose:	Print given string, changing non printable characters to
 *		hexadecimal notation.   Note that character values
 *		<DEL>, 28, 29, 30, and 31 can appear in MIC-E message.
 *
 * Inputs:	pstr	- Pointer to string.
 *
 *		len	- Number of bytes.  If < 0 we use strlen().
 *
 *		ascii_only	- Restrict output to only ASCII.
 *				  Normally we allow UTF-8.
 *
 *		Stops after non-zero len characters or at nul.
 *
 * Returns:	none
 *
 * Description:	Print a string in a "safe" manner.
 *		Anything that is not a printable character
 *		will be converted to a hexadecimal representation.
 *		For example, a Line Feed character will appear as <0x0a>
 *		rather than dropping down to the next line on the screen.
 *
 *		ax25_from_text can accept this format.
 *
 *
 * Example:	W1MED-1>T2QP0S,N1OHZ,N8VIM*,WIDE1-1:'cQBl <0x1c>-/]<0x0d>
 *		                                          ------   ------
 *
 * Questions:	What should we do about UTF-8?  Should that be displayed
 *		as hexadecimal for troubleshooting? Maybe an option so the
 *		packet raw data is in hexadecimal but an extracted
 *		comment displays UTF-8?  Or a command line option for only ASCII?
 *
 * Trailing space:
 *		I recently noticed a case where a packet has space character
 *		at the end.  If the last character of the line is a space,
 *		this will be displayed in hexadecimal to make it obvious.
 *
 *------------------------------------------------------------------*/

// #define MAXSAFE 500
const MAXSAFE = AX25_MAX_INFO_LEN

func ax25_safe_print(_pstr *C.char, length C.int, ascii_only C.int) {

	if length < 0 {
		length = C.int(C.strlen(_pstr))
	}

	if length > MAXSAFE {
		length = MAXSAFE
	}

	var safe_str string
	var pstr = C.GoString(_pstr)

	for i, ch := range pstr {
		if C.int(i) >= length {
			break
		}
		if ch == ' ' && i == len(pstr)-1 {
			safe_str += fmt.Sprintf("<0x%02x>", ch)
		} else if ch < ' ' || ch == 0x7f || ch == 0xfe || ch == 0xff ||
			(ascii_only != 0 && ch >= 0x80) {

			/* Control codes and delete. */
			/* UTF-8 does not use fe and ff except in a possible */
			/* "Byte Order Mark" (BOM) at the beginning. */

			safe_str += fmt.Sprintf("<0x%02x>", ch)
		} else {
			/* Let everything else thru so we can handle UTF-8 */
			/* Maybe we should have an option to display 0x80 */
			/* and above as hexadecimal. */

			safe_str += string(ch)
		}
	}

	// TODO1.2: should return string rather printing to remove a race condition.

	dw_printf("%s", safe_str)
} /* end ax25_safe_print */

/*------------------------------------------------------------------
 *
 * Function:	ax25_alevel_to_text
 *
 * Purpose:	Convert audio level to text representation.
 *
 * Inputs:	alevel	- Audio levels collected from demodulator.
 *
 * Outputs:	text	- Text representation for presentation to user.
 *			  Currently it will look something like this:
 *
 *				r(m/s)
 *
 *			  With n,m,s corresponding to received, mark, and space.
 *			  Comma is to be avoided because one place this
 *			  ends up is in a CSV format file.
 *
 *			  size should be AX25_ALEVEL_TO_TEXT_SIZE.
 *
 * Returns:	True if something to print.  (currently if alevel.original >= 0)
 *		False if not.
 *
 * Description:	Audio level used to be simple; it was a single number.
 *		In version 1.2, we start collecting more details.
 *		At the moment, it includes:
 *
 *		- Received level from new method.
 *		- Levels from mark & space filters to examine the ratio.
 *
 *		We print this in multiple places so put it into a function.
 *
 *------------------------------------------------------------------*/

func ax25_alevel_to_text(alevel C.alevel_t, text *C.char) C.int {

	if alevel.rec < 0 {
		C.strcpy(text, C.CString(""))
		return (0)
	}

	// TODO1.2: haven't thought much about non-AFSK cases yet.
	// What should we do for 9600 baud?

	// For DTMF omit the two extra numbers.

	if alevel.mark >= 0 && alevel.space < 0 { /* baseband */
		C.strcpy(text, C.CString(fmt.Sprintf("%d(%+d/%+d)", alevel.rec, alevel.mark, alevel.space)))
	} else if (alevel.mark == -1 && alevel.space == -1) || /* PSK */
		(alevel.mark == -99 && alevel.space == -99) { /* v. 1.7 "B" FM demodulator. */
		// ?? Where does -99 come from?

		C.strcpy(text, C.CString(fmt.Sprintf("%d", alevel.rec)))
	} else if alevel.mark == -2 && alevel.space == -2 { /* DTMF - single number. */

		C.strcpy(text, C.CString(fmt.Sprintf("%d", alevel.rec)))
	} else { /* AFSK */

		//snprintf (text, AX25_ALEVEL_TO_TEXT_SIZE, "%d:%d(%d/%d=%05.3f=)", alevel.original, alevel.rec, alevel.mark, alevel.space, alevel.ms_ratio);
		C.strcpy(text, C.CString(fmt.Sprintf("%d(%d/%d)", alevel.rec, alevel.mark, alevel.space)))
	}
	return (1)

} /* end ax25_alevel_to_text */

/*
 * APRS always has one control octet of 0x03 but the more
 * general AX.25 case is one or two control bytes depending on
 * whether "modulo 128 operation" is in effect.
 */

//#define DEBUGX 1

func ax25_get_control_offset(this_p C.packet_t) C.int {
	return (this_p.num_addr * 7)
}

func ax25_get_num_control(this_p C.packet_t) C.int {

	var c = this_p.frame_data[ax25_get_control_offset(this_p)]

	if (c & 0x01) == 0 { /* I   xxxx xxx0 */
		/*
			#if DEBUGX
				  dw_printf ("ax25_get_num_control, %02x is I frame, returns %d\n", c, (this_p.modulo == 128) ? 2 : 1);
			#endif
		*/
		if this_p.modulo == 128 {
			return 2
		} else {
			return 1
		}
	}

	if (c & 0x03) == 1 { /* S   xxxx xx01 */
		/*
			#if DEBUGX
				  dw_printf ("ax25_get_num_control, %02x is S frame, returns %d\n", c, (this_p.modulo == 128) ? 2 : 1);
			#endif
		*/
		if this_p.modulo == 128 {
			return 2
		} else {
			return 1
		}
	}

	/*
		#if DEBUGX
			dw_printf ("ax25_get_num_control, %02x is U frame, always returns 1.\n", c);
		#endif
	*/

	return (1) /* U   xxxx xx11 */
}

/*
 * APRS always has one protocol octet of 0xF0 meaning no level 3
 * protocol but the more general case is 0, 1 or 2 protocol ID octets.
 */

func ax25_get_pid_offset(this_p C.packet_t) C.int {
	return (ax25_get_control_offset(this_p) + ax25_get_num_control(this_p))
}

func ax25_get_num_pid(this_p C.packet_t) C.int {

	var c = this_p.frame_data[ax25_get_control_offset(this_p)]

	var pid C.int

	if (c&0x01) == 0 || /* I   xxxx xxx0 */
		c == 0x03 || c == 0x13 { /* UI  000x 0011 */

		pid = C.int(this_p.frame_data[ax25_get_pid_offset(this_p)])
		/*
			#if DEBUGX
				  dw_printf ("ax25_get_num_pid, %02x is I or UI frame, pid = %02x, returns %d\n", c, pid, (pid==AX25_PID_ESCAPE_CHARACTER) ? 2 : 1);
			#endif
		*/
		if pid == C.AX25_PID_ESCAPE_CHARACTER {
			return (2) /* pid 1111 1111 means another follows. */
		}
		return (1)
	}

	/*
		#if DEBUGX
			dw_printf ("ax25_get_num_pid, %02x is neither I nor UI frame, returns 0\n", c);
		#endif
	*/

	return (0)
}

/*
 * AX.25 has info field for 5 frame types depending on the control field.
 *
 *	xxxx xxx0	I
 *	000x 0011	UI		(which includes APRS)
 *	101x 1111	XID
 *	111x 0011	TEST
 *	100x 0111	FRMR
 *
 * APRS always has an Information field with at least one octet for the Data Type Indicator.
 */

func ax25_get_info_offset(this_p C.packet_t) C.int {
	var offset = ax25_get_control_offset(this_p) + ax25_get_num_control(this_p) + ax25_get_num_pid(this_p)
	/*
		#if DEBUGX
			dw_printf ("ax25_get_info_offset, returns %d\n", offset);
		#endif
	*/
	return (offset)
}

func ax25_get_num_info(this_p C.packet_t) C.int {

	/* assuming AX.25 frame. */

	var length = this_p.frame_len - this_p.num_addr*7 - ax25_get_num_control(this_p) - ax25_get_num_pid(this_p)
	if length < 0 {
		length = 0 /* print error? */
	}

	return (length)
}
