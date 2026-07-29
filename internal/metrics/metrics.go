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
 * Description:	Counters (monotonically increasing) are registered with
 *		the default Prometheus registry and updated from the
 *		various subsystems as events happen, via the Record*
 *		functions below.  Everything else is a gauge computed on
 *		the fly, at scrape time, by Collector.Collect, from a
 *		State snapshot supplied by the caller - this package has
 *		no knowledge of, or dependency on, the code whose state
 *		it's reporting.
 *
 *------------------------------------------------------------------*/

import (
	"fmt"
	"net/http"
	"strconv"

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

// ChannelState is a snapshot of one radio channel's gauge-worthy state, as
// reported by a State provider at scrape time.
type ChannelState struct {
	Up        bool
	DCD       bool
	AudioRecv int
	TxQueue   []int // indexed by priority
}

// State is a snapshot of everything the pull-based gauges report, supplied
// by the caller at scrape time.
type State struct {
	Channels            []ChannelState
	IgateRFRecv         int
	IgateRFXmit         int
	IgateUplink         int
	IgateDownlink       int
	IgateConnects       int
	IgateFailedConnects int
}

var (
	metricsDescChannelUp   = prometheus.NewDesc("samoyed_channel_up", "Whether a radio channel is configured (1) or not (0).", []string{"channel"}, nil)
	metricsDescDCD         = prometheus.NewDesc("samoyed_dcd", "Whether the data carrier detect (squelch) is currently active on a channel.", []string{"channel"}, nil)
	metricsDescAudioLevel  = prometheus.NewDesc("samoyed_audio_receive_level", "Received audio level, roughly 0 to 100.", []string{"channel"}, nil)
	metricsDescTxQueue     = prometheus.NewDesc("samoyed_tx_queue_depth", "Number of packets currently queued for transmission.", []string{"channel", "priority"}, nil)
	metricsDescIgateRFRecv = prometheus.NewDesc("samoyed_igate_rf_recv_packets_total", "Number of candidate APRS packets seen from the radio.", nil, nil)
	metricsDescIgateRFXmit = prometheus.NewDesc("samoyed_igate_rf_xmit_packets_total", "Number of packets transmitted to radio by the IGate function.", nil, nil)
	metricsDescIgateUpl    = prometheus.NewDesc("samoyed_igate_uplink_packets_total", "Number of packets forwarded to the APRS-IS server.", nil, nil)
	metricsDescIgateDnl    = prometheus.NewDesc("samoyed_igate_downlink_packets_total", "Number of packets received from the APRS-IS server.", nil, nil)
	metricsDescIgateConn   = prometheus.NewDesc("samoyed_igate_connects_total", "Number of successful connections to the APRS-IS server.", nil, nil)
	metricsDescIgateFailed = prometheus.NewDesc("samoyed_igate_failed_connects_total", "Number of failed connection attempts to the APRS-IS server.", nil, nil)
)

// Collector is a pull-based prometheus.Collector for state that the caller
// already tracks itself - it just reads a State snapshot at scrape time, so
// the code being reported on needs no push instrumentation for it.
type Collector struct {
	source func() State
}

// NewCollector returns a Collector that calls source to get a fresh State snapshot each scrape.
func NewCollector(source func() State) *Collector {
	return &Collector{source: source}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- metricsDescChannelUp
	ch <- metricsDescDCD
	ch <- metricsDescAudioLevel
	ch <- metricsDescTxQueue
	ch <- metricsDescIgateRFRecv
	ch <- metricsDescIgateRFXmit
	ch <- metricsDescIgateUpl
	ch <- metricsDescIgateDnl
	ch <- metricsDescIgateConn
	ch <- metricsDescIgateFailed
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	var state = c.source()

	for channel, cs := range state.Channels {
		var channelLabel = strconv.Itoa(channel)

		ch <- prometheus.MustNewConstMetric(metricsDescChannelUp, prometheus.GaugeValue, boolToFloat(cs.Up), channelLabel)

		if cs.Up {
			ch <- prometheus.MustNewConstMetric(metricsDescDCD, prometheus.GaugeValue, boolToFloat(cs.DCD), channelLabel)
			ch <- prometheus.MustNewConstMetric(metricsDescAudioLevel, prometheus.GaugeValue, float64(cs.AudioRecv), channelLabel)
		}

		for prio, depth := range cs.TxQueue {
			ch <- prometheus.MustNewConstMetric(metricsDescTxQueue, prometheus.GaugeValue, float64(depth), channelLabel, strconv.Itoa(prio))
		}
	}

	ch <- prometheus.MustNewConstMetric(metricsDescIgateRFRecv, prometheus.CounterValue, float64(state.IgateRFRecv))
	ch <- prometheus.MustNewConstMetric(metricsDescIgateRFXmit, prometheus.CounterValue, float64(state.IgateRFXmit))
	ch <- prometheus.MustNewConstMetric(metricsDescIgateUpl, prometheus.CounterValue, float64(state.IgateUplink))
	ch <- prometheus.MustNewConstMetric(metricsDescIgateDnl, prometheus.CounterValue, float64(state.IgateDownlink))
	ch <- prometheus.MustNewConstMetric(metricsDescIgateConn, prometheus.CounterValue, float64(state.IgateConnects))
	ch <- prometheus.MustNewConstMetric(metricsDescIgateFailed, prometheus.CounterValue, float64(state.IgateFailedConnects))
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}

	return 0
}

// Start starts the Prometheus "/metrics" HTTP endpoint on port, registering
// a Collector that calls source for gauge state at scrape time, and returns
// a channel that receives a single error if and when the listener fails.
// Logging of success/failure is the caller's responsibility.
func Start(port int, source func() State) <-chan error {
	prometheus.MustRegister(NewCollector(source))

	var mux = http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	var errCh = make(chan error, 1)

	go func() {
		errCh <- http.ListenAndServe(fmt.Sprintf(":%d", port), mux) //nolint:gosec
	}()

	return errCh
}
