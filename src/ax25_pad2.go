package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:	Packet assembler and disasembler, part 2.
 *
 * Description:
 *
 *	The original ax25_pad.c was written with APRS in mind.
 *	It handles UI frames and transparency for a KISS TNC.
 *	Here we add new functions that can handle the
 *	more general cases of AX.25 frames.
 *
 *
 *	* Destination Address  (note: opposite order in printed format)
 *
 *	* Source Address
 *
 *	* 0-8 Digipeater Addresses
 *				(The AX.25 v2.2 spec reduced this number to
 *				a maximum of 2 but I allow the original 8.)
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
 *			C = command/response.   Set to 1 for command.
 *			R R = Reserved = 1 1	(See RR note, below)
 *			SSID = substation ID
 *			0 = zero
 *
 *	The final octet of the Source has the form:
 *
 *		C R R SSID 0, where,
 *
 *			C = command/response.   Must be inverse of destination C bit.
 *			R R = Reserved = 1 1	(See RR note, below)
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
 *
 *
 *	Next we have:
 *
 *	* One or two byte Control Field - A U frame always has one control byte.
 *					When using modulo 128 sequence numbers, the
 *					I and S frames can have a second byte allowing
 *					7 bit fields instead of 3 bit fields.
 *					Unfortunately, we can't tell which we have by looking
 *					at a frame out of context.  :-(
 *					If we are one end of the link, we would know this
 *					from SABM/SABME and possible later negotiation
 *					with XID.  But if we start monitoring two other
 *					stations that are already conversing, we don't know.
 *
 *			RR note:	It seems that some implementations put a hint
 *					in the "RR" reserved bits.
 *					http://www.tapr.org/pipermail/ax25-layer2/2005-October/000297.html (now broken)
 *					https://elixir.bootlin.com/linux/latest/source/net/ax25/ax25_addr.c#L237
 *
 *					The RR bits can also be used for "DAMA" which is
 *					some sort of channel access coordination scheme.
 *					http://internet.freepage.de/cgi-bin/feets/freepage_ext/41030x030A/rewrite/hennig/afu/afudoc/afudama.html
 *					Neither is part of the official protocol spec.
 *
 *	* One byte Protocol ID 		- Only for I and UI frames.
 *					Normally we would use 0xf0 for no layer 3.
 *
 *	Finally the Information Field. The initial max size is 256 but it
 *	can be negotiated higher if both ends agree.
 *
 *	Only these types of frames can have an information part:
 *		- I
 *		- UI
 *		- XID
 *		- TEST
 *		- FRMR
 *
 *	The 2 byte CRC is not stored here.
 *
 *
 * Constructors:
 *		ax25_u_frame		- Construct a U frame.
 *		ax25_s_frame		- Construct a S frame.
 *		ax25_i_frame		- Construct a I frame.
 *
 * Get methods:	....			???
 *
 *------------------------------------------------------------------*/

// #include <stdlib.h>
// #include <string.h>
// #include <assert.h>
// #include <stdio.h>
// #include <ctype.h>
import "C"

import (
	"unsafe"
)

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_u_frame
 *
 * Purpose:	Construct a U frame.
 *
 * Input:	addrs		- Array of addresses.
 *
 *		num_addr	- Number of addresses, range 2 .. 10.
 *
 *		cr		- cr_cmd command frame, cr_res for a response frame.
 *
 *		ftype		- One of:
 *				        frame_type_U_SABME     // Set Async Balanced Mode, Extended
 *				        frame_type_U_SABM      // Set Async Balanced Mode
 *				        frame_type_U_DISC      // Disconnect
 *				        frame_type_U_DM        // Disconnect Mode
 *				        frame_type_U_UA        // Unnumbered Acknowledge
 *				        frame_type_U_FRMR      // Frame Reject
 *				        frame_type_U_UI        // Unnumbered Information
 *				        frame_type_U_XID       // Exchange Identification
 *				        frame_type_U_TEST      // Test
 *
 *		pf		- Poll/Final flag.
 *
 *		pid		- Protocol ID.  >>> Used ONLY for the UI type. <<<
 *				  Normally 0xf0 meaning no level 3.
 *				  Could be other values for NET/ROM, etc.
 *
 *		info		- Info field.  Allowed only for UI, XID, TEST, FRMR.
 *
 *
 * Returns:	Pointer to new packet object.
 *
 *------------------------------------------------------------------------------*/

func ax25_u_frame(addrs [AX25_MAX_ADDRS][AX25_MAX_ADDR_LEN]C.char, num_addr C.int, cr cmdres_t, ftype ax25_frame_type_t, pf int, pid int, info []byte) *packet_t {

	var this_p = ax25_new()

	if this_p == nil {
		return (nil)
	}

	this_p.modulo = 0

	if set_addrs(this_p, addrs, num_addr, cr) == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error in ax25_u_frame: Could not set addresses for U frame.\n")
		ax25_delete(this_p)
		return (nil)
	}

	var ctrl C.int
	var t cmdres_t // 1 = must be cmd, 0 = must be response, 2 = can be either.
	var i = false  // Is Info part allowed?
	switch ftype {
	// 1 = cmd only, 0 = res only, 2 = either
	case frame_type_U_SABME:
		ctrl = 0x6f
		t = 1
	case frame_type_U_SABM:
		ctrl = 0x2f
		t = 1
	case frame_type_U_DISC:
		ctrl = 0x43
		t = 1
	case frame_type_U_DM:
		ctrl = 0x0f
		t = 0
	case frame_type_U_UA:
		ctrl = 0x63
		t = 0
	case frame_type_U_FRMR:
		ctrl = 0x87
		t = 0
		i = true
	case frame_type_U_UI:
		ctrl = 0x03
		t = 2
		i = true
	case frame_type_U_XID:
		ctrl = 0xaf
		t = 2
		i = true
	case frame_type_U_TEST:
		ctrl = 0xe3
		t = 2
		i = true
	default:
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error in ax25_u_frame: Invalid ftype %d for U frame.\n", ftype)
		ax25_delete(this_p)
		return (nil)
	}

	if pf != 0 {
		ctrl |= 0x10
	}

	if t != 2 {
		if cr != t {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Internal error in ax25_u_frame: U frame, cr is %d but must be %d. ftype=%d\n", cr, t, ftype)
		}
	}

	var p = (*C.uchar)(unsafe.Add(unsafe.Pointer(&this_p.frame_data[0]), this_p.frame_len))
	*p = C.uchar(ctrl)
	p = (*C.uchar)(unsafe.Add(unsafe.Pointer(p), 1))
	this_p.frame_len++

	if ftype == frame_type_U_UI {

		// Definitely don't want pid value of 0 (not in valid list)
		// or 0xff (which means more bytes follow).

		if pid < 0 || pid == 0 || pid == 0xff {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Internal error in ax25_u_frame: U frame, Invalid pid value 0x%02x.\n", pid)
			pid = AX25_PID_NO_LAYER_3
		}
		*p = C.uchar(pid)
		p = (*C.uchar)(unsafe.Add(unsafe.Pointer(p), 1))
		this_p.frame_len++
	}

	if i {
		if len(info) > 0 {
			if len(info) > AX25_MAX_INFO_LEN {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Internal error in ax25_u_frame: U frame, Invalid information field length %d.\n", len(info))
				info = info[:AX25_MAX_INFO_LEN]
			}
			C.memcpy(unsafe.Pointer(p), C.CBytes(info), C.size_t(len(info)))
			p = (*C.uchar)(unsafe.Add(unsafe.Pointer(p), len(info)))
			this_p.frame_len += len(info)
		}
	} else {
		if len(info) > 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Internal error in ax25_u_frame: Info part not allowed for U frame type.\n")
		}
	}
	*p = 0

	// TODO KG Assert(p == this_p.frame_data+this_p.frame_len)
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	return (this_p)
} /* end ax25_u_frame */

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_s_frame
 *
 * Purpose:	Construct an S frame.
 *
 * Input:	addrs		- Array of addresses.
 *
 *		num_addr	- Number of addresses, range 2 .. 10.
 *
 *		cr		- cr_cmd command frame, cr_res for a response frame.
 *
 *		ftype		- One of:
 *				        frame_type_S_RR,        // Receive Ready - System Ready To Receive
 *				        frame_type_S_RNR,       // Receive Not Ready - TNC Buffer Full
 *				        frame_type_S_REJ,       // Reject Frame - Out of Sequence or Duplicate
 *				        frame_type_S_SREJ,      // Selective Reject - Request single frame repeat
 *
 *		modulo		- 8 or 128.  Determines if we have 1 or 2 control bytes.
 *
 *		nr		- N(R) field --- describe.
 *
 *		pf		- Poll/Final flag.
 *
 *		info		- Info field.  Allowed only for SREJ.
 *
 *
 * Returns:	Pointer to new packet object.
 *
 *------------------------------------------------------------------------------*/

