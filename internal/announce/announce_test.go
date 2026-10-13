// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package announce

import (
	"context"
	"testing"

	"github.com/brutella/dnssd"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cancelledContext is what the announcement tests hand Announce: the
// message we care about is logged before the responder goroutine starts, and
// an already-cancelled context stops that goroutine putting mDNS traffic on
// the network, or logging, after the test has finished.
func cancelledContext(t *testing.T) context.Context {
	t.Helper()

	var ctx, cancel = context.WithCancel(context.Background())
	cancel()

	return ctx
}

// requireMDNSSockets skips a test on a machine where we cannot bind the mDNS
// sockets at all - a container with no multicast, say - rather than reporting
// the environment as a failure.  This is the same connection dnssd.NewResponder
// makes, opened directly because a Responder holds its sockets for good and
// offers no way to hand them back.
func requireMDNSSockets(t *testing.T) {
	t.Helper()

	var conn, err = dnssd.NewMDNSConn()
	if err != nil {
		t.Skipf("mDNS sockets unavailable here: %v", err)
	}

	conn.Close()
}

// announceLogged runs f and returns the entry it logged with message, failing
// the test if there is none.
func announceLogged(t *testing.T, message string, f func()) *logrus.Entry {
	t.Helper()

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	f()

	for _, entry := range hook.AllEntries() {
		if entry.Message == message {
			return entry
		}
	}

	require.Failf(t, "message not logged", "%q", message)

	return nil
}

//nolint:paralleltest // Serial: hooks logrus's global logger.
func TestDNSSDAnnounceLogsService(t *testing.T) {
	requireMDNSSockets(t)

	var entry = announceLogged(t, "DNS-SD: Announcing", func() {
		Announce(cancelledContext(t), []Service{{Name: "Q1TEST TNC", Type: KISSType, Port: 8001, Text: nil}})
	})

	assert.Equal(t, logrus.InfoLevel, entry.Level)
	assert.Equal(t, "Q1TEST TNC", entry.Data["name"])
	assert.Equal(t, KISSType, entry.Data["type"])
	assert.Equal(t, 8001, entry.Data["port"])
}

// Every service handed over is announced, through the one responder.
//
//nolint:paralleltest // Serial: hooks logrus's global logger.
func TestDNSSDAnnounceSeveralServices(t *testing.T) {
	requireMDNSSockets(t)

	var hook = test.NewGlobal()
	t.Cleanup(hook.Reset)

	Announce(cancelledContext(t), []Service{
		{Name: "Q1TEST TNC", Type: KISSType, Port: 8001, Text: nil},
		{Name: "Q1TEST TNC (port 8002)", Type: KISSType, Port: 8002, Text: nil},
	})

	var ports []any

	for _, entry := range hook.AllEntries() {
		if entry.Message == "DNS-SD: Announcing" {
			ports = append(ports, entry.Data["port"])
		}
	}

	assert.Equal(t, []any{8001, 8002}, ports)
}

// Nothing is listening on port 0, so there is nothing to announce - and saying
// so beats advertising a service nobody can connect to.  It is reported before
// any mDNS socket is opened, so this needs none.
//
//nolint:paralleltest // Serial: hooks logrus's global logger.
func TestDNSSDAnnounceRejectsPortZero(t *testing.T) {
	var hook = test.NewGlobal()
	t.Cleanup(hook.Reset)

	Announce(cancelledContext(t), []Service{{Name: "Q1TEST TNC", Type: KISSType, Port: 0, Text: nil}})

	var entries = hook.AllEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, logrus.ErrorLevel, entries[0].Level)
	assert.Equal(t, "DNS-SD: Failed to create service", entries[0].Message)
	assert.Contains(t, entries[0].Data, logrus.ErrorKey)
}

// Announcing nothing opens no sockets and logs nothing.
//
//nolint:paralleltest // Serial: hooks logrus's global logger.
func TestDNSSDAnnounceNothing(t *testing.T) {
	var hook = test.NewGlobal()
	t.Cleanup(hook.Reset)

	Announce(cancelledContext(t), nil)

	assert.Empty(t, hook.AllEntries())
}
