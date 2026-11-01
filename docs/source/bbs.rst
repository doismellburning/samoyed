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
    In particular, it doesn't reach partners over TCP yet, nor forward compressed,
    and its forwarding has been tried against LinBPQ alone, not yet FBB or XRouter (see below).
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
    [SAMBBS-0.0-H$]
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
    A forwarding partner takes a line ending in ``>`` as the prompt,
    so one that doesn't end in ``>`` gets one added for partners.

``idleTimeout``
    How long a station may say nothing before it is disconnected, such as ``5m``.
    Left out, ten minutes.

``fbb``
    ``true`` to forward in FBB's protocol with partners that can, as described under Forwarding below,
    by claiming ``F`` in this BBS's system ID.
    Left out, the BBS forwards in the MBL/RLI style with every partner:
    it forwards in FBB's protocol only uncompressed so far,
    and LinBPQ turns that away.

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

``partners``
    The BBSes this one forwards mail to, as described under Routing below.
    Each has:

    ``call``
        The partner's callsign.

    ``covers``
        Where personal mail and traffic is sent to this partner for,
        as hierarchical addresses such as ``GBR.EURO``,
        or ``*`` for anywhere.
        Mail for the partner itself always goes to it.

    ``floods``
        The bulletin distributions passed on to this partner,
        such as ``GBR`` or ``WW``,
        or ``*`` for all of them.

    ``connect``
        Optionally, how this BBS calls the partner, as described under Calling partners below.
        Left out, the partner calls this BBS.
        It has:

        ``port``
            The AGW server port to call on, counting from 0.

        ``to``
            Optionally, who to connect to, if not the partner itself:
            a node, say, which the script then tells to connect on.

        ``via``
            Optionally, digipeaters to connect through.

        ``script``
            Optionally, steps to take once connected, to reach the partner itself.
            Each is one of
            ``send``, a line to send;
            ``sendEnv``, the name of an environment variable whose value is sent,
            for a password, which is then never in the file or the logs;
            or ``expect``, text to wait for in a line from the far end, in any case.

        ``every``
            How often to call, mail queued or not, such as ``30m``.
            Left out, an hour; at least a minute.

        ``timeout``
            How long each ``expect`` waits.
            Left out, two minutes.

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
A blank line keeps a message's ``R:`` lines apart from its text,
as on LinBPQ's messages:
XRouter takes the lines above the first blank line for headers,
so without it, the text doesn't show.
LinBPQ reads this BBS's ``R:`` lines,
taking a message's date from it,
and writes its own as ``R:261101/0905Z 12@Q3BPQ.#33.GBR.EURO LinBPQ6.0.26``,
the message's number there before the ``@``,
which this BBS reads in turn.
FBB's and XRouter's haven't been checked yet,
so the form may change;
it reads other BBSes' lines leniently, needing only the date, the time and the BBS's callsign.

Routing
-------

Each message sent at the BBS, by a user or a partner, is routed as it is taken,
and queued for the partners it is to go on to,
until it is forwarded (see below).

Personal mail and traffic for a user here,
with no ``@``, or with an ``@`` naming this BBS,
stays here.
Anything else goes to the one partner whose ``covers`` best fit its address:
mail for the partner itself first,
then the cover that matches the most of the address,
read from its least specific end,
then a ``*``.
With equal fits, the partner listed first wins.
If nothing fits, the message is held for the sysop,
and its sender is told so.

A bulletin is kept here to be read,
and passed to every partner whose ``floods`` include its distribution,
the area after its ``@``.
A bulletin with no ``@`` stays here.

Neither is ever sent to a partner it came from,
or to a BBS already in its ``R:`` lines.

.. code:: yaml

    call: Q1BBS
    hroute: "#77.GBR.EURO"
    store: /var/lib/samoyed-bbs/bbs.db
    partners:
      - call: Q2BBS
        covers: ["#77.GBR.EURO"]
        floods: ["#77", GBR]
      - call: Q3BBS
        covers: ["*"]
        floods: ["*"]

Forwarding
----------

A partner forwards mail by connecting to the BBS's callsign, as a user would,
and sending its system ID, such as ``[FBB-7.0-ABFHM$]``, as its first line.
A station that isn't one of the ``partners`` is turned away.
As with users, the callsign is only the station's word for who it is.

