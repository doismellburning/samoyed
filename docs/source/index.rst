Samoyed
=======

.. ai-label:: basic
   :text: Parts of this site were written with

`Samoyed <https://github.com/doismellburning/samoyed>`__ is a soundcard modem, software TNC and NET/ROM node for amateur packet radio, written in Go.
It speaks AX.25 v2.2, FX.25, IL2P and APRS over 300-9600 bps AFSK, PSK and G3RUH modems,
and can act as a digipeater and an IGate,
with KISS and AGW interfaces for other applications,
a web dashboard and Prometheus metrics.

Samoyed started as a port of `Dire Wolf <https://github.com/wb2osz/direwolf>`__ and aims to stay compatible with it,
so an existing ``direwolf.conf`` should work as it is.

.. image:: https://github.com/doismellburning/samoyed/actions/workflows/build-and-test.yml/badge.svg?branch=main
   :alt: Build and test
   :target: https://github.com/doismellburning/samoyed/actions/workflows/build-and-test.yml?query=branch%3Amain

.. image:: https://img.shields.io/github/v/release/doismellburning/samoyed?include_prereleases&sort=semver
   :alt: Latest release
   :target: https://github.com/doismellburning/samoyed/releases

.. image:: https://img.shields.io/badge/licence-AGPL--3.0--or--later-blue
   :alt: Licence: AGPL-3.0-or-later
   :target: https://github.com/doismellburning/samoyed#license

.. note::

   Samoyed is pre-release.
   The port is functionally complete,
   but the automated tests run against audio files and loopback rather than real radios.
   Reports from on-air use are especially welcome - working or not - via
   `GitHub issues <https://github.com/doismellburning/samoyed/issues>`__.

See it work
-----------

No radio or soundcard needed:
``samoyed-gen_packets`` turns a packet into 1200 bps AFSK audio,
and ``samoyed-atest`` demodulates it again,
as ``samoyed-direwolf`` would from a receiver.

.. code::

    $ echo 'Q1TEST>APDW17:>Hello from Samoyed' > hello.txt
    $ samoyed-gen_packets -o hello.wav hello.txt
    $ samoyed-atest hello.wav
    ...
    DECODED[1] 0:00.507 Q1TEST audio level = 50(14/14)
    [0] Q1TEST>APDW17:>Hello from Samoyed


    1 from hello.wav
    1 packets decoded in 0.016 seconds.  32.1 x realtime

Features
--------

* Modems: 300 and 1200 bps :doc:`glossary/afsk`, 300-4800 bps :doc:`glossary/psk`, 9600 bps :doc:`glossary/g3ruh`,
  and receive from an RTL-SDR with no soundcard
* :doc:`glossary/ax25` v2.2, including connected mode,
  with :doc:`glossary/fx25` and :doc:`glossary/il2p` forward error correction
* :doc:`glossary/aprs` encoding and decoding, :doc:`glossary/digipeater` and :doc:`glossary/igate`
* :doc:`glossary/netrom` node
* :doc:`glossary/kiss` (TCP, serial, Bluetooth) and :doc:`glossary/agwpe` interfaces for other applications
* :doc:`glossary/aprstt` gateway, GPS tracker beacons with SmartBeaconing
* :doc:`glossary/ptt` by serial RTS/DTR, :doc:`glossary/cm108`, GPIO or hamlib
* A web dashboard and Prometheus metrics
* Several soundcards and radio channels at once
* Linux (x86_64 and arm64) and macOS

Getting started
---------------

* :doc:`installation` - packages, tarballs, building from source, and device permissions
* :doc:`quickstart` - a first configuration, connecting an application, and running it as a service

The `README <https://github.com/doismellburning/samoyed#readme>`__ also covers
`switching from Dire Wolf <https://github.com/doismellburning/samoyed#switching-from-dire-wolf>`__,
and the full `feature list <https://github.com/doismellburning/samoyed#features>`__.

Where this site doesn't yet cover something,
`the Dire Wolf documentation <https://github.com/wb2osz/direwolf/tree/master/doc>`__ should provide a useful baseline.

.. toctree::
   :caption: Getting started
   :hidden:

   installation
   quickstart

Usage
-----

* :doc:`usage` - Samoyed as a complete packet node, on top of an external modem, or as the modem under another packet stack

.. toctree::
   :caption: Usage
   :hidden:

   usage

Using Samoyed
-------------

For running Samoyed.

* :doc:`how-to-guides` - recipes for particular jobs, from checking a configuration file to running a NET/ROM node
* :doc:`yaml-config` - Samoyed's YAML configuration format, an alternative to Dire Wolf's line-at-a-time one
* :doc:`bbs` - ``samoyed-bbs``, a packet bulletin board, in its early days

.. toctree::
   :caption: Using Samoyed
   :hidden:

   how-to-guides
   yaml-config
   bbs

Reference
---------

* :doc:`glossary` - the protocols and systems Samoyed implements, with pointers to their specifications

.. toctree::
   :caption: Reference
   :hidden:

   glossary

Getting help
------------

* :doc:`support` - where to ask, and what to put in a bug report

.. toctree::
   :caption: Getting help
   :hidden:

   support

Working on Samoyed
------------------

For anyone changing the code, or wondering why a configuration is refused.

* :doc:`contributing` - getting set up, and where the project's conventions are written down
* :doc:`internals` - how parts of Samoyed work inside

.. toctree::
   :caption: Working on Samoyed
   :hidden:

   contributing
   internals

.. toctree::
   :caption: Project links
   :hidden:

   Samoyed on GitHub <https://github.com/doismellburning/samoyed>
   Releases <https://github.com/doismellburning/samoyed/releases>
   Issue tracker <https://github.com/doismellburning/samoyed/issues>
