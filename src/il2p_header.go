package direwolf

// #include "direwolf.h"
// #include <assert.h>
// #include <string.h>
// #include <stdio.h>
// #include <ctype.h>
// #include "textcolor.h"
// #include "ax25_pad.h"
// #include "ax25_pad2.h"
// #include "il2p.h"
import "C"

import (
	"fmt"
	"unsafe"
)

/*--------------------------------------------------------------------------------
 *
 * Purpose:	Functions to deal with the IL2P header.
 *
 * Reference:	http://tarpn.net/t/il2p/il2p-specification0-4.pdf
 *
 *--------------------------------------------------------------------------------*/

// Convert ASCII to/from DEC SIXBIT as defined here:
// https://en.wikipedia.org/wiki/Six-bit_character_code#DEC_six-bit_code

func ascii_to_sixbit(a C.uchar) C.uchar {
	if a >= ' ' && a <= '_' {
		return (a - ' ')
	}
	return (31) // '?' for any invalid.
}

func sixbit_to_ascii(s C.uchar) C.uchar {
	return (s + ' ')
}

// Functions for setting the various header fields.
// It is assumed that it was zeroed first so only the '1' bits are set.

func set_il2p_field(hdr *C.uchar, bit_num C.int, lsb_index C.int, width C.int, value C.int) {
	for width > 0 && value != 0 {
		Assert(lsb_index >= 0 && lsb_index <= 11)
		if value&1 != 0 {
			var x = (*C.uchar)(unsafe.Add(unsafe.Pointer(hdr), lsb_index))
			*x |= C.uchar(1 << bit_num)
		}
		value >>= 1
		lsb_index--
		width--
	}
	Assert(value == 0)
}

func SET_UI(hdr *C.uchar, val C.int) {
	set_il2p_field(hdr, 6, 0, 1, val)
}

func SET_PID(hdr *C.uchar, val C.int) {
	set_il2p_field(hdr, 6, 4, 4, val)
}

func SET_CONTROL(hdr *C.uchar, val C.int) {
	set_il2p_field(hdr, 6, 11, 7, val)
}

func SET_FEC_LEVEL(hdr *C.uchar, val C.int) {
	set_il2p_field(hdr, 7, 0, 1, val)
}

func SET_HDR_TYPE(hdr *C.uchar, val C.int) {
	set_il2p_field(hdr, 7, 1, 1, val)
}

func SET_PAYLOAD_BYTE_COUNT(hdr *C.uchar, val C.int) {
	set_il2p_field(hdr, 7, 11, 10, val)
}

// Extracting the fields.

func get_il2p_field(hdr *C.uchar, bit_num C.int, lsb_index C.int, width C.int) C.int {
	var result C.int = 0
	lsb_index -= width - 1
	for width > 0 {
		result <<= 1
		Assert(lsb_index >= 0 && lsb_index <= 11)
		var x = (*C.uchar)(unsafe.Add(unsafe.Pointer(hdr), lsb_index))
		if *x&(1<<bit_num) != 0 {
			result |= 1
		}
		lsb_index++
		width--
	}
	return (result)
}

func GET_UI(hdr *C.uchar) C.int {
	return get_il2p_field(hdr, 6, 0, 1)
}

func GET_PID(hdr *C.uchar) C.int {
	return get_il2p_field(hdr, 6, 4, 4)
}

func GET_CONTROL(hdr *C.uchar) C.int {
	return get_il2p_field(hdr, 6, 11, 7)
}

func GET_FEC_LEVEL(hdr *C.uchar) C.int {
	return get_il2p_field(hdr, 7, 0, 1)
}

func GET_HDR_TYPE(hdr *C.uchar) C.int {
	return get_il2p_field(hdr, 7, 1, 1)
}

func GET_PAYLOAD_BYTE_COUNT(hdr *C.uchar) C.int {
	return get_il2p_field(hdr, 7, 11, 10)
}

// AX.25 'I' and 'UI' frames have a protocol ID which determines how the
// information part should be interpreted.
// Here we squeeze the most common cases down to 4 bits.
// Return -1 if translation is not possible.  Fall back to type 0 header in this case.

