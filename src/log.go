package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:	Save received packets to a log file.
 *
 * Description: Rather than saving the raw, sometimes rather cryptic and
 *		unreadable, format, write separated properties into
 *		CSV format for easy reading and later processing.
 *
 *		There are two alternatives here.
 *
 *		-L logfile		Specify full file path.
 *
 *		-l logdir		Daily names will be created here.
 *
 *		Use one or the other but not both.
 *
 *------------------------------------------------------------------*/

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

type PacketLogger struct {
	mu         sync.Mutex // Guards the fields below, since Write/Close may be called from multiple goroutines (e.g. beacon and main receive loop).
	dailyNames bool       // True if daily names should be generated. In this case path is a directory.
	logPath    string     // Save directory or full name here for later use.
	logFp      *os.File   // File pointer for writing. Note that file is kept open. We don't open/close for every new item.
	openFname  string     // Name of currently open file. Applicable only when dailyNames is true.
}

/*-------------------------------------------------------------------
 *
 * Name:	NewPacketLogger
 *
 * Purpose:	Initialise and return a new PacketLogger.
 *
 * Inputs:	daily_names	- True if daily names should be generated.
 *				  In this case path is a directory.
 *				  When false, path would be the file name.
 *
 *		path		- Log file name or just directory.
 *				  Use "." for current directory.
 *				  Empty string disables feature.
 *
 *---------------------------------------------------------------*/

func NewPacketLogger(daily_names bool, path string) *PacketLogger {
	var pl = &PacketLogger{ //nolint:exhaustruct
		dailyNames: daily_names,
	}

	if len(path) == 0 {
		return pl
	}

	if pl.dailyNames {
		// Original strategy.  Automatic daily file names.
		var stat, statErr = os.Stat(path)
		if statErr == nil {
			// Exists, but is it a directory?
			if stat.IsDir() {
				// Specified directory exists.
				pl.logPath = path
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Log file location \"%s\" is not a directory.\n", path)
				dw_printf("Using current working directory \".\" instead.\n")

				pl.logPath = "."
			}
		} else {
			// Doesn't exist.  Try to create it.
			// parent directory must exist.
			// We don't create multiple levels like "mkdir -p"
			var mkdirErr = os.Mkdir(path, 0750)
			if mkdirErr == nil {
				// Success.
				text_color_set(DW_COLOR_INFO)
				dw_printf("Log file location \"%s\" has been created.\n", path)
				pl.logPath = path
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Failed to create log file location \"%s\".\n", path)
				dw_printf("%s\n", mkdirErr)
				dw_printf("Using current working directory \".\" instead.\n")

				pl.logPath = "."
			}
		}
	} else {
		// Added in version 1.5.  Single file.
		// Typically logrotate would be used to keep size under control.
		text_color_set(DW_COLOR_INFO)
		dw_printf("Log file is \"%s\"\n", path)
		pl.logPath = path
	}

	return pl
} /* end NewPacketLogger */

/*-------------------------------------------------------------------
 *
 * Name:        Write
 *
 * Purpose:     Save information to log file.
 *
 * Inputs:	chan	- Radio channel where heard.
 *
 *		A	- Explode information from APRS packet.
 *
 *		pp	- Received packet object.
 *
 * 		alevel	- audio level.
 *
 *		retries	- Amount of effort to get a good CRC.
 *
 *--------------------------------------------------------------------*/

