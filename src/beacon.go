package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Transmit messages on a fixed schedule.
 *
 * Description:	Transmit periodic messages as specified in the config file.
 *
 *---------------------------------------------------------------*/

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
)

type BeaconService struct {
	modemConfig       *audio_s
	miscConfig        *misc_config_s
	igateConfig       *igate_config_s
	gps               *GPS
	trackerDebugLevel int
}

/*-------------------------------------------------------------------
 *
 * Name:        NewBeaconService
 *
 * Purpose:     Initialize the beacon process.
 *
 * Inputs:	pmodem		- Audio device and modem configuration.
 *			  Used only to find valid channels.
 *
 *		pconfig		- misc. configuration from config file.
 *			  Beacon stuff ended up here.
 *
 *		pigate		- IGate configuration.
 *			  Need this for calculating IGate statistics.
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

func NewBeaconService(pmodem *audio_s, pconfig *misc_config_s, pigate *igate_config_s, gps *GPS) *BeaconService {
	var bs = &BeaconService{ //nolint:exhaustruct_v5
		modemConfig: pmodem,
		miscConfig:  pconfig,
		igateConfig: pigate,
		gps:         gps,
	}

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

	for j := range bs.miscConfig.num_beacons {
		var channel = bs.miscConfig.beacon[j].sendto_chan

		if channel < 0 {
			channel = 0 /* For IGate, use channel 0 call. */
		}

		if channel >= MAX_TOTAL_CHANS {
			channel = 0 // For ICHANNEL, use channel 0 call.
		}

		if bs.modemConfig.chan_medium[channel] == MEDIUM_RADIO ||
			bs.modemConfig.chan_medium[channel] == MEDIUM_NETTNC {
			if !IsNoCall(bs.modemConfig.mycall[channel]) {
				switch bs.miscConfig.beacon[j].btype {
				case BEACON_OBJECT:
					/* Object name is required. */
					if bs.miscConfig.beacon[j].objname == "" {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("Config file, line %d: OBJNAME is required for OBEACON.\n", bs.miscConfig.beacon[j].lineno)
						bs.miscConfig.beacon[j].btype = BEACON_IGNORE

						continue
					}
					/* Fall thru.  Ignore any warning about missing break. */
					fallthrough

				case BEACON_POSITION:
					/* Location is required. */
					if bs.miscConfig.beacon[j].lat.IsNothing() || bs.miscConfig.beacon[j].lon.IsNothing() {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("Config file, line %d: Latitude and longitude are required.\n", bs.miscConfig.beacon[j].lineno)
						bs.miscConfig.beacon[j].btype = BEACON_IGNORE

						continue
					}

					/* INFO and INFOCMD are only for Custom Beacon. */

					if bs.miscConfig.beacon[j].custom_info != "" || bs.miscConfig.beacon[j].custom_infocmd != "" {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("Config file, line %d: INFO or INFOCMD are allowed only for custom beacon.\n", bs.miscConfig.beacon[j].lineno)
						dw_printf("INFO and INFOCMD allow you to specify contents of the Information field so it\n")
						dw_printf("so it would not make sense to use these with other beacon types which construct\n")
						dw_printf("the Information field. Perhaps you want to use COMMENT or COMMENTCMD option.\n")
						// bs.miscConfig.beacon[j].btype = BEACON_IGNORE;
						continue
					}

				case BEACON_TRACKER:
					{
						var gpsinfo GPSInfo

						var fix = bs.gps.Read(&gpsinfo)
						if fix == DWFIX_NOT_INIT {
							text_color_set(DW_COLOR_ERROR)
							dw_printf("Config file, line %d: GPS must be configured to use TBEACON.\n", bs.miscConfig.beacon[j].lineno)
							bs.miscConfig.beacon[j].btype = BEACON_IGNORE

							dw_printf("You must specify the source of the GPS data in your configuration file.\n")
							dw_printf("It can be either GPSD, meaning the gpsd daemon, or GPSNMEA for\n")
							dw_printf("for a serial port connection with exclusive use.\n")
						}
					}

					/* INFO and INFOCMD are only for Custom Beacon. */

					if bs.miscConfig.beacon[j].custom_info != "" || bs.miscConfig.beacon[j].custom_infocmd != "" {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("Config file, line %d: INFO or INFOCMD are allowed only for custom beacon.\n", bs.miscConfig.beacon[j].lineno)
						dw_printf("INFO and INFOCMD allow you to specify contents of the Information field so it\n")
						dw_printf("so it would not make sense to use these with other beacon types which construct\n")
						dw_printf("the Information field. Perhaps you want to use COMMENT or COMMENTCMD option.\n")
						// bs.miscConfig.beacon[j].btype = BEACON_IGNORE;
						continue
					}

				case BEACON_CUSTOM:
					/* INFO or INFOCMD is required. */
					if bs.miscConfig.beacon[j].custom_info == "" && bs.miscConfig.beacon[j].custom_infocmd == "" {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("Config file, line %d: INFO or INFOCMD is required for custom beacon.\n", bs.miscConfig.beacon[j].lineno)
						bs.miscConfig.beacon[j].btype = BEACON_IGNORE

						continue
					}

				case BEACON_IGATE:
					/* Doesn't make sense if IGate is not configured. */
					if bs.igateConfig.t2_server_name == "" ||
						bs.igateConfig.t2_login == "" ||
						bs.igateConfig.t2_passcode == "" {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("Config file, line %d: Doesn't make sense to use IBEACON without IGate Configured.\n", bs.miscConfig.beacon[j].lineno)
						dw_printf("IBEACON has been disabled.\n")

						bs.miscConfig.beacon[j].btype = BEACON_IGNORE

						continue
					}

				case BEACON_IGNORE:
				}
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file, line %d: MYCALL must be set for beacon on channel %d. \n", bs.miscConfig.beacon[j].lineno, channel)
				bs.miscConfig.beacon[j].btype = BEACON_IGNORE
			}
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: Invalid channel number %d for beacon. \n", bs.miscConfig.beacon[j].lineno, channel)
			bs.miscConfig.beacon[j].btype = BEACON_IGNORE
		}
	}

	/*
	 * Calculate first time for each beacon from the 'slot' or 'delay' value.
	 */

	var now = time.Now()

	for j := range bs.miscConfig.num_beacons {
		var bp = &(bs.miscConfig.beacon[j])
		logrus.WithFields(logrus.Fields{
			"beacon":  j,
			"channel": bp.sendto_chan,
			"delay":   bp.delay,
			"slot":    bp.slot,
			"every":   bp.every,
		}).Debug("beacon")

		/*
		 * If timeslots, there must be a full number of beacon intervals per hour.
		 */

		if slot, slotted := bp.slot.Get(); slotted {
			if !IS_GOOD(bp.every) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file, line %d: When using timeslots, there must be a whole number of beacon intervals per hour.\n", bp.lineno)

				// Try to make it valid by adjusting up or down.

				for n := 1; ; n++ {
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
			bp.delay = slot - (now.Minute()*60 + now.Second())
			for bp.delay > bp.every {
				bp.delay -= bp.every
			}

			for bp.delay < 5 {
				bp.delay += bp.every
			}
		}

		bs.miscConfig.beacon[j].next = now.Add(time.Duration(bs.miscConfig.beacon[j].delay) * time.Second)
	}

	return bs
} /* end NewBeaconService */