func encode_pid(pp C.packet_t) C.int {
	var pid = ax25_get_pid(pp)

	if (pid & 0x30) == 0x20 {
		return (0x2) // AX.25 Layer 3
	}
	if (pid & 0x30) == 0x10 {
		return (0x2) // AX.25 Layer 3
	}
	if pid == 0x01 {
		return (0x3) // ISO 8208 / CCIT X.25 PLP
	}
	if pid == 0x06 {
		return (0x4) // Compressed TCP/IP
	}
	if pid == 0x07 {
		return (0x5) // Uncompressed TCP/IP
	}
	if pid == 0x08 {
		return (0x6) // Segmentation fragmen
	}
	if pid == 0xcc {
		return (0xb) // ARPA Internet Protocol
	}
	if pid == 0xcd {
		return (0xc) // ARPA Address Resolution
	}
	if pid == 0xce {
		return (0xd) // FlexNet
	}
	if pid == 0xcf {
		return (0xe) // TheNET
	}
	if pid == 0xf0 {
		return (0xf) // No L3
	}
	return (-1)
}

// Convert IL2P 4 bit PID to AX.25 8 bit PID.

func decode_pid(pid C.int) C.int {
	var axpid = [16]C.int{
		0xf0, // Should not happen. 0 is for 'S' frames.
		0xf0, // Should not happen. 1 is for 'U' frames (but not UI).
		0x20, // AX.25 Layer 3
		0x01, // ISO 8208 / CCIT X.25 PLP
		0x06, // Compressed TCP/IP
		0x07, // Uncompressed TCP/IP
		0x08, // Segmentation fragment
		0xf0, // Future
		0xf0, // Future
		0xf0, // Future
		0xf0, // Future
		0xcc, // ARPA Internet Protocol
		0xcd, // ARPA Address Resolution
		0xce, // FlexNet
		0xcf, // TheNET
		0xf0, // No L3
	}

	Assert(pid >= 0 && pid <= 15)

	return (axpid[pid])
}

/*--------------------------------------------------------------------------------
 *
 * Function:	il2p_type_1_header
 *
 * Purpose:	Attempt to create type 1 header from packet object.
 *
 * Inputs:	pp	- Packet object.
 *
 *		max_fec	- 1 to use maximum FEC symbols , 0 for automatic.
 *
 * Outputs:	hdr	- IL2P header with no scrambling or parity symbols.
 *			  Must be large enough to hold IL2P_HEADER_SIZE unsigned bytes.
 *
 * Returns:	Number of bytes for information part or -1 for failure.
 *		In case of failure, fall back to type 0 transparent encapsulation.
 *
 * Description:	Type 1 Headers do not support AX.25 repeater callsign addressing,
 *		Modulo-128 extended mode window sequence numbers, nor any callsign
 *		characters that cannot translate to DEC SIXBIT.
 *		If these cases are encountered during IL2P packet encoding,
 *		the encoder switches to Type 0 Transparent Encapsulation.
 *		SABME can't be handled by type 1.
 *
 *--------------------------------------------------------------------------------*/

