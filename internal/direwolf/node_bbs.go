// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

// The node's BBS: a command in its shell, and a callsign of its own that
// stations and partner BBSes can connect to directly, over AX.25 or NET/ROM.
// Like the node's other applications, it runs on the applications' goroutine.

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs"
	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/doismellburning/samoyed/internal/node"
	"github.com/doismellburning/samoyed/internal/nodeevents"
	"github.com/doismellburning/samoyed/internal/version"
	"github.com/doismellburning/samoyed/internal/webui"
)

// bbsTickInterval is how often the BBS looks for partners due to be
// forwarded to.
const bbsTickInterval = 10 * time.Second

// addBBS gives the node a BBS, configured by settings, keeping its messages
// under dataDir, reaching partners with shell's AX.25 links or nr's
// circuits (nil without NET/ROM), and publishing new messages on events.  It
// must be called before the shell starts.
func (a *nodeApps) addBBS(ctx context.Context, settings BBSSettings, dataDir string,
	nodeConfig node.Config, shell *node.Node, nr *netromNode, events *nodeevents.Bus,
) error {
	if dataDir == "" {
		return errors.New("the BBS needs dataDir set, to keep its messages in")
	}

	var cerr = bbsConfigFor(settings, nodeConfig, nr != nil)
	if cerr != nil {
		return cerr
	}

	var store, err = bbs.OpenStore(filepath.Join(dataDir, "bbs"))
	if err != nil {
		return err
	}

	var call = settings.Call
	if call == "" {
		call = nodeConfig.Address
	}

	var partners = make([]bbs.Partner, 0, len(settings.Partners))
	for _, p := range settings.Partners {
		partners = append(partners, bbs.Partner{
			Call: p.Call, Node: p.Node, Port: p.Channel, Via: p.Via, Script: p.Script,
			Routes: p.Routes, Bulletins: p.Bulletins, Interval: deref(p.Interval),
		})
	}

	var dial = func(p *bbs.Partner, events node.Downlink) (node.Conn, error) {
		var queued = queuedDownlink{events: events, do: a.do}

		if p.Node != "" {
			return nr.Connect(p.Node, call, queued), nil
		}

		return shell.ConnectFrom(p.Port, call, p.Call, p.Via, queued), nil
	}

	var server, berr = bbs.New(bbs.Config{
		Call:      call,
		HRoute:    settings.HRoute,
		Version:   version.Version,
		Partners:  partners,
		Dial:      dial,
		Now:       time.Now,
		OnMessage: func(m *bbs.Message) { publishBBSMessage(events, nodeConfig.Address, m) },
	}, store)
	if berr != nil {
		return berr
	}

	a.bbs = server
	a.add(shell, server)

	if settings.Call != "" {
		shell.AddService(settings.Call, a.bbsService)

		if nr != nil {
			var lerr error

			var qerr = nr.query(ctx, func() {
				lerr = nr.router.Listen(settings.Call, settings.Alias, deref(settings.Quality), a.acceptBBSCircuit(nr))
			})
			if qerr != nil {
				return qerr
			}

			if lerr != nil {
				return lerr
			}
		}
	}

	go every(ctx, bbsTickInterval, func() { a.do(func() { server.Tick(time.Now()) }) })

	return nil
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}

	return *p
}

// every runs f every interval until ctx is cancelled.
func every(ctx context.Context, interval time.Duration, f func()) {
	var ticker = time.NewTicker(interval)
	defer ticker.Stop()

	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f()
		}
	}
}

// bbsService gives a station that connects to the BBS's callsign over AX.25
// a BBS session, on the applications' goroutine.
func (a *nodeApps) bbsService(user string, conn node.Conn) node.AppSession { //nolint:ireturn // node.Service's signature.
	var s = &queuedAppSession{do: a.do, session: nil}

	a.do(func() { s.session = a.bbs.OpenDirect(user, conn) })

	return s
}

// acceptBBSCircuit gives a station connecting to the BBS over NET/ROM a BBS
// session.
func (a *nodeApps) acceptBBSCircuit(nr *netromNode) netrom.Acceptor {
	return func(c *netrom.Circuit) netrom.CircuitHandler {
		return &circuitApp{
			apps:    a,
			user:    c.User(),
			conn:    &circuitConn{n: nr, c: c},
			session: &queuedAppSession{do: a.do, session: nil},
			open:    false,
		}
	}
}

// circuitApp passes what happens on a circuit to the BBS session at its end.
type circuitApp struct {
	apps    *nodeApps
	user    string
	conn    *circuitConn
	session *queuedAppSession
	open    bool
}

