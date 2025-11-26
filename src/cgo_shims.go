package direwolf

// Assorted utilities when porting from C to Go

// #include "direwolf.h"
// #include "audio.h"
// #include "decode_aprs.h"
// #include "textcolor.h"
// #include "dwgps.h"
// #include "config.h"
// #include "tq.h"
// #include "kiss_frame.h"
// #include "ax25_link.h"
// #include "il2p.h"
import "C"

import (
	"fmt"
	"math"
	"os"
	"runtime"
)

const G_UNKNOWN = C.G_UNKNOWN

const DW_COLOR_ERROR = C.DW_COLOR_ERROR
const DW_COLOR_DEBUG = C.DW_COLOR_DEBUG
const DW_COLOR_INFO = C.DW_COLOR_INFO
const DW_COLOR_XMIT = C.DW_COLOR_XMIT
const DW_COLOR_REC = C.DW_COLOR_REC
const DW_COLOR_DECODED = C.DW_COLOR_DECODED

const MAX_RADIO_CHANS = C.MAX_RADIO_CHANS
const MAX_TOTAL_CHANS = C.MAX_TOTAL_CHANS
const MAX_SUBCHANS = C.MAX_SUBCHANS
const MAX_SLICERS = C.MAX_SLICERS

const MEDIUM_NONE = C.MEDIUM_NONE
const MEDIUM_RADIO = C.MEDIUM_RADIO
const MEDIUM_IGATE = C.MEDIUM_IGATE
const MEDIUM_NETTNC = C.MEDIUM_NETTNC

const DWFIX_NOT_INIT = C.DWFIX_NOT_INIT
const DWFIX_2D = C.DWFIX_2D
const DWFIX_3D = C.DWFIX_3D
const DWFIX_ERROR = C.DWFIX_ERROR
const DWFIX_NO_FIX = C.DWFIX_NO_FIX

const SENDTO_IGATE = C.SENDTO_IGATE
const SENDTO_RECV = C.SENDTO_RECV

const TQ_NUM_PRIO = C.TQ_NUM_PRIO
const TQ_PRIO_0_HI = C.TQ_PRIO_0_HI
const TQ_PRIO_1_LO = C.TQ_PRIO_1_LO

const AX25_DESTINATION = C.AX25_DESTINATION
const AX25_MAX_ADDR_LEN = C.AX25_MAX_ADDR_LEN
const AX25_MAX_ADDRS = C.AX25_MAX_ADDRS
const AX25_MAX_INFO_LEN = C.AX25_MAX_INFO_LEN
const AX25_MAX_PACKET_LEN = C.AX25_MAX_PACKET_LEN
const AX25_MAX_REPEATERS = C.AX25_MAX_REPEATERS
const AX25_N1_PACLEN_MAX = C.AX25_N1_PACLEN_MAX
const AX25_REPEATER_1 = C.AX25_REPEATER_1
const AX25_SOURCE = C.AX25_SOURCE

const WPL_FORMAT_NMEA_GENERIC = C.WPL_FORMAT_NMEA_GENERIC
const WPL_FORMAT_GARMIN = C.WPL_FORMAT_GARMIN
const WPL_FORMAT_MAGELLAN = C.WPL_FORMAT_MAGELLAN
const WPL_FORMAT_KENWOOD = C.WPL_FORMAT_KENWOOD
const WPL_FORMAT_AIS = C.WPL_FORMAT_AIS

const KS_SEARCHING = C.KS_SEARCHING
const KS_COLLECTING = C.KS_COLLECTING

const MAX_NOISE_LEN = C.MAX_NOISE_LEN

const frame_type_I C.ax25_frame_type_t = C.frame_type_I
const frame_type_S_RR C.ax25_frame_type_t = C.frame_type_S_RR
const frame_type_S_RNR C.ax25_frame_type_t = C.frame_type_S_RNR
const frame_type_S_REJ C.ax25_frame_type_t = C.frame_type_S_REJ
const frame_type_S_SREJ C.ax25_frame_type_t = C.frame_type_S_SREJ
const frame_type_U_SABME C.ax25_frame_type_t = C.frame_type_U_SABME
const frame_type_U_SABM C.ax25_frame_type_t = C.frame_type_U_SABM
const frame_type_U_DISC C.ax25_frame_type_t = C.frame_type_U_DISC
const frame_type_U_DM C.ax25_frame_type_t = C.frame_type_U_DM
const frame_type_U_UA C.ax25_frame_type_t = C.frame_type_U_UA
const frame_type_U_FRMR C.ax25_frame_type_t = C.frame_type_U_FRMR
const frame_type_U_UI C.ax25_frame_type_t = C.frame_type_U_UI
const frame_type_U_XID C.ax25_frame_type_t = C.frame_type_U_XID
const frame_type_U_TEST C.ax25_frame_type_t = C.frame_type_U_TEST
const frame_type_U C.ax25_frame_type_t = C.frame_type_U
const frame_not_AX25 C.ax25_frame_type_t = C.frame_not_AX25

