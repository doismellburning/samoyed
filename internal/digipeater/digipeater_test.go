// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package digipeater

// digipeat_match, which decides whether a single frame should be repeated, is
// covered thoroughly by the tests ported from Dire Wolf.  What is here is the
// layer above it: the two passes that put the result on a queue, the count
// each channel pair keeps, and the regeneration option.

import (
	"bytes"
	"regexp"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/doismellburning/samoyed/internal/pfilter"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	digiFromChan = 0
	digiToChan   = 1
)

// sentFrame is a frame a digipeater handed over to be transmitted.
type sentFrame struct {
	channel int
	prio    int
	pp      *ax25.Packet
}

// frameRecorder stands in for the transmit queue, keeping each frame a
// digipeater sends.
type frameRecorder struct {
	sent []sentFrame
}

func (r *frameRecorder) send(channel int, prio int, pp *ax25.Packet) {
	r.sent = append(r.sent, sentFrame{channel: channel, prio: prio, pp: pp})
}

// take removes and returns the first frame sent on the channel at the
// priority, or nil if there is none.
func (r *frameRecorder) take(channel int, prio int) *ax25.Packet {
	for i, f := range r.sent {
		if f.channel == channel && f.prio == prio {
			r.sent = append(r.sent[:i], r.sent[i+1:]...)

			return f.pp
		}
	}

	return nil
}

// setupDigipeater points a new APRS digipeater at two radio channels with our
// callsign on each, and returns what it sends.
func setupDigipeater(t *testing.T) (*Digipeater, *Config, *frameRecorder) {
	t.Helper()

	var ports [phy.MaxTotalChans]Port
	ports[digiFromChan] = Port{MyCall: "Q1TEST", Medium: phy.MediumRadio}
	ports[digiToChan] = Port{MyCall: "Q2TEST", Medium: phy.MediumRadio}

	var digiConfig = new(Config)
	digiConfig.DedupeTime = 30

	var tx = new(frameRecorder)

	return New(ports, digiConfig, new(pfilter.PacketFilter), nil, tx.send), digiConfig, tx
}

// enableDigipeat turns on digipeating from digiFromChan to the given channel,
// with the usual WIDEn-n handling.
func enableDigipeat(digiConfig *Config, to int) {
	digiConfig.Enabled[digiFromChan][to] = true
	digiConfig.Alias[digiFromChan][to] = regexp.MustCompile("^WIDE[4-7]-[1-7]|CITYD$")
	digiConfig.Wide[digiFromChan][to] = regexp.MustCompile("^WIDE[1-7]-[1-7]$")
	digiConfig.Preempt[digiFromChan][to] = PreemptOff
}

// Repeating on the channel it came in on is the ordinary case, and it goes out
// at high priority: every digipeater that heard it is supposed to transmit at
// once, so that one packet time clears the packet out of the area.
func TestDigipeaterSameChannel(t *testing.T) {
	t.Parallel()

	var digi, digiConfig, tx = setupDigipeater(t)

	enableDigipeat(digiConfig, digiFromChan)

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digi.Digipeat(digiFromChan, pp)

	var sent = tx.take(digiFromChan, dwcore.PrioHigh)
	require.NotNil(t, sent, "the repeated frame was not queued for transmission")
	assert.Equal(t, "Q3TEST>APDW17,Q1TEST*,WIDE2-1:", sent.FormatAddrs())

	assert.Equal(t, 1, digi.GetCount(digiFromChan, digiFromChan))
}

// Cross-band repeating is the second pass, and goes out at low priority: the
// other channel's listeners have not heard it yet, so there is no rush.
func TestDigipeaterCrossChannel(t *testing.T) {
	t.Parallel()

	var digi, digiConfig, tx = setupDigipeater(t)

	enableDigipeat(digiConfig, digiToChan)

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digi.Digipeat(digiFromChan, pp)

	assert.Nil(t, tx.take(digiFromChan, dwcore.PrioHigh), "it should not have gone out on the channel it arrived on")

	var sent = tx.take(digiToChan, dwcore.PrioLow)
	require.NotNil(t, sent, "the repeated frame was not queued for the other channel")
	assert.Equal(t, "Q3TEST>APDW17,Q2TEST*,WIDE2-1:", sent.FormatAddrs(),
		"the callsign of the channel it goes out on should have been used")

	assert.Equal(t, 1, digi.GetCount(digiFromChan, digiToChan))
}

