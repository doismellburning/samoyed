package direwolf

// #include "fx25.h"
import "C"

const CTAG_MIN = 0x01
const CTAG_MAX = 0x0B

// Maximum sizes of "data" and "check" parts.

const FX25_MAX_DATA = 239   // i.e. RS(255,239)
const FX25_MAX_CHECK = 64   // e.g. RS(255, 191)
const FX25_BLOCK_SIZE = 255 // Block size always 255 for 8 bit symbols.

func modnn(rs *C.struct_rs, _x int) C.int {

	var x = C.uint(_x)

	for x >= rs.nn {
		x -= rs.nn
		x = (x >> rs.mm) + (x & rs.nn)
	}

	return C.int(x)
}
