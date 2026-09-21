// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

// The connected mode digipeater: a station asks to be repeated by naming us,
// or one of our aliases, in the next unused digipeater field, and we put our
// own transmitting callsign there and mark it used.

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	cdigiFromChan = 0
	cdigiToChan   = 1
)

// setupCDigipeater points the connected mode digipeater at two radio channels
// with our callsign on each, and empties the transmit queues it fills.
func setupCDigipeater(t *testing.T) (*audio_s, *cdigi_config_s) {
	t.Helper()

	var origAudio, origCDigi, origCount = save_audio_config_p, save_cdigi_config_p, cdigi_count

	t.Cleanup(func() {
		save_audio_config_p, save_cdigi_config_p, cdigi_count = origAudio, origCDigi, origCount

		for c := range MAX_RADIO_CHANS {
			for p := range TQ_NUM_PRIO {
				for tq_remove(c, p) != nil { //revive:disable-line:empty-block
				}
			}
		}
	})

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[cdigiFromChan] = MEDIUM_RADIO
	audioConfig.chan_medium[cdigiToChan] = MEDIUM_RADIO
	audioConfig.mycall[cdigiFromChan] = "Q1TEST"
	audioConfig.mycall[cdigiToChan] = "Q2TEST"

	var cdigiConfig = new(cdigi_config_s)

	cdigipeater_init(audioConfig, cdigiConfig)

	cdigi_count = [MAX_RADIO_CHANS][MAX_RADIO_CHANS]int{}

	tq_init(audioConfig)

	return audioConfig, cdigiConfig
}

// A station that named us as its next digipeater is repeated, with the
// callsign of the channel we transmit on put in place of the one it asked for
// and marked as used.
func TestCDigipeatMatchExplicitCall(t *testing.T) {
	setupCDigipeater(t)

	var pp = AX25FromText("Q3TEST>Q4TEST,Q1TEST:hello", true)
	require.NotNil(t, pp)

	var result = cdigipeat_match(cdigiFromChan, pp, "Q1TEST", "Q2TEST", false, nil, cdigiToChan, "")

	require.NotNil(t, result, "a frame addressed to us was not repeated")
	assert.Equal(t, "Q3TEST>Q4TEST,Q2TEST*:", AX25FormatAddrs(result))

	// The original is repeated to every enabled channel in turn, so it has
	// to come back unchanged.
	assert.Equal(t, "Q3TEST>Q4TEST,Q1TEST:", AX25FormatAddrs(pp))
}

// An alias is the other way of asking: a pattern the configuration gives for
// names we also answer to.
func TestCDigipeatMatchAlias(t *testing.T) {
	setupCDigipeater(t)

	var pp = AX25FromText("Q3TEST>Q4TEST,WIDE1-1:hello", true)
	require.NotNil(t, pp)

	var alias = regexp.MustCompile("^WIDE[1-7]-[1-7]$")

	var result = cdigipeat_match(cdigiFromChan, pp, "Q1TEST", "Q2TEST", true, alias, cdigiToChan, "")

	require.NotNil(t, result, "a frame addressed to one of our aliases was not repeated")
	assert.Equal(t, "Q3TEST>Q4TEST,Q2TEST*:", AX25FormatAddrs(result))
}

// An alias that does not match is somebody else's business.
func TestCDigipeatMatchAliasNoMatch(t *testing.T) {
	setupCDigipeater(t)

	var pp = AX25FromText("Q3TEST>Q4TEST,Q5TEST:hello", true)
	require.NotNil(t, pp)

	var alias = regexp.MustCompile("^WIDE[1-7]-[1-7]$")

	assert.Nil(t, cdigipeat_match(cdigiFromChan, pp, "Q1TEST", "Q2TEST", true, alias, cdigiToChan, ""))
}

