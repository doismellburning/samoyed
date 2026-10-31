..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

Contributing
============

.. ai-label:: generated

Contributions are welcome.
With the port from Dire Wolf functionally complete,
the focus is on internal tidying - code quality, test coverage and idiomatic Go -
so bug fixes and test improvements are especially appreciated.

Getting set up
--------------

.. code::

    $ git clone https://github.com/doismellburning/samoyed
    $ cd samoyed
    $ make setup    # install build, test and lint dependencies (apt-based Linux, or macOS with Homebrew)
    $ make all      # build and test everything
    $ make check    # run the linters

``make fix`` applies what automatic fixes it can,
and is worth running after every change.

Where the rules are
-------------------

These live in the repository rather than here, so they change alongside the code:

* `CONTRIBUTING.md <https://github.com/doismellburning/samoyed/blob/main/CONTRIBUTING.md>`__ - where to start
* `AGENTS.md <https://github.com/doismellburning/samoyed/blob/main/AGENTS.md>`__ -
  the ``make`` targets, code style, commit and pull request conventions, testing and licensing.
  Despite the name, it is written for people as much as for agents
* `AUTHORS.md <https://github.com/doismellburning/samoyed/blob/main/AUTHORS.md>`__ -
  who has written Samoyed, and Dire Wolf before it

:doc:`internals` covers how parts of Samoyed work inside,
and the site's own source is ``docs/source`` in the repository.
