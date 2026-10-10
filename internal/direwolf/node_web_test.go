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

	"github.com/doismellburning/samoyed/internal/webui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// webNet is two nodes that know each other, with a user connected to the
// first, whose goroutine is then left running to answer the web interface.
func webNet(t *testing.T, token string) http.Handler {
	t.Helper()

	var n = newFakeNet(t)
	var a = n.add("Q1TEST", "ONE")
	n.add("Q2TEST", "TWO")
	n.tick()
	n.userConnects("Q4TEST", "Q1TEST")
	n.userTypes("Q4TEST", "Q1TEST", "C TWO\r")

	var ctx, cancel = context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go a.run(ctx)

	var w = newNodeWeb(token)
	w.set(a)

	return webui.Handler(webui.NewHub(), w.routes()...)
}

func get(t *testing.T, h http.Handler, path string, v any) int {
	t.Helper()

	var rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))

	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), v))
	}

	return rec.Code
}

func TestNodeWebViews(t *testing.T) {
	var h = webNet(t, "")

	var info nodeInfoJSON
	require.Equal(t, http.StatusOK, get(t, h, "/api/node/info", &info))
	assert.Equal(t, "Q1TEST", info.Call)
	assert.Equal(t, "ONE", info.Alias)

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

	var links []linkJSON
	require.Equal(t, http.StatusOK, get(t, h, "/api/node/links", &links))
	require.Len(t, links, 2)
	assert.Equal(t, "Q2TEST", links[0].Remote)
	assert.Equal(t, "neighbour", links[0].Role)
	assert.Equal(t, "Q4TEST", links[1].Remote)
	assert.Equal(t, "user", links[1].Role)

	var heard []heardJSON
	assert.Equal(t, http.StatusOK, get(t, h, "/api/node/heard", &heard))

	// No token configured: no admin API.
	var rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/admin/nodes/broadcast", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestNodeWebWithoutNode(t *testing.T) {
	var h = webui.Handler(webui.NewHub(), newNodeWeb("token").routes()...)

	var info nodeInfoJSON
	assert.Equal(t, http.StatusNotFound, get(t, h, "/api/node/info", &info))

	var rec = httptest.NewRecorder()
	var r = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/admin/nodes/broadcast", nil)
	r.Header.Set("Authorization", "Bearer token")
	h.ServeHTTP(rec, r)
	assert.Equal(t, http.StatusNotFound, rec.Code)
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

	assert.Equal(t, http.StatusNotFound, post(t, h, http.MethodPost, "/api/admin/links/close", "s3cret", `{"port": 0, "local": "Q1TEST", "remote": "Q8TEST"}`))
	assert.Equal(t, http.StatusNoContent, post(t, h, http.MethodPost, "/api/admin/links/close", "s3cret", `{"port": 0, "local": "Q1TEST", "remote": "Q4TEST"}`))
}

func TestConfigNodeAndMQTT(t *testing.T) {
	var c = parseYAMLConfig(t, `
node:
  info: hello
  idleTimeout: 0s
  adminToken: s3cret
mqtt:
  broker: tcp://broker.example.org:1883
  username: u
`)
	require.Zero(t, c.errors, c.output)
	assert.Equal(t, "hello", c.misc.node.Info)
	assert.Zero(t, c.misc.node.IdleTimeout)
	assert.Equal(t, "s3cret", c.misc.node_admin_token)
	require.NotNil(t, c.misc.mqtt)
	assert.Equal(t, "tcp://broker.example.org:1883", c.misc.mqtt.Broker)
	assert.Empty(t, c.misc.mqtt.ClientID, "made from the node's callsign at startup")

	c = parseYAMLConfig(t, "mqtt:\n  broker: http://nope\n")
	assert.NotZero(t, c.errors)

	c = parseYAMLConfig(t, "node:\n  idleTimeout: -1s\n")
	assert.NotZero(t, c.errors)

	c = parseYAMLConfig(t, "")
	assert.Equal(t, defaultNodeIdleTimeout, c.misc.node.IdleTimeout)
}
