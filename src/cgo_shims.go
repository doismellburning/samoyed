package direwolf

// Assorted utilities when porting from C to Go

// #include "direwolf.h"
// #include "audio.h"
// #include "decode_aprs.h"
// #include "textcolor.h"
// #include "dwgps.h"
// #include "config.h"
// #include "tq.h"
import "C"

import (
	"fmt"
	"os"
)

const G_UNKNOWN = C.G_UNKNOWN

const DW_COLOR_ERROR = C.DW_COLOR_ERROR
const DW_COLOR_DEBUG = C.DW_COLOR_DEBUG
const DW_COLOR_INFO = C.DW_COLOR_INFO
const DW_COLOR_XMIT = C.DW_COLOR_XMIT
const DW_COLOR_REC = C.DW_COLOR_REC
const DW_COLOR_DECODED = C.DW_COLOR_DECODED

const MAX_TOTAL_CHANS = C.MAX_TOTAL_CHANS

const MEDIUM_NONE = C.MEDIUM_NONE
const MEDIUM_RADIO = C.MEDIUM_RADIO
const MEDIUM_IGATE = C.MEDIUM_IGATE
const MEDIUM_NETTNC = C.MEDIUM_NETTNC

const DWFIX_NOT_INIT = C.DWFIX_NOT_INIT
const DWFIX_2D = C.DWFIX_2D
const DWFIX_3D = C.DWFIX_3D

const SENDTO_IGATE = C.SENDTO_IGATE
const SENDTO_RECV = C.SENDTO_RECV

const TQ_PRIO_1_LO = C.TQ_PRIO_1_LO

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
