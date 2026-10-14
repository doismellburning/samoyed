// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
)

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

		var output = testutils.CaptureOutput(t, func() {
			settleModemOptions(audioConfig)
		})

		assert.Contains(t, output, "Channel 0: FIX_BITS option has been turned off for "+tc.name)
		assert.Contains(t, output, "Channel 0: PASSALL option has been turned off for "+tc.name)
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

// A channel with no radio on it has no modem options to settle.
func TestSettleModemOptionsSkipsNonRadioChannels(t *testing.T) {
	var audioConfig = newTestRadioConfig(0, MODEM_AFSK, 1200, 1200, 2200, 44100)
	audioConfig.achan[1].ModemType = MODEM_QPSK

	settleModemOptions(audioConfig)

	assert.Equal(t, V26_UNSPECIFIED, audioConfig.achan[1].V26Alternative)
}

// Each radio channel's modem is settled by its own rules too, and what they
// changed is said.
func TestSettleModemOptionsSettlesTheModem(t *testing.T) {
	var savedHooks = logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))
	t.Cleanup(func() { logrus.StandardLogger().ReplaceHooks(savedHooks) })

	var hook = test.NewGlobal()

	var audioConfig = newTestRadioConfig(0, MODEM_QPSK, 2400, 0, 0, 44100)
	audioConfig.achan[0].Decimate = 3

	settleModemOptions(audioConfig)

	assert.Equal(t, V26_DEFAULT, audioConfig.achan[0].V26Alternative)
	assert.Equal(t, 1, audioConfig.achan[0].Decimate)

	var messages []string
	for _, entry := range hook.AllEntries() {
		messages = append(messages, entry.Message)
	}

	assert.Equal(t, []string{
		"Two incompatible versions of 2400 bps QPSK are now available. The default is now MFJ-2400 compatibility mode.",
		"Decimation is not supported for PSK - ignoring",
	}, messages)
}
