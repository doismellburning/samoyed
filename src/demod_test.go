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

// PSK has no decimating path in demod_process_sample, so demod_init must rule
// decimation out, and say so rather than doing it silently.
func TestDemodInitRejectsPSKDecimation(t *testing.T) {
	for _, modemType := range []modem_t{MODEM_QPSK, MODEM_8PSK, MODEM_BPSK} {
		var channel = 0
		var audioConfig = newTestAudioConfig(channel, modemType, 2400, 0, 0, 44100)
		audioConfig.achan[channel].decimate = 3
		audioConfig.achan[channel].num_freq = 1

		testutils.AssertOutputContains(t, func() {
			demod_init(audioConfig)
		}, "Decimation is not supported for PSK")

		assert.Equal(t, 1, audioConfig.achan[channel].decimate)
	}
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
	var d = NewDemodulator(channel, newTestAudioConfig(channel, MODEM_OFF, 1200, 1200, 2200, 44100))

	var done = make(chan struct{})

	go func() {
		defer close(done)

		for i := range 1000 {
			d.Mute(i%2 != 0)
		}
	}()

	for range 1000 {
		d.ProcessSample(0, 1000)
	}

	<-done
}

// The derived values NewDemodulator writes back must land in the
// configuration the rest of the receive path reads, not in a copy of it.
func TestNewDemodulatorSharesAudioConfig(t *testing.T) {
	var channel = 1
	var audioConfig = newTestAudioConfig(channel, MODEM_AFSK, 1200, 1200, 2200, 44100)
	audioConfig.achan[channel].num_freq = 1
	audioConfig.achan[channel].profiles = "ab"

	var d = NewDemodulator(channel, audioConfig)

	assert.Same(t, audioConfig, d.audioConfig)
	assert.Equal(t, 2, audioConfig.achan[channel].num_subchan)
	assert.Equal(t, "AB", audioConfig.achan[channel].profiles)
}

// Only a radio channel gets a Demodulator, and until demod_init has run there
// are none at all, so what reaches one by channel number has to cope.
func TestDemodNilChannel(t *testing.T) {
	var saved = demodulators

	t.Cleanup(func() {
		demodulators = saved
	})

	demodulators = [MAX_RADIO_CHANS]*Demodulator{}

	var zero ALevel

	assert.Equal(t, zero, demod_get_audio_level(0, 0))
	assert.NotPanics(t, func() { demod_mute_input(0, 1) })
	assert.Panics(t, func() { demod_process_sample(0, 0, 0) })
}

// demod_init builds a Demodulator for each radio channel, and drops any left
// over from before for a channel that no longer is one.
func TestDemodInitBuildsRadioChannelsOnly(t *testing.T) {
	var saved = demodulators

	t.Cleanup(func() {
		demodulators = saved
	})

	demodulators[1] = new(Demodulator)

	var audioConfig = newTestAudioConfig(0, MODEM_AFSK, 1200, 1200, 2200, 44100)
	audioConfig.achan[0].num_freq = 1

	demod_init(audioConfig)

	require.NotNil(t, demodulators[0])
	assert.Equal(t, 0, demodulators[0].channel)
	assert.Same(t, audioConfig, demodulators[0].audioConfig)
	assert.Nil(t, demodulators[1])
}
