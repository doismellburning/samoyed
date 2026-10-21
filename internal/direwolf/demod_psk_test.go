// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// genBPSKPackets writes frames numbered packets of BPSK at baud to a new .WAV
// file, as "samoyed-gen_packets -B baud -k -n frames -r samplesPerSec" would,
// and returns its name.  noisy adds more noise to each frame than the one
// before.
func genBPSKPackets(t *testing.T, baud int, samplesPerSec int, frames int, noisy bool) string {
	t.Helper()

	var wav = filepath.Join(t.TempDir(), "bpsk.wav")

	var flags = pflag.NewFlagSet("gen_packets", pflag.ContinueOnError)
	var modem = AddGenPacketsModemFlags(flags)
	require.NoError(t, flags.Parse([]string{"-B", strconv.Itoa(baud), "-k"}))

	var opts = newGenPacketsOptions()
	opts.Modem = modem
	opts.SampleRate = samplesPerSec

	var g, err = NewGenPackets(opts, wav)
	require.NoError(t, err)

	g.SendNumbered(frames, noisy)
	require.NoError(t, g.Close())

	return wav
}

// decodeBPSK decodes wav as BPSK at baud with demodulator profile, as
// "samoyed-atest -B baud -k -P profile" would, and returns how many frames it
// found.
func decodeBPSK(t *testing.T, wav string, baud int, profile string) int {
	t.Helper()

	var flags = pflag.NewFlagSet("atest", pflag.ContinueOnError)
	var modem = AddModemFlags(flags, true)
	require.NoError(t, flags.Parse([]string{"-B", strconv.Itoa(baud), "-k", "-P", profile}))

	var opts = atestTestOptions()
	opts.Modem = modem

	var atest, err = NewAtest(opts)
	require.NoError(t, err)

	var result, decodeErr = atest.DecodeFile(wav)
	require.NoError(t, decodeErr)

	return result.PacketsDecoded
}

// The self correlating BPSK demodulator, profile M, keeps up with the one
// that mixes with a local oscillator, profile N, as the noise rises.  Its
// taps used to be placed as fractions of a symbol, which put them in
// quadrature only for some ratios of carrier to symbol rate; at 1200 bit/s
// it decoded 26 of these 50 frames to N's 40 at 1800 Hz, and 18 to N's 39 at
// 1500 Hz.
func TestBPSKSelfCorrelationKeepsUpWithLocalOscillator(t *testing.T) {
	t.Parallel()

	var wav = genBPSKPackets(t, 1200, 44100, 50, true)

	var correlated = decodeBPSK(t, wav, 1200, "M")
	var mixed = decodeBPSK(t, wav, 1200, "N")

	assert.GreaterOrEqual(t, correlated, mixed-5, "profile M decoded %d, profile N %d", correlated, mixed)
}

// Every BPSK demodulator profile, at each rate, decodes every frame of a
// clean signal from the BPSK modulator.
func TestBPSKRoundTripEveryProfile(t *testing.T) {
	t.Parallel()

	const frames = 5

	for _, baud := range []int{300, 1200} {
		var wav = genBPSKPackets(t, baud, 44100, frames, false)

		for _, profile := range []string{"L", "M", "N", "O"} {
			t.Run(fmt.Sprintf("%d/%s", baud, profile), func(t *testing.T) {
				t.Parallel()

				assert.Equal(t, frames, decodeBPSK(t, wav, baud, profile))
			})
		}
	}
}

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
			demod_psk_init(MODEM_BPSK, V26_UNSPECIFIED, samplesPerSec, bps, 'L', D)

			assert.Less(t, D.u.psk.soffs, D.u.psk.delay_line_taps, "%d bit/s, %d samples a second", bps, samplesPerSec)
			assert.Less(t, D.u.psk.coffs, D.u.psk.soffs, "%d bit/s, %d samples a second", bps, samplesPerSec)
		}
	}
}

// The BPSK demodulators' bandpass filters fit below the carrier at each of
// the NinoTNC's rates.  Profile M's used to reach below 0 Hz at 1200 bit/s
// once the carrier moved to 1500 Hz.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestBPSKPrefilterFitsBelowCarrier(t *testing.T) {
	for _, baud := range []int{300, 1200} {
		var audioConfig = newTestRadioConfig(0, MODEM_BPSK, baud, 0, 0, 44100)
		audioConfig.achan[0].num_freq = 1
		audioConfig.achan[0].profiles = "LMNO"

		var hook = test.NewGlobal()

		NewDemodulator(0, audioConfig.achan[0], 44100)

		assert.Nil(t, lastEntryWithMessage(hook, "Prefilter range doesn't make sense"), "at %d baud", baud)

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

// newBPSKTestRadioConfig is a BPSK channel at baud, sending with layer2, and
// demodulating with profile.
func newBPSKTestRadioConfig(baud int, layer2 phy.Layer2, profile string) *RadioConfig {
	var audioConfig = newTestRadioConfig(0, MODEM_BPSK, baud, 0, 0, 44100)
	audioConfig.adev[0].defined = 1
	audioConfig.achan[0].num_freq = 1
	audioConfig.achan[0].profiles = profile
	audioConfig.achan[0].fix_bits = RETRY_NONE
	audioConfig.achan[0].layer2_xmit = layer2
	audioConfig.achan[0].il2p_max_fec = 1
	audioConfig.achan[0].il2p_crc = true

	return audioConfig
}

// What the tone generator sends over BPSK, as AX.25 or as IL2P, the
// demodulator decodes back, at each of the NinoTNC's rates.
func TestBPSKLayer2RoundTrip(t *testing.T) {
	t.Parallel()

	for _, layer2 := range []phy.Layer2{LAYER2_AX25, LAYER2_IL2P} {
		for _, baud := range []int{300, 1200} {
			t.Run(fmt.Sprintf("%v/%d", layer2, baud), func(t *testing.T) {
				t.Parallel()

				var texts, frames = afskTestFrames(t, 2)

				var samples = generateFrames(t, newBPSKTestRadioConfig(baud, layer2, "N"), frames)

				var got, _ = demodulateFrames(t, newBPSKTestRadioConfig(baud, layer2, "N"), samples)

				require.Len(t, got, len(texts))

				for i, pp := range got {
					assert.Equal(t, texts[i], pp.FormatAddrs()+string(pp.Info()))
				}
			})
		}
	}
}