// With no alias configured there is nothing to match against, and the alias
// pattern must not be looked at at all - it will not have been set up.
func TestCDigipeatMatchNoAlias(t *testing.T) {
	setupCDigipeater(t)

	var pp = AX25FromText("Q3TEST>Q4TEST,Q5TEST:hello", true)
	require.NotNil(t, pp)

	assert.Nil(t, cdigipeat_match(cdigiFromChan, pp, "Q1TEST", "Q2TEST", false, nil, cdigiToChan, ""))
}

// A frame with no digipeater path is not asking to be repeated by anyone.
func TestCDigipeatMatchNoDigipeaters(t *testing.T) {
	setupCDigipeater(t)

	var pp = AX25FromText("Q3TEST>Q4TEST:hello", true)
	require.NotNil(t, pp)

	assert.Nil(t, cdigipeat_match(cdigiFromChan, pp, "Q1TEST", "Q2TEST", false, nil, cdigiToChan, ""))
}

// A path whose every entry has been used has been all the way round already.
func TestCDigipeatMatchPathExhausted(t *testing.T) {
	setupCDigipeater(t)

	var pp = AX25FromText("Q3TEST>Q4TEST,Q1TEST*:hello", true)
	require.NotNil(t, pp)

	assert.Nil(t, cdigipeat_match(cdigiFromChan, pp, "Q1TEST", "Q2TEST", false, nil, cdigiToChan, ""))
}

// CFILTER is how the configuration narrows what a channel pair will repeat,
// and it is consulted before the address is even looked at.
func TestCDigipeatMatchFilterRejects(t *testing.T) {
	setupCDigipeater(t)

	var igateConfig igate_config_s
	pfilter_init(&igateConfig, 0)

	var pp = AX25FromText("Q3TEST>Q4TEST,Q1TEST:hello", true)
	require.NotNil(t, pp)

	assert.Nil(t, cdigipeat_match(cdigiFromChan, pp, "Q1TEST", "Q2TEST", false, nil, cdigiToChan, "b/Q9TEST"))
}

func TestCDigipeatMatchFilterAccepts(t *testing.T) {
	setupCDigipeater(t)

	var igateConfig igate_config_s
	pfilter_init(&igateConfig, 0)

	var pp = AX25FromText("Q3TEST>Q4TEST,Q1TEST:hello", true)
	require.NotNil(t, pp)

	assert.NotNil(t, cdigipeat_match(cdigiFromChan, pp, "Q1TEST", "Q2TEST", false, nil, cdigiToChan, "b/Q3TEST"))
}

// A filter that cannot be understood says so, and nothing is repeated through
// it - a filter that silently passed everything would be worse than none.
func TestCDigipeatMatchFilterError(t *testing.T) {
	setupCDigipeater(t)

	var igateConfig igate_config_s
	pfilter_init(&igateConfig, 0)

	var pp = AX25FromText("Q3TEST>Q4TEST,Q1TEST:hello", true)
	require.NotNil(t, pp)

	var result *packet_t

	var output = CaptureOutput(t, func() {
		result = cdigipeat_match(cdigiFromChan, pp, "Q1TEST", "Q2TEST", false, nil, cdigiToChan, "z/nonsense")
	})

	assert.NotEmpty(t, output, "a filter that could not be understood was not reported")
	assert.Nil(t, result)
}

// Repeating on the channel it came in on is the ordinary case, and the frame
// goes out at high priority - it is somebody's connected session waiting.
func TestCDigipeaterSameChannel(t *testing.T) {
	var _, cdigiConfig = setupCDigipeater(t)

	cdigiConfig.enabled[cdigiFromChan][cdigiFromChan] = true

	var pp = AX25FromText("Q3TEST>Q4TEST,Q1TEST:hello", true)
	require.NotNil(t, pp)

	cdigipeater(cdigiFromChan, pp)

	var sent = tq_remove(cdigiFromChan, TQ_PRIO_0_HI)
	require.NotNil(t, sent, "the repeated frame was not queued for transmission")
	assert.Equal(t, "Q3TEST>Q4TEST,Q1TEST*:", AX25FormatAddrs(sent))

	assert.Equal(t, 1, cdigipeater_get_count(cdigiFromChan, cdigiFromChan))
}

