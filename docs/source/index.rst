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

.. note::

   Samoyed is pre-release.
   The port is functionally complete,
   but the automated tests run against audio files and loopback rather than real radios.
   Reports from on-air use are especially welcome - working or not - via
   `GitHub issues <https://github.com/doismellburning/samoyed/issues>`__.

Getting started
---------------

The `README <https://github.com/doismellburning/samoyed#readme>`__ covers
`installation <https://github.com/doismellburning/samoyed#installation>`__,
a `quick start <https://github.com/doismellburning/samoyed#quick-start>`__ with a minimal configuration,
`switching from Dire Wolf <https://github.com/doismellburning/samoyed#switching-from-dire-wolf>`__,
and the full `feature list <https://github.com/doismellburning/samoyed#features>`__.
Pre-built binaries and ``.deb`` packages are on the
`releases page <https://github.com/doismellburning/samoyed/releases>`__.

These pages pick up from there:

* :doc:`how-to-guides` - recipes for particular jobs, from checking a configuration file to running a NET/ROM node
* :doc:`yaml-config` - Samoyed's YAML configuration format, an alternative to Dire Wolf's line-at-a-time one
* :doc:`internals` - how parts of Samoyed work inside, for anyone changing the code
* :doc:`glossary` - the protocols and systems Samoyed implements, with pointers to their specifications

Where these don't yet cover something,
`the Dire Wolf documentation <https://github.com/wb2osz/direwolf/tree/master/doc>`__ should provide a useful baseline.

.. toctree::
   :maxdepth: 2
   :caption: Contents:
   :hidden:

   how-to-guides
   yaml-config
   internals
   glossary
