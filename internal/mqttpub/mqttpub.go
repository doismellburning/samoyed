// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package mqttpub publishes a packet node's events to an MQTT broker, as JSON,
// one topic per kind of event under the node's own: a link coming up on node
// Q1TEST goes to "samoyed/Q1TEST/link/up".  The node's status - "online", or
// "offline" when it goes, by way of the broker's last will - is retained at
// "samoyed/Q1TEST/status".
package mqttpub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/nodeevents"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

// DefaultTopicPrefix is the first level of every topic, unless configured
// otherwise.
const DefaultTopicPrefix = "samoyed"

// Config is where and how to publish.
type Config struct {
	// Broker is the broker's URL: tcp://host:1883, ssl://host:8883 for TLS,
	// or ws:// or wss:// for MQTT over WebSockets.
	Broker string

	Username string
	Password string

	// ClientID identifies the node to the broker; left empty, it is made
	// from the node's callsign.
	ClientID string

	// TopicPrefix is the first level of every topic.
	TopicPrefix string

	// Node is the node's callsign, the second level of every topic.
	Node string
}

// Validate checks c, filling in what was left out.
func (c *Config) Validate() error {
	var u, err = url.Parse(c.Broker)
	if err != nil {
		return fmt.Errorf("mqtt: broker %q: %w", c.Broker, err)
	}

	switch u.Scheme {
	case "tcp", "mqtt", "ssl", "tls", "mqtts", "ws", "wss":
	default:
		return fmt.Errorf("mqtt: broker %q: scheme must be one of tcp, ssl, ws or wss", c.Broker)
	}

	if u.Host == "" {
		return fmt.Errorf("mqtt: broker %q has no host", c.Broker)
	}

	if c.TopicPrefix == "" {
		c.TopicPrefix = DefaultTopicPrefix
	}

	if strings.ContainsAny(c.TopicPrefix, "#+") {
		return fmt.Errorf("mqtt: topic prefix %q must not contain wildcards", c.TopicPrefix)
	}

	if c.ClientID == "" {
		c.ClientID = "samoyed-" + c.Node
	}

	return nil
}

// Topic returns the topic an event of kind goes to.
func (c *Config) Topic(kind string) string {
	return c.TopicPrefix + "/" + c.Node + "/" + kind
}

// Sender is where a Publisher sends to: the broker, or a stand-in for one.
type Sender interface {
	Send(topic string, retained bool, payload []byte)
}

// Run publishes the events from events through s until ctx is done or events
// closes.
func Run(ctx context.Context, cfg Config, events <-chan nodeevents.Event, s Sender) {
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-events:
			if !ok {
				return
			}

			payload, err := json.Marshal(e) // := for errchkjson, which doesn't follow var.
			if err != nil {
				continue
			}

			s.Send(cfg.Topic(string(e.Kind)), false, payload)
		}
	}
}

// Broker is a Sender publishing to a real broker.
type Broker struct {
	client mqtt.Client
}

// sendTimeout is how long a publish may take before it is given up on.
const sendTimeout = 5 * time.Second

// Send publishes payload to topic, at most once.  Anything published while the
// broker cannot be reached is lost.
func (b *Broker) Send(topic string, retained bool, payload []byte) {
	if !b.client.IsConnectionOpen() {
		return
	}

	var token = b.client.Publish(topic, 0, retained, payload)
	if !token.WaitTimeout(sendTimeout) || token.Error() != nil {
		logrus.WithFields(logrus.Fields{"topic": topic, "error": token.Error()}).Debug("MQTT publish failed")
	}
}

// Connect starts connecting to the broker cfg names, retrying for as long as
// it takes and reconnecting whenever the connection drops, until ctx is done.
// It returns at once; what is sent before the connection is made is lost.
func Connect(ctx context.Context, cfg Config) (*Broker, error) {
	var opts = mqtt.NewClientOptions()
	opts.AddBroker(cfg.Broker)
	opts.SetClientID(cfg.ClientID)
	opts.SetUsername(cfg.Username)
	opts.SetPassword(cfg.Password)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(30 * time.Second)
	opts.SetWill(cfg.Topic("status"), "offline", 0, true)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		logrus.WithField("broker", cfg.Broker).Info("MQTT connected")
		c.Publish(cfg.Topic("status"), 0, true, "online")
	})
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		logrus.WithError(err).WithField("broker", cfg.Broker).Warn("MQTT connection lost")
	})

	var client = mqtt.NewClient(opts)

	var token = client.Connect()
	if token.WaitTimeout(0) && token.Error() != nil && !errors.Is(token.Error(), context.Canceled) {
		return nil, token.Error()
	}

	context.AfterFunc(ctx, func() {
		if client.IsConnectionOpen() {
			client.Publish(cfg.Topic("status"), 0, true, "offline").WaitTimeout(time.Second)
		}

		client.Disconnect(250)
	})

	var b = new(Broker)
	b.client = client

	return b, nil
}