This BBS's system ID, ``[SAMBBS-0.0-H$]``,
says it understands hierarchical addresses (``H``) and message IDs (``$``),
and with ``fbb: true``, ``[SAMBBS-0.0-FH$]``, FBB's protocol (``F``) too.
With a partner that claims ``F`` as well, the two forward in FBB's protocol;
otherwise in the plain MBL/RLI style.
Whichever made the connection sends first.

FBB's protocol
~~~~~~~~~~~~~~

Mail goes in blocks.
The side sending offers up to five messages at once, in a proposal such as::

    FB P Q2TEST Q1BBS.#77.GBR.EURO Q1TEST 123_Q2BBS 1345
    FB B Q2TEST GBR ALL 124_Q2BBS 512
    F> 3A

each line the message's type, sender, ``@``, addressee, ID and size,
and the last a checksum.
The other side answers with a sign for each:
``+`` for one it wants,
``-`` for one it has seen or can't take,
``=`` for one it wants later,
as in ``FS +-``,
and the messages it wants follow,
each its title, its text and a Ctrl-Z.
Then the other side takes its turn, with its own proposal,
or ``FF`` if it has nothing;
either says it had everything sent to it,
and only then is that marked forwarded.
A side with nothing to send after ``FF`` says ``FQ``, which ends the exchange.
A proposal stops short of about 10KB of messages, though it always offers one.

That is the protocol as F6FBB's documentation describes it,
uncompressed;
compressed forwarding is still to come.
LinBPQ 6.0.26.1 doesn't take it uncompressed:
offered it, it logs "Uncompressed Blocked Forwarding is no longer supported",
and hangs up,
so ``fbb`` is for partners running FBB itself, and is off unless it is set.

MBL/RLI style
~~~~~~~~~~~~~

With a partner that doesn't claim ``F``, mail goes one message at a time.
The side sending offers each message with a line such as::

    SP Q1TEST @ Q1BBS.#77.GBR.EURO < Q2TEST $123_Q2BBS

which this BBS answers ``OK``, or ``NO`` for one whose ID it has seen before or that it can't make sense of.
An accepted message follows as its title, its body and ``/EX``.
After each message, and each ``NO``,
the side receiving says it is ready for the next:
with ``F>`` if it made the connection,
or with its prompt if it was called.
A partner with nothing more to send hands this BBS the turn with ``F>``,
and it offers whatever is queued for the partner the same way,
marking each forwarded once the partner says it is ready again, or refused if the partner says ``NO``.
With nothing more of its own, it ends with ``*** DONE``.

A message taken from a partner gets this BBS's ``R:`` line on top of the others,
and is routed on like any other,
though never back to the partner it came from.

That is the exchange as seen forwarding with LinBPQ both ways.
FBB and XRouter haven't been tried yet,
so it may change.

Calling partners
----------------

A partner with a ``connect`` block is called by this BBS,
over the same AGW server stations reach it through.
It is called as soon as mail is queued for it,
and every so often whatever is queued,
so that it can send what it has.
A partner already forwarding with this BBS isn't called,
and one calling in while this BBS is forwarding with it is turned away,
so the two never work through the same mail at once.

Once connected, the BBS runs the partner's script, if it has one,
and then forwards as the side that called:
it sends what it has first,
and then hands the partner the turn.
This has been tried with LinBPQ,
calling its BBS directly and through its node,
whose ``BBS`` command connects on to it.

.. code:: yaml

    partners:
      - call: Q3BPQ
        covers: ["*"]
        floods: ["*"]
        connect:
          port: 0
          to: Q3NOD
          script:
            - send: C Q3BPQ
            - expect: Connected to
          every: 30m

A call that fails - no answer, a script step that times out,
or the partner hanging up before the exchange is over -
is tried again after a minute,
then after two, four and so on, up to the partner's ``every``.
Each failure counts against the mail waiting for that partner,
and mail that has waited through five is held for the sysop:
personal mail and traffic outright,
and a bulletin only for that partner, since it is here to be read too.

References
----------

- LinBPQ 6.0.26.1, as forwarded with:
  https://www.cantab.net/users/john.wiseman/Downloads/Beta/
- F6FBB, "FBB forward protocol", appendix 9 of FBB's documentation:
  https://www.qsl.net/pe1nmb/bbs/fbb/docfwpro.htm
- F6FBB, "Compressed forward protocol", FBB's documentation, for the checksum:
  https://www.qsl.net/pe1nmb/bbs/fbb/docfwcom.htm
