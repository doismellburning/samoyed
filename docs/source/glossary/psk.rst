..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

PSK
===

.. ai-label:: generated

Phase-shift keying:
data sent as changes of phase of a single audio tone,
packing more bits into each symbol than :doc:`afsk` does.
The packet modems follow the ITU modem standards:
2400 bit/s QPSK as V.26 and 4800 bit/s 8PSK as V.27.
V.26 comes in alternatives A and B,
and both ends need to agree on which.

BPSK uses a 1500 Hz carrier at any bit rate,
and puts bits on it as the NinoTNC's PSK modes do.
IL2P reverses the phase for each ``0`` bit and holds it for each ``1``.
AX.25 and FX.25 are G3RUH scrambled, as at 9600 baud,
and the scrambled line level is the phase itself.
Samoyed eases each reversal through zero amplitude over the middle half of the symbol,
rather than jumping the phase,
which keeps the signal from splattering across neighbouring frequencies.
The amplitude dip means the transmitter has to be linear:
keep the audio level below where the ALC starts to act.

References
----------

* `ITU-T V.26 <https://www.itu.int/rec/T-REC-V.26>`__ and `V.27 ter <https://www.itu.int/rec/T-REC-V.27ter>`__
* `Phase-shift keying on Wikipedia <https://en.wikipedia.org/wiki/Phase-shift_keying>`__
