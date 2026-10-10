..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

NET/ROM
=======

.. ai-label:: generated

A network and transport layer above :doc:`ax25`,
which lets a station connect through a network of nodes
to a node it cannot hear directly.

Nodes learn about each other from **NODES broadcasts**:
:doc:`ax25` UI frames to ``NODES`` with PID ``0xCF``.
Each starts with the byte ``0xFF`` and the sender's six-character alias,
then lists destinations the sender knows,
each as its callsign, its alias, the sender's best neighbour towards it
and that route's quality, from 0 to 255.
A node hearing a broadcast derates each quality by that of its link to the sender
- the product of the two, divided by 256 -
ignores routes whose best neighbour is itself,
and gives each route an obsolescence count that falls by one each broadcast interval,
so a route that stops being advertised is forgotten.

Between neighbouring nodes, NET/ROM packets travel in connected-mode :doc:`ax25` I frames with the same PID.
Each carries a network header
(origin node, destination node and a time to live, decremented at each hop)
and a five-byte transport header:
a circuit index and ID, send and receive sequence numbers,
and an opcode -
connect request and acknowledgement, disconnect request and acknowledgement,
information and information acknowledgement -
with choke, NAK and more-follows flags.
A **circuit** is a windowed, acknowledged, end-to-end connection carrying one user's session.

References
----------

* `Linux kernel NET/ROM implementation, net/netrom <https://github.com/torvalds/linux/tree/master/net/netrom>`__
* `ax25-tools, whose netromd sends and receives NODES broadcasts <https://launchpad.net/ubuntu/+source/ax25-tools>`__
