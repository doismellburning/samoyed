package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Encode and decode the info field of XID frames.
 *
 * Description:	If we originate the connection, and the other end is
 *		capable of AX.25 version 2.2,
 *
 *		 - We send an XID command frame with our capabilities.
 *		 - the other sends back an XID response, possibly
 *			reducing some values to be acceptable there.
 *		 - Both ends use the values in that response.
 *
 *		If the other end originates the connection,
 *
 *		  - It sends XID command frame with its capabilities.
 *		  - We might decrease some of them to be acceptable.
 *		  - Send XID response.
 *		  - Both ends use values in my response.
 *
 * References:	AX.25 Protocol Spec, sections 4.3.3.7 & 6.3.2.
 *
 *---------------------------------------------------------------*/

// #include <stdlib.h>
// #include <string.h>
// #include <assert.h>
// #include <stdio.h>
// #include <unistd.h>
import "C"

import (
	"fmt"
)

const FI_Format_Indicator = 0x82
const GI_Group_Identifier = 0x80

const PI_Classes_of_Procedures = 2
const PI_HDLC_Optional_Functions = 3
const PI_I_Field_Length_Rx = 6
const PI_Window_Size_Rx = 8
const PI_Ack_Timer = 9
const PI_Retries = 10

// Forget about the bit order at the physical layer (e.g. HDLC).
// It doesn't matter at all here.  We are dealing with bytes.
// A different encoding could send the bits in the opposite order.

// The bit numbers are confusing because this one table (Fig. 4.5) starts
// with 1 for the LSB when everywhere else refers to the LSB as bit 0.

// The first byte must be of the form	0xx0 0001
// The second byte must be of the form	0000 0000
// If we process the two byte "Classes of Procedures" like
// the other multibyte numeric fields, with the more significant
// byte first, we end up with the bit masks below.
// The bit order would be  8 7 6 5 4 3 2 1   16 15 14 13 12 11 10 9

// (This has nothing to do with the HDLC serializing order.
// I'm talking about the way we would normally write binary numbers.)

const PV_Classes_Procedures_Balanced_ABM = 0x0100
const PV_Classes_Procedures_Half_Duplex = 0x2000
const PV_Classes_Procedures_Full_Duplex = 0x4000

// The first byte must be of the form	1000 0xx0
// The second byte must be of the form	1010 xx00
// The third byte must be of the form	0000 0010
// If we process the three byte "HDLC Optional Parameters" like
// the other multibyte numeric fields, with the most significant
// byte first, we end up with bit masks like this.
// The bit order would be  8 7 6 5 4 3 2 1   16 15 14 13 12 11 10 9   24 23 22 21 20 19 18 17

const PV_HDLC_Optional_Functions_REJ_cmd_resp = 0x020000
const PV_HDLC_Optional_Functions_SREJ_cmd_resp = 0x040000
const PV_HDLC_Optional_Functions_Extended_Address = 0x800000

const PV_HDLC_Optional_Functions_Modulo_8 = 0x000400
const PV_HDLC_Optional_Functions_Modulo_128 = 0x000800
const PV_HDLC_Optional_Functions_TEST_cmd_resp = 0x002000
const PV_HDLC_Optional_Functions_16_bit_FCS = 0x008000

const PV_HDLC_Optional_Functions_Multi_SREJ_cmd_resp = 0x000020
const PV_HDLC_Optional_Functions_Segmenter = 0x000040

const PV_HDLC_Optional_Functions_Synchronous_Tx = 0x000002

type srej_e int

// Order is important because negotiation keeps the lower value of
// REJ  (srej_none),  SREJ (default without negotiation), Multi-SREJ (if both agree).

const (
	srej_none          srej_e = 0
	srej_single        srej_e = 1
	srej_multi         srej_e = 2
	srej_not_specified srej_e = 3
)

