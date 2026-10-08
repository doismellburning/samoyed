// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"errors"
	"net/http"

	"github.com/doismellburning/samoyed/internal/aprs"
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/webui"
	"github.com/sirupsen/logrus"
)

// webui_init starts the web dashboard and map if a port was configured with
// WEBPORT, and returns the hub to publish frames to.  A port of 0 (the
// default) disables it, as does failing to start it, and either returns nil -
// which webPublishReceived and webPublishTransmitted take as nothing to do.
func webui_init(ctx context.Context, audio *RadioConfig, mc *misc_config_s) *webui.Hub {
	if mc.web_port == 0 {
		logrus.Debug("Web interface disabled")

		return nil
	}

	var hub = webui.NewHub()

	for channel := range MAX_TOTAL_CHANS {
		if description, ok := channelDescription(audio, channel).Get(); ok {
			hub.AddChannel(channel, description)
		}
	}

	var errCh, startErr = webui.Start(ctx, mc.web_port, hub)
	if startErr != nil {
		logrus.WithError(startErr).WithField("port", mc.web_port).Error("Unable to start web interface")

		return nil
	}

	go func() {
		var err = <-errCh
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.WithError(err).WithField("port", mc.web_port).Error("Web interface stopped")
		}
	}()

	logrus.WithField("port", mc.web_port).Info("Web interface listening")

	return hub
}

// channelDescription names what a configured channel is connected to, or
// Nothing for one that isn't configured.
func channelDescription(audio *RadioConfig, channel int) maybe.Maybe[string] {
	if audio == nil || channel < 0 || channel >= MAX_TOTAL_CHANS {
		return maybe.Nothing[string]()
	}

	switch audio.chan_medium[channel] {
	case MEDIUM_RADIO:
		return maybe.Just("radio")
	case MEDIUM_IGATE:
		return maybe.Just("aprs-is")
	case MEDIUM_NETTNC:
		return maybe.Just("network")
	case MEDIUM_AXUDP:
		return maybe.Just("axudp")
	case MEDIUM_NONE:
	}

	return maybe.Nothing[string]()
}

// subchanVia names where app_process_rec_packet's subchan says a frame came
// from.
func subchanVia(subchan int) string {
	switch subchan {
	case -1:
		return "dtmf"
	case -2:
		return "aprs-is"
	case -3:
		return "network"
	case -4:
		return "axudp"
	}

	return "radio"
}

// webPublishReceived hands a received frame to the web interface, if there is
// one.
func webPublishReceived(hub *webui.Hub, channel int, subchan int, pp *ax25.Packet, A *aprs.Decoded, alevel ax25.ALevel) {
	if hub == nil {
		return
	}

	var p = webui.NewPacket(pp, A, webui.Received)
	p.Channel = channel
	p.Via = subchanVia(subchan)

	if alevel.Rec >= 0 && subchan >= 0 {
		p.AudioLevel = maybe.Just(alevel.Rec)
	}

	hub.Publish(p)
}

// webPublishTransmitted hands a transmitted frame to the web interface, if
// there is one.
func webPublishTransmitted(hub *webui.Hub, channel int, pp *ax25.Packet) {
	if hub == nil {
		return
	}

	var p = webui.NewPacket(pp, nil, webui.Transmitted)
	p.Channel = channel

	hub.Publish(p)
}
