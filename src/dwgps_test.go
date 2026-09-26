// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
)

// With no GPS receiver to read, Read says so, rather than that one is
// running but has yet to be heard from - TBEACON's config check, for one,
// relies on telling the two apart.
func TestDWGPSReadWithoutAReceiver(t *testing.T) {
	var testCases = map[string]string{
		"none configured": "",
		"can't be opened": filepath.Join(t.TempDir(), "missing"),
	}

	for name, port := range testCases {
		t.Run(name, func(t *testing.T) {
			var config = new(misc_config_s)
			config.gpsnmea_port = port

			var gps = NewGPS(context.Background(), config, 0)

			var info dwgps_info_t

			assert.Equal(t, DWFIX_NOT_INIT, gps.Read(&info))
		})
	}
}

// A GPS that was never started reads the same as one with no receiver, so
// the beacon's config check works before startup has got that far.
func TestDWGPSNilReadsAsNotInitialised(t *testing.T) {
	var gps *GPS

	var info dwgps_info_t
	info.dlat = maybe.Just(1.0)

	assert.Equal(t, DWFIX_NOT_INIT, gps.Read(&info))
	assert.Equal(t, DWFIX_NOT_INIT, info.fix)
	assert.True(t, info.dlat.IsNothing(), "a stale position leaked through a nil GPS")

	gps.Term()
}

func TestDWGPSReadReturnsWhatWasSet(t *testing.T) {
	var gps = new(GPS)

	var report = new(dwgps_info_t)
	report.timestamp = time.Now()
	report.fix = DWFIX_3D
	report.dlat = maybe.Just(42.6)
	report.dlon = maybe.Just(-71.3)
	report.altitude = maybe.Just(33.5)

	gps.setData(report)

	var info dwgps_info_t

	assert.Equal(t, DWFIX_3D, gps.Read(&info))
	assert.Equal(t, *report, info)
}

// The reader goroutines set while beacons read; run under -race.
func TestDWGPSConcurrentSetAndRead(t *testing.T) {
	var gps = new(GPS)

	var wg sync.WaitGroup

	wg.Go(func() {
		for i := range 1000 {
			var report = new(dwgps_info_t)
			report.fix = DWFIX_2D
			report.dlat = maybe.Just(float64(i))
			report.dlon = maybe.Just(float64(i))

			gps.setData(report)
		}
	})

	wg.Go(func() {
		for range 1000 {
			var info dwgps_info_t

			gps.Read(&info)

			// The two halves of one report always arrive together.
			assert.Equal(t, info.dlat, info.dlon)
		}
	})

	wg.Wait()
}