// Repeating a frame is what makes it a duplicate of itself: the same frame
// heard again by another route is not repeated a second time.
func TestDigipeaterRemembersWhatItRepeated(t *testing.T) {
	t.Parallel()

	var digi, digiConfig, tx = setupDigipeater(t)

	enableDigipeat(digiConfig, digiFromChan)

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digi.Digipeat(digiFromChan, pp)

	require.NotNil(t, tx.take(digiFromChan, dwcore.PrioHigh))

	assert.True(t, digi.dedupe.Check(digiFromChan, pp),
		"the repeated frame was not remembered, so the next copy of it would go out too")
}

// A channel pair with no DIGIPEAT line repeats nothing, however the frame is
// addressed.
func TestDigipeaterNotEnabled(t *testing.T) {
	t.Parallel()

	var digi, _, tx = setupDigipeater(t)

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digi.Digipeat(digiFromChan, pp)

	assert.Empty(t, tx.sent, "a frame was repeated with digipeating disabled")
}

// A frame that is not asking to be repeated is not repeated, and does not
// count.
func TestDigipeaterNothingToDo(t *testing.T) {
	t.Parallel()

	var digi, digiConfig, tx = setupDigipeater(t)

	enableDigipeat(digiConfig, digiFromChan)

	var pp = ax25.FromText("Q3TEST>APDW17:>hello", true)
	require.NotNil(t, pp)

	digi.Digipeat(digiFromChan, pp)

	assert.Nil(t, tx.take(digiFromChan, dwcore.PrioHigh))
	assert.Zero(t, digi.GetCount(digiFromChan, digiFromChan))
}

// A channel that cannot have been the source of a frame is a mistake worth
// saying out loud.
//
//nolint:paralleltest // Serial: watches logrus's global logger through a test hook.
func TestDigipeaterInvalidChannel(t *testing.T) {
	var digi, _, _ = setupDigipeater(t)

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	for _, channel := range []int{-1, phy.MaxTotalChans} {
		hook.Reset()

		digi.Digipeat(channel, pp)

		var entry = hook.LastEntry()
		require.NotNil(t, entry, "nothing was logged for channel %d", channel)
		assert.Equal(t, logrus.ErrorLevel, entry.Level)
		assert.Contains(t, entry.Message, "Did not expect to receive on invalid channel")
		assert.Equal(t, channel, entry.Data["channel"])
	}
}

// A serial TNC's channel is heard as a radio's is, so what arrives on it can
// be digipeated onto a radio channel.
//
//nolint:paralleltest // Serial: watches logrus's global logger through a test hook.
func TestDigipeaterFromSerialTNC(t *testing.T) {
	const serialChan = phy.MaxRadioChans

	var ports [phy.MaxTotalChans]Port
	ports[digiToChan] = Port{MyCall: "Q2TEST", Medium: phy.MediumRadio}
	ports[serialChan] = Port{MyCall: "Q1TEST", Medium: phy.MediumSerialTNC}

	var digiConfig = new(Config)
	digiConfig.DedupeTime = 30
	digiConfig.Enabled[serialChan][digiToChan] = true
	digiConfig.Alias[serialChan][digiToChan] = regexp.MustCompile("^WIDE[4-7]-[1-7]|CITYD$")
	digiConfig.Wide[serialChan][digiToChan] = regexp.MustCompile("^WIDE[1-7]-[1-7]$")
	digiConfig.Preempt[serialChan][digiToChan] = PreemptOff

	var tx = new(frameRecorder)

	var digi = New(ports, digiConfig, new(pfilter.PacketFilter), nil, tx.send)

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	digi.Digipeat(serialChan, pp)

	for _, entry := range hook.AllEntries() {
		assert.NotContains(t, entry.Message, "Did not expect to receive on invalid channel")
	}

	var sent = tx.take(digiToChan, dwcore.PrioLow)
	require.NotNil(t, sent, "the repeated frame was not queued for transmission")
	assert.Equal(t, "Q3TEST>APDW17,Q2TEST*,WIDE2-1:", sent.FormatAddrs())
}

