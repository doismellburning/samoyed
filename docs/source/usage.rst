..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

Usage
=====

.. ai-label:: generated

A packet station has three layers:
a modem that turns frames into audio and back,
a link layer (AX.25 connected mode, and NET/ROM above it) that makes those frames into conversations,
and the applications and users at the top.
Samoyed can do all three,
or fit in alongside another program that does some of them.

Samoyed as a complete packet node
---------------------------------

.. graphviz::

    digraph full {
        rankdir=TB;
        node [shape=box, style="rounded,filled", fillcolor="#e8f0fe", fontname="sans-serif"];
        edge [fontname="sans-serif", fontsize=10];
        subgraph cluster_samoyed {
            label="samoyed-direwolf"; fontname="sans-serif"; style=rounded;
            shell [label="Node shell"];
            netrom [label="NET/ROM"];
            ax25 [label="AX.25 link layer"];
            modem [label="Modem"];
            shell -> netrom; shell -> ax25; netrom -> ax25; ax25 -> modem;
        }
        apps [label="Applications\n(AGW, KISS)", fillcolor="#f1f3f4"];
        radio [label="Sound card and radio", fillcolor="#f1f3f4"];
        apps -> ax25; modem -> radio;
    }

One ``samoyed-direwolf`` is the modem, the link layer,
a node shell that users connect to,
and a NET/ROM node that links it to other nodes.
The node shell and NET/ROM are configured in YAML only:

.. code:: yaml

    samoyed:
      audioDevices:
        - input: "plughw:1,0"
      channels:
        - channel: 0
          mycall: Q1TEST
          modem: {speed: "1200"}
          ptt: {method: serial, device: /dev/ttyUSB0, line: rts}
      node:
        address: Q1TEST-7
        alias: QNODE
        infoMessage: |
          Q1TEST's node, 1200 bps on 144.950 MHz.
      netrom: {}
      agwPort: 8000
      kissPorts:
        - port: 8001
      legacy: |
        WEBPORT 8080
        METRICSPORT 9100

A station connects to ``Q1TEST-7`` or ``QNODE`` and gets the node's prompt,
where ``HELP`` lists the commands:
``CONNECT`` onward to another station,
``NODES`` and ``ROUTES`` for what NET/ROM can reach,
``MHEARD``, ``PORTS``, ``INFO`` and ``BYE``.
NET/ROM shares the node's callsign and alias,
sends ``NODES`` broadcasts,
and learns routes from those it hears;
another node reaches it by connecting to the same callsign.
Applications can still use the AGW and KISS ports alongside the node,
and the APRS side of Samoyed - digipeating, beacons, IGate - works as it does without one.

More on each part:

* :doc:`yaml-config` - every ``node`` and ``netrom`` setting,
  including reporting the node to the UK packet network map
* :ref:`how-to-guides:Run a NET/ROM node` - NET/ROM on its own, with the node shell, and over AXUDP
* :ref:`how-to-guides:Accept incoming connections (connected mode)` - connected mode for an application of your own
* :ref:`how-to-guides:Watch a station in a web browser` and
  :ref:`how-to-guides:Monitor a station with Prometheus / Grafana` - ``WEBPORT`` and ``METRICSPORT``

Samoyed with an external modem
------------------------------

.. graphviz::

    digraph extmodem {
        rankdir=TB;
        node [shape=box, style="rounded,filled", fillcolor="#e8f0fe", fontname="sans-serif"];
        edge [fontname="sans-serif", fontsize=10];
        subgraph cluster_samoyed {
            label="samoyed-direwolf"; fontname="sans-serif"; style=rounded;
            upper [label="Node shell, NET/ROM,\nAX.25, APRS"];
        }
        apps [label="Applications\n(AGW, KISS)", fillcolor="#f1f3f4"];
        tnc [label="KISS TNC\n(NinoTNC, pdn-soundmodem)", fillcolor="#f1f3f4"];
        radio [label="Radio", fillcolor="#f1f3f4"];
        apps -> upper;
        upper -> tnc [label=" KISS over serial (SCHANNEL)\n or TCP (NCHANNEL)"];
        tnc -> radio;
    }

Samoyed can use a KISS TNC as one of its channels,
in place of its own modem,
and run everything above it on that channel:
APRS digipeating, beacons and IGate,
connected mode for applications,
the node shell and NET/ROM.
``SCHANNEL`` reaches a TNC on a serial port,
``NCHANNEL`` one over TCP:

.. code::

    SCHANNEL chan device [baud] [SETTING=value ...]
    NCHANNEL chan host port [SETTING=value ...]

``chan`` has to be a virtual channel, 6 to 15,
since 0 to 5 are radio channels.
``baud`` is one of 1200, 2400, 4800, 9600, 19200, 38400, 57600 or 115200,
and 9600 if left out.

