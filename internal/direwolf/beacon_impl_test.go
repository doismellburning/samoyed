// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/aprslog"
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwgps"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// Helpers

func makeBeaconModemConfig() *RadioConfig {
	var cfg = new(RadioConfig)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.mycall[0] = "Q1TEST"

	return cfg
}

func makeBeaconIGateConfig() *igate_config_s {
	var cfg = new(igate_config_s)
	cfg.t2_server_name = "rotate.aprs2.net"
	cfg.t2_login = "Q1TEST"
	cfg.t2_passcode = "12345"

	return cfg
}

func makeSBConfig() *misc_config_s {
	var cfg = new(misc_config_s)
	cfg.sb_configured = true
	cfg.sb_fast_speed = 60  // MPH
	cfg.sb_fast_rate = 30   // seconds
	cfg.sb_slow_speed = 5   // MPH
	cfg.sb_slow_rate = 1800 // seconds
	cfg.sb_turn_time = 15   // seconds
	cfg.sb_turn_angle = 30  // degrees
	cfg.sb_turn_slope = 255 // degrees * MPH

	return cfg
}

// IS_GOOD tests — see property-based Test_IS_GOOD_matches_modulo_oracle below.

// heading_change tests

func Test_heading_change(t *testing.T) {
	var tests = []struct {
		name string
		a, b float64
		want float64
	}{
		{"simple forward", 10, 20, 10},
		{"simple reverse", 20, 10, 10},
		{"wrap around clockwise", 350, 10, 20},
		{"wrap around counter-clockwise", 10, 350, 20},
		{"exactly opposite", 0, 180, 180},
		{"just past opposite", 0, 181, 179},
		{"opposite cardinal directions", 90, 270, 180},
		{"same heading", 45, 45, 0},
		{"zero to zero", 0, 0, 0},
		{"small angle", 359, 1, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.InDelta(t, tt.want, heading_change(tt.a, tt.b), 1e-9)
		})
	}
}

// sbCalculateNextTime tests
// Fast and slow speed exact-rate cases are covered by property tests below.

func Test_sbCalculateNextTime_mid_speed_proportional(t *testing.T) {
	var bs = &BeaconService{miscConfig: makeSBConfig()} //nolint:exhaustruct_v5
	var now = time.Now()
	// At 30 MPH (between 5 and 60), rate = (30 * 60) / 30 = 60 seconds
	var lastXmit = now.Add(-120 * time.Second)

	var next = bs.sbCalculateNextTime(now, maybe.Just(30.0), maybe.Just(90.0), lastXmit, maybe.Just(90.0))

	var expected = lastXmit.Add(60 * time.Second)
	assert.Equal(t, expected, next)
}

func Test_sbCalculateNextTime_unknown_speed(t *testing.T) {
	var bs = &BeaconService{miscConfig: makeSBConfig()} //nolint:exhaustruct_v5
	var now = time.Now()
	var lastXmit = now.Add(-2000 * time.Second)

	var next = bs.sbCalculateNextTime(now, maybe.Nothing[float64](), maybe.Nothing[float64](), lastXmit, maybe.Nothing[float64]())

	// Unknown speed: rate = (fast_rate + slow_rate) / 2 = (30 + 1800) / 2 = 915
	var expected = lastXmit.Add(915 * time.Second)
	assert.Equal(t, expected, next)
}

func Test_sbCalculateNextTime_unknown_course_no_corner_peg(t *testing.T) {
	var bs = &BeaconService{miscConfig: makeSBConfig()} //nolint:exhaustruct_v5
	var now = time.Now()
	var lastXmit = now.Add(-20 * time.Second)

	// Moving, but the GPS reported no course, so there is no turn to detect.
	var next = bs.sbCalculateNextTime(now, maybe.Just(30.0), maybe.Nothing[float64](), lastXmit, maybe.Just(90.0))

	assert.Equal(t, lastXmit.Add(60*time.Second), next)
}

func Test_sbCalculateNextTime_corner_pegging(t *testing.T) {
	var bs = &BeaconService{miscConfig: makeSBConfig()} //nolint:exhaustruct_v5
	var now = time.Now()
	// Last transmitted 20s ago (>= sb_turn_time of 15s)
	var lastXmit = now.Add(-20 * time.Second)

	// Large heading change: 90 degrees > turn_threshold (30 + 255/30 = 38.5)
	var next = bs.sbCalculateNextTime(now, maybe.Just(30.0), maybe.Just(180.0), lastXmit, maybe.Just(90.0))

	assert.Equal(t, now, next, "corner pegging should trigger immediate transmission")
}

func Test_sbCalculateNextTime_corner_pegging_suppressed_too_soon(t *testing.T) {
	var bs = &BeaconService{miscConfig: makeSBConfig()} //nolint:exhaustruct_v5
	var now = time.Now()
	// Last transmitted only 5s ago (< sb_turn_time of 15s), so no corner pegging
	var lastXmit = now.Add(-5 * time.Second)

	var next = bs.sbCalculateNextTime(now, maybe.Just(30.0), maybe.Just(180.0), lastXmit, maybe.Just(90.0))

	// Should NOT be now — should be the normal rate-based next time
	assert.NotEqual(t, now, next)
	assert.True(t, next.After(now))
}

func Test_sbCalculateNextTime_no_corner_peg_below_threshold(t *testing.T) {
	var bs = &BeaconService{miscConfig: makeSBConfig()} //nolint:exhaustruct_v5
	var now = time.Now()
	var lastXmit = now.Add(-20 * time.Second)

	// Heading change of 5 degrees is below threshold (~38.5 at 30 MPH)
	var next = bs.sbCalculateNextTime(now, maybe.Just(30.0), maybe.Just(95.0), lastXmit, maybe.Just(90.0))

	// Should be rate-based, not now
	assert.NotEqual(t, now, next)
}

// NewBeaconService validation tests

func Test_NewBeaconService_obeacon_without_objname_is_ignored(t *testing.T) {
	var modem = makeBeaconModemConfig()
	var cfg = new(misc_config_s)
	var igate = new(igate_config_s)

	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_OBJECT
	cfg.beacon[0].sendto_chan = 0
	cfg.beacon[0].delay = 60
	cfg.beacon[0].every = 600
	// objname intentionally empty

	var bs = NewBeaconService(modem, cfg, igate, nil, nil, nil, nil)
	assert.Equal(t, BEACON_IGNORE, bs.miscConfig.beacon[0].btype)
}

