// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package bitstuff

import (
	"errors"
	"fmt"

	"github.com/doismellburning/samoyed/internal/dwutil"
)

// flag is the HDLC flag octet, 01111110, which bit stuffing keeps out of the
// data so that it can only mean the start or end of a frame.
const flag byte = 0x7e

// Stuff performs HDLC bit stuffing on in, a frame including its FCS, and
// surrounds the result with flags: a start flag, the data with a 0 inserted
// after every five 1s in a row, then an end flag.
//
// If maxBytes is above 0, the output is filled to exactly that many bytes by
// carrying on with the flag pattern, as an FX.25 codeblock wants.
//
// It returns the stuffed bytes, and how many of them hold the frame and its
// flags rather than padding.
//
// Is it particularly time/space efficient? No.  But it should work!
func Stuff(in []byte, maxBytes int) ([]byte, int) {
	var outBits []bool

	// Start flag

	for i := range 8 {
		var v = flag&(1<<i) > 0
		outBits = append(outBits, v)
	}

	// In data

	var ones = 0

	for _, b := range in {
		for i := range 8 {
			var v = b&(1<<i) > 0
			outBits = append(outBits, v)

			if v {
				ones++
				if ones == 5 {
					outBits = append(outBits, false)
					ones = 0
				}
			} else {
				ones = 0
			}
		}
	}

	// End flag

	for i := range 8 {
		var v = flag&(1<<i) > 0
		outBits = append(outBits, v)
	}

	dwutil.Assert(len(outBits) >= 16) // Start and end flags
	dwutil.Assert(len(outBits) >= 16+8*len(in))

	// Remember where meaningful data ends (before flag padding)
	meaningfulBits := len(outBits)

	// Fill remainder with flag patterns (rotating through flag bits)
	if maxBytes > 0 {
		maxBits := maxBytes * 8

		bitPos := 0 // Which bit position of flag to use (0-7)
		for len(outBits) < maxBits {
			v := flag&(1<<bitPos) > 0
			outBits = append(outBits, v)
			bitPos = (bitPos + 1) % 8
		}
	}

	// Now byte it up

	var outBytes []byte

	for len(outBits) >= 8 {
		var b byte

		for bitIdx := range 8 {
			if outBits[bitIdx] {
				b |= 1 << bitIdx
			}
		}

		outBytes = append(outBytes, b)
		outBits = outBits[8:]
	}

	// And the last 0-7 bits if present

	if len(outBits) > 0 {
		var b byte

		for bitIdx := 0; bitIdx < 8 && bitIdx < len(outBits); bitIdx++ {
			if outBits[bitIdx] {
				b |= 1 << bitIdx
			}
		}

		outBytes = append(outBytes, b)
	}

	var meaningfulLen = (meaningfulBits + 7) / 8 // Round up to bytes

	return outBytes, meaningfulLen
}

// The ways Unstuff can find its input is not an HDLC frame.  Each comes
// wrapped with the offset, into the input, of the byte it was found in.
var (
	ErrNoStartFlag   = errors.New("data does not start with a flag")
	ErrSevenOnes     = errors.New("seven 1 bits in a row")
	ErrNotWholeBytes = errors.New("not a whole number of bytes")
	ErrNoEndFlag     = errors.New("terminating flag not found")
)

// Unstuff undoes Stuff: it takes a buffer that starts with a flag, possibly
// more than one, and returns the frame up to the next flag, with the stuffed
// 0 bits removed.  The frame still has its FCS on the end.  The terminating
// flag need not be byte aligned, and anything after it is ignored.
func Unstuff(in []byte) ([]byte, error) {
	if len(in) == 0 || in[0] != flag {
		return nil, fmt.Errorf("%w at byte 0", ErrNoStartFlag)
	}

	var start = 0
	for start < len(in) && in[start] == flag {
		start++ // Skip over leading flag byte(s).
	}

	var patDet byte = 0 // The most recent eight bits.
	var oacc byte = 0   // Accumulator for a byte out.
	var olen = 0        // Number of good bits in oacc.

	var frame []byte

	for i := start; i < len(in); i++ {
		for imask := byte(0x01); imask != 0; imask <<= 1 {
			var dbit = dwutil.IfThenElse[byte]((in[i]&imask) != 0, 1, 0)

			patDet >>= 1
			patDet |= dbit << 7

			if patDet == 0xfe {
				return nil, fmt.Errorf("%w at byte %d", ErrSevenOnes, i)
			}

			if dbit != 0 {
				oacc >>= 1
				oacc |= 0x80
			} else {
				if patDet == flag { // End of frame.
					if olen == 7 {
						return frame, nil // Whole number of bytes in result including CRC
					}

					return nil, fmt.Errorf("%w at byte %d", ErrNotWholeBytes, i)
				} else if (patDet >> 2) == 0x1f {
					continue // Five '1' bits in a row, followed by '0'.  Discard the '0'.
				}

				oacc >>= 1
			}

			olen++
			if olen&8 != 0 {
				olen = 0

				frame = append(frame, oacc)
			}
		}
	}

	return nil, fmt.Errorf("%w at byte %d", ErrNoEndFlag, len(in))
}
