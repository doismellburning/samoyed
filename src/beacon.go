package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Transmit messages on a fixed schedule.
 *
 * Description:	Transmit periodic messages as specified in the config file.
 *
 *---------------------------------------------------------------*/

// #include "direwolf.h"
// #include <stdio.h>
// #include <unistd.h>
// #include <stdlib.h>
// #include <assert.h>
// #include <string.h>
// #include <math.h>
// #include <time.h>
// #include "ax25_pad.h"
// #include "textcolor.h"
// #include "audio.h"
// #include "tq.h"
// #include "xmit.h"
// #include "config.h"
// #include "version.h"
// #include "encode_aprs.h"
// #include "latlong.h"
// #include "dwgps.h"
// #include "log.h"
// #include "dlq.h"
// #include "aprs_tt.h"		// for dw_run_cmd - should relocate someday.
// #include "mheard.h"
import "C"

import (
	"fmt"
	"math"
	"strings"
	"unsafe"
)

const BEACON_OBJECT = C.BEACON_OBJECT
const BEACON_CUSTOM = C.BEACON_CUSTOM
const BEACON_IGATE = C.BEACON_IGATE
const BEACON_TRACKER = C.BEACON_TRACKER
const BEACON_IGNORE = C.BEACON_IGNORE
const BEACON_POSITION = C.BEACON_POSITION

/*
 * Save pointers to configuration settings.
 */

var g_modem_config_p *C.struct_audio_s
var g_misc_config_p *C.struct_misc_config_s
var g_igate_config_p *C.struct_igate_config_s

var g_tracker_debug_level = 0 // 1 for data from gps.
// 2 + Smart Beaconing logic.
// 3 + Send transmissions to log file.

func beacon_tracker_set_debug(level int) {
	g_tracker_debug_level = level
}

/*-------------------------------------------------------------------
 *
 * Name:        beacon_init
 *
 * Purpose:     Initialize the beacon process.
 *
 * Inputs:	pmodem		- Audio device and modem configuration.
 *				  Used only to find valid channels.
 *
 *		pconfig		- misc. configuration from config file.
 *				  Beacon stuff ended up here.
 *
 *		pigate		- IGate configuration.
 *				  Need this for calculating IGate statistics.
 *
 *
 * Outputs:	Remember required information for future use.
 *
 * Description:	Do some validity checking on the beacon configuration.
 *
 *		Start up beacon_thread to actually send the packets
 *		at the appropriate time.
 *
 *--------------------------------------------------------------------*/

