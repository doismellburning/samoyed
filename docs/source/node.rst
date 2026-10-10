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