func Test_NewBeaconService_pbeacon_without_lat_lon_is_ignored(t *testing.T) {
	var modem = makeBeaconModemConfig()
	var cfg = new(misc_config_s)
	var igate = new(igate_config_s)

	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_POSITION
	cfg.beacon[0].sendto_chan = 0
	cfg.beacon[0].delay = 60
	cfg.beacon[0].every = 600
	// lat and lon are left unset, which is what the zero value of a Maybe means.

	var bs = NewBeaconService(modem, cfg, igate, nil, nil, nil, nil)
	assert.Equal(t, BEACON_IGNORE, bs.miscConfig.beacon[0].btype)
}

func Test_NewBeaconService_pbeacon_with_valid_lat_lon_not_ignored(t *testing.T) {
	var modem = makeBeaconModemConfig()
	var cfg = new(misc_config_s)
	var igate = new(igate_config_s)

	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_POSITION
	cfg.beacon[0].sendto_chan = 0
	cfg.beacon[0].delay = 60
	cfg.beacon[0].every = 600
	cfg.beacon[0].lat = maybe.Just(42.3601)
	cfg.beacon[0].lon = maybe.Just(-71.0589)

	var bs = NewBeaconService(modem, cfg, igate, nil, nil, nil, nil)
	assert.Equal(t, BEACON_POSITION, bs.miscConfig.beacon[0].btype)
}

func Test_NewBeaconService_cbeacon_without_custom_info_is_ignored(t *testing.T) {
	var modem = makeBeaconModemConfig()
	var cfg = new(misc_config_s)
	var igate = new(igate_config_s)

	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_CUSTOM
	cfg.beacon[0].sendto_chan = 0
	cfg.beacon[0].delay = 60
	cfg.beacon[0].every = 600
	// custom_info and custom_infocmd intentionally empty

	var bs = NewBeaconService(modem, cfg, igate, nil, nil, nil, nil)
	assert.Equal(t, BEACON_IGNORE, bs.miscConfig.beacon[0].btype)
}

func Test_NewBeaconService_cbeacon_with_custom_info_not_ignored(t *testing.T) {
	var modem = makeBeaconModemConfig()
	var cfg = new(misc_config_s)
	var igate = new(igate_config_s)

	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_CUSTOM
	cfg.beacon[0].sendto_chan = 0
	cfg.beacon[0].delay = 60
	cfg.beacon[0].every = 600
	cfg.beacon[0].custom_info = ">Hello from Q1TEST"

	var bs = NewBeaconService(modem, cfg, igate, nil, nil, nil, nil)
	assert.Equal(t, BEACON_CUSTOM, bs.miscConfig.beacon[0].btype)
}

func Test_NewBeaconService_ibeacon_without_igate_config_is_ignored(t *testing.T) {
	var modem = makeBeaconModemConfig()
	var cfg = new(misc_config_s)
	var igate = new(igate_config_s) // empty — no IGate configured

	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_IGATE
	cfg.beacon[0].sendto_chan = 0
	cfg.beacon[0].delay = 60
	cfg.beacon[0].every = 600

	var bs = NewBeaconService(modem, cfg, igate, nil, nil, nil, nil)
	assert.Equal(t, BEACON_IGNORE, bs.miscConfig.beacon[0].btype)
}

func Test_NewBeaconService_ibeacon_with_igate_config_not_ignored(t *testing.T) {
	var modem = makeBeaconModemConfig()
	var cfg = new(misc_config_s)
	var igate = makeBeaconIGateConfig()

	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_IGATE
	cfg.beacon[0].sendto_chan = 0
	cfg.beacon[0].delay = 60
	cfg.beacon[0].every = 600

	var bs = NewBeaconService(modem, cfg, igate, nil, nil, nil, nil)
	assert.Equal(t, BEACON_IGATE, bs.miscConfig.beacon[0].btype)
}

func Test_NewBeaconService_missing_mycall_is_ignored(t *testing.T) {
	var modem = new(RadioConfig)
	modem.chan_medium[0] = MEDIUM_RADIO
	// mycall[0] intentionally empty

	var cfg = new(misc_config_s)
	var igate = new(igate_config_s)

	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_POSITION
	cfg.beacon[0].sendto_chan = 0
	cfg.beacon[0].delay = 60
	cfg.beacon[0].every = 600
	cfg.beacon[0].lat = maybe.Just(42.0)
	cfg.beacon[0].lon = maybe.Just(-71.0)

	var bs = NewBeaconService(modem, cfg, igate, nil, nil, nil, nil)
	assert.Equal(t, BEACON_IGNORE, bs.miscConfig.beacon[0].btype)
}

func Test_NewBeaconService_invalid_channel_medium_is_ignored(t *testing.T) {
	var modem = new(RadioConfig)
	modem.chan_medium[0] = MEDIUM_NONE // not RADIO or NETTNC
	modem.mycall[0] = "Q1TEST"

	var cfg = new(misc_config_s)
	var igate = new(igate_config_s)

	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_POSITION
	cfg.beacon[0].sendto_chan = 0
	cfg.beacon[0].delay = 60
	cfg.beacon[0].every = 600
	cfg.beacon[0].lat = maybe.Just(42.0)
	cfg.beacon[0].lon = maybe.Just(-71.0)

	var bs = NewBeaconService(modem, cfg, igate, nil, nil, nil, nil)
	assert.Equal(t, BEACON_IGNORE, bs.miscConfig.beacon[0].btype)
}

