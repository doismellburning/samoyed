// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

// The node on the web interface: read-only views of its tables for anyone,
// and actions for its sysop, who must give the admin token.

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
	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/doismellburning/samoyed/internal/webui"
)

// nodeQueryTimeout is how long a web request waits for the node to answer.
const nodeQueryTimeout = 5 * time.Second

// nodeWeb serves the node's part of the web interface.  The web interface
// starts before the node does, so the node is handed over once there is one;
// until then, and without one, the routes answer 404.
type nodeWeb struct {
	node  atomic.Pointer[netromNode]
	token string
}

func newNodeWeb(adminToken string) *nodeWeb {
	var w = new(nodeWeb)
	w.token = adminToken

	return w
}

// set hands over the node, or nil for none.
func (w *nodeWeb) set(n *netromNode) {
	w.node.Store(n)
}

// routes returns the routes to serve.  The admin routes are there only with an
// admin token configured.
func (w *nodeWeb) routes() []webui.Route {
	var routes = []webui.Route{
		{Pattern: "GET /api/node/info", Handler: w.view(nodeInfo)},
		{Pattern: "GET /api/node/nodes", Handler: w.view(nodeNodes)},
		{Pattern: "GET /api/node/neighbours", Handler: w.view(nodeNeighbours)},
		{Pattern: "GET /api/node/circuits", Handler: w.view(nodeCircuits)},
		{Pattern: "GET /api/node/links", Handler: w.view(nodeLinks)},
		{Pattern: "GET /api/node/heard", Handler: w.view(nodeHeard)},
	}

	if w.token == "" {
		return routes
	}

	for _, r := range []webui.Route{
		{Pattern: "POST /api/admin/nodes/broadcast", Handler: w.action(adminBroadcast)},
		{Pattern: "POST /api/admin/circuits/{id}/close", Handler: w.action(adminCloseCircuit)},
		{Pattern: "POST /api/admin/links/close", Handler: w.action(adminCloseLink)},
		{Pattern: "POST /api/admin/neighbours", Handler: w.action(adminLockNeighbour)},
		{Pattern: "DELETE /api/admin/neighbours/{port}/{call}", Handler: w.action(adminUnlockNeighbour)},
	} {
		r.Handler = webui.RequireToken(w.token, r.Handler)
		routes = append(routes, r)
	}

	return routes
}

// view serves what f makes of the node, made on the node's goroutine.
func (w *nodeWeb) view(f func(n *netromNode) any) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var n = w.node.Load()
		if n == nil {
			http.NotFound(rw, r)

			return
		}

		var v any

		var err = n.query(r.Context(), func() { v = f(n) })
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

// action runs f on the node's goroutine, answering 204 when it succeeds.
func (w *nodeWeb) action(f func(n *netromNode, r *http.Request) error) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var n = w.node.Load()
		if n == nil {
			http.NotFound(rw, r)

			return
		}

		var err error

		var qerr = n.query(r.Context(), func() { err = f(n, r) })

		switch {
		case qerr != nil:
			http.Error(rw, "node busy", http.StatusServiceUnavailable)
		case errors.Is(err, errBadRequest):
			http.Error(rw, err.Error(), http.StatusBadRequest)
		case errors.Is(err, errNotFound):
			http.Error(rw, err.Error(), http.StatusNotFound)
		case err != nil:
			http.Error(rw, err.Error(), http.StatusInternalServerError)
		default:
			rw.WriteHeader(http.StatusNoContent)
		}
	})
}

// query runs f on the node's goroutine and waits for it, giving up after
// nodeQueryTimeout or when ctx is done.
func (n *netromNode) query(ctx context.Context, f func()) error {
	var cctx, cancel = context.WithTimeout(ctx, nodeQueryTimeout)
	defer cancel()

	var done = make(chan struct{})

	select {
	case n.work <- func() { f(); close(done) }:
	case <-cctx.Done():
		return cctx.Err()
	}

	select {
	case <-done:
		return nil
	case <-cctx.Done():
		return cctx.Err()
	}
}

