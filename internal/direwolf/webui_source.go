// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/doismellburning/samoyed/internal/aprs"
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/doismellburning/samoyed/internal/version"
	"github.com/doismellburning/samoyed/internal/webui"
	"github.com/sirupsen/logrus"
)

// webui_init makes the web dashboard and map's hub, for frames to be published
// to, and binds addr - every address on the port configured with WEBPORT - to
// serve it from, which webui_start then does.  Failing to bind returns a nil
// hub and server, and webPublishReceived and webPublishTransmitted take a nil
// hub as nothing to do.  A port of 0 (the default) disables it, so
// DirewolfMain doesn't call this, and has a nil hub too.
func webui_init(ctx context.Context, audio *RadioConfig, addr string) (*webui.Hub, *webui.Server) {
	var hub = webui.NewHub()

	for channel := range MAX_TOTAL_CHANS {
		if c, ok := webChannel(audio, channel).Get(); ok {
			hub.AddChannel(c)
		}
	}

	var server, serverErr = webui.NewServer(ctx, addr, hub)
	if serverErr != nil {
		logrus.WithError(serverErr).WithField("address", addr).Error("Unable to start web interface")

		return nil, nil
	}

	return hub, server
}

// webui_start serves the web interface webui_init bound, reporting the
// station's own state from status, until ctx is cancelled.
func webui_start(ctx context.Context, server *webui.Server, status webui.StatusFunc) {
	var listening = server.Addr()

	var errCh = server.Start(ctx, status)

	go func() {
		var err = <-errCh
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.WithError(err).WithField("address", listening).Error("Web interface stopped")
		}
	}()

	logrus.WithField("address", listening).Info("Web interface listening")
}

// webStatusSources is what the web interface's status is drawn from.  Any of
// igate and netrom may be nil, for a station without one.
type webStatusSources struct {
	started time.Time
	audio   *RadioConfig
	igate   *IGate
	netrom  *netrom.Node

	// queueLengths is how much waits in each radio channel's transmit
	// queue.
	queueLengths func() [MAX_RADIO_CHANS][TQ_NUM_PRIO]int
}

// webStatus reports the station's own state to the web interface, from src.
func webStatus(src *webStatusSources) webui.StatusFunc {
	return func() webui.Status {
		var s webui.Status
		s.Version = version.Version
		s.Started = src.started
		s.IGate = webIGateStatus(src.igate)
		s.NetROM = webNetROMStatus(src.netrom)
		s.Queues = webQueues(src.audio, src.queueLengths())

		return s
	}
}

// webQueues describes the transmit queue of each radio channel for the web
// interface.  Only a radio channel has one: frames for any other kind go
// straight to whatever carries them.
func webQueues(audio *RadioConfig, lengths [MAX_RADIO_CHANS][TQ_NUM_PRIO]int) []webui.QueueStatus {
	if audio == nil {
		return nil
	}

	var queues []webui.QueueStatus

	for channel := range MAX_RADIO_CHANS {
		if audio.chan_medium[channel] != MEDIUM_RADIO {
			continue
		}

		queues = append(queues, webui.QueueStatus{
			Channel: channel,
			High:    lengths[channel][TQ_PRIO_0_HI],
			Low:     lengths[channel][TQ_PRIO_1_LO],
		})
	}

	return queues
}

// webChannel describes a configured channel for the web interface, or Nothing
// for one that isn't configured.
func webChannel(audio *RadioConfig, channel int) maybe.Maybe[webui.Channel] {
	return maybe.Fmap(func(description string) webui.Channel {
		var c webui.Channel
		c.Number = channel
		c.Description = description

		if !IsNoCall(audio.mycall[channel]) {
			c.Callsign = audio.mycall[channel]
		}

		return c
	}, channelDescription(audio, channel))
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

// webIGateStatus describes ig's connection to APRS-IS for the web interface,
// or Nothing if there is no IGate, or it has no server to connect to.
func webIGateStatus(ig *IGate) maybe.Maybe[webui.IGateStatus] {
	if ig == nil || !ig.configured() {
		return maybe.Nothing[webui.IGateStatus]()
	}

	var snap = ig.snapshot()

	var s webui.IGateStatus
	s.Server = net.JoinHostPort(ig.config.t2_server_name, strconv.Itoa(ig.config.t2_server_port))
	s.Connected = snap.connected
	s.LoggedIn = snap.loggedIn

	// The connect thread notes the time of every attempt, failed ones
	// included, so it is only the time of a connection while there is one.
	if snap.connected && !snap.stats.connectedAt.IsZero() {
		s.ConnectedAt = maybe.Just(snap.stats.connectedAt)
	}

	s.UplinkPackets = snap.stats.uplinkPackets
	s.UplinkBytes = snap.stats.uplinkBytes
	s.DownlinkPackets = snap.stats.downlinkPackets
	s.DownlinkBytes = snap.stats.downlinkBytes
	s.RFPackets = snap.stats.rfXmitPackets

	return maybe.Just(s)
}

// webNetROMStatus describes node for the web interface, or Nothing if there
// is no NET/ROM node.
func webNetROMStatus(node *netrom.Node) maybe.Maybe[webui.NetROMStatus] {
	if node == nil {
		return maybe.Nothing[webui.NetROMStatus]()
	}

	var status = node.Status()

	var s webui.NetROMStatus
	s.Channel = status.Channel
	s.Callsign = status.Callsign
	s.Alias = status.Alias

	// Empty rather than nil, so that the page gets a list either way.
	s.Neighbours = make([]webui.NetROMNeighbour, 0, len(status.Neighbours))
	for _, n := range status.Neighbours {
		s.Neighbours = append(s.Neighbours, webui.NetROMNeighbour{
			Callsign: n.Callsign,
			Link:     n.Link,
			Failures: n.Failures,
			Failing:  n.Failing,
		})
	}

	s.Destinations = make([]webui.NetROMDestination, 0, len(status.Destinations))
	for _, d := range status.Destinations {
		var routes = make([]webui.NetROMRoute, 0, len(d.Routes))
		for _, rt := range d.Routes {
			routes = append(routes, webui.NetROMRoute{
				Neighbour:    rt.Neighbour,
				Quality:      rt.Quality,
				Obsolescence: rt.Obsolescence,
			})
		}

		s.Destinations = append(s.Destinations, webui.NetROMDestination{
			Callsign: d.Callsign,
			Alias:    d.Alias,
			Routes:   routes,
		})
	}

	s.Circuits = make([]webui.NetROMCircuit, 0, len(status.Circuits))
	for _, c := range status.Circuits {
		s.Circuits = append(s.Circuits, webui.NetROMCircuit{
			OwnCall:     c.OwnCall,
			RemoteCall:  c.RemoteCall,
			RemoteNode:  c.RemoteNode,
			User:        c.User,
			Incoming:    c.Incoming,
			State:       c.State,
			Outstanding: c.Outstanding,
		})
	}

	return maybe.Just(s)
}
