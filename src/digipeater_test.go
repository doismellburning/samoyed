// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

// digipeat_match, which decides whether a single frame should be repeated, is
// covered thoroughly by the tests ported from Dire Wolf.  What is here is the
// layer above it: the two passes that put the result on a queue, the count
// each channel pair keeps, and the regeneration option.

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	digiFromChan = 0
	digiToChan   = 1
)

// setupDigipeater points the APRS digipeater at two radio channels with our
// callsign on each, and empties the transmit queues it fills.
func setupDigipeater(t *testing.T) *digi_config_s {
	t.Helper()

	var origAudio, origDigi, origDedupe = digipeater_audio_config, save_digi_config_p, dedupeService
	var origCount = digi_count

	t.Cleanup(func() {
		digipeater_audio_config, save_digi_config_p, dedupeService = origAudio, origDigi, origDedupe
		digi_count = origCount

		for c := range MAX_RADIO_CHANS {
			for p := range TQ_NUM_PRIO {
				for tq_remove(c, p) != nil { //revive:disable-line:empty-block
				}
			}
		}
	})

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[digiFromChan] = MEDIUM_RADIO
	audioConfig.chan_medium[digiToChan] = MEDIUM_RADIO
	audioConfig.mycall[digiFromChan] = "Q1TEST"
	audioConfig.mycall[digiToChan] = "Q2TEST"

	var digiConfig = new(digi_config_s)
	digiConfig.dedupe_time = 30

	digipeater_init(audioConfig, digiConfig)

	digi_count = [MAX_TOTAL_CHANS][MAX_TOTAL_CHANS]int{}

	tq_init(audioConfig)

	return digiConfig
}

// enableDigipeat turns on digipeating from digiFromChan to the given channel,
// with the usual WIDEn-n handling.
func enableDigipeat(digiConfig *digi_config_s, to int) {
	digiConfig.enabled[digiFromChan][to] = true
	digiConfig.alias[digiFromChan][to] = regexp.MustCompile("^WIDE[4-7]-[1-7]|CITYD$")
	digiConfig.wide[digiFromChan][to] = regexp.MustCompile("^WIDE[1-7]-[1-7]$")
	digiConfig.preempt[digiFromChan][to] = PREEMPT_OFF
}

// Repeating on the channel it came in on is the ordinary case, and it goes out
// at high priority: every digipeater that heard it is supposed to transmit at
// once, so that one packet time clears the packet out of the area.
func TestDigipeaterSameChannel(t *testing.T) {
	var digiConfig = setupDigipeater(t)

	enableDigipeat(digiConfig, digiFromChan)

	var pp = AX25FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digipeater(digiFromChan, pp)

	var sent = tq_remove(digiFromChan, TQ_PRIO_0_HI)
	require.NotNil(t, sent, "the repeated frame was not queued for transmission")
	assert.Equal(t, "Q3TEST>APDW17,Q1TEST*,WIDE2-1:", AX25FormatAddrs(sent))

	assert.Equal(t, 1, digipeater_get_count(digiFromChan, digiFromChan))
}

// Cross-band repeating is the second pass, and goes out at low priority: the
// other channel's listeners have not heard it yet, so there is no rush.
func TestDigipeaterCrossChannel(t *testing.T) {
	var digiConfig = setupDigipeater(t)

	enableDigipeat(digiConfig, digiToChan)

	var pp = AX25FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digipeater(digiFromChan, pp)

	assert.Nil(t, tq_remove(digiFromChan, TQ_PRIO_0_HI), "it should not have gone out on the channel it arrived on")

	var sent = tq_remove(digiToChan, TQ_PRIO_1_LO)
	require.NotNil(t, sent, "the repeated frame was not queued for the other channel")
	assert.Equal(t, "Q3TEST>APDW17,Q2TEST*,WIDE2-1:", AX25FormatAddrs(sent),
		"the callsign of the channel it goes out on should have been used")

	assert.Equal(t, 1, digipeater_get_count(digiFromChan, digiToChan))
}

