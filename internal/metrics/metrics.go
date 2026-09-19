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
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// labelChannel is the Prometheus label naming the radio channel a metric belongs to.
const labelChannel = "channel"

var metricFramesReceived = promauto.NewCounterVec(prometheus.CounterOpts{ //nolint:exhaustruct_v5
	Name: "samoyed_frames_received_total",
	Help: "Number of AX.25 frames received with a valid FCS.",
}, []string{labelChannel})

var metricFramesTransmitted = promauto.NewCounterVec(prometheus.CounterOpts{ //nolint:exhaustruct_v5
	Name: "samoyed_frames_transmitted_total",
	Help: "Number of AX.25 frames handed to the modem for transmission.",
}, []string{labelChannel})

var metricAX25Retries = promauto.NewCounterVec(prometheus.CounterOpts{ //nolint:exhaustruct_v5
	Name: "samoyed_ax25_link_retries_total",
	Help: "Number of AX.25 connected-mode link layer retries.",
}, []string{labelChannel})

var metricCorrectedSymbols = promauto.NewCounterVec(prometheus.CounterOpts{ //nolint:exhaustruct_v5
	Name: "samoyed_corrected_symbols_total",
	Help: "Number of FX.25/IL2P Reed-Solomon symbols corrected in received frames.",
}, []string{labelChannel, "type"})

// Frames recovered by the HDLC bit-fix logic are counted separately, because
// what that path reports is which inversion strategy succeeded, not how many
// bits it had to touch - summing those ordinals would be meaningless.
var metricBitCorrectedFrames = promauto.NewCounterVec(prometheus.CounterOpts{ //nolint:exhaustruct_v5
	Name: "samoyed_frames_bit_corrected_total",
	Help: "Number of received frames recovered by the HDLC bit-fix logic, by the inversion strategy that succeeded.",
}, []string{labelChannel, "level"})

var metricDedupeHits = promauto.NewCounterVec(prometheus.CounterOpts{ //nolint:exhaustruct_v5
	Name: "samoyed_dedupe_hits_total",
	Help: "Number of transmit duplicates suppressed by the digipeater dedupe logic.",
}, []string{labelChannel})

// RecordFrameReceived is called for every frame accepted with a valid FCS.
// fecType should be "fx25" or "il2p" when forward error correction recovered
// the frame, in which case corrected is the number of Reed-Solomon symbols
// corrected, or "" when no FEC was involved.
func RecordFrameReceived(channel int, fecType string, corrected int) {
	var channelLabel = strconv.Itoa(channel)

	metricFramesReceived.WithLabelValues(channelLabel).Inc()

	if fecType == "" || corrected <= 0 {
		return
	}

	metricCorrectedSymbols.WithLabelValues(channelLabel, fecType).Add(float64(corrected))
}

// RecordBitFixed is called for a frame the HDLC bit-fix logic recovered, with
// the name of the inversion strategy that succeeded (e.g. "SINGLE", "TRIPLE").
func RecordBitFixed(channel int, level string) {
	metricBitCorrectedFrames.WithLabelValues(strconv.Itoa(channel), level).Inc()
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

var metricChannelUp = promauto.NewGaugeVec(prometheus.GaugeOpts{ //nolint:exhaustruct_v5
	Name: "samoyed_channel_up",
	Help: "Whether a radio channel is configured (1) or not (0).",
}, []string{labelChannel})

var metricDCD = promauto.NewGaugeVec(prometheus.GaugeOpts{ //nolint:exhaustruct_v5
	Name: "samoyed_dcd",
	Help: "Whether the data carrier detect (squelch) is currently active on a channel.",
}, []string{labelChannel})

var metricTxQueueDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{ //nolint:exhaustruct_v5
	Name: "samoyed_tx_queue_depth",
	Help: "Number of packets currently queued for transmission.",
}, []string{labelChannel, "priority"})

var metricAudioLevel = promauto.NewGaugeVec(prometheus.GaugeOpts{ //nolint:exhaustruct_v5
	Name: "samoyed_audio_receive_level",
	Help: "Received audio level, roughly 0 to 100.",
}, []string{labelChannel})

var metricIgateRFRecv = promauto.NewCounter(prometheus.CounterOpts{ //nolint:exhaustruct_v5
	Name: "samoyed_igate_rf_recv_packets_total",
	// Counted where Dire Wolf keeps stats_rf_recv_packets, which also sees
	// beacons and APRStt objects, not just traffic received off the air.
	Help: "Number of candidate APRS packets offered to the IGate for forwarding.",
})

var metricIgateRFXmit = promauto.NewCounter(prometheus.CounterOpts{ //nolint:exhaustruct_v5
	Name: "samoyed_igate_rf_xmit_packets_total",
	// Counted where Dire Wolf keeps stats_rf_xmit_packets: at the queue
	// insertion, so a packet the transmit queue then discards is included.
	Help: "Number of packets the IGate function queued for transmission to radio.",
})

var metricIgateUplink = promauto.NewCounter(prometheus.CounterOpts{ //nolint:exhaustruct_v5
	Name: "samoyed_igate_uplink_packets_total",
	Help: "Number of packets forwarded to the APRS-IS server.",
})

var metricIgateDownlink = promauto.NewCounter(prometheus.CounterOpts{ //nolint:exhaustruct_v5
	Name: "samoyed_igate_downlink_packets_total",
	Help: "Number of packets received from the APRS-IS server.",
})