// A beacon can go out on an AXUDP channel, as on a network TNC's.
func Test_NewBeaconService_axudp_channel_not_ignored(t *testing.T) {
	var modem = new(RadioConfig)
	modem.chan_medium[MAX_RADIO_CHANS] = MEDIUM_AXUDP
	modem.mycall[MAX_RADIO_CHANS] = "Q1TEST"

	var cfg = new(misc_config_s)
	var igate = new(igate_config_s)

	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_POSITION
	cfg.beacon[0].sendto_chan = MAX_RADIO_CHANS
	cfg.beacon[0].delay = 60
	cfg.beacon[0].every = 600
	cfg.beacon[0].lat = maybe.Just(42.0)
	cfg.beacon[0].lon = maybe.Just(-71.0)

	var bs = NewBeaconService(modem, cfg, igate, nil, nil, nil, nil)
	assert.Equal(t, BEACON_POSITION, bs.miscConfig.beacon[0].btype)
}

func Test_NewBeaconService_sets_next_time_from_delay(t *testing.T) {
	var modem = makeBeaconModemConfig()
	var cfg = new(misc_config_s)
	var igate = new(igate_config_s)

	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_POSITION
	cfg.beacon[0].sendto_chan = 0
	cfg.beacon[0].delay = 120
	// No slot, so the delay is what schedules it.
	cfg.beacon[0].every = 600
	cfg.beacon[0].lat = maybe.Just(42.0)
	cfg.beacon[0].lon = maybe.Just(-71.0)

	var before = time.Now()
	var bs = NewBeaconService(modem, cfg, igate, nil, nil, nil, nil)

	var next = bs.miscConfig.beacon[0].next
	assert.WithinDuration(t, before.Add(120*time.Second), next, 5*time.Second,
		"next should be approximately 120s after construction")
}

func Test_NewBeaconService_slotted_beacon_adjusts_interval_if_not_IS_GOOD(t *testing.T) {
	var modem = makeBeaconModemConfig()
	var cfg = new(misc_config_s)
	var igate = new(igate_config_s)

	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_POSITION
	cfg.beacon[0].sendto_chan = 0
	cfg.beacon[0].slot = maybe.Just(0)
	cfg.beacon[0].every = 7 // 7 is not a divisor of 3600
	cfg.beacon[0].lat = maybe.Just(42.0)
	cfg.beacon[0].lon = maybe.Just(-71.0)

	var bs = NewBeaconService(modem, cfg, igate, nil, nil, nil, nil)

	// After adjustment, every should be a valid divisor of 3600
	assert.True(t, IS_GOOD(bs.miscConfig.beacon[0].every),
		"slot beacon interval should have been adjusted to a valid divisor of 3600")
}

// Start tests

func Test_BeaconService_Start_no_goroutine_if_all_ignored(t *testing.T) {
	// Directly construct with all beacons set to BEACON_IGNORE; Start should not panic.
	var cfg = new(misc_config_s)
	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_IGNORE

	var bs = &BeaconService{miscConfig: cfg} //nolint:exhaustruct_v5
	// If there's no panic, the test passes — goroutine is not started.
	bs.Start(t.Context())
}

// SetDebug test

func Test_BeaconService_SetDebug(t *testing.T) {
	var bs = &BeaconService{} //nolint:exhaustruct_v5
	bs.SetDebug(2)
	assert.Equal(t, 2, bs.trackerDebugLevel)
}

// Property-based tests

// Property: IS_GOOD(x) iff 3600 is evenly divisible by x, for all x in [1, 3600].
func Test_IS_GOOD_matches_modulo_oracle(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var x = rapid.IntRange(1, 3600).Draw(t, "x")
		assert.Equal(t, 3600%x == 0, IS_GOOD(x))
	})
}

// Property: heading_change result is always in [0, 180].
func Test_heading_change_result_bounded(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var a = rapid.Float64Range(0, 360).Draw(t, "a")
		var b = rapid.Float64Range(0, 360).Draw(t, "b")
		var diff = heading_change(a, b)
		assert.GreaterOrEqual(t, diff, 0.0)
		assert.LessOrEqual(t, diff, 180.0)
	})
}

// Property: heading_change(a, b) == heading_change(b, a) for all a, b.
func Test_heading_change_symmetric(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var a = rapid.Float64Range(0, 360).Draw(t, "a")
		var b = rapid.Float64Range(0, 360).Draw(t, "b")
		assert.InDelta(t, heading_change(a, b), heading_change(b, a), 1e-9)
	})
}

// Property: heading_change(a, a) == 0 for all a.
func Test_heading_change_self_is_zero(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var a = rapid.Float64Range(0, 360).Draw(t, "a")
		assert.InDelta(t, 0.0, heading_change(a, a), 1e-9)
	})
}

// Property: at speed strictly above sb_fast_speed with no turn, next time is
// exactly last_xmit + sb_fast_rate (corner pegging cannot fire when course is unchanged).
func Test_sbCalculateNextTime_fast_speed_rate_exact(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var cfg = makeSBConfig()
		var bs = &BeaconService{miscConfig: cfg} //nolint:exhaustruct_v5

		// Speed strictly above fast threshold.
		var speed = rapid.Float64Range(float64(cfg.sb_fast_speed)+0.01, 300).Draw(t, "speed")

		// Same course for both — heading_change == 0, so corner pegging never fires.
		var course = rapid.Float64Range(0, 360).Draw(t, "course")

		var lastXmit = time.Now().Add(-time.Duration(rapid.IntRange(cfg.sb_turn_time, 3600).Draw(t, "elapsed")) * time.Second)
		var now = lastXmit.Add(time.Duration(rapid.IntRange(cfg.sb_turn_time, 3600).Draw(t, "sinceXmit")) * time.Second)

		var next = bs.sbCalculateNextTime(now, maybe.Just(speed), maybe.Just(course), lastXmit, maybe.Just(course))
		var expected = lastXmit.Add(time.Duration(cfg.sb_fast_rate) * time.Second)

		assert.Equal(t, expected, next)
	})
}

