package direwolf

/********************************************************************************
 *
 * Purpose:	Raw Received Bit Buffer.
 *		An array of bits used to hold data out of
 *		the demodulator before feeding it into the HLDC decoding.
 *
 * Version 1.2: Save initial state of 9600 baud descrambler so we can
 *		attempt bit fix up on G3RUH/K9NG scrambled data.
 *
 * Version 1.3:	Store as bytes rather than packing 8 bits per byte.
 *
 *******************************************************************************/

// #include "direwolf.h"
// #include <stdio.h>
// #include <assert.h>
// #include <stdlib.h>
// #include <string.h>
// #include "textcolor.h"
// #include "ax25_pad.h"
// #include "rrbb.h"
import "C"

import (
	"unsafe"
)

var new_count = 0
var delete_count = 0

type rrbb_t C.rrbb_t

/***********************************************************************************
 *
 * Name:	rrbb_new
 *
 * Purpose:	Allocate space for an array of samples.
 *
 * Inputs:	channel	- Radio channel from whence it came.
 *
 *		subchannel	- Which demodulator of the channel.
 *
 *		slice	- multiple thresholds per demodulator.
 *
 *		is_scrambled - Is data scrambled? (true, false)
 *
 *		descram_state - State of data descrambler.
 *
 *		prev_descram - Previous descrambled bit.
 *
 * Returns:	Handle to be used by other functions.
 *
 * Description:
 *
 ***********************************************************************************/

//export rrbb_new
func rrbb_new(channel C.int, subchannel C.int, slice C.int, is_scrambled C.int, descram_state C.int, prev_descram C.int) rrbb_t {

	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)
	Assert(subchannel >= 0 && subchannel < MAX_SUBCHANS)
	Assert(slice >= 0 && slice < MAX_SLICERS)

	var result = rrbb_t(C.malloc(C.sizeof_struct_rrbb_s))
	if result == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("FATAL ERROR: Out of memory.\n")
		exit(1)
	}
	result.magic1 = MAGIC1
	result._chan = channel
	result.subchan = subchannel
	result.slice = slice
	result.magic2 = MAGIC2

	new_count++

	if new_count > delete_count+100 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("MEMORY LEAK, rrbb_new, new_count=%d, delete_count=%d\n", new_count, delete_count)
	}

	rrbb_clear(result, is_scrambled, descram_state, prev_descram)

	return (result)
}

/***********************************************************************************
 *
 * Name:	rrbb_clear
 *
 * Purpose:	Clear by setting length to zero, etc.
 *
 * Inputs:	b 		-Handle for sample array.
 *
 *		is_scrambled 	- Is data scrambled? (true, false)
 *
 *		descram_state 	- State of data descrambler.
 *
 *		prev_descram 	- Previous descrambled bit.
 *
 ***********************************************************************************/

//export rrbb_clear
func rrbb_clear(b rrbb_t, is_scrambled C.int, descram_state C.int, prev_descram C.int) {
	Assert(b != nil)
	Assert(b.magic1 == MAGIC1)
	Assert(b.magic2 == MAGIC2)

	Assert(is_scrambled == 0 || is_scrambled == 1)
	Assert(prev_descram == 0 || prev_descram == 1)

	b.nextp = nil

	b.alevel.rec = 9999 // TODO: was there some reason for this instead of 0 or -1?
	b.alevel.mark = 9999
	b.alevel.space = 9999

	b.len = 0

	b.is_scrambled = is_scrambled
	b.descram_state = descram_state
	b.prev_descram = prev_descram
}

/***********************************************************************************
 *
 * Name:	rrbb_append_bit
 *
 * Purpose:	Append another bit to the end.
 *
 * Inputs:	Handle for sample array.
 *		Value for the sample.
 *
 ***********************************************************************************/

/* Definition in header file so it can be inlined. */

/***********************************************************************************
 *
 * Name:	rrbb_chop8
 *
 * Purpose:	Remove 8 from the length.
 *
 * Inputs:	Handle for bit array.
 *
 * Description:	Back up after appending the flag sequence.
 *
 ***********************************************************************************/

//export rrbb_chop8
func rrbb_chop8(b rrbb_t) {

	Assert(b != nil)
	Assert(b.magic1 == MAGIC1)
	Assert(b.magic2 == MAGIC2)

	if b.len >= 8 {
		b.len -= 8
	}
}

