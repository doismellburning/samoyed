// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package mqttpub

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/nodeevents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sent struct {
	topic    string
	retained bool
	payload  string
}

type fakeSender struct {
	sent []sent
}

func (f *fakeSender) Send(topic string, retained bool, payload []byte) {
	f.sent = append(f.sent, sent{topic: topic, retained: retained, payload: string(payload)})
}

func TestRunPublishesEvents(t *testing.T) {
	var cfg = Config{Broker: "tcp://localhost:1883", Username: "", Password: "", ClientID: "", TopicPrefix: "", Node: "Q1TEST"}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, DefaultTopicPrefix, cfg.TopicPrefix)
	assert.Equal(t, "samoyed-Q1TEST", cfg.ClientID)

	var events = make(chan nodeevents.Event, 2)

	var port = 3
	events <- nodeevents.Event{
		Time: time.Unix(1000, 0).UTC(), Kind: nodeevents.LinkUp, Node: "Q1TEST", Port: &port,
		Local: "", Remote: "Q2TEST", User: "", Incoming: false, Role: "user", Error: "", Room: "",
		Destinations: 0, Neighbours: 0,
	}
	close(events)

	var f = new(fakeSender)
	Run(context.Background(), cfg, events, f)

	require.Len(t, f.sent, 1)
	assert.Equal(t, "samoyed/Q1TEST/link/up", f.sent[0].topic)
	assert.False(t, f.sent[0].retained)

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(f.sent[0].payload), &got))
	assert.Equal(t, map[string]any{
		"time": "1970-01-01T00:16:40Z", "kind": "link/up", "node": "Q1TEST",
		"port": float64(3), "remote": "Q2TEST", "role": "user",
	}, got)
}

func TestRunStopsWithContext(t *testing.T) {
	var ctx, cancel = context.WithCancel(context.Background())
	cancel()

	Run(ctx, Config{Broker: "", Username: "", Password: "", ClientID: "", TopicPrefix: "p", Node: "N"}, make(chan nodeevents.Event), new(fakeSender))
}

func TestValidateRejects(t *testing.T) {
	for _, broker := range []string{"", "localhost:1883", "http://localhost", "tcp://", "::"} {
		var cfg = Config{Broker: broker, Username: "", Password: "", ClientID: "", TopicPrefix: "", Node: "Q1TEST"}
		require.Error(t, cfg.Validate(), broker)
	}

	var cfg = Config{Broker: "ssl://broker.example.org:8883", Username: "", Password: "", ClientID: "", TopicPrefix: "a/#", Node: "Q1TEST"}
	require.Error(t, cfg.Validate())
}

// Connecting to a broker that is not there neither fails nor waits: the
// client keeps trying in the background.
func TestConnectUnreachable(t *testing.T) {
	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var cfg = Config{Broker: "tcp://127.0.0.1:1", Username: "", Password: "", ClientID: "", TopicPrefix: "", Node: "Q1TEST"}
	require.NoError(t, cfg.Validate())

	var start = time.Now()

	var b, err = Connect(ctx, cfg)
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 2*time.Second)

	b.Send("x", false, []byte("dropped, not waited on"))
}
