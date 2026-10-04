// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
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
		var audioConfig = newTestRadioConfig(channel, modemType, 2400, 0, 0, 44100)
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

	var eas = newTestRadioConfig(channel, MODEM_EAS, 521, 2083, 1563, 44100)
	eas.achan[channel].num_freq = 1
	eas.achan[channel].fix_bits = RETRY_INVERT_SINGLE
	eas.achan[channel].passall = true

	demod_init(eas)

	assert.Equal(t, RETRY_INVERT_SINGLE, eas.achan[channel].fix_bits)
	assert.True(t, eas.achan[channel].passall)

	var qpsk = newTestRadioConfig(channel, MODEM_QPSK, 2400, 0, 0, 44100)
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
	var audioConfig = newTestRadioConfig(channel, MODEM_AFSK, 1200, 1200, 2200, 48000)
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
	var audioConfig = newTestRadioConfig(channel, MODEM_AFSK, 1200, 1200, 2200, 44100)
	audioConfig.achan[channel].num_freq = 1
	audioConfig.achan[channel].profiles = "ABDEABDEABDE"
	require.Greater(t, len(audioConfig.achan[channel].profiles), MAX_SUBCHANS)

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	var d = NewDemodulator(channel, audioConfig.achan[channel], 44100)

	assert.Equal(t, MAX_SUBCHANS, d.NumSubchan())
	assert.Equal(t, "ABDEABDEA", d.profiles)

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
		var audioConfig = newTestRadioConfig(channel, tc.modemType, 2400, 0, 0, 44100)
		audioConfig.achan[channel].num_freq = 1
		audioConfig.achan[channel].profiles = tc.profiles
		require.Greater(t, len(tc.profiles), MAX_SUBCHANS)

		var hook = test.NewGlobal()

		var d = NewDemodulator(channel, audioConfig.achan[channel], 44100)

		assert.Equal(t, MAX_SUBCHANS, d.NumSubchan())
		assert.Equal(t, tc.want, d.profiles)

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
	var audioConfig = newTestRadioConfig(channel, MODEM_OFF, 1200, 1200, 2200, 44100)
	var d = NewDemodulator(channel, audioConfig.achan[channel], 44100)

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

// What NewDemodulator works out from a channel's settings, it keeps: the
// configuration is left as the file and command line had it.
func TestDemodInitLeavesDerivedValuesOutOfConfig(t *testing.T) {
	var saved = demodulators

	t.Cleanup(func() {
		demodulators = saved
	})

	var channel = 0
	var audioConfig = newTestRadioConfig(channel, MODEM_AFSK, 300, 1600, 1800, 48000)
	audioConfig.achan[channel].num_freq = 3
	audioConfig.achan[channel].offset = 30
	audioConfig.achan[channel].profiles = "ab+"

	var before = audioConfig.achan[channel]

	demod_init(audioConfig)

	var d = demodulators[channel]
	require.NotNil(t, d)
	assert.Equal(t, "AB+", d.profiles)
	assert.Equal(t, 2, d.NumSubchan(), "+ can't be combined with multiple frequencies, so one per letter")
	assert.Equal(t, MAX_SLICERS, d.NumSlicers())
	assert.Equal(t, 3, d.decimate, "300 baud at 48000 samples per second decimates")

	assert.Equal(t, before, audioConfig.achan[channel])
}

// Only a radio channel gets a Demodulator, and until demod_init has run there
// are none at all, so what reaches one by channel number has to cope.
func TestDemodNilChannel(t *testing.T) {
	var saved = demodulators

	t.Cleanup(func() {
		demodulators = saved
	})

	demodulators = [MAX_RADIO_CHANS]*Demodulator{}

	var zero ax25.ALevel

	assert.Equal(t, zero, demod_get_audio_level(0, 0))
	assert.NotPanics(t, func() { demod_mute_input(0, 1) })
}

// demod_init builds a Demodulator for each radio channel, and drops any left
// over from before for a channel that no longer is one.
func TestDemodInitBuildsRadioChannelsOnly(t *testing.T) {
	var saved = demodulators

	t.Cleanup(func() {
		demodulators = saved
	})

	demodulators[1] = new(Demodulator)

	var audioConfig = newTestRadioConfig(0, MODEM_AFSK, 1200, 1200, 2200, 44100)
	audioConfig.achan[0].num_freq = 1

	demod_init(audioConfig)

	require.NotNil(t, demodulators[0])
	assert.Equal(t, 0, demodulators[0].channel)
	assert.Nil(t, demodulators[1])
}

// A received frame's subchannel and slicer are shown only where the channel's
// demodulator has more than one; a channel without one, whether not a radio
// or out of range altogether, has one of each.
func TestChannelLayout(t *testing.T) {
	var saved = demodulators

	t.Cleanup(func() {
		demodulators = saved
	})

	var audioConfig = newTestRadioConfig(0, MODEM_AFSK, 1200, 1200, 2200, 44100)
	audioConfig.achan[0].num_freq = 1
	audioConfig.achan[0].profiles = "AB+"

	demod_init(audioConfig)

	var numSubchan, numSlicers = channelLayout(0)
	assert.Equal(t, 2, numSubchan)
	assert.Equal(t, MAX_SLICERS, numSlicers)

	for _, channel := range []int{1, MAX_RADIO_CHANS, -1} {
		numSubchan, numSlicers = channelLayout(channel)
		assert.Equal(t, 1, numSubchan, "channel %d", channel)
		assert.Equal(t, 1, numSlicers, "channel %d", channel)
	}
}
