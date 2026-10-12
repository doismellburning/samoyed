// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PSK has no decimating path, so decimation is ruled out for it, and said so
// rather than done silently.
func TestSettleModemOptionsRejectsPSKDecimation(t *testing.T) {
	for _, modemType := range []modem_t{MODEM_QPSK, MODEM_8PSK, MODEM_BPSK} {
		var channel = 0
		var audioConfig = newTestRadioConfig(channel, modemType, 2400, 0, 0, 44100)
		audioConfig.achan[channel].decimate = 3

		var hook = test.NewGlobal()

		settleModemOptions(audioConfig)

		var entry = lastEntryWithMessage(hook, "Decimation is not supported for PSK, ignoring")

		require.NotNil(t, entry)
		assert.Equal(t, logrus.WarnLevel, entry.Level)
		assert.Equal(t, 3, entry.Data["decimate"])

		hook.Reset()

		assert.Equal(t, 1, audioConfig.achan[channel].decimate)
	}
}

// AFSK, by contrast, does decimate.
func TestSettleModemOptionsKeepsAFSKDecimation(t *testing.T) {
	var channel = 1
	var audioConfig = newTestRadioConfig(channel, MODEM_AFSK, 1200, 1200, 2200, 48000)
	audioConfig.achan[channel].decimate = 3

	settleModemOptions(audioConfig)

	assert.Equal(t, 3, audioConfig.achan[channel].decimate)
}

// EAS and AIS take only a frame with a good CRC as it came.
func TestSettleModemOptionsTurnsOffFixBitsForEASAndAIS(t *testing.T) {
	for _, tc := range []struct {
		modemType modem_t
		name      string
	}{
		{MODEM_EAS, "EAS"},
		{MODEM_AIS, "AIS"},
	} {
		var channel = 0
		var audioConfig = newTestRadioConfig(channel, tc.modemType, 1200, 0, 0, 44100)
		audioConfig.achan[channel].fix_bits = RETRY_INVERT_SINGLE
		audioConfig.achan[channel].passall = true

		var hook = test.NewGlobal()

		settleModemOptions(audioConfig)

		for _, message := range []string{"FIX_BITS option has been turned off", "PASSALL option has been turned off"} {
			var entry = lastEntryWithMessage(hook, message)

			require.NotNil(t, entry, message)
			assert.Equal(t, channel, entry.Data["channel"])
			assert.Equal(t, tc.name, entry.Data["modem"])
		}

		hook.Reset()
		assert.Equal(t, RETRY_NONE, audioConfig.achan[channel].fix_bits)
		assert.False(t, audioConfig.achan[channel].passall)
	}
}

// Other modems keep their bit fixing.
func TestSettleModemOptionsKeepsFixBitsForAFSK(t *testing.T) {
	var channel = 0
	var audioConfig = newTestRadioConfig(channel, MODEM_AFSK, 1200, 1200, 2200, 44100)
	audioConfig.achan[channel].fix_bits = RETRY_INVERT_SINGLE
	audioConfig.achan[channel].passall = true

	settleModemOptions(audioConfig)

	assert.Equal(t, RETRY_INVERT_SINGLE, audioConfig.achan[channel].fix_bits)
	assert.True(t, audioConfig.achan[channel].passall)
}

// The transmitter reads the V.26 alternative as well as the receiver, so it
// is settled here, before either is set up, rather than by the demodulator.
func TestSettleModemOptionsDefaultsV26Alternative(t *testing.T) {
	var channel = 0
	var audioConfig = newTestRadioConfig(channel, MODEM_QPSK, 2400, 0, 0, 44100)

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	settleModemOptions(audioConfig)

	var entry = hook.LastEntry()

	require.NotNil(t, entry)
	assert.Contains(t, entry.Message, "Defaulting to MFJ-2400 compatibility mode")
	assert.Equal(t, V26_DEFAULT, audioConfig.achan[channel].v26_alternative)

	hook.Reset()

	settleModemOptions(audioConfig)

	assert.Empty(t, hook.AllEntries(), "a V.26 alternative already chosen is left alone")
	assert.Equal(t, V26_DEFAULT, audioConfig.achan[channel].v26_alternative)
}

// A channel with no radio on it has no modem options to settle.
func TestSettleModemOptionsSkipsNonRadioChannels(t *testing.T) {
	var audioConfig = newTestRadioConfig(0, MODEM_AFSK, 1200, 1200, 2200, 44100)
	audioConfig.achan[1].modem_type = MODEM_QPSK

	settleModemOptions(audioConfig)

	assert.Equal(t, V26_UNSPECIFIED, audioConfig.achan[1].v26_alternative)
}