//export il2p_type_1_header
func il2p_type_1_header(pp C.packet_t, max_fec C.int, hdr *C.uchar) C.int {
	C.memset(unsafe.Pointer(hdr), 0, IL2P_HEADER_SIZE)

	if ax25_get_num_addr(pp) != 2 {
		// Only two addresses are allowed for type 1 header.
		return (-1)
	}

	// Check does not apply for 'U' frames but put in one place rather than two.

	if ax25_get_modulo(pp) == 128 {
		return (-1)
	}

	// Destination and source addresses go into low bits 0-5 for bytes 0-11.

	var dst_addr [AX25_MAX_ADDR_LEN]C.char
	var src_addr [AX25_MAX_ADDR_LEN]C.char

	ax25_get_addr_no_ssid(pp, AX25_DESTINATION, &dst_addr[0])
	var dst_ssid = ax25_get_ssid(pp, AX25_DESTINATION)

	ax25_get_addr_no_ssid(pp, AX25_SOURCE, &src_addr[0])
	var src_ssid = ax25_get_ssid(pp, AX25_SOURCE)

	for i := 0; ; i++ {
		var a = (*C.uchar)(unsafe.Add(unsafe.Pointer(&dst_addr[0]), i))
		if *a == 0 {
			break
		}
		if *a < ' ' || *a > '_' {
			// Shouldn't happen but follow the rule.
			return (-1)
		}
		var h = (*C.uchar)(unsafe.Add(unsafe.Pointer(hdr), i))
		*h = ascii_to_sixbit(*a)
	}

	for i := 0; ; i++ {
		var a = (*C.uchar)(unsafe.Add(unsafe.Pointer(&src_addr[0]), i))
		if *a == 0 {
			break
		}
		if *a < ' ' || *a > '_' {
			// Shouldn't happen but follow the rule.
			return (-1)
		}
		var h = (*C.uchar)(unsafe.Add(unsafe.Pointer(hdr), 6+i))
		*h = ascii_to_sixbit(*a)
	}

	// Byte 12 has DEST SSID in upper nybble and SRC SSID in lower nybble and
	var x = (*C.uchar)(unsafe.Add(unsafe.Pointer(hdr), 12))
	*x = C.uchar((dst_ssid << 4) | src_ssid)

	var cr C.cmdres_t // command or response.
	var description [64]C.char
	var pf C.int     // Poll/Final.
	var nr, ns C.int // Sequence numbers.

	var frame_type = ax25_frame_type(pp, &cr, &description[0], &pf, &nr, &ns)

	//dw_printf ("%s(): %s-%d>%s-%d: %s\n", __func__, src_addr, src_ssid, dst_addr, dst_ssid, description);

	switch frame_type {

	case frame_type_S_RR, frame_type_S_RNR, frame_type_S_REJ, frame_type_S_SREJ:
		// Receive Ready - System Ready To Receive
		// Receive Not Ready - TNC Buffer Full
		// Reject Frame - Out of Sequence or Duplicate
		// Selective Reject - Request single frame repeat

		// S frames (RR, RNR, REJ, SREJ), mod 8, have control N(R) P/F S S 0 1
		// These are mapped into    P/F N(R) C S S
		// Bit 6 is not mentioned in documentation but it is used for P/F for the other frame types.
		// C is copied from the C bit in the destination addr.
		// C from source is not used here.  Reception assumes it is the opposite.
		// PID is set to 0, meaning none, for S frames.

		SET_UI(hdr, 0)
		SET_PID(hdr, 0)
		SET_CONTROL(hdr, (pf<<6)|(nr<<3)|(((IfThenElse((cr == cr_cmd), C.int(1), C.int(0)))|(IfThenElse((cr == cr_11), C.int(1), C.int(0))))<<2))

		// This gets OR'ed into the above.
		switch frame_type {
		case frame_type_S_RR:
			SET_CONTROL(hdr, 0)
		case frame_type_S_RNR:
			SET_CONTROL(hdr, 1)
		case frame_type_S_REJ:
			SET_CONTROL(hdr, 2)
		case frame_type_S_SREJ:
			SET_CONTROL(hdr, 3)
		default:
		}

	case frame_type_U_SABM, frame_type_U_DISC, frame_type_U_DM, frame_type_U_UA, frame_type_U_FRMR, frame_type_U_UI, frame_type_U_XID, frame_type_U_TEST:
		// Set Async Balanced Mode
		// Disconnect
		// Disconnect Mode
		// Unnumbered Acknowledge
		// Frame Reject
		// Unnumbered Information
		// Exchange Identification
		// Test

		// The encoding allows only 3 bits for frame type and SABME got left out.
		// Control format:  P/F opcode[3] C n/a n/a
		// The grayed out n/a bits are observed as 00 in the example.
		// The header UI field must also be set for UI frames.
		// PID is set to 1 for all U frames other than UI.

		if frame_type == frame_type_U_UI {
			SET_UI(hdr, 1) // I guess this is how we distinguish 'I' and 'UI'
			// on the receiving end.
			var pid = encode_pid(pp)
			if pid < 0 {
				return (-1)
			}
			SET_PID(hdr, pid)
		} else {
			SET_PID(hdr, 1) // 1 for 'U' other than 'UI'.
		}

		// Each of the destination and source addresses has a "C" bit.
		// They should normally have the opposite setting.
		// IL2P has only a single bit to represent 4 possbilities.
		//
		//	dst	src	il2p	meaning
		//	---	---	----	-------
		//	0	0	0	Not valid (earlier protocol version)
		//	1	0	1	Command (v2)
		//	0	1	0	Response (v2)
		//	1	1	1	Not valid (earlier protocol version)
		//
		// APRS does not mention how to set these bits and all 4 combinations
		// are seen in the wild.  Apparently these are ignored on receive and no
		// one cares.  Here we copy from the C bit in the destination address.
		// It should be noted that the case of both C bits being the same can't
		// be represented so the il2p encode/decode bit not produce exactly the
		// same bits.  We see this in the second example in the protocol spec.
		// The original UI frame has both C bits of 0 so it is received as a response.

		SET_CONTROL(hdr, (pf<<6)|(((IfThenElse((cr == cr_cmd), C.int(1), C.int(0)))|(IfThenElse((cr == cr_11), C.int(1), C.int(0))))<<2))

		// This gets OR'ed into the above.
		switch frame_type {
		case frame_type_U_SABM:
			SET_CONTROL(hdr, 0<<3)
		case frame_type_U_DISC:
			SET_CONTROL(hdr, 1<<3)
		case frame_type_U_DM:
			SET_CONTROL(hdr, 2<<3)
		case frame_type_U_UA:
			SET_CONTROL(hdr, 3<<3)
		case frame_type_U_FRMR:
			SET_CONTROL(hdr, 4<<3)
		case frame_type_U_UI:
			SET_CONTROL(hdr, 5<<3)
		case frame_type_U_XID:
			SET_CONTROL(hdr, 6<<3)
		case frame_type_U_TEST:
			SET_CONTROL(hdr, 7<<3)
		default:
		}

	case frame_type_I: // Information

		// I frames (mod 8 only)
		// encoded control: P/F N(R) N(S)

		SET_UI(hdr, 0)

		var pid2 = encode_pid(pp)
		if pid2 < 0 {
			return (-1)
		}
		SET_PID(hdr, pid2)

		SET_CONTROL(hdr, (pf<<6)|(nr<<3)|ns)

	default:
		// case frame_type_U_SABME:		// Set Async Balanced Mode, Extended
		// case frame_type_U:			// other Unnumbered, not used by AX.25.
		// case frame_not_AX25:		// Could not get control byte from frame.

		// Fall back to the header type 0 for these.
		return (-1)
	}

	// Common for all header type 1.

	// Bit 7 has [FEC Level:1], [HDR Type:1], [Payload byte Count:10]

	SET_FEC_LEVEL(hdr, max_fec)
	SET_HDR_TYPE(hdr, 1)

	var pinfo *C.uchar

	var info_len = ax25_get_info(pp, &pinfo)
	if info_len < 0 || info_len > IL2P_MAX_PAYLOAD_SIZE {
		return (-2)
	}

	SET_PAYLOAD_BYTE_COUNT(hdr, info_len)
	return (info_len)
}

