// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package metrics exposes Prometheus counters and gauges over HTTP.
//
//nolint:gochecknoglobals
package metrics

/*------------------------------------------------------------------
 *
 * Purpose:	Expose internal counters and gauges in Prometheus text
 *		exposition format over HTTP, so a fleet of digipeaters
 *		can be scraped and monitored.
 *
 * Description:	Everything here is push-based: counters and gauges are
 *		registered with the default Prometheus registry, and
 *		updated from the various subsystems as events happen, via
 *		the exported Record and Set functions below.  This package
 *		has no knowledge of, or dependency on, the code reporting
 *		into it.
 *
 *------------------------------------------------------------------*/

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var metricFramesReceived = promauto.NewCounterVec(prometheus.CounterOpts{ //nolint:exhaustruct
	Name: "samoyed_frames_received_total",
	Help: "Number of AX.25 frames received with a valid FCS.",
}, []string{"channel"})

var metricFramesTransmitted = promauto.NewCounterVec(prometheus.CounterOpts{ //nolint:exhaustruct
	Name: "samoyed_frames_transmitted_total",
	Help: "Number of AX.25 frames handed to the modem for transmission.",
}, []string{"channel"})

var metricAX25Retries = promauto.NewCounterVec(prometheus.CounterOpts{ //nolint:exhaustruct
	Name: "samoyed_ax25_link_retries_total",
	Help: "Number of AX.25 connected-mode link layer retries.",
}, []string{"channel"})

var metricCorrectedSymbols = promauto.NewCounterVec(prometheus.CounterOpts{ //nolint:exhaustruct
	Name: "samoyed_corrected_symbols_total",
	Help: "Sum of correction amounts (bit-fix level, or FX.25/IL2P Reed-Solomon symbols corrected) for received frames.",
}, []string{"channel", "type"})

var metricDedupeHits = promauto.NewCounterVec(prometheus.CounterOpts{ //nolint:exhaustruct
	Name: "samoyed_dedupe_hits_total",
	Help: "Number of transmit duplicates suppressed by the digipeater dedupe logic.",
}, []string{"channel"})

// RecordFrameReceived is called for every frame accepted with a valid FCS.
// fecType should be "fx25", "il2p", or "fix_bits", describing what kind of
// correction (if any) was needed; corrected is the correction amount.
func RecordFrameReceived(channel int, fecType string, corrected int) {
	var channelLabel = strconv.Itoa(channel)

	metricFramesReceived.WithLabelValues(channelLabel).Inc()

	if corrected <= 0 {
		return
	}

	metricCorrectedSymbols.WithLabelValues(channelLabel, fecType).Add(float64(corrected))
}

// RecordFrameTransmitted is called for every frame handed to the modem for transmission.
func RecordFrameTransmitted(channel int) {
	metricFramesTransmitted.WithLabelValues(strconv.Itoa(channel)).Inc()
}

// RecordRetry is called whenever the AX.25 link layer retry count for a
// connected-mode session increases.
func RecordRetry(channel int) {
	metricAX25Retries.WithLabelValues(strconv.Itoa(channel)).Inc()
}

// RecordDedupeHit is called whenever the digipeater dedupe logic finds a duplicate.
func RecordDedupeHit(channel int) {
	metricDedupeHits.WithLabelValues(strconv.Itoa(channel)).Inc()
}

var metricChannelUp = promauto.NewGaugeVec(prometheus.GaugeOpts{ //nolint:exhaustruct
	Name: "samoyed_channel_up",
	Help: "Whether a radio channel is configured (1) or not (0).",
}, []string{"channel"})

var metricDCD = promauto.NewGaugeVec(prometheus.GaugeOpts{ //nolint:exhaustruct
	Name: "samoyed_dcd",
	Help: "Whether the data carrier detect (squelch) is currently active on a channel.",
}, []string{"channel"})

var metricTxQueueDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{ //nolint:exhaustruct
	Name: "samoyed_tx_queue_depth",
	Help: "Number of packets currently queued for transmission.",
}, []string{"channel", "priority"})

var metricAudioLevel = promauto.NewGaugeVec(prometheus.GaugeOpts{ //nolint:exhaustruct
	Name: "samoyed_audio_receive_level",
	Help: "Received audio level, roughly 0 to 100.",
}, []string{"channel"})