// Repeating a frame is what makes it a duplicate of itself: the same frame
// heard again by another route is not repeated a second time.
func TestDigipeaterRemembersWhatItRepeated(t *testing.T) {
	var digiConfig = setupDigipeater(t)

	enableDigipeat(digiConfig, digiFromChan)

	var pp = AX25FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digipeater(digiFromChan, pp)

	require.NotNil(t, tq_remove(digiFromChan, TQ_PRIO_0_HI))

	assert.True(t, dedupeService.Check(pp, digiFromChan),
		"the repeated frame was not remembered, so the next copy of it would go out too")
}

// A channel pair with no DIGIPEAT line repeats nothing, however the frame is
// addressed.
func TestDigipeaterNotEnabled(t *testing.T) {
	setupDigipeater(t)

	var pp = AX25FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digipeater(digiFromChan, pp)

	for c := range MAX_RADIO_CHANS {
		for p := range TQ_NUM_PRIO {
			assert.Nil(t, tq_remove(c, p), "channel %d repeated a frame with digipeating disabled", c)
		}
	}
}

// A frame that is not asking to be repeated is not repeated, and does not
// count.
func TestDigipeaterNothingToDo(t *testing.T) {
	var digiConfig = setupDigipeater(t)

	enableDigipeat(digiConfig, digiFromChan)

	var pp = AX25FromText("Q3TEST>APDW17:>hello", true)
	require.NotNil(t, pp)

	digipeater(digiFromChan, pp)

	assert.Nil(t, tq_remove(digiFromChan, TQ_PRIO_0_HI))
	assert.Zero(t, digipeater_get_count(digiFromChan, digiFromChan))
}

// A channel that cannot have been the source of a frame is a mistake worth
// saying out loud.
func TestDigipeaterInvalidChannel(t *testing.T) {
	setupDigipeater(t)

	var pp = AX25FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	for _, channel := range []int{-1, MAX_TOTAL_CHANS} {
		var output = CaptureOutput(t, func() { digipeater(channel, pp) })

		assert.Contains(t, output, "Did not expect to receive on invalid channel")
	}
}

// Regeneration sends the frame on again exactly as it arrived, rather than
// digipeating it - a separate option, for a separate purpose.
func TestDigiRegen(t *testing.T) {
	var digiConfig = setupDigipeater(t)

	digiConfig.regen[digiFromChan][digiToChan] = true

	var pp = AX25FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digi_regen(digiFromChan, pp)

	var sent = tq_remove(digiToChan, TQ_PRIO_1_LO)
	require.NotNil(t, sent, "nothing was regenerated")
	assert.Equal(t, "Q3TEST>APDW17,WIDE2-2:", AX25FormatAddrs(sent),
		"a regenerated frame goes out exactly as it arrived")
}

// Without the option nothing is regenerated.
func TestDigiRegenDisabled(t *testing.T) {
	setupDigipeater(t)

	var pp = AX25FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digi_regen(digiFromChan, pp)

	for c := range MAX_RADIO_CHANS {
		assert.Nil(t, tq_remove(c, TQ_PRIO_1_LO))
	}
}

func TestDigipeaterInit(t *testing.T) {
	var origAudio, origDigi, origDedupe = digipeater_audio_config, save_digi_config_p, dedupeService

	t.Cleanup(func() { digipeater_audio_config, save_digi_config_p, dedupeService = origAudio, origDigi, origDedupe })

	var audioConfig = new(audio_s)
	var digiConfig = new(digi_config_s)
	digiConfig.dedupe_time = 30

	digipeater_init(audioConfig, digiConfig)

	assert.Same(t, audioConfig, digipeater_audio_config)
	assert.Same(t, digiConfig, save_digi_config_p)
	assert.NotNil(t, dedupeService, "the duplicate suppression the digipeater relies on was not set up")
}
