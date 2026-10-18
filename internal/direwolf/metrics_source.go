// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/doismellburning/samoyed/internal/metrics"
	"github.com/sirupsen/logrus"
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

// isRadioChannel reports whether a channel is a configured radio channel in
// audio, as opposed to a virtual one (ICHANNEL, a network TNC) or one that was
// never configured at all.  Metrics that describe the radio - received frames,
// DCD, audio level - are confined to these, and they are the channels
// metrics_init marks up and seeds.  A nil audio has no radio channels.
func isRadioChannel(audio *RadioConfig, channel int) bool {
	return channel >= 0 && channel < MAX_RADIO_CHANS &&
		audio != nil &&
		audio.chan_medium[channel] == MEDIUM_RADIO
}

// recordRadioFrame accounts for a frame the demodulator accepted.  It is called
// from the multi-modem's two hand-off points rather than from
// app_process_rec_packet, which is also reached by APRS-IS packets injected on
// ICHANNEL, network-TNC frames, APRStt, and locally generated SENDTO_RECV
// beacons - none of which came off the air, and counting those would leave
// samoyed_frames_received_total dominated by internet traffic on an IGate.
//
// PASSALL frames are excluded: hdlc_rec2 forwards those after a *failed* FCS
// check, flagged with BitFixPassall, so they are neither received with a valid
// FCS nor bit-corrected.  That test only holds without FEC - "retries" is a
// count of corrected Reed-Solomon symbols for FX.25 and IL2P, where the same
// value is a perfectly good frame that happened to need five of them.
func recordRadioFrame(channel int, fecType fec_type_t, retries BitFixLevel) {
	if fecType == fec_type_none && retries == BitFixPassall {
		return
	}

	metrics.RecordFrameReceived(channel, fecTypeLabel(fecType), int(retries))

	// Without FEC, "retries" is the bit-fix strategy that succeeded rather than
	// a count of anything, so it is reported by name on its own metric.
	if fecType == fec_type_none && retries != RETRY_NONE {
		metrics.RecordBitFixed(channel, retries.String())
	}
}

// metrics_init starts the Prometheus "/metrics" HTTP endpoint on addr - every
// address on the port configured with METRICSPORT, or "127.0.0.1:0" for any
// free one - and pushes each channel's static up/down state from audio,
// seeding the rest of that channel's series at zero (everything else pushes
// from its own subsystem as events happen).  A port of 0 (the default)
// disables the endpoint, so DirewolfMain doesn't call this.  It reports where
// the endpoint is listening: nil if it failed to start.
func metrics_init(ctx context.Context, audio *RadioConfig, addr string) net.Addr {
	for channel := range MAX_RADIO_CHANS {
		var isRadio = isRadioChannel(audio, channel)

		metrics.SetChannelUp(channel, isRadio)

		if isRadio {
			metrics.SeedChannel(channel, TQ_NUM_PRIO)
		}
	}

	var server, serverErr = metrics.NewServer(ctx, addr)
	if serverErr != nil {
		logrus.WithError(serverErr).WithField("addr", addr).Error("Unable to start Prometheus metrics endpoint")

		return nil
	}

	var listening = server.Addr()

	var errCh = server.Start(ctx)

	go func() {
		var err = <-errCh
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.WithError(err).WithField("port", addrPort(listening)).Error("Prometheus metrics endpoint stopped")
		}
	}()

	logrus.WithField("port", addrPort(listening)).Info("Prometheus metrics endpoint listening")

	return listening
}