type xid_param_s struct {
	full_duplex C.int

	srej srej_e

	modulo ax25_modulo_t

	i_field_length_rx C.int /* In bytes.  XID has it in bits. */

	window_size_rx C.int

	ack_timer C.int /* "T1" in mSec. */

	retries C.int /* "N1" */
}

/*-------------------------------------------------------------------
 *
 * Name:        xid_parse
 *
 * Purpose:    	Decode information part of XID frame into individual values.
 *
 * Inputs:	info		- pointer to information part of frame.
 *
 *		info_len	- Number of bytes in information part of frame.
 *				  Could be 0.
 *
 * Returns:	result		- Structure with extracted values.
 *
 *		desc		- Text description for troubleshooting.
 *
 * statusNo -	1 for mostly successful (with possible error messages), 0 for failure.
 *
 * Description:	6.3.2 "The receipt of an XID response from the other station
 *		establishes that both stations are using AX.25 version
 *		2.2 or higher and enables the use of the segmenter/reassembler
 *		and selective reject."
 *
 *--------------------------------------------------------------------*/

func xid_parse(info []byte) (*xid_param_s, string, int) {

	// What should we do when some fields are missing?

	// The  AX.25 v2.2 protocol spec says, for most of these,
	//	"If this field is not present, the current values are retained."

	// We set the numeric values to our usual G_UNKNOWN to mean undefined and let the caller deal with it.
	// rej and modulo are enum so we can't use G_UNKNOWN there.

	var result = new(xid_param_s)

	result.full_duplex = G_UNKNOWN
	result.srej = srej_not_specified
	result.modulo = modulo_unknown
	result.i_field_length_rx = G_UNKNOWN
	result.window_size_rx = G_UNKNOWN
	result.ack_timer = G_UNKNOWN
	result.retries = G_UNKNOWN

	var desc string

	/* Information field is optional but that seems pretty lame. */

	if len(info) == 0 {
		return result, desc, 1
	}

	var i = 0

	if info[i] != FI_Format_Indicator {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("XID error: First byte of info field should be Format Indicator, %02x.\n", FI_Format_Indicator)
		dw_printf("XID info part: %02x %02x %02x %02x %02x ... length=%d\n", info[0], info[1], info[2], info[3], info[4], len(info))
		return result, desc, 0
	}
	i++

	if info[i] != GI_Group_Identifier {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("XID error: Second byte of info field should be Group Indicator, %d.\n", GI_Group_Identifier)
		return result, desc, 0
	}
	i++

	var group_len = C.int(info[i])
	i++
	group_len = (group_len << 8) + C.int(info[i])
	i++

	for C.int(i) < 4+group_len {

		var pind = info[i]
		i++

		var plen = info[i] // should have sanity checking
		i++

		if plen < 1 || plen > 4 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("XID error: Length ?????   TODO   ????  %d.\n", plen)
			return result, desc, 1 // got this far.
		}

		var pval C.int = 0
		for j := byte(0); j < plen; j++ {
			pval = (pval << 8) + C.int(info[i])
			i++
		}

		switch pind {

		case PI_Classes_of_Procedures:

			if (pval & PV_Classes_Procedures_Balanced_ABM) == 0 { //nolint:staticcheck
				//  https://groups.io/g/bpq32/topic/113348033#msg44169
				//text_color_set (DW_COLOR_ERROR);
				//dw_printf ("XID error: Expected Balanced ABM to be set.\n");
			}

			if pval&PV_Classes_Procedures_Half_Duplex > 0 && (pval&PV_Classes_Procedures_Full_Duplex) == 0 {
				result.full_duplex = 0
				desc += "Half-Duplex "
			} else if pval&PV_Classes_Procedures_Full_Duplex > 0 && (pval&PV_Classes_Procedures_Half_Duplex) == 0 {
				result.full_duplex = 1
				desc += "Full-Duplex "
			} else {
				//  https://groups.io/g/bpq32/topic/113348033#msg44169
				//text_color_set (DW_COLOR_ERROR);
				//dw_printf ("XID error: Expected one of Half or Full Duplex be set.\n");
				result.full_duplex = 0
			}

		case PI_HDLC_Optional_Functions:

			// Pick highest of those offered.

			if pval&PV_HDLC_Optional_Functions_REJ_cmd_resp > 0 {
				desc += "REJ "
			}
			if pval&PV_HDLC_Optional_Functions_SREJ_cmd_resp > 0 {
				desc += "SREJ "
			}
			if pval&PV_HDLC_Optional_Functions_Multi_SREJ_cmd_resp > 0 {
				desc += "Multi-SREJ "
			}

			if pval&PV_HDLC_Optional_Functions_Multi_SREJ_cmd_resp > 0 {
				result.srej = srej_multi
			} else if pval&PV_HDLC_Optional_Functions_SREJ_cmd_resp > 0 {
				result.srej = srej_single
			} else if pval&PV_HDLC_Optional_Functions_REJ_cmd_resp > 0 {
				result.srej = srej_none
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("XID error: Expected at least one of REJ, SREJ, Multi-SREJ to be set.\n")
				result.srej = srej_none
			}

			if (pval&PV_HDLC_Optional_Functions_Modulo_8) > 0 && (pval&PV_HDLC_Optional_Functions_Modulo_128) == 0 {
				result.modulo = modulo_8
				desc += "modulo-8 "
			} else if (pval&PV_HDLC_Optional_Functions_Modulo_128) > 0 && (pval&PV_HDLC_Optional_Functions_Modulo_8) == 0 {
				result.modulo = modulo_128
				desc += "modulo-128 "
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("XID error: Expected one of Modulo 8 or 128 be set.\n")
			}

			if (pval & PV_HDLC_Optional_Functions_Extended_Address) == 0 { //nolint:staticcheck
				//  https://groups.io/g/bpq32/topic/113348033#msg44169
				//text_color_set (DW_COLOR_ERROR);
				//dw_printf ("XID error: Expected Extended Address to be set.\n");
			}

			if (pval & PV_HDLC_Optional_Functions_TEST_cmd_resp) == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("XID error: Expected TEST cmd/resp to be set.\n")
			}

			if (pval & PV_HDLC_Optional_Functions_16_bit_FCS) == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("XID error: Expected 16 bit FCS to be set.\n")
			}

			if (pval & PV_HDLC_Optional_Functions_Synchronous_Tx) == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("XID error: Expected Synchronous Tx to be set.\n")
			}

		case PI_I_Field_Length_Rx:

			result.i_field_length_rx = pval / 8

			desc += fmt.Sprintf("I-Field-Length-Rx=%d ", result.i_field_length_rx)

			if pval&0x7 > 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("XID error: I Field Length Rx, %d, is not a whole number of bytes.\n", pval)
			}

		case PI_Window_Size_Rx:

			result.window_size_rx = pval

			desc += fmt.Sprintf("Window-Size-Rx=%d ", result.window_size_rx)

			if pval < 1 || pval > 127 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("XID error: Window Size Rx, %d, is not in range of 1 thru 127.\n", pval)
				result.window_size_rx = 127
				// Let the caller deal with modulo 8 consideration.
			}

			//continue here with more error checking.

		case PI_Ack_Timer:
			result.ack_timer = pval

			desc += fmt.Sprintf("Ack-Timer=%d ", result.ack_timer)

		case PI_Retries: // Is it retrys or retries?
			result.retries = pval

			desc += fmt.Sprintf("Retries=%d ", result.retries)

		default: // Ignore anything we don't recognize.
		}
	}

	if i != len(info) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("XID error: Frame / Group Length mismatch.\n")
	}

	return result, desc, 1

} /* end xid_parse */

