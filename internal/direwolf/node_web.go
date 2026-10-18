// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

// The node on the web interface: read-only views of its sessions and of what
// NET/ROM knows, for anyone, and actions for its sysop, who must give the
// admin token.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/mheard"
	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/doismellburning/samoyed/internal/node"
	"github.com/doismellburning/samoyed/internal/webui"
)

// nodeQueryTimeout is how long a web request waits for NET/ROM to answer.
const nodeQueryTimeout = 5 * time.Second

// webNode is the node, as the web interface sees it.
type webNode struct {
	config node.Config
	shell  *node.Node
	netrom *netromNode // Nil without NET/ROM.
	link   node.DataLink
	heard  node.HeardStations
}

// nodeWeb serves the node's part of the web interface.  The web interface
// starts before the node does, so the node is handed over once there is one;
// until then, and without one, the routes answer 404.
type nodeWeb struct {
	node  atomic.Pointer[webNode]
	token string
}

func newNodeWeb(adminToken string) *nodeWeb {
	var w = new(nodeWeb)
	w.token = adminToken

	return w
}

// set hands over the node.
func (w *nodeWeb) set(n *webNode) {
	w.node.Store(n)
}

// routes returns the routes to serve.  The admin routes are there only with an
// admin token configured.
func (w *nodeWeb) routes() []webui.Route {
	var routes = []webui.Route{
		{Pattern: "GET /api/node/info", Handler: w.view(nodeInfo)},
		{Pattern: "GET /api/node/sessions", Handler: w.view(nodeSessions)},
		{Pattern: "GET /api/node/heard", Handler: w.view(nodeHeard)},
		{Pattern: "GET /api/node/nodes", Handler: w.netromView(nodeNodes)},
		{Pattern: "GET /api/node/neighbours", Handler: w.netromView(nodeNeighbours)},
		{Pattern: "GET /api/node/circuits", Handler: w.netromView(nodeCircuits)},
	}

	if w.token == "" {
		return routes
	}

	for _, r := range []webui.Route{
		{Pattern: "POST /api/admin/links/close", Handler: w.action(adminCloseLink)},
		{Pattern: "POST /api/admin/nodes/broadcast", Handler: w.netromAction(adminBroadcast)},
		{Pattern: "POST /api/admin/circuits/{id}/close", Handler: w.netromAction(adminCloseCircuit)},
		{Pattern: "POST /api/admin/neighbours", Handler: w.netromAction(adminLockNeighbour)},
		{Pattern: "DELETE /api/admin/neighbours/{port}/{call}", Handler: w.netromAction(adminUnlockNeighbour)},
	} {
		r.Handler = webui.RequireToken(w.token, r.Handler)
		routes = append(routes, r)
	}

	return routes
}

// view serves what f makes of the node.
func (w *nodeWeb) view(f func(n *webNode) any) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var n = w.node.Load()
		if n == nil {
			http.NotFound(rw, r)

			return
		}

		webui.WriteJSON(rw, f(n))
	})
}

// netromView serves what f makes of NET/ROM, made on its goroutine: nothing,
// without NET/ROM.
func (w *nodeWeb) netromView(f func(n *netromNode) any) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var n = w.node.Load()
		if n == nil {
			http.NotFound(rw, r)

			return
		}

		if n.netrom == nil {
			webui.WriteJSON(rw, []struct{}{})

			return
		}

		var v any

		var err = n.netrom.query(r.Context(), func() { v = f(n.netrom) })
		if err != nil {
			http.Error(rw, "node busy", http.StatusServiceUnavailable)

			return
		}

		webui.WriteJSON(rw, v)
	})
}

// errBadRequest marks an action's error as the request's fault.
var errBadRequest = errors.New("bad request")

// errNotFound marks an action's error as naming nothing there is.
var errNotFound = errors.New("not found")

// writeActionResult answers an action: 204 if it worked, or why not.
func writeActionResult(rw http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errBadRequest):
		http.Error(rw, err.Error(), http.StatusBadRequest)
	case errors.Is(err, errNotFound):
		http.Error(rw, err.Error(), http.StatusNotFound)
	case err != nil:
		http.Error(rw, err.Error(), http.StatusInternalServerError)
	default:
		rw.WriteHeader(http.StatusNoContent)
	}
}

// action runs f, answering 204 when it succeeds.
func (w *nodeWeb) action(f func(n *webNode, r *http.Request) error) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var n = w.node.Load()
		if n == nil {
			http.NotFound(rw, r)

			return
		}

		writeActionResult(rw, f(n, r))
	})
}

