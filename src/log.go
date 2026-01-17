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

// #include "direwolf.h"
// #include <stdio.h>
// #include <time.h>
// #include <assert.h>
// #include <stdlib.h>
// #include <string.h>
// #include <ctype.h>
// #include <sys/types.h>
// #include <sys/stat.h>
// #include <unistd.h>
// #include <errno.h>
import "C"

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

/*------------------------------------------------------------------
 *
 * Function:	log_init
 *
 * Purpose:	Initialization at start of application.
 *
 * Inputs:	daily_names	- True if daily names should be generated.
 *				  In this case path is a directory.
 *				  When false, path would be the file name.
 *
 *		path		- Log file name or just directory.
 *				  Use "." for current directory.
 *				  Empty string disables feature.
 *
 * Global Out:	g_daily_names	- True if daily names should be generated.
 *
 *		g_log_path 	- Save directory or full name here for later use.
 *
 *		g_log_fp	- File pointer for writing.
 *				  Note that file is kept open.
 *				  We don't open/close for every new item.
 *
 *		g_open_fname	- Name of currently open file.
 *				  Applicable only when g_daily_names is true.
 *
 *------------------------------------------------------------------*/

var g_daily_names bool
var g_log_path string
var g_log_fp *os.File
var g_open_fname string

func log_init(daily_names bool, path string) {
	g_daily_names = daily_names
	g_log_path = ""
	g_log_fp = nil
	g_open_fname = ""

	if len(path) == 0 {
		return
	}

	if g_daily_names {
		// Original strategy.  Automatic daily file names.
		var stat, statErr = os.Stat(path)

		if statErr == nil {
			// Exists, but is it a directory?
			if stat.IsDir() {
				// Specified directory exists.
				g_log_path = path
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Log file location \"%s\" is not a directory.\n", path)
				dw_printf("Using current working directory \".\" instead.\n")
				g_log_path = "."
			}
		} else {
			// Doesn't exist.  Try to create it.
			// parent directory must exist.
			// We don't create multiple levels like "mkdir -p"
			var mkdirErr = os.Mkdir(path, 0755)
			if mkdirErr == nil {
				// Success.
				text_color_set(DW_COLOR_INFO)
				dw_printf("Log file location \"%s\" has been created.\n", path)
				g_log_path = path
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Failed to create log file location \"%s\".\n", path)
				dw_printf("%s\n", mkdirErr)
				dw_printf("Using current working directory \".\" instead.\n")
				g_log_path = "."
			}
		}
	} else {
		// Added in version 1.5.  Single file.
		// Typically logrotate would be used to keep size under control.

		text_color_set(DW_COLOR_INFO)
		dw_printf("Log file is \"%s\"\n", path)
		g_log_path = path
	}
} /* end log_init */

/*------------------------------------------------------------------
 *
 * Function:	log_write
 *
 * Purpose:	Save information to log file.
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
 *------------------------------------------------------------------*/

