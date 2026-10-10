// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"net/http"
	"testing"

	"github.com/doismellburning/samoyed/internal/bbs"
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

func TestNodeBBSForwardsOverNetROM(t *testing.T) {
	var n = newFakeNet(t)
	var a = n.add("Q1TEST", "ONE")
	var b = n.add("Q2TEST", "TWO")

	require.NoError(t, a.addBBS(bbsSettings("Q1TEST-1", "ONEBBS", "Q2TEST-1"), t.TempDir()))
	require.NoError(t, b.addBBS(bbsSettings("Q2TEST-1", "TWOBBS", "Q1TEST-1"), t.TempDir()))

	n.tick()

	var d, ok = a.router.Table().Lookup("TWOBBS")
	require.True(t, ok, "B's BBS is advertised in its NODES broadcasts")
	assert.Equal(t, "Q2TEST-1", d.Call)

	// A user of A's node writes to a user of B's.
	n.userConnects("Q4TEST", "Q1TEST")
	n.userSaw("Q4TEST")
	n.userTypes("Q4TEST", "Q1TEST", "BBS\r")
	assert.Contains(t, n.userSaw("Q4TEST"), "[SAMOYED-")

	n.userTypes("Q4TEST", "Q1TEST", "S Q5TEST@Q2TEST\rHello\rFrom across the network.\r/EX\rB\r")
	var saw = n.userSaw("Q4TEST")
	assert.Contains(t, saw, "Message 1 saved.")
	assert.Contains(t, saw, "Returned to ONE:Q1TEST")

	a.do(func() { require.NoError(t, a.bbs.ForwardNow("Q2TEST-1")) })
	n.pump()

	var got = b.bbs.Messages()
	require.Len(t, got, 1, "A's BBS forwarded to B's over NET/ROM")
	assert.Equal(t, "Hello", got[0].Subject)
	assert.Equal(t, "Q1TEST-1", got[0].Origin)
	assert.Equal(t, []string{"Q2TEST-1"}, a.bbs.Messages()[0].Forwarded)

	// Its addressee reads it on B.
	n.userConnects("Q5TEST", "Q2TEST")
	n.userTypes("Q5TEST", "Q2TEST", "BBS\rR 1\r")
	assert.Contains(t, n.userSaw("Q5TEST"), "From across the network.")

	// A station can connect to the BBS's own callsign, skipping the shell.
	a.LinkEstablished(0, firstAppClient, "Q6TEST", "Q1TEST-1", true)
	n.pump()
	assert.Contains(t, n.userSaw("Q6TEST"), "Hello Q6TEST, this is the Q1TEST BBS.")
	assert.Equal(t, "bbs", a.sessions[axKey{port: 0, own: "Q1TEST-1", remote: "Q6TEST"}].role)

	a.RecConnData(0, firstAppClient, "Q6TEST", "Q1TEST-1", 0xf0, []byte("I\r"))
	n.pump()
	assert.Contains(t, n.userSaw("Q6TEST"), "Q1TEST BBS, #TEST.GBR.EURO.")

	// The admin API sees the messages and the partner.
	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	go a.run(ctx)

	var w = newNodeWeb("s3cret")
	w.set(a)

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
	assert.Equal(t, "/var/lib/samoyed", c.misc.data_dir)
	require.NotNil(t, c.misc.bbs)
	assert.Equal(t, "Q1TEST-1", c.misc.bbs.Call)
	assert.Equal(t, "ONEBBS", c.misc.bbs.Alias)
	assert.Equal(t, defaultBBSQuality, *c.misc.bbs.Quality)
	assert.Equal(t, "Q2TEST-1", c.misc.bbs.Partners[0].Call)

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

func TestNodeBBSNeedsDataDir(t *testing.T) {
	var n = newFakeNet(t)
	var a = n.add("Q1TEST", "ONE")

	require.Error(t, a.addBBS(bbsSettings("", "", "Q2TEST-1"), ""))
}
