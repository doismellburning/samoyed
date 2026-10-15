// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

// nodeClient is the client number the node shell is known to the data link
// by: the first after the AGW server's own, 0 to MAX_NET_CLIENTS-1.
const nodeClient = MAX_NET_CLIENTS

// linkClientRouter is the linkClients for when the data link has both AGW
// clients and the node shell to tell about links.  Each is handed what is
// about its own links, going by the client number the data link gives.
type linkClientRouter struct {
	agw  linkClients
	node linkClients
}

// newLinkClientRouter routes to agw and node, either of which may be nil
// when there is none.
func newLinkClientRouter(agw linkClients, node linkClients) *linkClientRouter {
	var r = new(linkClientRouter)

	r.agw = agw
	if r.agw == nil {
		r.agw = noLinkClients{}
	}

	r.node = node
	if r.node == nil {
		r.node = noLinkClients{}
	}

	return r
}

func (r *linkClientRouter) LinkEstablished(channel int, client int, remoteCall string, ownCall string, incoming bool) {
	if client == nodeClient {
		r.node.LinkEstablished(channel, client, remoteCall, ownCall, incoming)
	} else {
		r.agw.LinkEstablished(channel, client, remoteCall, ownCall, incoming)
	}
}

func (r *linkClientRouter) LinkTerminated(channel int, client int, remoteCall string, ownCall string, timeout bool) {
	if client == nodeClient {
		r.node.LinkTerminated(channel, client, remoteCall, ownCall, timeout)
	} else {
		r.agw.LinkTerminated(channel, client, remoteCall, ownCall, timeout)
	}
}

func (r *linkClientRouter) RecConnData(channel int, client int, remoteCall string, ownCall string, pid int, data []byte) {
	if client == nodeClient {
		r.node.RecConnData(channel, client, remoteCall, ownCall, pid, data)
	} else {
		r.agw.RecConnData(channel, client, remoteCall, ownCall, pid, data)
	}
}

func (r *linkClientRouter) OutstandingFramesReply(channel int, client int, ownCall string, remoteCall string, count int) {
	if client == nodeClient {
		r.node.OutstandingFramesReply(channel, client, ownCall, remoteCall, count)
	} else {
		r.agw.OutstandingFramesReply(channel, client, ownCall, remoteCall, count)
	}
}
