// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/chat"
	"github.com/doismellburning/samoyed/internal/node"
	"github.com/doismellburning/samoyed/internal/nodeevents"
	"github.com/doismellburning/samoyed/internal/webui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeChat(t *testing.T) {
	var f = newFakeNet(t)
	var a = f.add("Q1TEST", "ONE")

	var bus = nodeevents.NewBus(nil)

	var events, stop = bus.Subscribe(10)
	defer stop()

	// On NET/ROM's goroutine, which pump stands in for.  The fake node
	// is already started, which the shell takes no notice of.
	var apps = newNodeApps(t.Context(), a.netrom)
	require.NoError(t, apps.addChat(a.shell, chat.DefaultConfig(), bus, "Q1TEST"))

	f.userConnects("Q4TEST", "Q1TEST")
	f.userConnects("Q5TEST", "Q1TEST")
	f.userSaw("Q4TEST")
	f.userSaw("Q5TEST")

	f.userTypes("Q4TEST", "Q1TEST", "?\r")
	assert.Contains(t, f.userSaw("Q4TEST"), "CHAT - ", "chat is a command of the shell")

	f.userTypes("Q4TEST", "Q1TEST", "CHAT\r")
	f.userTypes("Q5TEST", "Q1TEST", "chat\r")
	f.userSaw("Q5TEST")
	assert.Contains(t, f.userSaw("Q4TEST"), "*** Q5TEST has joined General.")

	f.userTypes("Q4TEST", "Q1TEST", "hello\r")
	assert.Equal(t, "<Q4TEST> hello\n", f.userSaw("Q5TEST"))

	require.Len(t, events, 2)
	var e = <-events
	assert.Equal(t, nodeevents.ChatJoin, e.Kind)
	assert.Equal(t, "Q4TEST", e.User)
	assert.Equal(t, "General", e.Room)
	assert.Equal(t, "Q1TEST", e.Node)
	<-events

	f.nodes["Q1TEST"].shell.LinkTerminated(0, nodeClient, "Q5TEST", "Q1TEST", false)
	f.pump()
	assert.Contains(t, f.userSaw("Q4TEST"), "*** Q5TEST has disconnected.", "a user who goes leaves chat")
	assert.Equal(t, nodeevents.ChatLeave, (<-events).Kind)

	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	go a.netrom.run(ctx)

	var w = newNodeWeb("s3cret")
	w.set(&webNode{config: a.config, shell: a.shell, netrom: a.netrom, apps: apps, link: fakeLink{n: a}, heard: noneHeard})

	var h = webui.Handler(webui.NewHub(), w.routes()...)

	var rooms []chat.Room
	require.Equal(t, http.StatusOK, get(t, h, "/api/node/chat", &rooms))
	assert.Equal(t, []chat.Room{{Name: "General", Users: []string{"Q4TEST"}}}, rooms)

	var sessions []sessionJSON
	require.Equal(t, http.StatusOK, get(t, h, "/api/node/sessions", &sessions))
	require.Len(t, sessions, 1)
	assert.Equal(t, "CHAT", sessions[0].App)

	assert.Equal(t, http.StatusUnauthorized, post(t, h, http.MethodPost, "/api/admin/chat/announce", "", `{"text": "hi"}`))
	assert.Equal(t, http.StatusBadRequest, post(t, h, http.MethodPost, "/api/admin/chat/announce", "s3cret", `{"text": "two\nlines"}`))
	assert.Equal(t, http.StatusNoContent, post(t, h, http.MethodPost, "/api/admin/chat/announce", "s3cret", `{"text": "rebooting soon"}`))

	// The announcement is made by the time the response comes back.
	assert.Contains(t, f.users["Q4TEST"].String(), "*** Announcement: rebooting soon")
}

// TestNodeChatWithoutNetROM has the chat server on a goroutine of its own.
func TestNodeChatWithoutNetROM(t *testing.T) {
	var f = newFakeNet(t)

	var config = node.Config{
		Address: "Q1TEST", Alias: "", Ports: []node.Port{{Channel: 0, Description: "Test"}},
		InfoMessage: "", ConnectMessage: "",
	}

	var n = &fakeNode{net: f, call: "Q1TEST", config: config, shell: nil, netrom: nil}
	n.shell = node.New(config, fakeLink{n: n}, noneHeard, nodeClient)
	f.nodes["Q1TEST"] = n

	var apps = newNodeApps(t.Context(), nil)
	require.NoError(t, apps.addChat(n.shell, chat.DefaultConfig(), nodeevents.NewBus(nil), "Q1TEST"))
	n.shell.Start()

	n.shell.LinkEstablished(0, nodeClient, "Q4TEST", "Q1TEST", true)
	n.shell.RecConnData(0, nodeClient, "Q4TEST", "Q1TEST", 0xF0, []byte("CHAT\r/who\r"))

	// Waiting on the applications' goroutine for nothing in particular
	// waits for everything queued before it.
	require.NoError(t, apps.query(t.Context(), func() {}))

	var saw = f.userSaw("Q4TEST")
	assert.Contains(t, saw, "*** You are in General.")
	assert.Equal(t, 2, strings.Count(saw, "*** In General: Q4TEST"), "joining says who is there, and so does /who")

	var w = newNodeWeb("")
	w.set(&webNode{config: config, shell: n.shell, netrom: nil, apps: apps, link: fakeLink{n: n}, heard: noneHeard})

	var h = webui.Handler(webui.NewHub(), w.routes()...)

	var rooms []chat.Room
	require.Equal(t, http.StatusOK, get(t, h, "/api/node/chat", &rooms))
	assert.Len(t, rooms, 1)
}

func TestNodeWebWithoutChat(t *testing.T) {
	var h = webNet(t, "s3cret")

	var rooms []chat.Room
	require.Equal(t, http.StatusOK, get(t, h, "/api/node/chat", &rooms))
	assert.Empty(t, rooms)
	assert.Equal(t, http.StatusNotFound, post(t, h, http.MethodPost, "/api/admin/chat/announce", "s3cret", `{"text": "hi"}`))
}

func TestConfigNodeChat(t *testing.T) {
	var node = "channels:\n  - channel: 0\n    mycall: Q1TEST\nnode:\n  address: Q1TEST\n"

	var c = parseYAMLConfig(t, node+"  chat:\n    defaultRoom: lobby\n    rateLines: 0\n    rateWindow: 1m\n")
	require.Zero(t, c.errors, c.output)

	var cfg, ok = nodeChatConfig(c.misc).Get()
	require.True(t, ok)
	assert.Equal(t, "lobby", cfg.DefaultRoom)
	assert.Zero(t, cfg.RateLines)
	assert.Equal(t, time.Minute, cfg.RateWindow)

	c = parseYAMLConfig(t, node+"  chat: {}\n")
	require.Zero(t, c.errors, c.output)

	cfg, ok = nodeChatConfig(c.misc).Get()
	require.True(t, ok)
	assert.Equal(t, chat.DefaultRoom, cfg.DefaultRoom)

	c = parseYAMLConfig(t, node+"  chat:\n    defaultRoom: not valid\n")
	assert.NotZero(t, c.errors)

	c = parseYAMLConfig(t, node)
	require.Zero(t, c.errors, c.output)
	assert.True(t, nodeChatConfig(c.misc).IsNothing())
}
