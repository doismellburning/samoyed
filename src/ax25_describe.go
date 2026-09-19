package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:	Describe an AX.25 frame in human readable form.
 *
 * Description:	The pieces - ax25_hex_dump, AX25FormatAddrs, decode_aprs -
 *		already exist.  This puts them together the way anything
 *		inspecting frames off the wire wants them, and says what is
 *		wrong with a frame too malformed for the next step, rather
 *		than walking off the end of it.
 *
 *------------------------------------------------------------------*/

import (
	"fmt"
)

/*------------------------------------------------------------------
 *
 * Function:	DescribeAX25Frame
 *
 * Purpose:	Print a description of one AX.25 frame: the header, the
 *		digipeater path, the control and PID fields, and the
 *		information field decoded as APRS where the frame is APRS.
 *
 * Inputs:	frame	- The frame, as it appeared on the air, without the
 *			  FCS and without any KISS framing.
 *
 * Returns:	The number of problems found with the frame.
 *
 * Assumption:	DecodeAPRSInit has been called.
 *
 *------------------------------------------------------------------*/

func DescribeAX25Frame(frame []byte) int {
	if len(frame) < AX25_MIN_PACKET_LEN {
		fmt.Printf("ERROR: The frame is %d bytes, too short for an AX.25 header of at least %d.\n", len(frame), AX25_MIN_PACKET_LEN)

		return 1
	}

	var alevel ALevel

	var pp = AX25FromFrame(frame, alevel)
	if pp == nil {
		fmt.Printf("ERROR: Could not construct an AX.25 frame from those %d bytes.\n", len(frame))

		return 1
	}

	/*
	 * Establish that the frame has the fields the description is about to read
	 * before reading any of them.  ax25_hex_dump takes the control and PID
	 * octets on trust, so a frame that stops short of them would otherwise be
	 * described in terms of the zero padding past its end.
	 */

	if ax25_get_num_addr(pp) < AX25_MIN_ADDRS {
		/*
		 * The end of address bit is not at the end of a 7 byte address, or it
		 * marks out fewer than 2 or more than 10 addresses.  Without knowing
		 * where the address field stops there is nothing more to say.
		 */
		fmt.Printf("ERROR: The address field is malformed - the end of address bit does not mark out %d to %d addresses of 7 bytes each.\n",
			AX25_MIN_ADDRS, AX25_MAX_ADDRS)
		HexDump(frame)

		return 1
	}

	var frameLen = ax25_get_frame_len(pp)

	if ax25_get_control_offset(pp) >= frameLen {
		fmt.Printf("ERROR: The frame ends after the address field - there is no control byte.\n")
		HexDump(frame)

		return 1
	}

	if ax25_get_info_offset(pp) > frameLen {
		fmt.Printf("ERROR: The frame is %d bytes, but the address, control and PID fields need %d - it ends before the information field.\n",
			frameLen, ax25_get_info_offset(pp))
		HexDump(frame)

		return 1
	}

	fmt.Printf("--- AX.25 frame ---\n")
	ax25_hex_dump(pp)
	fmt.Printf("-------------------\n")

	var problems = 0

	fmt.Printf("%s\n", AX25FormatAddrs(pp))

	if !ax25_check_addresses(pp, addrStrict) {
		problems++
	}

	var info = AX25GetInfo(pp)

	if ax25_is_aprs(pp) {
		AX25SafePrint(info, true) // Display non-ASCII as hexadecimal.
		fmt.Printf("\n")
		NoteSafePrintTruncation(len(info))

		var A = decode_aprs(pp, false, "") // Extract information into structure.

		decode_aprs_print(A) // Now print it in human readable format.
	} else {
		/*
		 * The control and PID octets are in the dump above, and either of them
		 * can be the reason, so don't name one of them as the culprit.
		 */
		fmt.Printf("APRS travels in a UI frame with PID 0xf0.  This is not one, so its %d byte information field is not decoded as APRS.\n", len(info))

		if len(info) > 0 {
			AX25SafePrint(info, true)
			fmt.Printf("\n")
			NoteSafePrintTruncation(len(info))
		}
	}

	return problems
}
