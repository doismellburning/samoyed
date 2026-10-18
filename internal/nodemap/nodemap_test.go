// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package nodemap

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// received is a request the fake map was sent.
type received struct {
	method      string
	path        string
	contentType string
	userAgent   string
	body        string
}

// fakeMap is a map that remembers what it is sent, and answers with status.
func fakeMap(t *testing.T, status int) (*httptest.Server, <-chan received) {
	t.Helper()

	var requests = make(chan received, 10)

	var server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body, _ = io.ReadAll(req.Body)
		requests <- received{
			method:      req.Method,
			path:        req.URL.EscapedPath(),
			contentType: req.Header.Get("Content-Type"),
			userAgent:   req.Header.Get("User-Agent"),
			body:        string(body),
		}

		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)

	return server, requests
}

// reporterFor is a reporter for config that reports to server.
func reporterFor(config Config, server *httptest.Server) *Reporter {
	var r = New(config, server.Client())
	r.url = server.URL

	return r
}

func TestReport(t *testing.T) {
	t.Parallel()

	t.Run("with an alias", func(t *testing.T) {
		t.Parallel()

		var r = New(Config{Address: "Q1TEST-7", Alias: "SAMNOD"}, http.DefaultClient)
		assert.Equal(t, Report{NodeAlias: "SAMNOD", Software: Software{Name: "Samoyed", Version: ""}}, r.Report())
	})

	t.Run("without an alias", func(t *testing.T) {
		t.Parallel()

		var r = New(Config{Address: "Q1TEST-7", Alias: ""}, http.DefaultClient)
		assert.Empty(t, r.Report().NodeAlias)
	})
}

func TestSend(t *testing.T) {
	t.Parallel()

	t.Run("posts the report as JSON to the node's own path", func(t *testing.T) {
		t.Parallel()

		var server, requests = fakeMap(t, http.StatusOK)
		var r = reporterFor(Config{Address: "Q1TEST-7", Alias: "SAMNOD"}, server)

		require.NoError(t, r.send(t.Context()))

		var got = <-requests
		assert.Equal(t, http.MethodPost, got.method)
		assert.Equal(t, "/api/nodedata/Q1TEST-7", got.path)
		assert.Equal(t, "application/json", got.contentType)
		assert.Equal(t, "Samoyed", got.userAgent, "with no version, as in a test")
		assert.JSONEq(t, `{"nodeAlias": "SAMNOD", "software": {"name": "Samoyed"}}`, got.body)
	})

	t.Run("leaves out an alias the node has not got", func(t *testing.T) {
		t.Parallel()

		var server, requests = fakeMap(t, http.StatusOK)
		var r = reporterFor(Config{Address: "Q1TEST", Alias: ""}, server)

		require.NoError(t, r.send(t.Context()))
		assert.JSONEq(t, `{"software": {"name": "Samoyed"}}`, (<-requests).body)
	})

	t.Run("a refused report is an error", func(t *testing.T) {
		t.Parallel()

		var server, _ = fakeMap(t, http.StatusTooManyRequests)
		var r = reporterFor(Config{Address: "Q1TEST", Alias: ""}, server)

		var err = r.send(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "429")
	})

	t.Run("an unreachable map is an error", func(t *testing.T) {
		t.Parallel()

		var server, _ = fakeMap(t, http.StatusOK)
		var r = reporterFor(Config{Address: "Q1TEST", Alias: ""}, server)

		server.Close()
		require.Error(t, r.send(t.Context()))
	})
}

func TestRun(t *testing.T) {
	t.Parallel()

	t.Run("reports at once, and returns when cancelled", func(t *testing.T) {
		t.Parallel()

		var server, requests = fakeMap(t, http.StatusOK)
		var r = reporterFor(Config{Address: "Q1TEST", Alias: ""}, server)

		var ctx, cancel = context.WithCancel(t.Context())

		var done = make(chan struct{})

		go func() {
			r.Run(ctx)
			close(done)
		}()

		select {
		case <-requests:
		case <-time.After(10 * time.Second):
			t.Fatal("no report sent")
		}

		cancel()

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("Run did not return once cancelled")
		}
	})

	t.Run("carries on after a failed report", func(t *testing.T) {
		t.Parallel()

		var server, _ = fakeMap(t, http.StatusInternalServerError)
		var r = reporterFor(Config{Address: "Q1TEST", Alias: ""}, server)

		var ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()

		// Returns once cancelled, not when the report fails.
		var start = time.Now()

		r.Run(ctx)
		assert.GreaterOrEqual(t, time.Since(start), 100*time.Millisecond)
	})
}