func (h *circuitApp) Connected(*netrom.Circuit) {
	h.open = true
	h.apps.do(func() { h.session.session = h.apps.bbs.OpenDirect(h.user, h.conn) })
}

func (h *circuitApp) Received(_ *netrom.Circuit, data []byte) {
	if h.open {
		h.session.Input(data)
	}
}

func (h *circuitApp) Closed(*netrom.Circuit, error) {
	if h.open {
		h.session.Close()
	}
}

// queuedDownlink tells events what happens on a connection the BBS opened, on
// the applications' goroutine.
type queuedDownlink struct {
	events node.Downlink
	do     func(f func())
}

func (q queuedDownlink) Connected() { q.do(q.events.Connected) }

func (q queuedDownlink) Received(data []byte) {
	var copied = bytes.Clone(data)

	q.do(func() { q.events.Received(copied) })
}

func (q queuedDownlink) Closed(failure string) {
	q.do(func() { q.events.Closed(failure) })
}

// publishBBSMessage publishes a new BBS message on bus, as the node
// nodeCall's.
func publishBBSMessage(bus *nodeevents.Bus, nodeCall string, m *bbs.Message) {
	var ev = newEvent(nodeevents.BBSMessage, nodeCall, time.Now())
	ev.Number = m.Number
	ev.Type = m.Type
	ev.From = m.From
	ev.To = m.To
	ev.At = m.At
	bus.Publish(ev)
}

// bbsMessageJSON is a BBS message for the admin API: everything but its body.
type bbsMessageJSON struct {
	Number    int       `json:"number"`
	Type      string    `json:"type"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	At        string    `json:"at"`
	BID       string    `json:"bid"`
	Subject   string    `json:"subject"`
	Size      int       `json:"size"`
	Date      time.Time `json:"date"`
	Read      bool      `json:"read"`
	Origin    string    `json:"origin"`
	Forward   []string  `json:"forward"`
	Forwarded []string  `json:"forwarded"`
}

func bbsMessages(b *bbs.BBS) any {
	var out = []bbsMessageJSON{}

	for _, m := range b.Messages() {
		out = append(out, bbsMessageJSON{
			Number: m.Number, Type: m.Type, From: m.From, To: m.To, At: m.At, BID: m.BID,
			Subject: m.Subject, Size: m.Size(), Date: m.Date, Read: m.Read, Origin: m.Origin,
			Forward: append([]string{}, m.Forward...), Forwarded: append([]string{}, m.Forwarded...),
		})
	}

	return out
}

func bbsPartners(b *bbs.BBS) any {
	return b.Partners()
}

func adminKillBBSMessage(b *bbs.BBS, r *http.Request) error {
	var number, err = strconv.Atoi(r.PathValue("number"))
	if err != nil {
		return errors.Join(errBadRequest, err)
	}

	var kerr = b.Kill(number)
	if kerr != nil {
		return errors.Join(errNotFound, kerr)
	}

	return nil
}

func adminForwardBBS(b *bbs.BBS, r *http.Request) error {
	var err = b.ForwardNow(r.PathValue("call"))
	if errors.Is(err, bbs.ErrNoPartner) {
		return errors.Join(errNotFound, err)
	}

	return err
}

// bbsView serves what f makes of the BBS, made on the applications'
// goroutine: 404 without a BBS.
func (w *nodeWeb) bbsView(f func(b *bbs.BBS) any) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var v any

		if w.onBBS(rw, r, func(b *bbs.BBS) { v = f(b) }) {
			webui.WriteJSON(rw, v)
		}
	})
}

// bbsAdmin runs f on the BBS, on the applications' goroutine, answering 204
// when it succeeds: 404 without a BBS.
func (w *nodeWeb) bbsAdmin(f func(b *bbs.BBS, r *http.Request) error) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var err error

		if w.onBBS(rw, r, func(b *bbs.BBS) { err = f(b, r) }) {
			writeActionResult(rw, err)
		}
	})
}

// onBBS runs f on the BBS, on the applications' goroutine, and says whether
// it did, having answered the request itself if not.
func (w *nodeWeb) onBBS(rw http.ResponseWriter, r *http.Request, f func(b *bbs.BBS)) bool {
	var n = w.node.Load()
	if n == nil || n.apps == nil {
		http.NotFound(rw, r)

		return false
	}

	var found bool

	var qerr = n.apps.query(r.Context(), func() {
		found = n.apps.bbs != nil
		if found {
			f(n.apps.bbs)
		}
	})

	switch {
	case qerr != nil:
		http.Error(rw, "node busy", http.StatusServiceUnavailable)

		return false
	case !found:
		http.NotFound(rw, r)

		return false
	}

	return true
}
