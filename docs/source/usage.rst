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
