// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// Helpers

func makeBeaconModemConfig() *audio_s {
	var cfg = new(audio_s)
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
	var bs = &BeaconService{miscConfig: makeSBConfig()} //nolint:exhaustruct
	var now = time.Now()
	// At 30 MPH (between 5 and 60), rate = (30 * 60) / 30 = 60 seconds
	var lastXmit = now.Add(-120 * time.Second)

	var next = bs.sbCalculateNextTime(now, 30, 90, lastXmit, 90)

	var expected = lastXmit.Add(60 * time.Second)
	assert.Equal(t, expected, next)
}

func Test_sbCalculateNextTime_unknown_speed(t *testing.T) {
	var bs = &BeaconService{miscConfig: makeSBConfig()} //nolint:exhaustruct
	var now = time.Now()
	var lastXmit = now.Add(-2000 * time.Second)

	var next = bs.sbCalculateNextTime(now, G_UNKNOWN, G_UNKNOWN, lastXmit, G_UNKNOWN)

	// Unknown speed: rate = (fast_rate + slow_rate) / 2 = (30 + 1800) / 2 = 915
	var expected = lastXmit.Add(915 * time.Second)
	assert.Equal(t, expected, next)
}

func Test_sbCalculateNextTime_corner_pegging(t *testing.T) {
	var bs = &BeaconService{miscConfig: makeSBConfig()} //nolint:exhaustruct
	var now = time.Now()
	// Last transmitted 20s ago (>= sb_turn_time of 15s)
	var lastXmit = now.Add(-20 * time.Second)

	// Large heading change: 90 degrees > turn_threshold (30 + 255/30 = 38.5)
	var next = bs.sbCalculateNextTime(now, 30, 180, lastXmit, 90)

	assert.Equal(t, now, next, "corner pegging should trigger immediate transmission")
}

func Test_sbCalculateNextTime_corner_pegging_suppressed_too_soon(t *testing.T) {
	var bs = &BeaconService{miscConfig: makeSBConfig()} //nolint:exhaustruct
	var now = time.Now()
	// Last transmitted only 5s ago (< sb_turn_time of 15s), so no corner pegging
	var lastXmit = now.Add(-5 * time.Second)

	var next = bs.sbCalculateNextTime(now, 30, 180, lastXmit, 90)

	// Should NOT be now — should be the normal rate-based next time
	assert.NotEqual(t, now, next)
	assert.True(t, next.After(now))
}

func Test_sbCalculateNextTime_no_corner_peg_below_threshold(t *testing.T) {
	var bs = &BeaconService{miscConfig: makeSBConfig()} //nolint:exhaustruct
	var now = time.Now()
	var lastXmit = now.Add(-20 * time.Second)

	// Heading change of 5 degrees is below threshold (~38.5 at 30 MPH)
	var next = bs.sbCalculateNextTime(now, 30, 95, lastXmit, 90)

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

	var bs = NewBeaconService(modem, cfg, igate)
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
	cfg.beacon[0].lat = G_UNKNOWN
	cfg.beacon[0].lon = G_UNKNOWN

	var bs = NewBeaconService(modem, cfg, igate)
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
	cfg.beacon[0].lat = 42.3601
	cfg.beacon[0].lon = -71.0589

	var bs = NewBeaconService(modem, cfg, igate)
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

	var bs = NewBeaconService(modem, cfg, igate)
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

	var bs = NewBeaconService(modem, cfg, igate)
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

	var bs = NewBeaconService(modem, cfg, igate)
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

	var bs = NewBeaconService(modem, cfg, igate)
	assert.Equal(t, BEACON_IGATE, bs.miscConfig.beacon[0].btype)
}

func Test_NewBeaconService_missing_mycall_is_ignored(t *testing.T) {
	var modem = new(audio_s)
	modem.chan_medium[0] = MEDIUM_RADIO
	// mycall[0] intentionally empty

	var cfg = new(misc_config_s)
	var igate = new(igate_config_s)

	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_POSITION
	cfg.beacon[0].sendto_chan = 0
	cfg.beacon[0].delay = 60
	cfg.beacon[0].every = 600
	cfg.beacon[0].lat = 42.0
	cfg.beacon[0].lon = -71.0

	var bs = NewBeaconService(modem, cfg, igate)
	assert.Equal(t, BEACON_IGNORE, bs.miscConfig.beacon[0].btype)
}

func Test_NewBeaconService_invalid_channel_medium_is_ignored(t *testing.T) {
	var modem = new(audio_s)
	modem.chan_medium[0] = MEDIUM_NONE // not RADIO or NETTNC
	modem.mycall[0] = "Q1TEST"

	var cfg = new(misc_config_s)
	var igate = new(igate_config_s)

	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_POSITION
	cfg.beacon[0].sendto_chan = 0
	cfg.beacon[0].delay = 60
	cfg.beacon[0].every = 600
	cfg.beacon[0].lat = 42.0
	cfg.beacon[0].lon = -71.0

	var bs = NewBeaconService(modem, cfg, igate)
	assert.Equal(t, BEACON_IGNORE, bs.miscConfig.beacon[0].btype)
}

