..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

Getting Help
============

.. ai-label:: generated

Questions, problems and reports of Samoyed working go to
`GitHub issues <https://github.com/doismellburning/samoyed/issues>`__.
Search there first, in case someone has already asked.

Samoyed is pre-release,
and its automated tests run against audio files and loopback rather than real radios,
so a report of it working on the air is as useful as one of it failing.
Reports are most useful on:

* on-air use with real soundcards and radios
* PTT methods (serial RTS/DTR, CM108, GPIO, hamlib)
* macOS
* interoperability with other TNCs, and with KISS and AGW applications

What to put in a report
-----------------------

The more of this there is, the less back-and-forth it takes:

* The version, from ``samoyed-direwolf --version``
* The platform: operating system, architecture, and how Samoyed was installed
* The radio, the audio interface, and how PTT is done
* The configuration file, with any passwords taken out,
  and what ``samoyed-direwolf -c <file> --config-check`` says about it
  (see :ref:`how-to-guides:Check a config file without starting up`)
* What happened, what you expected to happen,
  and the log output around it
* If Dire Wolf behaves differently with the same configuration, say so - Samoyed aims to match it

For a frame that doesn't decode,
a recording of the audio is worth more than any description of it:
``samoyed-atest`` decodes a ``.wav`` file the way ``samoyed-direwolf`` decodes a receiver,
so a recording that it gets wrong can be fixed and turned into a test.
Say which ``samoyed-atest`` options you ran it with.
