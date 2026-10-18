// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/nodeevents"
	"github.com/doismellburning/samoyed/internal/webui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// webNet is two nodes that know each other, with a user of the first
// connected onward to the second, whose NET/ROM goroutine is then left running
// to answer the web interface.
func webNet(t *testing.T, token string) http.Handler {
	t.Helper()

	var f = newFakeNet(t)
	var a = f.add("Q1TEST", "ONE")
	f.add("Q2TEST", "TWO")
	f.tick()
	f.userConnects("Q4TEST", "Q1TEST")
	f.userTypes("Q4TEST", "Q1TEST", "C TWO\r")

	var ctx, cancel = context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go a.netrom.run(ctx)

	var w = newNodeWeb(token)
	w.set(&webNode{config: a.config, shell: a.shell, netrom: a.netrom, link: fakeLink{n: a}, heard: noneHeard})

	return webui.Handler(webui.NewHub(), w.routes()...)
}

// get reads a view, which needs no token.
func get(t *testing.T, h http.Handler, path string, v any) int {
	t.Helper()

	var rec = httptest.NewRecorder()
	var r = httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)

	h.ServeHTTP(rec, r)

	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), v))
	}

	return rec.Code
}

func post(t *testing.T, h http.Handler, method string, path string, token string, body string) int {
	t.Helper()

	var rec = httptest.NewRecorder()
	var r = httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))

	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}

	h.ServeHTTP(rec, r)

	return rec.Code
}

func TestNodeWebViews(t *testing.T) {
	var h = webNet(t, "")

	var info nodeInfoJSON
	require.Equal(t, http.StatusOK, get(t, h, "/api/node/info", &info))
	assert.Equal(t, "Q1TEST", info.Address)
	assert.Equal(t, "ONE", info.Alias)
	assert.True(t, info.NetROM)

	var nodes []destinationJSON
	require.Equal(t, http.StatusOK, get(t, h, "/api/node/nodes", &nodes))
	require.Len(t, nodes, 1)
	assert.Equal(t, "TWO", nodes[0].Alias)

	var neighbours []neighbourJSON
	require.Equal(t, http.StatusOK, get(t, h, "/api/node/neighbours", &neighbours))
	require.Len(t, neighbours, 1)
	assert.True(t, neighbours[0].Linked)

	var circuits []circuitJSON
	require.Equal(t, http.StatusOK, get(t, h, "/api/node/circuits", &circuits))
	require.Len(t, circuits, 1)
	assert.Equal(t, "Q4TEST", circuits[0].User)
	assert.Equal(t, "connected", circuits[0].State)

	var sessions []sessionJSON
	require.Equal(t, http.StatusOK, get(t, h, "/api/node/sessions", &sessions))
	require.Len(t, sessions, 1)
	assert.Equal(t, "Q4TEST", sessions[0].User)
	assert.Equal(t, "TWO", sessions[0].Downlink)
	assert.True(t, sessions[0].Connected)

	var heard []heardJSON
	assert.Equal(t, http.StatusOK, get(t, h, "/api/node/heard", &heard))

	// No token configured: no admin API.
	assert.Equal(t, http.StatusNotFound, post(t, h, http.MethodPost, "/api/admin/nodes/broadcast", "", ""))
}

func TestNodeWebWithoutNode(t *testing.T) {
	var h = webui.Handler(webui.NewHub(), newNodeWeb("token").routes()...)

	var info nodeInfoJSON
	assert.Equal(t, http.StatusNotFound, get(t, h, "/api/node/info", &info))
	assert.Equal(t, http.StatusNotFound, post(t, h, http.MethodPost, "/api/admin/nodes/broadcast", "token", ""))
}

