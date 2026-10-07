// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fcos256 and fsin256 take a phase whose top byte is the fraction of a turn.
func TestFcos256Table(t *testing.T) {
	assert.InDelta(t, 1.0, fcos256(0), 1e-12)
	assert.InDelta(t, 0.0, fsin256(0), 1e-12)
	assert.InDelta(t, 0.0, fcos256(64<<24), 1e-12)
	assert.InDelta(t, 1.0, fsin256(64<<24), 1e-12)
	assert.InDelta(t, -1.0, fcos256(128<<24), 1e-12)
}

// The space gains run geometrically from MIN_G to MAX_G.
func TestAFSKSpaceGain(t *testing.T) {
	assert.InDelta(t, MIN_G, afskSpaceGain[0], 1e-12)
	assert.InDelta(t, MAX_G, afskSpaceGain[MAX_SUBCHANS-1], 1e-9)

	var ratio = afskSpaceGain[1] / afskSpaceGain[0]
	for j := 2; j < MAX_SUBCHANS; j++ {
		assert.InDelta(t, ratio, afskSpaceGain[j]/afskSpaceGain[j-1], 1e-12)
	}
}

// Setting up a demodulator only touches the demodulator, so two can be set
// up at once - as tests running in parallel would - without racing over a
// shared table.
func TestDemodulatorInitsDoNotShareState(t *testing.T) {
	var wg sync.WaitGroup

	for range 2 {
		wg.Go(func() {
			demod_afsk_init(44100, 1200, 1200, 2200, 'A', new(demodulator_state_s))
		})
		wg.Go(func() {
			demod_9600_init(MODEM_SCRAMBLE, 48000, 1, 9600, new(demodulator_state_s))
		})
	}

	wg.Wait()
}
