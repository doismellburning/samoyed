// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package webui

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
)

// Source is what the web interface reads: the message store.
type Source interface {
	List(ctx context.Context, q mail.ListQuery) ([]mail.Summary, error)
	Get(ctx context.Context, n int) (maybe.Maybe[mail.Message], error)
	ForwardEntries(ctx context.Context, n int) ([]mail.ForwardEntry, error)
	ForwardTotals(ctx context.Context) ([]mail.PartnerTotals, error)
}

// Info is what the BBS says about itself: its settings, and since when it
// has been running.
type Info struct {
	Call    string              `json:"call"`
	HRoute  maybe.Maybe[string] `json:"hroute,omitzero"`
	SID     string              `json:"sid"`
	Version string              `json:"version"`
	Started time.Time           `json:"started"`
	FBB     bool                `json:"fbb"`

	// AGW is the AGW server's host:port.
	AGW string `json:"agw"`

	Partners []Partner `json:"partners"`
}

// Partner is a partner BBS as configured.
type Partner struct {
	Call   string   `json:"call"`
	Covers []string `json:"covers"`
	Floods []string `json:"floods"`

	// Connect is how this BBS calls the partner, or Nothing if the partner
	// calls in.
	Connect maybe.Maybe[Connect] `json:"connect,omitzero"`
}

// Connect is how a partner is called.
type Connect struct {
	Port         int      `json:"port"`
	To           string   `json:"to"`
	Via          []string `json:"via,omitempty"`
	EverySeconds int64    `json:"everySeconds"`
}

// infoJSON is Info with how long the BBS has been up, worked out here so a
// page needn't trust its browser's clock to agree with the BBS's.
type infoJSON struct {
	Info

	UptimeSeconds int64 `json:"uptimeSeconds"`
}