// This should create a packet from the IL2P header.
// The information part will not be filled in.

func trim(stuff *C.char) {
	if C.strlen(stuff) == 0 {
		return
	}

	// strlen >= 1

	for i := C.strlen(stuff) - 1; ; i-- {
		if i == 0 {
			break
		}

		var p = (*C.char)(unsafe.Add(unsafe.Pointer(stuff), i))
		if *p == ' ' {
			*p = 0
		} else {
			break
		}
	}
}

/*--------------------------------------------------------------------------------
 *
 * Function:	il2p_decode_header_type_1
 *
 * Purpose:	Attempt to convert type 1 header to a packet object.
 *
 * Inputs:	hdr - IL2P header with no scrambling or parity symbols.
 *
 *		num_sym_changed - Number of symbols changed by FEC in the header.
 *				Should be 0 or 1.
 *
 * Returns:	Packet Object or nil for failure.
 *
 * Description:	A later step will process the payload for the information part.
 *
 *--------------------------------------------------------------------------------*/

//export il2p_decode_header_type_1
func il2p_decode_header_type_1(hdr *C.uchar, num_sym_changed C.int) C.packet_t {

	if GET_HDR_TYPE(hdr) != 1 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("IL2P Internal error.  Should not be here: il2p_decode_header_type_1, when header type is 0.\n")
		return (nil)
	}

	// First get the addresses including SSID.

	var addrs [AX25_MAX_ADDRS][AX25_MAX_ADDR_LEN]C.char
	var num_addr C.int = 2
	C.memset(unsafe.Pointer(&addrs[0]), 0, 2*AX25_MAX_ADDR_LEN)

	// The IL2P header uses 2 parity symbols which means a single corrupted symbol (byte)
	// can always be corrected.
	// However, I have seen cases, where the error rate is very high, where the RS decoder
	// thinks it found a valid code block by changing one symbol but it was the wrong one.
	// The result is trash.  This shows up as address fields like 'R&G4"A' and 'TEW\ !'.
	// I added a sanity check here to catch characters other than upper case letters and digits.
	// The frame should be rejected in this case.  The question is whether to discard it
	// silently or print a message so the user can see that something strange is happening?
	// My current thinking is that it should be silently ignored if the header has been
	// modified (correctee or more likely, made worse in this cases).
	// If no changes were made, something weird is happening.  We should mention it for
	// troubleshooting rather than sweeping it under the rug.

	// The same thing has been observed with the payload, under very high error conditions,
	// and max_fec==0.  Here I don't see a good solution.  AX.25 information can contain
	// "binary" data so I'm not sure what sort of sanity check could be added.
	// This was not observed with max_fec==1.  If we make that the default, same as Nino TNC,
	// it would be extremely extremely unlikely unless someone explicitly selects weaker FEC.

	// TODO: We could do something similar for header type 0.
	// The address fields should be all binary zero values.
	// Someone overly ambitious might check the addresses found in the first payload block.

	for i := 0; i <= 5; i++ {
		addrs[AX25_DESTINATION][i] = C.char(sixbit_to_ascii(*(*C.uchar)(unsafe.Add(unsafe.Pointer(hdr), i)) & C.uchar(0x3f)))
	}
	trim(&addrs[AX25_DESTINATION][0])
	for i := C.int(0); i < C.int(C.strlen(&addrs[AX25_DESTINATION][0])); i++ {
		if C.isupper(C.int(addrs[AX25_DESTINATION][i])) == 0 && C.isdigit(C.int(addrs[AX25_DESTINATION][i])) == 0 {
			if num_sym_changed == 0 { //nolint:staticcheck
				// This can pop up sporadically when receiving random noise.
				// Would be better to show only when debug is enabled but variable not available here.
				// TODO: For now we will just suppress it.
				//text_color_set(DW_COLOR_ERROR);
				//dw_printf ("IL2P: Invalid character '%c' in destination address '%s'\n", addrs[AX25_DESTINATION][i], addrs[AX25_DESTINATION]);
			}
			return (nil)
		}
	}
	var destSSID = int((*(*C.uchar)(unsafe.Add(unsafe.Pointer(hdr), 12)) >> 4) & 0xf)
	C.strcat(&addrs[AX25_DESTINATION][0], C.CString(fmt.Sprintf("-%d", destSSID))) // TODO KG This is a bit grim and makes some assumptions about lengths but should do for now

	for i := 0; i <= 5; i++ {
		addrs[AX25_SOURCE][i] = C.char(sixbit_to_ascii(*(*C.uchar)(unsafe.Add(unsafe.Pointer(hdr), i+6)) & C.uchar(0x3f)))
	}
	trim(&addrs[AX25_SOURCE][0])
	for i := C.int(0); i < C.int(C.strlen(&addrs[AX25_SOURCE][0])); i++ {
		if C.isupper(C.int(addrs[AX25_SOURCE][i])) == 0 && C.isdigit(C.int(addrs[AX25_SOURCE][i])) == 0 {
			if num_sym_changed == 0 { //nolint:staticcheck
				// This can pop up sporadically when receiving random noise.
				// Would be better to show only when debug is enabled but variable not available here.
				// TODO: For now we will just suppress it.
				//text_color_set(DW_COLOR_ERROR);
				//dw_printf ("IL2P: Invalid character '%c' in source address '%s'\n", addrs[AX25_SOURCE][i], addrs[AX25_SOURCE]);
			}
			return (nil)
		}
	}
	var srcSSID = int((*(*C.uchar)(unsafe.Add(unsafe.Pointer(hdr), 12))) & 0xf)
	C.strcat(&addrs[AX25_SOURCE][0], C.CString(fmt.Sprintf("-%d", srcSSID))) // TODO KG This is a bit grim and makes some assumptions about lengths but should do for now

	// The PID field gives us the general type.
	// 0 = 'S' frame.
	// 1 = 'U' frame other than UI.
	// others are either 'UI' or 'I' depending on the UI field.

	var pid = GET_PID(hdr)
	var ui = GET_UI(hdr)

	if pid == 0 {

		// 'S' frame.
		// The control field contains: P/F N(R) C S S

		var control = GET_CONTROL(hdr)
		var cr = IfThenElse((control&0x04) != 0, cr_cmd, cr_res)
		var ftype ax25_frame_type_t
		switch control & 0x03 {
		case 0:
			ftype = frame_type_S_RR
		case 1:
			ftype = frame_type_S_RNR
		case 2:
			ftype = frame_type_S_REJ
		default:
			ftype = frame_type_S_SREJ
		}
		var modulo C.int = 8
		var nr = (control >> 3) & 0x07
		var pf = (control >> 6) & 0x01
		var pinfo *C.uchar // Any info for SREJ will be added later.
		var info_len C.int = 0
		return (ax25_s_frame(addrs, num_addr, cr, ftype, modulo, nr, pf, pinfo, info_len))
	} else if pid == 1 {

		// 'U' frame other than 'UI'.
		// The control field contains: P/F OPCODE{3) C x x

		var control = GET_CONTROL(hdr)
		var cr = IfThenElse((control&0x04) != 0, cr_cmd, cr_res)
		var axpid C.int = 0 // unused for U other than UI.
		var ftype ax25_frame_type_t
		switch (control >> 3) & 0x7 {
		case 0:
			ftype = frame_type_U_SABM
		case 1:
			ftype = frame_type_U_DISC
		case 2:
			ftype = frame_type_U_DM
		case 3:
			ftype = frame_type_U_UA
		case 4:
			ftype = frame_type_U_FRMR
		case 5:
			ftype = frame_type_U_UI
			axpid = 0xf0
			// Should not happen with IL2P pid == 1.
		case 6:
			ftype = frame_type_U_XID
		default:
			ftype = frame_type_U_TEST
		}
		var pf = (control >> 6) & 0x01
		var pinfo *C.uchar // Any info for UI, XID, TEST will be added later.
		var info_len C.int = 0
		return (ax25_u_frame(addrs, num_addr, cr, ftype, pf, axpid, pinfo, info_len))
	} else if ui != 0 {

		// 'UI' frame.
		// The control field contains: P/F OPCODE{3) C x x

		var control = GET_CONTROL(hdr)
		var cr = IfThenElse((control&0x04) != 0, cr_cmd, cr_res)
		var ftype = frame_type_U_UI
		var pf = (control >> 6) & 0x01
		var axpid = decode_pid(GET_PID(hdr))
		var pinfo *C.uchar // Any info for UI, XID, TEST will be added later.
		var info_len C.int = 0
		return (ax25_u_frame(addrs, num_addr, cr, ftype, pf, axpid, pinfo, info_len))
	} else {

		// 'I' frame.
		// The control field contains: P/F N(R) N(S)

		var control = GET_CONTROL(hdr)
		var cr = cr_cmd // Always command.
		var pf = (control >> 6) & 0x01
		var nr = (control >> 3) & 0x7
		var ns = control & 0x7
		var modulo C.int = 8
		var axpid = decode_pid(GET_PID(hdr))
		var pinfo *C.uchar // Any info for UI, XID, TEST will be added later.
		var info_len C.int = 0
		return (ax25_i_frame(addrs, num_addr, cr, modulo, nr, ns, pf, axpid, pinfo, info_len))
	}
} // end

