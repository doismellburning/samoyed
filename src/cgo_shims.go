package direwolf

// Assorted utilities when porting from C to Go

// #include "decode_aprs.h"
// #include "textcolor.h"
import "C"

import (
	"fmt"
	"os"
)

const G_UNKNOWN = C.G_UNKNOWN

const DW_COLOR_ERROR = C.DW_COLOR_ERROR

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

func ADEVFIRSTCHAN(n int) int {
	return n * 2
}
