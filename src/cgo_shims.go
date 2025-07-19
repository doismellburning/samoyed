package direwolf

// Assorted utilities when porting from C to Go

// #include "decode_aprs.h"
import "C"

import "fmt"

const G_UNKNOWN = C.G_UNKNOWN

func dw_printf(format string, a ...any) (int, error) {
	// Can't call variadic functions through cgo, so let's define our own!
	// Fortunately dw_printf doesn't do much
	return fmt.Printf(format, a...)
}
