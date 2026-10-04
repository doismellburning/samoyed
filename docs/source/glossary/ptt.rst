..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

PTT
===

.. ai-label:: generated

Push to talk:
switching the radio into transmit before sending and back afterwards.
It can be done with a serial port's RTS or DTR line,
a GPIO pin,
the GPIO of a :doc:`cm108` audio adapter,
or a CAT command through Hamlib.
Its counterpart is DCD, data carrier detect,
which says the channel is busy so a transmission should wait.

References
----------

* `Hamlib <https://hamlib.github.io/>`__
* `Push-to-talk on Wikipedia <https://en.wikipedia.org/wiki/Push-to-talk>`__
