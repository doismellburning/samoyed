// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/mqttpub"
)

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

	// Checked as it is, but kept as it was written: the node's address,
	// which the client ID defaults from, is only known at startup.
	var check = cfg

	var err = check.Validate()
	if err != nil {
		return fmt.Errorf("line %d: %w", ps.line, err)
	}

	ps.misc.mqtt = maybe.Just(cfg)

	return nil
}
