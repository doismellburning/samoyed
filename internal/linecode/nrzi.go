// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package linecode

// Decoder recovers data bits from the bits a demodulator hears, undoing G3RUH
// scrambling first where the channel uses it, then NRZI: a data 1 leaves the
// line as it was, a data 0 inverts it.  Its zero value is ready to use.
type Decoder struct {
	descrambler Descrambler
	prevDescram int  // Previous descrambled bit, for NRZI after descrambling.
	prevRaw     bool // Previous received bit, for NRZI without scrambling.
}

// Restore puts the decoder back to a state State and PrevRaw returned
// earlier, so a frame can be decoded again from its start.
func (d *Decoder) Restore(descramState int, prevDescram int, prevRaw bool) {
	d.descrambler = NewDescrambler(descramState)
	d.prevDescram = prevDescram
	d.prevRaw = prevRaw
}

// Decode takes one received bit and returns the data bit it carries.
// scrambled says whether the channel scrambles its data, as 9600 baud does.
func (d *Decoder) Decode(raw bool, scrambled bool) bool {
	var dbit bool

	if scrambled {
		var in = 0
		if raw {
			in = 1
		}

		var descram = d.descrambler.Descramble(in)

		dbit = (descram == d.prevDescram)
		d.prevDescram = descram
	} else {
		dbit = (raw == d.prevRaw)
	}

	d.prevRaw = raw

	return dbit
}

// State returns the descrambler's shift register and the previous descrambled
// bit, which with PrevRaw are all Restore needs to pick up from here.
func (d *Decoder) State() (descramState int, prevDescram int) {
	return d.descrambler.State(), d.prevDescram
}

// PrevRaw returns the last bit Decode was given.
func (d *Decoder) PrevRaw() bool {
	return d.prevRaw
}

// Encoder puts bits on the line for a sender, handing each line level to a
// sink.  It keeps the NRZI level from one bit to the next, and so from one
// frame to the next, because the receiver decodes each bit against the one
// before it.  Each channel wants its own.
type Encoder struct {
	sink  func(level int)
	level int // The level the line was last left at by WriteNRZI.
}

// NewEncoder makes an Encoder that hands each line level, 0 or 1, to sink.
// The line starts low.
func NewEncoder(sink func(level int)) *Encoder {
	var e = new(Encoder)
	e.sink = sink

	return e
}

// WriteNRZI sends one data bit NRZI: a 1 leaves the line as it was, a 0
// inverts it.
func (e *Encoder) WriteNRZI(bit bool) {
	if !bit {
		e.level = 1 - e.level
	}

	e.sink(e.level)
}

// Write sends one bit as it is, or inverted if invert is set, without NRZI.
// It leaves the level WriteNRZI carries on from alone.
func (e *Encoder) Write(bit bool, invert bool) {
	var level = 0
	if bit != invert {
		level = 1
	}

	e.sink(level)
}

// Level returns the level WriteNRZI last left the line at.
func (e *Encoder) Level() int {
	return e.level
}
