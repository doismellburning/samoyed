..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

BBS
===

.. ai-label:: generated

With a ``bbs`` section in its YAML configuration (see :doc:`yaml-config`),
the packet node (see :doc:`node`) runs a bulletin board:
personal mail and bulletins,
kept under ``dataDir``,
read and written by users,
and forwarded to and from partner BBSes with :doc:`glossary/fbb`.

Reaching the BBS
----------------

A user reaches the BBS with the ``BBS`` command in the node's shell,
and returns to the node with ``B``.
With a ``call`` of its own, the BBS can also be connected to directly,
over :doc:`glossary/ax25` on any of the node's ports,
or over :doc:`glossary/netrom`,
where it is advertised in the node's NODES broadcasts under its ``alias``.

The BBS starts each session with its SID,
``[SAMOYED-<version>-B12FHM$]``,
then a greeting and its prompt, ``de <call>>``.

Commands
--------

``L [n]``
    List the latest messages, or those from number *n* on.

``LM``, ``LB``
    List your personal mail, or the bulletins.

``R n ...``
    Read messages.
    Personal mail can be read by its sender and its addressee;
    bulletins by anyone.

``S call[@bbs]``
    Send a personal message.
    The BBS asks for a subject, then the text,
    which ends with ``/EX`` (or Ctrl-Z) on a line of its own;
    ``/ABORT`` cancels it.

``SB to@distribution``
    Send a bulletin, such as ``SB ALL@GBR``.

``K n ...``
    Kill messages you sent, or personal mail sent to you.

``I``
    About the BBS.

``B``
    Leave.

Forwarding
----------

A partner BBS that connects and answers the BBS's SID with its own forwards there and then;
only partners in the configuration may.
The BBS connects to each partner in turn:
soon after mail for it arrives,
and every ``interval`` if one is set,
to collect any mail the partner has.

The two BBSes settle on the best protocol they share:
B1 (compressed, with a CRC),
B2 (Winlink's B2F messages, compressed),
plain B (compressed, without a CRC),
or FBB's uncompressed protocol.

Personal mail goes one way:
to the BBS it is addressed at, if that is a partner,
or else to the first partner whose ``routes`` match its @ field.
Bulletins go to every partner with ``bulletins: true`` whose ``routes`` match their distribution.
A message is never sent back to the partner it came from,
and mail for this BBS goes nowhere.
Each message forwarded gets an ``R:`` line on top saying when and where it passed through.

Administration
--------------

With ``adminToken`` set in the ``node`` section (see :doc:`node`),
the web interface takes these requests too:

``GET /api/admin/bbs/messages``
    Every message but its text.

``DELETE /api/admin/bbs/messages/<number>``
    Kill a message.

``GET /api/admin/bbs/partners``
    How forwarding with each partner is going.

``POST /api/admin/bbs/partners/<call>/forward``
    Connect to a partner now.

The node publishes ``bbs/message`` (see :doc:`node`) for each new message,
with its ``number``, ``type``, ``from``, ``to`` and ``at``.