type nodeInfoJSON struct {
	Call  string     `json:"call"`
	Alias string     `json:"alias"`
	Ports []portJSON `json:"ports"`
	Info  string     `json:"info"`
	Time  time.Time  `json:"time"`
}

type portJSON struct {
	Number      int    `json:"number"`
	Description string `json:"description"`
	NetROM      bool   `json:"netrom"`
}

func nodeInfo(n *netromNode) any {
	var ports = make([]portJSON, 0, len(n.ports))
	for _, p := range n.ports {
		var _, netromPort = n.cfg.PortConfig(p.Number)
		ports = append(ports, portJSON{Number: p.Number, Description: p.Description, NetROM: netromPort})
	}

	return nodeInfoJSON{Call: n.cfg.Call, Alias: n.cfg.Alias, Ports: ports, Info: n.shellCfg.Info, Time: n.now().UTC()}
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
		var s, linked = n.sessions[axKey{port: nb.Port, own: n.cfg.Call, remote: nb.Call}]

		out = append(out, neighbourJSON{
			Port: nb.Port, Call: nb.Call, Alias: nb.Alias, Quality: nb.Quality, Locked: nb.Locked,
			Routes: n.router.Table().RoutesVia(nb.NeighbourKey),
			Linked: linked && s.up,
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

type linkJSON struct {
	Port     int      `json:"port"`
	Local    string   `json:"local"`
	Remote   string   `json:"remote"`
	Via      []string `json:"via"`
	Role     string   `json:"role"`
	Up       bool     `json:"up"`
	Incoming bool     `json:"incoming"`
}

func nodeLinks(n *netromNode) any {
	var out = []linkJSON{}

	for _, s := range n.sessions {
		var via = []string{}
		for i := ax25.Repeater1; i < s.numAddr; i++ {
			via = append(via, s.addrs[i])
		}

		out = append(out, linkJSON{Port: s.key.port, Local: s.key.own, Remote: s.key.remote, Via: via, Role: s.role, Up: s.up, Incoming: s.incoming})
	}

	slices.SortFunc(out, func(a, b linkJSON) int {
		if a.Port != b.Port {
			return a.Port - b.Port
		}

		if c := strings.Compare(a.Remote, b.Remote); c != 0 {
			return c
		}

		return strings.Compare(a.Local, b.Local)
	})

	return out
}

type heardJSON struct {
	Port   int       `json:"port"`
	Call   string    `json:"call"`
	Last   time.Time `json:"last"`
	Frames int       `json:"frames"`
}

func nodeHeard(n *netromNode) any {
	var out = []heardJSON{}
	for _, h := range n.heard.Stations(-1) {
		out = append(out, heardJSON{Port: h.Port, Call: h.Call, Last: h.Last.UTC(), Frames: h.Frames})
	}

	return out
}

func adminBroadcast(n *netromNode, _ *http.Request) error {
	n.router.BroadcastNow(n.now())

	return nil
}

func adminCloseCircuit(n *netromNode, r *http.Request) error {
	var id = strings.ToUpper(r.PathValue("id"))
	if !n.router.CloseCircuit(id, n.now()) {
		return errNotFound
	}

	return nil
}

// linkRequest names an AX.25 link of the node's.
type linkRequest struct {
	Port   int    `json:"port"`
	Local  string `json:"local"`
	Remote string `json:"remote"`
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

func adminCloseLink(n *netromNode, r *http.Request) error {
	var req linkRequest

	var err = decodeBody(r, &req)
	if err != nil {
		return err
	}

	var s, ok = n.sessions[axKey{port: req.Port, own: normalisedCall(req.Local), remote: normalisedCall(req.Remote)}]
	if !ok {
		return errNotFound
	}

	n.links.DisconnectRequest(s.addrs, s.numAddr, s.key.port, n.client)

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

	if !n.router.UnlockNeighbour(netrom.NeighbourKey{Port: port, Call: normalisedCall(r.PathValue("call"))}, n.now()) {
		return errNotFound
	}

	return nil
}
