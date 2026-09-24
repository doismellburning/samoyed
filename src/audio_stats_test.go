package direwolf

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// newTestAudioStats gives each audio device a fresh AudioStats, and gives
// demod_get_audio_level something known to report, so a test starts from a
// known position and leaves nothing behind.
func newTestAudioStats(t *testing.T) *[MAX_ADEVS]AudioStats {
	t.Helper()

	var savedConfig = save_audio_config_p

	t.Cleanup(func() {
		save_audio_config_p = savedConfig
	})

	save_audio_config_p = new(audio_s)

	setTestAudioLevels(t)

	return new([MAX_ADEVS]AudioStats)
}

// audioStatsTestLevel is the receive audio level a test rigs channel ch to
// report.  They differ per channel so a report says which channel it is about.
func audioStatsTestLevel(ch int) int {
	return 10 * (ch + 1)
}

// setTestAudioLevels points each channel's demodulator state at a known peak,
// so demod_get_audio_level returns audioStatsTestLevel rather than whatever
// some earlier test left in the global demodulator state.
func setTestAudioLevels(t *testing.T) {
	t.Helper()

	type savedLevel struct {
		num_slicers int
		peak        float64
		valley      float64
	}

	var saved [MAX_RADIO_CHANS]savedLevel

	for ch := range MAX_RADIO_CHANS {
		var D = &demodulators[ch].states[0]

		saved[ch] = savedLevel{num_slicers: D.num_slicers, peak: D.alevel_rec_peak, valley: D.alevel_rec_valley}

		// demod_get_audio_level halves the peak-to-peak swing, in units of 100.
		D.num_slicers = 1
		D.alevel_rec_peak = float64(audioStatsTestLevel(ch)) / 50.0
		D.alevel_rec_valley = 0
	}

	t.Cleanup(func() {
		for ch := range MAX_RADIO_CHANS {
			var D = &demodulators[ch].states[0]

			D.num_slicers = saved[ch].num_slicers
			D.alevel_rec_peak = saved[ch].peak
			D.alevel_rec_valley = saved[ch].valley
		}
	})
}

// audioStatsTestInterval is the reporting interval, in seconds, that the tests
// below run the statistics at.
const audioStatsTestInterval = 10

// audioStatsRewind moves a device's last report one interval into the past, so
// the next call reaches the reporting branch without the test waiting for real
// time to pass.
func audioStatsRewind(s *AudioStats) {
	s.lastTime = s.lastTime.Add(-audioStatsTestInterval * time.Second)
}

// audioStatsPastFirstReport runs a device up to the point where the next
// elapsed interval will actually print: started, and with the deliberately
// suppressed first report out of the way.
func audioStatsPastFirstReport(t *testing.T, stats *[MAX_ADEVS]AudioStats, adev int, nchan int) {
	t.Helper()

	stats[adev].record(adev, nchan, 1, audioStatsTestInterval)
	audioStatsRewind(&stats[adev])
	stats[adev].record(adev, nchan, 1, audioStatsTestInterval)

	assert.False(t, stats[adev].suppressFirst)
}

// An interval of 0 is how the statistics are turned off, and nothing should be
// collected in that case, never mind printed.
func TestAudioStatsIntervalOffDoesNothing(t *testing.T) {
	var stats = newTestAudioStats(t)

	var output = CaptureOutput(t, func() {
		stats[0].record(0, 1, 44100, 0)
		stats[0].record(0, 1, 44100, -1)
	})

	assert.Empty(t, output)
	assert.True(t, stats[0].lastTime.IsZero(), "collection should not have started")
}

// The first call only starts the clock - and shortens the first collection
// period to 3 seconds, so there isn't a long silence before the first report.
func TestAudioStatsFirstCallStartsCollecting(t *testing.T) {
	var stats = newTestAudioStats(t)

	var output = CaptureOutput(t, func() {
		stats[0].record(0, 1, 44100, 100)
	})

	assert.Empty(t, output)
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

	var output = CaptureOutput(t, func() {
		stats[0].record(0, 1, 0, 100) // Starts collecting.
		stats[0].record(0, 1, 1000, 100)
		stats[0].record(0, 1, 500, 100)
		stats[0].record(0, 1, 0, 100)
		stats[0].record(0, 1, -1, 100)
	})

	assert.Empty(t, output, "the interval has not elapsed")
	assert.Equal(t, 1500, stats[0].sampleCount)
	assert.Equal(t, 2, stats[0].errorCount)
}

