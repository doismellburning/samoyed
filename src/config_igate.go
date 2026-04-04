package direwolf

import (
	"strconv"
	"strings"
)

// handleIGSERVER handles the IGSERVER keyword.
func handleIGSERVER(ps *parseState) bool {
	/*
	 * ==================== Internet gateway ====================
	 */

	/*
	 * IGSERVER 		- Name of IGate server.
	 *
	 * IGSERVER  hostname [ port ] 				-- original implementation.
	 *
	 * IGSERVER  hostname:port				-- more in line with usual conventions.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing IGate server name for IGSERVER command.\n", ps.line)

		return true
	}

	ps.igate.t2_server_name = t

	/* If there is a : in the name, split it out as the port number. */

	if strings.Contains(t, ":") {
		var hostname, portStr, _ = strings.Cut(t, ":")
		ps.igate.t2_server_name = hostname

		var port, portErr = strconv.Atoi(portStr)
		if port >= MIN_IP_PORT_NUMBER && port <= MAX_IP_PORT_NUMBER && portErr == nil {
			ps.igate.t2_server_port = port
		} else {
			ps.igate.t2_server_port = DEFAULT_IGATE_PORT

			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Invalid port number for IGate server. Using default %d.\n",
				ps.line, ps.igate.t2_server_port)
		}
	}

	/* Alternatively, the port number could be separated by white space. */

	t = split("", false)
	if t != "" {
		var n, _ = strconv.Atoi(t)
		if n >= MIN_IP_PORT_NUMBER && n <= MAX_IP_PORT_NUMBER {
			ps.igate.t2_server_port = n
		} else {
			ps.igate.t2_server_port = DEFAULT_IGATE_PORT

			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Invalid port number for IGate server. Using default %d.\n",
				ps.line, ps.igate.t2_server_port)
		}
	}
	// dw_printf ("DEBUG  server=%s   port=%d\n", p_igate_config.t2_server_name, p_igate_config.t2_server_port);
	// exit (0);
	return false
}

// handleIGLOGIN handles the IGLOGIN keyword.
func handleIGLOGIN(ps *parseState) bool {
	/*
	 * IGLOGIN 		- Login callsign and passcode for IGate server
	 *
	 * IGLOGIN  callsign  passcode
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing login callsign for IGLOGIN command.\n", ps.line)

		return true
	}
	// TODO: Wouldn't hurt to do validity checking of format.
	ps.igate.t2_login = t

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing passcode for IGLOGIN command.\n", ps.line)

		return true
	}

	ps.igate.t2_passcode = t
	return false
}

// handleIGTXVIA handles the IGTXVIA keyword.
func handleIGTXVIA(ps *parseState) bool {
	/*
	 * IGTXVIA 		- Transmit channel and VIA path for messages from IGate server
	 *
	 * IGTXVIA  channel  [ path ]
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing transmit channel for IGTXVIA command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n < 0 || n > MAX_TOTAL_CHANS-1 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Transmit channel must be in range of 0 to %d on line %d.\n",
			MAX_TOTAL_CHANS-1, ps.line)

		return true
	}

	ps.igate.tx_chan = n

	t = split("", false)
	if t != "" {
		// TODO KG#if 1	// proper checking
		n = check_via_path(t)
		if n >= 0 {
			ps.igate.max_digi_hops = n
			ps.igate.tx_via = "," + t
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: invalid via path.\n", ps.line)
		}

		/* TODO KG #else	// previously

		   	      char *p;
		   	      ps.igate.tx_via[0] = ',';
		   	      strlcpy (ps.igate.tx_via + 1, t, sizeof(ps.igate.tx_via)-1);
		   	      for (p = ps.igate.tx_via; *p != 0; p++) {
		   	        if (islower(*p)) {
		   		  *p = toupper(*p);	// silently force upper case.
		   	        }
		   	      }
		   #endif
		*/
	}
	return false
}

