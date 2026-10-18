// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

// nodeClient is the client number the node shell is known to the data link
// by: the first after the AGW server's own, 0 to MAX_NET_CLIENTS-1.
const nodeClient = MAX_NET_CLIENTS

// netromClient is the client number the NET/ROM node's links to its
// neighbours are known to the data link by.
const netromClient = MAX_NET_CLIENTS + 1

// linkClientRouter is the linkClients for when the data link has AGW
// clients, the node shell and the NET/ROM node to tell about links.  Each is
// handed what is about its own links, going by the client number the data
// link gives.
type linkClientRouter struct {
	agw    linkClients
	node   linkClients
	netrom linkClients
}

// newLinkClientRouter routes to agw, node and netrom, any of which may be
// nil when there is none.
func newLinkClientRouter(agw linkClients, node linkClients, netrom linkClients) *linkClientRouter {
	var r = new(linkClientRouter)

	r.agw = orNoLinkClients(agw)
	r.node = orNoLinkClients(node)
	r.netrom = orNoLinkClients(netrom)

	return r
}

func orNoLinkClients(c linkClients) linkClients { //nolint:ireturn // the one given, or a stand-in for none
	if c == nil {
		return noLinkClients{}
	}

	return c
}

// clientFor is who is told about client's links.
func (r *linkClientRouter) clientFor(client int) linkClients { //nolint:ireturn // choosing among them is the point
	switch client {
	case nodeClient:
		return r.node
	case netromClient:
		return r.netrom
	default:
		return r.agw
	}
}

func (r *linkClientRouter) LinkEstablished(channel int, client int, remoteCall string, ownCall string, incoming bool) {
	r.clientFor(client).LinkEstablished(channel, client, remoteCall, ownCall, incoming)
}

func (r *linkClientRouter) LinkTerminated(channel int, client int, remoteCall string, ownCall string, timeout bool) {
	r.clientFor(client).LinkTerminated(channel, client, remoteCall, ownCall, timeout)
}

func (r *linkClientRouter) RecConnData(channel int, client int, remoteCall string, ownCall string, pid int, data []byte) {
	r.clientFor(client).RecConnData(channel, client, remoteCall, ownCall, pid, data)
}

func (r *linkClientRouter) OutstandingFramesReply(channel int, client int, ownCall string, remoteCall string, count int) {
	r.clientFor(client).OutstandingFramesReply(channel, client, ownCall, remoteCall, count)
}
