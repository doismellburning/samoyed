..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: GPL-2.0-or-later

HDLC
====

.. ai-label:: generated

High-Level Data Link Control,
the framing :doc:`ax25` borrows:
a frame starts and ends with the ``01111110`` flag,
a 0 is stuffed after five 1s in a row so data never looks like a flag,
and a 16-bit FCS (a CRC) at the end catches corruption.
On air the bits are NRZI-coded,
a 0 as a change of tone or level and a 1 as no change.

References
----------

* `High-Level Data Link Control on Wikipedia <https://en.wikipedia.org/wiki/High-Level_Data_Link_Control>`__
* `AX.25 Link Access Protocol for Amateur Packet Radio, v2.2 <https://www.tapr.org/pdf/AX25.2.2.pdf>`__ (TAPR), section 3