/***********************************************************************************
 *
 * Name:	rrbb_get_len
 *
 * Purpose:	Get number of bits in the array.
 *
 * Inputs:	Handle for bit array.
 *
 ***********************************************************************************/

//export rrbb_get_len
func rrbb_get_len(b rrbb_t) C.int {
	Assert(b != nil)
	Assert(b.magic1 == MAGIC1)
	Assert(b.magic2 == MAGIC2)

	return C.int(b.len)
}

/***********************************************************************************
 *
 * Name:	rrbb_get_bit
 *
 * Purpose:	Get value of bit in specified position.
 *
 * Inputs:	Handle for sample array.
 *		Index into array.
 *
 ***********************************************************************************/

/* Definition in header file so it can be inlined. */

/***********************************************************************************
 *
 * Name:	rrbb_flip_bit
 *
 * Purpose:	Complement the value of bit in specified position.
 *
 * Inputs:	Handle for bit array.
 *		Index into array.
 *
 ***********************************************************************************/

//void rrbb_flip_bit (rrbb_t b, unsigned int ind)
//{
//	unsigned int di, mi;
//
//	Assert (b != nil);
//	Assert (b.magic1 == MAGIC1);
//	Assert (b.magic2 == MAGIC2);
//
//	Assert (ind < b.len);
//
//	di = ind / SOI;
//	mi = ind % SOI;
//
//	b.data[di] ^= masks[mi];
//}

/***********************************************************************************
 *
 * Name:	rrbb_delete
 *
 * Purpose:	Free the storage associated with the bit array.
 *
 * Inputs:	Handle for bit array.
 *
 ***********************************************************************************/

//export rrbb_delete
func rrbb_delete(b rrbb_t) {
	Assert(b != nil)
	Assert(b.magic1 == MAGIC1)
	Assert(b.magic2 == MAGIC2)

	b.magic1 = 0
	b.magic2 = 0

	C.free(unsafe.Pointer(b))

	delete_count++
}

/***********************************************************************************
 *
 * Name:	rrbb_set_netxp
 *
 * Purpose:	Set the nextp field, used to maintain a queue.
 *
 * Inputs:	b	Handle for bit array.
 *		np	New value for nextp.
 *
 ***********************************************************************************/

//export rrbb_set_nextp
func rrbb_set_nextp(b rrbb_t, np rrbb_t) {
	Assert(b != nil)
	Assert(b.magic1 == MAGIC1)
	Assert(b.magic2 == MAGIC2)

	b.nextp = np
}

/***********************************************************************************
 *
 * Name:	rrbb_get_netxp
 *
 * Purpose:	Get value of nextp field.
 *
 * Inputs:	b	Handle for bit array.
 *
 ***********************************************************************************/

//export rrbb_get_nextp
func rrbb_get_nextp(b rrbb_t) rrbb_t {
	Assert(b != nil)
	Assert(b.magic1 == MAGIC1)
	Assert(b.magic2 == MAGIC2)

	return (b.nextp)
}

/***********************************************************************************
 *
 * Name:	rrbb_get_chan
 *
 * Purpose:	Get channel from which bit buffer was received.
 *
 * Inputs:	b	Handle for bit array.
 *
 ***********************************************************************************/

//export rrbb_get_chan
func rrbb_get_chan(b rrbb_t) C.int {
	Assert(b != nil)
	Assert(b.magic1 == MAGIC1)
	Assert(b.magic2 == MAGIC2)

	Assert(b._chan >= 0 && b._chan < MAX_RADIO_CHANS)

	return (b._chan)
}

/***********************************************************************************
 *
 * Name:	rrbb_get_subchan
 *
 * Purpose:	Get subchannel from which bit buffer was received.
 *
 * Inputs:	b	Handle for bit array.
 *
 ***********************************************************************************/

//export rrbb_get_subchan
func rrbb_get_subchan(b rrbb_t) C.int {
	Assert(b != nil)
	Assert(b.magic1 == MAGIC1)
	Assert(b.magic2 == MAGIC2)

	Assert(b.subchan >= 0 && b.subchan < MAX_SUBCHANS)

	return (b.subchan)
}