const cr_00 C.cmdres_t = C.cr_00
const cr_cmd C.cmdres_t = C.cr_cmd
const cr_res C.cmdres_t = C.cr_res
const cr_11 C.cmdres_t = C.cr_11

const OCTYPE_PTT = C.OCTYPE_PTT
const OCTYPE_DCD = C.OCTYPE_DCD
const OCTYPE_CON = C.OCTYPE_CON

const MODEM_OFF = C.MODEM_OFF
const MODEM_AFSK = C.MODEM_AFSK
const MODEM_BASEBAND = C.MODEM_BASEBAND
const MODEM_EAS = C.MODEM_EAS
const MODEM_SCRAMBLE = C.MODEM_SCRAMBLE
const MODEM_QPSK = C.MODEM_QPSK
const MODEM_8PSK = C.MODEM_8PSK
const MODEM_AIS = C.MODEM_AIS

const RETRY_NONE = C.RETRY_NONE

const FEND = C.FEND
const FESC = C.FESC
const TFEND = C.TFEND
const TFESC = C.TFESC

const IL2P_HEADER_SIZE = C.IL2P_HEADER_SIZE
const IL2P_HEADER_PARITY = C.IL2P_HEADER_PARITY
const IL2P_MAX_PAYLOAD_SIZE = C.IL2P_MAX_PAYLOAD_SIZE

func dw_printf(format string, a ...any) (int, error) {
	// Can't call variadic functions through cgo, so let's define our own!
	// Fortunately dw_printf doesn't do much
	return fmt.Printf(format, a...)
}

func text_color_set(c C.enum_dw_color_e) {
	C.text_color_set(C.dw_color_t(c))
}

func exit(x int) {
	os.Exit(x)
}

// #define ACHAN2ADEV(n) ((n)>>1)
func ACHAN2ADEV(n C.int) C.int {
	return n >> 1
}

func ADEVFIRSTCHAN(n int) int {
	return n * 2
}

// #define DW_KNOTS_TO_MPH(x) ((x) == G_UNKNOWN ? G_UNKNOWN : (x) * 1.15077945)
func DW_KNOTS_TO_MPH(x float64) float64 {
	if x == G_UNKNOWN {
		return G_UNKNOWN
	}

	return x * 1.15077945
}

// #define DW_MPH_TO_KNOTS(x) ((x) == G_UNKNOWN ? G_UNKNOWN : (x) * 0.868976)
func DW_MPH_TO_KNOTS(x float64) float64 {
	if x == G_UNKNOWN {
		return G_UNKNOWN
	}

	return x * 0.868976
}

// #define DW_METERS_TO_FEET(x) ((x) == G_UNKNOWN ? G_UNKNOWN : (x) * 3.2808399)
func DW_METERS_TO_FEET(x float64) float64 {
	if x == G_UNKNOWN {
		return G_UNKNOWN
	}

	return x * 3.2808399
}

// #define DW_FEET_TO_METERS(x) ((x) == G_UNKNOWN ? G_UNKNOWN : (x) * 0.3048)
func DW_FEET_TO_METERS(x float64) float64 {
	if x == G_UNKNOWN {
		return G_UNKNOWN
	}

	return x * 0.3048
}

// #define DW_MILES_TO_KM(x) ((x) == G_UNKNOWN ? G_UNKNOWN : (x) * 1.609344)
func DW_MILES_TO_KM(x float64) float64 {
	if x == G_UNKNOWN {
		return G_UNKNOWN
	}

	return x * 1.609344
}

// #define DW_MBAR_TO_INHG(x) ((x) == G_UNKNOWN ? G_UNKNOWN : (x) * 0.0295333727)
func DW_MBAR_TO_INHG(x float64) float64 {
	if x == G_UNKNOWN {
		return G_UNKNOWN
	}

	return x * 0.0295333727
}

// #define DW_KM_TO_MILES(x) ((x) == G_UNKNOWN ? G_UNKNOWN : (x) * 0.621371192)
func DW_KM_TO_MILES(x float64) float64 {
	if x == G_UNKNOWN {
		return G_UNKNOWN
	}

	return x * 0.621371192
}

var retry_text = []string{
	"NONE",
	"SINGLE",
	"DOUBLE",
	"TRIPLE",
	"TWO_SEP",
	"PASSALL",
}

func D2R(d float64) float64 {
	return d * math.Pi / 180
}

func R2D(r float64) float64 {
	return r * 180 / math.Pi
}

// Can't be "assert" because of conflicts with stretchr/testify/assert, but otherwise, it's compatible enough
func Assert(t bool) {
	if !t {
		_, file, line, _ := runtime.Caller(1)
		panic(fmt.Sprintf("Assertion failed at %s:%d", file, line))
	}
}

type fromto_t = C.fromto_t

const FROM_CLIENT fromto_t = C.FROM_CLIENT
const TO_CLIENT fromto_t = C.TO_CLIENT

var FROMTO_PREFIX = map[fromto_t]string{
	FROM_CLIENT: "<<<",
	TO_CLIENT:   ">>>",
}

func bool2Cint(t bool) C.int {
	if t {
		return 1
	} else {
		return 0
	}
}
