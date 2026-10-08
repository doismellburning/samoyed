..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

Channels
========

.. ai-label:: generated

A channel is a number naming one path frames travel in and out by:
a radio on the internal modem, a remote TNC, APRS-IS and so on.
Most of the code treats it as just that, an index.
Each channel also has a *medium*,
``chan_medium[channel]`` in the audio configuration,
and the parts of Samoyed that care what is on the far end consult it.

.. note::

   Channels, and the media hung off them, are inherited from Dire Wolf as they stand.
   They're a candidate for restructuring,
   though probably not any time soon.

Numbering and media
-------------------

There are ``MAX_TOTAL_CHANS`` (16) channels,
because the KISS port field is four bits wide.
Channels overlap with KISS ports conceptually but aren't the same thing;
for now, though, Samoyed treats a KISS port number and a channel number as one and the same.
The first ``MAX_RADIO_CHANS`` (6: three audio devices, two channels each) are for radios;
the rest are *virtual* channels.

.. list-table::
   :header-rows: 1

   * - Medium
     - Configured by
     - Channel numbers
   * - ``MEDIUM_RADIO``
     - ``ADEVICE`` / ``CHANNEL`` - the internal soundcard modem
     - 0 to 5
   * - ``MEDIUM_IGATE``
     - ``ICHANNEL`` - APRS-IS, offered to client applications as a port
     - 6 to 15
   * - ``MEDIUM_NETTNC``
     - ``NCHANNEL`` - a remote TNC reached by KISS over TCP
     - 6 to 15
   * - ``MEDIUM_AXUDP``
     - ``axudpPorts`` in the :doc:`../yaml-config` - AX.25 over UDP
     - 6 to 15
   * - ``MEDIUM_NONE``
     - nothing: the default, and not usable
     -

Radio channel numbers follow the audio devices, so they can have gaps:
a mono device 0 and a stereo device 1 give channels 0, 2 and 3.
A virtual channel can't claim a number another channel already has.

Samoyed keeps one IGate channel number, ``igate_vchannel``,
so it can recognise frames that came from APRS-IS without searching every channel.

What any medium will do
-----------------------

Client applications
    A KISS client may transmit on any channel whose medium isn't ``MEDIUM_NONE``.
    Every frame received, whatever its channel, goes to the KISS and AGW clients,
    and AGW's port list describes all four media.

The transmit queue
    ``TransmitQueue.Append`` is where the medium decides where a frame goes:
    a radio channel's frames join the queue for the modem,
    an IGate channel's go to APRS-IS,
    and a network TNC's or AXUDP channel's are sent straight out.
    Anything that ends up there therefore works on any configured channel.

Beacons
    A beacon may be sent to any configured channel.
    One sent to the IGate channel uses channel 0's ``MYCALL``,
    as the IGate channel has none of its own.

What will cross-route
---------------------

For the APRS paths, radio, network TNC and AXUDP channels are interchangeable,
in either direction.
The digipeater's own comment gives the reason:
"Network TNC is OK for UI frames where we don't care about timing."

``DIGIPEAT`` and ``FILTER``
    The APRS digipeater takes its FROM and TO channels from
    ``MEDIUM_RADIO``, ``MEDIUM_NETTNC`` and ``MEDIUM_AXUDP``,
    so a frame heard on a radio can be repeated to an AXUDP neighbour, or the reverse.
    ``FILTER`` names APRS-IS as ``IG`` and refuses the IGate channel's number.

IGate, RF to APRS-IS
    Frames received on any of those three media are gated.
    Each such channel needs a ``MYCALL`` before the IGate will start,
    and each gets the default ``i/180`` APRS-IS-to-RF filter.

IGate, APRS-IS to RF (``IGTXVIA``)
    The transmit channel is checked to be in the range 0 to 15,
    but its medium is not checked.

``TTOBJ`` transmit
    Any of the three media, alongside the ``APP`` and ``IG`` destinations.

What only a radio will do
-------------------------

Anything that needs channel-busy detection, push-to-talk or the audio itself
insists on ``MEDIUM_RADIO``:

* ``CDIGIPEAT`` and ``CFILTER``, the connected-mode digipeater,
  on both FROM and TO channels -
  a network TNC "probably would not provide information about channel status"
* ``REGEN``, on both FROM and TO channels
* the receive channel of ``TTOBJ``, since the DTMF decoder is part of the modem
* push-to-talk, the modems and tone generation

Loose ends
----------

Connected mode
    The AX.25 link layer, and AGW's connect command, accept network TNC and AXUDP channels as well as radios.
    With no modem to seize, the transmit queue confirms the channel at once
    and sends each frame straight out, leaving the timing to whatever is on the far end.
    The connected-mode digipeater, by contrast, refuses them.
    Its receive check lists ``MEDIUM_NETTNC`` as acceptable
    but also requires a radio channel number,
    so in practice it only ever accepts radio channels.

The IGate channel
    Frames received from APRS-IS on the IGate channel go to client applications and nowhere else:
    they are not digipeated and not gated back.

References
----------

* `Dire Wolf's src/direwolf.h <https://github.com/wb2osz/direwolf/blob/master/src/direwolf.h>`__ - where the channel limits come from
* `internal/direwolf/channel_config.go <https://github.com/doismellburning/samoyed/blob/main/internal/direwolf/channel_config.go>`__ - the media
* `internal/direwolf/direwolf_h.go <https://github.com/doismellburning/samoyed/blob/main/internal/direwolf/direwolf_h.go>`__
  and `internal/phy/consts.go <https://github.com/doismellburning/samoyed/blob/main/internal/phy/consts.go>`__ - the channel limits
* `internal/direwolf/config.go <https://github.com/doismellburning/samoyed/blob/main/internal/direwolf/config.go>`__ -
  ``ICHANNEL``, ``NCHANNEL``, the AXUDP port checks, ``DIGIPEAT``, ``REGEN``, ``CDIGIPEAT``, ``FILTER``, ``CFILTER``, ``TTOBJ``, ``IGTXVIA`` and the IGate ``MYCALL`` checks
* `internal/direwolf/config_yaml.go <https://github.com/doismellburning/samoyed/blob/main/internal/direwolf/config_yaml.go>`__ - ``axudpPorts``
* `internal/direwolf/tq.go <https://github.com/doismellburning/samoyed/blob/main/internal/direwolf/tq.go>`__ -
  ``TransmitQueue.Append``, ``LMDataRequest`` and ``LMSeizeRequest``
* `internal/direwolf/kiss_frame.go <https://github.com/doismellburning/samoyed/blob/main/internal/direwolf/kiss_frame.go>`__ - KISS client transmit check
* `internal/direwolf/server.go <https://github.com/doismellburning/samoyed/blob/main/internal/direwolf/server.go>`__ - AGW port list and connected-mode check
* `internal/direwolf/beacon.go <https://github.com/doismellburning/samoyed/blob/main/internal/direwolf/beacon.go>`__ - beacon ``MYCALL`` choice
* `internal/direwolf/digipeater.go <https://github.com/doismellburning/samoyed/blob/main/internal/direwolf/digipeater.go>`__
  and `internal/direwolf/cdigipeater.go <https://github.com/doismellburning/samoyed/blob/main/internal/direwolf/cdigipeater.go>`__ - the digipeaters' receive checks
* `internal/direwolf/direwolf.go <https://github.com/doismellburning/samoyed/blob/main/internal/direwolf/direwolf.go>`__ -
  ``app_process_rec_packet``, which hands received frames on
