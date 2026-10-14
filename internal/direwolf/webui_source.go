// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"errors"
	"net/http"

	"github.com/doismellburning/samoyed/internal/aprs"
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/webui"
	"github.com/sirupsen/logrus"
)

// webui_init starts the web dashboard and map on addr - every address on the
// port configured with WEBPORT - and returns the hub to publish frames to.
// Failing to start it returns nil, which webPublishReceived and
// webPublishTransmitted take as nothing to do.  A port of 0 (the default)
// disables it, so DirewolfMain doesn't call this, and has a nil hub too.
func webui_init(ctx context.Context, audio *RadioConfig, addr string) *webui.Hub {
	var hub = webui.NewHub()

	for channel := range MAX_TOTAL_CHANS {
		if description, ok := channelDescription(audio, channel).Get(); ok {
			hub.AddChannel(channel, description)
		}
	}

	var server, serverErr = webui.NewServer(ctx, addr, hub)
	if serverErr != nil {
		logrus.WithError(serverErr).WithField("address", addr).Error("Unable to start web interface")

		return nil
	}

	var listening = server.Addr()

	var errCh = server.Start(ctx)

	go func() {
		var err = <-errCh
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.WithError(err).WithField("address", listening).Error("Web interface stopped")
		}
	}()

	logrus.WithField("address", listening).Info("Web interface listening")

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
	case MediumSerialTNC:
		return maybe.Just("serial")
	case MEDIUM_AXUDP:
		return maybe.Just("axudp")
	case MEDIUM_NONE:
	}

	return maybe.Nothing[string]()
}

// webPublishReceived hands a frame received from src to the web interface,
// if there is one.
func webPublishReceived(hub *webui.Hub, src dwcore.Source, pp *ax25.Packet, A *aprs.Decoded, alevel ax25.ALevel) {
	if hub == nil {
		return
	}

	var p = webui.NewPacket(pp, A, webui.Received)
	p.Channel = src.Slicer.Channel
	p.Via = src.Kind.String()

	if alevel.Rec >= 0 && src.Kind == dwcore.SourceRadio {
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