func (bs *BeaconService) SetDebug(level int) {
	bs.trackerDebugLevel = level
}

/*-------------------------------------------------------------------
 *
 * Name:        Start
 *
 * Purpose:     Start the beacon thread.
 *
 * Inputs:	ctx	- Stops the beacon thread when cancelled.
 *
 * Description:	Call after all configuration (e.g. SetDebug) is done.
 *		Starts the goroutine only if at least one beacon is valid.
 *
 *--------------------------------------------------------------------*/

func (bs *BeaconService) Start(ctx context.Context) {
	var count = 0

	for j := range bs.miscConfig.num_beacons {
		if bs.miscConfig.beacon[j].btype != BEACON_IGNORE {
			count++
		}
	}

	if count >= 1 {
		go bs.thread(ctx)
	}
}

func IS_GOOD(x int) bool {
	return x >= 1 && (3600/(x))*(x) == 3600
}

/*-------------------------------------------------------------------
 *
 * Name:        thread
 *
 * Purpose:     Transmit beacons when it is time.
 *
 * Inputs:	bs.miscConfig.beacon
 *
 * Outputs:	bs.miscConfig.beacon[].next_time
 *
 * Description:	Go to sleep until it is time for the next beacon.
 *		Transmit any beacons scheduled for now.
 *		Repeat.
 *
 *--------------------------------------------------------------------*/

