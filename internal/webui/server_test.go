// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package webui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	var w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))

	return w
}

func TestAPIHandlers(t *testing.T) {
	var hub, _ = newTestHub()
	hub.AddChannel(0, "radio")

	var p = rx("Q1TEST")
	// Whatever arrives over the air is data, however it is dressed up.
	p.Comment = `<script>alert("x")</script>`
	hub.Publish(p)

	var h = Handler(hub)

	t.Run("packets", func(t *testing.T) {
		var w = get(t, h, "/api/packets")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var got []map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		require.Len(t, got, 1)
		assert.Equal(t, `<script>alert("x")</script>`, got[0]["comment"])
	})

	t.Run("stations", func(t *testing.T) {
		var got []map[string]any
		require.NoError(t, json.Unmarshal(get(t, h, "/api/stations").Body.Bytes(), &got))
		require.Len(t, got, 1)
		assert.Equal(t, "Q1TEST", got[0]["name"])
	})

	t.Run("channels", func(t *testing.T) {
		var got []Channel
		require.NoError(t, json.Unmarshal(get(t, h, "/api/channels").Body.Bytes(), &got))
		assert.Equal(t, []Channel{{Number: 0, Description: "radio", Received: 1, Transmitted: 0}}, got)
	})

	t.Run("security headers", func(t *testing.T) {
		var w = get(t, h, "/api/packets")
		assert.Contains(t, w.Header().Get("Content-Security-Policy"), "default-src 'self'")
		assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	})

	t.Run("page", func(t *testing.T) {
		var w = get(t, h, "/")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "<title>Samoyed</title>")
	})

	t.Run("assets", func(t *testing.T) {
		for _, path := range []string{"/static/app.js", "/static/app.css", "/static/vendor/leaflet/leaflet.js"} {
			assert.Equal(t, http.StatusOK, get(t, h, path).Code, path)
		}
	})

	t.Run("unknown path", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, get(t, h, "/nope").Code)
	})

	t.Run("no writes", func(t *testing.T) {
		var w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/packets", nil))
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestStartServesEventsAndShutsDown(t *testing.T) {
	var hub, _ = newTestHub()

	var ctx, cancel = context.WithCancel(t.Context())
	defer cancel()

	// Port 0 isn't a valid WEBPORT, but here it lets the kernel pick a
	// free one; we find out which from the listener's address below.
	var lc net.ListenConfig

	var probe, probeErr = lc.Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, probeErr)

	var addr, isTCP = probe.Addr().(*net.TCPAddr)
	require.True(t, isTCP)

	var port = addr.Port
	require.NoError(t, probe.Close())

	var errCh, err = Start(ctx, port, hub)
	require.NoError(t, err)

	var req, reqErr = http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/events", port), nil)
	require.NoError(t, reqErr)

	var resp, respErr = http.DefaultClient.Do(req)
	require.NoError(t, respErr)

	defer resp.Body.Close()

	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	var lines = bufio.NewScanner(resp.Body)
	require.True(t, lines.Scan())
	assert.Equal(t, ": connected", lines.Text())

	hub.Publish(rx("Q1TEST"))

	var events []string

	for lines.Scan() {
		if name, ok := strings.CutPrefix(lines.Text(), "event: "); ok {
			events = append(events, name)
		}

		if len(events) == 2 {
			break
		}
	}

	assert.Equal(t, []string{"packet", "station"}, events)

	// Cancelling must end the open stream too, not leave Shutdown
	// waiting on it.
	cancel()

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, http.ErrServerClosed)
	case <-time.After(shutdownTimeout / 2):
		t.Fatal("server did not shut down promptly with an event stream open")
	}
}
