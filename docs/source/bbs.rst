..
    SPDX-FileCopyrightText: 2026 The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

BBS
===

.. ai-label:: generated

``samoyed-bbs`` is a packet bulletin board:
users leave personal messages for each other and bulletins for everyone,
and list, read and kill them.
It is early days.
For now the BBS serves only its own console -
a session on the terminal it was started from, as a given callsign -
so it can be tried, and its messages looked after, without a radio.
Stations reaching it over the air come next.

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
    Welcome to Q1BBS, a Samoyed BBS.
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

Configuration
-------------

``call``
    The BBS's callsign, with an optional SSID from 0 to 15.
    Messages sent here get their IDs from it.

``store``
    The path of the message database, which is created if it isn't there.

``banner``
    What a station is greeted with.
    Use ``|`` for several lines.
    Left out, it says which BBS this is.

``prompt``
    What a user is prompted with for each command.
    Left out, ``de`` and the BBS's callsign, as in ``de Q1BBS>``.

``idleTimeout``
    How long a station may say nothing before it is disconnected, such as ``5m``.
    Left out, ten minutes.

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
