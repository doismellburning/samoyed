..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

Quick Start
===========

.. ai-label:: generated

This gets one radio on 1200 bps packet,
with ports open for other applications,
starting from Samoyed :doc:`installed <installation>`.

A configuration file
--------------------

``samoyed-direwolf`` is the modem and TNC.
It reads a configuration file:
``direwolf.conf`` in the current directory,
or whatever ``--config-file`` names.
A minimal one for a single 1200 bps channel on Linux,
in Samoyed's :doc:`YAML format <yaml-config>`
(for a file whose name ends in ``.yaml`` or ``.yml``):

.. code:: yaml

    samoyed:
      audioDevices:
        # Your soundcard - see `arecord -l`
        - input: plughw:1,0
      channels:
        - channel: 0
          # Your callsign
          mycall: Q1TEST
          modem:
            speed: "1200"
      agwPort: 8000
      kissPorts:
        - port: 8001

The same in Dire Wolf's line-at-a-time format, which works too:

.. code::

    # Your soundcard - see `arecord -l`
    ADEVICE plughw:1,0
    CHANNEL 0
    # Your callsign
    MYCALL Q1TEST
    MODEM 1200
    AGWPORT 8000
    KISSPORT 8001

:doc:`how-to-guides` has more on choosing the sound card,
and on PTT, which this leaves out:
without it, Samoyed receives but can't key a transmitter.

Check it, then start it
-----------------------

.. code::

    $ samoyed-direwolf --config-file samoyed.yaml --config-check
    $ samoyed-direwolf --config-file samoyed.yaml

``--config-check`` reports every problem in the file and stops
(see :ref:`how-to-guides:Check a config file without starting up`).
``samoyed-direwolf --help`` lists the command-line options.

When it hears a packet, it logs it, then prints it in the usual monitoring format:

.. code::

    INFO[2026-10-31T10:18:15.602Z] Heard                    audio_level="50(14/14)" heard=Q2TEST retries= spectrum="_||||||__"
    INFO[2026-10-31T10:18:15.602Z] Packet ax25_safe_print below:  addrs="Q2TEST>APDW17:" channel=0 slice=3
    >Hello from Q2TEST
    Status Report, UNKNOWN vendor/model
    Hello from Q2TEST

Adjust the receive volume so that the audio level is somewhere around 50.

Connect an application
----------------------

Packet applications talk to Samoyed through the :doc:`glossary/kiss` port (8001 here)
or the :doc:`glossary/agwpe` port (8000).
``samoyed-kissutil`` is a KISS client that shows what is heard,
and sends what you type in the same monitoring format:

.. code::

    $ samoyed-kissutil
    [0] Q2TEST>APDW17:>Hello from Q2TEST

The ``[0]`` is the channel it was heard on.

Run it as a service
-------------------

The ``.deb`` doesn't install a service,
so on a systemd system, put the configuration in ``/etc/samoyed/samoyed.yaml``
and this in ``/etc/systemd/system/samoyed.service``:

.. code:: ini

    [Unit]
    Description=Samoyed packet radio modem and TNC
    After=sound.target network-online.target
    Wants=network-online.target

    [Service]
    DynamicUser=yes
    SupplementaryGroups=audio dialout
    ExecStartPre=/usr/bin/samoyed-direwolf --config-file /etc/samoyed/samoyed.yaml --config-check
    ExecStart=/usr/bin/samoyed-direwolf --config-file /etc/samoyed/samoyed.yaml
    Restart=on-failure

    [Install]
    WantedBy=multi-user.target

then start it, and have it start at boot:

.. code::

    $ sudo systemctl daemon-reload
    $ sudo systemctl enable --now samoyed
    $ journalctl -u samoyed -f

``DynamicUser`` runs it as a throwaway user rather than root,
and ``SupplementaryGroups`` gives that user the sound card and serial ports
(see :ref:`installation:Permissions on Linux`).
``systemctl stop`` unkeys the transmitter on the way out
(see :ref:`how-to-guides:Stop it without leaving the rig keyed`).