func ax25_s_frame(
	addrs [AX25_MAX_ADDRS][AX25_MAX_ADDR_LEN]C.char,
	num_addr C.int,
	cr cmdres_t,
	ftype ax25_frame_type_t,
	modulo ax25_modulo_t,
	nr int,
	pf int,
	info []byte,
) *packet_t {

	var this_p = ax25_new()

	if this_p == nil {
		return (nil)
	}

	if set_addrs(this_p, addrs, num_addr, cr) == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error in ax25_s_frame: Could not set addresses for S frame.\n")
		ax25_delete(this_p)
		return (nil)
	}

	if modulo != 8 && modulo != 128 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error in ax25_s_frame: Invalid modulo %d for S frame.\n", modulo)
		modulo = 8
	}
	this_p.modulo = modulo

	if nr < 0 || nr >= int(modulo) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error in ax25_s_frame: Invalid N(R) %d for S frame.\n", nr)
		nr &= int(modulo - 1)
	}

	// Erratum: The AX.25 spec is not clear about whether SREJ should be command, response, or both.
	// The underlying X.25 spec clearly says it is response only.  Let's go with that.

	if ftype == frame_type_S_SREJ && cr != cr_res {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error in ax25_s_frame: SREJ must be response.\n")
	}

	var ctrl int
	switch ftype {
	case frame_type_S_RR:
		ctrl = 0x01
	case frame_type_S_RNR:
		ctrl = 0x05
	case frame_type_S_REJ:
		ctrl = 0x09
	case frame_type_S_SREJ:
		ctrl = 0x0d
	default:
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error in ax25_s_frame: Invalid ftype %d for S frame.\n", ftype)
		ax25_delete(this_p)
		return (nil)
	}

	var p = (*C.uchar)(unsafe.Add(unsafe.Pointer(&this_p.frame_data[0]), this_p.frame_len))

	if modulo == 8 {
		if pf != 0 {
			ctrl |= 0x10
		}
		ctrl |= nr << 5
		*p = C.uchar(ctrl)
		p = (*C.uchar)(unsafe.Add(unsafe.Pointer(p), 1))
		this_p.frame_len++
	} else {
		*p = C.uchar(ctrl)
		p = (*C.uchar)(unsafe.Add(unsafe.Pointer(p), 1))
		this_p.frame_len++

		ctrl = pf & 1
		ctrl |= nr << 1
		*p = C.uchar(ctrl)
		p = (*C.uchar)(unsafe.Add(unsafe.Pointer(p), 1))
		this_p.frame_len++
	}

	if ftype == frame_type_S_SREJ {
		if len(info) > 0 {
			if len(info) > AX25_MAX_INFO_LEN {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Internal error in ax25_s_frame: SREJ frame, Invalid information field length %d.\n", len(info))
				info = info[:AX25_MAX_INFO_LEN]
			}
			C.memcpy(unsafe.Pointer(p), C.CBytes(info), C.size_t(len(info)))
			p = (*C.uchar)(unsafe.Add(unsafe.Pointer(p), len(info)))
			this_p.frame_len += len(info)
		}
	} else {
		if len(info) > 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Internal error in ax25_s_frame: Info part not allowed for RR, RNR, REJ frame.\n")
		}
	}
	*p = 0

	// TODO KG Assert(p == this_p.frame_data+this_p.frame_len)
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	return (this_p)

} /* end ax25_s_frame */

