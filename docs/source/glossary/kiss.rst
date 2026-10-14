..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

KISS
====

.. ai-label:: generated

"Keep It Simple, Stupid":
the minimal protocol between a TNC and a host application.
Each :doc:`ax25` frame travels whole,
delimited by ``FEND`` bytes and with a one-byte command and port number in front;
the application does everything above the modem itself.

Samoyed also accepts the ACKMODE data frame (command ``0x0C``) from the multi-drop KISS extension:
its two-byte frame ID goes back to the client,
as a ``0x0C`` frame of its own,
once the frame has been transmitted.
Winlink Express calls this its ``ACKMODE`` TNC model.
The rest of multi-drop KISS, polling (``0x0E``) and checksums, is not supported.

References
----------

* `The KISS TNC: A simple Host-to-TNC communications protocol <http://www.ka9q.net/papers/kiss.html>`__ (Chepponis and Karn)
* `Multi-drop KISS <http://he.fi/pub/oh7lzb/bpq/multi-kiss.pdf>`__
* `KISS on Wikipedia <https://en.wikipedia.org/wiki/KISS_(amateur_radio_protocol)>`__