var metricIgateConnects = promauto.NewCounter(prometheus.CounterOpts{ //nolint:exhaustruct_v5
	Name: "samoyed_igate_connects_total",
	Help: "Number of successful connections to the APRS-IS server.",
})

var metricIgateFailedConnects = promauto.NewCounter(prometheus.CounterOpts{ //nolint:exhaustruct_v5
	Name: "samoyed_igate_failed_connects_total",
	Help: "Number of failed connection attempts to the APRS-IS server.",
})

// SetChannelUp is called once per channel at startup, from configuration state.
func SetChannelUp(channel int, up bool) {
	metricChannelUp.WithLabelValues(strconv.Itoa(channel)).Set(boolToFloat(up))
}

// SeedChannel instantiates the per-channel series for a radio channel at
// startup, so that a quiet or freshly-restarted station exposes zeroes rather
// than nothing at all - dashboards and alerts can then tell "nothing has
// happened yet" apart from "this station is not reporting".
func SeedChannel(channel, numPriorities int) {
	var channelLabel = strconv.Itoa(channel)

	metricFramesReceived.WithLabelValues(channelLabel)
	metricFramesTransmitted.WithLabelValues(channelLabel)
	metricAX25Retries.WithLabelValues(channelLabel)
	metricDedupeHits.WithLabelValues(channelLabel)

	metricDCD.WithLabelValues(channelLabel)
	metricAudioLevel.WithLabelValues(channelLabel)

	// The FEC label values are this package's own contract - see
	// RecordFrameReceived - so a station receiving cleanly reports 0 corrected
	// symbols rather than no series at all.  The bit-fix strategies are
	// deliberately not seeded: which of them can ever appear depends on the
	// channel's FIX_BITS setting, which is off by default, so seeding them
	// would give most stations a handful of permanently-zero series.
	for _, fecType := range []string{"fx25", "il2p"} {
		metricCorrectedSymbols.WithLabelValues(channelLabel, fecType)
	}

	for prio := range numPriorities {
		metricTxQueueDepth.WithLabelValues(channelLabel, strconv.Itoa(prio))
	}
}

// SetDCD is called whenever a channel's data carrier detect (squelch) state changes.
func SetDCD(channel int, active bool) {
	metricDCD.WithLabelValues(strconv.Itoa(channel)).Set(boolToFloat(active))
}

// SetTxQueueDepth is called whenever a channel/priority's transmit queue depth changes.
func SetTxQueueDepth(channel, prio, depth int) {
	metricTxQueueDepth.WithLabelValues(strconv.Itoa(channel), strconv.Itoa(prio)).Set(float64(depth))
}

// SetAudioLevel records a channel's received audio level.  The demodulator
// sees every audio sample, so it is the caller's job to decimate - see
// audioLevelDecimation in demod.go - rather than call this tens of thousands
// of times a second for a value only scraped every 15-30s.
func SetAudioLevel(channel, level int) {
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

// RecordIgateConnect is called for each connection to the APRS-IS server that
// was actually established; a failed attempt calls RecordIgateFailedConnect
// instead, so exactly one of the two moves per attempt.
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

// Timeouts for the metrics HTTP server.  A scrape is a single small GET, so
// these are generous; the point is that a slow or idle client cannot pin a
// goroutine and a file descriptor indefinitely.
const (
	metricsReadHeaderTimeout = 5 * time.Second
	metricsReadTimeout       = 10 * time.Second
	metricsWriteTimeout      = 30 * time.Second
	metricsIdleTimeout       = 60 * time.Second

	// How long a shutdown waits for in-flight scrapes before giving up on
	// them.  A scrape is a handful of milliseconds of work, so anything
	// still going after this is not going to finish.
	metricsShutdownTimeout = 5 * time.Second
)

// Start starts the Prometheus "/metrics" HTTP endpoint on port.  The listening
// socket is bound before returning, so a failure to bind (a port already in
// use, say) comes back as the error return rather than arriving later on the
// channel - the caller can report "listening" without getting ahead of itself.
// The returned channel receives a single error if and when the server stops.
// Logging is the caller's responsibility.
//
// Cancelling ctx shuts the endpoint down, so the channel then reports
// http.ErrServerClosed.
func Start(ctx context.Context, port int) (<-chan error, error) {
	var mux = http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	var listener, listenErr = new(net.ListenConfig).Listen(ctx, "tcp", fmt.Sprintf(":%d", port))
	if listenErr != nil {
		return nil, listenErr
	}

	var server = new(http.Server)
	server.Handler = mux
	server.ReadHeaderTimeout = metricsReadHeaderTimeout
	server.ReadTimeout = metricsReadTimeout
	server.WriteTimeout = metricsWriteTimeout
	server.IdleTimeout = metricsIdleTimeout

	var errCh = make(chan error, 1)

	go func() {
		errCh <- server.Serve(listener)
	}()

	// Serve holds the listening socket until it is told to stop, so a
	// cancellation has to reach it here or the port stays bound for as long
	// as the process runs.
	context.AfterFunc(ctx, func() {
		// ctx is cancelled by the time this runs, so the shutdown gets a
		// deadline of its own rather than inheriting a dead one.
		var shutdownCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), metricsShutdownTimeout)
		defer cancel()

		_ = server.Shutdown(shutdownCtx)
	})

	return errCh, nil
}