func beacon_init(pmodem *C.struct_audio_s, pconfig *C.struct_misc_config_s, pigate *C.struct_igate_config_s) {
	/*  FIXME KG
	struct tm tm;
	int j;
	pthread_t beacon_tid;
	*/

	/*
	 * Save parameters for later use.
	 */
	g_modem_config_p = pmodem
	g_misc_config_p = pconfig
	g_igate_config_p = pigate

	/*
	 * Precompute the packet contents so any errors are
	 * Reported once at start up time rather than for each transmission.
	 * If a serious error is found, set type to BEACON_IGNORE and that
	 * table entry should be ignored later on.
	 */

	// TODO: Better checking.
	// We should really have a table for which keywords are are required,
	// optional, or not allowed for each beacon type.  Options which
	// are not applicable are often silently ignored, causing confusion.

	for j := C.int(0); j < g_misc_config_p.num_beacons; j++ {
		var channel = g_misc_config_p.beacon[j].sendto_chan

		if channel < 0 {
			channel = 0 /* For IGate, use channel 0 call. */
		}
		if channel >= MAX_TOTAL_CHANS {
			channel = 0 // For ICHANNEL, use channel 0 call.
		}

		if g_modem_config_p.chan_medium[channel] == MEDIUM_RADIO ||
			g_modem_config_p.chan_medium[channel] == MEDIUM_NETTNC {
			if C.GoString(&g_modem_config_p.mycall[channel][0]) != "" &&
				!strings.EqualFold(C.GoString(&g_modem_config_p.mycall[channel][0]), "N0CALL") &&
				!strings.EqualFold(C.GoString(&g_modem_config_p.mycall[channel][0]), "NOCALL") {
				switch g_misc_config_p.beacon[j].btype {
				case BEACON_OBJECT:

					/* Object name is required. */

					if C.GoString(&g_misc_config_p.beacon[j].objname[0]) == "" {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("Config file, line %d: OBJNAME is required for OBEACON.\n", g_misc_config_p.beacon[j].lineno)
						g_misc_config_p.beacon[j].btype = BEACON_IGNORE
						continue
					}
					/* Fall thru.  Ignore any warning about missing break. */
					fallthrough

				case BEACON_POSITION:

					/* Location is required. */

					if g_misc_config_p.beacon[j].lat == G_UNKNOWN || g_misc_config_p.beacon[j].lon == G_UNKNOWN {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("Config file, line %d: Latitude and longitude are required.\n", g_misc_config_p.beacon[j].lineno)
						g_misc_config_p.beacon[j].btype = BEACON_IGNORE
						continue
					}

					/* INFO and INFOCMD are only for Custom Beacon. */

					if g_misc_config_p.beacon[j].custom_info != nil || g_misc_config_p.beacon[j].custom_infocmd != nil {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("Config file, line %d: INFO or INFOCMD are allowed only for custom beacon.\n", g_misc_config_p.beacon[j].lineno)
						dw_printf("INFO and INFOCMD allow you to specify contents of the Information field so it\n")
						dw_printf("so it would not make sense to use these with other beacon types which construct\n")
						dw_printf("the Information field. Perhaps you want to use COMMENT or COMMENTCMD option.\n")
						// g_misc_config_p.beacon[j].btype = BEACON_IGNORE;
						continue
					}

				case BEACON_TRACKER:
					{
						var gpsinfo C.dwgps_info_t
						var fix = C.dwgps_read(&gpsinfo)
						if fix == DWFIX_NOT_INIT {
							text_color_set(DW_COLOR_ERROR)
							dw_printf("Config file, line %d: GPS must be configured to use TBEACON.\n", g_misc_config_p.beacon[j].lineno)
							g_misc_config_p.beacon[j].btype = BEACON_IGNORE
							dw_printf("You must specify the source of the GPS data in your configuration file.\n")
							dw_printf("It can be either GPSD, meaning the gpsd daemon, or GPSNMEA for\n")
							dw_printf("for a serial port connection with exclusive use.\n")
						}
					}

					/* INFO and INFOCMD are only for Custom Beacon. */

					if g_misc_config_p.beacon[j].custom_info != nil || g_misc_config_p.beacon[j].custom_infocmd != nil {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("Config file, line %d: INFO or INFOCMD are allowed only for custom beacon.\n", g_misc_config_p.beacon[j].lineno)
						dw_printf("INFO and INFOCMD allow you to specify contents of the Information field so it\n")
						dw_printf("so it would not make sense to use these with other beacon types which construct\n")
						dw_printf("the Information field. Perhaps you want to use COMMENT or COMMENTCMD option.\n")
						// g_misc_config_p.beacon[j].btype = BEACON_IGNORE;
						continue
					}

				case BEACON_CUSTOM:

					/* INFO or INFOCMD is required. */

					if g_misc_config_p.beacon[j].custom_info == nil && g_misc_config_p.beacon[j].custom_infocmd == nil {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("Config file, line %d: INFO or INFOCMD is required for custom beacon.\n", g_misc_config_p.beacon[j].lineno)
						g_misc_config_p.beacon[j].btype = BEACON_IGNORE
						continue
					}

				case BEACON_IGATE:

					/* Doesn't make sense if IGate is not configured. */

					if C.GoString(&g_igate_config_p.t2_server_name[0]) == "" ||
						C.GoString(&g_igate_config_p.t2_login[0]) == "" ||
						C.GoString(&g_igate_config_p.t2_passcode[0]) == "" {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("Config file, line %d: Doesn't make sense to use IBEACON without IGate Configured.\n", g_misc_config_p.beacon[j].lineno)
						dw_printf("IBEACON has been disabled.\n")
						g_misc_config_p.beacon[j].btype = BEACON_IGNORE
						continue
					}

				case BEACON_IGNORE:
				}
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file, line %d: MYCALL must be set for beacon on channel %d. \n", g_misc_config_p.beacon[j].lineno, channel)
				g_misc_config_p.beacon[j].btype = BEACON_IGNORE
			}
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: Invalid channel number %d for beacon. \n", g_misc_config_p.beacon[j].lineno, channel)
			g_misc_config_p.beacon[j].btype = BEACON_IGNORE
		}
	}

	/*
	 * Calculate first time for each beacon from the 'slot' or 'delay' value.
	 */

	var now = C.time(nil)
	var tm C.struct_tm
	C.localtime_r(&now, &tm)

	for j := C.int(0); j < g_misc_config_p.num_beacons; j++ {
		var bp = &(g_misc_config_p.beacon[j])
		/* FIXME KG
		#if DEBUG

			  text_color_set(DW_COLOR_DEBUG);
			  dw_printf ("beacon[%d] chan=%d, delay=%d, slot=%d, every=%d\n",
				j,
				bp.sendto_chan,
				bp.delay,
				bp.slot,
				bp.every);
		#endif
		*/

		/*
		 * If timeslots, there must be a full number of beacon intervals per hour.
		 */

		if bp.slot != G_UNKNOWN {
			if !IS_GOOD(bp.every) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file, line %d: When using timeslots, there must be a whole number of beacon intervals per hour.\n", bp.lineno)

				// Try to make it valid by adjusting up or down.

				for n := C.int(1); ; n++ {
					var e = bp.every + n
					if e > 3600 {
						bp.every = 3600
						break
					}
					if IS_GOOD(e) {
						bp.every = e
						break
					}
					e = bp.every - n
					if e < 1 {
						bp.every = 1 // Impose a larger minimum?
						break
					}
					if IS_GOOD(e) {
						bp.every = e
						break
					}
				}
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file, line %d: Time between slotted beacons has been adjusted to %d seconds.\n", bp.lineno, bp.every)
			}
			/*
			 * Determine when next slot time will arrive.
			 */
			bp.delay = bp.slot - (tm.tm_min*60 + tm.tm_sec)
			for bp.delay > bp.every {
				bp.delay -= bp.every
			}
			for bp.delay < 5 {
				bp.delay += bp.every
			}
		}

		g_misc_config_p.beacon[j].next = now + C.long(g_misc_config_p.beacon[j].delay)
	}

	/*
	 * Start up thread for processing only if at least one is valid.
	 */

	var count = 0
	for j := C.int(0); j < g_misc_config_p.num_beacons; j++ {
		if g_misc_config_p.beacon[j].btype != BEACON_IGNORE {
			count++
		}
	}

	if count >= 1 {
		go beacon_thread()
	}
} /* end beacon_init */

