// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"time"

	"github.com/doismellburning/samoyed/internal/chat"
	"github.com/doismellburning/samoyed/internal/mqttpub"
	"github.com/doismellburning/samoyed/internal/netrom"
)

// NetROMSettings describes the NET/ROM node.  It has no line-format directive:
// it is written in YAML, under "netrom".
type NetROMSettings struct {
	// Call is the node's callsign; left out, it is the first port's MYCALL.
	Call string `yaml:"call"`

	// Alias is the node's alias, up to six characters.
	Alias string `yaml:"alias"`

	// Ports are the channels NET/ROM runs on.
	Ports []NetROMPortSettings `yaml:"ports"`

	// Neighbours are neighbours configured at a fixed quality, rather than
	// learned from their broadcasts.
	Neighbours []NetROMNeighbourSettings `yaml:"neighbours"`

	MinQuality               *int           `yaml:"minQuality"`
	Obsolescence             *int           `yaml:"obsolescence"`
	MinObsolescenceBroadcast *int           `yaml:"minObsolescenceBroadcast"`
	BroadcastInterval        *time.Duration `yaml:"broadcastInterval"`

	TTL         *int           `yaml:"ttl"`
	Window      *int           `yaml:"window"`
	Timeout     *time.Duration `yaml:"timeout"`
	Retries     *int           `yaml:"retries"`
	AckDelay    *time.Duration `yaml:"ackDelay"`
	BusyDelay   *time.Duration `yaml:"busyDelay"`
	IdleTimeout *time.Duration `yaml:"idleTimeout"`
}

// NetROMPortSettings is one channel NET/ROM runs on.
type NetROMPortSettings struct {
	Channel int `yaml:"channel"`

	// Quality is given to neighbours heard on the channel; left out, 192.
	Quality *int `yaml:"quality"`

	// Broadcast says whether NODES broadcasts are sent on the channel; left
	// out, they are.
	Broadcast *bool `yaml:"broadcast"`
}

// NetROMNeighbourSettings is a neighbour configured at a fixed quality.
type NetROMNeighbourSettings struct {
	Channel int    `yaml:"channel"`
	Call    string `yaml:"call"`
	Alias   string `yaml:"alias"`
	Quality int    `yaml:"quality"`
}

// defaultPortQuality is what a neighbour heard on a port is given, unless the
// port says otherwise - a common choice for a radio port.
const defaultPortQuality = 192

// applyNETROM checks the NET/ROM settings and keeps them for startup.  Whether
// the channels named are in use is checked then, since a legacy block read
// after the YAML can still define them.
func (ps *parseState) applyNETROM(settings NetROMSettings) error {
	var cfg = netrom.DefaultConfig()
	cfg.Call = settings.Call
	cfg.Alias = settings.Alias

	if len(settings.Ports) == 0 {
		return fmt.Errorf("line %d: NET/ROM needs at least one port", ps.line)
	}

	for _, p := range settings.Ports {
		if p.Channel < 0 || p.Channel >= MAX_TOTAL_CHANS {
			return fmt.Errorf("line %d: NET/ROM port channel %d must be in range 0 to %d", ps.line, p.Channel, MAX_TOTAL_CHANS-1)
		}

		var port = netrom.PortConfig{Port: p.Channel, Quality: defaultPortQuality, Broadcast: true}
		if p.Quality != nil {
			port.Quality = *p.Quality
		}

		if p.Broadcast != nil {
			port.Broadcast = *p.Broadcast
		}

		cfg.Ports = append(cfg.Ports, port)
	}

	for _, n := range settings.Neighbours {
		if n.Channel < 0 || n.Channel >= MAX_TOTAL_CHANS {
			return fmt.Errorf("line %d: NET/ROM neighbour %s channel %d must be in range 0 to %d", ps.line, n.Call, n.Channel, MAX_TOTAL_CHANS-1)
		}

		cfg.Neighbours = append(cfg.Neighbours, netrom.LockedNeighbour{Port: n.Channel, Call: n.Call, Alias: n.Alias, Quality: n.Quality})
	}

	setIf(&cfg.MinQuality, settings.MinQuality)
	setIf(&cfg.ObsolescenceInit, settings.Obsolescence)
	setIf(&cfg.MinObsolescenceBroadcast, settings.MinObsolescenceBroadcast)
	setIf(&cfg.BroadcastInterval, settings.BroadcastInterval)
	setIf(&cfg.TTL, settings.TTL)
	setIf(&cfg.Window, settings.Window)
	setIf(&cfg.Timeout, settings.Timeout)
	setIf(&cfg.Retries, settings.Retries)
	setIf(&cfg.AckDelay, settings.AckDelay)
	setIf(&cfg.BusyDelay, settings.BusyDelay)
	setIf(&cfg.IdleTimeout, settings.IdleTimeout)

	var err = cfg.ValidateParameters()
	if err != nil {
		return fmt.Errorf("line %d: %w", ps.line, err)
	}

	if cfg.Call != "" {
		var _, cerr = netrom.NormaliseCall(cfg.Call)
		if cerr != nil {
			return fmt.Errorf("line %d: %w", ps.line, cerr)
		}
	}

	var _, aerr = netrom.NormaliseAlias(cfg.Alias)
	if aerr != nil {
		return fmt.Errorf("line %d: %w", ps.line, aerr)
	}

	ps.misc.netrom = &cfg

	return nil
}

