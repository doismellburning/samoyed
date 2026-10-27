..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

Test callsigns
==============

.. ai-label:: generated

Tests, examples and documentation need callsigns,
and Samoyed uses ``Q1TEST``, ``Q2TEST`` and so on for them.
The convention is the project's own,
set out in ``AGENTS.md``,
rather than an external standard.

Why not a real one
------------------

A callsign identifies a licensed station.
One used in a test or example attributes made-up traffic to whoever holds it,
and an example configuration copied as it stands would put their callsign on air or on APRS-IS.
A callsign that can never be issued avoids both.

Tests ported from Dire Wolf keep the callsigns upstream used,
``WB2OSZ`` among them,
so that they still track upstream.

Why ``Q``
---------

The ITU Radio Regulations require every amateur station to have a callsign
from the international series allocated to its administration in Appendix 42
(No. 19.29),
and Appendix 42 allocates no series beginning with ``Q``:
it runs from ``P5A-P9Z`` straight to ``RAA-RZZ``.
Such a callsign could be confused with a Q-code
(``QRM``, ``QTH``, ``QSL`` and so on),
and No. 19.48 rules out callsigns made of the abbreviations reserved in
`Recommendation ITU-R M.1172 <https://www.itu.int/rec/R-REC-M.1172/en>`__,
where the Q-codes are listed.
So no amateur holds a callsign beginning with ``Q``.

The digit after the ``Q`` keeps the usual shape of an amateur callsign:
a prefix, a digit, then a suffix.
A prefix of a single letter is in any case only allowed for
``B``, ``F``, ``G``, ``I``, ``K``, ``M``, ``N``, ``R`` and ``W``
(No. 19.68),
so ``Q1TEST`` couldn't be issued even if ``Q`` were allocated.
``Q1TEST`` is also six characters,
the most an :doc:`AX.25 <../glossary/ax25>` address holds besides its SSID,
so ``Q1TEST`` to ``Q9TEST`` fit, SSID and all (``Q1TEST-7``),
and ``Q10TEST`` does not.

Why not ``N0CALL``
------------------

``N0CALL`` and ``NOCALL`` are what Dire Wolf,
and so Samoyed,
take to mean that ``MYCALL`` hasn't been set.
``IsNoCall`` treats either, or an empty string, as no callsign at all,
and the configuration then refuses what needs a station to identify itself,
such as beaconing or an IGate.
A test using ``N0CALL`` as an ordinary station would take those paths by accident,
so keep it for tests of that sentinel.

References
----------

* `Radio Regulations, Edition of 2024, Volume 1: Articles <https://search.itu.int/history/HistoryDigitalCollectionDocLibrary/1.49.48.en.101.pdf>`__ (ITU) -
  Article 19, Nos. 19.29, 19.48 and 19.68
* `Radio Regulations, Edition of 2024, Volume 2: Appendices <https://search.itu.int/history/HistoryDigitalCollectionDocLibrary/1.49.48.en.102.pdf>`__ (ITU) -
  Appendix 42, the table of allocation of international call sign series
* `ITU prefix on Wikipedia <https://en.wikipedia.org/wiki/ITU_prefix>`__ -
  why ``Q`` prefixes aren't allocated
* `Recommendation ITU-R M.1172 <https://www.itu.int/rec/R-REC-M.1172/en>`__ - the Q-codes
* `AX.25 Link Access Protocol for Amateur Packet Radio, v2.2 <https://www.tapr.org/pdf/AX25.2.2.pdf>`__ (TAPR) -
  the six-character callsign in an address field
