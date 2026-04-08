package direwolf

import (
	"strconv"
	"strings"
	"unicode"
)

// handleGPSNMEA handles the GPSNMEA keyword.
func handleGPSNMEA(ps *parseState) bool {
	/*
	 * GPSNMEA  serial-device  [ speed ]		- Direct connection to GPS receiver.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: Missing serial port name for GPS receiver.\n", ps.line)

		return true
	}

	ps.misc.gpsnmea_port = t

	t = split("", false)
	if t != "" {
		var n, _ = strconv.Atoi(t)
		ps.misc.gpsnmea_speed = n
	} else {
		ps.misc.gpsnmea_speed = 4800 // The standard at one time.
	}
	return false
}

// handleGPSD handles the GPSD keyword.
func handleGPSD(ps *parseState) bool {
	/*
	 * GPSD		- Use GPSD server.
	 *
	 * GPSD [ host [ port ] ]
	 */

	/*
	   TODO KG

	   	#if __WIN32__

	   		    text_color_set(DW_COLOR_ERROR);
	   		    dw_printf ("Config file, line %d: The GPSD interface is not available for Windows.\n", ps.line);
	   		    continue;

	   	#elif ENABLE_GPSD
	*/
	dw_printf("Warning: GPSD support currently disabled pending a rewrite of the integration.\n")

	ps.misc.gpsd_host = "localhost"
	ps.misc.gpsd_port = DEFAULT_GPSD_PORT

	var t = split("", false)
	if t != "" {
		ps.misc.gpsd_host = t

		t = split("", false)
		if t != "" {
			var n, _ = strconv.Atoi(t)
			if (n >= MIN_IP_PORT_NUMBER && n <= MAX_IP_PORT_NUMBER) || n == 0 {
				ps.misc.gpsd_port = n
			} else {
				ps.misc.gpsd_port = DEFAULT_GPSD_PORT

				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: Invalid port number for GPSD Socket Interface. Using default of %d.\n",
					ps.line, ps.misc.gpsd_port)
			}
		}
	}
	/*
	   	TODO KG

	   #else

	   	text_color_set(DW_COLOR_ERROR);
	   	dw_printf ("Config file, line %d: The GPSD interface has not been enabled.\n", ps.line);
	   	dw_printf ("Install gpsd and libgps-dev packages then rebuild direwolf.\n");
	   	continue;

	   #endif
	*/
	return false
}

// handleWAYPOINT handles the WAYPOINT keyword.
func handleWAYPOINT(ps *parseState) bool {
	/*
	 * WAYPOINT		- Generate WPL and AIS NMEA sentences for display on map.
	 *
	 * WAYPOINT  serial-device [ formats ]
	 * WAYPOINT  host:udpport [ formats ]
	 *
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing output device for WAYPOINT on line %d.\n", ps.line)

		return true
	}

	/* If there is a ':' in the name, split it into hostname:udpportnum. */
	/* Otherwise assume it is serial port name. */

	if strings.Contains(t, ":") {
		var hostname, portStr, _ = strings.Cut(t, ":")

		var port, _ = strconv.Atoi(portStr)
		if port >= MIN_IP_PORT_NUMBER && port <= MAX_IP_PORT_NUMBER {
			ps.misc.waypoint_udp_hostname = hostname
			if ps.misc.waypoint_udp_hostname == "" {
				ps.misc.waypoint_udp_hostname = "localhost"
			}

			ps.misc.waypoint_udp_portnum = port
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Invalid UDP port number %d for sending waypoints.\n", ps.line, port)
		}
	} else {
		ps.misc.waypoint_serial_port = t
	}

	/* Anything remaining is the formats to enable. */

	t = split("", true)
	for _, c := range t {
		switch unicode.ToUpper(c) {
		case 'N':
			ps.misc.waypoint_formats |= WPL_FORMAT_NMEA_GENERIC
		case 'G':
			ps.misc.waypoint_formats |= WPL_FORMAT_GARMIN
		case 'M':
			ps.misc.waypoint_formats |= WPL_FORMAT_MAGELLAN
		case 'K':
			ps.misc.waypoint_formats |= WPL_FORMAT_KENWOOD
		case 'A':
			ps.misc.waypoint_formats |= WPL_FORMAT_AIS
		case ' ', ',':
		default:
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Invalid output format '%c' for WAYPOINT on line %d.\n", c, ps.line)
		}
	}
	return false
}
