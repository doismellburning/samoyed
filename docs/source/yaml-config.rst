YAML Configuration
==================

As well as Dire Wolf's line-at-a-time format, ``samoyed-direwolf`` can read its
configuration from YAML.  A configuration file whose name ends in ``.yaml`` or
``.yml`` is read as YAML; anything else is read as before.

.. code::

    $ samoyed-direwolf -c samoyed.yaml --config-check

This is new, and only some directives have a YAML form so far.  Everything else
can still be used, from a ``legacy`` block (see below).

Example
-------

.. code:: yaml

    samoyed:
      audioDevices:
        - input: plughw:1,0
          output: plughw:2,0
          channels: 2
          rate: 48000

      channels:
        - channel: 0
          mycall: Q1TEST-1
          txdelay: 30
          txtail: 15
          modem:
            speed: "1200"
            tones: {mark: 1200, space: 2200}
            decoders: {count: 3, offset: 30}
          ptt:
            method: serial
            device: /dev/ttyUSB0
            line: rts
            line2: dtr
            invert2: true

        - channel: 1
          mycall: Q1TEST-2
          modem: {speed: "9600", type: g3ruh}
          ptt: {method: gpio, pin: 25, invert: true}
          il2ptx: {}

      agwPort: 8000
      kissPorts:
        - port: 8001
        - {port: 8002, channel: 1}

      legacy: |
        DIGIPEAT 0 0 ^WIDE[3-7]-[1-7]$|^TEST$ ^WIDE[12]-[12]$

That is the same configuration as:

.. code::

    ADEVICE plughw:1,0 plughw:2,0
    ACHANNELS 2
    ARATE 48000

    CHANNEL 0
    MYCALL Q1TEST-1
    TXDELAY 30
    TXTAIL 15
    MODEM 1200 1200:2200 3@30
    PTT /dev/ttyUSB0 RTS -DTR

    CHANNEL 1
    MYCALL Q1TEST-2
    MODEM 9600 G3RUH
    PTT GPIO -25
    IL2PTX

    AGWPORT 8000
    KISSPORT 8001
    KISSPORT 8002 1
    DIGIPEAT 0 0 ^WIDE[3-7]-[1-7]$|^TEST$ ^WIDE[12]-[12]$

Both are checked by the same code, so a setting means the same, and draws the
same complaints, whichever way it is written.

Reference
---------

Every setting goes under a top-level ``samoyed`` key,
leaving the rest of the file free for anything else that might one day want a place in it.
A file with settings outside it is refused.
These are the keys under ``samoyed``:

``audioDevices``
    A list of audio devices (``ADEVICE``, ``ACHANNELS``).  Each has ``input``,
    an optional ``output`` when transmitting goes somewhere else, and
    ``channels``, 1 for mono (the default) or 2 for stereo, and ``rate``, the
    sample rate (``ARATE``).  A device's number is its position in the list,
    unless it gives one with ``device``.

