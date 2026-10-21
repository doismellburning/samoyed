// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"path/filepath"
	"strconv"
	"testing"

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
// and 1800 Hz it decoded 26 of these 50 frames to N's 40.
func TestBPSKSelfCorrelationKeepsUpWithLocalOscillator(t *testing.T) {
	t.Parallel()

	var wav = genBPSKPackets(t, 1200, 44100, 50, true)

	var correlated = decodeBPSK(t, wav, 1200, "M")
	var mixed = decodeBPSK(t, wav, 1200, "N")

	assert.GreaterOrEqual(t, correlated, mixed-5, "profile M decoded %d, profile N %d", correlated, mixed)
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
