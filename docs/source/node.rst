..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

Packet Node
===========

.. ai-label:: generated

With a ``netrom`` section in its YAML configuration (see :doc:`yaml-config`),
``samoyed-direwolf`` runs a packet node.
It exchanges NODES broadcasts with its neighbours,
routes :doc:`glossary/netrom` traffic between them,
and gives a station that connects to it a command shell.

Reaching the node
-----------------

A station reaches the shell by connecting to the node's callsign,
or to its alias where the alias is also a valid callsign,
over :doc:`glossary/ax25` on any of the node's ports,
or by connecting to the node over NET/ROM from another node.
A neighbour node connecting to exchange NET/ROM traffic gets no shell.

Everything the node itself says starts with ``ALIAS:CALL}``,
so a user several hops away can tell which node is speaking.

Commands
--------

Commands are not case sensitive, and most have a one-letter form.

``C <node>``
    Connect onwards to a node, by alias or callsign, over NET/ROM.

``C <port> <call> [V <digipeater> ...]``
    Connect onwards to a station on a port, over AX.25,
    by way of up to eight digipeaters.
    The link comes from the user's callsign with its SSID turned round -
    SSID *n* becomes 15-*n* -
    so it is told apart from any link the user has of their own.

``N [node]``
    List the nodes the node knows, or the routes to one of them.

``R``
    List the neighbours: the nodes heard directly.

``MH [port]``
    List the stations heard directly, on one port or all of them.

``P``
    List the ports.

``I``
    Show the information text the configuration gives.

``B``
    Disconnect.

``?``
    List the commands.

When a connection onwards closes,
the user is returned to the node's prompt.
A user idle for longer than the ``node`` section's ``idleTimeout`` is disconnected.

Watching the node
-----------------

The web interface,
when ``WEBPORT`` turns it on,
has a Node tab showing the node's neighbours, the nodes it knows,
its circuits and its AX.25 links.
The same is there as JSON for other programs:

``GET /api/node/info``
    The node's callsign, alias, ports and information text.

``GET /api/node/neighbours``, ``/api/node/nodes``
    The routing table: neighbours with their quality and how many routes go through each,
    and every node known with its routes.

``GET /api/node/circuits``, ``/api/node/links``
    The NET/ROM circuits and AX.25 links open,
    each link with its role:
    ``neighbour``, ``user`` (connected to the node)
    or ``downlink`` (opened onwards for a user).

``GET /api/node/heard``
    The stations heard directly on the node's ports.

MQTT
~~~~

With an ``mqtt`` section in the configuration,
the node publishes what happens to a broker,
as JSON under ``samoyed/<callsign>/``:

``link/up``, ``link/down``
    An AX.25 link came up or went,
    with ``port``, ``local``, ``remote``, ``incoming`` and ``role``.

``circuit/up``, ``circuit/down``
    A NET/ROM circuit came up or went,
    with ``local``, ``remote``, ``user`` and ``incoming``.

``routes/changed``
    The routing table changed,
    with how many ``destinations`` and ``neighbours`` it now has.

``heard``
    A station was heard directly on ``port``,
    for the first time or the first in fifteen minutes.

Each event also has ``time``, ``kind`` and ``node``,
and ``error`` when a link or circuit went other than in an orderly way.
``samoyed/<callsign>/status`` is retained,
``online`` while the node runs and ``offline`` when it stops or loses the broker.
Events are published at most once,
and any the broker cannot take, while it is unreachable, are lost.

Administration
--------------

With ``adminToken`` set in the ``node`` section,
the web interface takes these requests from a sysop,
who must send the token as ``Authorization: Bearer <token>``:

``POST /api/admin/nodes/broadcast``
    Send a NODES broadcast now.

``POST /api/admin/circuits/<id>/close``
    Close a circuit, by the ``id`` ``/api/node/circuits`` gives it.

``POST /api/admin/links/close``
    Close an AX.25 link, named by a JSON body with ``port``, ``local`` and ``remote``.

``POST /api/admin/neighbours``
    Lock a neighbour at a fixed quality,
    given by a JSON body with ``port``, ``call``, ``alias`` and ``quality``.

``DELETE /api/admin/neighbours/<port>/<call>``
    Unlock a neighbour, whose routes then age out like any other's
    unless its broadcasts refresh them.

The web interface serves plain HTTP,
so the token crosses the network in the clear:
reach it through a TLS-terminating reverse proxy,
or only from the machine it runs on.

.. code::

    $ curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/admin/nodes/broadcast