func (bs *BeaconService) thread(ctx context.Context) {
	/*
	 * SmartBeaconing state.
	 */

	/*
	 * See if any tracker beacons are configured.
	 * No need to obtain GPS data if none.
	 */
	var number_of_tbeacons = 0

	for j := range bs.miscConfig.num_beacons {
		if bs.miscConfig.beacon[j].btype == BEACON_TRACKER {
			number_of_tbeacons++
		}
	}

	var now = time.Now()
	var sb_prev_time time.Time              /* Time of most recent transmission. */
	var sb_prev_course maybe.Maybe[float64] /* Most recent course reported. */

	// The sleep below is where this thread spends nearly all of its life, but
	// not all of it: a beacon that is already overdue - a short EVERY, or a
	// stall - goes round without sleeping at all, and would otherwise carry on
	// transmitting throughout a shutdown.
	for ctx.Err() == nil {
		/*
		 * Sleep until time for the earliest scheduled or
		 * the soonest we could transmit due to corner pegging.
		 */
		var earliest = now.Add(time.Hour)

		for j := range bs.miscConfig.num_beacons {
			if bs.miscConfig.beacon[j].btype != BEACON_IGNORE {
				var t = bs.miscConfig.beacon[j].next
				if t.Before(earliest) {
					earliest = t
				}
			}
		}

		if bs.miscConfig.sb_configured && number_of_tbeacons > 0 {
			var t = now.Add(time.Duration(bs.miscConfig.sb_turn_time) * time.Second)
			if t.Before(earliest) {
				earliest = t
			}

			t = now.Add(time.Duration(bs.miscConfig.sb_fast_rate) * time.Second)
			if t.Before(earliest) {
				earliest = t
			}
		}

		if earliest.After(now) {
			// Almost all of a beacon thread's life is spent here, so this
			// is where a cancellation has to reach it.
			if !sleepCtx(ctx, earliest.Sub(now)) {
				return
			}
		}

		/*
		 * Woke up.  See what needs to be done.
		 */
		now = time.Now()

		/*
		 * Get information from GPS if being used.
		 * This needs to be done before the next scheduled tracker
		 * beacon because corner pegging make it sooner.
		 */
		var gpsinfo GPSInfo

		if number_of_tbeacons > 0 {
			var fix = bs.gps.Read(&gpsinfo)
			var my_speed_mph = maybe.Fmap(DW_KNOTS_TO_MPH, gpsinfo.speed_knots)

			if bs.trackerDebugLevel >= 1 {
				var hms = now.Format("15:04:05")

				text_color_set(DW_COLOR_DEBUG)

				switch fix {
				case DWFIX_3D:
					dw_printf("%s  3D, %s, %s, %s mph, %s\xc2\xb0, %s m\n", hms,
						formatMaybeFloat("%.6f", gpsinfo.dlat), formatMaybeFloat("%.6f", gpsinfo.dlon),
						formatMaybeFloat("%.1f", my_speed_mph), formatMaybeFloat("%.0f", gpsinfo.track),
						formatMaybeFloat("%.1f", gpsinfo.altitude))
				case DWFIX_2D:
					dw_printf("%s  2D, %s, %s, %s mph, %s\xc2\xb0\n", hms,
						formatMaybeFloat("%.6f", gpsinfo.dlat), formatMaybeFloat("%.6f", gpsinfo.dlon),
						formatMaybeFloat("%.1f", my_speed_mph), formatMaybeFloat("%.0f", gpsinfo.track))
				default:
					dw_printf("%s  No GPS fix\n", hms)
				}
			}

			/* Don't complain here for no fix. */
			/* Possibly at the point where about to transmit. */

			/*
			 * Run SmartBeaconing calculation if configured and GPS data available.
			 */
			if bs.miscConfig.sb_configured && fix >= DWFIX_2D {
				var tnext = bs.sbCalculateNextTime(now,
					my_speed_mph, gpsinfo.track,
					sb_prev_time, sb_prev_course)

				for j := range bs.miscConfig.num_beacons {
					if bs.miscConfig.beacon[j].btype == BEACON_TRACKER {
						/* Haven't thought about the consequences of SmartBeaconing */
						/* and having more than one tbeacon configured. */
						if tnext.Before(bs.miscConfig.beacon[j].next) {
							bs.miscConfig.beacon[j].next = tnext
						}
					}
				} /* Update next time if sooner. */
			} /* apply SmartBeaconing */
		} /* tbeacon(s) configured. */

		/*
		 * Send if the time has arrived.
		 */
		for j := range bs.miscConfig.num_beacons {
			var bp = &(bs.miscConfig.beacon[j])

			if bp.btype == BEACON_IGNORE {
				continue
			}

			if !bp.next.After(now) {
				/* Send the beacon. */
				bs.send(ctx, j, &gpsinfo)

				/* Calculate when the next one should be sent. */
				/* Easy for fixed interval.  SmartBeaconing takes more effort. */

				if bp.btype == BEACON_TRACKER {
					var _, _, havePosition = trackerPosition(&gpsinfo)
					if !havePosition {
						/* No position available so beacon was not sent. */
						if bs.miscConfig.sb_configured {
							/* Try again in a couple seconds. */
							bp.next = now.Add(2 * time.Second)
						} else {
							/* Stay with the schedule. */
							/* Important for slotted.  Might reconsider otherwise. */
							bp.next = bp.next.Add(time.Duration(bp.every) * time.Second)
						}
					} else if bs.miscConfig.sb_configured {
						/* Remember most recent tracker beacon. */
						/* Compute next time if not turning. */
						sb_prev_time = now
						sb_prev_course = gpsinfo.track

						bp.next = bs.sbCalculateNextTime(now,
							maybe.Fmap(DW_KNOTS_TO_MPH, gpsinfo.speed_knots), gpsinfo.track,
							sb_prev_time, sb_prev_course)
					} else {
						/* Tracker beacon, fixed spacing. */
						bp.next = bp.next.Add(time.Duration(bp.every) * time.Second)
					}
				} else {
					/* Non-tracker beacon, fixed spacing. */
					/* Increment by 'every' so slotted times come out right. */
					/* i.e. Don't take relative to now in case there was some delay. */
					bp.next = bp.next.Add(time.Duration(bp.every) * time.Second)

					// https://github.com/wb2osz/direwolf/pull/301
					// https://github.com/wb2osz/direwolf/pull/301
					// This happens with a portable system with no Internet connection.
					// On reboot, the time is in the past.
					// After time gets set from GPS, all beacons from that interval are sent.
					// FIXME:  This will surely break time slotted scheduling.
					// TODO: The correct fix will be using monotonic, rather than clock, time.

					/* craigerl: if next beacon is scheduled in the past, then set next beacon relative to now (happens when NTP pushes clock AHEAD) */
					/* fixme: if NTP sets clock BACK an hour, this thread will sleep for that hour */
					if bp.next.Before(now) {
						bp.next = now.Add(time.Duration(bp.every) * time.Second)

						text_color_set(DW_COLOR_INFO)
						dw_printf("\nSystem clock appears to have jumped forward.  Beacon schedule updated.\n\n")
					}
				}
			} /* if time to send it */
		} /* for each configured beacon */
	} /* do forever */
} /* end thread */

