..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

FBB forwarding
==============

.. ai-label:: generated

The protocol packet BBSes use to pass mail to each other,
from the FBB BBS software.

Each BBS starts by sending its system identifier (SID) in square brackets,
its letters saying what it can do:
``F`` for this protocol, ``B`` for compression.
Then each side in turn sends a block of up to five proposals,
one line per message
(type, sender, @ field, addressee, bulletin ID and size)
ending with ``F>`` and a checksum.
The other side answers with an ``FS`` line,
a character per proposal:
``+`` to have it sent, ``-`` if it already has it, ``=`` to defer it.
A side with nothing to propose sends ``FF``,
and one answered ``FF`` with nothing either sends ``FQ`` to end the session.

With compression, a message travels in binary blocks
after a header carrying its title,
and ends with a checksum.
Its text is compressed with LZHUF.
The B2 version of the protocol, from Winlink,
carries messages in the B2F format:
headers, a blank line, then the body and any attachments.

References
----------

* `FBB forward protocol (FBB documentation, Appendix 9) <https://sources.debian.org/src/fbbdoc/1999/docfwpro.htm>`__
* `FBB compressed forward (FBB documentation, Appendix 10) <https://sources.debian.org/src/fbbdoc/1999/docfwcom.htm>`__
* `Open B2F: Winlink Message Structure and B2 Forwarding Protocol <https://winlink.org/B2F>`__