// netromAction runs f on NET/ROM's goroutine, answering 204 when it succeeds.
func (w *nodeWeb) netromAction(f func(n *netromNode, r *http.Request) error) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var n = w.node.Load()
		if n == nil || n.netrom == nil {
			http.NotFound(rw, r)

			return
		}

		var err error

		var qerr = n.netrom.query(r.Context(), func() { err = f(n.netrom, r) })
		if qerr != nil {
			http.Error(rw, "node busy", http.StatusServiceUnavailable)

			return
		}

		writeActionResult(rw, err)
	})
}

// query runs f on NET/ROM's goroutine and waits for it, giving up after
// nodeQueryTimeout or when ctx is done.
func (n *netromNode) query(ctx context.Context, f func()) error {
	var cctx, cancel = context.WithTimeout(ctx, nodeQueryTimeout)
	defer cancel()

	var done = make(chan struct{})

	n.do(func() { f(); close(done) })

	select {
	case <-done:
		return nil
	case <-cctx.Done():
		return cctx.Err()
	}
}

type nodeInfoJSON struct {
	Address string     `json:"address"`
	Alias   string     `json:"alias"`
	Ports   []portJSON `json:"ports"`
	Info    string     `json:"info"`
	NetROM  bool       `json:"netrom"`
}

type portJSON struct {
	Number      int    `json:"number"`
	Description string `json:"description"`
	NetROM      bool   `json:"netrom"`
}

func nodeInfo(n *webNode) any {
	var ports = make([]portJSON, 0, len(n.config.Ports))

	for _, p := range n.config.Ports {
		var netromPort = false
		if n.netrom != nil {
			_, netromPort = n.netrom.cfg.PortConfig(p.Channel)
		}

		ports = append(ports, portJSON{Number: p.Channel, Description: p.Description, NetROM: netromPort})
	}

	return nodeInfoJSON{Address: n.config.Address, Alias: n.config.Alias, Ports: ports, Info: n.config.InfoMessage, NetROM: n.netrom != nil}
}

type sessionJSON struct {
	User      string `json:"user"`
	Channel   *int   `json:"channel,omitempty"`
	Local     string `json:"local,omitempty"`
	Downlink  string `json:"downlink,omitempty"`
	Connected bool   `json:"connected"`
	App       string `json:"app,omitempty"`
}

func nodeSessions(n *webNode) any {
	var out = []sessionJSON{}

	for _, s := range n.shell.Sessions() {
		var j = sessionJSON{User: s.User, Channel: nil, Local: s.Local, Downlink: s.Downlink, Connected: s.Connected, App: s.App}
		if s.Channel >= 0 {
			var channel = s.Channel
			j.Channel = &channel
		}

		out = append(out, j)
	}

	return out
}

type heardJSON struct {
	Call    string    `json:"call"`
	Channel int       `json:"channel"`
	Last    time.Time `json:"last"`
}

// maxWebHeard is the most heard stations the web interface lists.
const maxWebHeard = 50

func nodeHeard(n *webNode) any {
	var out = []heardJSON{}

	var heard []mheard.Heard
	if n.heard != nil {
		heard = n.heard(maxWebHeard)
	}

	for _, h := range heard {
		out = append(out, heardJSON{Call: h.Callsign, Channel: h.Channel, Last: h.LastHeard.UTC()})
	}

	return out
}

type routeJSON struct {
	Port         int    `json:"port"`
	Neighbour    string `json:"neighbour"`
	Quality      int    `json:"quality"`
	Obsolescence int    `json:"obsolescence"`
	Locked       bool   `json:"locked"`
}

type destinationJSON struct {
	Call   string      `json:"call"`
	Alias  string      `json:"alias"`
	Routes []routeJSON `json:"routes"`
}

func nodeNodes(n *netromNode) any {
	var out = []destinationJSON{}

	for _, d := range n.router.Table().Destinations() {
		var routes = make([]routeJSON, 0, len(d.Routes))
		for _, r := range d.Routes {
			routes = append(routes, routeJSON{Port: r.Neighbour.Port, Neighbour: r.Neighbour.Call, Quality: r.Quality, Obsolescence: r.Obsolescence, Locked: r.Locked})
		}

		out = append(out, destinationJSON{Call: d.Call, Alias: d.Alias, Routes: routes})
	}

	return out
}

type neighbourJSON struct {
	Port    int    `json:"port"`
	Call    string `json:"call"`
	Alias   string `json:"alias"`
	Quality int    `json:"quality"`
	Locked  bool   `json:"locked"`
	Routes  int    `json:"routes"`
	Linked  bool   `json:"linked"`
}