/*-------------------------------------------------------------------
 *
 * Name:        sbCalculateNextTime
 *
 * Purpose:     Calculate next transmission time using the SmartBeaconing algorithm.
 *
 * Inputs:	now			- Current time.
 *
 *		current_speed_mph	- Current speed from GPS.
 *			  	  Not expecting Nothing but should check for it.
 *
 *		current_course		- Current direction of travel.
 *			  	  Could be Nothing if stationary.
 *
 *		last_xmit_time		- Time of most recent transmission.
 *
 *		last_xmit_course	- Direction included in most recent transmission.
 *
 * Global In:	bs.miscConfig.
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

func heading_change(a, b float64) float64 {
	var diff = math.Abs(a - b)

	if diff <= 180. {
		return (diff)
	} else {
		return (360. - diff)
	}
}

func (bs *BeaconService) sbCalculateNextTime(
	now time.Time,
	current_speed_mph maybe.Maybe[float64],
	current_course maybe.Maybe[float64],
	last_xmit_time time.Time,
	last_xmit_course maybe.Maybe[float64],
) time.Time {
	var beacon_rate int

	/*
	 * Compute time between beacons for travelling in a straight line.
	 */

	var speed_mph, speed_known = current_speed_mph.Get()

	switch {
	case !speed_known:
		beacon_rate = int(math.Round(float64(bs.miscConfig.sb_fast_rate+bs.miscConfig.sb_slow_rate) / 2.))
	case speed_mph > float64(bs.miscConfig.sb_fast_speed):
		beacon_rate = bs.miscConfig.sb_fast_rate
	case speed_mph < float64(bs.miscConfig.sb_slow_speed):
		beacon_rate = bs.miscConfig.sb_slow_rate
	default:
		/* Can't divide by 0 assuming sb_slow_speed > 0. */
		beacon_rate = int(math.Round(float64(bs.miscConfig.sb_fast_rate*bs.miscConfig.sb_fast_speed) / speed_mph))
	}

	if bs.trackerDebugLevel >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("SmartBeaconing: Beacon Rate = %d seconds for %s MPH\n", beacon_rate, formatMaybeFloat("%.1f", current_speed_mph))
	}

	var next_time = last_xmit_time.Add(time.Duration(beacon_rate) * time.Second)

	/*
	 * Test for "Corner Pegging" if moving.
	 */
	var course_change = maybe.LiftA2(heading_change, current_course, last_xmit_course)

	if change, turning := course_change.Get(); speed_known && speed_mph >= 1.0 && turning {
		var turn_threshold = float64(bs.miscConfig.sb_turn_angle) + float64(bs.miscConfig.sb_turn_slope)/speed_mph

		if change > turn_threshold && !now.Before(last_xmit_time.Add(time.Duration(bs.miscConfig.sb_turn_time)*time.Second)) {
			if bs.trackerDebugLevel >= 2 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("SmartBeaconing: Send now for heading change of %.0f\n", change)
			}

			next_time = now
		}
	}

	return (next_time)
} /* end sbCalculateNextTime */