``channels``
    A list of radio channels.  Each needs ``channel``, its number, and may have:

    ``mycall``
        The station callsign (``MYCALL``).  As with ``MYCALL``, it is also used
        for any channel that does not set its own.

    ``txdelay``, ``txtail``, ``dwait``, ``slottime``
        Transmit timing, in 10 ms units (``TXDELAY``, ``TXTAIL``, ``DWAIT``,
        ``SLOTTIME``).

    ``persist``
        The chance, out of 255, of transmitting in a slot (``PERSIST``).

    ``fulldup``
        ``true`` for full duplex (``FULLDUP``).

    ``fx25tx``
        FX.25 transmission (``FX25TX``): 0 for off, 1 for automatic, or the
        number of parity bytes (16, 32 or 64).

    ``il2ptx``
        IL2P transmission (``IL2PTX``), with ``invert`` (``-``) to invert the
        polarity, ``maxfec: false`` (``0``) for the weaker FEC, and
        ``crc: false`` (``c``) to leave out the CRC.  ``il2ptx: {}`` turns it
        on with the defaults.  Given along with ``fx25tx``, IL2P wins.

    ``il2pversion``
        The IL2P version (``IL2PVERSION``): ``0.4``, ``0.6`` or ``compat``.

    ``modem``
        The modem (``MODEM``):

        * ``speed`` - bits per second, or ``AIS`` or ``EAS``.  Quote it, as
          ``"1200"``.
        * ``type`` - ``bpsk`` or ``g3ruh``, whatever the default for the speed.
        * ``tones`` - ``mark`` and ``space`` in Hz for AFSK.
        * ``decoders`` - ``count`` decoders, ``offset`` Hz apart.
        * ``divide`` - sample rate division factor, 1 to 8.
        * ``upsample`` - upsample ratio for G3RUH, 1 to 4.
        * ``profiles`` - demodulator profile letters, such as ``A+``.
        * ``v26`` - ``a`` or ``b``, the V.26 alternative for 2400 bps.

    ``ptt``, ``dcd``, ``con``
        The output controls (``PTT``, ``DCD``, ``CON``).  ``method`` is one of:

        * ``serial`` - ``device``, ``line`` (``rts`` or ``dtr``), and
          optionally ``line2``.
        * ``gpio`` - ``pin``.
        * ``gpiod`` - ``device`` (the GPIO chip) and ``pin``.
        * ``lpt`` - ``pin``, the bit number.
        * ``rig`` - hamlib: ``model`` (a number, or ``auto``), ``device``, and
          optionally ``rate``.
        * ``cm108`` - optionally ``pin`` (3 if left out) and ``device``.

        ``invert`` drives the pin or line low to transmit, where Dire Wolf's
        format has a ``-`` in front of it; ``invert2`` does the same for
        ``line2``.

``agwPort``
    The port for the AGW TCPIP Socket Interface (``AGWPORT``), or 0 for none.

``kissPorts``
    KISS TCP ports (``KISSPORT``), each with ``port`` and optionally
    ``channel``, to give that port just one radio channel.  They are taken in
    order, like successive ``KISSPORT`` lines, so ``port: 0`` first drops the
    default port 8001.

``axudpPorts``
    AXUDP ports: AX.25 frames to and from other nodes (BPQ, XRouter, another
    Samoyed, ...) as UDP datagrams, each port a virtual channel of its own, as
    ``NCHANNEL`` makes one for a network TNC.  These have no equivalent in Dire
    Wolf's format.  Each has:

    ``port``
        The local UDP port, listened on and sent from.

    ``channel``
        The virtual channel, outside the radio channels' range, and not
        otherwise in use.

    ``maps``
        Where frames go, each with ``ax25addr``, the destination, and ``host``
        and ``port``, the node to send it to.  An address with no SSID matches
        any SSID of that callsign, unless another entry names the SSID itself.
        ``broadcast: true`` also sends the node frames for the broadcast
        addresses, like the ``B`` on a BPQ ``MAP`` line.

    ``broadcast``
        The destinations, such as NET/ROM's ``NODES``, whose frames go to every
        map with ``broadcast: true`` rather than to one node, like BPQ's
        ``BROADCAST`` lines.

    ``automap``
        ``true`` sends a frame for an address no map covers
        back to wherever a datagram from that address last came from,
        within the last hour,
        like BPQ's ``AUTOADDMAP`` -
        so long as that datagram came from the IP address of a host in ``maps``,
        from any port.
        A node reached through a mapped neighbour can then connect
        without a map of its own.
        Maps always win over what is learned,
        and a learned node gets no broadcasts.
        Off by default.

    Frames are sent with the RFC 1226 checksum appended; one on a received
    datagram is recognised and removed.  Datagrams are accepted from anyone,
    not only the nodes in ``maps``, so the port should not be open to the
    internet at large.

    .. code:: yaml

        samoyed:
          axudpPorts:
            - port: 10093
              channel: 10
              broadcast: [NODES]
              automap: true
              maps:
                - {ax25addr: Q1TEST-2, host: node.example.org, port: 10093, broadcast: true}
                - {ax25addr: Q2TEST, host: 192.0.2.2, port: 93}

