..
    SPDX-FileCopyrightText: 2026 The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

BBS
===

.. ai-label:: generated

.. warning::

    ``samoyed-bbs`` is experimental.
    Expect missing features, and changes - to its configuration, to what it sends other BBSes,
    and to its message database - without notice,
    and don't rely on it for mail that matters.
    In particular, it can't yet forward mail to other BBSes,
    and the ``R:`` lines it writes haven't been checked against other BBSes' (see below).
    It says so when it starts, in ``--help``, and in the banner it greets stations with.

``samoyed-bbs`` is a packet bulletin board:
users leave personal messages for each other and bulletins for everyone,
and list, read and kill them.
It is early days.
Stations reach it over the air through an AGW server, such as ``samoyed-direwolf``,
and it can also run a session on its own console,
so it can be tried, and its messages looked after, without a radio.

Running it
----------

Write a configuration file, ``samoyed-bbs.yaml`` by default:

.. code:: yaml

    call: Q1BBS
    store: /var/lib/samoyed-bbs/bbs.db

Check it, then start a session as yourself:

.. code::

    $ samoyed-bbs -c samoyed-bbs.yaml --config-check
    Configuration file samoyed-bbs.yaml is OK.

    $ samoyed-bbs -c samoyed-bbs.yaml --console Q1TEST
    Welcome to Q1BBS, a Samoyed BBS (experimental).
    [SAMBBS-0.0-$]
    de Q1BBS>

The line in square brackets is the BBS's system ID,
which a BBS sends to say what it can do.
The session ends with ``B``, or at the end of the input,
so a session can be piped in as well as typed.
``-v`` logs sessions starting and ending, which otherwise stay out of the way of the session itself.

The console is trusted:
whoever is at it runs the BBS,
so it can read and kill anyone's messages.

On the air
----------

Given an ``agw`` block, ``samoyed-bbs`` attaches to an AGW server,
registers its callsign on the server's ports,
and serves each station that connects to that callsign:

.. code:: yaml

    call: Q1BBS
    store: /var/lib/samoyed-bbs/bbs.db
    agw:
      addr: localhost:8000

.. code::

    $ samoyed-bbs -c samoyed-bbs.yaml

For ``samoyed-direwolf``, the address is its ``AGWPORT``, 8000 unless it says otherwise.
If it has an ``AGWLOGIN``, give the BBS the same user name,
and its password in an environment variable,
so that the password itself isn't in the BBS's file:

.. code:: yaml

    agw:
      addr: localhost:8000
      user: Q1BBS
      passwordEnv: BBS_AGW_PASSWORD

The BBS waits for the AGW server if it isn't up yet,
and attaches again if it goes away and comes back.
It waits for what it last sent a station to be acknowledged before hanging up,
so a farewell, or the end of a long message, isn't cut off.

Stations connect to the BBS's own callsign.
A node on the same ``samoyed-direwolf`` can't connect a user to the BBS on the same channel without transmitting:
a callsign registered over AGW only takes connections arriving from the radio.

Configuration
-------------

``call``
    The BBS's callsign, with an optional SSID from 0 to 15.
    Messages sent here get their IDs from it.

``hroute``
    Optionally, where the BBS is,
    as the rest of its hierarchical address after its callsign,
    most specific first, such as ``#77.GBR.EURO``.
    It goes in the ``R:`` line the BBS puts on each message it takes.

``store``
    The path of the message database, which is created if it isn't there.

``banner``
    What a station is greeted with.
    Use ``|`` for several lines.
    Left out, it says which BBS this is, and that it is experimental.

``prompt``
    What a user is prompted with for each command.
    Left out, ``de`` and the BBS's callsign, as in ``de Q1BBS>``.

``idleTimeout``
    How long a station may say nothing before it is disconnected, such as ``5m``.
    Left out, ten minutes.

``agw``
    The AGW server stations reach the BBS through.
    Left out, the BBS can only be used from its console.
    It has:

    ``addr``
        The server's ``host:port``.

    ``user``
        Optionally, the user name to log in with,
        for a server that wants a login.

    ``passwordEnv``
        The environment variable the password is in.
        Needed with ``user``.

    ``ports``
        Optionally, the server's ports to take connections on, counting from 0,
        such as ``[0]``.
        Left out, every port the server has.

    ``paclen``
        Optionally, the most the BBS sends the server at a time, in bytes,
        from 1 to 2048.
        Matching the TNC's own ``PACLEN`` keeps frames full.

    ``highWater``
        Optionally, how many of a station's frames may be unacknowledged before the BBS waits to send more,
        so a long message doesn't flood the TNC's transmit queue.

``--config-check`` reports every problem in the file at once,
and its exit status is 1 if there are any,
so it can go in a systemd unit's ``ExecStartPre``.

Commands
--------

``L``
    List messages, newest first.
    ``L n`` lists from number ``n`` up, and ``L n-m`` from ``n`` to ``m``.
    ``LM`` lists messages to you,
    ``LB`` bulletins, and ``LP`` personal messages.
    ``L< call`` lists messages from a station, and ``L> call`` to one.

``R n``
    Read message ``n``, or several: ``R 1 2 3``.
    Reading a personal message to you marks it read.
    The BBSes a message has come through are shown as its path;
    ``RH n`` shows their ``R:`` lines in full.

``S call``
    Send a personal message (``SP`` does the same).
    ``SB area`` sends a bulletin, such as ``SB ALL``,
    and ``ST`` NTS traffic.
    Add ``@bbs`` to the address for a station on another BBS,
    and ``$`` and an ID to give the message an ID of its own.
    The BBS asks for a title,
    then takes the message a line at a time until ``/EX``, or a Ctrl-Z,
    on a line of its own.
    A message whose ID has been seen before isn't saved again.

``K n``
    Kill message ``n``: one of your own, to or from you.

``H``, ``?``
    List the commands.

``B``
    Disconnect.

Bulletins are for everyone to read.
Personal messages and traffic are seen only by whoever they are from or to.

Addresses and R: lines
----------------------

An address after the ``@`` is hierarchical:
the BBS, then where it is, most specific first,
as in ``Q1TEST@Q1BBS.#77.GBR.EURO``.
A bulletin's is the area it is for, such as ``ALL@GBR``.

Each BBS a message passes through puts a line at the top of it,
saying when it took the message and which BBS it is:

.. code::

    R:261101/0905Z @:Q1BBS.#77.GBR.EURO

That is the form this BBS writes.
It hasn't yet been checked against what FBB, BPQ and XRouter write and expect,
so it may change;
it reads other BBSes' lines leniently, needing only the date, the time and the BBS's callsign.