// trackerPosition is the position a TBEACON can transmit from a GPS reading.
// A fix is not a promise of a position: gpsd can raise the mode without ever
// having reported a latitude and longitude.  The scheduler asks the same
// question as send does, so a beacon that was skipped is not scheduled for as
// though it had gone out.
func trackerPosition(gpsinfo *GPSInfo) (float64, float64, bool) {
	var dlat, haveLat = gpsinfo.dlat.Get()
	var dlon, haveLon = gpsinfo.dlon.Get()

	return dlat, dlon, gpsinfo.fix >= DWFIX_2D && haveLat && haveLon
}

// beaconPosition is the position a fixed beacon was configured with.
// NewBeaconService refuses a position or object beacon that has neither, so
// the absent case should be unreachable; unwrapping keeps it that way rather
// than letting a beacon without a position invent a coordinate.
func beaconPosition(bp *beacon_s) (float64, float64, bool) {
	var dlat, haveLat = bp.lat.Get()
	var dlon, haveLon = bp.lon.Get()

	return dlat, dlon, haveLat && haveLon
}

// beaconPHG is a PHG component from the beacon configuration, whose "not
// specified" is zero.  The power, height and gain fields are still plain
// numbers; see issue #619.
func beaconPHG(value float64) maybe.Maybe[int] {
	if value == 0 {
		return maybe.Nothing[int]()
	}

	return maybe.Just(int(value))
}