// The first report would be measured over a period that didn't start on a
// second boundary, so its rate would be noticeably wrong.  It is dropped.
func TestAudioStatsSuppressesFirstReport(t *testing.T) {
	var stats = newTestAudioStats(t)

	stats[0].record(0, 1, 44100, audioStatsTestInterval)
	audioStatsRewind(&stats[0])

	var output = CaptureOutput(t, func() {
		stats[0].record(0, 1, 44100, audioStatsTestInterval)
	})

	assert.Empty(t, output)
	assert.False(t, stats[0].suppressFirst, "only the first one is suppressed")
	assert.Zero(t, stats[0].sampleCount, "counters restart for the next interval")
	assert.Zero(t, stats[0].errorCount)
}

func TestAudioStatsReportsSampleRate(t *testing.T) {
	var stats = newTestAudioStats(t)
	audioStatsPastFirstReport(t, stats, 0, 1)

	audioStatsRewind(&stats[0])

	var output = CaptureOutput(t, func() {
		stats[0].record(0, 1, 441000, audioStatsTestInterval)
	})

	assert.Contains(t, output, "ADEVICE0: Sample rate approx. 44.1 k, 0 errors, receive audio level CH0 10")
	assert.Zero(t, stats[0].sampleCount, "counters restart after a report")
}

func TestAudioStatsReportsErrorCount(t *testing.T) {
	var stats = newTestAudioStats(t)
	audioStatsPastFirstReport(t, stats, 0, 1)

	stats[0].record(0, 1, 0, audioStatsTestInterval)
	stats[0].record(0, 1, 0, audioStatsTestInterval)
	audioStatsRewind(&stats[0])

	var output = CaptureOutput(t, func() {
		stats[0].record(0, 1, 0, audioStatsTestInterval)
	})

	assert.Contains(t, output, "ADEVICE0: Sample rate approx. 0.0 k, 3 errors, receive audio level CH0 10")
	assert.Zero(t, stats[0].errorCount, "counters restart after a report")
}

// A stereo device reports a level per channel.
func TestAudioStatsReportsBothChannels(t *testing.T) {
	var stats = newTestAudioStats(t)
	audioStatsPastFirstReport(t, stats, 0, 2)

	audioStatsRewind(&stats[0])

	var output = CaptureOutput(t, func() {
		stats[0].record(0, 2, 441000, audioStatsTestInterval)
	})

	assert.Contains(t, output, "ADEVICE0: Sample rate approx. 44.1 k, 0 errors, receive audio levels CH0 10, CH1 20")
}

// Each device keeps its own counters, and reports its own channel numbers.
func TestAudioStatsSecondDeviceIsIndependent(t *testing.T) {
	var stats = newTestAudioStats(t)
	audioStatsPastFirstReport(t, stats, 1, 1)

	stats[0].record(0, 1, 44100, audioStatsTestInterval) // Device 0 only just starts collecting.
	audioStatsRewind(&stats[1])

	var output = CaptureOutput(t, func() {
		stats[1].record(1, 1, 220500, audioStatsTestInterval)
	})

	assert.Contains(t, output, "ADEVICE1: Sample rate approx. 22.1 k, 0 errors, receive audio level CH2 30")
	assert.NotContains(t, output, "ADEVICE0")
	assert.True(t, stats[0].suppressFirst, "device 0 should be untouched")
}

func TestAudioStatsRejectsDeviceOutOfRange(t *testing.T) {
	var stats = newTestAudioStats(t)

	assert.Panics(t, func() { stats[0].record(MAX_ADEVS, 1, 44100, audioStatsTestInterval) })
	assert.Panics(t, func() { stats[0].record(-1, 1, 44100, audioStatsTestInterval) })

	// Turned off, we never get as far as looking at the device number.
	assert.NotPanics(t, func() { stats[0].record(MAX_ADEVS, 1, 44100, 0) })
}