// handleIGFILTER handles the IGFILTER keyword.
func handleIGFILTER(ps *parseState) bool {
	/*
	 * IGFILTER 		- IGate Server side filters.
	 *			  Is this name too confusing.  Too similar to FILTER IG 0 ...
	 *			  Maybe SSFILTER suggesting Server Side.
	 *			  SUBSCRIBE might be better because it's not a filter that limits.
	 *
	 * IGFILTER  filter-spec ...
	 */
	var t = split("", true) /* Take rest of ps.line as one string. */

	if ps.igate.t2_filter != "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Warning - IGFILTER already configured (%s), this one (%s) will be ignored.\n", ps.line, ps.igate.t2_filter, t)

		return true
	}

	if t != "" {
		ps.igate.t2_filter = t

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Warning - IGFILTER is a rarely needed expert level feature.\n", ps.line)
		dw_printf("If you don't have a special situation and a good understanding of\n")
		dw_printf("how this works, you probably should not be messing with it.\n")
		dw_printf("The default behavior is appropriate for most situations.\n")
		dw_printf("Please read \"Successful-APRS-IGate-Operation.pdf\".\n")
	}
	return false
}

// handleIGTXLIMIT handles the IGTXLIMIT keyword.
func handleIGTXLIMIT(ps *parseState) bool {
	/*
	 * IGTXLIMIT 		- Limit transmissions during 1 and 5 minute intervals.
	 *
	 * IGTXLIMIT  one-minute-limit  five-minute-limit
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing one minute limit for IGTXLIMIT command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n < 1 {
		ps.igate.tx_limit_1 = 1
	} else if n <= IGATE_TX_LIMIT_1_MAX {
		ps.igate.tx_limit_1 = n
	} else {
		ps.igate.tx_limit_1 = IGATE_TX_LIMIT_1_MAX

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: One minute transmit limit has been reduced to %d.\n",
			ps.line, ps.igate.tx_limit_1)
		dw_printf("You won't make friends by setting a limit this high.\n")
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing five minute limit for IGTXLIMIT command.\n", ps.line)

		return true
	}

	n, _ = strconv.Atoi(t)
	if n < 1 {
		ps.igate.tx_limit_5 = 1
	} else if n <= IGATE_TX_LIMIT_5_MAX {
		ps.igate.tx_limit_5 = n
	} else {
		ps.igate.tx_limit_5 = IGATE_TX_LIMIT_5_MAX

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Five minute transmit limit has been reduced to %d.\n",
			ps.line, ps.igate.tx_limit_5)
		dw_printf("You won't make friends by setting a limit this high.\n")
	}
	return false
}

// handleIGMSP handles the IGMSP keyword.
func handleIGMSP(ps *parseState) bool {
	/*
	 * IGMSP 		- Number of times to send position of message sender.
	 *
	 * IGMSP  n
	 */
	var t = split("", false)
	if t != "" {
		var n, _ = strconv.Atoi(t)
		if n >= 0 && n <= 10 {
			ps.igate.igmsp = n
		} else {
			ps.igate.igmsp = 1

			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Unreasonable number of times for message sender position.  Using default 1.\n", ps.line)
		}
	} else {
		ps.igate.igmsp = 1

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing number of times for message sender position.  Using default 1.\n", ps.line)
	}
	return false
}

// handleSATGATE handles the SATGATE keyword.
func handleSATGATE(ps *parseState) bool {
	/*
	 * SATGATE 		- Special SATgate mode to delay packets heard directly.
	 *
	 * SATGATE [ n ]
	 */
	text_color_set(DW_COLOR_INFO)
	dw_printf("Line %d: SATGATE is pretty useless and will be removed in a future version.\n", ps.line)

	var t = split("", false)
	if t != "" {
		var n, _ = strconv.Atoi(t)
		if n >= MIN_SATGATE_DELAY && n <= MAX_SATGATE_DELAY {
			ps.igate.satgate_delay = n
		} else {
			ps.igate.satgate_delay = DEFAULT_SATGATE_DELAY

			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Unreasonable SATgate delay.  Using default.\n", ps.line)
		}
	} else {
		ps.igate.satgate_delay = DEFAULT_SATGATE_DELAY
	}
	return false
}
