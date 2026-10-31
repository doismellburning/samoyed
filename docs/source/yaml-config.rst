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

    An ``input`` of ``rtl_tcp:host:port`` receives from an RTL-SDR served by
    ``rtl_tcp``, which needs an ``sdr`` block, with no line-at-a-time equivalent:

    ``frequencies``
        A frequency in MHz for each of the device's channels, one or two of
        them, which sets ``channels``.

    ``gain``
        The tuner gain in dB.  Left out, the tuner sets its own.

    ``ppm``
        The correction for the dongle's oscillator, in parts per million.

    See :doc:`how-to-guides` for an example.

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

    ``il2prxcrc``
        ``false`` to receive IL2P frames without a trailing CRC (``IL2PRXCRC``).
        Left out, it is ``true``: frames must carry one, and it must match.

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

``networkTNCs``
    Network TNCs (``NCHANNEL``):
    KISS TNCs reached over TCP,
    each a virtual channel of its own.
    Each has:

    ``channel``
        The virtual channel, outside the radio channels' range, and not
        otherwise in use.

    ``host``
        The TNC's hostname or IP address.

    ``port``
        The TNC's KISS TCP port.

    .. code:: yaml

        samoyed:
          networkTNCs:
            - {channel: 6, host: 192.0.2.1, port: 8001}

``serialTNCs``
    Serial TNCs (``SCHANNEL``):
    KISS TNCs on a serial port,
    each a virtual channel of its own.
    Each has:

    ``channel``
        The virtual channel, outside the radio channels' range, and not
        otherwise in use.

    ``device``
        The serial port the TNC is on.

    ``baud``
        The serial port's speed:
        1200, 2400, 4800, 9600, 19200, 38400, 57600 or 115200.
        Left out, it is 9600.

    .. code:: yaml

        samoyed:
          serialTNCs:
            - {channel: 10, device: /dev/ttyACM0, baud: 57600}

``axudpPorts``
    AXUDP ports: AX.25 frames to and from other nodes (BPQ, XRouter, another
    Samoyed, ...) as UDP datagrams,
    each port a virtual channel of its own, like a network TNC.
    These have no equivalent in Dire Wolf's format.
    Each has:

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
        With no map marked ``broadcast: true``, they go nowhere,
        which the configuration is warned about when it is read.

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

    ``map``
        Report the node to the `UK Packet Radio network map <https://nodes.ukpacketradio.network/>`_,
        once at startup, again ten minutes later, once it has had a chance to hear its neighbours,
        and every hour after.
        At startup, the log says where the node can be seen:
        the public map's page of nodes,
        or, with ``apiUrl`` given, that map's API.
        Left out, the node is not reported.
        ``map: {}`` reports it with nothing more than its callsign, its alias, that it runs Samoyed,
        and its ports: one for each of its channels, numbered as the channel is.
        A radio channel is reported as RF, with its modem's modulation, baud and bit rate,
        and whether it sends AX.25, FX.25 or IL2P.
        A network or serial TNC is reported as RF, and nothing more,
        since its radio is out of sight;
        an AXUDP port is reported as Internet.
        With a NET/ROM node on one of those ports,
        each report also lists the neighbours it has a link up to at the time,
        which the map draws as links between the nodes.
        Each report also lists up to 100 of the stations most recently heard directly on those ports,
        with how many times each has been heard, when it was first and last heard,
        and where it last said it was, if it has.
        A station heard only through a digipeater is left out,
        since Samoyed does not keep which digipeaters it came through,
        and the map would take it to have been heard directly.
        The settings below say more.
        It has:

        ``apiUrl``
            Optionally, the base URL of the map's API,
            for a map other than the public one.

        ``locator``
            Optionally, the Maidenhead locator of where the node is,
            such as ``IO91wm``,
            which the map plots it at.
            Left out, the map is not told where the node is.

        ``asl``
            Optionally, the height of the node's antenna above sea level, in metres,
            from -500 to 9000.
            It needs ``locator``.

        ``contact``
            Optionally, how to reach whoever runs the node,
            which the map shows to anyone.

        ``comment``
            Optionally, a short note about the node.

        ``ports``
            Optionally, what the map is told about some of the node's ports,
            keyed by channel number,
            where that is not what the channel's configuration suggests,
            or is more than it can.
            Each setting given replaces the one suggested,
            and the rest are left as they are.
            Each channel must be one the node listens on.
            Each has:

            ``report``
                ``false`` leaves the port off the map.

            ``linkType``
                ``RF``, ``PrivateNet``, ``Internet`` or ``Other``,
                such as ``PrivateNet`` for an AXUDP port over a private network.

            ``freq``
                The port's frequency, in Hz, such as ``144950000``,
                which the map works out the band from.
                Without one, the map shows an RF port as having no known frequency.

            ``mode``
                The layer 2 protocol, such as ``AX.25`` or ``IL2P``.

            ``modulation``
                Such as ``AFSK``, ``FSK`` or ``QPSK``.

            ``baud``
                Symbols per second.

            ``bitrate``
                Bits per second.

            ``usage``
                What the port is for, such as ``Access`` or ``Backbone``.

            ``comment``
                A short note about the port.

        ``aliases``
            Optionally, the services the node offers besides itself,
            such as a BBS or chat server that another program runs over AGW or KISS,
            keyed by the alias each is known by:
            up to six letters, digits or ``#_&-/``, such as ``SAMBBS``.
            Samoyed cannot see what such a program offers,
            so the map is told only what is listed here.
            Each has:

            ``service``
                What it is, such as ``BBS`` or ``CHAT``.

            ``callsign``
                The address it answers to, with any SSID, such as ``Q1TEST-3``.

    The node answers each line it is sent,
    heading the reply with its alias and callsign, as ``SAMNOD:Q1TEST-7}``.
    Its commands are:

    ``BYE`` (``B``, ``QUIT``, ``Q``)
        Disconnect.

    ``CONNECT`` (``C``) *port* *callsign*, or ``CONNECT`` *node*
        Connect onward, from the node's callsign, to the station on that channel.
        The channel can be left out when the node has only one.
        With no channel, and a node that ``NODES`` lists, by alias or callsign,
        connect to that node over a NET/ROM circuit instead,
        however many nodes lie between,
        as BPQ's ``C`` *node* does.
        Naming a channel is always a station on it:
        ``C 6 NPRNOD`` asks for an AX.25 link to ``NPRNOD`` itself,
        which over AXUDP needs a map for ``NPRNOD``,
        where ``C NPRNOD`` reaches it through whichever neighbour NET/ROM routes by.
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

    ``NODES`` (``N``) *node*
        List the nodes NET/ROM can reach, as ``ALIAS:CALLSIGN``;
        with a node's alias or callsign, list the routes to it,
        best first, with the port of the neighbour each goes through,
        its quality and its obsolescence count.

    ``ROUTES`` (``R``)
        List the neighbouring nodes, each with its port,
        whether the link to each is up,
        the quality it gives that link,
        and how many nodes are reached through it.

    Without a top-level ``netrom`` block,
    ``NODES`` and ``ROUTES`` say that the node does not do NET/ROM,
    rather than that the command is invalid.

    A line of more than 256 characters is ignored.

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
            map:
              locator: IO91wm
              comment: Club node
              ports:
                0:
                  freq: 144950000
                  usage: Access
                  comment: Hilltop, vertical

