..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

NET/ROM
=======

.. ai-label:: generated

A network and transport layer above :doc:`ax25`:
nodes learn routes from each other's ``NODES`` broadcasts,
and carry a user's data end to end over circuits that may cross several nodes.
Its frames travel in AX.25 frames with PID 0xCF.
:ref:`Running a NET/ROM node <how-to-guides:Run a NET/ROM node>` covers configuring one.

In Samoyed
----------

Samoyed follows the NET/ROM protocol description below,
with these differences and choices:

* **Trivial loops are dropped.**
  A broadcast entry whose best neighbour is the receiving node itself
  is ignored, as netromd does,
  rather than kept at quality 0 as the protocol description has it.
* **Ageing routes are not passed on.**
  A route's obsolescence count starts at 6 and drops by one at each of the node's own broadcasts,
  as the protocol description has it,
  but a route is not broadcast once its count is below 4,
  as netromd does.
* **Traffic teaches routes too.**
  A frame arriving from a node the table does not know
  adds a route back to it through the neighbour it came from, at quality 0,
  as the Linux kernel does.
  Such a route is used only if there is nothing better,
  and never broadcast.
* **One routing table across several channels.**
  A node can run on several channels,
  sending its ``NODES`` broadcasts on each.
  A neighbour is a callsign on a channel,
  as the Linux kernel has it a callsign on a device,
  so the same callsign heard on two channels is two neighbours,
  each a route of its own.
* **A failing neighbour is passed over.**
  After two failed attempts in a row to link to a neighbour,
  routes through it are skipped until it is heard from again.
* **Limits.**
  The table holds at most 1000 destinations,
  and a node at most 256 circuits.
* **Timers**, which the protocol description leaves to the implementation,
  are those of the Linux kernel:
  a window of 4,
  retransmission after 120 seconds up to 3 times,
  acknowledgement within 5 seconds,
  and a CHOKE honoured for up to 180 seconds.
  Unlike the kernel, answering a NAK restarts the retransmission timer rather than stopping it,
  since stopping it leaves nothing to retry with if the resent frame is lost too.
* **BPQ extensions** are understood and answered in kind:
  the transport timeout on a CONNECT REQUEST,
  and the time to live on a CONNECT ACKNOWLEDGE.
* **A minimum quality of 0** accepts every route,
  rather than turning broadcasts off as in the protocol description.
* **A malformed broadcast entry** is skipped,
  where netromd abandons the rest of the broadcast.
* **One node, users and neighbours alike.**
  Configured with the node shell,
  NET/ROM shares the shell's callsign and alias.
  A link made to the node is a neighbouring node's
  if the first thing to arrive on it is a NET/ROM frame,
  sent to the node's callsign on one of NET/ROM's channels,
  and a user's otherwise;
  a neighbour is greeted as a user is before that,
  and NET/ROM ignores the greeting.
* **Defaults**: a time to live of 16,
  a broadcast every hour,
  a quality of 192 for every neighbour,
  and a minimum quality of 10.

Not implemented:

* IP over NET/ROM (opcode 0) and XRouter's circuit reset (opcode 7):
  frames with either are ignored.
* The RS232 interconnect between TNCs,
  routes entered by hand,
  and neighbours reached through digipeaters:
  a broadcast that arrived through a digipeater is ignored.
* INP3 routing.
* An inactivity timeout:
  an idle circuit stays open until one end closes it.

References
----------

* `The NET/ROM Protocol <https://wiki.oarc.uk/_media/packet:thenetromprotocol.pdf>`__
  (Software 2000, hosted by OARC):
  the frame layouts, the transport messages, the ``NODES`` broadcast and the routing rules.
* The Linux kernel's NET/ROM implementation, as of v6.6:
  `net/netrom <https://github.com/torvalds/linux/tree/v6.6/net/netrom>`__
  and `include/net/netrom.h <https://github.com/torvalds/linux/blob/v6.6/include/net/netrom.h>`__:
  the timers, routes learned from traffic, the BPQ extensions,
  and a neighbour identified by its callsign and device (``nr_neigh_get_dev`` in ``nr_route.c``).
* netromd, the routing daemon in ax25tools:
  `ax25tools/netrom <https://github.com/ve7fet/linuxax25/tree/master/ax25tools/netrom>`__:
  trivial loops, the broadcast threshold and malformed entries.
