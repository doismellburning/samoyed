// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package webui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 5 * time.Second

	// keepaliveInterval is how often an idle event stream gets a comment
	// line, so a proxy in between doesn't take it for dead.
	keepaliveInterval = 15 * time.Second
)

// The page and everything it loads, Leaflet included, are built in, so the
// interface works on a station with no route to a CDN.  Only the map tiles
// come from outside.
//
//go:embed static
var staticFiles embed.FS

// Handler returns the web interface's routes, reading from hub.
func Handler(hub *Hub) http.Handler {
	var mux = http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFiles, "static/index.html")
	})
	mux.Handle("GET /static/", http.FileServerFS(staticFiles))

	mux.HandleFunc("GET /api/packets", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, hub.Packets())
	})
	mux.HandleFunc("GET /api/stations", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, hub.Stations())
	})
	mux.HandleFunc("GET /api/channels", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, hub.Channels())
	})
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		serveEvents(w, r, hub)
	})

	return securityHeaders(mux)
}

// Serve serves the web interface on listener, which the caller has already
// bound, as with metrics.Serve.  The returned channel receives a single error
// if and when the server stops.  Logging is the caller's responsibility.
//
// Cancelling ctx shuts the server down, event streams included, closing
// listener, so the channel then reports http.ErrServerClosed.
func Serve(ctx context.Context, listener net.Listener, hub *Hub) <-chan error {
	var server = new(http.Server)
	server.Handler = Handler(hub)
	server.ReadHeaderTimeout = readHeaderTimeout
	server.ReadTimeout = readTimeout
	server.WriteTimeout = writeTimeout
	server.IdleTimeout = idleTimeout
	// An event stream never goes idle, so Shutdown would wait on it until
	// its deadline.  Deriving every request's context from ctx ends the
	// streams as soon as ctx is cancelled.
	server.BaseContext = func(net.Listener) context.Context { return ctx }

	var errCh = make(chan error, 1)

	go func() {
		errCh <- server.Serve(listener)
	}()

	context.AfterFunc(ctx, func() {
		var shutdownCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()

		_ = server.Shutdown(shutdownCtx)
	})

	return errCh
}

// serveEvents streams the Hub's events to one page as Server-Sent Events until
// the page goes away, the server shuts down, or the Hub cuts the page off for
// falling behind - in which case the browser's EventSource reconnects by
// itself and the page re-fetches its snapshot.
func serveEvents(w http.ResponseWriter, r *http.Request, hub *Hub) {
	if hub == nil {
		http.NotFound(w, r)

		return
	}

	var rc = http.NewResponseController(w)

	// The server's write timeout is for ordinary responses; this one is
	// meant to run for as long as the page is open.
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	var sub = hub.subscribe()
	defer hub.unsubscribe(sub)

	var _, err = fmt.Fprint(w, ": connected\n\n")
	if err != nil || rc.Flush() != nil {
		return
	}

	var keepalive = time.NewTicker(keepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-sub.events:
			if !ok {
				return
			}

			_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.name, e.data)
		case <-keepalive.C:
			_, err = fmt.Fprint(w, ": keepalive\n\n")
		}

		if err != nil || rc.Flush() != nil {
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	err := json.NewEncoder(w).Encode(v) // := for errchkjson, which doesn't follow var.
	if err != nil {
		// Most likely the browser went away mid-response; there's no
		// one left to tell.
		logrus.WithError(err).Debug("Web UI response not sent")
	}
}

// securityHeaders keeps whatever arrives over the air - a callsign, a comment -
// from ever being run as script, even if the page were to mishandle it.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data: https://tile.openstreetmap.org; "+
				"object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// OpenStreetMap's tile usage policy wants a Referer.
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}