Samoyed sends the TNC its frames as KISS data on KISS port 0,
and doesn't wait for the channel to be clear first:
getting on the air is left to the TNC.
The settings - ``TXDELAY``, ``PERSIST``, ``SLOTTIME`` and ``TXTAIL``,
each 0 to 255 in the units of its KISS command -
go to the TNC as KISS commands when Samoyed attaches to it and each time it reattaches;
one left out isn't sent, and the TNC keeps its own value.
A KISS client's settings for the channel are passed on to the TNC too.
Samoyed sends no mode or other hardware commands.
``CDIGIPEAT``, ``CFILTER`` and ``REGEN`` work on radio channels only.
:ref:`how-to-guides:Use a network KISS TNC as a channel` has the rest of the detail,
which holds for ``SCHANNEL`` too,
except that a serial TNC that goes away is retried every 30 seconds rather than 5.

A station whose only channels are TNCs needs no sound card,
and says so with ``ADEVICE none``
(see :ref:`how-to-guides:Run with no audio device`):

.. code::

    ADEVICE none
    MYCALL Q1TEST
    SCHANNEL 10 /dev/ttyACM0 57600
    NCHANNEL 11 localhost 8105

In YAML, they are ``serialTNCs`` and ``networkTNCs``,
with the settings in lower case,
and leaving out ``audioDevices`` is enough to have no sound card:

.. code:: yaml

    samoyed:
      serialTNCs:
        - {channel: 10, device: /dev/ttyACM0, baud: 57600}
      networkTNCs:
        - {channel: 11, host: localhost, port: 8105}
      node:
        address: Q1TEST-7
        alias: QNODE
        channels: [10, 11]
      netrom: {}

Every channel without a ``MYCALL`` of its own takes the first one,
and a TNC channel can't be given one;
with no radio channel to carry it, a YAML configuration gives it in the ``legacy`` block.
The node and NET/ROM need none, using the node's ``address``.
``DIGIPEAT``, ``FILTER`` and a beacon's ``SENDTO`` naming the channel
have to come after the ``SCHANNEL`` or ``NCHANNEL`` line.

NinoTNC
~~~~~~~

The NinoTNC is a hardware modem and TNC on USB,
which shows up on Linux as a serial port such as ``/dev/ttyACM0``,
and talks KISS at 57600 baud:

.. code::

    SCHANNEL 10 /dev/ttyACM0 57600

A udev rule can give each NinoTNC a name in ``/dev`` that stays the same
whichever order they are plugged in.
The NinoTNC takes some of its settings from the host and some from the board:

* The ``MODE`` DIP switches choose the modem.
  Set to 1111, they select the mode last set by a ``SETHW`` KISS command instead,
  which Samoyed doesn't send.
* The ``TXDELAY`` potentiometer sets the transmit delay.
  At its minimum, the NinoTNC uses the host's ``TXDELAY`` instead.
* ``SLOTTIME`` and ``PERSIST`` come from the host,
  and default to 50 and 225 without it;
  its documentation suggests a ``PERSIST`` of at least 225.

This is from the NinoTNC's documentation,
and hasn't been tried with Samoyed on a NinoTNC.

pdn-soundmodem
~~~~~~~~~~~~~~

pdn-soundmodem is a sound-card modem for Linux,
covering every NinoTNC mode among others,
that offers KISS over TCP,
on ``127.0.0.1`` port 8105 unless its ``kissPort`` and ``bind`` settings say otherwise:

.. code::

    NCHANNEL 11 localhost 8105

That port carries every modem pdn-soundmodem runs, each on its own KISS sub-channel.
Samoyed sends on KISS port 0, and takes whatever comes back as the channel's,
so either run one modem there, on ``subChannel`` 0,
or give the one Samoyed should use a ``port`` of its own,
where it is presented as sub-channel 0.
pdn-soundmodem has no settings of its own for channel access:
it expects the host to set ``TXDELAY``, persistence, slot time and ``TXTAIL`` over KISS,
so give them all:

.. code::

    NCHANNEL 11 localhost 8105 TXDELAY=30 PERSIST=63 SLOTTIME=10 TXTAIL=5

This is from pdn-soundmodem's documentation,
and hasn't been tried with Samoyed.

References
~~~~~~~~~~

* `NinoTNC N9600A operation <https://tarpn.net/t/nino-tnc/n9600a/n9600a_operation.html>`__ -
  57600 baud; the ``MODE`` switches and ``SETHW``; the ``TXDELAY`` potentiometer; ``SLOTTIME`` and ``PPERSIST`` from the host, and their defaults
* `OARC wiki: NinoTNC <https://wiki.oarc.uk/packet:ninotnc>`__ -
  the device name, and udev rules for a stable one
* `pdn-soundmodem <https://github.com/packet-net/pdn-soundmodem>`__ and its
  `configuration reference <https://github.com/packet-net/pdn-soundmodem/blob/main/docs/reference/config.md>`__ -
  ``kissPort``, ``bind``, ``subChannel``, per-modem ``port``, and channel access from the host
