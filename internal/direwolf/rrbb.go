package direwolf

import (
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwutil"
)

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
	channel    int /* Radio channel from which it was received. */
	subchannel int /* Which modem when more than one per channel. */
	slice      int /* Which slicer. */

	alevel      ax25.ALevel /* Received audio level at time of frame capture. */
	speed_error float64     /* Received data speed error as percentage. */
	length      int         /* Current number of samples in array. */

	is_scrambled  bool /* Is data scrambled G3RUH / K9NG style? */
	descram_state int  /* Descrambler state before first data bit of frame. */
	prev_descram  int  /* Previous descrambled bit. */

	fdata [MAX_NUM_BITS]byte
}

// rrbb_new allocates a bit buffer for frames heard on the given radio
// channel, demodulator (subchannel) and slicer, and clears it as Clear does.
func rrbb_new(channel int, subchannel int, slice int, is_scrambled bool, descram_state int, prev_descram int) *rrbb_t {
	var result = new(rrbb_t)

	result.channel = channel
	result.subchannel = subchannel
	result.slice = slice

	result.Clear(is_scrambled, descram_state, prev_descram)

	return (result)
}

// Clear empties the buffer and records the state of the data descrambler, and
// the previous descrambled bit (0 or 1), before the first data bit of the frame.
// is_scrambled says whether the data is scrambled G3RUH / K9NG style.
func (b *rrbb_t) Clear(is_scrambled bool, descram_state int, prev_descram int) {
	dwutil.Assert(prev_descram == 0 || prev_descram == 1)

	b.alevel.Rec = 9999 // TODO: was there some reason for this instead of 0 or -1?
	b.alevel.Mark = 9999
	b.alevel.Space = 9999

	b.length = 0

	b.is_scrambled = is_scrambled
	b.descram_state = descram_state
	b.prev_descram = prev_descram
}

// AppendBit appends another bit to the end, silently discarding it if the
// buffer is full.
func (b *rrbb_t) AppendBit(val byte) {
	if b.length >= MAX_NUM_BITS {
		return /* Silently discard if full. */
	}

	b.fdata[b.length] = val
	b.length++
}

// Chop8 removes 8 bits from the end, to back up after appending the flag
// sequence.
func (b *rrbb_t) Chop8() {
	if b.length >= 8 {
		b.length -= 8
	}
}

// Len returns the number of bits in the buffer.
func (b *rrbb_t) Len() int {
	return b.length
}

// Bit returns the value of the bit at index ind.
func (b *rrbb_t) Bit(ind int) byte {
	return b.fdata[ind]
}

// Channel returns the radio channel the bits were received on.
func (b *rrbb_t) Channel() int {
	return (b.channel)
}

// Subchannel returns the demodulator the bits were received by.
func (b *rrbb_t) Subchannel() int {
	return (b.subchannel)
}

// Slice returns the slicer the bits were received by.
func (b *rrbb_t) Slice() int {
	return (b.slice)
}

// SetAudioLevel sets the audio level at the time the frame was received.
func (b *rrbb_t) SetAudioLevel(alevel ax25.ALevel) {
	b.alevel = alevel
}

// AudioLevel returns the audio level at the time the frame was received.
func (b *rrbb_t) AudioLevel() ax25.ALevel {
	return (b.alevel)
}

// SetSpeedError sets the speed error of the received frame, as a percentage.
func (b *rrbb_t) SetSpeedError(speed_error float64) {
	b.speed_error = speed_error
}

// SpeedError returns the speed error of the received frame, as a percentage.
func (b *rrbb_t) SpeedError() float64 {
	return (b.speed_error)
}

// IsScrambled reports whether the data is scrambled: true for 9600 baud,
// false for slower AFSK.
func (b *rrbb_t) IsScrambled() bool {
	return (b.is_scrambled)
}

// DescramState returns the data descrambler state before the first data bit
// of the frame.
func (b *rrbb_t) DescramState() int {
	return (b.descram_state)
}

// PrevDescram returns the previous descrambled bit before the first data bit
// of the frame.
func (b *rrbb_t) PrevDescram() int {
	return (b.prev_descram)
}

/* end rrbb.c */