/***********************************************************************************
 *
 * Name:	rrbb_get_slice
 *
 * Purpose:	Get slice number from which bit buffer was received.
 *
 * Inputs:	b	Handle for bit array.
 *
 ***********************************************************************************/

//export rrbb_get_slice
func rrbb_get_slice(b rrbb_t) C.int {
	Assert(b != nil)
	Assert(b.magic1 == MAGIC1)
	Assert(b.magic2 == MAGIC2)

	Assert(b.slice >= 0 && b.slice < MAX_SLICERS)

	return (b.slice)
}

/***********************************************************************************
 *
 * Name:	rrbb_set_audio_level
 *
 * Purpose:	Set audio level at time the frame was received.
 *
 * Inputs:	b	Handle for bit array.
 *		alevel	Audio level.
 *
 ***********************************************************************************/

//export rrbb_set_audio_level
func rrbb_set_audio_level(b rrbb_t, alevel C.alevel_t) {
	Assert(b != nil)
	Assert(b.magic1 == MAGIC1)
	Assert(b.magic2 == MAGIC2)

	b.alevel = alevel
}

/***********************************************************************************
 *
 * Name:	rrbb_get_audio_level
 *
 * Purpose:	Get audio level at time the frame was received.
 *
 * Inputs:	b	Handle for bit array.
 *
 ***********************************************************************************/

//export rrbb_get_audio_level
func rrbb_get_audio_level(b rrbb_t) C.alevel_t {
	Assert(b != nil)
	Assert(b.magic1 == MAGIC1)
	Assert(b.magic2 == MAGIC2)

	return (b.alevel)
}

/***********************************************************************************
 *
 * Name:	rrbb_set_speed_error
 *
 * Purpose:	Set speed error of the received frame.
 *
 * Inputs:	b		Handle for bit array.
 *		speed_error	In percentage.
 *
 ***********************************************************************************/

//export rrbb_set_speed_error
func rrbb_set_speed_error(b rrbb_t, speed_error C.float) {
	Assert(b != nil)
	Assert(b.magic1 == MAGIC1)
	Assert(b.magic2 == MAGIC2)

	b.speed_error = speed_error
}

/***********************************************************************************
 *
 * Name:	rrbb_get_speed_error
 *
 * Purpose:	Get speed error of the received frame.
 *
 * Inputs:	b	Handle for bit array.
 *
 * Returns:	speed error in percentage.
 *
 ***********************************************************************************/

//export rrbb_get_speed_error
func rrbb_get_speed_error(b rrbb_t) C.float {
	Assert(b != nil)
	Assert(b.magic1 == MAGIC1)
	Assert(b.magic2 == MAGIC2)

	return (b.speed_error)
}

/***********************************************************************************
 *
 * Name:	rrbb_get_is_scrambled
 *
 * Purpose:	Find out if using scrambled data.
 *
 * Inputs:	b	Handle for bit array.
 *
 * Returns:	True (for 9600 baud) or false (for slower AFSK).
 *
 ***********************************************************************************/

//export rrbb_get_is_scrambled
func rrbb_get_is_scrambled(b rrbb_t) C.int {
	Assert(b != nil)
	Assert(b.magic1 == MAGIC1)
	Assert(b.magic2 == MAGIC2)

	return (b.is_scrambled)
}

/***********************************************************************************
 *
 * Name:	rrbb_get_descram_state
 *
 * Purpose:	Get data descrambler state before first data bit of frame.
 *
 * Inputs:	b	Handle for bit array.
 *
 ***********************************************************************************/

//export rrbb_get_descram_state
func rrbb_get_descram_state(b rrbb_t) C.int {
	Assert(b != nil)
	Assert(b.magic1 == MAGIC1)
	Assert(b.magic2 == MAGIC2)

	return (b.descram_state)
}

/***********************************************************************************
 *
 * Name:	rrbb_get_prev_descram
 *
 * Purpose:	Get previous descrambled bit before first data bit of frame.
 *
 * Inputs:	b	Handle for bit array.
 *
 ***********************************************************************************/

//export rrbb_get_prev_descram
func rrbb_get_prev_descram(b rrbb_t) C.int {
	Assert(b != nil)
	Assert(b.magic1 == MAGIC1)
	Assert(b.magic2 == MAGIC2)

	return (b.prev_descram)
}

/* end rrbb.c */
