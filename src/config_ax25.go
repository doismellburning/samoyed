package direwolf

import (
	"strconv"
)

// handleFRACK handles the FRACK keyword.
func handleFRACK(ps *parseState) bool {
	/*
	 * ==================== AX.25 connected mode ====================
	 */

	/*
	 * FRACK  n 		- Number of seconds to wait for ack to transmission.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing value for FRACK.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= AX25_T1V_FRACK_MIN && n <= AX25_T1V_FRACK_MAX {
		ps.misc.frack = n
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid FRACK time. Using default %d.\n", ps.line, ps.misc.frack)
	}
	return false
}

// handleRETRY handles the RETRY keyword.
func handleRETRY(ps *parseState) bool {
	/*
	 * RETRY  n 		- Number of times to retry before giving up.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing value for RETRY.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= AX25_N2_RETRY_MIN && n <= AX25_N2_RETRY_MAX {
		ps.misc.retry = n
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid RETRY number. Using default %d.\n", ps.line, ps.misc.retry)
	}
	return false
}

// handlePACLEN handles the PACLEN keyword.
func handlePACLEN(ps *parseState) bool {
	/*
	 * PACLEN  n 		- Maximum number of bytes in information part.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing value for PACLEN.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= AX25_N1_PACLEN_MIN && n <= AX25_N1_PACLEN_MAX {
		ps.misc.paclen = n
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid PACLEN value. Using default %d.\n", ps.line, ps.misc.paclen)
	}
	return false
}

// handleMAXFRAME handles the MAXFRAME keyword.
func handleMAXFRAME(ps *parseState) bool {
	/*
	 * MAXFRAME  n 		- Max frames to send before ACK.  mod 8 "Window" size.
	 *
	 * Window size would make more sense but everyone else calls it MAXFRAME.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing value for MAXFRAME.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= AX25_K_MAXFRAME_BASIC_MIN && n <= AX25_K_MAXFRAME_BASIC_MAX {
		ps.misc.maxframe_basic = n
	} else {
		ps.misc.maxframe_basic = AX25_K_MAXFRAME_BASIC_DEFAULT

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid MAXFRAME value outside range of %d to %d. Using default %d.\n",
			ps.line, AX25_K_MAXFRAME_BASIC_MIN, AX25_K_MAXFRAME_BASIC_MAX, ps.misc.maxframe_basic)
	}
	return false
}

// handleEMAXFRAME handles the EMAXFRAME keyword.
func handleEMAXFRAME(ps *parseState) bool {
	/*
	 * EMAXFRAME  n 		- Max frames to send before ACK.  mod 128 "Window" size.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing value for EMAXFRAME.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= AX25_K_MAXFRAME_EXTENDED_MIN && n <= AX25_K_MAXFRAME_EXTENDED_MAX {
		ps.misc.maxframe_extended = n
	} else {
		ps.misc.maxframe_extended = AX25_K_MAXFRAME_EXTENDED_DEFAULT

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid EMAXFRAME value outside of range %d to %d. Using default %d.\n",
			ps.line, AX25_K_MAXFRAME_EXTENDED_MIN, AX25_K_MAXFRAME_EXTENDED_MAX, ps.misc.maxframe_extended)
	}
	return false
}

// handleMAXV22 handles the MAXV22 keyword.
func handleMAXV22(ps *parseState) bool {
	/*
	 * MAXV22  n 		- Max number of SABME sent before trying SABM.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing value for MAXV22.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 0 && n <= AX25_N2_RETRY_MAX {
		ps.misc.maxv22 = n
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid MAXV22 number. Will use half of RETRY.\n", ps.line)
	}
	return false
}

// handleV20 handles the V20 keyword.
func handleV20(ps *parseState) bool {
	/*
	 * V20  address [ address ... ] 	- Stations known to support only AX.25 v2.0.
	 *					  When connecting to these, skip SABME and go right to SABM.
	 *					  Possible to have multiple and they are cumulative.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing address(es) for V20.\n", ps.line)

		return true
	}

	for t != "" {
		var strictness = 2
		var _, _, _, ok = ax25_parse_addr(AX25_DESTINATION, t, strictness)

		if ok {
			ps.misc.v20_addrs = append(ps.misc.v20_addrs, t)
			ps.misc.v20_count++
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Invalid station address for V20 command.\n", ps.line)

			// continue processing any others following.
		}

		t = split("", false)
	}
	return false
}

// handleNOXID handles the NOXID keyword.
func handleNOXID(ps *parseState) bool {
	/*
	 * NOXID  address [ address ... ] 	- Stations known not to understand XID.
	 *					  After connecting to these (with v2.2 obviously), don't try using XID command.
	 *					  AX.25 for Linux is the one known case so far.
	 *					  Possible to have multiple and they are cumulative.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing address(es) for NOXID.\n", ps.line)

		return true
	}

	for t != "" {
		var strictness = 2
		var _, _, _, ok = ax25_parse_addr(AX25_DESTINATION, t, strictness)

		if ok {
			ps.misc.noxid_addrs = append(ps.misc.noxid_addrs, t)
			ps.misc.noxid_count++
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Invalid station address for NOXID command.\n", ps.line)

			// continue processing any others following.
		}

		t = split("", false)
	}
	return false
}
