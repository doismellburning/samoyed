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

			var info GPSInfo

			assert.Equal(t, DWFIX_NOT_INIT, gps.Read(&info))
		})
	}
}

// A GPS that was never started reads the same as one with no receiver, so
// the beacon's config check works before startup has got that far.
func TestDWGPSNilReadsAsNotInitialised(t *testing.T) {
	var gps *GPS

	var info GPSInfo
	info.Lat = maybe.Just(1.0)

	assert.Equal(t, DWFIX_NOT_INIT, gps.Read(&info))
	assert.Equal(t, DWFIX_NOT_INIT, info.Fix)
	assert.True(t, info.Lat.IsNothing(), "a stale position leaked through a nil GPS")

	gps.Term()
}

func TestDWGPSReadReturnsWhatWasSet(t *testing.T) {
	var gps = new(GPS)

	var report = new(GPSInfo)
	report.Timestamp = time.Now()
	report.Fix = DWFIX_3D
	report.Lat = maybe.Just(42.6)
	report.Lon = maybe.Just(-71.3)
	report.Altitude = maybe.Just(33.5)

	gps.setData(report)

	var info GPSInfo

	assert.Equal(t, DWFIX_3D, gps.Read(&info))
	assert.Equal(t, *report, info)
}

// The reader goroutines set while beacons read; run under -race.
func TestDWGPSConcurrentSetAndRead(t *testing.T) {
	var gps = new(GPS)

	var wg sync.WaitGroup

	wg.Go(func() {
		for i := range 1000 {
			var report = new(GPSInfo)
			report.Fix = DWFIX_2D
			report.Lat = maybe.Just(float64(i))
			report.Lon = maybe.Just(float64(i))

			gps.setData(report)
		}
	})

	wg.Go(func() {
		for range 1000 {
			var info GPSInfo

			gps.Read(&info)

			// The two halves of one report always arrive together.
			assert.Equal(t, info.Lat, info.Lon)
		}
	})

	wg.Wait()
}