func IS_GOOD(x C.int) bool {
	return (3600/(x))*(x) == 3600
}

/*-------------------------------------------------------------------
 *
 * Name:        beacon_thread
 *
 * Purpose:     Transmit beacons when it is time.
 *
 * Inputs:	g_misc_config_p.beacon
 *
 * Outputs:	g_misc_config_p.beacon[].next_time
 *
 * Description:	Go to sleep until it is time for the next beacon.
 *		Transmit any beacons scheduled for now.
 *		Repeat.
 *
 *--------------------------------------------------------------------*/

func beacon_thread() {
	/*
	 * SmartBeaconing state.
	 */

	/*
	 * See if any tracker beacons are configured.
	 * No need to obtain GPS data if none.
	 */

	var number_of_tbeacons = 0
	for j := range g_misc_config_p.num_beacons {
		if g_misc_config_p.beacon[j].btype == BEACON_TRACKER {
			number_of_tbeacons++
		}
	}

	var now = C.time(nil)
	var sb_prev_time C.time_t = 0  /* Time of most recent transmission. */
	var sb_prev_course C.float = 0 /* Most recent course reported. */

	for {
		/*
		 * Sleep until time for the earliest scheduled or
		 * the soonest we could transmit due to corner pegging.
		 */

		var earliest = now + 60*60
		for j := range g_misc_config_p.num_beacons {
			if g_misc_config_p.beacon[j].btype != BEACON_IGNORE {
				earliest = min(g_misc_config_p.beacon[j].next, earliest)
			}
		}

		if g_misc_config_p.sb_configured > 0 && number_of_tbeacons > 0 {
			earliest = min(now+C.long(g_misc_config_p.sb_turn_time), earliest)
			earliest = min(now+C.long(g_misc_config_p.sb_fast_rate), earliest)
		}

		if earliest > now {
			SLEEP_SEC(int(earliest - now))
		}

		/*
		 * Woke up.  See what needs to be done.
		 */
		now = C.time(nil)

		/*
		 * Get information from GPS if being used.
		 * This needs to be done before the next scheduled tracker
		 * beacon because corner pegging make it sooner.
		 */
		var gpsinfo C.dwgps_info_t

		if number_of_tbeacons > 0 {
			var fix = C.dwgps_read(&gpsinfo)
			var my_speed_mph = DW_KNOTS_TO_MPH(float64(gpsinfo.speed_knots))

			if g_tracker_debug_level >= 1 {
				var tm C.struct_tm
				C.localtime_r(&now, &tm)

				var hms [20]C.char
				C.strftime(&hms[0], C.ulong(len(hms)), C.CString("%H:%M:%S"), &tm)

				text_color_set(DW_COLOR_DEBUG)
				switch fix {
				case DWFIX_3D:
					dw_printf("%s  3D, %.6f, %.6f, %.1f mph, %.0f\xc2\xb0, %.1f m\n", C.GoString(&hms[0]), gpsinfo.dlat, gpsinfo.dlon, my_speed_mph, gpsinfo.track, gpsinfo.altitude)
				case DWFIX_2D:
					dw_printf("%s  2D, %.6f, %.6f, %.1f mph, %.0f\xc2\xb0\n", C.GoString(&hms[0]), gpsinfo.dlat, gpsinfo.dlon, my_speed_mph, gpsinfo.track)
				default:
					dw_printf("%s  No GPS fix\n", C.GoString(&hms[0]))
				}
			}

			/* Don't complain here for no fix. */
			/* Possibly at the point where about to transmit. */

			/*
			 * Run SmartBeaconing calculation if configured and GPS data available.
			 */
			if g_misc_config_p.sb_configured > 0 && fix >= DWFIX_2D {
				var tnext = sb_calculate_next_time(now,
					C.float(DW_KNOTS_TO_MPH(float64(gpsinfo.speed_knots))), gpsinfo.track,
					sb_prev_time, sb_prev_course)

				for j := range g_misc_config_p.num_beacons {
					if g_misc_config_p.beacon[j].btype == BEACON_TRACKER {
						/* Haven't thought about the consequences of SmartBeaconing */
						/* and having more than one tbeacon configured. */
						if tnext < g_misc_config_p.beacon[j].next {
							g_misc_config_p.beacon[j].next = tnext
						}
					}
				} /* Update next time if sooner. */
			} /* apply SmartBeaconing */
		} /* tbeacon(s) configured. */

		/*
		 * Send if the time has arrived.
		 */
		for j := range g_misc_config_p.num_beacons {
			var bp = &(g_misc_config_p.beacon[j])

			if bp.btype == BEACON_IGNORE {
				continue
			}

			if bp.next <= now {
				/* Send the beacon. */

				beacon_send(int(j), &gpsinfo)

				/* Calculate when the next one should be sent. */
				/* Easy for fixed interval.  SmartBeaconing takes more effort. */

				if bp.btype == BEACON_TRACKER {
					if gpsinfo.fix < DWFIX_2D {
						/* Fix not available so beacon was not sent. */

						if g_misc_config_p.sb_configured > 0 {
							/* Try again in a couple seconds. */
							bp.next = now + 2
						} else {
							/* Stay with the schedule. */
							/* Important for slotted.  Might reconsider otherwise. */
							bp.next += C.long(bp.every)
						}
					} else if g_misc_config_p.sb_configured > 0 {
						/* Remember most recent tracker beacon. */
						/* Compute next time if not turning. */

						sb_prev_time = now
						sb_prev_course = gpsinfo.track

						bp.next = sb_calculate_next_time(now,
							C.float(DW_KNOTS_TO_MPH(float64(gpsinfo.speed_knots))), gpsinfo.track,
							sb_prev_time, sb_prev_course)
					} else {
						/* Tracker beacon, fixed spacing. */
						bp.next += C.long(bp.every)
					}
				} else {
					/* Non-tracker beacon, fixed spacing. */
					/* Increment by 'every' so slotted times come out right. */
					/* i.e. Don't take relative to now in case there was some delay. */

					bp.next += C.long(bp.every)

					// https://github.com/wb2osz/direwolf/pull/301
					// https://github.com/wb2osz/direwolf/pull/301
					// This happens with a portable system with no Internet connection.
					// On reboot, the time is in the past.
					// After time gets set from GPS, all beacons from that interval are sent.
					// FIXME:  This will surely break time slotted scheduling.
					// TODO: The correct fix will be using monotonic, rather than clock, time.

					/* craigerl: if next beacon is scheduled in the past, then set next beacon relative to now (happens when NTP pushes clock AHEAD) */
					/* fixme: if NTP sets clock BACK an hour, this thread will sleep for that hour */
					if bp.next < now {
						bp.next = now + C.long(bp.every)
						text_color_set(DW_COLOR_INFO)
						dw_printf("\nSystem clock appears to have jumped forward.  Beacon schedule updated.\n\n")
					}
				}
			} /* if time to send it */
		} /* for each configured beacon */
	} /* do forever */
} /* end beacon_thread */