var metricIgateRFRecv = promauto.NewCounter(prometheus.CounterOpts{ //nolint:exhaustruct
	Name: "samoyed_igate_rf_recv_packets_total",
	Help: "Number of candidate APRS packets seen from the radio.",
})

var metricIgateRFXmit = promauto.NewCounter(prometheus.CounterOpts{ //nolint:exhaustruct
	Name: "samoyed_igate_rf_xmit_packets_total",
	Help: "Number of packets transmitted to radio by the IGate function.",
})

var metricIgateUplink = promauto.NewCounter(prometheus.CounterOpts{ //nolint:exhaustruct
	Name: "samoyed_igate_uplink_packets_total",
	Help: "Number of packets forwarded to the APRS-IS server.",
})

var metricIgateDownlink = promauto.NewCounter(prometheus.CounterOpts{ //nolint:exhaustruct
	Name: "samoyed_igate_downlink_packets_total",
	Help: "Number of packets received from the APRS-IS server.",
})

var metricIgateConnects = promauto.NewCounter(prometheus.CounterOpts{ //nolint:exhaustruct
	Name: "samoyed_igate_connects_total",
	Help: "Number of successful connections to the APRS-IS server.",
})

var metricIgateFailedConnects = promauto.NewCounter(prometheus.CounterOpts{ //nolint:exhaustruct
	Name: "samoyed_igate_failed_connects_total",
	Help: "Number of failed connection attempts to the APRS-IS server.",
})

// SetChannelUp is called once per channel at startup, from configuration state.
func SetChannelUp(channel int, up bool) {
	metricChannelUp.WithLabelValues(strconv.Itoa(channel)).Set(boolToFloat(up))
}

// SetDCD is called whenever a channel's data carrier detect (squelch) state changes.
func SetDCD(channel int, active bool) {
	metricDCD.WithLabelValues(strconv.Itoa(channel)).Set(boolToFloat(active))
}

// SetTxQueueDepth is called whenever a channel/priority's transmit queue depth changes.
func SetTxQueueDepth(channel, prio, depth int) {
	metricTxQueueDepth.WithLabelValues(strconv.Itoa(channel), strconv.Itoa(prio)).Set(float64(depth))
}

// audioLevelSampleCounts decimates SetAudioLevel calls per channel, since
// the caller reports on every audio sample (up to tens of thousands of
// times per second) but the metric is only ever scraped every 15-30s.
var audioLevelSampleCounts sync.Map // channel int -> *atomic.Uint32

const audioLevelDecimation = 4410 // ~10Hz at a 44.1kHz sample rate.

// SetAudioLevel is called on every audio sample with the current received
// audio level for a channel; actual recording is decimated internally.
func SetAudioLevel(channel, level int) {
	var v, _ = audioLevelSampleCounts.LoadOrStore(channel, new(atomic.Uint32))

	var counter, _ = v.(*atomic.Uint32)
	if counter.Add(1)%audioLevelDecimation != 0 {
		return
	}

	metricAudioLevel.WithLabelValues(strconv.Itoa(channel)).Set(float64(level))
}

// RecordRFReceived is called for every candidate APRS packet seen from the radio.
func RecordRFReceived() {
	metricIgateRFRecv.Inc()
}

// RecordRFTransmitted is called for every packet transmitted to radio by the IGate function.
func RecordRFTransmitted() {
	metricIgateRFXmit.Inc()
}

// RecordUplink is called for every packet forwarded to the APRS-IS server.
func RecordUplink() {
	metricIgateUplink.Inc()
}

// RecordDownlink is called for every packet received from the APRS-IS server.
func RecordDownlink() {
	metricIgateDownlink.Inc()
}

// RecordIgateConnect is called for every attempted connection to the APRS-IS server.
func RecordIgateConnect() {
	metricIgateConnects.Inc()
}

// RecordIgateFailedConnect is called whenever a connection attempt to the APRS-IS server fails.
func RecordIgateFailedConnect() {
	metricIgateFailedConnects.Inc()
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}

	return 0
}

// Start starts the Prometheus "/metrics" HTTP endpoint on port, and returns
// a channel that receives a single error if and when the listener fails.
// Logging of success/failure is the caller's responsibility.
func Start(port int) <-chan error {
	var mux = http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	var errCh = make(chan error, 1)

	go func() {
		errCh <- http.ListenAndServe(fmt.Sprintf(":%d", port), mux) //nolint:gosec
	}()

	return errCh
}