// Property: at speed strictly below sb_slow_speed with no turn, next time is
// exactly last_xmit + sb_slow_rate.
func Test_sbCalculateNextTime_slow_speed_rate_exact(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var cfg = makeSBConfig()
		var bs = &BeaconService{miscConfig: cfg} //nolint:exhaustruct_v5

		// Speed strictly below slow threshold (but above 1.0 so motion is detected).
		var speed = rapid.Float64Range(1.01, float64(cfg.sb_slow_speed)-0.01).Draw(t, "speed")

		var course = rapid.Float64Range(0, 360).Draw(t, "course")
		var lastXmit = time.Now().Add(-time.Duration(rapid.IntRange(cfg.sb_turn_time, 3600).Draw(t, "elapsed")) * time.Second)
		var now = lastXmit.Add(time.Duration(rapid.IntRange(cfg.sb_turn_time, 3600).Draw(t, "sinceXmit")) * time.Second)

		var next = bs.sbCalculateNextTime(now, maybe.Just(speed), maybe.Just(course), lastXmit, maybe.Just(course))
		var expected = lastXmit.Add(time.Duration(cfg.sb_slow_rate) * time.Second)

		assert.Equal(t, expected, next)
	})
}

// Property: without a corner-peg trigger (same course), next time is always
// within [last_xmit + sb_fast_rate, last_xmit + sb_slow_rate].
func Test_sbCalculateNextTime_result_within_rate_bounds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var cfg = makeSBConfig()
		var bs = &BeaconService{miscConfig: cfg} //nolint:exhaustruct_v5

		var speed = rapid.Float64Range(0, 300).Draw(t, "speed")
		var course = rapid.Float64Range(0, 360).Draw(t, "course")
		var lastXmit = time.Now().Add(-time.Duration(rapid.IntRange(cfg.sb_turn_time, 3600).Draw(t, "elapsed")) * time.Second)
		var now = lastXmit.Add(time.Duration(rapid.IntRange(cfg.sb_turn_time, 3600).Draw(t, "sinceXmit")) * time.Second)

		var next = bs.sbCalculateNextTime(now, maybe.Just(speed), maybe.Just(course), lastXmit, maybe.Just(course))
		var lo = lastXmit.Add(time.Duration(cfg.sb_fast_rate) * time.Second)
		var hi = lastXmit.Add(time.Duration(cfg.sb_slow_rate) * time.Second)

		assert.False(t, next.Before(lo), "next should be >= last_xmit + sb_fast_rate")
		assert.False(t, next.After(hi), "next should be <= last_xmit + sb_slow_rate")
	})
}

// send and thread tests
//
// Beacons that go to a radio channel end up in the transmit queue, so that is
// where these look for what was sent.

// setupBeaconTransmitQueue points the transmit queue at the given modem
// configuration and empties it again once the test is done.
func setupBeaconTransmitQueue(t *testing.T, modem *RadioConfig) {
	t.Helper()

	var drain = func() {
		for c := range MAX_RADIO_CHANS {
			for p := range TQ_NUM_PRIO {
				for transmitQueue.Remove(c, p) != nil { //revive:disable-line:empty-block
				}
			}
		}
	}

	transmitQueue.Init(modem)
	drain()
	t.Cleanup(drain)
}

// newSendTestBeaconService is a BeaconService with one beacon, sending to
// radio channel 0 with Q1TEST as its call, and a transmit queue ready to
// catch what it sends.
func newSendTestBeaconService(t *testing.T) *BeaconService {
	t.Helper()

	var modem = makeBeaconModemConfig()
	setupBeaconTransmitQueue(t, modem)

	var cfg = new(misc_config_s)
	cfg.num_beacons = 1
	cfg.beacon[0].sendto_chan = 0
	cfg.beacon[0].sendto_type = SENDTO_XMIT
	cfg.beacon[0].symtab = '/'
	cfg.beacon[0].symbol = '-'
	cfg.beacon[0].every = 3600

	return &BeaconService{ //nolint:exhaustruct_v5
		modemConfig: modem,
		miscConfig:  cfg,
		igateConfig: makeBeaconIGateConfig(),
	}
}

// sentBeacon is the one beacon the test expects in the low priority queue of
// channel 0, formatted as monitor text.
func sentBeacon(t *testing.T) string {
	t.Helper()

	var pp = transmitQueue.Remove(0, TQ_PRIO_1_LO)
	require.NotNil(t, pp, "no beacon was transmitted")

	var more = transmitQueue.Remove(0, TQ_PRIO_1_LO)
	assert.Nil(t, more, "more than one beacon was transmitted")

	return pp.FormatAddrs() + string(pp.Info())
}

func assertNothingSent(t *testing.T) {
	t.Helper()

	for p := range TQ_NUM_PRIO {
		var pp = transmitQueue.Remove(0, p)
		if pp != nil {
			t.Errorf("transmitted %s", pp.FormatAddrs()+string(pp.Info()))
		}
	}
}

// beaconDefaultDest is the destination a beacon without DEST gets: our tocall and version.
func beaconDefaultDest() string {
	return fmt.Sprintf("%s%1d%1d", APP_TOCALL, MAJOR_VERSION, MINOR_VERSION)
}

func Test_BeaconSend_position(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	var bp = &bs.miscConfig.beacon[0]
	bp.btype = BEACON_POSITION
	bp.lat = maybe.Just(42.5)
	bp.lon = maybe.Just(-71.25)
	bp.alt_m = maybe.Just(100.0)
	bp.power = 10
	bp.height = 20
	bp.gain = 3
	bp.comment = "Q1TEST beacon"

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	var got = sentBeacon(t)
	assert.True(t, strings.HasPrefix(got, "Q1TEST>"+beaconDefaultDest()+":!4230.00N/07115.00W-"), got)
	assert.Contains(t, got, "PHG")
	assert.Contains(t, got, "/A=000328")
	assert.True(t, strings.HasSuffix(got, "Q1TEST beacon"), got)
}

func Test_BeaconSend_position_with_explicit_addresses(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	var bp = &bs.miscConfig.beacon[0]
	bp.btype = BEACON_POSITION
	bp.lat = maybe.Just(42.5)
	bp.lon = maybe.Just(-71.25)
	bp.source = "Q2TEST-5"
	bp.dest = "APZQ1T"
	bp.via = "WIDE1-1,WIDE2-1"
	bp.messaging = true

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assert.Equal(t, "Q2TEST-5>APZQ1T,WIDE1-1,WIDE2-1:=4230.00N/07115.00W-", sentBeacon(t))
}

func Test_BeaconSend_position_without_a_position_sends_nothing(t *testing.T) {
	// NewBeaconService refuses such a beacon, so this is only a backstop.
	var bs = newSendTestBeaconService(t)
	bs.miscConfig.beacon[0].btype = BEACON_POSITION

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assertNothingSent(t)
}

