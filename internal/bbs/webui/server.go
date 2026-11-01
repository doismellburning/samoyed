// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package webui is samoyed-bbs's web interface: a read-only view, for the
// sysop, of every message in the store and of how forwarding to each partner
// stands.  Nothing on it changes anything.
//
// It shows personal mail as well as bulletins, and asks nobody to log in, so
// it belongs on a loopback address or behind a proxy that does the asking.
package webui

import (
	"context"
	"embed"
	"encoding/json"
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
)

// The page and everything it loads are built in.
//
//go:embed static
var staticFiles embed.FS

// Server is the web interface, bound to its address but not yet serving.
type Server struct {
	listener net.Listener
}

// NewServer binds addr, so that a failure to bind comes back here, before
// anything starts; "127.0.0.1:0" binds any free port, which Addr then says.
// ctx bounds only the binding.
func NewServer(ctx context.Context, addr string) (*Server, error) {
	var listener, err = new(net.ListenConfig).Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	var s = new(Server)
	s.listener = listener

	return s, nil
}

// Addr is where the web interface is listening.
func (s *Server) Addr() net.Addr {
	return s.listener.Addr()
}

// Serve serves handler until ctx is cancelled, then shuts down and returns
// nil; anything else that stops it is returned.
func (s *Server) Serve(ctx context.Context, handler http.Handler) error {
	var server = new(http.Server)
	server.Handler = handler
	server.ReadHeaderTimeout = readHeaderTimeout
	server.ReadTimeout = readTimeout
	server.WriteTimeout = writeTimeout
	server.IdleTimeout = idleTimeout
	server.BaseContext = func(net.Listener) context.Context { return ctx }

	var stop = context.AfterFunc(ctx, func() {
		var shutdownCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()

		_ = server.Shutdown(shutdownCtx)
	})
	defer stop()

	var err = server.Serve(s.listener)
	if ctx.Err() != nil {
		return nil
	}

	return err
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	err := json.NewEncoder(w).Encode(v) // := for errchkjson, which doesn't follow var.
	if err != nil {
		// Most likely the browser went away mid-response.
		logrus.WithError(err).Debug("BBS web UI response not sent")
	}
}

// securityHeaders keeps whatever arrives over the air - a title, a message -
// from ever being run as script, even if the page were to mishandle it.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
