..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: GPL-2.0-or-later

KISS
====

.. ai-label:: generated

"Keep It Simple, Stupid":
the minimal protocol between a TNC and a host application.
Each :doc:`ax25` frame travels whole,
delimited by ``FEND`` bytes and with a one-byte command and port number in front;
the application does everything above the modem itself.

References
----------

* `The KISS TNC: A simple Host-to-TNC communications protocol <http://www.ka9q.net/papers/kiss.html>`__ (Chepponis and Karn)
* `Multi-drop KISS <http://he.fi/pub/oh7lzb/bpq/multi-kiss.pdf>`__
* `KISS on Wikipedia <https://en.wikipedia.org/wiki/KISS_(amateur_radio_protocol)>`__
