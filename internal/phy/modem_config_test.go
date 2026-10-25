// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package phy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A bit rate brings the usual modem for it, with that modem's tones, and AIS
// and EAS name theirs.  A bad one leaves the modem as it was.
func TestModemConfigSetBitrate(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		bitrate         string
		modem           Modem
		baud, mark, spc int
	}{
		{"300", AFSK, 300, 1600, 1800},
		{"1200", AFSK, 1200, DefaultMarkFreq, DefaultSpaceFreq},
		{"2400", QPSK, 2400, 0, 0},
		{"4800", PSK8, 4800, 0, 0},
		{"9600", Scramble, 9600, 0, 0},
		{"ais", AIS, 9600, 0, 0},
		{"EAS", EAS, 521, 2083, 1563},
	} {
		t.Run(tc.bitrate, func(t *testing.T) {
			t.Parallel()

			var m ModemConfig

			require.NoError(t, m.SetBitrate(tc.bitrate))
			assert.Equal(t, tc.modem, m.ModemType)
			assert.Equal(t, tc.baud, m.Baud)
			assert.Equal(t, tc.mark, m.MarkFreq)
			assert.Equal(t, tc.spc, m.SpaceFreq)
		})
	}

	for _, bad := range []string{"fast", "50", "100000"} {
		t.Run(bad, func(t *testing.T) {
			t.Parallel()

			var m = ModemConfig{ModemType: AFSK, Baud: 1200} //nolint:exhaustruct_v5

			require.Error(t, m.SetBitrate(bad))
			assert.Equal(t, AFSK, m.ModemType)
			assert.Equal(t, 1200, m.Baud)
		})
	}
}

// An AFSK profile survives a change of AFSK bit rate; other modems start
// afresh.
func TestModemConfigSetBitrateKeepsAnAFSKProfile(t *testing.T) {
	t.Parallel()

	var m = ModemConfig{ModemType: AFSK, Baud: 1200, Profiles: "E+"} //nolint:exhaustruct_v5

	require.NoError(t, m.SetBitrate("300"))
	assert.Equal(t, "E+", m.Profiles)

	require.NoError(t, m.SetBitrate("9600"))
	assert.Empty(t, m.Profiles)
}

// PSK has no decimating path, so decimation is ruled out for it, and the
// decimation asked for is reported so the caller can say so.
func TestModemConfigSettleRejectsPSKDecimation(t *testing.T) {
	t.Parallel()

	for _, modemType := range []Modem{QPSK, PSK8, BPSK} {
		var m = ModemConfig{ModemType: modemType, V26Alternative: V26B, Decimate: 3} //nolint:exhaustruct_v5

		assert.Equal(t, Settled{V26Defaulted: false, DecimationDropped: 3}, m.Settle())
		assert.Equal(t, 1, m.Decimate)
	}
}

// AFSK, by contrast, does decimate.
func TestModemConfigSettleKeepsAFSKDecimation(t *testing.T) {
	t.Parallel()

	var m = ModemConfig{ModemType: AFSK, Decimate: 3} //nolint:exhaustruct_v5

	assert.Equal(t, Settled{V26Defaulted: false, DecimationDropped: 0}, m.Settle())
	assert.Equal(t, 3, m.Decimate)
}

// The transmitter reads the V.26 alternative as well as the receiver, so it
// is settled before either is set up, rather than by the demodulator.
func TestModemConfigSettleDefaultsV26Alternative(t *testing.T) {
	t.Parallel()

	var m = ModemConfig{ModemType: QPSK} //nolint:exhaustruct_v5

	assert.True(t, m.Settle().V26Defaulted)
	assert.Equal(t, V26Default, m.V26Alternative)

	assert.False(t, m.Settle().V26Defaulted, "a V.26 alternative already chosen is left alone")
	assert.Equal(t, V26Default, m.V26Alternative)
}
