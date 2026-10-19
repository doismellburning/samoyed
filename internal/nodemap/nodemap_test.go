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

	"github.com/doismellburning/samoyed/internal/maybe"
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
	config.APIURL = server.URL

	return New(config, server.Client())
}

func TestReport(t *testing.T) {
	t.Parallel()

	t.Run("with an alias", func(t *testing.T) {
		t.Parallel()

		var r = New(Config{Address: "Q1TEST-7", Alias: "SAMNOD", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil}, http.DefaultClient)
		assert.Equal(t, Report{NodeAlias: "SAMNOD", Location: maybe.Nothing[Location](), Software: Software{Name: "Samoyed", Version: ""}, Contact: "", SysopComment: "", Ports: nil}, r.Report())
	})

	t.Run("without an alias", func(t *testing.T) {
		t.Parallel()

		var r = New(Config{Address: "Q1TEST-7", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil}, http.DefaultClient)
		assert.Empty(t, r.Report().NodeAlias)
	})
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("reports to the public map by default", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, DefaultURL, New(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil}, http.DefaultClient).url)
	})

	t.Run("reports to the map it is given", func(t *testing.T) {
		t.Parallel()

		assert.Equal(
			t,
			"http://map.example.org",
			New(Config{Address: "Q1TEST", Alias: "", APIURL: "http://map.example.org", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil}, http.DefaultClient).url,
		)
	})
}

func TestParseLinkType(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"RF", "PrivateNet", "Internet", "Other"} {
		var linkType, ok = ParseLinkType(name)
		assert.True(t, ok, name)
		assert.Equal(t, LinkType(name), linkType)
	}

	for _, name := range []string{"", "rf", "AXUDP"} {
		var _, ok = ParseLinkType(name)
		assert.False(t, ok, name)
	}
}

func TestLocationAt(t *testing.T) {
	t.Parallel()

	t.Run("is the middle of the square", func(t *testing.T) {
		t.Parallel()

		var location, err = LocationAt("IO91")
		require.NoError(t, err)
		assert.Equal(t, "IO91", location.Locator)
		assert.InDelta(t, 51.5, location.Coords.Lat, 1e-9)
		assert.InDelta(t, -1, location.Coords.Lon, 1e-9)
	})

	t.Run("is not a locator that is not one", func(t *testing.T) {
		t.Parallel()

		var _, err = LocationAt("IO9")
		require.Error(t, err)
	})
}

func TestSend(t *testing.T) {
	t.Parallel()

	t.Run("posts the report as JSON to the node's own path", func(t *testing.T) {
		t.Parallel()

		var server, requests = fakeMap(t, http.StatusOK)
		var r = reporterFor(Config{Address: "Q1TEST-7", Alias: "SAMNOD", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil}, server)

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
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil}, server)

		require.NoError(t, r.send(t.Context()))
		assert.JSONEq(t, `{"software": {"name": "Samoyed"}}`, (<-requests).body)
	})

	t.Run("posts to a map at a URL ending in a slash", func(t *testing.T) {
		t.Parallel()

		var server, requests = fakeMap(t, http.StatusOK)
		var r = New(Config{Address: "Q1TEST", Alias: "", APIURL: server.URL + "/", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil}, server.Client())

		require.NoError(t, r.send(t.Context()))
		assert.Equal(t, "/api/nodedata/Q1TEST", (<-requests).path)
	})

	t.Run("says where the node is", func(t *testing.T) {
		t.Parallel()

		var location = Location{Locator: "IO91", Coords: LatLon{Lat: 51.5, Lon: -1}}

		var server, requests = fakeMap(t, http.StatusOK)
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Just(location), Contact: "", Comment: "", Ports: nil}, server)

		require.NoError(t, r.send(t.Context()))
		assert.JSONEq(t, `{"location": {"locator": "IO91", "coords": {"lat": 51.5, "lon": -1}}, "software": {"name": "Samoyed"}}`, (<-requests).body)
	})

	t.Run("says who to contact, with a comment", func(t *testing.T) {
		t.Parallel()

		var server, requests = fakeMap(t, http.StatusOK)
		var r = reporterFor(Config{
			Address:  "Q1TEST",
			Alias:    "",
			APIURL:   "",
			Location: maybe.Nothing[Location](),
			Contact:  "sysop@example.org",
			Comment:  "Club node",
			Ports:    nil,
		}, server)

		require.NoError(t, r.send(t.Context()))
		assert.JSONEq(t, `{"contact": "sysop@example.org", "sysopComment": "Club node", "software": {"name": "Samoyed"}}`, (<-requests).body)
	})

	t.Run("lists the ports, leaving out what is not known", func(t *testing.T) {
		t.Parallel()

		var ports = []Port{
			{
				ID:         "0",
				LinkType:   LinkRF,
				Freq:       maybe.Just[int64](144950000),
				Mode:       "AX.25",
				Modulation: "AFSK",
				Baud:       maybe.Just[int32](1200),
				Bitrate:    maybe.Just[int32](1200),
				Usage:      "Access",
				Comment:    "Hilltop",
			},
			{
				ID:         "10",
				LinkType:   LinkInternet,
				Freq:       maybe.Nothing[int64](),
				Mode:       "",
				Modulation: "",
				Baud:       maybe.Nothing[int32](),
				Bitrate:    maybe.Nothing[int32](),
				Usage:      "",
				Comment:    "",
			},
		}

		var server, requests = fakeMap(t, http.StatusOK)
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: ports}, server)

		require.NoError(t, r.send(t.Context()))
		assert.JSONEq(t, `{"software": {"name": "Samoyed"}, "ports": [
			{"id": "0", "linkType": "RF", "freq": 144950000, "mode": "AX.25", "modulation": "AFSK", "baud": 1200, "bitrate": 1200, "usage": "Access", "comment": "Hilltop"},
			{"id": "10", "linkType": "Internet"}
		]}`, (<-requests).body)
	})

	// A node whose every port is left off the map has none to report,
	// which is not the same as saying nothing about them.
	t.Run("says when there are no ports", func(t *testing.T) {
		t.Parallel()

		var server, requests = fakeMap(t, http.StatusOK)
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: []Port{}}, server)

		require.NoError(t, r.send(t.Context()))
		assert.JSONEq(t, `{"software": {"name": "Samoyed"}, "ports": []}`, (<-requests).body)
	})

	t.Run("a refused report is an error", func(t *testing.T) {
		t.Parallel()

		var server, _ = fakeMap(t, http.StatusTooManyRequests)
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil}, server)

		var err = r.send(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "429")
	})

	t.Run("an unreachable map is an error", func(t *testing.T) {
		t.Parallel()

		var server, _ = fakeMap(t, http.StatusOK)
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil}, server)

		server.Close()
		require.Error(t, r.send(t.Context()))
	})
}

func TestRun(t *testing.T) {
	t.Parallel()

	t.Run("reports at once, and returns when cancelled", func(t *testing.T) {
		t.Parallel()

		var server, requests = fakeMap(t, http.StatusOK)
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil}, server)

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
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil}, server)

		var ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()

		// Returns once cancelled, not when the report fails.
		var start = time.Now()

		r.Run(ctx)
		assert.GreaterOrEqual(t, time.Since(start), 100*time.Millisecond)
	})
}
