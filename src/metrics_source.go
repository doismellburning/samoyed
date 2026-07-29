// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"github.com/doismellburning/samoyed/internal/metrics"
)

// metricsState builds a snapshot of the gauge-worthy state kept by other
// subsystems (audio config, HDLC receiver, demodulator, transmit queue,
// IGate), for internal/metrics.Collector to report at scrape time - none of
// those subsystems need any push instrumentation for this.
func metricsState() metrics.State {
	var channels = make([]metrics.ChannelState, MAX_RADIO_CHANS)

	for channel := range MAX_RADIO_CHANS {
		var isRadioChannel = save_audio_config_p != nil && save_audio_config_p.chan_medium[channel] == MEDIUM_RADIO

		var cs = metrics.ChannelState{ //nolint:exhaustruct
			Up:      isRadioChannel,
			TxQueue: make([]int, TQ_NUM_PRIO),
		}

		if isRadioChannel {
			if hdlcReceiver != nil {
				cs.DCD = hdlcReceiver.DataDetectAny(channel) != 0
			}

			cs.AudioRecv = demod_get_audio_level(channel, 0).rec
		}

		for prio := range TQ_NUM_PRIO {
			cs.TxQueue[prio] = tq_count(channel, prio, "", "", false)
		}

		channels[channel] = cs
	}

	return metrics.State{
		Channels:            channels,
		IgateRFRecv:         stats_rf_recv_packets,
		IgateRFXmit:         stats_rf_xmit_packets,
		IgateUplink:         stats_uplink_packets,
		IgateDownlink:       stats_downlink_packets,
		IgateConnects:       stats_connects,
		IgateFailedConnects: stats_failed_connect,
	}
}

// fecTypeLabel converts a fec_type_t into the Prometheus label value used
// for samoyed_corrected_symbols_total's "type" label.
func fecTypeLabel(fecType fec_type_t) string {
	switch fecType {
	case fec_type_fx25:
		return "fx25"
	case fec_type_il2p:
		return "il2p"
	case fec_type_none:
		return "fix_bits"
	}

	return "fix_bits"
}

// metrics_init starts the Prometheus "/metrics" HTTP endpoint if a port
// was configured with METRICSPORT.  A port of 0 (the default) disables it.
func metrics_init(mc *misc_config_s) {
	if mc.metrics_port == 0 {
		text_color_set(DW_COLOR_INFO)
		dw_printf("Disabled Prometheus metrics endpoint.\n")

		return
	}

	var errCh = metrics.Start(mc.metrics_port, metricsState)

	go func() {
		var err = <-errCh
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Unable to start Prometheus metrics endpoint on port %d: %v\n", mc.metrics_port, err)
		}
	}()

	text_color_set(DW_COLOR_INFO)
	dw_printf("Prometheus metrics endpoint listening on port %d.\n", mc.metrics_port)
}