func Test_NewBeaconService_sets_next_time_from_delay(t *testing.T) {
	var modem = makeBeaconModemConfig()
	var cfg = new(misc_config_s)
	var igate = new(igate_config_s)

	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_POSITION
	cfg.beacon[0].sendto_chan = 0
	cfg.beacon[0].delay = 120
	cfg.beacon[0].slot = G_UNKNOWN // disable slot-based scheduling
	cfg.beacon[0].every = 600
	cfg.beacon[0].lat = 42.0
	cfg.beacon[0].lon = -71.0

	var before = time.Now()
	var bs = NewBeaconService(modem, cfg, igate)

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
	cfg.beacon[0].slot = 0
	cfg.beacon[0].every = 7 // 7 is not a divisor of 3600
	cfg.beacon[0].lat = 42.0
	cfg.beacon[0].lon = -71.0

	var bs = NewBeaconService(modem, cfg, igate)

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

	var bs = &BeaconService{miscConfig: cfg} //nolint:exhaustruct
	// If there's no panic, the test passes — goroutine is not started.
	bs.Start()
}

// SetDebug test

func Test_BeaconService_SetDebug(t *testing.T) {
	var bs = &BeaconService{} //nolint:exhaustruct
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
		var bs = &BeaconService{miscConfig: cfg} //nolint:exhaustruct

		// Speed strictly above fast threshold.
		var speed = rapid.Float64Range(float64(cfg.sb_fast_speed)+0.01, 300).Draw(t, "speed")

		// Same course for both — heading_change == 0, so corner pegging never fires.
		var course = rapid.Float64Range(0, 360).Draw(t, "course")

		var lastXmit = time.Now().Add(-time.Duration(rapid.IntRange(cfg.sb_turn_time, 3600).Draw(t, "elapsed")) * time.Second)
		var now = lastXmit.Add(time.Duration(rapid.IntRange(cfg.sb_turn_time, 3600).Draw(t, "sinceXmit")) * time.Second)

		var next = bs.sbCalculateNextTime(now, speed, course, lastXmit, course)
		var expected = lastXmit.Add(time.Duration(cfg.sb_fast_rate) * time.Second)

		assert.Equal(t, expected, next)
	})
}

// Property: at speed strictly below sb_slow_speed with no turn, next time is
// exactly last_xmit + sb_slow_rate.
func Test_sbCalculateNextTime_slow_speed_rate_exact(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var cfg = makeSBConfig()
		var bs = &BeaconService{miscConfig: cfg} //nolint:exhaustruct

		// Speed strictly below slow threshold (but above 1.0 so motion is detected).
		var speed = rapid.Float64Range(1.01, float64(cfg.sb_slow_speed)-0.01).Draw(t, "speed")

		var course = rapid.Float64Range(0, 360).Draw(t, "course")
		var lastXmit = time.Now().Add(-time.Duration(rapid.IntRange(cfg.sb_turn_time, 3600).Draw(t, "elapsed")) * time.Second)
		var now = lastXmit.Add(time.Duration(rapid.IntRange(cfg.sb_turn_time, 3600).Draw(t, "sinceXmit")) * time.Second)

		var next = bs.sbCalculateNextTime(now, speed, course, lastXmit, course)
		var expected = lastXmit.Add(time.Duration(cfg.sb_slow_rate) * time.Second)

		assert.Equal(t, expected, next)
	})
}

// Property: without a corner-peg trigger (same course), next time is always
// within [last_xmit + sb_fast_rate, last_xmit + sb_slow_rate].
func Test_sbCalculateNextTime_result_within_rate_bounds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var cfg = makeSBConfig()
		var bs = &BeaconService{miscConfig: cfg} //nolint:exhaustruct

		var speed = rapid.Float64Range(0, 300).Draw(t, "speed")
		var course = rapid.Float64Range(0, 360).Draw(t, "course")
		var lastXmit = time.Now().Add(-time.Duration(rapid.IntRange(cfg.sb_turn_time, 3600).Draw(t, "elapsed")) * time.Second)
		var now = lastXmit.Add(time.Duration(rapid.IntRange(cfg.sb_turn_time, 3600).Draw(t, "sinceXmit")) * time.Second)

		var next = bs.sbCalculateNextTime(now, speed, course, lastXmit, course)
		var lo = lastXmit.Add(time.Duration(cfg.sb_fast_rate) * time.Second)
		var hi = lastXmit.Add(time.Duration(cfg.sb_slow_rate) * time.Second)

		assert.False(t, next.Before(lo), "next should be >= last_xmit + sb_fast_rate")
		assert.False(t, next.After(hi), "next should be <= last_xmit + sb_slow_rate")
	})
}
