// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
)

func Test_send_tracker_without_a_position_transmits_nothing(t *testing.T) {
	var cfg = new(misc_config_s)

	cfg.num_beacons = 1
	cfg.beacon[0].btype = BEACON_TRACKER
	cfg.beacon[0].sendto_chan = 0
	cfg.beacon[0].sendto_type = SENDTO_RECV
	cfg.beacon[0].symtab = '/'
	cfg.beacon[0].symbol = '>'

	var bs = &BeaconService{ //nolint:exhaustruct_v5
		modemConfig: makeBeaconModemConfig(),
		miscConfig:  cfg,
		igateConfig: new(igate_config_s),
	}

	var gpsinfo = new(GPSInfo)
	gpsinfo.fix = DWFIX_2D

	for dataLinkQueue.Remove() != nil {
	}

	bs.send(t.Context(), 0, gpsinfo)

	var item = dataLinkQueue.Remove()
	if item != nil {
		t.Errorf("transmitted %s", AX25FormatAddrs(item.pp)+string(AX25GetInfo(item.pp)))
	}
}

// The scheduler decides whether to try again in a couple of seconds by asking
// the same question send does.  When it asked only about the fix, a reading
// with a mode but no position was scheduled for as though a beacon had gone
// out, and under SmartBeaconing that put the next attempt a whole slow_rate
// away.
func Test_trackerPosition_needs_more_than_a_fix(t *testing.T) {
	var gpsinfo = new(GPSInfo)

	gpsinfo.fix = DWFIX_2D
	var _, _, havePosition = trackerPosition(gpsinfo)
	assert.False(t, havePosition, "a fix on its own is not a position")

	gpsinfo.dlat = maybe.Just(42.3601)
	_, _, havePosition = trackerPosition(gpsinfo)
	assert.False(t, havePosition, "half a position is not a position")

	gpsinfo.dlon = maybe.Just(-71.0589)
	var dlat, dlon, complete = trackerPosition(gpsinfo)
	assert.True(t, complete)
	assert.InDelta(t, 42.3601, dlat, 0.000001)
	assert.InDelta(t, -71.0589, dlon, 0.000001)

	gpsinfo.fix = DWFIX_NO_FIX
	_, _, havePosition = trackerPosition(gpsinfo)
	assert.False(t, havePosition, "a stale position without a fix is not a position")
}