func log_write(channel int, A *decode_aprs_t, pp *packet_t, alevel alevel_t, retries retry_t) { //nolint:gocritic
	if len(g_log_path) == 0 {
		return
	}

	var now = time.Now().UTC()

	if g_daily_names {
		// Original strategy.  Automatic daily file names.

		// Generate the file name from current date, UTC.
		// Why UTC rather than local time?  I don't recall the reasoning.
		// It's been there a few years and no on complained so leave it alone for now.

		// Microsoft doesn't recognize %F as equivalent to %Y-%m-%d

		var fname = now.Format("2006-01-02.log")

		// Close current file if name has changed

		if g_log_fp != nil && fname != g_open_fname {
			log_term()
		}

		// Open for append if not already open.

		if g_log_fp == nil {
			var full_path = filepath.Join(g_log_path, fname)

			// See if file already exists and not empty.
			// This is used later to write a header if it did not exist already.

			var _, statErr = os.Stat(full_path)
			var already_there = statErr == nil

			text_color_set(DW_COLOR_INFO)
			dw_printf("Opening log file \"%s\".\n", fname)

			var f, openErr = os.OpenFile(full_path, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0644)

			if openErr == nil {
				g_log_fp = f
				g_open_fname = fname
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Can't open log file \"%s\" for write.\n", full_path)
				dw_printf("%s\n", openErr)
				g_open_fname = ""
				return
			}

			// Write a header suitable for importing into a spreadsheet
			// only if this will be the first line.

			if !already_there {
				fmt.Fprintf(g_log_fp, "chan,utime,isotime,source,heard,level,error,dti,name,symbol,latitude,longitude,speed,course,altitude,frequency,offset,tone,system,status,telemetry,comment\n")
			}
		}
	} else { //nolint:gocritic
		// Added in version 1.5.  Single file.

		// Open for append if not already open.

		if g_log_fp == nil {
			// See if file already exists and not empty.
			// This is used later to write a header if it did not exist already.

			var _, statErr = os.Stat(g_log_path)
			var already_there = statErr == nil

			text_color_set(DW_COLOR_INFO)
			dw_printf("Opening log file \"%s\"\n", g_log_path)

			var f, openErr = os.OpenFile(g_log_path, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0644)

			if openErr == nil {
				g_log_fp = f
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Can't open log file \"%s\" for write.\n", g_log_path)
				dw_printf("%s\n", openErr)
				g_log_path = ""
				return
			}

			// Write a header suitable for importing into a spreadsheet
			// only if this will be the first line.

			if !already_there {
				fmt.Fprintf(g_log_fp, "chan,utime,isotime,source,heard,level,error,dti,name,symbol,latitude,longitude,speed,course,altitude,frequency,offset,tone,system,status,telemetry,comment\n")
			}
		}
	}

	// Add line to file if it is now open.

	if g_log_fp != nil {
		var itime = now.Format("2006-01-02T15:04:05Z")

		/* Who are we hearing?   Original station or digipeater? */
		/* Similar code in direwolf.c.  Combine into one function? */

		var heard = ""
		var h C.int
		if pp != nil {
			if ax25_get_num_addr(pp) == 0 {
				/* Not AX.25. No station to display below. */
				h = -1
			} else {
				h = ax25_get_heard(pp)
				var _heard [AX25_MAX_ADDR_LEN + 1]C.char
				ax25_get_addr_with_ssid(pp, h, &_heard[0])
				heard = C.GoString(&_heard[0])
			}

			if h >= AX25_REPEATER_2 &&
				heard[:4] == "WIDE" &&
				unicode.IsDigit(rune(heard[4])) &&
				len(heard) == 5 {
				var _heard [AX25_MAX_ADDR_LEN + 1]C.char
				ax25_get_addr_with_ssid(pp, h-1, &_heard[0])
				heard = C.GoString(&_heard[0])
				heard += "?"
			}
		}

		var _alevel_text [40]C.char
		ax25_alevel_to_text(alevel, &_alevel_text[0])
		var alevel_text = C.GoString(&_alevel_text[0])

		var sdti string
		if pp != nil {
			sdti = string(rune(ax25_get_dti(pp)))
		}

		var sname = C.GoString(&A.g_src[0])
		if C.strlen(&A.g_name[0]) > 0 {
			sname = C.GoString(&A.g_name[0])
		}

		var ssymbol string = string(rune(A.g_symbol_table)) + string(rune(A.g_symbol_code))

		var smfr = C.GoString(&A.g_mfr[0])
		var sstatus = C.GoString(&A.g_mic_e_status[0])
		var stelemetry = C.GoString(&A.g_telemetry[0])
		var scomment = C.GoString(&A.g_comment[0])

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

		var w = csv.NewWriter(g_log_fp)
		w.Write([]string{
			strconv.Itoa(channel), strconv.Itoa(int(now.Unix())), itime,
			C.GoString(&A.g_src[0]), heard, alevel_text, strconv.Itoa(int(retries)), sdti,
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
} /* end log_write */

/*------------------------------------------------------------------
 *
 * Function:	log_rr_bits
 *
 * Purpose:	Quick hack to look at the C and RR bits just to see what is there.
 *		This seems like a good place because it is a small subset of the function above.
 *
 * Inputs:	A	- Explode information from APRS packet.
 *
 *		pp	- Received packet object.
 *
 *------------------------------------------------------------------*/

func log_rr_bits(A *decode_aprs_t, pp *packet_t) { //nolint:gocritic
	if true {
		// Sanitize system type (manufacturer) changing any comma to period.

		var smfr = strings.ReplaceAll(C.GoString(&A.g_mfr[0]), ",", ".")

		/* Who are we hearing?   Original station or digipeater? */
		/* Similar code in direwolf.c.  Combine into one function? */

		var heard = ""

		if pp != nil {
			var h C.int
			if ax25_get_num_addr(pp) == 0 {
				/* Not AX.25. No station to display below. */
				h = -1
			} else {
				h = ax25_get_heard(pp)
				var _heard [AX25_MAX_ADDR_LEN + 1]C.char
				ax25_get_addr_with_ssid(pp, h, &_heard[0])
				heard = C.GoString(&_heard[0])
			}

			if h >= AX25_REPEATER_2 &&
				heard[:4] == "WIDE" &&
				unicode.IsDigit(rune(heard[4])) &&
				len(heard) == 5 {
				var _heard [AX25_MAX_ADDR_LEN + 1]C.char
				ax25_get_addr_with_ssid(pp, h-1, &_heard[0])
				heard = C.GoString(&_heard[0])
				heard += "?"
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
				smfr, C.GoString(&A.g_src[0]), heard)
		}
	}
} /* end log_rr_bits */

/*------------------------------------------------------------------
 *
 * Function:	log_term
 *
 * Purpose:	Close any open log file.
 *		Called when exiting or when date changes.
 *
 *------------------------------------------------------------------*/

func log_term() {
	if g_log_fp != nil {
		text_color_set(DW_COLOR_INFO)

		if g_daily_names {
			dw_printf("Closing log file \"%s\".\n", g_open_fname)
		} else {
			dw_printf("Closing log file \"%s\".\n", g_log_path)
		}

		g_log_fp.Close()

		g_log_fp = nil
		g_open_fname = ""
	}
} /* end log_term */
