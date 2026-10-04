..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

Digipeater
==========

.. ai-label:: generated

A digital repeater:
a station that retransmits :doc:`ax25` frames
whose path names it, or a generic alias such as ``WIDE2-2``,
marking that hop as used so the frame doesn't bounce back and forth.
:doc:`aprs` relies on them to extend a station's reach,
and the ``WIDEn-N`` scheme limits how far a packet spreads.

References
----------

* `The New n-N Paradigm <http://www.aprs.org/fix14439.html>`__
* `Preemptive digipeating <http://www.aprs.org/aprs12/preemptive-digipeating.txt>`__
* ``APRS-Digipeater-Algorithm.pdf`` in `the APRS Documentation Project <https://github.com/wb2osz/aprsspec>`__
* `Digipeater on Wikipedia <https://en.wikipedia.org/wiki/Digipeater>`__
