// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"github.com/doismellburning/samoyed/internal/metrics"
)

// fecTypeLabel converts a fec_type_t into the Prometheus label value used for
// samoyed_corrected_symbols_total's "type" label.  It returns "" for frames
// that did not come via forward error correction: for those, the "retries"
// value accompanying the frame is a BitFixLevel strategy rather than a count
// of corrected symbols, and is reported by samoyed_frames_bit_corrected_total
// instead.
func fecTypeLabel(fecType fec_type_t) string {
	switch fecType {
	case fec_type_fx25:
		return "fx25"
	case fec_type_il2p:
		return "il2p"
	case fec_type_none:
		return ""
	}

	return ""
}

// isRadioChannel reports whether a channel is a configured radio channel, as
// opposed to a virtual one (ICHANNEL, a network TNC) or one that was never
// configured at all.  Metrics that describe the radio - received frames, DCD,
// audio level - are confined to these, and they are the channels metrics_init
// marks up and seeds.
func isRadioChannel(channel int) bool {
	return channel >= 0 && channel < MAX_RADIO_CHANS &&
		save_audio_config_p != nil &&
		save_audio_config_p.chan_medium[channel] == MEDIUM_RADIO
}

// recordRadioFrame accounts for a frame the demodulator accepted.  It is called
// from multi_modem's two hand-off points rather than from
// app_process_rec_packet, which is also reached by APRS-IS packets injected on
// ICHANNEL, network-TNC frames, APRStt, and locally generated SENDTO_RECV
// beacons - none of which came off the air, and counting those would leave
// samoyed_frames_received_total dominated by internet traffic on an IGate.
//
// PASSALL frames are excluded: hdlc_rec2 forwards those after a *failed* FCS
// check, flagged with RETRY_MAX, so they are neither received with a valid FCS
// nor bit-corrected.
func recordRadioFrame(channel int, fecType fec_type_t, retries BitFixLevel) {
	if retries == RETRY_MAX {
		return
	}

	metrics.RecordFrameReceived(channel, fecTypeLabel(fecType), int(retries))

	// Without FEC, "retries" is the bit-fix strategy that succeeded rather than
	// a count of anything, so it is reported by name on its own metric.
	if fecType == fec_type_none && retries != RETRY_NONE {
		metrics.RecordBitFixed(channel, retries.String())
	}
}

// metrics_init starts the Prometheus "/metrics" HTTP endpoint if a port was
// configured with METRICSPORT, and pushes each channel's static up/down
// state from the audio config, seeding the rest of that channel's series at
// zero (everything else pushes from its own subsystem as events happen).
// A port of 0 (the default) disables it.
func metrics_init(mc *misc_config_s) {
	if mc.metrics_port == 0 {
		text_color_set(DW_COLOR_INFO)
		dw_printf("Disabled Prometheus metrics endpoint.\n")

		return
	}

	for channel := range MAX_RADIO_CHANS {
		var isRadio = isRadioChannel(channel)

		metrics.SetChannelUp(channel, isRadio)

		if isRadio {
			metrics.SeedChannel(channel, TQ_NUM_PRIO)
		}
	}

	var errCh, startErr = metrics.Start(mc.metrics_port)
	if startErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Unable to start Prometheus metrics endpoint on port %d: %v\n", mc.metrics_port, startErr)

		return
	}

	go func() {
		var err = <-errCh
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Prometheus metrics endpoint on port %d stopped: %v\n", mc.metrics_port, err)
		}
	}()

	text_color_set(DW_COLOR_INFO)
	dw_printf("Prometheus metrics endpoint listening on port %d.\n", mc.metrics_port)
}
