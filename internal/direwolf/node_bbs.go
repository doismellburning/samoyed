// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

// The node's BBS: a command in its shells, and a callsign of its own that
// stations and partner BBSes can connect to directly, over AX.25 or NET/ROM.

import (
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs"
	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/doismellburning/samoyed/internal/node"
	"github.com/doismellburning/samoyed/internal/nodeevents"
)

// addBBS gives the node a BBS, keeping its messages under dataDir.  It must be
// called before the node starts.
func (n *netromNode) addBBS(settings BBSSettings, dataDir string) error {
	if dataDir == "" {
		return errors.New("the BBS needs dataDir set, to keep its messages in")
	}

	var store, err = bbs.OpenStore(filepath.Join(dataDir, "bbs"))
	if err != nil {
		return err
	}

	var call = settings.Call
	if call == "" {
		call = n.cfg.Call
	}

	var partners = make([]bbs.Partner, 0, len(settings.Partners))
	for _, p := range settings.Partners {
		partners = append(partners, bbs.Partner{
			Call: p.Call, Node: p.Node, Port: p.Channel, Via: p.Via, Script: p.Script,
			Routes: p.Routes, Bulletins: p.Bulletins, Interval: deref(p.Interval),
		})
	}

	var server, berr = bbs.New(bbs.Config{
		Call:      call,
		HRoute:    settings.HRoute,
		Version:   SAMOYED_VERSION,
		Partners:  partners,
		Dial:      func(p *bbs.Partner, events node.Downlink) (node.Conn, error) { return n.dialPartner(call, p, events) },
		Now:       n.now,
		OnMessage: n.bbsMessage,
	}, store)
	if berr != nil {
		return berr
	}

	n.bbs = server
	n.shellCfg.Applications = append(n.shellCfg.Applications, server)

	if settings.Call != "" && settings.Call != n.cfg.Call {
		n.bbsCall = settings.Call

		var lerr = n.router.Listen(settings.Call, settings.Alias, deref(settings.Quality), n.acceptBBSCircuit)
		if lerr != nil {
			return lerr
		}
	}

	return nil
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}

	return *p
}

// dialPartner connects to a partner BBS, as the BBS's callsign.
func (n *netromNode) dialPartner(call string, p *bbs.Partner, events node.Downlink) (node.Conn, error) { //nolint:ireturn // bbs.Dialer's signature.
	if p.Node != "" {
		return n.ConnectNode(p.Node, call, events)
	}

	return n.ConnectAX25(p.Port, p.Call, p.Via, call, events)
}

// acceptBBSCircuit gives a station connecting to the BBS over NET/ROM a BBS
// session.
func (n *netromNode) acceptBBSCircuit(c *netrom.Circuit) netrom.CircuitHandler { //nolint:ireturn // netrom.Acceptor's signature.
	var h = new(circuitApp)
	h.n = n
	h.user = c.User()
	h.conn = circuitConn{c: c}

	return h
}

// circuitApp passes what happens on a circuit to the application session at
// its end.
type circuitApp struct {
	n       *netromNode
	user    string
	conn    circuitConn
	session node.AppSession
}

func (h *circuitApp) Connected(*netrom.Circuit) {
	h.session = h.n.bbs.OpenDirect(h.user, h.conn)
}

func (h *circuitApp) Received(_ *netrom.Circuit, data []byte) {
	if h.session != nil {
		h.session.Input(data)
	}
}

func (h *circuitApp) Closed(*netrom.Circuit, error) {
	if h.session != nil {
		h.session.Close()
	}
}

// bbsMessage publishes a new BBS message.
func (n *netromNode) bbsMessage(m *bbs.Message) {
	var ev = n.event(nodeevents.BBSMessage)
	ev.Number = m.Number
	ev.Type = m.Type
	ev.From = m.From
	ev.To = m.To
	ev.At = m.At
	n.events.Publish(ev)
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

func nodeBBSMessages(n *netromNode) any {
	var out = []bbsMessageJSON{}

	if n.bbs == nil {
		return out
	}

	for _, m := range n.bbs.Messages() {
		out = append(out, bbsMessageJSON{
			Number: m.Number, Type: m.Type, From: m.From, To: m.To, At: m.At, BID: m.BID,
			Subject: m.Subject, Size: m.Size(), Date: m.Date, Read: m.Read, Origin: m.Origin,
			Forward: append([]string{}, m.Forward...), Forwarded: append([]string{}, m.Forwarded...),
		})
	}

	return out
}

func nodeBBSPartners(n *netromNode) any {
	if n.bbs == nil {
		return []bbs.PartnerStatus{}
	}

	return n.bbs.Partners()
}

func adminKillBBSMessage(n *netromNode, r *http.Request) error {
	if n.bbs == nil {
		return errNotFound
	}

	var number, err = strconv.Atoi(r.PathValue("number"))
	if err != nil {
		return errors.Join(errBadRequest, err)
	}

	var kerr = n.bbs.Kill(number)
	if kerr != nil {
		return errors.Join(errNotFound, kerr)
	}

	return nil
}

func adminForwardBBS(n *netromNode, r *http.Request) error {
	if n.bbs == nil {
		return errNotFound
	}

	var err = n.bbs.ForwardNow(r.PathValue("call"))
	if errors.Is(err, bbs.ErrNoPartner) {
		return errors.Join(errNotFound, err)
	}

	return err
}