func Test_BeaconSend_object(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	var bp = &bs.miscConfig.beacon[0]
	bp.btype = BEACON_OBJECT
	bp.objname = "Q1OBJ"
	bp.lat = maybe.Just(42.5)
	bp.lon = maybe.Just(-71.25)

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	var got = sentBeacon(t)
	assert.True(t, strings.HasPrefix(got, "Q1TEST>"+beaconDefaultDest()+":;Q1OBJ    *"), got)
	assert.Contains(t, got, "4230.00N/07115.00W-")
}

func Test_BeaconSend_object_without_a_position_sends_nothing(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.miscConfig.beacon[0].btype = BEACON_OBJECT
	bs.miscConfig.beacon[0].objname = "Q1OBJ"

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assertNothingSent(t)
}

func Test_BeaconSend_custom_info(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.miscConfig.beacon[0].btype = BEACON_CUSTOM
	bs.miscConfig.beacon[0].custom_info = ">Hello from Q1TEST"

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assert.Equal(t, "Q1TEST>"+beaconDefaultDest()+":>Hello from Q1TEST", sentBeacon(t))
}

func Test_BeaconSend_custom_without_info_sends_nothing(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.miscConfig.beacon[0].btype = BEACON_CUSTOM

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assertNothingSent(t)
}

func Test_BeaconSend_custom_infocmd_failure_sends_nothing(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.miscConfig.beacon[0].btype = BEACON_CUSTOM
	bs.miscConfig.beacon[0].custom_infocmd = "/nonexistent/q1test-infocmd"

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assertNothingSent(t)
}

func Test_BeaconSend_custom_infocmd_output_is_the_info(t *testing.T) {
	// dw_run_cmd runs the command without a shell, so an argument-free command
	// with predictable output is what is needed: "true" prints nothing.
	var truePath, err = exec.LookPath("true")
	if err != nil {
		t.Skip("no true command available")
	}

	var bs = newSendTestBeaconService(t)
	bs.miscConfig.beacon[0].btype = BEACON_CUSTOM
	bs.miscConfig.beacon[0].custom_infocmd = truePath

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assert.Equal(t, "Q1TEST>"+beaconDefaultDest()+":", sentBeacon(t))
}

func Test_BeaconSend_commentcmd_failure_keeps_fixed_comment(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	var bp = &bs.miscConfig.beacon[0]
	bp.btype = BEACON_POSITION
	bp.lat = maybe.Just(42.5)
	bp.lon = maybe.Just(-71.25)
	bp.comment = "fixed"
	bp.commentcmd = "/nonexistent/q1test-commentcmd"

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assert.True(t, strings.HasSuffix(sentBeacon(t), "-fixed"))
}

func Test_BeaconSend_commentcmd_output_is_appended(t *testing.T) {
	var truePath, err = exec.LookPath("true")
	if err != nil {
		t.Skip("no true command available")
	}

	var bs = newSendTestBeaconService(t)
	var bp = &bs.miscConfig.beacon[0]
	bp.btype = BEACON_POSITION
	bp.lat = maybe.Just(42.5)
	bp.lon = maybe.Just(-71.25)
	bp.comment = "fixed"
	bp.commentcmd = truePath

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assert.True(t, strings.HasSuffix(sentBeacon(t), "-fixed"))
}

// heardCounterByHops counts however many stations it was told it had heard
// within each hop limit.
type heardCounterByHops map[int]int

func (c heardCounterByHops) Count(maxHops int, _ int) int {
	return c[maxHops]
}

func Test_BeaconSend_igate_status(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.miscConfig.beacon[0].btype = BEACON_IGATE

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assert.Equal(t, "Q1TEST>"+beaconDefaultDest()+
		":<IGATE,MSG_CNT=0,PKT_CNT=0,DIR_CNT=0,LOC_CNT=0,RF_CNT=0,UPL_CNT=0,DNL_CNT=0",
		sentBeacon(t))
}

// The stations heard directly, within IGTXVIA's hop count and within the
// most hops of all are counted separately.
func Test_BeaconSend_igate_status_counts_stations_heard(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.miscConfig.beacon[0].btype = BEACON_IGATE
	bs.igateConfig.max_digi_hops = 2
	bs.heardCounter = heardCounterByHops{0: 3, 2: 5, 8: 7}

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assert.Equal(t, "Q1TEST>"+beaconDefaultDest()+
		":<IGATE,MSG_CNT=0,PKT_CNT=0,DIR_CNT=3,LOC_CNT=5,RF_CNT=7,UPL_CNT=0,DNL_CNT=0",
		sentBeacon(t))
}

func Test_BeaconSend_tracker_with_a_3D_fix(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.logger = new(aprslog.Logger) // No path, so it writes nothing.
	bs.SetDebug(3)

	var bp = &bs.miscConfig.beacon[0]
	bp.btype = BEACON_TRACKER
	bp.symbol = '>'
	bp.alt_m = maybe.Just(1.0) // Any positive altitude asks for the GPS one.

	var gpsinfo = new(dwgps.GPSInfo)
	gpsinfo.Fix = dwgps.DWFIX_3D
	gpsinfo.Lat = maybe.Just(42.5)
	gpsinfo.Lon = maybe.Just(-71.25)
	gpsinfo.Altitude = maybe.Just(100.0)
	gpsinfo.Track = maybe.Just(90.4)
	gpsinfo.SpeedKnots = maybe.Just(10.6)

	bs.send(t.Context(), 0, gpsinfo)

	var got = sentBeacon(t)
	assert.True(t, strings.HasPrefix(got, "Q1TEST>"+beaconDefaultDest()+":!4230.00N/07115.00W>090/011"), got)
	assert.Contains(t, got, "/A=000328")
}