func TestNodeWebAdmin(t *testing.T) {
	var h = webNet(t, "s3cret")

	assert.Equal(t, http.StatusUnauthorized, post(t, h, http.MethodPost, "/api/admin/nodes/broadcast", "", ""))
	assert.Equal(t, http.StatusUnauthorized, post(t, h, http.MethodPost, "/api/admin/nodes/broadcast", "wrong", ""))
	assert.Equal(t, http.StatusNoContent, post(t, h, http.MethodPost, "/api/admin/nodes/broadcast", "s3cret", ""))

	assert.Equal(t, http.StatusNoContent, post(t, h, http.MethodPost, "/api/admin/neighbours", "s3cret", `{"port": 0, "call": "q9test", "alias": "nine", "quality": 100}`))
	assert.Equal(t, http.StatusBadRequest, post(t, h, http.MethodPost, "/api/admin/neighbours", "s3cret", `{"port": 0, "call": "q9test", "quality": 999}`))
	assert.Equal(t, http.StatusBadRequest, post(t, h, http.MethodPost, "/api/admin/neighbours", "s3cret", `{"bogus": 1}`))
	assert.Equal(t, http.StatusNoContent, post(t, h, http.MethodDelete, "/api/admin/neighbours/0/Q9TEST", "s3cret", ""))
	assert.Equal(t, http.StatusNotFound, post(t, h, http.MethodDelete, "/api/admin/neighbours/0/Q9TEST", "s3cret", ""))
	assert.Equal(t, http.StatusBadRequest, post(t, h, http.MethodDelete, "/api/admin/neighbours/x/Q9TEST", "s3cret", ""))

	var circuits []circuitJSON
	require.Equal(t, http.StatusOK, get(t, h, "/api/node/circuits", &circuits))
	require.Len(t, circuits, 1)

	assert.Equal(t, http.StatusNotFound, post(t, h, http.MethodPost, "/api/admin/circuits/FFFF/close", "s3cret", ""))
	assert.Equal(t, http.StatusNoContent, post(t, h, http.MethodPost, "/api/admin/circuits/"+circuits[0].ID+"/close", "s3cret", ""))

	assert.Equal(t, http.StatusBadRequest, post(t, h, http.MethodPost, "/api/admin/links/close", "s3cret", `{"port": 7, "local": "Q1TEST", "remote": "Q4TEST"}`))
	assert.Equal(t, http.StatusNoContent, post(t, h, http.MethodPost, "/api/admin/links/close", "s3cret", `{"port": 0, "local": "Q1TEST", "remote": "Q4TEST"}`))
}

func TestNodePublishesEvents(t *testing.T) {
	var f = newFakeNet(t)
	var a = f.add("Q1TEST", "ONE")
	f.add("Q2TEST", "TWO")

	var bus = nodeevents.NewBus(nil)
	a.netrom.events = bus

	var events, stop = bus.Subscribe(100)
	defer stop()

	f.tick()

	var kinds []string

	for len(events) > 0 {
		var e = <-events
		assert.Equal(t, "Q1TEST", e.Node)
		kinds = append(kinds, string(e.Kind))
	}

	assert.Equal(t, []string{"heard", "routes/changed"}, kinds, "TWO's NODES broadcast")
}

func TestShellObserver(t *testing.T) {
	var bus = nodeevents.NewBus(nil)

	var events, stop = bus.Subscribe(10)
	defer stop()

	var f = newFakeNet(t)
	var a = f.add("Q1TEST", "ONE")
	f.add("Q2TEST", "TWO")
	a.shell.SetObserver(shellObserver(bus, "Q1TEST"))
	f.tick()

	f.userConnects("Q4TEST", "Q1TEST")
	f.userTypes("Q4TEST", "Q1TEST", "C TWO\r")
	a.shell.LinkTerminated(0, nodeClient, "Q4TEST", "Q1TEST", false)
	f.pump()

	var got []string

	for len(events) > 0 {
		var e = <-events
		got = append(got, string(e.Kind)+" "+e.Role+" "+e.Remote)
	}

	assert.Equal(t, []string{"link/up user Q4TEST", "link/up downlink TWO", "link/down user Q4TEST"}, got)
}

func TestConfigNodeAdminAndMQTT(t *testing.T) {
	var c = parseYAMLConfig(t, `
channels:
  - channel: 0
    mycall: Q1TEST
node:
  address: Q1TEST
  adminToken: s3cret
mqtt:
  broker: tcp://broker.example.org:1883
  username: u
`)
	require.Zero(t, c.errors, c.output)
	assert.Equal(t, "s3cret", nodeAdminToken(c.misc))

	var cfg, ok = c.misc.mqtt.Get()
	require.True(t, ok)
	assert.Equal(t, "tcp://broker.example.org:1883", cfg.Broker)
	assert.Empty(t, cfg.ClientID, "made from the node's address at startup")

	c = parseYAMLConfig(t, "mqtt:\n  broker: http://nope\n")
	assert.NotZero(t, c.errors)
}
