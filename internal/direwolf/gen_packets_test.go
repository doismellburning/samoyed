// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// genPacketsDecode decodes the .WAV file called name as a bare
// "samoyed-atest" would, returning how many frames it found.
func genPacketsDecode(t *testing.T, name string) int {
	t.Helper()

	var opts = new(AtestOptions)
	opts.IL2PVersion = "0.6"

	var atest, err = NewAtest(opts)
	require.NoError(t, err)

	var result, decodeErr = atest.DecodeFile(name)
	require.NoError(t, decodeErr)

	return result.PacketsDecoded
}

func newGenPacketsOptions() *GenPacketsOptions {
	var opts = new(GenPacketsOptions)
	opts.Amplitude = 50

	return opts
}

func Test_GenPackets_sendsWhatAtestDecodes(t *testing.T) {
	var testCases = map[string]struct {
		send func(t *testing.T, g *GenPackets)
		want int
	}{
		"built in": {func(_ *testing.T, g *GenPackets) { g.SendBuiltIn() }, 4},
		"numbered": {func(_ *testing.T, g *GenPackets) { g.SendNumbered(3, false) }, 3},
		"given": {func(t *testing.T, g *GenPackets) {
			t.Helper()
			require.NoError(t, g.SendPacket("Q1TEST>APDW17:>First"))
			require.NoError(t, g.SendPacket("Q2TEST-9>APDW17,WIDE1-1:!4237.14NS07120.83W#Second"))
		}, 2},
		"variable speed": {func(t *testing.T, g *GenPackets) {
			t.Helper()
			require.NoError(t, g.SendVariableSpeed(1, 0.5))
		}, 5},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var f = filepath.Join(t.TempDir(), "out.wav")

			var g, err = NewGenPackets(newGenPacketsOptions(), f)
			require.NoError(t, err)

			tc.send(t, g)
			require.NoError(t, g.Close())

			assert.Equal(t, tc.want, genPacketsDecode(t, f))
		})
	}
}

func Test_GenPackets_noiseLosesSomeFrames(t *testing.T) {
	var f = filepath.Join(t.TempDir(), "out.wav")

	var g, err = NewGenPackets(newGenPacketsOptions(), f)
	require.NoError(t, err)

	g.SendNumbered(20, true)
	require.NoError(t, g.Close())

	var decoded = genPacketsDecode(t, f)
	assert.Positive(t, decoded)
	assert.Less(t, decoded, 20)
}

func Test_GenPackets_SendPacket_refusesWhatIsNotTNC2(t *testing.T) {
	var g, err = NewGenPackets(newGenPacketsOptions(), filepath.Join(t.TempDir(), "out.wav"))
	require.NoError(t, err)

	defer g.Close()

	assert.ErrorContains(t, g.SendPacket("bogus"), "not valid TNC2 monitoring format")
}

func Test_GenPackets_SendVariableSpeed_refusesNoIncrement(t *testing.T) {
	var g, err = NewGenPackets(newGenPacketsOptions(), filepath.Join(t.TempDir(), "out.wav"))
	require.NoError(t, err)

	defer g.Close()

	assert.ErrorContains(t, g.SendVariableSpeed(5, 0), "increment must be more than 0")
}

func Test_NewGenPackets_refusesBadOptions(t *testing.T) {
	var testCases = map[string]struct {
		change func(opts *GenPacketsOptions)
		want   string
	}{
		"amplitude too low":  {func(opts *GenPacketsOptions) { opts.Amplitude = -1 }, "amplitude must be in range"},
		"amplitude too high": {func(opts *GenPacketsOptions) { opts.Amplitude = 201 }, "amplitude must be in range"},
		"sample rate":        {func(opts *GenPacketsOptions) { opts.SampleRate = 100 }, "more reasonable audio sample rate"},
		"morse too slow":     {func(opts *GenPacketsOptions) { opts.MorseWPM = 4 }, "morse code speed must be in range"},
		"morse too fast":     {func(opts *GenPacketsOptions) { opts.MorseWPM = 51 }, "morse code speed must be in range"},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var opts = newGenPacketsOptions()
			tc.change(opts)

			var _, err = NewGenPackets(opts, filepath.Join(t.TempDir(), "out.wav"))
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func Test_NewGenPackets_refusesAFileItCannotCreate(t *testing.T) {
	var _, err = NewGenPackets(newGenPacketsOptions(), filepath.Join(t.TempDir(), "missing", "out.wav"))
	assert.Error(t, err)
}