``node``
    A node shell:
    a station that connects to the node's callsign or alias gets a command prompt.
    This has no equivalent in Dire Wolf's format.
    It has:

    ``address``
        The node's address: its callsign, with any SSID.

    ``alias``
        Optionally, a second name the node answers to,
        such as a short mnemonic for where it is.

    ``channels``
        The channels it accepts connections on.
        Left out, it is every channel connected mode can be used on:
        radio channels, network and serial TNCs, and AXUDP ports.

    ``infoMessage``
        What ``INFO`` says about the node.
        Use ``|`` for several lines.

    ``connectMessage``
        What a station is told when it connects,
        such as what the node is for and who runs it.
        Left out, it is a short welcome.
        Use ``|`` for several lines.

    ``adminToken``
        A secret that lets the web interface's admin API act on the node (see below).
        Left out, there is no admin API.

    ``chat``
        A chat server, started with the ``CHAT`` command (see below),
        with ``defaultRoom``, the room users start in (``General``),
        and ``rateLines`` and ``rateWindow``,
        how many lines a user may send in how long before the rest are turned away
        (10 in ``30s``; ``rateLines: 0`` for no limit).
        ``chat: {}`` gives one with the defaults.

    The node answers each line it is sent,
    heading the reply with its alias and callsign, as ``SAMNOD:Q1TEST-7}``.
    Its commands are:

    ``BYE`` (``B``, ``QUIT``, ``Q``)
        Disconnect.

    ``CONNECT`` (``C``) *port* *callsign*
        Connect onward, from the node's callsign, to the station on that channel.
        The channel can be left out when the node has only one.
        Typing a line before it connects gives up on it.
        Once connected, everything sent goes to and from the station as it is;
        when the station disconnects, the user is back at the node,
        and when the user disconnects, so does the station.

    ``HELP`` (``H``, ``?``)
        List the commands.

    ``INFO`` (``I``)
        Say what ``infoMessage`` says.

    ``PORTS`` (``P``)
        List the channels the node listens on, by number,
        and what sort of channel each is.

    ``MHEARD`` (``MH``)
        List the stations most recently heard over the radio,
        with the channel each was last heard on and how long ago.

    ``NODES`` (``N``) [*node*], ``ROUTES`` (``R``)
        With ``netrom``, list the nodes NET/ROM knows, or the routes to one of them,
        and the neighbouring nodes.
        Without it, say that the node does not do NET/ROM yet,
        rather than that the command is invalid.

    With ``netrom``, ``CONNECT`` (``C``) *node*, naming a node NET/ROM knows by callsign or alias,
    connects onward over NET/ROM instead,
    and a user can reach the node over NET/ROM as well as AX.25.

    With ``chat``, ``CHAT`` puts the user into a room of the node's chat server,
    and sends each line they type to everyone else in it.
    Rooms are the node's own, not linked to chat servers elsewhere.
    A line starting with ``/`` is a command:
    ``/join`` *room* moves to another room
    (its name letters, digits, ``-`` and ``_``, up to sixteen of them),
    ``/leave`` goes back to the room users start in,
    ``/who`` [*room*] lists who is in a room, by default the user's own,
    ``/rooms`` lists the rooms with anyone in them,
    ``/msg`` *user* *text* sends a line to one user, wherever they are,
    and ``/quit`` goes back to the node.

    A line of more than 256 characters is ignored.

    With the web interface on (``WEBPORT``, in ``legacy``),
    its Node tab shows the node's users, the stations it has heard,
    and with ``netrom`` its nodes, neighbours and circuits.
    The same views are JSON under ``/api/node/``:
    ``info``, ``sessions``, ``heard``, ``nodes``, ``neighbours``, ``circuits``,
    and ``chat``, the chat rooms with anyone in them, and who.

    With ``adminToken`` set,
    requests carrying it as ``Authorization: Bearer`` *token* can act on the node,
    each answering 204 when it worked:

    ``POST /api/admin/links/close``
        Close one of the node's AX.25 links,
        given as JSON ``{"port": 0, "local": "Q1TEST-7", "remote": "Q2TEST"}``.

    ``POST /api/admin/nodes/broadcast``
        Send NODES broadcasts now, rather than waiting for ``broadcastInterval``.

    ``POST /api/admin/circuits/``\ *id*\ ``/close``
        Close a NET/ROM circuit, by the ``id`` that ``circuits`` gives it.

    ``POST /api/admin/neighbours``
        Lock a NET/ROM neighbour at a fixed quality, as ``neighbours`` in ``netrom`` does,
        given as JSON ``{"port": 0, "call": "Q2TEST", "alias": "TWO", "quality": 200}``.

    ``DELETE /api/admin/neighbours/``\ *port*\ ``/``\ *call*
        Unlock a neighbour, so its routes are learned and aged again.

    ``POST /api/admin/chat/announce``
        Send everyone in chat a line, given as JSON ``{"text": "Back in five minutes"}``.

    The web interface has no TLS of its own,
    so put it behind a proxy that has before setting ``adminToken`` on a port others can reach.

    .. code:: yaml

        samoyed:
          node:
            address: Q1TEST-7
            alias: SAMNOD
            channels: [0, 10]
            infoMessage: |
              Samoyed node at the club station.
            connectMessage: |
              Welcome to the club node.
              Type ? for a list of commands.
            chat: {}

