package direwolf

// #include "direwolf.h"
// #include <stdlib.h>
// #include <stdio.h>
// #include <assert.h>
// #include <string.h>
// #include "audio.h"
// #include "gen_tone.h"
import "C"

import (
	"unsafe"
)

var number_of_il2p_bits_sent [MAX_RADIO_CHANS]C.int // Count number of bits sent by "il2p_send_frame"

/*-------------------------------------------------------------
 *
 * Name:	il2p_send_frame
 *
 * Purpose:	Convert frames to a stream of bits in IL2P format.
 *
 * Inputs:	chan	- Audio channel number, 0 = first.
 *
 *		pp	- Pointer to packet object.
 *
 *		max_fec	- 1 to force 16 parity symbols for each payload block.
 *			  0 for automatic depending on block size.
 *
 *		polarity - 0 for normal.  1 to invert signal.
 *			   2 special case for testing - introduce some errors to test FEC.
 *
 * Outputs:	Bits are shipped out by calling tone_gen_put_bit().
 *
 * Returns:	Number of bits sent including
 *		- Preamble   (01010101...)
 *		- 3 byte Sync Word.
 *		- 15 bytes for Header.
 *		- Optional payload.
 *		The required time can be calculated by dividing this
 *		number by the transmit rate of bits/sec.
 *		-1 is returned for failure.
 *
 * Description:	Generate an IL2P encoded frame.
 *
 * Assumptions:	It is assumed that the tone_gen module has been
 *		properly initialized so that bits sent with
 *		tone_gen_put_bit() are processed correctly.
 *
 * Errors:	Return -1 for error.  Probably frame too large.
 *
 * Note:	Inconsistency here. ax25 version has just a byte array
 *		and length going in.  Here we need the full packet object.
 *
 *--------------------------------------------------------------*/

func il2p_send_frame(channel C.int, pp C.packet_t, max_fec C.int, polarity C.int) C.int {
	var encoded [IL2P_MAX_PACKET_SIZE]C.uchar

	encoded[0] = (IL2P_SYNC_WORD >> 16) & 0xff
	encoded[1] = (IL2P_SYNC_WORD >> 8) & 0xff
	encoded[2] = (IL2P_SYNC_WORD) & 0xff

	var elen = il2p_encode_frame(pp, max_fec, &encoded[IL2P_SYNC_WORD_SIZE])
	if elen <= 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("IL2P: Unable to encode frame into IL2P.\n")
		return (-1)
	}

	elen += IL2P_SYNC_WORD_SIZE

	number_of_il2p_bits_sent[channel] = 0

	if il2p_get_debug() >= 1 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("IL2P frame, max_fec = %d, %d encoded bytes total\n", max_fec, elen)
		fx_hex_dump(&encoded[0], elen)
	}

	// Clobber some bytes for testing.
	if polarity >= 2 {
		for j := C.int(10); j < elen; j += 100 {
			encoded[j] = ^encoded[j]
		}
	}

	// Send bits to modulator.

	var preamble C.uchar = IL2P_PREAMBLE

	send_il2p_bytes(channel, &preamble, 1, polarity)
	send_il2p_bytes(channel, &encoded[0], elen, polarity)

	return (number_of_il2p_bits_sent[channel])
}

func send_il2p_bytes(channel C.int, _b *C.uchar, count C.int, polarity C.int) {
	var b = C.GoBytes(unsafe.Pointer(_b), count)
	for j := C.int(0); j < count; j++ {
		var x = C.int(b[j])
		for k := 0; k < 8; k++ {
			var bit C.int = 0
			if (x & 0x80) != 0 {
				bit = 1
			}
			send_il2p_bit(channel, bit, polarity)
			x <<= 1
		}
	}
}

// NRZI would be applied for AX.25 but IL2P does not use it.
// However we do have an option to invert the signal.
// The direwolf receive implementation will automatically compensate
// for either polarity but other implementations might not.

func send_il2p_bit(channel C.int, b C.int, polarity C.int) {
	C.tone_gen_put_bit(channel, (b^polarity)&1)
	number_of_il2p_bits_sent[channel]++
}

// end il2p_send.c