// "-dttt" with no logger to write to still sends the beacon, rather than
// tripping over the missing logger.
func Test_BeaconSend_tracker_debug_without_a_logger(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.SetDebug(3)

	var bp = &bs.miscConfig.beacon[0]
	bp.btype = BEACON_TRACKER
	bp.symbol = '>'

	var gpsinfo = new(dwgps.GPSInfo)
	gpsinfo.Fix = dwgps.DWFIX_2D
	gpsinfo.Lat = maybe.Just(42.5)
	gpsinfo.Lon = maybe.Just(-71.25)

	bs.send(t.Context(), 0, gpsinfo)

	assert.True(t, strings.HasPrefix(sentBeacon(t), "Q1TEST>"+beaconDefaultDest()+":!4230.00N/07115.00W>"))
}

func Test_BeaconSend_tracker_with_a_2D_fix_has_no_altitude(t *testing.T) {
	var bs = newSendTestBeaconService(t)

	var bp = &bs.miscConfig.beacon[0]
	bp.btype = BEACON_TRACKER
	bp.symbol = '>'
	bp.alt_m = maybe.Just(1.0)

	var gpsinfo = new(dwgps.GPSInfo)
	gpsinfo.Fix = dwgps.DWFIX_2D
	gpsinfo.Lat = maybe.Just(42.5)
	gpsinfo.Lon = maybe.Just(-71.25)
	gpsinfo.Altitude = maybe.Just(100.0)

	bs.send(t.Context(), 0, gpsinfo)

	var got = sentBeacon(t)
	assert.True(t, strings.HasPrefix(got, "Q1TEST>"+beaconDefaultDest()+":!4230.00N/07115.00W>"), got)
	assert.NotContains(t, got, "/A=")
}

func Test_BeaconSend_no_channel_sends_nothing(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.miscConfig.beacon[0].btype = BEACON_CUSTOM
	bs.miscConfig.beacon[0].custom_info = ">Hello"
	bs.miscConfig.beacon[0].sendto_chan = -1

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assertNothingSent(t)
}

func Test_BeaconSend_no_mycall_sends_nothing(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.modemConfig.mycall[0] = ""
	bs.miscConfig.beacon[0].btype = BEACON_CUSTOM
	bs.miscConfig.beacon[0].custom_info = ">Hello"

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assertNothingSent(t)
}

func Test_BeaconSend_unparseable_packet_sends_nothing(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.miscConfig.beacon[0].btype = BEACON_CUSTOM
	bs.miscConfig.beacon[0].custom_info = ">Hello"
	bs.miscConfig.beacon[0].dest = "NOT A VALID CALL"

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assertNothingSent(t)
}

func Test_BeaconSend_ichannel_uses_channel_0_call(t *testing.T) {
	// An ICHANNEL beacon takes the call of channel 0.  The transmit queue
	// hands it to the IGate, which silently drops it when not connected, so
	// the only thing to check is that no radio channel was sent it.
	var bs = newSendTestBeaconService(t)
	bs.modemConfig.chan_medium[1] = MEDIUM_IGATE
	bs.miscConfig.beacon[0].btype = BEACON_CUSTOM
	bs.miscConfig.beacon[0].custom_info = ">Hello"
	bs.miscConfig.beacon[0].sendto_chan = 1

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assertNothingSent(t)
	assert.Nil(t, transmitQueue.Remove(1, TQ_PRIO_1_LO))
}

// recordingBeaconIGate is an IGate that reports fixed counts and records the
// packets it is given.
type recordingBeaconIGate struct {
	sent []string
}

func (r *recordingBeaconIGate) sendRecPacket(channel int, pp *ax25.Packet) {
	r.sent = append(r.sent, fmt.Sprintf("%d %s%s", channel, pp.FormatAddrs(), pp.Info()))
}

func (*recordingBeaconIGate) msgCount() int      { return 1 }
func (*recordingBeaconIGate) pktCount() int      { return 2 }
func (*recordingBeaconIGate) uplinkCount() int   { return 3 }
func (*recordingBeaconIGate) downlinkCount() int { return 4 }

func Test_BeaconSend_to_igate_bypasses_transmit_queue(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.miscConfig.beacon[0].btype = BEACON_CUSTOM
	bs.miscConfig.beacon[0].custom_info = ">Hello"
	bs.miscConfig.beacon[0].sendto_type = SENDTO_IGATE

	var ig = new(recordingBeaconIGate)
	bs.igate = ig

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assertNothingSent(t)
	assert.Equal(t, []string{"-1 Q1TEST>" + beaconDefaultDest() + ":>Hello"}, ig.sent, "on its way to APRS-IS, channel -1 to skip RF>IS filtering")
}

// The IGate statistics beacon reports the counts of the IGate it was given.
func Test_BeaconSend_igate_status_reports_the_igates_counts(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.miscConfig.beacon[0].btype = BEACON_IGATE
	bs.igate = new(recordingBeaconIGate)

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assert.Equal(t, "Q1TEST>"+beaconDefaultDest()+
		":<IGATE,MSG_CNT=1,PKT_CNT=2,DIR_CNT=0,LOC_CNT=0,RF_CNT=0,UPL_CNT=3,DNL_CNT=4",
		sentBeacon(t))
}

func Test_BeaconSend_to_recv_is_simulated_reception(t *testing.T) {
	for dataLinkQueue.Remove() != nil { //revive:disable-line:empty-block
	}

	var bs = newSendTestBeaconService(t)
	bs.miscConfig.beacon[0].btype = BEACON_CUSTOM
	bs.miscConfig.beacon[0].custom_info = ">Hello"
	bs.miscConfig.beacon[0].sendto_type = SENDTO_RECV

	bs.send(t.Context(), 0, new(dwgps.GPSInfo))

	assertNothingSent(t)

	var item = dataLinkQueue.Remove()
	require.NotNil(t, item)
	assert.Equal(t, ">Hello", string(item.pp.Info()))
	assert.Nil(t, dataLinkQueue.Remove())
}

// runBeaconThread runs bs.thread until the test calls the returned stop, which
// waits for the thread to have returned.  bs must not be touched while the
// thread runs.
func runBeaconThread(t *testing.T, bs *BeaconService) func() {
	t.Helper()

	var ctx, cancel = context.WithCancel(t.Context())
	var done = make(chan struct{})

	go func() {
		defer close(done)

		bs.thread(ctx)
	}()

	var stop = func() {
		cancel()
		<-done
	}

	t.Cleanup(stop)

	return stop
}