// setIf sets *dst to *src, if src is not nil.
func setIf[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}

// netromConfigFor finishes the NET/ROM configuration once all of the
// configuration has been read: the node's callsign defaults to its first
// port's MYCALL, and every port must be a channel in use.
func netromConfigFor(cfg netrom.Config, audio *RadioConfig) (netrom.Config, error) {
	for _, p := range cfg.Ports {
		if audio.chan_medium[p.Port] == MEDIUM_NONE {
			return cfg, fmt.Errorf("NET/ROM port channel %d is not configured", p.Port)
		}

		if audio.chan_medium[p.Port] == MEDIUM_IGATE {
			return cfg, fmt.Errorf("NET/ROM cannot run on the IGate channel %d", p.Port)
		}
	}

	for _, n := range cfg.Neighbours {
		var _, ok = cfg.PortConfig(n.Port)
		if !ok {
			return cfg, fmt.Errorf("NET/ROM neighbour %s is on channel %d, which is not a NET/ROM port", n.Call, n.Port)
		}
	}

	if cfg.Call == "" {
		var mycall = audio.mycall[cfg.Ports[0].Port]
		if mycall == "" || IsNoCall(mycall) {
			return cfg, fmt.Errorf("NET/ROM has no callsign: set its call, or MYCALL for channel %d", cfg.Ports[0].Port)
		}

		cfg.Call = mycall
	}

	return cfg, cfg.Validate()
}

// NodeSettings describes how the node serves the users who connect to it.  It
// has no line-format directive: it is written in YAML, under "node".
type NodeSettings struct {
	// Info is what the INFO command shows.
	Info string `yaml:"info"`

	// IdleTimeout disconnects a user who has done nothing for this long;
	// "0s" for never.
	IdleTimeout *time.Duration `yaml:"idleTimeout"`

	// AdminToken is the bearer token the web interface's admin API wants;
	// left out, there is no admin API.
	AdminToken string `yaml:"adminToken"`

	// Chat gives the node a chat server; left out, there is none.
	Chat *ChatSettings `yaml:"chat"`
}

// ChatSettings describes the node's chat server.
type ChatSettings struct {
	// DefaultRoom is the room users start in; left out, "General".
	DefaultRoom string `yaml:"defaultRoom"`

	// RateLines is how many lines a user may send in RateWindow; left out,
	// 10 in 30s.  0 is no limit.
	RateLines  *int           `yaml:"rateLines"`
	RateWindow *time.Duration `yaml:"rateWindow"`
}

// MQTTSettings describes where the node publishes its events.  It has no
// line-format directive: it is written in YAML, under "mqtt".
type MQTTSettings struct {
	Broker      string `yaml:"broker"`
	Username    string `yaml:"username"`
	Password    string `yaml:"password"`
	ClientID    string `yaml:"clientId"`
	TopicPrefix string `yaml:"topicPrefix"`
}

// applyMQTT checks the MQTT settings and keeps them for startup.
func (ps *parseState) applyMQTT(settings MQTTSettings) error {
	var cfg = mqttpub.Config{
		Broker:      settings.Broker,
		Username:    settings.Username,
		Password:    settings.Password,
		ClientID:    settings.ClientID,
		TopicPrefix: settings.TopicPrefix,
		Node:        "",
	}

	// Checked as it is, but kept as it was written: the node's callsign,
	// which the client ID defaults from, is only known at startup.
	var check = cfg

	var err = check.Validate()
	if err != nil {
		return fmt.Errorf("line %d: %w", ps.line, err)
	}

	ps.misc.mqtt = &cfg

	return nil
}

// defaultNodeIdleTimeout is how long a node user may sit idle, unless the
// configuration says otherwise.
const defaultNodeIdleTimeout = 15 * time.Minute

// applyNODE checks the node's settings and keeps them for startup.
func (ps *parseState) applyNODE(settings NodeSettings) error {
	ps.misc.node.Info = settings.Info
	ps.misc.node_admin_token = settings.AdminToken

	if settings.Chat != nil {
		var cfg = chat.DefaultConfig()
		if settings.Chat.DefaultRoom != "" {
			cfg.DefaultRoom = settings.Chat.DefaultRoom
		}

		setIf(&cfg.RateLines, settings.Chat.RateLines)
		setIf(&cfg.RateWindow, settings.Chat.RateWindow)

		var _, err = chat.New(cfg)
		if err != nil {
			return fmt.Errorf("line %d: %w", ps.line, err)
		}

		ps.misc.node_chat = &cfg
	}

	if settings.IdleTimeout != nil {
		if *settings.IdleTimeout < 0 {
			return fmt.Errorf("line %d: node idleTimeout must not be negative", ps.line)
		}

		ps.misc.node.IdleTimeout = *settings.IdleTimeout
	}

	return nil
}