/*--------------------------------------------------------------------------------
 *
 * Function:	il2p_type_0_header
 *
 * Purpose:	Attempt to create type 0 header from packet object.
 *
 * Inputs:	pp	- Packet object.
 *
 *		max_fec	- 1 to use maximum FEC symbols, 0 for automatic.
 *
 * Outputs:	hdr	- IL2P header with no scrambling or parity symbols.
 *			  Must be large enough to hold IL2P_HEADER_SIZE unsigned bytes.
 *
 * Returns:	Number of bytes for information part or -1 for failure.
 *		In case of failure, fall back to type 0 transparent encapsulation.
 *
 * Description:	The type 0 header is used when it is not one of the restricted cases
 *		covered by the type 1 header.
 *		The AX.25 frame is put in the payload.
 *		This will cover: more than one address, mod 128 sequences, etc.
 *
 *--------------------------------------------------------------------------------*/

//export il2p_type_0_header
func il2p_type_0_header(pp C.packet_t, max_fec C.int, hdr *C.uchar) C.int {
	C.memset(unsafe.Pointer(hdr), 0, IL2P_HEADER_SIZE)

	// Bit 7 has [FEC Level:1], [HDR Type:1], [Payload byte Count:10]

	SET_FEC_LEVEL(hdr, max_fec)
	SET_HDR_TYPE(hdr, 0)

	var frame_len = ax25_get_frame_len(pp)

	if frame_len < 14 || frame_len > IL2P_MAX_PAYLOAD_SIZE {
		return (-2)
	}

	SET_PAYLOAD_BYTE_COUNT(hdr, frame_len)
	return (frame_len)
}