// waitForBeacon polls the transmit queue of channel 0 for a beacon.
func waitForBeacon(t *testing.T) string {
	t.Helper()

	var deadline = time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		var pp = transmitQueue.Remove(0, TQ_PRIO_1_LO)
		if pp != nil {
			return pp.FormatAddrs() + string(pp.Info())
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatal("no beacon was transmitted")

	return ""
}

// runBeaconThreadUntilRescheduled runs bs.thread until beacon 0 is scheduled
// for later than first.  Nothing is sent to wait for, and the schedule can
// be read only while the thread is stopped, so it is run in short bursts.
func runBeaconThreadUntilRescheduled(t *testing.T, bs *BeaconService, first time.Time) {
	t.Helper()

	var deadline = time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		var stop = runBeaconThread(t, bs)

		time.Sleep(10 * time.Millisecond)
		stop()

		if bs.miscConfig.beacon[0].next.After(first) {
			return
		}
	}

	t.Fatal("beacon was never rescheduled")
}

func Test_BeaconThread_sends_a_due_beacon_then_waits(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	var bp = &bs.miscConfig.beacon[0]
	bp.btype = BEACON_CUSTOM
	bp.custom_info = ">Hello"
	// Well overdue, as though the clock had jumped forward: the schedule is
	// rebuilt from now rather than sending every missed beacon.
	bp.next = time.Now().Add(-2 * time.Hour)
	bp.every = 60

	// An ignored beacon is left alone.
	bs.miscConfig.num_beacons = 2
	bs.miscConfig.beacon[1].btype = BEACON_IGNORE

	var start = time.Now()
	var stop = runBeaconThread(t, bs)

	assert.Equal(t, "Q1TEST>"+beaconDefaultDest()+":>Hello", waitForBeacon(t))

	stop()

	assertNothingSent(t)
	assert.WithinDuration(t, start.Add(60*time.Second), bp.next, 5*time.Second)
}

func Test_BeaconThread_fixed_rate_tracker(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.SetDebug(1)

	var gpsinfo = new(dwgps.GPSInfo)
	gpsinfo.Fix = dwgps.DWFIX_3D
	gpsinfo.Lat = maybe.Just(42.5)
	gpsinfo.Lon = maybe.Just(-71.25)
	gpsinfo.Altitude = maybe.Just(100.0)
	gpsinfo.Track = maybe.Just(90.0)
	gpsinfo.SpeedKnots = maybe.Just(10.0)

	bs.gps = new(dwgps.GPS)
	bs.gps.SetData(gpsinfo)

	var bp = &bs.miscConfig.beacon[0]
	bp.btype = BEACON_TRACKER
	bp.symbol = '>'

	var first = time.Now()
	bp.next = first

	var stop = runBeaconThread(t, bs)

	assert.Contains(t, waitForBeacon(t), ":!4230.00N/07115.00W>090/010")

	stop()

	assert.Equal(t, first.Add(3600*time.Second), bp.next)
}

func Test_BeaconThread_fixed_rate_tracker_without_position_keeps_schedule(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.SetDebug(1)

	var gpsinfo = new(dwgps.GPSInfo)
	gpsinfo.Fix = dwgps.DWFIX_NO_FIX

	bs.gps = new(dwgps.GPS)
	bs.gps.SetData(gpsinfo)

	var bp = &bs.miscConfig.beacon[0]
	bp.btype = BEACON_TRACKER

	var first = time.Now()
	bp.next = first

	runBeaconThreadUntilRescheduled(t, bs, first)

	assertNothingSent(t)
	assert.Equal(t, first.Add(3600*time.Second), bp.next)
}

func Test_BeaconThread_smartbeaconing_tracker(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.SetDebug(2)

	var sb = makeSBConfig()
	bs.miscConfig.sb_configured = sb.sb_configured
	bs.miscConfig.sb_fast_speed = sb.sb_fast_speed
	bs.miscConfig.sb_fast_rate = sb.sb_fast_rate
	bs.miscConfig.sb_slow_speed = sb.sb_slow_speed
	bs.miscConfig.sb_slow_rate = sb.sb_slow_rate
	bs.miscConfig.sb_turn_time = sb.sb_turn_time
	bs.miscConfig.sb_turn_angle = sb.sb_turn_angle
	bs.miscConfig.sb_turn_slope = sb.sb_turn_slope

	var gpsinfo = new(dwgps.GPSInfo)
	gpsinfo.Fix = dwgps.DWFIX_2D
	gpsinfo.Lat = maybe.Just(42.5)
	gpsinfo.Lon = maybe.Just(-71.25)
	gpsinfo.Track = maybe.Just(180.0)
	gpsinfo.SpeedKnots = maybe.Just(100.0) // Faster than sb_fast_speed.

	bs.gps = new(dwgps.GPS)
	bs.gps.SetData(gpsinfo)

	var bp = &bs.miscConfig.beacon[0]
	bp.btype = BEACON_TRACKER
	bp.symbol = '>'
	bp.next = time.Now()

	var start = time.Now()
	var stop = runBeaconThread(t, bs)

	assert.Contains(t, waitForBeacon(t), ":!4230.00N/07115.00W>180/100")

	stop()

	assert.WithinDuration(t, start.Add(time.Duration(sb.sb_fast_rate)*time.Second), bp.next, 5*time.Second)
}

func Test_BeaconThread_smartbeaconing_tracker_without_position_retries_soon(t *testing.T) {
	var bs = newSendTestBeaconService(t)

	bs.miscConfig.sb_configured = true
	bs.miscConfig.sb_fast_rate = 30
	bs.miscConfig.sb_turn_time = 15

	var gpsinfo = new(dwgps.GPSInfo)
	gpsinfo.Fix = dwgps.DWFIX_2D // A mode, but never a position.

	bs.gps = new(dwgps.GPS)
	bs.gps.SetData(gpsinfo)

	var bp = &bs.miscConfig.beacon[0]
	bp.btype = BEACON_TRACKER

	var first = time.Now()
	bp.next = first

	runBeaconThreadUntilRescheduled(t, bs, first)

	assertNothingSent(t)
	assert.WithinDuration(t, first.Add(2*time.Second), bp.next, time.Second)
}

