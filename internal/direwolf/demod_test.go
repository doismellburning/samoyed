// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression test for #671: the demodulator types are one letter per
// demodulator, and the demodulator state is phy.MaxSubchans wide, so a longer
// list used to walk off the end of it and hit an assert during startup.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestDemodInitCapsProfileLetters(t *testing.T) {
	var channel = 0
	var modem phy.ModemConfig
	modem.ModemType = phy.AFSK
	modem.Baud = 1200
	modem.MarkFreq = 1200
	modem.SpaceFreq = 2200
	modem.NumFreq = 1
	modem.Profiles = "ABDEABDEABDE"
	require.Greater(t, len(modem.Profiles), phy.MaxSubchans)

	var hook = testutils.CaptureLogrus(t)

	var d, dErr = NewDemodulator(channel, modem, phy.Layer2AX25, 44100)
	require.NoError(t, dErr)

	assert.Equal(t, phy.MaxSubchans, d.NumSubchan())
	assert.Equal(t, "ABDEABDEA", d.profiles)

	var entry = testutils.LastEntryWithMessage(hook, "More demodulator types than there are demodulators")

	require.NotNil(t, entry, "using fewer demodulator types than asked for must be reported")
	assert.Equal(t, logrus.ErrorLevel, entry.Level)
	assert.Equal(t, channel, entry.Data["channel"])
	assert.Equal(t, phy.MaxSubchans, entry.Data["available"])
	assert.Equal(t, "ABDEABDEA", entry.Data["using"])
}

// An AFSK demodulator type that doesn't exist used to get past the check for
// letters and reach demod_afsk_init, which exited.  It is reported and left
// out, and a list left with no letters gets the usual default.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestDemodInitIgnoresUnknownAFSKProfiles(t *testing.T) {
	var hook = testutils.CaptureLogrus(t)

	for _, tc := range []struct {
		profiles string
		want     string
	}{
		{"C", "A+"},
		{"C-", "A"},
		{"AC", "A"},
		{"zb+", "B+"},
	} {
		var channel = 0
		var modem phy.ModemConfig
		modem.ModemType = phy.AFSK
		modem.Baud = 1200
		modem.MarkFreq = 1200
		modem.SpaceFreq = 2200
		modem.NumFreq = 1
		modem.Profiles = tc.profiles

		var d, dErr = NewDemodulator(channel, modem, phy.Layer2AX25, 44100)
		require.NoError(t, dErr)

		assert.Equal(t, tc.want, d.profiles, tc.profiles)

		var entry = testutils.LastEntryWithMessage(hook, "Unknown AFSK demodulator type; ignoring it")

		require.NotNil(t, entry, "an unknown demodulator type must be reported")
		assert.Equal(t, logrus.ErrorLevel, entry.Level)
		assert.Equal(t, channel, entry.Data["channel"])
		assert.Equal(t, tc.profiles, entry.Data["profiles"])

		hook.Reset()
	}
}

// The PSK modems take their profiles straight from the configuration rather
// than through AFSK's normalising, so they need the same cap: an overlong list
// for any of them used to abort startup on the subchannel index assert.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestDemodInitCapsProfileLettersForPSK(t *testing.T) {
	var hook = testutils.CaptureLogrus(t)

	for _, tc := range []struct {
		modemType phy.Modem
		profiles  string
		want      string
	}{
		{phy.QPSK, "PQRSPQRSPQRS", "PQRSPQRSP"},
		{phy.PSK8, "TUVWTUVWTUVW", "TUVWTUVWT"},
		{phy.BPSK, "QQQQQQQQQQQQ", "QQQQQQQQQ"},
	} {
		var channel = 0
		var modem phy.ModemConfig
		modem.ModemType = tc.modemType
		modem.Baud = 2400
		modem.NumFreq = 1
		modem.Profiles = tc.profiles
		require.Greater(t, len(tc.profiles), phy.MaxSubchans)

		var d, dErr = NewDemodulator(channel, modem, phy.Layer2AX25, 44100)
		require.NoError(t, dErr)

		assert.Equal(t, phy.MaxSubchans, d.NumSubchan())
		assert.Equal(t, tc.want, d.profiles)

		var entry = testutils.LastEntryWithMessage(hook, "More demodulator types than there are demodulators")

		require.NotNil(t, entry, "an overlong PSK profile list must be reported")
		assert.Equal(t, logrus.ErrorLevel, entry.Level)

		hook.Reset()
	}
}

// PTT.Set mutes a half duplex channel's input from the transmit thread while
// the audio thread is reading the flag for every sample, so the flag has to be
// safe to share between them.  Run under -race.
func TestDemodMuteInputConcurrentWithProcessSample(t *testing.T) {
	t.Parallel()

	var channel = 0
	var modem phy.ModemConfig
	modem.ModemType = phy.Off
	modem.Baud = 1200
	modem.MarkFreq = 1200
	modem.SpaceFreq = 2200
	var d, dErr = NewDemodulator(channel, modem, phy.Layer2AX25, 44100)
	require.NoError(t, dErr)

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
