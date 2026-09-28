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

    channels:
      - channel: 0
        mycall: Q1TEST-1
        txdelay: 30
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

    legacy: |
      AGWPORT 8000
      KISSPORT 8001
      DIGIPEAT 0 0 ^WIDE[3-7]-[1-7]$|^TEST$ ^WIDE[12]-[12]$

That is the same configuration as:

.. code::

    ADEVICE plughw:1,0 plughw:2,0
    ACHANNELS 2

    CHANNEL 0
    MYCALL Q1TEST-1
    TXDELAY 30
    MODEM 1200 1200:2200 3@30
    PTT /dev/ttyUSB0 RTS -DTR

    CHANNEL 1
    MYCALL Q1TEST-2
    MODEM 9600 G3RUH
    PTT GPIO -25

    AGWPORT 8000
    KISSPORT 8001
    DIGIPEAT 0 0 ^WIDE[3-7]-[1-7]$|^TEST$ ^WIDE[12]-[12]$

Both are checked by the same code, so a setting means the same, and draws the
same complaints, whichever way it is written.

Reference
---------

``audioDevices``
    A list of audio devices (``ADEVICE``, ``ACHANNELS``).  Each has ``input``,
    an optional ``output`` when transmitting goes somewhere else, and
    ``channels``, 1 for mono (the default) or 2 for stereo.  A device's number
    is its position in the list, unless it gives one with ``device``.

``channels``
    A list of radio channels.  Each needs ``channel``, its number, and may have:

    ``mycall``
        The station callsign (``MYCALL``).  As with ``MYCALL``, it is also used
        for any channel that does not set its own.

    ``txdelay``
        The transmit delay, in 10 ms units (``TXDELAY``).

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
