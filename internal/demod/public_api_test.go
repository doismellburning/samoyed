// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package demod_test

import (
	"math"
	"testing"

	"github.com/doismellburning/samoyed/internal/demod"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bitCollector is the simplest BitReceiver: it keeps every bit, and notes
// whether data was ever detected.
type bitCollector struct {
	raw      []bool
	scramble bool
	heard    bool
}

func (c *bitCollector) RecBit(_ dwcore.SlicerID, raw bool, isScrambled bool, _ *dwcore.PLLStats) {
	c.raw = append(c.raw, raw)
	c.scramble = isScrambled
}

func (c *bitCollector) DCDChange(_ dwcore.SlicerID, on bool) {
	c.heard = c.heard || on
}

// afskFlags is the audio of n HDLC flags sent as 1200 baud AFSK at
// samplesPerSec: NRZI, so a 0 bit changes tone and a 1 keeps it, with the
// phase carried smoothly from one tone to the other.
func afskFlags(n int, samplesPerSec int) []int {
	const baud = 1200

	var bits []bool
	for range n {
		for i := range 8 {
			bits = append(bits, (0x7e>>i)&1 == 1) // LSB first.
		}
	}

	var samples []int
	var mark = true
	var phase float64
	var bit = -1

	for s := 0; ; s++ {
		var b = s * baud / samplesPerSec
		if b >= len(bits) {
			return samples
		}

		if b != bit {
			bit = b
			if !bits[b] {
				mark = !mark
			}
		}

		var freq = 2200.0
		if mark {
			freq = 1200.0
		}

		phase += 2 * math.Pi * freq / float64(samplesPerSec)
		samples = append(samples, int(16000*math.Sin(phase)))
	}
}

// A demodulator can be set up, fed audio and read from with nothing but its
// exported API: build it from the modem settings, hand it a BitReceiver, and
// give it each sample.  What comes out is the line's bits, which NRZI
// decoding turns back into the flags that went in.
func TestDemodulatorFromOutsideThePackage(t *testing.T) {
	t.Parallel()

	const samplesPerSec = 44100

	var modem phy.ModemConfig
	modem.ModemType = phy.AFSK
	modem.Baud = 1200
	modem.MarkFreq = 1200
	modem.SpaceFreq = 2200
	modem.Profiles = "A"
	modem.NumFreq = 1

	var d, err = demod.New(0, modem, phy.Layer2AX25, samplesPerSec)
	require.NoError(t, err)

	var bits = new(bitCollector)
	d.SetReceiver(bits)

	const flags = 100

	for _, sample := range afskFlags(flags, samplesPerSec) {
		d.ProcessSample(sample)
	}

	assert.InDelta(t, flags*8, len(bits.raw), flags*8/20, "about one bit out per bit in")
	assert.False(t, bits.scramble, "1200 baud AFSK isn't scrambled")
	assert.True(t, bits.heard, "a run of flags is data")

	var line linecode.Decoder
	var shift byte
	var found = 0

	for _, raw := range bits.raw {
		shift >>= 1
		if line.Decode(raw, false) {
			shift |= 0x80
		}

		if shift == 0x7e {
			found++
		}
	}

	assert.Greater(t, found, flags*8/10, "most of the flags come back out")
}