// Regeneration sends the frame on again exactly as it arrived, rather than
// digipeating it - a separate option, for a separate purpose.
func TestDigiRegen(t *testing.T) {
	t.Parallel()

	var digi, digiConfig, tx = setupDigipeater(t)

	digiConfig.Regen[digiFromChan][digiToChan] = true

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digi.Regen(digiFromChan, pp)

	var sent = tx.take(digiToChan, dwcore.PrioLow)
	require.NotNil(t, sent, "nothing was regenerated")
	assert.Equal(t, "Q3TEST>APDW17,WIDE2-2:", sent.FormatAddrs(),
		"a regenerated frame goes out exactly as it arrived")
}

// Without the option nothing is regenerated.
func TestDigiRegenDisabled(t *testing.T) {
	t.Parallel()

	var digi, _, tx = setupDigipeater(t)

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digi.Regen(digiFromChan, pp)

	assert.Empty(t, tx.sent, "a frame was regenerated with regeneration disabled")
}

func TestNew(t *testing.T) {
	t.Parallel()

	var ports [phy.MaxTotalChans]Port
	ports[digiFromChan] = Port{MyCall: "Q1TEST", Medium: phy.MediumRadio}

	var digiConfig = new(Config)
	digiConfig.DedupeTime = 30

	var filter = new(pfilter.PacketFilter)

	var digi = New(ports, digiConfig, filter, nil, nil)

	assert.Equal(t, ports, digi.ports)
	assert.Same(t, digiConfig, digi.config)
	assert.Same(t, filter, digi.filter)
	require.NotNil(t, digi.dedupe, "the duplicate suppression the digipeater relies on was not set up")
	assert.Equal(t, 30*time.Second, digi.dedupe.TTL())
}

// APRStt object reports reach for the digipeater whether or not startup has
// got that far.
func TestDigipeaterRememberNil(t *testing.T) {
	t.Parallel()

	var pp = ax25.FromText("Q3TEST>APDW17:>hello", true)
	require.NotNil(t, pp)

	var digi *Digipeater

	assert.NotPanics(t, func() { digi.Remember(digiFromChan, pp) })
}

// What an APRStt object report remembers, the digipeater will not repeat.
func TestDigipeaterRemember(t *testing.T) {
	t.Parallel()

	var digi, digiConfig, tx = setupDigipeater(t)

	enableDigipeat(digiConfig, digiFromChan)

	var pp = ax25.FromText("Q3TEST>APDW17,WIDE2-2:>hello", true)
	require.NotNil(t, pp)

	digi.Remember(digiFromChan, pp)
	digi.Digipeat(digiFromChan, pp)

	assert.Nil(t, tx.take(digiFromChan, dwcore.PrioHigh))
	assert.Zero(t, digi.GetCount(digiFromChan, digiFromChan))
}

// A frame already as long as a frame can be has no room for our call, so it
// is repeated without one, as when the via path is already full.  Inserting
// it regardless used to run off the end of the frame buffer.
func TestDigipeaterFullLengthFrame(t *testing.T) {
	t.Parallel()

	var digi, digiConfig, tx = setupDigipeater(t)

	enableDigipeat(digiConfig, digiFromChan)

	var frame = ax25.MustFromText("Q3TEST>APDW17,WIDE2-2:>").FrameData()
	frame = append(frame, bytes.Repeat([]byte{'x'}, ax25.MaxPacketLen-len(frame))...)

	var pp = ax25.FromFrame(frame, ax25.ALevel{Rec: 50, Mark: 50, Space: 50})
	require.NotNil(t, pp)

	digi.Digipeat(digiFromChan, pp)

	var sent = tx.take(digiFromChan, dwcore.PrioHigh)
	require.NotNil(t, sent, "the repeated frame was not queued for transmission")
	assert.Equal(t, "Q3TEST>APDW17,WIDE2-1:", sent.FormatAddrs())
	assert.Len(t, sent.Pack(), ax25.MaxPacketLen)
}