/*------------------------------------------------------------------------------
 *
 * Name:	ax25_i_frame
 *
 * Purpose:	Construct an I frame.
 *
 * Input:	addrs		- Array of addresses.
 *
 *		num_addr	- Number of addresses, range 2 .. 10.
 *
 *		cr		- cr_cmd command frame, cr_res for a response frame.
 *
 *		modulo		- 8 or 128.
 *
 *		nr		- N(R) field --- describe.
 *
 *		ns		- N(S) field --- describe.
 *
 *		pf		- Poll/Final flag.
 *
 *		pid		- Protocol ID.
 *				  Normally 0xf0 meaning no level 3.
 *				  Could be other values for NET/ROM, etc.
 *
 *		info		- Info field.
 *
 *
 * Returns:	Pointer to new packet object.
 *
 *------------------------------------------------------------------------------*/

func ax25_i_frame(
	addrs [AX25_MAX_ADDRS][AX25_MAX_ADDR_LEN]C.char,
	num_addr C.int,
	cr cmdres_t,
	modulo ax25_modulo_t,
	nr int,
	ns int,
	pf int,
	pid int,
	info []byte,
) *packet_t {

	var this_p = ax25_new()

	if this_p == nil {
		return (nil)
	}

	if set_addrs(this_p, addrs, num_addr, cr) == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error in ax25_i_frame: Could not set addresses for I frame.\n")
		ax25_delete(this_p)
		return (nil)
	}

	if modulo != 8 && modulo != 128 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error in ax25_i_frame: Invalid modulo %d for I frame.\n", modulo)
		modulo = 8
	}
	this_p.modulo = modulo

	if nr < 0 || nr >= int(modulo) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error in ax25_i_frame: Invalid N(R) %d for I frame.\n", nr)
		nr &= int(modulo - 1)
	}

	if ns < 0 || ns >= int(modulo) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error in ax25_i_frame: Invalid N(S) %d for I frame.\n", ns)
		ns &= int(modulo - 1)
	}

	var p = (*C.uchar)(unsafe.Add(unsafe.Pointer(&this_p.frame_data[0]), this_p.frame_len))

	var ctrl int
	if modulo == 8 {
		ctrl = (nr << 5) | (ns << 1)
		if pf != 0 {
			ctrl |= 0x10
		}
		*p = C.uchar(ctrl)
		p = (*C.uchar)(unsafe.Add(unsafe.Pointer(p), 1))
		this_p.frame_len++
	} else {
		ctrl = ns << 1
		*p = C.uchar(ctrl)
		p = (*C.uchar)(unsafe.Add(unsafe.Pointer(p), 1))
		this_p.frame_len++

		ctrl = nr << 1
		if pf != 0 {
			ctrl |= 0x01
		}
		*p = C.uchar(ctrl)
		p = (*C.uchar)(unsafe.Add(unsafe.Pointer(p), 1))
		this_p.frame_len++
	}

	// Definitely don't want pid value of 0 (not in valid list)
	// or 0xff (which means more bytes follow).

	if pid < 0 || pid == 0 || pid == 0xff {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("Warning: Client application provided invalid PID value, 0x%02x, for I frame.\n", pid)
		pid = AX25_PID_NO_LAYER_3
	}
	*p = C.uchar(pid)
	p = (*C.uchar)(unsafe.Add(unsafe.Pointer(p), 1))
	this_p.frame_len++

	if len(info) > 0 {
		if len(info) > AX25_MAX_INFO_LEN {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Internal error in ax25_i_frame: I frame, Invalid information field length %d.\n", len(info))
			info = info[:AX25_MAX_INFO_LEN]
		}
		C.memcpy(unsafe.Pointer(p), C.CBytes(info), C.size_t(len(info)))
		p = (*C.uchar)(unsafe.Add(unsafe.Pointer(p), len(info)))
		this_p.frame_len += len(info)
	}

	*p = 0

	// TODO KG Assert(p == this_p.frame_data+this_p.frame_len)
	Assert(this_p.magic1 == MAGIC)
	Assert(this_p.magic2 == MAGIC)

	return (this_p)

} /* end ax25_i_frame */

