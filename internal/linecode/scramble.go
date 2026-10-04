// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package linecode

// Scrambler applies G3RUH scrambling (1 + x^12 + x^17), as used at 9600
// baud, to the bits going out.  Its zero value is ready to use.
type Scrambler struct {
	lfsr int // The bits sent so far, most recent in bit 0.
}

// Scramble takes one bit to send, 0 or 1, and returns the bit to put on the
// line in its place.
func (s *Scrambler) Scramble(in int) int {
	var out = (in ^ (s.lfsr >> 16) ^ (s.lfsr >> 11)) & 1
	s.lfsr = (s.lfsr << 1) | (out & 1)

	return out
}

// Descrambler undoes G3RUH scrambling (1 + x^12 + x^17), as used at 9600
// baud.  It is self-synchronising: each output bit depends only on the last
// 17 bits received, so it locks on to a transmission within 17 bits whatever
// state it starts in.  Its zero value is ready to use.
type Descrambler struct {
	lfsr int // The bits received so far, most recent in bit 0.
}

// NewDescrambler makes a Descrambler that carries on from state, a value
// State returned earlier.
func NewDescrambler(state int) Descrambler {
	return Descrambler{lfsr: state}
}

// Descramble takes one received bit, 0 or 1, and returns the bit that was
// scrambled to make it.
func (d *Descrambler) Descramble(in int) int {
	var out = (in ^ (d.lfsr >> 16) ^ (d.lfsr >> 11)) & 1
	d.lfsr = (d.lfsr << 1) | (in & 1)

	return out
}

// State returns the descrambler's shift register, for NewDescrambler to
// carry on from later.
func (d *Descrambler) State() int {
	return d.lfsr
}
