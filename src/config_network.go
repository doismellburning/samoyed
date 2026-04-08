package direwolf

import (
	"strconv"
)

// handleAGWPORT handles the AGWPORT keyword.
func handleAGWPORT(ps *parseState) bool {
	/*
	 * ==================== All the left overs ====================
	 */

	/*
	 * AGWPORT 		- Port number for "AGW TCPIP Socket Interface"
	 *
	 * In version 1.2 we allow 0 to disable listening.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing port number for AGWPORT command.\n", ps.line)

		return true
	}
	var n, _ = strconv.Atoi(t)

	t = split("", false)
	if t != "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Unexpected \"%s\" after the port number.\n", ps.line, t)
		dw_printf("Perhaps you were trying to use feature available only with KISSPORT.\n")

		return true
	}

	if (n >= MIN_IP_PORT_NUMBER && n <= MAX_IP_PORT_NUMBER) || n == 0 {
		ps.misc.agwpe_port = n
	} else {
		ps.misc.agwpe_port = DEFAULT_AGWPE_PORT

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid port number for AGW TCPIP Socket Interface. Using %d.\n",
			ps.line, ps.misc.agwpe_port)
	}
	return false
}

// handleKISSPORT handles the KISSPORT keyword.
func handleKISSPORT(ps *parseState) bool {
	/*
	 * KISSPORT port [ chan ]		- Port number for KISS over IP.
	 */

	// Previously we allowed only a single TCP port for KISS.
	// An increasing number of people want to run multiple radios.
	// Unfortunately, most applications don't know how to deal with multi-radio TNCs.
	// They ignore the channel on receive and always transmit to channel 0.
	// Running multiple instances of direwolf is a work-around but this leads to
	// more complex configuration and we lose the cross-channel digipeating capability.
	// In release 1.7 we add a new feature to assign a single radio channel to a TCP port.
	// e.g.
	//
	//	KISSPORT 8001		# default, all channels.  Radio channel = KISS channel.
	//
	//	KISSPORT 7000 0		# Only radio channel 0 for receive.
	//				# Transmit to radio channel 0, ignoring KISS channel.
	//
	//	KISSPORT 7001 1		# Only radio channel 1 for receive.  KISS channel set to 0.
	//				# Transmit to radio channel 1, ignoring KISS channel.
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing TCP port number for KISSPORT command.\n", ps.line)

		return true
	}
	var n, _ = strconv.Atoi(t)

	var tcp_port int
	if (n >= MIN_IP_PORT_NUMBER && n <= MAX_IP_PORT_NUMBER) || n == 0 {
		tcp_port = n
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid TCP port number for KISS TCPIP Socket Interface.\n", ps.line)
		dw_printf("Use something in the range of %d to %d.\n", MIN_IP_PORT_NUMBER, MAX_IP_PORT_NUMBER)

		return true
	}

	t = split("", false)
	var kissChannel = -1 // optional.  default to all if not specified.

	if t != "" {
		var channelErr error

		kissChannel, channelErr = strconv.Atoi(t)
		if ps.channel < 0 || kissChannel >= MAX_TOTAL_CHANS || channelErr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Invalid channel %d for KISSPORT command.  Must be in range 0 thru %d.\n", ps.line, kissChannel, MAX_TOTAL_CHANS-1)

			return true
		}
	}

	// "KISSPORT 0" is used to remove the default entry.

	if tcp_port == 0 {
		ps.misc.kiss_port[0] = 0 // Should all be wiped out?
	} else {
		// Try to find an empty slot.
		// A duplicate TCP port number will overwrite the previous value.
		var slot = -1
		for i := 0; i < MAX_KISS_TCP_PORTS && slot == -1; i++ {
			if ps.misc.kiss_port[i] == tcp_port { //nolint:staticcheck
				slot = i
				if !(slot == 0 && tcp_port == DEFAULT_KISS_PORT) {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Warning: Duplicate TCP port %d will overwrite previous value.\n", ps.line, tcp_port)
				}
			} else if ps.misc.kiss_port[i] == 0 {
				slot = i
			}
		}

		if slot >= 0 {
			ps.misc.kiss_port[slot] = tcp_port
			ps.misc.kiss_chan[slot] = kissChannel
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Too many KISSPORT commands.\n", ps.line)
		}
	}
	return false
}

// handleNULLMODEM handles the NULLMODEM keyword.
func handleNULLMODEM(ps *parseState) bool {
	/*
	 * NULLMODEM name [ speed ]	- Device name for serial port or our end of the virtual "null modem"
	 * SERIALKISS name  [ speed ]
	 *
	 * Version 1.5:  Added SERIALKISS which is equivalent to NULLMODEM.
	 * The original name sort of made sense when it was used only for one end of a virtual
	 * null modem cable on Windows only.  Now it is also available for Linux.
	 * TODO1.5: In retrospect, this doesn't seem like such a good name.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing serial port name on line %d.\n", ps.line)

		return true
	} else {
		if ps.misc.kiss_serial_port != "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Warning serial port name on line %d replaces earlier value.\n", ps.line)
		}

		ps.misc.kiss_serial_port = t
		ps.misc.kiss_serial_speed = 0
		ps.misc.kiss_serial_poll = 0
	}

	t = split("", false)
	if t != "" {
		ps.misc.kiss_serial_speed, _ = strconv.Atoi(t)
	}
	return false
}

// handleSERIALKISSPOLL handles the SERIALKISSPOLL keyword.
func handleSERIALKISSPOLL(ps *parseState) bool {
	/*
	 * SERIALKISSPOLL name		- Poll for serial port name that might come and go.
	 *			  	  e.g. /dev/rfcomm0 for bluetooth.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing serial port name on line %d.\n", ps.line)

		return true
	} else {
		if ps.misc.kiss_serial_port != "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Warning serial port name on line %d replaces earlier value.\n", ps.line)
		}

		ps.misc.kiss_serial_port = t
		ps.misc.kiss_serial_speed = 0
		ps.misc.kiss_serial_poll = 1 // set polling.
	}
	return false
}

// handleKISSCOPY handles the KISSCOPY keyword.
func handleKISSCOPY(ps *parseState) bool {
	/*
	 * KISSCOPY 		- Data from network KISS client is copied to all others.
	 *			  This does not apply to pseudo terminal KISS.
	 */
	ps.misc.kiss_copy = true
	return false
}

// handleDNSSD handles the DNSSD keyword.
func handleDNSSD(ps *parseState) bool {
	/*
	 * DNSSD 		- Enable or disable (1/0) dns-sd, DNS Service Discovery announcements
	 * DNSSDNAME            - Set DNS-SD service name, defaults to "Dire Wolf on <hostname>"
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing integer value for DNSSD command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n == 0 || n == 1 {
		ps.misc.dns_sd_enabled = n != 0
	} else {
		ps.misc.dns_sd_enabled = false

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid integer value for DNSSD. Disabling dns-sd.\n", ps.line)
	}
	return false
}

// handleDNSSDNAME handles the DNSSDNAME keyword.
func handleDNSSDNAME(ps *parseState) bool {
	var t = split("", true)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing service name for DNSSDNAME.\n", ps.line)

		return true
	} else {
		ps.misc.dns_sd_name = t
	}
	return false
}
