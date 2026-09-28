..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: GPL-2.0-or-later

Protocols
=========

Notes on protocols samoyed speaks, beyond AX.25 itself: what they put on the
air, and what samoyed does and does not implement of them.

NET/ROM
-------

🤖 *This section was written by an AI assistant (Claude), from the sources
listed under References below, and has not yet been checked against other
implementations on the air.*

NET/ROM is implemented by, among others, the Linux kernel, BPQ32 and XRouter,
whose extensions to it the Linux implementation takes account of.  It adds two
layers above AX.25:

* A **network layer**, which routes frames from node to node towards their
  destination, learning routes from broadcasts.
* A **transport layer**, which carries a user's data between two nodes, end to
  end, in order and without loss, over a *circuit* that may cross several
  nodes.

Everything NET/ROM sends goes in the information field of an AX.25 frame with
PID 0xCF.  :ref:`How to run a node <Run a NET/ROM node>` covers configuring
one.

Routing broadcasts
~~~~~~~~~~~~~~~~~~

Every so often (hourly, by default, in samoyed) a node sends a ``NODES``
broadcast: a UI frame from its callsign to the pseudo-callsign ``NODES``,
listing the nodes it can reach.

========== ===== =============================================================
Offset     Bytes Content
========== ===== =============================================================
0          1     0xFF, the signature
1          6     The sender's alias, space padded
7          21    An entry, of which there may be up to eleven:
========== ===== =============================================================

Each entry is a destination's callsign (7 bytes, encoded as in an AX.25
address field), its alias (6 bytes, space padded), the neighbour through which
the sender best reaches it (7 bytes) and the quality of that route (1 byte).
Eleven entries is the most that fit in the 256 bytes the Linux routing daemon
allows itself, so a large table goes out as several broadcasts, each with the
signature and alias.

A node hearing a broadcast from a neighbour:

* Takes the neighbour itself to be reachable directly, at the quality it
  assigns to links (``QUALITY``, default 192 in samoyed).
* Takes each entry to be reachable through that neighbour, at a quality of
  ``(link quality × advertised quality + 128) / 256``.  Every hop therefore
  makes a route worse, and a short way round beats a long one.
* Ignores an entry whose best neighbour is itself.  That destination is reached
  through *this* node, so going to it by way of the neighbour would only bring
  the traffic back.  Without this, two nodes can keep a destination that has
  gone alive between them, each learning it afresh from the other.
* Ignores a route below the minimum quality (``MINQUAL``, default 10), or one
  to itself.

Each destination keeps its best three routes.  A route's *obsolescence count*
starts at 6 and loses one each time the node itself broadcasts; a route is
dropped when it reaches zero, and is not passed on in broadcasts once it is
below 4.  Routes therefore age on the node's own clock, so a neighbour that
falls silent takes its routes with it.

Frames between nodes
~~~~~~~~~~~~~~~~~~~~

Everything other than a broadcast travels between neighbouring nodes over an
ordinary **connected-mode AX.25 link**, one I frame per NET/ROM frame.  A node
makes the link when it first has something for that neighbour, and answers
links other nodes make to its callsign.

Each frame starts with a 15-byte network header and a 5-byte transport header:

====== ===== ====================================================================
Offset Bytes Content
====== ===== ====================================================================
0      7     Origin node callsign
7      7     Destination node callsign
14     1     Time to live: hops remaining, decremented by each node that
             forwards the frame, which is dropped when it runs out
15     1     Circuit index
16     1     Circuit ID
17     1     Transmit sequence number, or (CONNECT ACK) the sender's circuit
             index
18     1     Receive sequence number, or (CONNECT ACK) the sender's circuit ID
19     1     Opcode (low four bits) and flags (high four bits)
====== ===== ====================================================================

The circuit index and ID in bytes 15 and 16 always name a circuit: on a
CONNECT REQUEST the sender's own, and on everything else the recipient's,
which is how the recipient finds the circuit a frame belongs to.

========= ===============================================================
Opcode    Meaning, and what follows the header
========= ===============================================================
1         CONNECT REQUEST: proposed window (1 byte), user callsign (7),
          originating node callsign (7), and optionally the originator's
          transport timeout in seconds (2, low byte first; a BPQ extension)
2         CONNECT ACKNOWLEDGE: accepted window (1 byte), and optionally the
          answering node's TTL (1; a BPQ extension).  With the CHOKE flag,
          the request is refused.
3         DISCONNECT REQUEST
4         DISCONNECT ACKNOWLEDGE
5         INFORMATION: the data
6         INFORMATION ACKNOWLEDGE
========= ===============================================================

The flags are CHOKE (0x80: stop sending for now; on a CONNECT ACKNOWLEDGE, a
refusal), NAK (0x40: send again from the acknowledged sequence number) and MORE
(0x20: this message continues in the next INFORMATION frame).

Circuits
~~~~~~~~

samoyed's transport follows the Linux implementation's:

* Sequence numbers run modulo 256, with a window of 4 frames sent ahead of
  acknowledgement, or fewer if the other end asks for fewer.
* Every INFORMATION frame carries the sender's receive sequence number, which
  acknowledges the other direction's frames.  An INFORMATION ACKNOWLEDGE does
  the same when there is no data to carry it: within 5 seconds of data
  arriving, or at once when the sender's window is full.
* Frames arriving out of order within the window are held and put back in
  sequence.
* A message longer than 236 bytes is split over several INFORMATION frames,
  all but the last with MORE set, and put back together at the far end.
* Anything unacknowledged is sent again after 120 seconds (T1), up to three
  times, after which the circuit is abandoned.  The same timer governs repeats
  of a CONNECT REQUEST or DISCONNECT REQUEST.
* CHOKE is honoured for up to 180 seconds.

samoyed differs from Linux in one respect: answering a NAK restarts the
retransmission timer rather than stopping it, since stopping it leaves nothing
to retry with if the resent frame is lost too.

Not implemented
~~~~~~~~~~~~~~~

* IP over NET/ROM (opcode 0, protocol extensions), and XRouter's reset
  (opcode 7).  Frames with either are ignored.
* INP3 routing.
* A node user interface: stations cannot connect to the node over AX.25 and
  give it commands.  Applications drive circuits through the AGW port instead.
* Applications with callsigns and aliases of their own, advertised in the
  node's broadcasts as BPQ allows.
* An inactivity timeout: an idle circuit stays open until one end closes it.

References
~~~~~~~~~~

* The Linux kernel's NET/ROM implementation, as of v6.6:
  `net/netrom <https://github.com/torvalds/linux/tree/v6.6/net/netrom>`__ and
  `include/net/netrom.h
  <https://github.com/torvalds/linux/blob/v6.6/include/net/netrom.h>`__.  The
  frame layouts, the transport state machine and its timers here follow it.
* The Linux NET/ROM routing daemon, netromd, in ax25tools:
  `ax25tools/netrom
  <https://github.com/ve7fet/linuxax25/tree/master/ax25tools/netrom>`__.  The
  broadcast format and the route quality rules here follow it.