// Cross-band repeating is the reason for the second pass: the callsign put in
// is the one belonging to the channel it goes out on, not the one it came in
// on.
func TestCDigipeaterCrossChannel(t *testing.T) {
	var _, cdigiConfig = setupCDigipeater(t)

	cdigiConfig.enabled[cdigiFromChan][cdigiToChan] = true

	var pp = AX25FromText("Q3TEST>Q4TEST,Q1TEST:hello", true)
	require.NotNil(t, pp)

	cdigipeater(cdigiFromChan, pp)

	assert.Nil(t, tq_remove(cdigiFromChan, TQ_PRIO_0_HI), "it should not have gone out on the channel it arrived on")

	var sent = tq_remove(cdigiToChan, TQ_PRIO_0_HI)
	require.NotNil(t, sent, "the repeated frame was not queued for the other channel")
	assert.Equal(t, "Q3TEST>Q4TEST,Q2TEST*:", AX25FormatAddrs(sent))

	assert.Equal(t, 1, cdigipeater_get_count(cdigiFromChan, cdigiToChan))
}

// A channel pair with no CDIGIPEAT line repeats nothing, however the frame is
// addressed.
func TestCDigipeaterNotEnabled(t *testing.T) {
	setupCDigipeater(t)

	var pp = AX25FromText("Q3TEST>Q4TEST,Q1TEST:hello", true)
	require.NotNil(t, pp)

	cdigipeater(cdigiFromChan, pp)

	for c := range MAX_RADIO_CHANS {
		assert.Nil(t, tq_remove(c, TQ_PRIO_0_HI), "channel %d repeated a frame with digipeating disabled", c)
	}

	assert.Zero(t, cdigipeater_get_count(cdigiFromChan, cdigiFromChan))
}

// Connected mode is only allowed on channels with an internal modem, so
// anything else arriving here is a mistake worth saying out loud.
func TestCDigipeaterInvalidChannel(t *testing.T) {
	var audioConfig, _ = setupCDigipeater(t)

	audioConfig.chan_medium[2] = MEDIUM_IGATE

	var pp = AX25FromText("Q3TEST>Q4TEST,Q1TEST:hello", true)
	require.NotNil(t, pp)

	for _, channel := range []int{-1, 2, MAX_RADIO_CHANS} {
		var output = CaptureOutput(t, func() { cdigipeater(channel, pp) })

		assert.Contains(t, output, "Did not expect to receive on invalid channel")
	}
}

// A network TNC channel carries connected mode too, so it digipeats like a
// radio channel rather than being turned away as invalid.
func TestCDigipeaterNetworkTNCChannel(t *testing.T) {
	var audioConfig, cdigiConfig = setupCDigipeater(t)

	audioConfig.chan_medium[cdigiFromChan] = MEDIUM_NETTNC
	cdigiConfig.enabled[cdigiFromChan][cdigiFromChan] = true

	var pp = AX25FromText("Q3TEST>Q4TEST,Q1TEST:hello", true)
	require.NotNil(t, pp)

	// A network TNC channel has no transmit queue of its own - tq_append
	// hands the frame straight to the TNC - so the count is what says it was
	// repeated rather than turned away.
	CaptureOutput(t, func() { cdigipeater(cdigiFromChan, pp) })

	assert.Equal(t, 1, cdigipeater_get_count(cdigiFromChan, cdigiFromChan),
		"a network TNC channel should digipeat")
}

func TestCDigipeaterInit(t *testing.T) {
	var origAudio, origCDigi = save_audio_config_p, save_cdigi_config_p

	t.Cleanup(func() { save_audio_config_p, save_cdigi_config_p = origAudio, origCDigi })

	var audioConfig = new(audio_s)
	var cdigiConfig = new(cdigi_config_s)

	cdigipeater_init(audioConfig, cdigiConfig)

	assert.Same(t, audioConfig, save_audio_config_p)
	assert.Same(t, cdigiConfig, save_cdigi_config_p)
}
