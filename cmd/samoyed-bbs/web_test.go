// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/bbs/store/sqlite"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// webAddrPattern finds the address the web interface says it is on.
var webAddrPattern = regexp.MustCompile(`addr="?(127\.0\.0\.1:[0-9]+)`)

// The web interface shows the store's messages, and is up whether or not the
// AGW server is.
func TestServingTheWebInterface(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()
	var db = filepath.Join(dir, "bbs.db")

	var store, openErr = sqlite.Open(t.Context(), db)
	require.NoError(t, openErr)

	var to, _ = mail.ParseAddress("Q1TEST")

	var m = new(mail.NewMessage)
	m.Type = mail.Personal
	m.From = "Q2TEST"
	m.To = to
	m.Title = "Hello"
	m.Body = []byte("Hi.\r")

	var _, acceptErr = store.Accept(t.Context(), *m, "Q1BBS")
	require.NoError(t, acceptErr)
	require.NoError(t, store.Close())

	var file = filepath.Join(dir, "samoyed-bbs.yaml")

	require.NoError(t, os.WriteFile(file, underBBS("call: Q1BBS\nstore: "+db+
		"\nagw:\n  addr: 127.0.0.1:"+testutils.UnusedPort(t)+"\nweb:\n  addr: 127.0.0.1:0\n"), 0o600))

	var p = testutils.StartMain(t, "-c", file)

	var lines = p.WaitFor(t, "Serving the web interface")
	var match = webAddrPattern.FindStringSubmatch(lines[len(lines)-1])
	require.NotNil(t, match, lines[len(lines)-1])

	var client = new(http.Client)
	client.Timeout = 10 * time.Second

	var req, reqErr = http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+match[1]+"/api/messages", nil)
	require.NoError(t, reqErr)

	var resp, getErr = client.Do(req)
	require.NoError(t, getErr)

	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var messages []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&messages))
	require.Len(t, messages, 1)
	assert.Equal(t, "Hello", messages[0]["title"])
	assert.Equal(t, "Q1TEST", messages[0]["to"])

	p.Signal(t, syscall.SIGTERM)
	assert.Equal(t, 0, p.Wait())
}

// A web interface that can't have its address stops the BBS starting.
func TestWebInterfaceAddressInUse(t *testing.T) {
	t.Parallel()

	var ln, port = testutils.Listen(t)
	defer func() { _ = ln.Close() }()

	var dir = t.TempDir()
	var file = filepath.Join(dir, "samoyed-bbs.yaml")

	require.NoError(t, os.WriteFile(file, underBBS("call: Q1BBS\nstore: "+filepath.Join(dir, "bbs.db")+
		"\nagw:\n  addr: 127.0.0.1:"+testutils.UnusedPort(t)+"\nweb:\n  addr: 127.0.0.1:"+port+"\n"), 0o600))

	var result = testutils.RunMain(t, "", "-c", file)
	assert.Equal(t, 1, result.Status)
	assert.Contains(t, result.Output(), "web interface:")
}
