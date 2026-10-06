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

    ``il2prxcrc``
        ``false`` to receive IL2P frames without a trailing CRC
        (``IL2PRXCRC``).  Left out, it is ``true``: frames must carry one, and
        it must match.

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

    Frames are sent with the RFC 1226 checksum appended; one on a received
    datagram is recognised and removed.  Datagrams are accepted from anyone,
    not only the nodes in ``maps``, so the port should not be open to the
    internet at large.

    .. code:: yaml

        axudpPorts:
          - port: 10093
            channel: 10
            broadcast: [NODES]
            maps:
              - {ax25addr: Q1TEST-2, host: node.example.org, port: 10093, broadcast: true}
              - {ax25addr: Q2TEST, host: 192.0.2.2, port: 93}

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
