// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"math"
	"testing"

	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The self correlating BPSK demodulators' second tap is a fraction of a
// carrier cycle past one symbol, rather than a fraction of the symbol, so at a
// bit rate near twice the carrier it used to land beyond the end of the delay
// line, where nothing is written: at 2400 bit/s and 22050 samples a second,
// for one.
func TestBPSKDelayLineReachesBothTaps(t *testing.T) {
	t.Parallel()

	for _, bps := range []int{300, 1200, 2400, 4800} {
		for _, samplesPerSec := range []int{8000, 11025, 22050, 44100, 48000} {
			if samplesPerSec < 4*bps {
				continue // Too few samples a symbol to demodulate at all.
			}

			var D = new(demodulator_state_s)
			require.NoError(t, demod_psk_init(phy.BPSK, phy.V26Unspecified, samplesPerSec, bps, 'L', D))

			assert.Less(t, D.u.psk.soffs, D.u.psk.delay_line_taps, "%d bit/s, %d samples a second", bps, samplesPerSec)
			assert.Less(t, D.u.psk.coffs, D.u.psk.soffs, "%d bit/s, %d samples a second", bps, samplesPerSec)
		}
	}
}

// A bit rate so low that the bandpass filter would need more taps than
// there is room for is an error for the caller to act on, not the end of the
// process.  At 150 bit/s and 44100 samples a second, profile O's prefilter is
// too long while its delay line and low pass filter still fit.
func TestPSKPrefilterTooLargeIsAnError(t *testing.T) {
	t.Parallel()

	var D = new(demodulator_state_s)

	var err = demod_psk_init(phy.BPSK, phy.V26Unspecified, 44100, 150, 'O', D)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pre filter")
}

// Likewise a delay line too long to fit.  At 100 bit/s and 44100 samples a
// second, profile L, which has no prefilter, needs one, while its low pass
// filter still fits.
func TestPSKDelayLineTooLargeIsAnError(t *testing.T) {
	t.Parallel()

	var D = new(demodulator_state_s)

	var err = demod_psk_init(phy.BPSK, phy.V26Unspecified, 44100, 100, 'L', D)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "delay line")
}

// The BPSK demodulators' bandpass filters fit below the carrier at each of
// the NinoTNC's rates.  Profile M's used to reach below 0 Hz at 1200 bit/s
// once the carrier moved to 1500 Hz.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestBPSKPrefilterFitsBelowCarrier(t *testing.T) {
	var hook = testutils.CaptureLogrus(t)

	for _, baud := range []int{300, 1200} {
		var modem phy.ModemConfig
		modem.ModemType = phy.BPSK
		modem.Baud = baud
		modem.NumFreq = 1
		modem.Profiles = "LMNO"

		var _, initErr = NewDemodulator(0, modem, phy.Layer2AX25, 44100)
		require.NoError(t, initErr)

		assert.Nil(t, testutils.LastEntryWithMessage(hook, "Prefilter range doesn't make sense"), "at %d baud", baud)

		hook.Reset()
	}
}

// A BPSK symbol with the same phase as the one before is a 1, and one with
// the phase reversed a 0, as the NinoTNC sends IL2P.
func TestBPSKNoPhaseChangeIsOne(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1, phase_shift_to_symbol(0, 1))
	assert.Equal(t, 0, phase_shift_to_symbol(math.Pi, 1))
}
