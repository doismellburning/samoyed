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

/*
 * Maximum number of bits in AX.25 frame excluding the flags.
 * Adequate for extreme case of bit stuffing after every 5 bits
 * which could never happen.
 */

const MAX_NUM_BITS = (MAX_FRAME_LEN * 8 * 6 / 5)

type rrbb_t struct {
	nextp *rrbb_t /* Next pointer to maintain a queue. */

	channel    int /* Radio channel from which it was received. */
	subchannel int /* Which modem when more than one per channel. */
	slice      int /* Which slicer. */

	alevel      ALevel  /* Received audio level at time of frame capture. */
	speed_error float64 /* Received data speed error as percentage. */
	length      int     /* Current number of samples in array. */

	is_scrambled  bool /* Is data scrambled G3RUH / K9NG style? */
	descram_state int  /* Descrambler state before first data bit of frame. */
	prev_descram  int  /* Previous descrambled bit. */

	fdata [MAX_NUM_BITS]byte
}

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

func rrbb_new(channel int, subchannel int, slice int, is_scrambled bool, descram_state int, prev_descram int) *rrbb_t {
	var result = new(rrbb_t)

	result.channel = channel
	result.subchannel = subchannel
	result.slice = slice

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

func rrbb_clear(b *rrbb_t, is_scrambled bool, descram_state int, prev_descram int) {
	Assert(prev_descram == 0 || prev_descram == 1)

	b.nextp = nil

	b.alevel.rec = 9999 // TODO: was there some reason for this instead of 0 or -1?
	b.alevel.mark = 9999
	b.alevel.space = 9999

	b.length = 0

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

func rrbb_append_bit(b *rrbb_t, val byte) {
	if b.length >= MAX_NUM_BITS {
		return /* Silently discard if full. */
	}

	b.fdata[b.length] = val
	b.length++
}

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

func rrbb_chop8(b *rrbb_t) {
	if b.length >= 8 {
		b.length -= 8
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

func rrbb_get_len(b *rrbb_t) int {
	return b.length
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

func rrbb_get_bit(b *rrbb_t, ind int) byte {
	return b.fdata[ind]
}

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

//void rrbb_flip_bit (*rrbb_t b, unsigned int ind)
//{
//	unsigned int di, mi;
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
 * Name:	rrbb_set_netxp
 *
 * Purpose:	Set the nextp field, used to maintain a queue.
 *
 * Inputs:	b	Handle for bit array.
 *		np	New value for nextp.
 *
 ***********************************************************************************/

func rrbb_set_nextp(b *rrbb_t, np *rrbb_t) { //nolint:unused
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

func rrbb_get_nextp(b *rrbb_t) *rrbb_t { //nolint:unused
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

func rrbb_get_chan(b *rrbb_t) int {
	return (b.channel)
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

func rrbb_get_subchan(b *rrbb_t) int {
	return (b.subchannel)
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

func rrbb_get_slice(b *rrbb_t) int {
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

func rrbb_set_audio_level(b *rrbb_t, alevel ALevel) {
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

func rrbb_get_audio_level(b *rrbb_t) ALevel {
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

func rrbb_set_speed_error(b *rrbb_t, speed_error float64) {
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

func rrbb_get_speed_error(b *rrbb_t) float64 { //nolint:unused
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

func rrbb_get_is_scrambled(b *rrbb_t) bool {
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

func rrbb_get_descram_state(b *rrbb_t) int {
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

func rrbb_get_prev_descram(b *rrbb_t) int {
	return (b.prev_descram)
}

/* end rrbb.c */
