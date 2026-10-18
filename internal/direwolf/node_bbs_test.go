// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/bbs"
	"github.com/doismellburning/samoyed/internal/nodeevents"
	"github.com/doismellburning/samoyed/internal/webui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bbsSettings(call string, alias string, partner string) BBSSettings {
	var quality = 200

	return BBSSettings{
		Call: call, Alias: alias, Quality: &quality, HRoute: "#TEST.GBR.EURO",
		Partners: []BBSPartnerSettings{{
			Call: partner, Node: partner, Channel: 0, Via: nil, Script: nil,
			Routes: []string{"*"}, Bulletins: true, Interval: nil,
		}},
	}
}

// withBBS gives n a BBS on NET/ROM's goroutine, which pump stands in for.
// The fake node is already started, which the shell takes no notice of.
func withBBS(t *testing.T, n *fakeNode, settings BBSSettings, bus *nodeevents.Bus) *nodeApps {
	t.Helper()

	var apps = newNodeApps(t.Context(), n.netrom)

	// addBBS waits for NET/ROM's goroutine to listen at the BBS's
	// callsign, so for now there needs to be one.
	var ctx, cancel = context.WithCancel(t.Context())
	var done = make(chan struct{})

	go func() {
		n.netrom.run(ctx)
		close(done)
	}()

	require.NoError(t, apps.addBBS(t.Context(), settings, t.TempDir(), n.config, n.shell, n.netrom, bus))

	// Gone before pump takes over.
	cancel()
	<-done

	return apps
}

func getAuthed(t *testing.T, h http.Handler, path string, token string, v any) int {
	t.Helper()

	var rec = httptest.NewRecorder()
	var r = httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, r)

	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), v))
	}

	return rec.Code
}

func TestNodeBBSForwardsOverNetROM(t *testing.T) {
	var f = newFakeNet(t)
	var a = f.add("Q1TEST", "ONE")
	var b = f.add("Q2TEST", "TWO")

	var bus = nodeevents.NewBus(nil)

	var events, stop = bus.Subscribe(10)
	defer stop()

	var aApps = withBBS(t, a, bbsSettings("Q1TEST-1", "ONEBBS", "Q2TEST-1"), bus)
	var bApps = withBBS(t, b, bbsSettings("Q2TEST-1", "TWOBBS", "Q1TEST-1"), nodeevents.NewBus(nil))

	f.tick()

	var d, ok = a.netrom.router.Table().Lookup("TWOBBS")
	require.True(t, ok, "B's BBS is advertised in its NODES broadcasts")
	assert.Equal(t, "Q2TEST-1", d.Call)

	// A user of A's node writes to a user of B's.
	f.userConnects("Q4TEST", "Q1TEST")
	f.userSaw("Q4TEST")
	f.userTypes("Q4TEST", "Q1TEST", "BBS\r")
	assert.Contains(t, f.userSaw("Q4TEST"), "[SAMOYED-")

	f.userTypes("Q4TEST", "Q1TEST", "S Q5TEST@Q2TEST\rHello\rFrom across the network.\r/EX\rB\r")
	var saw = f.userSaw("Q4TEST")
	assert.Contains(t, saw, "Message 1 saved.")
	assert.Contains(t, saw, "Back at the node")

	require.Len(t, events, 1)
	var e = <-events
	assert.Equal(t, nodeevents.BBSMessage, e.Kind)
	assert.Equal(t, "Q5TEST", e.To)
	assert.Equal(t, "Q2TEST", e.At)

	a.netrom.do(func() { require.NoError(t, aApps.bbs.ForwardNow("Q2TEST-1")) })
	f.pump()

	var got = bApps.bbs.Messages()
	require.Len(t, got, 1, "A's BBS forwarded to B's over NET/ROM")
	assert.Equal(t, "Hello", got[0].Subject)
	assert.Equal(t, "Q1TEST-1", got[0].Origin)
	assert.Equal(t, []string{"Q2TEST-1"}, aApps.bbs.Messages()[0].Forwarded)

	// Its addressee reads it on B.
	f.userConnects("Q5TEST", "Q2TEST")
	f.userTypes("Q5TEST", "Q2TEST", "BBS\rR 1\r")
	assert.Contains(t, f.userSaw("Q5TEST"), "From across the network.")

	// A station can connect to the BBS's own callsign, skipping the shell.
	a.shell.LinkEstablished(0, nodeClient, "Q6TEST", "Q1TEST-1", true)
	f.pump()
	assert.Contains(t, f.userSaw("Q6TEST"), "Hello Q6TEST, this is the Q1TEST BBS.")

	a.shell.RecConnData(0, nodeClient, "Q6TEST", "Q1TEST-1", ax25.PIDNoLayer3, []byte("I\r"))
	f.pump()
	assert.Contains(t, f.userSaw("Q6TEST"), "Q1TEST BBS, #TEST.GBR.EURO.")

	// The admin API sees the messages and the partner.
	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	go a.netrom.run(ctx)

	var w = newNodeWeb("s3cret")
	w.set(&webNode{config: a.config, shell: a.shell, netrom: a.netrom, apps: aApps, link: fakeLink{n: a}, heard: noneHeard})

	var h = webui.Handler(webui.NewHub(), w.routes()...)

	assert.Equal(t, http.StatusUnauthorized, post(t, h, http.MethodGet, "/api/admin/bbs/messages", "", ""))

	var partners []bbs.PartnerStatus
	require.Equal(t, http.StatusOK, getAuthed(t, h, "/api/admin/bbs/partners", "s3cret", &partners))
	require.Len(t, partners, 1)
	assert.Equal(t, "Q2TEST-1", partners[0].Call)

	var messages []bbsMessageJSON
	require.Equal(t, http.StatusOK, getAuthed(t, h, "/api/admin/bbs/messages", "s3cret", &messages))
	require.Len(t, messages, 1)
	assert.Equal(t, "Hello", messages[0].Subject)

	assert.Equal(t, http.StatusNotFound, post(t, h, http.MethodPost, "/api/admin/bbs/partners/Q9TEST/forward", "s3cret", ""))
	assert.Equal(t, http.StatusNotFound, post(t, h, http.MethodDelete, "/api/admin/bbs/messages/99", "s3cret", ""))
	assert.Equal(t, http.StatusNoContent, post(t, h, http.MethodDelete, "/api/admin/bbs/messages/1", "s3cret", ""))
}