/*-------------------------------------------------------------------
 *
 * Name:        sb_calculate_next_time
 *
 * Purpose:     Calculate next transmission time using the SmartBeaconing algorithm.
 *
 * Inputs:	now			- Current time.
 *
 *		current_speed_mph	- Current speed from GPS.
 *				  	  Not expecting G_UNKNOWN but should check for it.
 *
 *		current_course		- Current direction of travel.
 *				  	  Could be G_UNKNOWN if stationary.
 *
 *		last_xmit_time		- Time of most recent transmission.
 *
 *		last_xmit_course	- Direction included in most recent transmission.
 *
 * Global In:	g_misc_config_p.
 *			sb_configured	TRUE if SmartBeaconing is configured.
 *			sb_fast_speed	MPH
 *			sb_fast_rate	seconds
 *			sb_slow_speed	MPH
 *			sb_slow_rate	seconds
 *			sb_turn_time	seconds
 *			sb_turn_angle	degrees
 *			sb_turn_slope	degrees * MPH
 *
 * Returns:	Time of next transmission.
 *		Could vary from now to sb_slow_rate in the future.
 *
 * Caution:	The algorithm is defined in MPH units.    GPS uses knots.
 *		The caller must be careful about using the proper conversions.
 *
 *--------------------------------------------------------------------*/