/*-------------------------------------------------------------------
 *
 * Name:        xid_encode
 *
 * Purpose:    	Encode the information part of an XID frame.
 *
 * Inputs:	param.
 *			full_duplex	- As command, am I capable of full duplex operation?
 *					  When a response, are we both?
 *					  0 = half duplex.
 *					  1 = full duplex.
 *
 * 			srej		- Level of selective reject.
 *					  srej_none (use REJ), srej_single, srej_multi
 *					  As command, offer a menu of what I can handle.  (i.e. perhaps multiple bits set)
 *					  As response, take minimum of what is offered and what I can handle. (one bit set)
 *
 *			modulo	- 8 or 128.
 *
 *			i_field_length_rx - Maximum number of bytes I can handle in info part.
 *					    Default is 256.
 *					    Up to 8191 will fit into the field.
 *					    Use G_UNKNOWN to omit this.
 *
 *			window_size_rx 	- Maximum window size ("k") that I can handle.
 *				   Defaults are are 4 for modulo 8 and 32 for modulo 128.
 *
 *			ack_timer	- Acknowledge timer in milliseconds.
 *					*** describe meaning.  ***
 *				  Default is 3000.
 *				  Use G_UNKNOWN to omit this.
 *
 *			retries		- Allows negotiation of retries.
 *				  Default is 10.
 *				  Use G_UNKNOWN to omit this.
 *
 *		cr	- Is it a command or response?
 *
 * Returns:	info	- Information part of XID frame.
 *			  Does not include the control byte.
 *
 * Description:	6.3.2  "Parameter negotiation occurs at any time. It is accomplished by sending
 *		the XID command frame and receiving the XID response frame. Implementations of
 *		AX.25 prior to version 2.2 respond to an XID command frame with a FRMR response
 *		frame. The TNC receiving the FRMR uses a default set of parameters compatible
 *		with previous versions of AX.25."
 *
 *		"This version of AX.25 implements the negotiation or notification of six AX.25
 *		parameters. Notification simply tells the distant TNC some limit that cannot be exceeded.
 *		The distant TNC can choose to use the limit or some other value that is within the
 *		limits. Notification is used with the Window Size Receive (k) and Information
 *		Field Length Receive (N1) parameters. Negotiation involves both TNCs choosing a
 *		value that is mutually acceptable. The XID command frame contains a set of values
 *		acceptable to the originating TNC. The distant TNC chooses to accept the values
 *		offered, or other acceptable values, and places these values in the XID response.
 *		Both TNCs set themselves up based on the values used in the XID response. Negotiation
 *		is used by Classes of Procedures, HDLC Optional Functions, Acknowledge Timer and Retries."
 *
 * Comment:	I have a problem with "... occurs at any time."  What if we were in the middle
 *		of transferring a large file with k=32 then along comes XID which says switch to modulo 8?
 *
 * Insight:	Or is it Erratum?
 *		After reading the base standards documents, it seems that the XID command should offer
 *		up a menu of all the acceptable choices.  e.g.  REJ, SREJ, Multi-SREJ.  One or more bits
 *		can be set.  The XID response, would set a single bit which is the desired choice from
 *		among those offered.
 *		Should go back and review half/full duplex and modulo.
 *
 *--------------------------------------------------------------------*/

