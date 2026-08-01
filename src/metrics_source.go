// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"github.com/doismellburning/samoyed/internal/metrics"
)

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

// metrics_init starts the Prometheus "/metrics" HTTP endpoint if a port was
// configured with METRICSPORT, and pushes each channel's static up/down
// state from the audio config (everything else pushes from its own
// subsystem as events happen). A port of 0 (the default) disables it.
func metrics_init(mc *misc_config_s) {
	if mc.metrics_port == 0 {
		text_color_set(DW_COLOR_INFO)
		dw_printf("Disabled Prometheus metrics endpoint.\n")

		return
	}

	for channel := range MAX_RADIO_CHANS {
		var isRadioChannel = save_audio_config_p != nil && save_audio_config_p.chan_medium[channel] == MEDIUM_RADIO

		metrics.SetChannelUp(channel, isRadioChannel)
	}

	var errCh = metrics.Start(mc.metrics_port)

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