/*------------------------------------------------------------------------------
 *
 * Name:	set_addrs
 *
 * Purpose:	Set address fields
 *
 * Input:	pp		- Packet object.
 *
 *		addrs		- Array of addresses.  Same order as in frame.
 *
 *		num_addr	- Number of addresses, range 2 .. 10.
 *
 *		cr		- cr_cmd command frame, cr_res for a response frame.
 *
 * Output:	pp.frame_data 	- 7 bytes for each address.
 *
 *		pp.frame_len	- num_addr * 7
 *
 *		p.num_addr	- num_addr
 *
 * Returns:	1 for success.  0 for failure.
 *
 *------------------------------------------------------------------------------*/

func set_addrs(pp *packet_t, addrs [AX25_MAX_ADDRS][AX25_MAX_ADDR_LEN]C.char, num_addr C.int, cr cmdres_t) C.int {

	Assert(pp.frame_len == 0)
	Assert(cr == cr_cmd || cr == cr_res)

	if num_addr < AX25_MIN_ADDRS || num_addr > AX25_MAX_ADDRS {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("INTERNAL ERROR: set_addrs, num_addr = %d\n", num_addr)
		return (0)
	}

	for n := C.int(0); n < num_addr; n++ {

		var pa = (*C.uchar)(unsafe.Add(unsafe.Pointer(&pp.frame_data[0]), n*7))
		var strictness = 1

		var oaddr, ssid, _, ok = ax25_parse_addr(int(n), C.GoString(&addrs[n][0]), strictness)

		if !ok {
			return (0)
		}

		// Fill in address.

		C.memset(unsafe.Pointer(pa), ' '<<1, 6)
		var pb = pa
		for _, c := range oaddr {
			*pb = C.uchar(c << 1)
			pb = (*C.uchar)(unsafe.Add(unsafe.Pointer(pb), 1))
		}
		pa = (*C.uchar)(unsafe.Add(unsafe.Pointer(pa), 6))

		// Fill in SSID.

		*pa = C.uchar(0x60 | ((ssid & 0xf) << 1))

		// Command / response flag.

		switch n {
		case AX25_DESTINATION:
			if cr == cr_cmd {
				*pa |= 0x80
			}
		case AX25_SOURCE:
			if cr == cr_res {
				*pa |= 0x80
			}
		default:
		}

		// Is this the end of address field?

		if n == num_addr-1 {
			*pa |= 1
		}

		pp.frame_len += 7
	}

	pp.num_addr = int(num_addr)
	return (1)
} /* end set_addrs */