/* Difference between two angles. */

func heading_change(a, b C.float) C.float {
	var diff = C.float(math.Abs(float64(a) - float64(b)))

	if diff <= 180. {
		return (diff)
	} else {
		return (360. - diff)
	}
}

func sb_calculate_next_time(now C.time_t, current_speed_mph C.float, current_course C.float, last_xmit_time C.time_t, last_xmit_course C.float) C.time_t {
	var beacon_rate C.int

	/*
	 * Compute time between beacons for travelling in a straight line.
	 */

	if current_speed_mph == G_UNKNOWN {
		beacon_rate = C.int(math.Round(float64(g_misc_config_p.sb_fast_rate+g_misc_config_p.sb_slow_rate) / 2.))
	} else if current_speed_mph > C.float(g_misc_config_p.sb_fast_speed) {
		beacon_rate = g_misc_config_p.sb_fast_rate
	} else if current_speed_mph < C.float(g_misc_config_p.sb_slow_speed) {
		beacon_rate = g_misc_config_p.sb_slow_rate
	} else {
		/* Can't divide by 0 assuming sb_slow_speed > 0. */
		beacon_rate = C.int(math.Round(float64(C.float(g_misc_config_p.sb_fast_rate*g_misc_config_p.sb_fast_speed) / current_speed_mph)))
	}

	if g_tracker_debug_level >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("SmartBeaconing: Beacon Rate = %d seconds for %.1f MPH\n", beacon_rate, current_speed_mph)
	}

	var next_time = last_xmit_time + C.long(beacon_rate)

	/*
	 * Test for "Corner Pegging" if moving.
	 */
	if current_speed_mph != G_UNKNOWN && current_speed_mph >= 1.0 &&
		current_course != G_UNKNOWN && last_xmit_course != G_UNKNOWN {
		var change = heading_change(current_course, last_xmit_course)
		var turn_threshold = C.float(g_misc_config_p.sb_turn_angle) + C.float(g_misc_config_p.sb_turn_slope)/current_speed_mph

		if change > turn_threshold && now >= last_xmit_time+C.long(g_misc_config_p.sb_turn_time) {
			if g_tracker_debug_level >= 2 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("SmartBeaconing: Send now for heading change of %.0f\n", change)
			}

			next_time = now
		}
	}

	return (next_time)
} /* end sb_calculate_next_time */

