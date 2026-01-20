package direwolf

// #include <ctype.h>
import "C"

import (
	"unsafe"
)

func hex_dump(_p *C.uchar, length C.int) {
	var p = C.GoBytes(unsafe.Pointer(_p), length)
	var offset = 0

	for length > 0 {
		var n = int(min(length, 16))

		dw_printf("  %03x: ", offset)
		for i := 0; i < n; i++ {
			dw_printf(" %02x", p[i])
		}
		for i := n; i < 16; i++ {
			dw_printf("   ")
		}
		dw_printf("  ")
		for i := 0; i < n; i++ {
			if p[i] >= 0x20 && p[i] <= 0x7E {
				dw_printf("%c", p[i])
			} else {
				dw_printf(".")
			}
		}
		dw_printf("\n")
		p = p[n:]
		offset += n
		length -= C.int(n)
	}
}
