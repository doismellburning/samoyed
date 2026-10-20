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

	return New(config, server.Client(), nil, nil)
}

func TestReport(t *testing.T) {
	t.Parallel()

	t.Run("with an alias", func(t *testing.T) {
		t.Parallel()

		var r = New(Config{Address: "Q1TEST-7", Alias: "SAMNOD", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil, Aliases: nil}, http.DefaultClient, nil, nil)
		assert.Equal(
			t,
			Report{
				NodeAlias:    "SAMNOD",
				Location:     maybe.Nothing[Location](),
				Software:     Software{Name: "Samoyed", Version: ""},
				Contact:      "",
				SysopComment: "",
				Ports:        nil,
				Neighbours:   nil,
				Aliases:      nil,
				MHeard:       nil,
			},
			r.Report(),
		)
	})

	t.Run("without an alias", func(t *testing.T) {
		t.Parallel()

		var r = New(Config{Address: "Q1TEST-7", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil, Aliases: nil}, http.DefaultClient, nil, nil)
		assert.Empty(t, r.Report().NodeAlias)
	})
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("reports to the public map by default", func(t *testing.T) {
		t.Parallel()

		assert.Equal(
			t,
			DefaultURL,
			New(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil, Aliases: nil}, http.DefaultClient, nil, nil).url,
		)
	})

	t.Run("reports to the map it is given", func(t *testing.T) {
		t.Parallel()

		assert.Equal(
			t,
			"http://map.example.org",
			New(
				Config{Address: "Q1TEST", Alias: "", APIURL: "http://map.example.org", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil, Aliases: nil},
				http.DefaultClient,
				nil,
				nil,
			).url,
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

func TestWhere(t *testing.T) {
	t.Parallel()

	t.Run("is the public map's page of nodes by default", func(t *testing.T) {
		t.Parallel()

		var r = New(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil, Aliases: nil}, http.DefaultClient, nil, nil)
		assert.Equal(t, "https://nodes.ukpacketradio.network/maps/packet", r.where())
	})

	t.Run("is the public map's page of nodes when its API is given", func(t *testing.T) {
		t.Parallel()

		var r = New(Config{Address: "Q1TEST", Alias: "", APIURL: DefaultURL + "/", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil, Aliases: nil}, http.DefaultClient, nil, nil)
		assert.Equal(t, "https://nodes.ukpacketradio.network/maps/packet", r.where())
	})

	t.Run("is the API of any other map", func(t *testing.T) {
		t.Parallel()

		var config = Config{Address: "Q1TEST", Alias: "", APIURL: "http://localhost:8080/", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil, Aliases: nil}
		var r = New(config, http.DefaultClient, nil, nil)
		assert.Equal(t, "http://localhost:8080", r.where())
	})
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
		var r = reporterFor(Config{Address: "Q1TEST-7", Alias: "SAMNOD", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil, Aliases: nil}, server)

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
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil, Aliases: nil}, server)

		require.NoError(t, r.send(t.Context()))
		assert.JSONEq(t, `{"software": {"name": "Samoyed"}}`, (<-requests).body)
	})

	t.Run("posts to a map at a URL ending in a slash", func(t *testing.T) {
		t.Parallel()

		var server, requests = fakeMap(t, http.StatusOK)
		var r = New(Config{Address: "Q1TEST", Alias: "", APIURL: server.URL + "/", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil, Aliases: nil}, server.Client(), nil, nil)

		require.NoError(t, r.send(t.Context()))
		assert.Equal(t, "/api/nodedata/Q1TEST", (<-requests).path)
	})

	t.Run("says where the node is", func(t *testing.T) {
		t.Parallel()

		var location = Location{Locator: "IO91", Coords: LatLon{Lat: 51.5, Lon: -1}, ASL: maybe.Nothing[int32]()}

		var server, requests = fakeMap(t, http.StatusOK)
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Just(location), Contact: "", Comment: "", Ports: nil, Aliases: nil}, server)

		require.NoError(t, r.send(t.Context()))
		assert.JSONEq(t, `{"location": {"locator": "IO91", "coords": {"lat": 51.5, "lon": -1}}, "software": {"name": "Samoyed"}}`, (<-requests).body)
	})

	t.Run("says how high the antenna is", func(t *testing.T) {
		t.Parallel()

		var location = Location{Locator: "IO91", Coords: LatLon{Lat: 51.5, Lon: -1}, ASL: maybe.Just[int32](120)}

		var server, requests = fakeMap(t, http.StatusOK)
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Just(location), Contact: "", Comment: "", Ports: nil, Aliases: nil}, server)

		require.NoError(t, r.send(t.Context()))
		assert.JSONEq(t, `{"location": {"locator": "IO91", "coords": {"lat": 51.5, "lon": -1}, "asl": 120}, "software": {"name": "Samoyed"}}`, (<-requests).body)
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
			Aliases:  nil,
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
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: ports, Aliases: nil}, server)

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
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: []Port{}, Aliases: nil}, server)

		require.NoError(t, r.send(t.Context()))
		assert.JSONEq(t, `{"software": {"name": "Samoyed"}, "ports": []}`, (<-requests).body)
	})

	t.Run("lists the node's other services by alias", func(t *testing.T) {
		t.Parallel()

		var server, requests = fakeMap(t, http.StatusOK)
		var r = reporterFor(Config{
			Address:  "Q1TEST",
			Alias:    "",
			APIURL:   "",
			Location: maybe.Nothing[Location](),
			Contact:  "",
			Comment:  "",
			Ports:    nil,
			Aliases:  map[string]Service{"SAMBBS": {Service: "BBS", Callsign: "Q1TEST-3"}},
		}, server)

		require.NoError(t, r.send(t.Context()))
		assert.JSONEq(t, `{"software": {"name": "Samoyed"}, "aliases": {"SAMBBS": {"service": "BBS", "callsign": "Q1TEST-3"}}}`, (<-requests).body)
	})

	t.Run("lists the links to neighbours that are up as each report is made", func(t *testing.T) {
		t.Parallel()

		var links []NeighbourLink

		var server, requests = fakeMap(t, http.StatusOK)
		var config = Config{Address: "Q1TEST", Alias: "", APIURL: server.URL, Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil, Aliases: nil}
		var r = New(config, server.Client(), func() []NeighbourLink { return links }, nil)

		require.NoError(t, r.send(t.Context()))
		assert.JSONEq(t, `{"software": {"name": "Samoyed"}}`, (<-requests).body)

		links = []NeighbourLink{{Node: "Q2TEST-7", Port: "0", Quality: maybe.Just[int32](192), Type: NeighbourNetROM}}

		require.NoError(t, r.send(t.Context()))
		assert.JSONEq(t, `{"software": {"name": "Samoyed"}, "neighbours": [{"node": "Q2TEST-7", "port": "0", "quality": 192, "type": "NetRom"}]}`, (<-requests).body)
	})

	t.Run("lists the stations heard as each report is made", func(t *testing.T) {
		t.Parallel()

		var heard []Heard

		var server, requests = fakeMap(t, http.StatusOK)
		var config = Config{Address: "Q1TEST", Alias: "", APIURL: server.URL, Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil, Aliases: nil}
		var r = New(config, server.Client(), nil, func() []Heard { return heard })

		require.NoError(t, r.send(t.Context()))
		assert.JSONEq(t, `{"software": {"name": "Samoyed"}}`, (<-requests).body)

		var bst = time.FixedZone("BST", 60*60)

		heard = []Heard{
			{
				Callsign:   "Q2TEST-7",
				Port:       "0",
				Packets:    maybe.Just[int64](42),
				FirstHeard: Timestamp{Time: time.Date(2026, 9, 6, 7, 8, 9, 500, bst)},
				LastHeard:  Timestamp{Time: time.Date(2026, 10, 20, 16, 46, 58, 0, time.UTC)},
				Coords:     maybe.Just(LatLon{Lat: 51.5, Lon: -1}),
			},
			{Callsign: "Q3TEST", Port: "10", Packets: maybe.Nothing[int64](), FirstHeard: Timestamp{Time: time.Time{}}, LastHeard: Timestamp{Time: time.Time{}}, Coords: maybe.Nothing[LatLon]()},
		}

		require.NoError(t, r.send(t.Context()))
		assert.JSONEq(t, `{"software": {"name": "Samoyed"}, "mheard": [
			{"callsign": "Q2TEST-7", "port": "0", "packets": 42, "firstHeard": "2026-09-06 06:08:09", "lastHeard": "2026-10-20 16:46:58", "coords": {"lat": 51.5, "lon": -1}},
			{"callsign": "Q3TEST", "port": "10"}
		]}`, (<-requests).body)
	})

	t.Run("a refused report is an error", func(t *testing.T) {
		t.Parallel()

		var server, _ = fakeMap(t, http.StatusTooManyRequests)
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil, Aliases: nil}, server)

		var err = r.send(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "429")
	})

	t.Run("an unreachable map is an error", func(t *testing.T) {
		t.Parallel()

		var server, _ = fakeMap(t, http.StatusOK)
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil, Aliases: nil}, server)

		server.Close()
		require.Error(t, r.send(t.Context()))
	})
}

func TestWait(t *testing.T) {
	t.Parallel()

	assert.Equal(t, settle, wait(1), "the second report follows once the node has settled")
	assert.Equal(t, interval, wait(2))
	assert.Equal(t, interval, wait(3))
}

func TestRun(t *testing.T) {
	t.Parallel()

	t.Run("reports at once, and returns when cancelled", func(t *testing.T) {
		t.Parallel()

		var server, requests = fakeMap(t, http.StatusOK)
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil, Aliases: nil}, server)

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
		var r = reporterFor(Config{Address: "Q1TEST", Alias: "", APIURL: "", Location: maybe.Nothing[Location](), Contact: "", Comment: "", Ports: nil, Aliases: nil}, server)

		var ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()

		// Returns once cancelled, not when the report fails.
		var start = time.Now()

		r.Run(ctx)
		assert.GreaterOrEqual(t, time.Since(start), 100*time.Millisecond)
	})
}
