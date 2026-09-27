// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PSK has no decimating path, so a PSK channel configured to decimate is
// demodulated at the full sample rate - and demod_init leaves the
// configuration as it found it, settleModemOptions being where that is
// reported and put right.
func TestDemodInitIgnoresPSKDecimation(t *testing.T) {
	for _, modemType := range []modem_t{MODEM_QPSK, MODEM_8PSK, MODEM_BPSK} {
		var channel = 0
		var audioConfig = newTestAudioConfig(channel, modemType, 2400, 0, 0, 44100)
		audioConfig.achan[channel].decimate = 3
		audioConfig.achan[channel].num_freq = 1

		var output = testutils.CaptureOutput(t, func() {
			demod_init(audioConfig)
		})

		assert.NotContains(t, output, "/ 3")
		assert.Equal(t, 3, audioConfig.achan[channel].decimate)
	}
}

// demod_init sets up the demodulators from the modem options without
// changing them: the rules about which go together are settleModemOptions'.
func TestDemodInitLeavesModemOptionsAlone(t *testing.T) {
	var channel = 0

	var eas = newTestAudioConfig(channel, MODEM_EAS, 521, 2083, 1563, 44100)
	eas.achan[channel].num_freq = 1
	eas.achan[channel].fix_bits = RETRY_INVERT_SINGLE
	eas.achan[channel].passall = true

	demod_init(eas)

	assert.Equal(t, RETRY_INVERT_SINGLE, eas.achan[channel].fix_bits)
	assert.True(t, eas.achan[channel].passall)

	var qpsk = newTestAudioConfig(channel, MODEM_QPSK, 2400, 0, 0, 44100)
	qpsk.achan[channel].num_freq = 1

	var output = testutils.CaptureOutput(t, func() {
		demod_init(qpsk)
	})

	assert.Contains(t, output, "compatible with MFJ-2400", "an unsettled V.26 alternative still gets the default")
	assert.Equal(t, V26_UNSPECIFIED, qpsk.achan[channel].v26_alternative)
}

// AFSK, by contrast, does decimate.
func TestDemodInitKeepsAFSKDecimation(t *testing.T) {
	var channel = 1
	var audioConfig = newTestAudioConfig(channel, MODEM_AFSK, 1200, 1200, 2200, 48000)
	audioConfig.achan[channel].decimate = 3
	audioConfig.achan[channel].num_freq = 1

	demod_init(audioConfig)

	assert.Equal(t, 3, audioConfig.achan[channel].decimate)
}

// Regression test for #671: the demodulator types are one letter per
// demodulator, and the demodulator state is MAX_SUBCHANS wide, so a longer
// list used to walk off the end of it and hit an assert during startup.
func TestDemodInitCapsProfileLetters(t *testing.T) {
	var channel = 0
	var audioConfig = newTestAudioConfig(channel, MODEM_AFSK, 1200, 1200, 2200, 44100)
	audioConfig.achan[channel].num_freq = 1
	audioConfig.achan[channel].profiles = "ABDEABDEABDE"
	require.Greater(t, len(audioConfig.achan[channel].profiles), MAX_SUBCHANS)

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	demod_init(audioConfig)

	assert.Equal(t, MAX_SUBCHANS, audioConfig.achan[channel].num_subchan)
	assert.Equal(t, "ABDEABDEA", audioConfig.achan[channel].profiles)

	var entry = hook.LastEntry()

	require.NotNil(t, entry, "using fewer demodulator types than asked for must be reported")
	assert.Equal(t, logrus.ErrorLevel, entry.Level)
	assert.Contains(t, entry.Message, "More demodulator types than there are demodulators")
	assert.Equal(t, channel, entry.Data["channel"])
	assert.Equal(t, MAX_SUBCHANS, entry.Data["available"])
	assert.Equal(t, "ABDEABDEA", entry.Data["using"])
}

// The PSK modems take their profiles straight from the configuration rather
// than through AFSK's normalising, so they need the same cap: an overlong list
// for any of them used to abort startup on the subchannel index assert.
func TestDemodInitCapsProfileLettersForPSK(t *testing.T) {
	for _, tc := range []struct {
		modemType modem_t
		profiles  string
		want      string
	}{
		{MODEM_QPSK, "PQRSPQRSPQRS", "PQRSPQRSP"},
		{MODEM_8PSK, "TUVWTUVWTUVW", "TUVWTUVWT"},
		{MODEM_BPSK, "QQQQQQQQQQQQ", "QQQQQQQQQ"},
	} {
		var channel = 0
		var audioConfig = newTestAudioConfig(channel, tc.modemType, 2400, 0, 0, 44100)
		audioConfig.achan[channel].num_freq = 1
		audioConfig.achan[channel].profiles = tc.profiles
		require.Greater(t, len(tc.profiles), MAX_SUBCHANS)

		var hook = test.NewGlobal()

		demod_init(audioConfig)

		assert.Equal(t, MAX_SUBCHANS, audioConfig.achan[channel].num_subchan)
		assert.Equal(t, tc.want, audioConfig.achan[channel].profiles)

		var entry = hook.LastEntry()

		require.NotNil(t, entry, "an overlong PSK profile list must be reported")
		assert.Equal(t, logrus.ErrorLevel, entry.Level)
		assert.Contains(t, entry.Message, "More demodulator types than there are demodulators")

		hook.Reset()
	}
}

// PTT.Set mutes a half duplex channel's input from the transmit thread while
// the audio thread is reading the flag for every sample, so the flag has to be
// safe to share between them.  Run under -race.
func TestDemodMuteInputConcurrentWithProcessSample(t *testing.T) {
	var channel = 0
	var audioConfig = newTestAudioConfig(channel, MODEM_OFF, 1200, 1200, 2200, 44100)

	var savedConfig = save_audio_config_p

	t.Cleanup(func() {
		save_audio_config_p = savedConfig

		demod_mute_input(channel, 0)
	})

	save_audio_config_p = audioConfig

	var done = make(chan struct{})

	go func() {
		defer close(done)

		for i := range 1000 {
			demod_mute_input(channel, i%2)
		}
	}()

	for range 1000 {
		demod_process_sample(channel, 0, 1000)
	}

	<-done
}
