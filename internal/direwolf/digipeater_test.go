// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package direwolf

// digipeat_match, which decides whether a single frame should be repeated, is
// covered thoroughly by the tests ported from Dire Wolf.  What is here is the
// layer above it: the two passes that put the result on a queue, the count
// each channel pair keeps, and the regeneration option.

import (
	"regexp"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/pfilter"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	digiFromChan = 0
	digiToChan   = 1
)

// setupDigipeater points a new APRS digipeater at two radio channels with our
// callsign on each, and returns the transmit queue it fills.
func setupDigipeater(t *testing.T) (*Digipeater, *digi_config_s, *TransmitQueue) {
	t.Helper()

	var audioConfig = new(RadioConfig)
	audioConfig.chan_medium[digiFromChan] = MEDIUM_RADIO
	audioConfig.chan_medium[digiToChan] = MEDIUM_RADIO
	audioConfig.mycall[digiFromChan] = "Q1TEST"
	audioConfig.mycall[digiToChan] = "Q2TEST"

	var digiConfig = new(digi_config_s)
	digiConfig.dedupe_time = 30

	var tq = NewTransmitQueue()
	tq.Init(audioConfig)

	return NewDigipeater(audioConfig, digiConfig, new(pfilter.PacketFilter), nil, tq.Append), digiConfig, tq
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
	var digi, digiConfig, tq = setupDigipeater(t)

	enableDigipeat(digiConfig, digiFromChan)

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digi.Digipeat(digiFromChan, pp)

	var sent = tq.Remove(digiFromChan, TQ_PRIO_0_HI)
	require.NotNil(t, sent, "the repeated frame was not queued for transmission")
	assert.Equal(t, "Q3TEST>APDW17,Q1TEST*,WIDE2-1:", sent.FormatAddrs())

	assert.Equal(t, 1, digi.GetCount(digiFromChan, digiFromChan))
}

// Cross-band repeating is the second pass, and goes out at low priority: the
// other channel's listeners have not heard it yet, so there is no rush.
func TestDigipeaterCrossChannel(t *testing.T) {
	var digi, digiConfig, tq = setupDigipeater(t)

	enableDigipeat(digiConfig, digiToChan)

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digi.Digipeat(digiFromChan, pp)

	assert.Nil(t, tq.Remove(digiFromChan, TQ_PRIO_0_HI), "it should not have gone out on the channel it arrived on")

	var sent = tq.Remove(digiToChan, TQ_PRIO_1_LO)
	require.NotNil(t, sent, "the repeated frame was not queued for the other channel")
	assert.Equal(t, "Q3TEST>APDW17,Q2TEST*,WIDE2-1:", sent.FormatAddrs(),
		"the callsign of the channel it goes out on should have been used")

	assert.Equal(t, 1, digi.GetCount(digiFromChan, digiToChan))
}

// Repeating a frame is what makes it a duplicate of itself: the same frame
// heard again by another route is not repeated a second time.
func TestDigipeaterRemembersWhatItRepeated(t *testing.T) {
	var digi, digiConfig, tq = setupDigipeater(t)

	enableDigipeat(digiConfig, digiFromChan)

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digi.Digipeat(digiFromChan, pp)

	require.NotNil(t, tq.Remove(digiFromChan, TQ_PRIO_0_HI))

	assert.True(t, digi.dedupe.Check(pp, digiFromChan),
		"the repeated frame was not remembered, so the next copy of it would go out too")
}

// A channel pair with no DIGIPEAT line repeats nothing, however the frame is
// addressed.
func TestDigipeaterNotEnabled(t *testing.T) {
	var digi, _, tq = setupDigipeater(t)

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digi.Digipeat(digiFromChan, pp)

	for c := range MAX_RADIO_CHANS {
		for p := range TQ_NUM_PRIO {
			assert.Nil(t, tq.Remove(c, p), "channel %d repeated a frame with digipeating disabled", c)
		}
	}
}

// A frame that is not asking to be repeated is not repeated, and does not
// count.
func TestDigipeaterNothingToDo(t *testing.T) {
	var digi, digiConfig, tq = setupDigipeater(t)

	enableDigipeat(digiConfig, digiFromChan)

	var pp = ax25.FromText("Q3TEST>APDW17:>hello", true)
	require.NotNil(t, pp)

	digi.Digipeat(digiFromChan, pp)

	assert.Nil(t, tq.Remove(digiFromChan, TQ_PRIO_0_HI))
	assert.Zero(t, digi.GetCount(digiFromChan, digiFromChan))
}

// A channel that cannot have been the source of a frame is a mistake worth
// saying out loud.
func TestDigipeaterInvalidChannel(t *testing.T) {
	var digi, _, _ = setupDigipeater(t)

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	for _, channel := range []int{-1, MAX_TOTAL_CHANS} {
		var output = testutils.CaptureOutput(t, func() { digi.Digipeat(channel, pp) })

		assert.Contains(t, output, "Did not expect to receive on invalid channel")
	}
}

// Regeneration sends the frame on again exactly as it arrived, rather than
// digipeating it - a separate option, for a separate purpose.
func TestDigiRegen(t *testing.T) {
	var digi, digiConfig, tq = setupDigipeater(t)

	digiConfig.regen[digiFromChan][digiToChan] = true

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digi.Regen(digiFromChan, pp)

	var sent = tq.Remove(digiToChan, TQ_PRIO_1_LO)
	require.NotNil(t, sent, "nothing was regenerated")
	assert.Equal(t, "Q3TEST>APDW17,WIDE2-2:", sent.FormatAddrs(),
		"a regenerated frame goes out exactly as it arrived")
}

// Without the option nothing is regenerated.
func TestDigiRegenDisabled(t *testing.T) {
	var digi, _, tq = setupDigipeater(t)

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digi.Regen(digiFromChan, pp)

	for c := range MAX_RADIO_CHANS {
		assert.Nil(t, tq.Remove(c, TQ_PRIO_1_LO))
	}
}

func TestNewDigipeater(t *testing.T) {
	var audioConfig = new(RadioConfig)
	var digiConfig = new(digi_config_s)
	digiConfig.dedupe_time = 30

	var filter = new(pfilter.PacketFilter)

	var digi = NewDigipeater(audioConfig, digiConfig, filter, nil, nil)

	assert.Same(t, audioConfig, digi.audioConfig)
	assert.Same(t, digiConfig, digi.config)
	assert.Same(t, filter, digi.filter)
	require.NotNil(t, digi.dedupe, "the duplicate suppression the digipeater relies on was not set up")
	assert.Equal(t, 30*time.Second, digi.dedupe.TTL())
}

// APRStt object reports reach for the digipeater whether or not startup has
// got that far.
func TestDigipeaterRememberNil(t *testing.T) {
	var pp = ax25.FromText("Q3TEST>APDW17:>hello", true)
	require.NotNil(t, pp)

	var digi *Digipeater

	assert.NotPanics(t, func() { digi.Remember(pp, digiFromChan) })
}

// What an APRStt object report remembers, the digipeater will not repeat.
func TestDigipeaterRemember(t *testing.T) {
	var digi, digiConfig, tq = setupDigipeater(t)

	enableDigipeat(digiConfig, digiFromChan)

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digi.Remember(pp, digiFromChan)
	digi.Digipeat(digiFromChan, pp)

	assert.Nil(t, tq.Remove(digiFromChan, TQ_PRIO_0_HI))
	assert.Zero(t, digi.GetCount(digiFromChan, digiFromChan))
}
