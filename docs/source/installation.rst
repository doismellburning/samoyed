..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

Installation
============

.. ai-label:: generated

Samoyed runs on Linux (x86_64 and arm64) and macOS.

From a release
--------------

Each release on the
`releases page <https://github.com/doismellburning/samoyed/releases>`__
carries:

* ``samoyed-binary_<version>_amd64.deb`` and ``…_arm64.deb``,
  for Debian, Ubuntu, Raspberry Pi OS and other Debian-based systems
* ``samoyed-<version>-Linux-X64.tgz``, ``…-Linux-ARM64.tgz`` and ``…-macOS-ARM64.tgz``,
  each holding a ``dist/`` directory of the programs
  and a ``data/`` directory of the APRS data files they read

On a Debian-based system, install the package with ``apt``,
which fetches the libraries it needs as it goes:

.. code::

    $ sudo apt install ./samoyed-binary_*.deb

That puts every program in ``/usr/bin``, each prefixed ``samoyed-``,
along with their man pages,
and the data files in ``/usr/share/samoyed``.
It installs no configuration file and no service:
the `quick start <https://github.com/doismellburning/samoyed#quick-start>`__ covers the first.

From a tarball,
copy the programs from ``dist/`` to somewhere on your ``PATH``,
and the files in ``data/`` to ``/usr/local/share/samoyed``,
where Samoyed looks for them
(as well as in ``data/`` under the directory it is run from).
Without them, it can't name the device that sent an APRS packet.
``samoyed-direwolf``, ``samoyed-atest``, ``samoyed-gen_packets`` and ``samoyed-gen_tone``
need PortAudio
(``libportaudio2`` on Debian and Ubuntu, ``portaudio`` from Homebrew on macOS);
the other programs need nothing more.

From source
-----------

Building needs Go and the development packages ``dev-setup.sh`` lists.
On an apt-based Linux, or macOS with Homebrew,
``make setup`` installs them:

.. code::

    $ git clone https://github.com/doismellburning/samoyed
    $ cd samoyed
    $ make setup
    $ make cmds

The programs are then in ``dist/``.

Permissions on Linux
--------------------

To run as yourself rather than as root,
your user needs to be in the groups that own the devices Samoyed opens:

* ``audio``, for the sound card
* ``dialout``, for a serial port - serial PTT,
  or a KISS TNC on ``/dev/ttyUSB*`` or ``/dev/ttyACM*``

.. code::

    $ sudo usermod -a -G audio,dialout "$USER"

Log out and back in for that to take effect.

PTT through the GPIO of a :doc:`glossary/cm108` sound adapter
goes through ``/dev/hidraw*``, which only root can open by default.
The repository's
`conf/99-direwolf-cmedia.rules <https://github.com/doismellburning/samoyed/blob/main/conf/99-direwolf-cmedia.rules>`__
gives the ``audio`` group access to it.
The ``.deb`` doesn't install it, so copy it into place and reload udev:

.. code::

    $ sudo cp conf/99-direwolf-cmedia.rules /etc/udev/rules.d/
    $ sudo udevadm control --reload-rules && sudo udevadm trigger

Hamlib PTT loads the Hamlib library only when it's configured,
so it's needed only then
(``libhamlib4`` on Debian and Ubuntu, which the ``.deb`` recommends).

Checking it
-----------

.. code::

    $ samoyed-direwolf --version

prints the version and the commit it was built from.
Then carry on with the `quick start <https://github.com/doismellburning/samoyed#quick-start>`__.