/*-------------------------------------------------------------------
 *
 * Name:        beacon_send
 *
 * Purpose:     Transmit one beacon after it was determined to be time.
 *
 * Inputs:	j			Index into beacon configuration array below.
 *
 *		gpsinfo			Information from GPS.  Used only for TBEACON.
 *
 * Global In:	g_misc_config_p.beacon		Array of beacon configurations.
 *
 * Outputs:	Destination(s) specified:
 *		 - Transmit queue.
 *		 - IGate.
 *		 - Simulated reception.
 *
 * Description:	Prepare text in monitor format.
 *		Convert to packet object.
 *		Send to desired destination(s).
 *
 *--------------------------------------------------------------------*/

func beacon_send(j int, gpsinfo *C.dwgps_info_t) {
	var bp = &(g_misc_config_p.beacon[j])

	if !(bp.sendto_chan >= 0) { //nolint:staticcheck
		panic("assert(bp.sendto_chan >= 0)")
	}

	/*
	 * Obtain source call for the beacon.
	 * This could potentially be different on different channels.
	 * When sending to IGate server, use call from first radio channel.
	 *
	 * Check added in version 1.0a.  Previously used index of -1.
	 *
	 * Version 1.1 - channel should now be 0 for IGate.
	 * Type of destination is encoded separately.
	 */
	var mycall string

	if g_modem_config_p.chan_medium[bp.sendto_chan] == MEDIUM_IGATE { // ICHANNEL uses chan 0 mycall.
		// TODO: Maybe it should be allowed to have own.
		mycall = C.GoString(&g_modem_config_p.mycall[0][0])
	} else {
		mycall = C.GoString(&g_modem_config_p.mycall[bp.sendto_chan][0])
	}

	if mycall == "" || mycall == "NOCALL" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("MYCALL not set for beacon to chan %d in config file line %d.\n", bp.sendto_chan, bp.lineno)
		return
	}

	/*
	 * Prepare the monitor format header.
	 *
	 * 	src > dest [ , via ]
	 */

	var beacon_text string
	if bp.source != nil {
		beacon_text = C.GoString(bp.source)
	} else {
		beacon_text = mycall
	}
	beacon_text += ">"

	if bp.dest != nil {
		beacon_text += C.GoString(bp.dest)
	} else {
		var stemp = fmt.Sprintf("%s%1d%1d", C.APP_TOCALL, C.MAJOR_VERSION, C.MINOR_VERSION)
		beacon_text += stemp
	}

	if bp.via != nil {
		beacon_text += "," + C.GoString(bp.via)
	}
	beacon_text += ":"

	/*
	 * If the COMMENTCMD option was specified, run specified command to get variable part.
	 * Result is any fixed part followed by any variable part.
	 */

	// TODO: test & document.

	var super_comment = ""
	if bp.comment != nil {
		super_comment = C.GoString(bp.comment)
	}

	if bp.commentcmd != nil {
		/* Run given command to get variable part of comment. */
		var var_comment, k = dw_run_cmd(C.GoString(bp.commentcmd), 2)

		if k == nil {
			super_comment += string(var_comment)
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("xBEACON, config file line %d, COMMENTCMD failure: %s.\n", bp.lineno, k)
		}
	}

	/*
	 * Add the info part depending on beacon type.
	 */
	switch bp.btype {
	case BEACON_POSITION:

		beacon_text += encode_position(bp.messaging, bp.compress,
			bp.lat, bp.lon, bp.ambiguity,
			C.int(math.Round(DW_METERS_TO_FEET(float64(bp.alt_m)))),
			bp.symtab, bp.symbol,
			C.int(bp.power), C.int(bp.height), C.int(bp.gain), &bp.dir[0],
			G_UNKNOWN, G_UNKNOWN, /* course, speed */
			bp.freq, bp.tone, bp.offset,
			C.CString(super_comment))

	case BEACON_OBJECT:

		beacon_text += encode_object(&bp.objname[0], bp.compress, 1, bp.lat, bp.lon, bp.ambiguity,
			bp.symtab, bp.symbol,
			C.int(bp.power), C.int(bp.height), C.int(bp.gain), &bp.dir[0],
			G_UNKNOWN, G_UNKNOWN, /* course, speed */
			bp.freq, bp.tone, bp.offset, C.CString(super_comment))

	case BEACON_TRACKER:

		if gpsinfo.fix >= DWFIX_2D {
			/* Transmit altitude only if user asked for it. */
			/* A positive altitude in the config file enables */
			/* transmission of altitude from GPS. */

			var my_alt_ft C.int = G_UNKNOWN
			if gpsinfo.fix >= 3 && gpsinfo.altitude != G_UNKNOWN && bp.alt_m > 0 {
				my_alt_ft = C.int(math.Round(DW_METERS_TO_FEET(float64(gpsinfo.altitude))))
			}

			/* Round to nearest integer. retaining unknown state. */
			var coarse C.int = G_UNKNOWN
			if gpsinfo.track != G_UNKNOWN {
				coarse = C.int(math.Round(float64(gpsinfo.track)))
			}

			beacon_text += encode_position(bp.messaging, bp.compress,
				gpsinfo.dlat, gpsinfo.dlon, bp.ambiguity, my_alt_ft,
				bp.symtab, bp.symbol,
				C.int(bp.power), C.int(bp.height), C.int(bp.gain), &bp.dir[0],
				coarse, C.int(math.Round(float64(gpsinfo.speed_knots))),
				bp.freq, bp.tone, bp.offset,
				C.CString(super_comment))

			/* Write to log file for testing. */
			/* The idea is to run log2gpx and map the result rather than */
			/* actually transmitting and relying on someone else to receive */
			/* the signals. */

			if g_tracker_debug_level >= 3 {
				var A C.decode_aprs_t
				A.g_freq = G_UNKNOWN
				A.g_offset = G_UNKNOWN
				A.g_tone = G_UNKNOWN
				A.g_dcs = G_UNKNOWN

				C.strcpy(&A.g_src[0], C.CString(mycall))
				A.g_symbol_table = bp.symtab
				A.g_symbol_code = bp.symbol
				A.g_lat = gpsinfo.dlat
				A.g_lon = gpsinfo.dlon
				A.g_speed_mph = C.float(DW_KNOTS_TO_MPH(float64(gpsinfo.speed_knots)))
				A.g_course = C.float(coarse)
				A.g_altitude_ft = C.float(DW_METERS_TO_FEET(float64(gpsinfo.altitude)))

				/* Fake channel of 999 to distinguish from real data. */
				var alevel C.struct_alevel_s
				log_write(999, &A, nil, alevel, 0)
			}
		} else {
			return /* No fix.  Skip this time. */
		}

	case BEACON_CUSTOM:

		if bp.custom_info != nil {
			/* Fixed handcrafted text. */
			beacon_text += C.GoString(bp.custom_info)
		} else if bp.custom_infocmd != nil {
			/* Run given command to obtain the info part for packet. */

			var info_part, k = dw_run_cmd(C.GoString(bp.custom_infocmd), 2)

			if k == nil {
				beacon_text += string(info_part)
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("CBEACON, config file line %d, INFOCMD failure: %s.\n", bp.lineno, k)
				beacon_text = "" // abort!
			}
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Internal error. custom_info is null.\n")
			beacon_text = "" // abort!
		}

	case BEACON_IGATE:

		{
			var last_minutes C.int = 30

			var stuff = fmt.Sprintf("<IGATE,MSG_CNT=%d,PKT_CNT=%d,DIR_CNT=%d,LOC_CNT=%d,RF_CNT=%d,UPL_CNT=%d,DNL_CNT=%d",
				C.igate_get_msg_cnt(),
				C.igate_get_pkt_cnt(),
				C.mheard_count(0, last_minutes),
				C.mheard_count(g_igate_config_p.max_digi_hops, last_minutes),
				C.mheard_count(8, last_minutes),
				C.igate_get_upl_cnt(),
				C.igate_get_dnl_cnt())

			beacon_text += stuff
		}
	default:
	} /* switch beacon type. */

	/*
	 * Parse monitor format into form for transmission.
	 */
	if beacon_text == "" {
		return
	}

	var strict C.int = 1 // Strict packet checking because they will go over air.
	var pp = C.ax25_from_text(C.CString(beacon_text), strict)

	if pp != nil {
		/* Send to desired destination. */

		switch bp.sendto_type {
		case SENDTO_IGATE:
			text_color_set(DW_COLOR_XMIT)
			dw_printf("[ig] %s\n", beacon_text)

			C.igate_send_rec_packet(-1, pp) // Channel -1 to avoid RF>IS filtering.
			C.ax25_delete(pp)
		case SENDTO_RECV:
			/* Simulated reception from radio. */

			var alevel C.alevel_t
			C.memset(unsafe.Pointer(&alevel), 0xff, C.sizeof_alevel_t)
			C.dlq_rec_frame(bp.sendto_chan, 0, 0, pp, alevel, 0, 0, C.CString(""))
		default:
			C.tq_append(bp.sendto_chan, TQ_PRIO_1_LO, pp)
		}
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Failed to parse packet constructed from line %d.\n", bp.lineno)
		dw_printf("%s\n", beacon_text)
	}
} /* end beacon_send */
