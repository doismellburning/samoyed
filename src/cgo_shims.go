package direwolf

// Assorted utilities when porting from C to Go

// #include "direwolf.h"
// #include "audio.h"
// #include "fsk_demod_state.h"
import "C"

import (
	"fmt"
	"math"
	"os"
	"runtime"
)

const BP_WINDOW_TRUNCATED = C.BP_WINDOW_TRUNCATED
const BP_WINDOW_COSINE = C.BP_WINDOW_COSINE
const BP_WINDOW_HAMMING = C.BP_WINDOW_HAMMING
const BP_WINDOW_BLACKMAN = C.BP_WINDOW_BLACKMAN
const BP_WINDOW_FLATTOP = C.BP_WINDOW_FLATTOP

const MAX_RADIO_CHANS = C.MAX_RADIO_CHANS
const MAX_TOTAL_CHANS = C.MAX_TOTAL_CHANS
const MAX_SUBCHANS = C.MAX_SUBCHANS
const MAX_SLICERS = C.MAX_SLICERS
const MAX_ADEVS = C.MAX_ADEVS
const MAX_FILTER_SIZE = C.MAX_FILTER_SIZE

const MIN_SAMPLES_PER_SEC = C.MIN_SAMPLES_PER_SEC
const MAX_SAMPLES_PER_SEC = C.MAX_SAMPLES_PER_SEC

const MIN_BAUD = C.MIN_BAUD
const MAX_BAUD = C.MAX_BAUD

const DEFAULT_ADEVICE = C.DEFAULT_ADEVICE
const DEFAULT_NUM_CHANNELS = C.DEFAULT_NUM_CHANNELS
const DEFAULT_SAMPLES_PER_SEC = C.DEFAULT_SAMPLES_PER_SEC
const DEFAULT_BITS_PER_SAMPLE = C.DEFAULT_BITS_PER_SAMPLE
const DEFAULT_MARK_FREQ = C.DEFAULT_MARK_FREQ
const DEFAULT_SPACE_FREQ = C.DEFAULT_SPACE_FREQ
const DEFAULT_BAUD = C.DEFAULT_BAUD
const DEFAULT_FIX_BITS = C.DEFAULT_FIX_BITS

const V26_UNSPECIFIED = C.V26_UNSPECIFIED

const MEDIUM_NONE = C.MEDIUM_NONE
const MEDIUM_RADIO = C.MEDIUM_RADIO
const MEDIUM_IGATE = C.MEDIUM_IGATE
const MEDIUM_NETTNC = C.MEDIUM_NETTNC

const AX25_DESTINATION = C.AX25_DESTINATION
const AX25_MAX_ADDR_LEN = C.AX25_MAX_ADDR_LEN
const AX25_MAX_ADDRS = C.AX25_MAX_ADDRS
const AX25_MAX_INFO_LEN = C.AX25_MAX_INFO_LEN
const AX25_MAX_PACKET_LEN = C.AX25_MAX_PACKET_LEN
const AX25_MAX_REPEATERS = C.AX25_MAX_REPEATERS
const AX25_REPEATER_1 = C.AX25_REPEATER_1
const AX25_SOURCE = C.AX25_SOURCE

const frame_type_I ax25_frame_type_t = C.frame_type_I
const frame_type_S_RR ax25_frame_type_t = C.frame_type_S_RR
const frame_type_S_RNR ax25_frame_type_t = C.frame_type_S_RNR
const frame_type_S_REJ ax25_frame_type_t = C.frame_type_S_REJ
const frame_type_S_SREJ ax25_frame_type_t = C.frame_type_S_SREJ
const frame_type_U_SABME ax25_frame_type_t = C.frame_type_U_SABME
const frame_type_U_SABM ax25_frame_type_t = C.frame_type_U_SABM
const frame_type_U_DISC ax25_frame_type_t = C.frame_type_U_DISC
const frame_type_U_DM ax25_frame_type_t = C.frame_type_U_DM
const frame_type_U_UA ax25_frame_type_t = C.frame_type_U_UA
const frame_type_U_FRMR ax25_frame_type_t = C.frame_type_U_FRMR
const frame_type_U_UI ax25_frame_type_t = C.frame_type_U_UI
const frame_type_U_XID ax25_frame_type_t = C.frame_type_U_XID
const frame_type_U_TEST ax25_frame_type_t = C.frame_type_U_TEST
const frame_type_U ax25_frame_type_t = C.frame_type_U
const frame_not_AX25 ax25_frame_type_t = C.frame_not_AX25

const cr_00 C.cmdres_t = C.cr_00
const cr_cmd C.cmdres_t = C.cr_cmd
const cr_res C.cmdres_t = C.cr_res
const cr_11 C.cmdres_t = C.cr_11

const NUM_OCTYPES = C.NUM_OCTYPES
const OCTYPE_PTT = C.OCTYPE_PTT
const OCTYPE_DCD = C.OCTYPE_DCD
const OCTYPE_CON = C.OCTYPE_CON

const NUM_ICTYPES = C.NUM_ICTYPES

const PTT_METHOD_SERIAL = C.PTT_METHOD_SERIAL
const PTT_METHOD_NONE = C.PTT_METHOD_NONE
const PTT_METHOD_GPIO = C.PTT_METHOD_GPIO
const PTT_METHOD_GPIOD = C.PTT_METHOD_GPIOD
const PTT_METHOD_LPT = C.PTT_METHOD_LPT
const PTT_METHOD_HAMLIB = C.PTT_METHOD_HAMLIB

const MODEM_OFF = C.MODEM_OFF
const MODEM_AFSK = C.MODEM_AFSK
const MODEM_BASEBAND = C.MODEM_BASEBAND
const MODEM_EAS = C.MODEM_EAS
const MODEM_SCRAMBLE = C.MODEM_SCRAMBLE
const MODEM_QPSK = C.MODEM_QPSK
const MODEM_8PSK = C.MODEM_8PSK
const MODEM_AIS = C.MODEM_AIS

const RETRY_NONE = C.RETRY_NONE
const RETRY_MAX = C.RETRY_MAX

const SSID_H_MASK = C.SSID_H_MASK
const SSID_H_SHIFT = C.SSID_H_SHIFT
const SSID_RR_MASK = C.SSID_RR_MASK
const SSID_RR_SHIFT = C.SSID_RR_SHIFT
const SSID_SSID_MASK = C.SSID_SSID_MASK
const SSID_SSID_SHIFT = C.SSID_SSID_SHIFT
const SSID_LAST_MASK = C.SSID_LAST_MASK

func dw_printf(format string, a ...any) (int, error) {
	// Can't call variadic functions through cgo, so let's define our own!
	// Fortunately dw_printf doesn't do much
	return fmt.Printf(format, a...)
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

func bool2Cint(t bool) C.int {
	if t {
		return 1
	} else {
		return 0
	}
}