``netrom``
    A NET/ROM node (see :doc:`glossary/netrom`),
    which learns routes from the nodes it hears and carries circuits between them.
    With a ``node`` block,
    it runs under the node shell's address and alias,
    so that users and neighbouring nodes connect to the one node;
    the node then needs an alias of up to six characters, any SSID included,
    which is what other nodes know it by.
    ``netrom: {}`` runs it with the defaults.
    The ``NETROM`` directive of the line-at-a-time format is not available:
    a NET/ROM node is configured here only.
    It has:

    ``callsign``
        The node's AX.25 address,
        conventionally the station's callsign with an SSID of its own.
        Needed without a ``node`` block, and refused with one.

    ``alias``
        The short name, up to six letters, digits or ``#_&-/``,
        other nodes know it by.
        Needed without a ``node`` block, and refused with one.

    ``channels``
        Optionally, the channels it runs on,
        a map from each channel to its settings,
        each any channel connected mode can be used on,
        and with a ``node`` block, one the node listens on.
        It keeps one routing table across them all,
        sends its ``NODES`` broadcasts on each,
        and takes a neighbour to be a callsign on one of them,
        so it can route between, say, a radio channel and an AXUDP link.
        Left out, it runs on all the node's channels,
        or without a ``node`` block,
        on every channel connected mode can be used on,
        with the settings below.
        A channel's settings, used there in place of the ones below,
        can be left out, as can the whole of them.
        Each may have:

        ``quality``
            From 0 to 255,
            the quality it gives the link to a neighbour heard on that channel.
            A route's quality is its link's times what the neighbour advertised,
            so a destination reachable over two channels is reached over the better one:
            a fast AXUDP link, say, rather than a 1200 baud radio channel.

    ``ttl``
        Optionally, how many nodes a frame it originates may cross.
        16 if left out.

    ``nodesInterval``
        Optionally, the seconds between its ``NODES`` broadcasts,
        up to a week (604800).
        3600 if left out.

    ``quality``
        Optionally, from 0 to 255,
        the quality it gives the link to any neighbour it hears
        on a channel that does not give its own.
        192 if left out.

    ``minQuality``
        Optionally, the least quality a route is worth keeping at.
        10 if left out.

    .. code:: yaml

        samoyed:
          netrom:
            callsign: Q1TEST-7
            alias: QNODEA
            channels:
              0:
              2: {quality: 255}

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
