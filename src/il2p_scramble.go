package direwolf

/*--------------------------------------------------------------------------------
 *
 * Purpose:	Scramble / descramble data as specified in the IL2P protocol specification.
 *
 *--------------------------------------------------------------------------------*/

// #include <stdlib.h>
// #include <stdio.h>
// #include <string.h>
// #include <assert.h>
import "C"

import (
	"unsafe"
)

// Scramble bits for il2p transmit.

// Note that there is a delay of 5 until the first bit comes out.
// So we need to need to ignore the first 5 out and stick in
// an extra 5 filler bits to flush at the end.

const INIT_TX_LFSR C.int = 0x00f

func scramble_bit(in C.int, state *C.int) C.int {
	var out = ((*state >> 4) ^ *state) & 1
	*state = ((((in ^ *state) & 1) << 9) | (*state ^ ((*state & 1) << 4))) >> 1
	return (out)
}

// Undo data scrambling for il2p receive.

const INIT_RX_LFSR C.int = 0x1f0

func descramble_bit(in C.int, state *C.int) C.int {
	var out = (in ^ *state) & 1
	*state = ((*state >> 1) | ((in & 1) << 8)) ^ ((in & 1) << 3)
	return (out)
}

/*--------------------------------------------------------------------------------
 *
 * Function:	il2p_scramble_block
 *
 * Purpose:	Scramble a block before adding RS parity.
 *
 * Inputs:	in		Array of bytes.
 *		len		Number of bytes both in and out.
 *
 * Outputs:	out		Array of bytes.
 *
 *--------------------------------------------------------------------------------*/

func il2p_scramble_block(in []byte) []byte {
	var tx_lfsr_state = INIT_TX_LFSR

	Assert(len(in) >= 1)

	var out = make([]byte, len(in))

	var skipping = true // Discard the first 5 out.
	var ob = 0          // Index to output byte.
	var om byte = 0x80  // Output bit mask;
	for ib := 0; ib < len(in); ib++ {
		for im := C.int(0x80); im != 0; im >>= 1 {
			var s = scramble_bit(IfThenElse(((C.int(in[ib])&im) != 0), C.int(1), C.int(0)), &tx_lfsr_state)
			if ib == 0 && im == 0x04 {
				skipping = false
			}
			if !skipping {
				if s != 0 {
					out[ob] |= (om)
				}
				om >>= 1
				if om == 0 {
					om = 0x80
					ob++
				}
			}
		}
	}
	// Flush it.

	// This is a relic from when I thought the state would need to
	// be passed along for the next block.
	// Preserve the LFSR state from before flushing.
	// This might be needed as the initial state for later payload blocks.
	var x = tx_lfsr_state
	for n := 0; n < 5; n++ {
		var s = scramble_bit(0, &x)
		if s != 0 {
			out[ob] |= (om)
		}
		om >>= 1
		if om == 0 {
			om = 0x80
			ob++
		}
	}

	return out
}

/*--------------------------------------------------------------------------------
 *
 * Function:	il2p_descramble_block
 *
 * Purpose:	Descramble a block after removing RS parity.
 *
 * Inputs:	in		Array of bytes.
 *		len		Number of bytes both in and out.
 *
 * Outputs:	out		Array of bytes.
 *
 *--------------------------------------------------------------------------------*/

func il2p_descramble_block(_in *C.uchar, _out *C.uchar, length C.int) {
	var rx_lfsr_state = INIT_RX_LFSR

	var in = unsafe.Slice(_in, length)
	var out = make([]C.uchar, length)

	for b := C.int(0); b < length; b++ {
		for m := C.int(0x80); m != 0; m >>= 1 {
			var d = descramble_bit(IfThenElse(((C.int(in[b])&m) != 0), C.int(1), C.int(0)), &rx_lfsr_state)
			if d != 0 {
				out[b] |= C.uchar(m)
			}
		}
	}

	C.memcpy(unsafe.Pointer(_out), unsafe.Pointer(&out[0]), C.size_t(length))
}