func (pl *PacketLogger) Write(channel int, A *decode_aprs_t, pp *packet_t, alevel alevel_t, retries BitFixLevel) {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	if len(pl.logPath) == 0 {
		return
	}

	var now = time.Now().UTC()

	if pl.dailyNames {
		// Original strategy.  Automatic daily file names.

		// Generate the file name from current date, UTC.
		// Why UTC rather than local time?  I don't recall the reasoning.
		// It's been there a few years and no on complained so leave it alone for now.

		// Microsoft doesn't recognize %F as equivalent to %Y-%m-%d
		var fname = now.Format("2006-01-02.log")

		// Close current file if name has changed

		if pl.logFp != nil && fname != pl.openFname {
			pl.closeLocked()
		}

		// Open for append if not already open.

		if pl.logFp == nil {
			var full_path = filepath.Join(pl.logPath, fname)

			// See if file already exists and not empty.
			// This is used later to write a header if it did not exist already.

			var _, statErr = os.Stat(full_path)
			var already_there = statErr == nil

			text_color_set(DW_COLOR_INFO)
			dw_printf("Opening log file \"%s\".\n", fname)

			var f, openErr = os.OpenFile(full_path, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0644) //nolint:gosec // Happy to trust config-provided log file
			if openErr == nil {
				pl.logFp = f
				pl.openFname = fname
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Can't open log file \"%s\" for write.\n", full_path)
				dw_printf("%s\n", openErr)

				pl.openFname = ""

				return
			}

			// Write a header suitable for importing into a spreadsheet
			// only if this will be the first line.

			if !already_there {
				fmt.Fprintf(pl.logFp, "chan,utime,isotime,source,heard,level,error,dti,name,symbol,latitude,longitude,speed,course,altitude,frequency,offset,tone,system,status,telemetry,comment\n")
			}
		}
	} else {
		// Added in version 1.5.  Single file.

		// Open for append if not already open.
		if pl.logFp == nil {
			// See if file already exists and not empty.
			// This is used later to write a header if it did not exist already.
			var _, statErr = os.Stat(pl.logPath)
			var already_there = statErr == nil

			text_color_set(DW_COLOR_INFO)
			dw_printf("Opening log file \"%s\"\n", pl.logPath)

			var f, openErr = os.OpenFile(pl.logPath, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0644) //nolint:gosec // Happy to trust config-provided log file
			if openErr == nil {
				pl.logFp = f
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Can't open log file \"%s\" for write.\n", pl.logPath)
				dw_printf("%s\n", openErr)

				pl.logPath = ""

				return
			}

			// Write a header suitable for importing into a spreadsheet
			// only if this will be the first line.

			if !already_there {
				fmt.Fprintf(pl.logFp, "chan,utime,isotime,source,heard,level,error,dti,name,symbol,latitude,longitude,speed,course,altitude,frequency,offset,tone,system,status,telemetry,comment\n")
			}
		}
	}

	// Add line to file if it is now open.

	if pl.logFp != nil {
		var itime = now.Format("2006-01-02T15:04:05Z")

		/* Who are we hearing?   Original station or digipeater? */
		/* Similar code in direwolf.c.  Combine into one function? */

		var heard = ""
		var h int

		if pp != nil {
			if ax25_get_num_addr(pp) == 0 {
				/* Not AX.25. No station to display below. */
				h = -1
			} else {
				h = ax25_get_heard(pp)
				heard = ax25_get_addr_with_ssid(pp, h)
			}

			if h >= AX25_REPEATER_2 &&
				len(heard) == 5 &&
				heard[:4] == "WIDE" &&
				unicode.IsDigit(rune(heard[4])) {
				heard = ax25_get_addr_with_ssid(pp, h-1) + "?"
			}
		}

		var alevel_text = ax25_alevel_to_text(alevel)

		var sdti string
		if pp != nil {
			sdti = string(rune(ax25_get_dti(pp)))
		}

		var sname = A.g_src
		if len(A.g_name) > 0 {
			sname = A.g_name
		}

		var ssymbol = string(rune(A.g_symbol_table)) + string(rune(A.g_symbol_code))

		var smfr = A.g_mfr
		var sstatus = A.g_mic_e_status
		var stelemetry = A.g_telemetry
		var scomment = A.g_comment

		var slat = ""
		if A.g_lat != G_UNKNOWN {
			slat = fmt.Sprintf("%.6f", A.g_lat)
		}

		var slon = ""
		if A.g_lon != G_UNKNOWN {
			slon = fmt.Sprintf("%.6f", A.g_lon)
		}

		var sspd = ""
		if A.g_speed_mph != G_UNKNOWN {
			sspd = fmt.Sprintf("%.1f", DW_MPH_TO_KNOTS(float64(A.g_speed_mph)))
		}

		var scse = ""
		if A.g_course != G_UNKNOWN {
			scse = fmt.Sprintf("%.1f", A.g_course)
		}

		var salt = ""
		if A.g_altitude_ft != G_UNKNOWN {
			salt = fmt.Sprintf("%.1f", DW_FEET_TO_METERS(float64(A.g_altitude_ft)))
		}

		var sfreq = ""
		if A.g_freq != G_UNKNOWN {
			sfreq = fmt.Sprintf("%.3f", A.g_freq)
		}

		var soffs = ""
		if A.g_offset != G_UNKNOWN {
			soffs = fmt.Sprintf("%+d", A.g_offset)
		}

		var stone = ""
		if A.g_tone != G_UNKNOWN {
			stone = fmt.Sprintf("%.1f", A.g_tone)
		}

		if A.g_dcs != G_UNKNOWN {
			stone = fmt.Sprintf("D%03o", A.g_dcs)
		}

		var w = csv.NewWriter(pl.logFp)
		w.Write([]string{
			strconv.Itoa(channel), strconv.Itoa(int(now.Unix())), itime,
			A.g_src, heard, alevel_text, strconv.Itoa(int(retries)), sdti,
			sname, ssymbol,
			slat, slon, sspd, scse, salt,
			sfreq, soffs, stone,
			smfr, sstatus, stelemetry, scomment,
		})
		w.Flush()

		var writeError = w.Error()
		if writeError != nil {
			dw_printf("CSV write error: %s", writeError)
		}
	}
} /* end Write */

