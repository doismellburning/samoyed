// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

// What the node publishes: its users' sessions, from the shell, and its
// links, circuits, routes and stations heard, from NET/ROM - to an MQTT
// broker, if the configuration names one.

import (
	"context"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/mqttpub"
	"github.com/doismellburning/samoyed/internal/node"
	"github.com/doismellburning/samoyed/internal/nodeevents"
	"github.com/sirupsen/logrus"
)

// newEvent returns an event of kind about the node with address nodeCall, at
// now.
func newEvent(kind nodeevents.Kind, nodeCall string, now time.Time) nodeevents.Event {
	return nodeevents.Event{
		Time: now.UTC(), Kind: kind, Node: nodeCall,
		Port: nil, Local: "", Remote: "", User: "", Incoming: false, Role: "", Error: "", Room: "",
		Destinations: 0, Neighbours: 0,
	}
}

// shellObserver publishes what happens to the shell's users on bus.
func shellObserver(bus *nodeevents.Bus, nodeCall string) func(node.SessionEvent) {
	return func(e node.SessionEvent) {
		var ev = newEvent(nodeevents.LinkUp, nodeCall, time.Now())
		ev.User = e.User
		ev.Remote = e.User
		ev.Role = "user"
		ev.Incoming = true

		if e.Channel >= 0 {
			var channel = e.Channel
			ev.Port = &channel
			ev.Local = e.Local
		}

		switch e.Kind {
		case node.SessionStarted:
		case node.SessionEnded:
			ev.Kind = nodeevents.LinkDown
		case node.DownlinkUp, node.DownlinkDown:
			if e.Kind == node.DownlinkDown {
				ev.Kind = nodeevents.LinkDown
			}

			ev.Remote = e.Downlink
			ev.Role = "downlink"
			ev.Incoming = false
			ev.Error = e.Failure
		}

		bus.Publish(ev)
	}
}

// mqttBuffer is how many events the MQTT publisher can fall behind by before
// it starts missing them.
const mqttBuffer = 256

// mqtt_init starts publishing the node's events to an MQTT broker, if the
// configuration names one.
func mqtt_init(ctx context.Context, settings maybe.Maybe[mqttpub.Config], bus *nodeevents.Bus, nodeCall string) {
	var cfg, ok = settings.Get()
	if !ok {
		return
	}

	if bus == nil {
		logrus.Warn("MQTT is configured, but there is no node to publish the events of")

		return
	}

	cfg.Node = nodeCall

	var err = cfg.Validate()
	if err != nil {
		logrus.WithError(err).Error("MQTT")

		return
	}

	var broker, cerr = mqttpub.Connect(ctx, cfg)
	if cerr != nil {
		logrus.WithError(cerr).Error("Could not start MQTT")

		return
	}

	var events, stop = bus.Subscribe(mqttBuffer)
	context.AfterFunc(ctx, stop)

	go mqttpub.Run(ctx, cfg, events, broker)

	logrus.WithFields(logrus.Fields{"broker": cfg.Broker, "topic": cfg.Topic("#")}).Info("Publishing node events to MQTT")
}
