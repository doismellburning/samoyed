// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"strconv"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// metricValue reads one series out of the default Prometheus registry, which
// is where internal/metrics registers everything.  It returns 0 for a series
// that does not exist, which is what a caller wants for a "should not have
// moved" assertion.  Counters and gauges are both handled.
func metricValue(t *testing.T, name string, labels map[string]string) float64 {
	t.Helper()

	var value, err = gatherMetricValue(name, labels)
	require.NoError(t, err)

	return value
}

// gatherMetricValue is metricValue without the assertion, so that it is safe to
// call from a goroutine other than the one running the test.
func gatherMetricValue(name string, labels map[string]string) (float64, error) {
	var families, err = prometheus.DefaultGatherer.Gather()
	if err != nil {
		return 0, err
	}

	for _, family := range families {
		if family.GetName() != name {
			continue
		}

		for _, m := range family.GetMetric() {
			var matched = true

			for _, pair := range m.GetLabel() {
				if want, ok := labels[pair.GetName()]; ok && want != pair.GetValue() {
					matched = false

					break
				}
			}

			if !matched || len(m.GetLabel()) != len(labels) {
				continue
			}

			if m.GetCounter() != nil {
				return m.GetCounter().GetValue(), nil
			}

			if m.GetGauge() != nil {
				return m.GetGauge().GetValue(), nil
			}
		}
	}

	return 0, nil
}

func TestFecTypeLabel(t *testing.T) {
	require.Equal(t, "fx25", fecTypeLabel(fec_type_fx25))
	require.Equal(t, "il2p", fecTypeLabel(fec_type_il2p))

	// Without FEC the accompanying "retries" value is a BitFixLevel strategy
	// rather than a symbol count, so there is no corrected-symbols label for
	// it - samoyed_frames_bit_corrected_total reports that path instead.
	require.Empty(t, fecTypeLabel(fec_type_none))
}

// TestRecordRadioFrameExcludesPassall is a regression test: with PASSALL set,
// hdlc_rec2 forwards frames whose FCS check *failed*, flagged with RETRY_MAX.
// Those have neither a valid FCS nor any corrected bits, so they belong on
// neither samoyed_frames_received_total nor samoyed_frames_bit_corrected_total.
func TestRecordRadioFrameExcludesPassall(t *testing.T) {
	const channel = 0

	var frameLabels = map[string]string{"channel": strconv.Itoa(channel)}
	var passallLabels = map[string]string{
		"channel": strconv.Itoa(channel),
		"level":   RETRY_MAX.String(),
	}

	var framesBefore = metricValue(t, "samoyed_frames_received_total", frameLabels)

	recordRadioFrame(channel, fec_type_none, RETRY_MAX)

	assert.InDelta(t, framesBefore, metricValue(t, "samoyed_frames_received_total", frameLabels), 0,
		"a frame forwarded after a failed FCS check is not a received frame")
	assert.InDelta(t, 0, metricValue(t, "samoyed_frames_bit_corrected_total", passallLabels), 0,
		"nor was anything bit-corrected about it")

	// A frame the bit-fix logic really did recover is still counted.
	var fixedLabels = map[string]string{
		"channel": strconv.Itoa(channel),
		"level":   RETRY_INVERT_SINGLE.String(),
	}

	var fixedBefore = metricValue(t, "samoyed_frames_bit_corrected_total", fixedLabels)

	recordRadioFrame(channel, fec_type_none, RETRY_INVERT_SINGLE)

	assert.InDelta(t, framesBefore+1, metricValue(t, "samoyed_frames_received_total", frameLabels), 0)
	assert.InDelta(t, fixedBefore+1, metricValue(t, "samoyed_frames_bit_corrected_total", fixedLabels), 0)
}