type summaryJSON struct {
	Number  int       `json:"number"`
	MID     string    `json:"mid"`
	Type    string    `json:"type"`
	Status  string    `json:"status"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Title   string    `json:"title"`
	Size    int       `json:"size"`
	Created time.Time `json:"created"`
}

type messageJSON struct {
	summaryJSON

	// RLines are the R: lines at the top of the body, newest first, and
	// Path the BBSes they name.
	RLines []string `json:"rlines"`
	Path   []string `json:"path"`

	// Text is the rest of the body, with newlines for its CRs.
	Text string `json:"text"`

	Forwarding []forwardJSON `json:"forwarding"`
}

type forwardJSON struct {
	Partner  string                 `json:"partner"`
	State    string                 `json:"state"`
	Attempts int                    `json:"attempts"`
	LastTry  maybe.Maybe[time.Time] `json:"lastTry,omitzero"`
}

type partnerJSON struct {
	Partner

	// Configured is false for a partner mail was queued for that is no
	// longer in the configuration.
	Configured bool `json:"configured"`

	Pending   int                    `json:"pending"`
	Forwarded int                    `json:"forwarded"`
	Refused   int                    `json:"refused"`
	Held      int                    `json:"held"`
	LastTry   maybe.Maybe[time.Time] `json:"lastTry,omitzero"`
}

// Handler is the web interface's routes, reading messages from src and the
// BBS's settings from info.  now is the clock uptime is worked out with; nil
// is time.Now.
func Handler(src Source, info Info, now func() time.Time) http.Handler {
	if now == nil {
		now = time.Now
	}

	var mux = http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFiles, "static/index.html")
	})
	mux.Handle("GET /static/", http.FileServerFS(staticFiles))

	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, infoJSON{Info: info, UptimeSeconds: int64(now().Sub(info.Started).Seconds())})
	})
	mux.HandleFunc("GET /api/messages", func(w http.ResponseWriter, r *http.Request) {
		serveMessages(w, r, src)
	})
	mux.HandleFunc("GET /api/messages/{n}", func(w http.ResponseWriter, r *http.Request) {
		serveMessage(w, r, src)
	})
	mux.HandleFunc("GET /api/partners", func(w http.ResponseWriter, r *http.Request) {
		servePartners(w, r, src, info.Partners)
	})

	return securityHeaders(mux)
}

// serveMessages lists messages, newest first.  The query can narrow it with
// type=P,B,T, status=N,Y,F,K,H, from= and to=; without a status, killed
// messages are left out.
func serveMessages(w http.ResponseWriter, r *http.Request, src Source) {
	var q, err = listQuery(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	var summaries, listErr = src.List(r.Context(), q)
	if listErr != nil {
		internalError(w, listErr)

		return
	}

	var out = make([]summaryJSON, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, summaryOf(s))
	}

	writeJSON(w, out)
}

func listQuery(r *http.Request) (mail.ListQuery, error) {
	var q = new(mail.ListQuery)
	var values = r.URL.Query()

	for _, t := range splitList(values.Get("type")) {
		var parsed, err = mail.ParseType(t)
		if err != nil {
			return mail.ListQuery{}, err
		}

		q.Types = append(q.Types, parsed)
	}

	for _, s := range splitList(values.Get("status")) {
		var parsed, err = mail.ParseStatus(s)
		if err != nil {
			return mail.ListQuery{}, err
		}

		q.Statuses = append(q.Statuses, parsed)
	}

	if from := strings.TrimSpace(values.Get("from")); from != "" {
		q.From = maybe.Just(from)
	}

	if to := strings.TrimSpace(values.Get("to")); to != "" {
		q.To = maybe.Just(to)
	}

	return *q, nil
}

// splitList is a comma-separated query value's items, without blanks.
func splitList(value string) []string {
	return slices.DeleteFunc(strings.Split(value, ","), func(s string) bool { return strings.TrimSpace(s) == "" })
}

func serveMessage(w http.ResponseWriter, r *http.Request, src Source) {
	var n, parseErr = strconv.Atoi(r.PathValue("n"))
	if parseErr != nil {
		http.Error(w, "not a message number", http.StatusBadRequest)

		return
	}

	var got, getErr = src.Get(r.Context(), n)
	if getErr != nil {
		internalError(w, getErr)

		return
	}

	var msg, ok = got.Get()
	if !ok {
		http.NotFound(w, r)

		return
	}

	var entries, entriesErr = src.ForwardEntries(r.Context(), n)
	if entriesErr != nil {
		internalError(w, entriesErr)

		return
	}

	var rlines, rest = mail.SplitRLines(msg.Body)

	var out = new(messageJSON)
	out.summaryJSON = summaryOf(msg.Summary)
	out.RLines = orEmpty(rlines)
	out.Path = orEmpty(mail.Path(msg.Body))
	out.Text = strings.ReplaceAll(strings.ReplaceAll(string(rest), "\r\n", "\n"), "\r", "\n")
	out.Forwarding = make([]forwardJSON, 0, len(entries))

	for _, e := range entries {
		out.Forwarding = append(out.Forwarding, forwardJSON{Partner: e.Partner, State: string(e.State), Attempts: e.Attempts, LastTry: e.LastTry})
	}

	writeJSON(w, out)
}

// servePartners is each configured partner, as configured order, with how
// forwarding to it stands; then any partner mail was queued for that has
// since left the configuration.
func servePartners(w http.ResponseWriter, r *http.Request, src Source, partners []Partner) {
	var totals, err = src.ForwardTotals(r.Context())
	if err != nil {
		internalError(w, err)

		return
	}

	var byCall = make(map[string]mail.PartnerTotals, len(totals))
	for _, t := range totals {
		byCall[t.Partner] = t
	}

	var out = make([]partnerJSON, 0, len(partners)+len(totals))

	for _, p := range partners {
		var t = byCall[p.Call]
		delete(byCall, p.Call)

		out = append(out, partnerOf(p, true, t))
	}

	for _, t := range totals {
		if _, left := byCall[t.Partner]; left {
			out = append(out, partnerOf(Partner{Call: t.Partner, Covers: []string{}, Floods: []string{}, Connect: maybe.Nothing[Connect]()}, false, t))
		}
	}

	writeJSON(w, out)
}

func partnerOf(p Partner, configured bool, t mail.PartnerTotals) partnerJSON {
	return partnerJSON{
		Partner:    p,
		Configured: configured,
		Pending:    t.Pending,
		Forwarded:  t.Forwarded,
		Refused:    t.Refused,
		Held:       t.Held,
		LastTry:    t.LastTry,
	}
}

func summaryOf(s mail.Summary) summaryJSON {
	return summaryJSON{
		Number:  s.Number,
		MID:     s.MID,
		Type:    s.Type.String(),
		Status:  s.Status.String(),
		From:    s.From,
		To:      s.To.String(),
		Title:   s.Title,
		Size:    s.Size,
		Created: s.Created,
	}
}

// orEmpty is values, or an empty slice for nil, so JSON says [] not null.
func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}

	return values
}

func internalError(w http.ResponseWriter, err error) {
	logrus.WithError(err).Warn("BBS web UI couldn't read the message store")
	http.Error(w, "couldn't read the message store", http.StatusInternalServerError)
}