func TestNodeWebWithoutBBS(t *testing.T) {
	var h = webNet(t, "s3cret")

	var messages []bbsMessageJSON
	assert.Equal(t, http.StatusNotFound, getAuthed(t, h, "/api/admin/bbs/messages", "s3cret", &messages))
}

func TestConfigBBS(t *testing.T) {
	var c = parseYAMLConfig(t, `
dataDir: /var/lib/samoyed
bbs:
  call: q1test-1
  alias: onebbs
  hroute: "#HANTS.GBR.EURO"
  partners:
    - call: q2test-1
      node: TWOBBS
      routes: ["*"]
      bulletins: true
      interval: 1h
`)
	require.Zero(t, c.errors, c.output)
	assert.Equal(t, "/var/lib/samoyed", c.misc.dataDir)

	var settings, ok = c.misc.bbs.Get()
	require.True(t, ok)
	assert.Equal(t, "Q1TEST-1", settings.Call)
	assert.Equal(t, "ONEBBS", settings.Alias)
	assert.Equal(t, defaultBBSQuality, *settings.Quality)
	assert.Equal(t, "Q2TEST-1", settings.Partners[0].Call)

	for name, yaml := range map[string]string{
		"bad call":       "bbs:\n  call: not a call\n",
		"bad quality":    "bbs:\n  quality: 300\n",
		"no routes":      "bbs:\n  partners:\n    - call: Q2TEST\n",
		"twice":          "bbs:\n  partners:\n    - {call: Q2TEST, routes: ['*']}\n    - {call: q2test, routes: ['*']}\n",
		"short interval": "bbs:\n  partners:\n    - {call: Q2TEST, routes: ['*'], interval: 5s}\n",
	} {
		t.Run(name, func(t *testing.T) {
			var bad = parseYAMLConfig(t, yaml)
			assert.NotZero(t, bad.errors, bad.output)
		})
	}
}

func TestNodeBBSRejects(t *testing.T) {
	var f = newFakeNet(t)
	var a = f.add("Q1TEST", "ONE")
	var apps = newNodeApps(t.Context(), nil)

	var ax25Partner = bbsSettings("Q1TEST-1", "", "Q2TEST-1")
	ax25Partner.Partners[0].Node = ""

	var onChannel3 = bbsSettings("Q1TEST-1", "", "Q2TEST-1")
	onChannel3.Partners[0].Node = ""
	onChannel3.Partners[0].Channel = 3

	for name, test := range map[string]struct {
		settings BBSSettings
		dataDir  string
		netrom   *netromNode
	}{
		"no dataDir":          {settings: ax25Partner, dataDir: "", netrom: a.netrom},
		"the node's own call": {settings: bbsSettings("Q1TEST", "", "Q2TEST-1"), dataDir: t.TempDir(), netrom: a.netrom},
		"NET/ROM partner":     {settings: bbsSettings("Q1TEST-1", "", "Q2TEST-1"), dataDir: t.TempDir(), netrom: nil},
		"not a node port":     {settings: onChannel3, dataDir: t.TempDir(), netrom: a.netrom},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, apps.addBBS(t.Context(), test.settings, test.dataDir, a.config, a.shell, test.netrom, nodeevents.NewBus(nil)))
		})
	}
}