``netrom``
    NET/ROM for the node (see :doc:`glossary/netrom`), which needs ``node``:
    NET/ROM goes by the node's ``address`` and ``alias``.
    This has no equivalent in Dire Wolf's format.
    It has:

    ``ports``
        The channels NET/ROM runs on, from among the node's,
        each with ``channel``,
        ``quality`` (given to neighbours heard there, 0 to 255, 192 if left out)
        and ``broadcast`` (whether NODES broadcasts are sent there, true if left out).

    ``neighbours``
        Neighbours configured at a fixed quality rather than learned from their broadcasts,
        each with ``channel``, ``call``, ``alias`` and ``quality``.
        Their routes never age out.

    ``minQuality``, ``obsolescence``, ``minObsolescenceBroadcast``, ``broadcastInterval``
        How routes are judged and aged:
        the least quality worth keeping (50),
        what a refreshed route's obsolescence count starts at (6),
        the least count a route needs to be passed on in the node's own broadcasts (5),
        and how often those go out (``30m``).

    ``ttl``, ``window``, ``timeout``, ``retries``, ``ackDelay``, ``busyDelay``, ``idleTimeout``
        The network and transport layers:
        the hops a packet may make (16),
        the transport window offered (4),
        how long to wait for an acknowledgement before sending again (``120s``)
        and how many times (3),
        how long to hold an acknowledgement back in case data can carry it (``3s``),
        how long to wait when the far end says it is busy (``180s``),
        and how long a circuit may sit idle before it is closed (``15m``, ``0s`` for never).

    Durations are written as Go durations, such as ``90s`` or ``1h``.

    .. code:: yaml

        samoyed:
          node:
            address: Q1TEST-7
            alias: SAMNOD
          netrom:
            ports:
              - channel: 0
                quality: 192
              - channel: 10
                quality: 220
            neighbours:
              - {channel: 10, call: Q2TEST-2, alias: TWO, quality: 220}

``mqtt``
    Publish what the node does to an MQTT broker, as JSON, which needs ``node``.
    This has no equivalent in Dire Wolf's format.
    It has:

    ``broker``
        The broker's URL, such as ``tcp://localhost:1883``, or ``ssl://`` for TLS.

    ``username``, ``password``
        Optionally, what to log in to the broker with.

    ``clientId``
        The client ID to connect with, which is ``samoyed-`` and the node's address if left out.

    ``topicPrefix``
        The first level of every topic (``samoyed``).

    Each event goes to a topic under the prefix and the node's address,
    such as ``samoyed/Q1TEST-7/link/up``:
    ``link/up`` and ``link/down`` for a user's session, a downlink or a NET/ROM neighbour's link,
    ``circuit/up`` and ``circuit/down`` for a NET/ROM circuit,
    ``routes/changed`` when NET/ROM's routing table changes,
    ``heard`` for a station heard on a NET/ROM channel for the first time in a while,
    ``chat/join`` and ``chat/leave`` for a ``user`` joining or leaving a chat ``room``,
    and ``bbs/message`` for a new BBS message,
    with its ``number``, ``type``, ``from``, ``to`` and ``at``.
    ``status`` is retained, and is ``online`` while Samoyed is connected,
    and ``offline`` once it stops or, through the broker's last will, drops off.
    Events are published at most once,
    and those that arrive while the broker cannot be reached are dropped.

    .. code:: yaml

        samoyed:
          mqtt:
            broker: tcp://localhost:1883
            topicPrefix: club

``dataDir``
    Where what is kept from one run to the next lives:
    the BBS keeps its messages in ``bbs`` under it.
    This has no equivalent in Dire Wolf's format.