/***********************************************************************************
 *
 * Name:        il2p_get_header_attributes
 *
 * Purpose:     Extract a few attributes from an IL2p header.
 *
 * Inputs:      hdr	- IL2P header structure.
 *
 * Outputs:     hdr_type - 0 or 1.
 *
 *		max_fec	- 0 for automatic or 1 for fixed maximum size.
 *
 * Returns:	Payload byte count.   (actual payload size, not the larger encoded format)
 *
 ***********************************************************************************/

//export il2p_get_header_attributes
func il2p_get_header_attributes(hdr *C.uchar, hdr_type *C.int, max_fec *C.int) C.int {
	*hdr_type = GET_HDR_TYPE(hdr)
	*max_fec = GET_FEC_LEVEL(hdr)
	return (GET_PAYLOAD_BYTE_COUNT(hdr))
}

/***********************************************************************************
 *
 * Name:        il2p_clarify_header
 *
 * Purpose:     Convert received header to usable form.
 *		This involves RS FEC then descrambling.
 *
 * Inputs:      rec_hdr	- Header as received over the radio.
 *
 * Outputs:     corrected_descrambled_hdr - After RS FEC and unscrambling.
 *
 * Returns:	Number of symbols that were corrected:
 *		 0 = No errors
 *		 1 = Single symbol corrected.
 *		 <0 = Unable to obtain good header.
 *
 ***********************************************************************************/

//export il2p_clarify_header
func il2p_clarify_header(rec_hdr *C.uchar, corrected_descrambled_hdr *C.uchar) C.int {
	var corrected [IL2P_HEADER_SIZE + IL2P_HEADER_PARITY]C.uchar

	var e = C.il2p_decode_rs(rec_hdr, IL2P_HEADER_SIZE, IL2P_HEADER_PARITY, &corrected[0])

	il2p_descramble_block(&corrected[0], corrected_descrambled_hdr, IL2P_HEADER_SIZE)

	return (e)
}