/*-------------------------------------------------------------------
 *
 * Name:        RRBits
 *
 * Purpose:     Quick hack to look at the C and RR bits just to see what is there.
 *		This seems like a good place because it is a small subset of the function above.
 *
 * Inputs:	A	- Explode information from APRS packet.
 *
 *		pp	- Received packet object.
 *
 *--------------------------------------------------------------------*/

func (pl *PacketLogger) RRBits(A *decode_aprs_t, pp *packet_t) {
	// Sanitize system type (manufacturer) changing any comma to period.
	var smfr = strings.ReplaceAll(A.g_mfr, ",", ".")

	/* Who are we hearing?   Original station or digipeater? */
	/* Similar code in direwolf.c.  Combine into one function? */

	var heard = ""

	if pp != nil {
		var h int
		if ax25_get_num_addr(pp) == 0 {
			/* Not AX.25. No station to display below. */
			h = -1
		} else {
			h = ax25_get_heard(pp)
			heard = ax25_get_addr_with_ssid(pp, h)
		}

		if h >= AX25_REPEATER_2 &&
			len(heard) == 5 &&
			heard[:4] == "WIDE" &&
			unicode.IsDigit(rune(heard[4])) {
			heard = ax25_get_addr_with_ssid(pp, h-1) + "?"
		}

		var src_c = ax25_get_h(pp, AX25_SOURCE)
		var dst_c = ax25_get_h(pp, AX25_DESTINATION)
		var src_rr = ax25_get_rr(pp, AX25_SOURCE)
		var dst_rr = ax25_get_rr(pp, AX25_DESTINATION)

		// C RR	for source
		// C RR	for destination
		// system type
		// source
		// station heard

		text_color_set(DW_COLOR_INFO)

		dw_printf("%d %d%d  %d %d%d,%s,%s,%s\n",
			src_c, (src_rr>>1)&1, src_rr&1,
			dst_c, (dst_rr>>1)&1, dst_rr&1,
			smfr, A.g_src, heard)
	}
} /* end RRBits */

/*-------------------------------------------------------------------
 *
 * Name:        Close
 *
 * Purpose:	Close any open log file.
 *		Called when exiting or when date changes.
 *
 *------------------------------------------------------------------*/

func (pl *PacketLogger) Close() {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	pl.closeLocked()
} /* end Close */

// closeLocked does the work of Close, assuming pl.mu is already held.
func (pl *PacketLogger) closeLocked() {
	if pl.logFp != nil {
		text_color_set(DW_COLOR_INFO)

		if pl.dailyNames {
			dw_printf("Closing log file \"%s\".\n", pl.openFname)
		} else {
			dw_printf("Closing log file \"%s\".\n", pl.logPath)
		}

		pl.logFp.Close()

		pl.logFp = nil
		pl.openFname = ""
	}
}