``bbs``
    A BBS for the node, which needs ``node`` and ``dataDir``:
    personal mail and bulletins,
    read and written by users,
    and forwarded to and from partner BBSes with :doc:`glossary/fbb`.
    This has no equivalent in Dire Wolf's format.
    It has:

    ``call``
        The BBS's own callsign, such as the node's with another SSID,
        which stations and partners can connect to directly,
        over AX.25 on any of the node's channels,
        or with ``netrom``, over NET/ROM.
        Left out, the BBS is reached only from the node's shell,
        and goes by the node's address.

    ``alias``, ``quality``
        With ``netrom``, the alias and quality (200 if left out) the node advertises for ``call``
        in its NODES broadcasts.

    ``hroute``
        Where the BBS is, hierarchically, such as ``#HANTS.GBR.EURO``,
        for the ``R:`` lines it adds.

    ``partners``
        The BBSes it forwards with, each with:
        ``call``;
        ``node``, the NET/ROM node to connect to, which needs ``netrom``,
        or else ``channel``, one of the node's, and ``via`` to connect to ``call`` over AX.25;
        ``script``, lines to send once connected, to get from a node's shell to its BBS;
        ``routes``, which mail goes this way
        (a pattern matches an @ field with it as one of its parts, and ``*`` matches all);
        ``bulletins``, whether bulletins go this way;
        and ``interval``, how often to connect whether or not there is mail to send
        (at least a minute, or left out for only when there is).

    A user reaches the BBS with the ``BBS`` command in the node's shell,
    and goes back to the node with ``B``.
    The BBS starts each session with its SID, ``[SAMOYED-``\ *version*\ ``-B12FHM$]``,
    then a greeting and its prompt, ``de`` *call*\ ``>``.
    Its commands are:

    ``L`` [*n*]
        List the latest messages, or those from number *n* on.

    ``LM``, ``LB``
        List the user's personal mail, or the bulletins.

    ``R`` *n* ...
        Read messages.
        Personal mail can be read by its sender and its addressee,
        and bulletins by anyone.

    ``S`` *call*\ [``@``\ *bbs*]
        Send a personal message.
        The BBS asks for a subject, then the text,
        which ends with ``/EX`` (or Ctrl-Z) on a line of its own;
        ``/ABORT`` gives up on it.

    ``SB`` *to*\ ``@``\ *distribution*
        Send a bulletin, such as ``SB ALL@GBR``.

    ``K`` *n* ...
        Kill messages the user sent, or personal mail sent to them.

    ``I``
        About the BBS.

    ``B``
        Leave.

    A partner that connects and answers the BBS's SID with its own forwards there and then;
    only the partners configured may.
    The BBS connects to each partner in turn:
    soon after mail for it arrives,
    and every ``interval`` if one is set,
    to collect any mail the partner has.
    The two settle on the best protocol they share:
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

    With the node's ``adminToken``, the admin API also has:

    ``GET /api/admin/bbs/messages``
        Every message but its text.

    ``DELETE /api/admin/bbs/messages/``\ *number*
        Kill a message.

    ``GET /api/admin/bbs/partners``
        How forwarding with each partner is going.

    ``POST /api/admin/bbs/partners/``\ *call*\ ``/forward``
        Connect to a partner now.

    .. code:: yaml

        samoyed:
          dataDir: /var/lib/samoyed
          bbs:
            call: Q1TEST-1
            alias: ONEBBS
            hroute: "#HANTS.GBR.EURO"
            partners:
              - call: Q2TEST-1
                node: TWOBBS
                routes: ["*"]
                bulletins: true
                interval: 1h

``legacy``
    Directives in Dire Wolf's format, one per line, read after everything else as
    though they were a file of their own - so a channel setting there needs its
    own ``CHANNEL`` line first.  Use ``|`` to keep the lines apart, as in the
    example.

Things to look out for
----------------------

* A misspelt or unknown key is an error, not ignored, and a file with an error
  like that is not used at all.
* A file holds one YAML document; a second one (after ``---``) is an error.
* Values containing a comma, such as ALSA device names like ``plughw:1,0``,
  must be quoted inside ``{...}``, where a comma separates entries.
* Complaints give the line in the YAML file, including for the ``legacy``
  block.
