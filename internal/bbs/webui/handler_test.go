// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package webui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTime is when everything in these tests happens.
func testTime() time.Time { return time.Date(2026, 11, 1, 9, 5, 0, 0, time.UTC) }

// fakeSource is a store of one personal message and one bulletin, which
// records the last query it was asked to list.
type fakeSource struct {
	mu        sync.Mutex
	lastQuery mail.ListQuery
	fail      bool
}

func summary(n int, typ mail.Type, status mail.Status, to string) mail.Summary {
	var address, _ = mail.ParseAddress(to)

	return mail.Summary{Number: n, MID: "1_Q1BBS", Type: typ, Status: status, From: "Q2TEST", To: address, Title: `<b>Hi</b>`, Size: 5, Created: testTime()}
}

func (f *fakeSource) List(_ context.Context, q mail.ListQuery) ([]mail.Summary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastQuery = q

	if f.fail {
		return nil, errors.New("disk on fire")
	}

	return []mail.Summary{summary(2, mail.Bulletin, mail.New, "ALL@GBR"), summary(1, mail.Personal, mail.Held, "Q1TEST@Q6BBS")}, nil
}

func (f *fakeSource) Get(_ context.Context, n int) (maybe.Maybe[mail.Message], error) {
	if n != 1 {
		return maybe.Nothing[mail.Message](), nil
	}

	var body = []byte("R:261101/0905Z @:Q1BBS.#77.GBR.EURO\rR:261101/0900Z 12@Q3BPQ.GBR LinBPQ6.0.26\r\rHello\rthere\r")

	return maybe.Just(mail.Message{Summary: summary(1, mail.Personal, mail.Held, "Q1TEST@Q6BBS"), Body: body}), nil
}

func (f *fakeSource) ForwardEntries(_ context.Context, n int) ([]mail.ForwardEntry, error) {
	return []mail.ForwardEntry{{Msg: n, Partner: "Q3BBS", State: mail.ForwardHeld, Attempts: 5, LastTry: maybe.Just(testTime())}}, nil
}

func (f *fakeSource) ForwardTotals(_ context.Context) ([]mail.PartnerTotals, error) {
	return []mail.PartnerTotals{
		{Partner: "Q3BBS", Pending: 2, Forwarded: 7, Refused: 1, Held: 1, LastTry: maybe.Just(testTime())},
		{Partner: "Q9OLD", Pending: 1, Forwarded: 0, Refused: 0, Held: 0, LastTry: maybe.Nothing[time.Time]()},
	}, nil
}

func (f *fakeSource) query() mail.ListQuery {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.lastQuery
}

func testInfo() Info {
	return Info{
		Call:    "Q1BBS",
		HRoute:  maybe.Just("#77.GBR.EURO"),
		SID:     "[SAMBBS-0.0-H$]",
		Version: "2026.11.01",
		Started: testTime().Add(-time.Hour),
		FBB:     false,
		AGW:     "localhost:8000",
		Partners: []Partner{
			{Call: "Q2BBS", Covers: []string{"#77.GBR.EURO"}, Floods: []string{"GBR"}, Connect: maybe.Nothing[Connect]()},
			{Call: "Q3BBS", Covers: []string{"*"}, Floods: []string{"*"}, Connect: maybe.Just(Connect{Port: 0, To: "Q3NOD", Via: nil, EverySeconds: 1800})},
		},
	}
}

func get(t *testing.T, h http.Handler, method string, path string) *httptest.ResponseRecorder {
	t.Helper()

	var w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), method, path, nil))

	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var v T
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v))

	return v
}

func newHandler(src Source) http.Handler {
	return Handler(src, testInfo(), testTime)
}

func TestInfo(t *testing.T) {
	t.Parallel()

	var got = decode[map[string]any](t, get(t, newHandler(new(fakeSource)), http.MethodGet, "/api/info"))

	assert.Equal(t, "Q1BBS", got["call"])
	assert.Equal(t, "#77.GBR.EURO", got["hroute"])
	assert.Equal(t, "[SAMBBS-0.0-H$]", got["sid"])
	assert.InDelta(t, 3600, got["uptimeSeconds"], 0)
}

func TestMessages(t *testing.T) {
	t.Parallel()

	var src = new(fakeSource)
	var h = newHandler(src)

	var got = decode[[]map[string]any](t, get(t, h, http.MethodGet, "/api/messages"))
	require.Len(t, got, 2)
	assert.InDelta(t, 2, got[0]["number"], 0)
	assert.Equal(t, "B", got[0]["type"])
	assert.Equal(t, "ALL@GBR", got[0]["to"])
	assert.Equal(t, "<b>Hi</b>", got[0]["title"], "data, however it is dressed up")
	assert.True(t, isZeroQuery(src.query()), "killed left out, as the store does by default")

	decode[[]map[string]any](t, get(t, h, http.MethodGet, "/api/messages?type=P,b&status=H,K&from=q2test&to=+Q1TEST+"))

	var q = src.query()
	assert.Equal(t, []mail.Type{mail.Personal, mail.Bulletin}, q.Types)
	assert.Equal(t, []mail.Status{mail.Held, mail.Killed}, q.Statuses)
	assert.Equal(t, maybe.Just("q2test"), q.From)
	assert.Equal(t, maybe.Just("Q1TEST"), q.To)

	assert.Equal(t, http.StatusBadRequest, get(t, h, http.MethodGet, "/api/messages?type=X").Code)
	assert.Equal(t, http.StatusBadRequest, get(t, h, http.MethodGet, "/api/messages?status=Z").Code)
}

