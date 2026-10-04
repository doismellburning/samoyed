..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: GPL-2.0-or-later

PSK
===

🤖 Phase-shift keying:
data sent as changes of phase of a single audio tone,
packing more bits into each symbol than :doc:`afsk` does.
The packet modems follow the ITU modem standards:
2400 bit/s QPSK as V.26 and 4800 bit/s 8PSK as V.27.
V.26 comes in alternatives A and B,
and both ends need to agree on which.

References
----------

* `ITU-T V.26 <https://www.itu.int/rec/T-REC-V.26>`__ and `V.27 ter <https://www.itu.int/rec/T-REC-V.27ter>`__
* `Phase-shift keying on Wikipedia <https://en.wikipedia.org/wiki/Phase-shift_keying>`__
