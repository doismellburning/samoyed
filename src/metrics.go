// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//nolint:gochecknoglobals
package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:	Expose internal counters and gauges in Prometheus text
 *		exposition format over HTTP, so a fleet of digipeaters
 *		can be scraped and monitored.
 *
 * Description:	Counters (monotonically increasing) are registered with
 *		the default Prometheus registry and updated from the
 *		various subsystems as events happen, via the
 *		MetricsRecord* functions below.  Everything else is a
 *		gauge computed on the fly, at scrape time, by
 *		metricsCollector.Collect, from whatever state the
 *		owning subsystem already keeps (queue depths, DCD
 *		state, IGate counters, etc) - no push instrumentation
 *		required for those.
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

// MetricsRecordFrameReceived is called for every frame accepted with a valid
// FCS, from app_process_rec_packet.  It also accounts for how much
// correction (bit fixing, FX.25, or IL2P Reed-Solomon) was needed, if any.
func MetricsRecordFrameReceived(channel int, fecType fec_type_t, retries BitFixLevel) {
	var channelLabel = strconv.Itoa(channel)

	metricFramesReceived.WithLabelValues(channelLabel).Inc()

	if retries <= BitFixNone {
		return
	}

	switch fecType {
	case fec_type_fx25:
		metricCorrectedSymbols.WithLabelValues(channelLabel, "fx25").Add(float64(retries))
	case fec_type_il2p:
		metricCorrectedSymbols.WithLabelValues(channelLabel, "il2p").Add(float64(retries))
	case fec_type_none:
		metricCorrectedSymbols.WithLabelValues(channelLabel, "fix_bits").Add(float64(retries))
	}
}

// MetricsRecordFrameTransmitted is called for every frame handed to the
// modem for transmission, from XmitService.send_one_frame.
func MetricsRecordFrameTransmitted(channel int) {
	metricFramesTransmitted.WithLabelValues(strconv.Itoa(channel)).Inc()
}

// MetricsRecordRetry is called whenever the AX.25 link layer retry count
// for a connected-mode session increases, from SET_RC.
func MetricsRecordRetry(channel int) {
	metricAX25Retries.WithLabelValues(strconv.Itoa(channel)).Inc()
}

// MetricsRecordDedupeHit is called whenever DedupeService.Check finds a duplicate.
func MetricsRecordDedupeHit(channel int) {
	metricDedupeHits.WithLabelValues(strconv.Itoa(channel)).Inc()
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

// metricsCollector is a pull-based prometheus.Collector for state that
// other subsystems already track themselves - it just reads that state at
// scrape time, so none of those subsystems need any push instrumentation.
type metricsCollector struct{}

func (metricsCollector) Describe(ch chan<- *prometheus.Desc) {
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

func (metricsCollector) Collect(ch chan<- prometheus.Metric) {
	for channel := range MAX_RADIO_CHANS {
		var channelLabel = strconv.Itoa(channel)
		var isRadioChannel = save_audio_config_p != nil && save_audio_config_p.chan_medium[channel] == MEDIUM_RADIO

		ch <- prometheus.MustNewConstMetric(metricsDescChannelUp, prometheus.GaugeValue, boolToFloat(isRadioChannel), channelLabel)

		if isRadioChannel {
			if hdlcReceiver != nil {
				ch <- prometheus.MustNewConstMetric(metricsDescDCD, prometheus.GaugeValue, float64(hdlcReceiver.DataDetectAny(channel)), channelLabel)
			}

			ch <- prometheus.MustNewConstMetric(metricsDescAudioLevel, prometheus.GaugeValue, float64(demod_get_audio_level(channel, 0).rec), channelLabel)
		}

		for prio := range TQ_NUM_PRIO {
			ch <- prometheus.MustNewConstMetric(metricsDescTxQueue, prometheus.GaugeValue, float64(tq_count(channel, prio, "", "", false)), channelLabel, strconv.Itoa(prio))
		}
	}

	ch <- prometheus.MustNewConstMetric(metricsDescIgateRFRecv, prometheus.CounterValue, float64(stats_rf_recv_packets))
	ch <- prometheus.MustNewConstMetric(metricsDescIgateRFXmit, prometheus.CounterValue, float64(stats_rf_xmit_packets))
	ch <- prometheus.MustNewConstMetric(metricsDescIgateUpl, prometheus.CounterValue, float64(stats_uplink_packets))
	ch <- prometheus.MustNewConstMetric(metricsDescIgateDnl, prometheus.CounterValue, float64(stats_downlink_packets))
	ch <- prometheus.MustNewConstMetric(metricsDescIgateConn, prometheus.CounterValue, float64(stats_connects))
	ch <- prometheus.MustNewConstMetric(metricsDescIgateFailed, prometheus.CounterValue, float64(stats_failed_connect))
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}

	return 0
}

// metrics_init starts the Prometheus "/metrics" HTTP endpoint if a port
// was configured with METRICSPORT.  A port of 0 (the default) disables it.
func metrics_init(mc *misc_config_s) {
	if mc.metrics_port == 0 {
		text_color_set(DW_COLOR_INFO)
		dw_printf("Disabled Prometheus metrics endpoint.\n")

		return
	}

	prometheus.MustRegister(metricsCollector{})

	var mux = http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	go func() {
		var err = http.ListenAndServe(fmt.Sprintf(":%d", mc.metrics_port), mux) //nolint:gosec
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Unable to start Prometheus metrics endpoint on port %d: %v\n", mc.metrics_port, err)
		}
	}()

	text_color_set(DW_COLOR_INFO)
	dw_printf("Prometheus metrics endpoint listening on port %d.\n", mc.metrics_port)
}