func TestMessagesStoreFailure(t *testing.T) {
	t.Parallel()

	var src = new(fakeSource)
	src.fail = true

	var w = get(t, newHandler(src), http.MethodGet, "/api/messages")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "disk on fire", "the store's error stays in the log")
}

func TestMessage(t *testing.T) {
	t.Parallel()

	var h = newHandler(new(fakeSource))

	var got = decode[map[string]any](t, get(t, h, http.MethodGet, "/api/messages/1"))
	assert.Equal(t, "Hello\nthere\n", got["text"], "R: lines and the blank line after them are kept apart")
	assert.Equal(t, []any{"R:261101/0905Z @:Q1BBS.#77.GBR.EURO", "R:261101/0900Z 12@Q3BPQ.GBR LinBPQ6.0.26"}, got["rlines"])
	assert.Equal(t, []any{"Q1BBS", "Q3BPQ"}, got["path"])
	assert.Equal(t, "H", got["status"])
	assert.Equal(t, []any{map[string]any{"partner": "Q3BBS", "state": "held", "attempts": 5.0, "lastTry": "2026-11-01T09:05:00Z"}}, got["forwarding"])

	assert.Equal(t, http.StatusNotFound, get(t, h, http.MethodGet, "/api/messages/2").Code)
	assert.Equal(t, http.StatusBadRequest, get(t, h, http.MethodGet, "/api/messages/one").Code)
}

func TestPartners(t *testing.T) {
	t.Parallel()

	var got = decode[[]map[string]any](t, get(t, newHandler(new(fakeSource)), http.MethodGet, "/api/partners"))
	require.Len(t, got, 3)

	assert.Equal(t, "Q2BBS", got[0]["call"], "configured order")
	assert.Equal(t, true, got[0]["configured"])
	assert.InDelta(t, 0, got[0]["pending"], 0, "nothing queued yet")
	assert.NotContains(t, got[0], "connect", "calls in")
	assert.NotContains(t, got[0], "lastTry")

	assert.Equal(t, "Q3BBS", got[1]["call"])
	assert.InDelta(t, 2, got[1]["pending"], 0)
	assert.InDelta(t, 7, got[1]["forwarded"], 0)
	assert.Equal(t, map[string]any{"port": 0.0, "to": "Q3NOD", "everySeconds": 1800.0}, got[1]["connect"])

	assert.Equal(t, "Q9OLD", got[2]["call"])
	assert.Equal(t, false, got[2]["configured"], "queued for, but since taken out of the configuration")
}

func TestPageAndHeaders(t *testing.T) {
	t.Parallel()

	var h = newHandler(new(fakeSource))

	for _, path := range []string{"/", "/static/app.js", "/static/app.css", "/static/favicon.png"} {
		var w = get(t, h, http.MethodGet, path)
		assert.Equal(t, http.StatusOK, w.Code, path)
		assert.Contains(t, w.Header().Get("Content-Security-Policy"), "default-src 'self'", path)
		assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"), path)
	}

	assert.Contains(t, get(t, h, http.MethodGet, "/").Body.String(), "Samoyed BBS")
	assert.Equal(t, http.StatusNotFound, get(t, h, http.MethodGet, "/nothing").Code)
}

func TestReadOnly(t *testing.T) {
	t.Parallel()

	var h = newHandler(new(fakeSource))

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		for _, path := range []string{"/api/messages", "/api/messages/1", "/api/partners", "/api/info"} {
			assert.Equal(t, http.StatusMethodNotAllowed, get(t, h, method, path).Code, "%s %s", method, path)
		}
	}
}

func TestServerServesUntilCancelled(t *testing.T) {
	t.Parallel()

	var ctx, cancel = context.WithCancel(t.Context())

	var s, err = NewServer(ctx, "127.0.0.1:0")
	require.NoError(t, err)

	var done = make(chan error, 1)

	go func() { done <- s.Serve(ctx, newHandler(new(fakeSource))) }()

	var req, reqErr = http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+s.Addr().String()+"/api/info", nil)
	require.NoError(t, reqErr)

	var resp, getErr = http.DefaultClient.Do(req)
	require.NoError(t, getErr)

	_ = resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	cancel()

	select {
	case serveErr := <-done:
		require.NoError(t, serveErr, "a shutdown asked for is no error")
	case <-time.After(10 * time.Second):
		t.Fatal("still serving after cancellation")
	}
}

func TestNewServerReportsABadAddress(t *testing.T) {
	t.Parallel()

	var _, err = NewServer(t.Context(), "256.0.0.1:http-nope")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "256.0.0.1") || strings.Contains(err.Error(), "http-nope"), err.Error())
}

// isZeroQuery says whether q asks for nothing in particular.
func isZeroQuery(q mail.ListQuery) bool {
	return len(q.Types) == 0 && len(q.Statuses) == 0 && q.From.IsNothing() && q.To.IsNothing() && q.Lowest.IsNothing() && q.Highest.IsNothing()
}
