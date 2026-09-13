// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package metrics

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each test uses its own channel numbers: the metrics live in the default
// registry for the lifetime of the process and cannot be reset between tests.

// assertMetric compares a single labelled child of a vec.  Counters and gauges
// hold exact small integers here, so InDelta with a zero delta is an exact
// comparison that keeps testifylint happy about comparing floats.
func assertMetric(t *testing.T, want float64, c prometheus.Collector, msgAndArgs ...any) {
	t.Helper()

	assert.InDelta(t, want, testutil.ToFloat64(c), 0, msgAndArgs...)
}

// metricOf reads a labelled child's current value.  The metrics live in the
// default registry for the lifetime of the process and cannot be reset, so
// counter assertions are written as deltas against a reading taken first -
// otherwise they only hold on the first run and `go test -count=2` fails.
func metricOf(c prometheus.Collector) float64 {
	return testutil.ToFloat64(c)
}

func TestRecordFrameReceivedCountsFECSymbolsOnly(t *testing.T) {
	// Regression: the bit-fix path reports which inversion strategy succeeded,
	// not a count of corrected symbols, so feeding its value to a "symbols
	// corrected" counter summed enum ordinals - and non-monotonically, since a
	// three-bit inversion (3) scores lower than a two-bit one (4).  Only real
	// FEC symbol counts belong on this metric.
	const channel = 90

	var channelLabel = strconv.Itoa(channel)

	var framesBefore = metricOf(metricFramesReceived.WithLabelValues(channelLabel))
	var fx25Before = metricOf(metricCorrectedSymbols.WithLabelValues(channelLabel, "fx25"))
	var il2pBefore = metricOf(metricCorrectedSymbols.WithLabelValues(channelLabel, "il2p"))

	RecordFrameReceived(channel, "", 4) // As a bit-fixed frame arrives.

	assertMetric(t, framesBefore+1, metricFramesReceived.WithLabelValues(channelLabel),
		"the frame itself should still be counted")
	assertMetric(t, fx25Before, metricCorrectedSymbols.WithLabelValues(channelLabel, "fx25"),
		"a bit-fixed frame must not report corrected symbols")
	assertMetric(t, il2pBefore, metricCorrectedSymbols.WithLabelValues(channelLabel, "il2p"),
		"a bit-fixed frame must not report corrected symbols")

	RecordFrameReceived(channel, "fx25", 3)

	assertMetric(t, fx25Before+3, metricCorrectedSymbols.WithLabelValues(channelLabel, "fx25"),
		"FX.25 really does report symbols corrected")
}

func TestRecordBitFixedCountsFramesByStrategy(t *testing.T) {
	const channel = 91

	var channelLabel = strconv.Itoa(channel)

	var tripleBefore = metricOf(metricBitCorrectedFrames.WithLabelValues(channelLabel, "TRIPLE"))
	var singleBefore = metricOf(metricBitCorrectedFrames.WithLabelValues(channelLabel, "SINGLE"))

	RecordBitFixed(channel, "TRIPLE")
	RecordBitFixed(channel, "TRIPLE")
	RecordBitFixed(channel, "SINGLE")

	// Counts frames, so two TRIPLE recoveries are 2 - not 3 + 3.
	assertMetric(t, tripleBefore+2, metricBitCorrectedFrames.WithLabelValues(channelLabel, "TRIPLE"))
	assertMetric(t, singleBefore+1, metricBitCorrectedFrames.WithLabelValues(channelLabel, "SINGLE"))
}

func TestSeedChannelCreatesSeriesAtZero(t *testing.T) {
	// Regression: with the gauges pushed rather than collected at scrape time,
	// a quiet station exposed no series at all, so dashboards and alerts saw
	// "no data" rather than 0.
	const channel = 92

	var channelLabel = strconv.Itoa(channel)

	SeedChannel(channel, 2)

	assertMetric(t, 0, metricDCD.WithLabelValues(channelLabel))
	assertMetric(t, 0, metricAudioLevel.WithLabelValues(channelLabel))
	assertMetric(t, 0, metricFramesReceived.WithLabelValues(channelLabel))
	assertMetric(t, 0, metricFramesTransmitted.WithLabelValues(channelLabel))
	assertMetric(t, 0, metricAX25Retries.WithLabelValues(channelLabel))
	assertMetric(t, 0, metricDedupeHits.WithLabelValues(channelLabel))

	for prio := range 2 {
		assertMetric(t, 0, metricTxQueueDepth.WithLabelValues(channelLabel, strconv.Itoa(prio)),
			"priority %d should be seeded", prio)
	}

	// A station receiving cleanly should report 0 corrected symbols rather than
	// no series at all, same as everything else here.
	for _, fecType := range []string{"fx25", "il2p"} {
		assertMetric(t, 0, metricCorrectedSymbols.WithLabelValues(channelLabel, fecType),
			"%s should be seeded", fecType)
	}
}

func TestSetAudioLevelDoesNotAllocate(t *testing.T) {
	// Regression: this is called from the demodulator, which sees every audio
	// sample.  It used to LoadOrStore a freshly allocated counter on every
	// call, heap-allocating tens of thousands of times a second per channel.
	const channel = 93

	var allocs = testing.AllocsPerRun(100, func() {
		SetAudioLevel(channel, 42)
	})

	assert.InDelta(t, 0, allocs, 0, "SetAudioLevel must not allocate")
}

func TestStartReportsBindFailureSynchronously(t *testing.T) {
	// Regression: the bind used to happen inside the goroutine, so the caller
	// logged "listening on port N" and only then discovered the port was taken.
	//
	// The blocker has to occupy the same address the code under test binds -
	// the wildcard, not just loopback.  Linux treats a wildcard bind as
	// conflicting with a specific-address bind on the same port; BSD, with the
	// SO_REUSEADDR that Go sets by default, does not.  Blocking only 127.0.0.1
	// therefore fails to block anything on macOS.
	var blocker, listenErr = new(net.ListenConfig).Listen(context.Background(), "tcp", ":0")
	require.NoError(t, listenErr)

	t.Cleanup(func() { blocker.Close() }) //nolint:errcheck

	var port = blocker.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert

	var errCh, startErr = Start(port)

	require.Error(t, startErr, "a port already in use must fail before Start returns")
	assert.Nil(t, errCh)
}

func TestStartServesMetrics(t *testing.T) {
	var port = freePort(t)

	var errCh, startErr = Start(port)

	require.NoError(t, startErr)
	require.NotNil(t, errCh)

	var client = &http.Client{Timeout: 5 * time.Second} //nolint:exhaustruct

	var req, reqErr = http.NewRequestWithContext(context.Background(), http.MethodGet,
		"http://127.0.0.1:"+strconv.Itoa(port)+"/metrics", nil)
	require.NoError(t, reqErr)

	var resp, getErr = client.Do(req)
	require.NoError(t, getErr)

	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body, readErr = io.ReadAll(resp.Body)
	require.NoError(t, readErr)
	assert.Contains(t, string(body), "samoyed_")
}

// freePort returns a port that was free a moment ago.  Start takes a port
// rather than returning the one it bound, so a test has to guess.
func freePort(t *testing.T) int {
	t.Helper()

	var l, err = new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var port = l.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert

	require.NoError(t, l.Close())

	return port
}