func xid_encode(param *xid_param_s, cr cmdres_t) []byte {

	var info []byte

	info = append(info, FI_Format_Indicator)
	info = append(info, GI_Group_Identifier)
	info = append(info, 0)

	var m byte = 4 // classes of procedures
	m += 5         // HDLC optional features
	if param.i_field_length_rx != G_UNKNOWN {
		m += 4
	}
	if param.window_size_rx != G_UNKNOWN {
		m += 3
	}
	if param.ack_timer != G_UNKNOWN {
		m += 4
	}
	if param.retries != G_UNKNOWN {
		m += 3
	}

	info = append(info, m) // 0x17 if all present.

	// "Classes of Procedures" has half / full duplex.

	// We always send this.

	info = append(info, PI_Classes_of_Procedures)
	info = append(info, 2)

	var x C.int = PV_Classes_Procedures_Balanced_ABM

	if param.full_duplex == 1 {
		x |= PV_Classes_Procedures_Full_Duplex
	} else { // includes G_UNKNOWN
		x |= PV_Classes_Procedures_Half_Duplex
	}

	info = append(info, byte(x>>8)&0xff)
	info = append(info, byte(x)&0xff)

	// "HDLC Optional Functions" contains REJ/SREJ & modulo 8/128.

	// We always send this.
	// Watch out for unknown values and do something reasonable.

	info = append(info, PI_HDLC_Optional_Functions)
	info = append(info, 3)

	x = PV_HDLC_Optional_Functions_Extended_Address |
		PV_HDLC_Optional_Functions_TEST_cmd_resp |
		PV_HDLC_Optional_Functions_16_bit_FCS |
		PV_HDLC_Optional_Functions_Synchronous_Tx

	//text_color_set (DW_COLOR_ERROR);
	//dw_printf ("******      XID temp hack - test no SREJ      ******\n");
	// param.srej = srej_none;

	if cr == cr_cmd {
		// offer a "menu" of acceptable choices.  i.e. 1, 2 or 3 bits set.
		switch param.srej {
		default: // Includes srej_none
			x |= PV_HDLC_Optional_Functions_REJ_cmd_resp
		case srej_single:
			x |= PV_HDLC_Optional_Functions_REJ_cmd_resp |
				PV_HDLC_Optional_Functions_SREJ_cmd_resp
		case srej_multi:
			x |= PV_HDLC_Optional_Functions_REJ_cmd_resp |
				PV_HDLC_Optional_Functions_SREJ_cmd_resp |
				PV_HDLC_Optional_Functions_Multi_SREJ_cmd_resp
		}
	} else {
		// for response, set only a single bit.
		switch param.srej {
		default: // Includes srej_none
			x |= PV_HDLC_Optional_Functions_REJ_cmd_resp
		case srej_single:
			x |= PV_HDLC_Optional_Functions_SREJ_cmd_resp
		case srej_multi:
			x |= PV_HDLC_Optional_Functions_Multi_SREJ_cmd_resp
		}
	}

	if param.modulo == modulo_128 {
		x |= PV_HDLC_Optional_Functions_Modulo_128
	} else { // includes modulo_8 and modulo_unknown
		x |= PV_HDLC_Optional_Functions_Modulo_8
	}

	info = append(info, byte(x>>16)&0xff)
	info = append(info, byte(x>>8)&0xff)
	info = append(info, byte(x)&0xff)

	// The rest are skipped if undefined values.

	// "I Field Length Rx" - max I field length acceptable to me.
	// This is in bits.  8191 would be max number of bytes to fit in field.

	if param.i_field_length_rx != G_UNKNOWN {
		info = append(info, byte(PI_I_Field_Length_Rx))
		info = append(info, 2)

		x = param.i_field_length_rx * 8
		info = append(info, byte(x>>8)&0xff)
		info = append(info, byte(x)&0xff)
	}

	// "Window Size Rx"

	if param.window_size_rx != G_UNKNOWN {
		info = append(info, byte(PI_Window_Size_Rx))
		info = append(info, 1)
		info = append(info, byte(param.window_size_rx))
	}

	// "Ack Timer" milliseconds.  We could handle up to 65535 here.

	if param.ack_timer != G_UNKNOWN {
		info = append(info, byte(PI_Ack_Timer))
		info = append(info, 2)
		info = append(info, byte(param.ack_timer>>8)&0xff)
		info = append(info, byte(param.ack_timer)&0xff)
	}

	// "Retries."

	if param.retries != G_UNKNOWN {
		info = append(info, byte(PI_Retries))
		info = append(info, 1)
		info = append(info, byte(param.retries))
	}

	return info
} /* end xid_encode */