func Test_BeaconThread_returns_when_cancelled_before_anything_is_due(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.miscConfig.beacon[0].btype = BEACON_CUSTOM
	bs.miscConfig.beacon[0].custom_info = ">Hello"
	bs.miscConfig.beacon[0].next = time.Now().Add(time.Hour)

	var stop = runBeaconThread(t, bs)

	stop()

	assertNothingSent(t)
}

func Test_BeaconService_Start_runs_thread(t *testing.T) {
	var bs = newSendTestBeaconService(t)
	bs.miscConfig.beacon[0].btype = BEACON_CUSTOM
	bs.miscConfig.beacon[0].custom_info = ">Hello"
	bs.miscConfig.beacon[0].next = time.Now()

	var ctx, cancel = context.WithCancel(t.Context())
	defer cancel()

	bs.Start(ctx)

	// Once this has gone out the thread sleeps until the next one, an hour
	// away, and returns on cancellation without touching anything else.
	assert.Equal(t, "Q1TEST>"+beaconDefaultDest()+":>Hello", waitForBeacon(t))
}

// More NewBeaconService validation tests

func Test_NewBeaconService_tbeacon_without_gps_is_ignored(t *testing.T) {
	var cfg = new(misc_config_s)
	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_TRACKER
	cfg.beacon[0].every = 600

	var bs = NewBeaconService(makeBeaconModemConfig(), cfg, new(igate_config_s), nil, nil, nil, nil)
	assert.Equal(t, BEACON_IGNORE, bs.miscConfig.beacon[0].btype)
}

func Test_NewBeaconService_tbeacon_with_gps_not_ignored(t *testing.T) {
	var gps = new(dwgps.GPS)
	var gpsinfo = new(dwgps.GPSInfo)
	gpsinfo.Fix = dwgps.DWFIX_NO_FIX
	gps.SetData(gpsinfo)

	var cfg = new(misc_config_s)
	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_TRACKER
	cfg.beacon[0].every = 600

	var bs = NewBeaconService(makeBeaconModemConfig(), cfg, new(igate_config_s), gps, nil, nil, nil)
	assert.Equal(t, BEACON_TRACKER, bs.miscConfig.beacon[0].btype)
}

func Test_NewBeaconService_info_on_non_custom_beacons_is_only_complained_about(t *testing.T) {
	var gps = new(dwgps.GPS)
	var gpsinfo = new(dwgps.GPSInfo)
	gpsinfo.Fix = dwgps.DWFIX_NO_FIX
	gps.SetData(gpsinfo)

	var cfg = new(misc_config_s)
	cfg.num_beacons = 2
	cfg.beacon[0].btype = BEACON_POSITION
	cfg.beacon[0].lat = maybe.Just(42.0)
	cfg.beacon[0].lon = maybe.Just(-71.0)
	cfg.beacon[0].custom_info = ">Hello"
	cfg.beacon[1].btype = BEACON_TRACKER
	cfg.beacon[1].custom_infocmd = "q1test-cmd"

	var bs = NewBeaconService(makeBeaconModemConfig(), cfg, new(igate_config_s), gps, nil, nil, nil)
	assert.Equal(t, BEACON_POSITION, bs.miscConfig.beacon[0].btype)
	assert.Equal(t, BEACON_TRACKER, bs.miscConfig.beacon[1].btype)
}

func Test_NewBeaconService_obeacon_with_objname_and_position_not_ignored(t *testing.T) {
	var cfg = new(misc_config_s)
	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_OBJECT
	cfg.beacon[0].objname = "Q1OBJ"
	cfg.beacon[0].lat = maybe.Just(42.0)
	cfg.beacon[0].lon = maybe.Just(-71.0)

	var bs = NewBeaconService(makeBeaconModemConfig(), cfg, new(igate_config_s), nil, nil, nil, nil)
	assert.Equal(t, BEACON_OBJECT, bs.miscConfig.beacon[0].btype)
}

func Test_NewBeaconService_out_of_range_channels_use_channel_0_call(t *testing.T) {
	var cfg = new(misc_config_s)
	cfg.num_beacons = 3
	cfg.beacon[0].btype = BEACON_CUSTOM
	cfg.beacon[0].custom_info = ">Hello"
	cfg.beacon[0].sendto_chan = -1
	cfg.beacon[1].btype = BEACON_CUSTOM
	cfg.beacon[1].custom_info = ">Hello"
	cfg.beacon[1].sendto_chan = MAX_TOTAL_CHANS
	cfg.beacon[2].btype = BEACON_IGNORE

	var bs = NewBeaconService(makeBeaconModemConfig(), cfg, new(igate_config_s), nil, nil, nil, nil)
	assert.Equal(t, BEACON_CUSTOM, bs.miscConfig.beacon[0].btype)
	assert.Equal(t, BEACON_CUSTOM, bs.miscConfig.beacon[1].btype)
	assert.Equal(t, BEACON_IGNORE, bs.miscConfig.beacon[2].btype)
}

func Test_NewBeaconService_slotted_beacon_schedule(t *testing.T) {
	var tests = []struct {
		name      string
		every     int
		wantEvery int
	}{
		{"good interval kept", 600, 600},
		{"adjusted up", 7, 8},
		{"too long is capped at an hour", 7200, 3600},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg = new(misc_config_s)
			cfg.num_beacons = 1
			cfg.beacon[0].btype = BEACON_CUSTOM
			cfg.beacon[0].custom_info = ">Hello"
			cfg.beacon[0].slot = maybe.Just(30)
			cfg.beacon[0].every = tt.every

			var before = time.Now()
			var bs = NewBeaconService(makeBeaconModemConfig(), cfg, new(igate_config_s), nil, nil, nil, nil)
			var bp = bs.miscConfig.beacon[0]

			assert.Equal(t, tt.wantEvery, bp.every)
			assert.GreaterOrEqual(t, bp.delay, 5)
			assert.LessOrEqual(t, bp.delay, bp.every+5)
			assert.WithinDuration(t, before.Add(time.Duration(bp.delay)*time.Second), bp.next, 2*time.Second)
		})
	}
}