// beaconAltitudeFeet converts a configured beacon altitude in metres to the
// feet EncodePosition wants, or Nothing if no altitude was configured.
func beaconAltitudeFeet(alt_m maybe.Maybe[float64]) maybe.Maybe[int] {
	return maybe.Fmap(func(meters float64) int {
		return int(math.Round(DW_METERS_TO_FEET(meters)))
	}, alt_m)
}

/*-------------------------------------------------------------------
 *
 * Name:        send
 *
 * Purpose:     Transmit one beacon after it was determined to be time.
 *
 * Inputs:	j			Index into beacon configuration array below.
 *
 *		gpsinfo			Information from GPS.  Used only for TBEACON.
 *
 * Global In:	bs.miscConfig.beacon		Array of beacon configurations.
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

func (bs *BeaconService) send(ctx context.Context, j int, gpsinfo *GPSInfo) {
	var bp = &(bs.miscConfig.beacon[j])

	if bp.sendto_chan < 0 {
		logrus.WithFields(logrus.Fields{
			"line":    bp.lineno,
			"channel": bp.sendto_chan,
		}).Error("Beacon has no channel to send to, skipping it")

		return
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

	if bs.modemConfig.chan_medium[bp.sendto_chan] == MEDIUM_IGATE { // ICHANNEL uses chan 0 mycall.
		// TODO: Maybe it should be allowed to have own.
		mycall = bs.modemConfig.mycall[0]
	} else {
		mycall = bs.modemConfig.mycall[bp.sendto_chan]
	}

	if IsNoCall(mycall) {
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
	if bp.source != "" {
		beacon_text = bp.source
	} else {
		beacon_text = mycall
	}

	beacon_text += ">"

	if bp.dest != "" {
		beacon_text += bp.dest
	} else {
		var stemp = fmt.Sprintf("%s%1d%1d", APP_TOCALL, MAJOR_VERSION, MINOR_VERSION)
		beacon_text += stemp
	}

	if bp.via != "" {
		beacon_text += "," + bp.via
	}

	beacon_text += ":"

	/*
	 * If the COMMENTCMD option was specified, run specified command to get variable part.
	 * Result is any fixed part followed by any variable part.
	 */

	// TODO: test & document.

	var super_comment = ""
	if bp.comment != "" {
		super_comment = bp.comment
	}

	if bp.commentcmd != "" {
		/* Run given command to get variable part of comment. */
		var var_comment, k = dw_run_cmd(ctx, bp.commentcmd, 2)
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
		var dlat, dlon, havePosition = beaconPosition(bp)
		if !havePosition {
			return
		}

		beacon_text += EncodePosition(bp.messaging, bp.compress,
			dlat, dlon, bp.ambiguity,
			beaconAltitudeFeet(bp.alt_m),
			bp.symtab, bp.symbol,
			beaconPHG(bp.power), beaconPHG(bp.height), beaconPHG(bp.gain), bp.dir,
			maybe.Nothing[int](), maybe.Nothing[int](), /* course, speed */
			bp.freq, bp.tone, bp.offset,
			super_comment)

	case BEACON_OBJECT:
		var dlat, dlon, havePosition = beaconPosition(bp)
		if !havePosition {
			return
		}

		beacon_text += encode_object(bp.objname, bp.compress, time.Now(), dlat, dlon, bp.ambiguity,
			bp.symtab, bp.symbol,
			beaconPHG(bp.power), beaconPHG(bp.height), beaconPHG(bp.gain), bp.dir,
			maybe.Nothing[int](), maybe.Nothing[int](), /* course, speed */
			bp.freq, bp.tone, bp.offset, super_comment)

	case BEACON_TRACKER:
		var dlat, dlon, havePosition = trackerPosition(gpsinfo)

		if havePosition {
			/* Transmit altitude only if user asked for it. */
			/* A positive altitude in the config file enables */
			/* transmission of altitude from GPS. */
			var my_alt_ft maybe.Maybe[int]
			if gpsinfo.fix >= DWFIX_3D && maybe.FromMaybe(0, bp.alt_m) > 0 {
				my_alt_ft = maybe.Fmap(func(meters float64) int {
					return int(math.Round(DW_METERS_TO_FEET(meters)))
				}, gpsinfo.altitude)
			}

			/* Round to nearest integer, retaining unknown state. */
			var coarse = maybe.Fmap(func(degrees float64) int { return int(math.Round(degrees)) }, gpsinfo.track)
			var knots = maybe.Fmap(func(speed float64) int { return int(math.Round(speed)) }, gpsinfo.speed_knots)

			beacon_text += EncodePosition(bp.messaging, bp.compress,
				dlat, dlon, bp.ambiguity, my_alt_ft,
				bp.symtab, bp.symbol,
				beaconPHG(bp.power), beaconPHG(bp.height), beaconPHG(bp.gain), bp.dir,
				coarse, knots,
				bp.freq, bp.tone, bp.offset,
				super_comment)

			/* Write to log file for testing. */
			/* The idea is to run log2gpx and map the result rather than */
			/* actually transmitting and relying on someone else to receive */
			/* the signals. */

			if bs.trackerDebugLevel >= 3 {
				/* Frequency, offset, tone and DCS are unknown here, which is */
				/* what the zero value of each of those fields already means. */
				var A decode_aprs_t

				A.g_src = mycall
				A.g_symbol_table = bp.symtab
				A.g_symbol_code = bp.symbol
				A.g_lat = gpsinfo.dlat
				A.g_lon = gpsinfo.dlon
				A.g_speed_mph = maybe.Fmap(DW_KNOTS_TO_MPH, gpsinfo.speed_knots)
				A.g_course = maybe.Fmap(func(degrees int) float64 { return float64(degrees) }, coarse)
				A.g_altitude_ft = maybe.Fmap(DW_METERS_TO_FEET, gpsinfo.altitude)

				/* Fake channel of 999 to distinguish from real data. */
				var alevel ALevel
				packetLogger.Write(999, &A, nil, alevel, 0)
			}
		} else {
			return /* No position.  Skip this time. */
		}

	case BEACON_CUSTOM:
		if bp.custom_info != "" {
			/* Fixed handcrafted text. */
			beacon_text += bp.custom_info
		} else if bp.custom_infocmd != "" {
			/* Run given command to obtain the info part for packet. */
			var info_part, k = dw_run_cmd(ctx, bp.custom_infocmd, 2)
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
			var last_minutes = 30

			var stuff = fmt.Sprintf("<IGATE,MSG_CNT=%d,PKT_CNT=%d,DIR_CNT=%d,LOC_CNT=%d,RF_CNT=%d,UPL_CNT=%d,DNL_CNT=%d",
				igate.msgCount(),
				igate.pktCount(),
				mheardDB.Count(0, last_minutes),
				mheardDB.Count(bs.igateConfig.max_digi_hops, last_minutes),
				mheardDB.Count(8, last_minutes),
				igate.uplinkCount(),
				igate.downlinkCount())

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

	var strict = true // Strict packet checking because they will go over air.
	var pp = AX25FromText(beacon_text, strict)

	if pp != nil {
		/* Send to desired destination. */
		switch bp.sendto_type {
		case SENDTO_IGATE:
			text_color_set(DW_COLOR_XMIT)
			dw_printf("[ig] %s\n", beacon_text)

			igate.sendRecPacket(-1, pp) // Channel -1 to avoid RF>IS filtering.
		case SENDTO_RECV:
			/* Simulated reception from radio. */
			var alevel ALevel
			dataLinkQueue.RecFrame(bp.sendto_chan, 0, 0, pp, alevel, fec_type_none, 0, "")
		default:
			transmitQueue.Append(bp.sendto_chan, TQ_PRIO_1_LO, pp)
		}
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Failed to parse packet constructed from line %d.\n", bp.lineno)
		dw_printf("%s\n", beacon_text)
	}
} /* end send */
