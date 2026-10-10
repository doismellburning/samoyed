// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package audiostats

import (
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestAudioStats gives each audio device a fresh Stats, each of them
// reporting known audio levels, so a test starts from a known position and
// leaves nothing behind.
func newTestAudioStats(t *testing.T) *[phy.MaxADevs]Stats {
	t.Helper()

	var stats = new([phy.MaxADevs]Stats)

	for adev := range stats {
		stats[adev].AudioLevel = audioStatsTestLevels
	}

	return stats
}

// audioStatsTestLevel is the receive audio level a test rigs channel ch to
// report.  They differ per channel so a report says which channel it is about.
func audioStatsTestLevel(ch int) int {
	return 10 * (ch + 1)
}

// audioStatsTestLevels reports audioStatsTestLevel for each channel.
func audioStatsTestLevels(channel int, _ int) ax25.ALevel {
	var alevel ax25.ALevel
	alevel.Rec = audioStatsTestLevel(channel)

	return alevel
}

// audioStatsTestInterval is the reporting interval, in seconds, that the tests
// below run the statistics at.
const audioStatsTestInterval = 10

// audioStatsRewind moves a device's last report one interval into the past, so
// the next call reaches the reporting branch without the test waiting for real
// time to pass.
func audioStatsRewind(s *Stats) {
	s.lastTime = s.lastTime.Add(-audioStatsTestInterval * time.Second)
}

// audioStatsPastFirstReport runs a device up to the point where the next
// elapsed interval will actually print: started, and with the deliberately
// suppressed first report out of the way.
func audioStatsPastFirstReport(t *testing.T, stats *[phy.MaxADevs]Stats, adev int, nchan int) {
	t.Helper()

	stats[adev].Record(adev, nchan, 1, audioStatsTestInterval)
	audioStatsRewind(&stats[adev])
	stats[adev].Record(adev, nchan, 1, audioStatsTestInterval)

	assert.False(t, stats[adev].suppressFirst)
}

// audioStatsReports runs f and returns the statistics reports it logged.
func audioStatsReports(t *testing.T, f func()) []*logrus.Entry {
	t.Helper()

	testutils.DiscardLogrus(t)

	var hook = test.NewGlobal()
	t.Cleanup(hook.Reset)

	f()

	return hook.AllEntries()
}

// requireOneAudioStatsReport runs f and returns the one statistics report it
// should have logged.
func requireOneAudioStatsReport(t *testing.T, f func()) *logrus.Entry {
	t.Helper()

	var reports = audioStatsReports(t, f)
	require.Len(t, reports, 1)

	return reports[0]
}

// assertAudioStatsReport checks that report is the statistics for device
// adev: its sample rate in thousands of samples a second, its error count, and
// each of its channels' audio levels.
func assertAudioStatsReport(t *testing.T, report *logrus.Entry, adev int, rateKHz float64, errors int, levels map[int]int) {
	t.Helper()

	assert.Equal(t, logrus.InfoLevel, report.Level)
	assert.Equal(t, "Audio input statistics", report.Message)
	assert.Equal(t, adev, report.Data["adevice"])
	assert.InDelta(t, rateKHz, report.Data["sample_rate_khz"], 0.001)
	assert.Equal(t, errors, report.Data["errors"])
	assert.Equal(t, levels, report.Data["audio_levels"])
}

// An interval of 0 is how the statistics are turned off, and nothing should be
// collected in that case, never mind printed.
func TestAudioStatsIntervalOffDoesNothing(t *testing.T) {
	var stats = newTestAudioStats(t)

	var reports = audioStatsReports(t, func() {
		stats[0].Record(0, 1, 44100, 0)
		stats[0].Record(0, 1, 44100, -1)
	})

	assert.Empty(t, reports)
	assert.True(t, stats[0].lastTime.IsZero(), "collection should not have started")
}

// The first call only starts the clock - and shortens the first collection
// period to 3 seconds, so there isn't a long silence before the first report.
func TestAudioStatsFirstCallStartsCollecting(t *testing.T) {
	var stats = newTestAudioStats(t)

	var reports = audioStatsReports(t, func() {
		stats[0].Record(0, 1, 44100, 100)
	})

	assert.Empty(t, reports)
	assert.False(t, stats[0].lastTime.IsZero())
	assert.True(t, stats[0].suppressFirst)
	assert.Zero(t, stats[0].sampleCount, "the starting call's samples are not counted")
	assert.Zero(t, stats[0].errorCount)

	var due = stats[0].lastTime.Add(100 * time.Second)
	assert.WithinDuration(t, time.Now().Add(3*time.Second), due, time.Second)
}

// A read that returned samples counts as samples; one that didn't counts as an
// error.
func TestAudioStatsCountsSamplesAndErrors(t *testing.T) {
	var stats = newTestAudioStats(t)

	var reports = audioStatsReports(t, func() {
		stats[0].Record(0, 1, 0, 100) // Starts collecting.
		stats[0].Record(0, 1, 1000, 100)
		stats[0].Record(0, 1, 500, 100)
		stats[0].Record(0, 1, 0, 100)
		stats[0].Record(0, 1, -1, 100)
	})

	assert.Empty(t, reports, "the interval has not elapsed")
	assert.Equal(t, 1500, stats[0].sampleCount)
	assert.Equal(t, 2, stats[0].errorCount)
}

// The first report would be measured over a period that didn't start on a
// second boundary, so its rate would be noticeably wrong.  It is dropped.
func TestAudioStatsSuppressesFirstReport(t *testing.T) {
	var stats = newTestAudioStats(t)

	stats[0].Record(0, 1, 44100, audioStatsTestInterval)
	audioStatsRewind(&stats[0])

	var reports = audioStatsReports(t, func() {
		stats[0].Record(0, 1, 44100, audioStatsTestInterval)
	})

	assert.Empty(t, reports)
	assert.False(t, stats[0].suppressFirst, "only the first one is suppressed")
	assert.Zero(t, stats[0].sampleCount, "counters restart for the next interval")
	assert.Zero(t, stats[0].errorCount)
}

func TestAudioStatsReportsSampleRate(t *testing.T) {
	var stats = newTestAudioStats(t)
	audioStatsPastFirstReport(t, stats, 0, 1)

	audioStatsRewind(&stats[0])

	var report = requireOneAudioStatsReport(t, func() {
		stats[0].Record(0, 1, 441000, audioStatsTestInterval)
	})

	assertAudioStatsReport(t, report, 0, 44.1, 0, map[int]int{0: 10})

	assert.Zero(t, stats[0].sampleCount, "counters restart after a report")
}

func TestAudioStatsReportsErrorCount(t *testing.T) {
	var stats = newTestAudioStats(t)
	audioStatsPastFirstReport(t, stats, 0, 1)

	stats[0].Record(0, 1, 0, audioStatsTestInterval)
	stats[0].Record(0, 1, 0, audioStatsTestInterval)
	audioStatsRewind(&stats[0])

	var report = requireOneAudioStatsReport(t, func() {
		stats[0].Record(0, 1, 0, audioStatsTestInterval)
	})

	assertAudioStatsReport(t, report, 0, 0.0, 3, map[int]int{0: 10})

	assert.Zero(t, stats[0].errorCount, "counters restart after a report")
}

// A stereo device reports a level per channel.
func TestAudioStatsReportsBothChannels(t *testing.T) {
	var stats = newTestAudioStats(t)
	audioStatsPastFirstReport(t, stats, 0, 2)

	audioStatsRewind(&stats[0])

	var report = requireOneAudioStatsReport(t, func() {
		stats[0].Record(0, 2, 441000, audioStatsTestInterval)
	})

	assertAudioStatsReport(t, report, 0, 44.1, 0, map[int]int{0: 10, 1: 20})
}

// Each device keeps its own counters, and reports its own channel numbers.
func TestAudioStatsSecondDeviceIsIndependent(t *testing.T) {
	var stats = newTestAudioStats(t)
	audioStatsPastFirstReport(t, stats, 1, 1)

	stats[0].Record(0, 1, 44100, audioStatsTestInterval) // Device 0 only just starts collecting.
	audioStatsRewind(&stats[1])

	var report = requireOneAudioStatsReport(t, func() {
		stats[1].Record(1, 1, 220500, audioStatsTestInterval)
	})

	assertAudioStatsReport(t, report, 1, 22.05, 0, map[int]int{2: 30})

	assert.True(t, stats[0].suppressFirst, "device 0 should be untouched")
}

func TestAudioStatsRejectsDeviceOutOfRange(t *testing.T) {
	var stats = newTestAudioStats(t)

	assert.Panics(t, func() { stats[0].Record(phy.MaxADevs, 1, 44100, audioStatsTestInterval) })
	assert.Panics(t, func() { stats[0].Record(-1, 1, 44100, audioStatsTestInterval) })

	// Turned off, we never get as far as looking at the device number.
	assert.NotPanics(t, func() { stats[0].Record(phy.MaxADevs, 1, 44100, 0) })
}