func nodeNeighbours(n *netromNode) any {
	var out = []neighbourJSON{}

	for _, nb := range n.router.Table().Neighbours() {
		var l, linked = n.links[nb.NeighbourKey]

		out = append(out, neighbourJSON{
			Port: nb.Port, Call: nb.Call, Alias: nb.Alias, Quality: nb.Quality, Locked: nb.Locked,
			Routes: n.router.Table().RoutesVia(nb.NeighbourKey),
			Linked: linked && l.up,
		})
	}

	return out
}

type circuitJSON struct {
	ID       string    `json:"id"`
	Local    string    `json:"local"`
	Remote   string    `json:"remote"`
	User     string    `json:"user"`
	UserNode string    `json:"userNode"`
	Incoming bool      `json:"incoming"`
	State    string    `json:"state"`
	Window   int       `json:"window"`
	Unacked  int       `json:"unacked"`
	Queued   int       `json:"queued"`
	Started  time.Time `json:"started"`
	Sent     int       `json:"sent"`
	Received int       `json:"received"`
}

func nodeCircuits(n *netromNode) any {
	var out = []circuitJSON{}

	for _, c := range n.router.Circuits() {
		out = append(out, circuitJSON{
			ID: c.ID, Local: c.Local, Remote: c.Remote, User: c.User, UserNode: c.UserNode,
			Incoming: c.Incoming, State: c.State.String(), Window: c.Window,
			Unacked: c.Unacked, Queued: c.Queued, Started: c.Started.UTC(),
			Sent: c.Sent, Received: c.Received,
		})
	}

	return out
}

// maxAdminBody is the most of a request body an admin action reads.
const maxAdminBody = 4096

func decodeBody(r *http.Request, v any) error {
	var dec = json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxAdminBody))
	dec.DisallowUnknownFields()

	var err = dec.Decode(v)
	if err != nil {
		return errors.Join(errBadRequest, err)
	}

	return nil
}

// linkRequest names an AX.25 link of the node's.
type linkRequest struct {
	Port   int    `json:"port"`
	Local  string `json:"local"`
	Remote string `json:"remote"`
}

// adminCloseLink asks the data link to close one of the node's AX.25 links: a
// user's, or one to a neighbour.
func adminCloseLink(n *webNode, r *http.Request) error {
	var req linkRequest

	var err = decodeBody(r, &req)
	if err != nil {
		return err
	}

	var local, lok = node.Address(req.Local)
	var remote, rok = node.Address(req.Remote)

	if !lok || !rok || !slices.ContainsFunc(n.config.Ports, func(p node.Port) bool { return p.Channel == req.Port }) {
		return errors.Join(errBadRequest, errors.New("no such port, or not a callsign"))
	}

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Source] = local
	addrs[ax25.Destination] = remote

	n.link.DisconnectRequest(addrs, 2, req.Port, nodeClient)

	return nil
}

func adminBroadcast(n *netromNode, _ *http.Request) error {
	n.router.BroadcastNow(n.now())

	return nil
}

func adminCloseCircuit(n *netromNode, r *http.Request) error {
	if !n.router.CloseCircuit(strings.ToUpper(r.PathValue("id")), n.now()) {
		return errNotFound
	}

	return nil
}

// neighbourRequest is a neighbour to lock.
type neighbourRequest struct {
	Port    int    `json:"port"`
	Call    string `json:"call"`
	Alias   string `json:"alias"`
	Quality int    `json:"quality"`
}

func adminLockNeighbour(n *netromNode, r *http.Request) error {
	var req neighbourRequest

	var err = decodeBody(r, &req)
	if err != nil {
		return err
	}

	var lerr = n.router.LockNeighbour(netrom.LockedNeighbour{Port: req.Port, Call: req.Call, Alias: req.Alias, Quality: req.Quality}, n.now())
	if lerr != nil {
		return errors.Join(errBadRequest, lerr)
	}

	return nil
}

func adminUnlockNeighbour(n *netromNode, r *http.Request) error {
	var port, perr = strconv.Atoi(r.PathValue("port"))
	if perr != nil {
		return errors.Join(errBadRequest, perr)
	}

	if !n.router.UnlockNeighbour(neighbourKey(port, r.PathValue("call")), n.now()) {
		return errNotFound
	}

	return nil
}

// nodeAdminToken is the admin API's bearer token, or "" for none.
func nodeAdminToken(misc *misc_config_s) string {
	var settings, ok = misc.node.Get()
	if !ok {
		return ""
	}

	return settings.adminToken
}
