..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

Wiring
======

.. ai-label:: generated

How ``DirewolfMain`` (``internal/direwolf/direwolf.go``) builds the components of ``samoyed-direwolf``
and connects them.
No component reaches another through a package-level variable:
each is handed what it uses when it is made,
as a constructor argument.

Each arrow points from a component to something it uses,
and is labelled with what it is handed:
a function, often a method value such as ``dataLinkQueue.RecFrame``,
or a small interface declared beside the code that uses it.
Dashed arrows are connections made after construction rather than at it.

.. graphviz::
   :alt: The components DirewolfMain builds, and the functions and interfaces each is handed to reach the others

   digraph wiring {
       rankdir=LR;
       node [shape=box, fontname="sans-serif", fontsize=10];
       edge [fontname="sans-serif", fontsize=8];
       graph [fontname="sans-serif"];

       audio [label="AudioDevices"];
       l2r [label="Layer2Receiver\n(multi_modem_init)"];
       sink [label="radioSink"];
       dlq [label="DataLinkQueue"];
       nettnc [label="Network TNCs"];
       serialtnc [label="Serial TNCs"];
       axudp [label="AXUDP channels"];
       tq [label="TransmitQueue"];
       tones [label="ToneGenerators"];
       ptt [label="PTT"];
       xmit [label="XmitService\ntransmit threads"];
       web [label="Web UI hub"];
       agw [label="AGWServer"];
       link [label="AX25Link\ndata link state machine"];
       igate [label="IGate"];
       digi [label="Digipeater"];
       cdigi [label="ConnectedDigipeater"];
       kiss [label="KissHandler"];
       kissnet [label="KISS TCP, serial,\npseudo terminal"];
       apps [label="clientApps"];
       tt [label="TTGateway\nAPRStt"];
       beacon [label="BeaconService"];
       recvthreads [label="recv_init\naudio device goroutines"];
       handler [label="recPacketHandler"];
       recvproc [label="recv_process\nsingle goroutine"];

       l2r -> sink [label="ReceiveSink"];
       sink -> dlq [label="RecFrame\n(frameReceiver)"];
       sink -> ptt [label="SetDCD", style=dashed];
       l2r -> ptt [label="TransmitInhibited", style=dashed];
       audio -> l2r [label="AudioLevel", style=dashed];

       nettnc -> dlq [label="RecFrame"];
       serialtnc -> dlq [label="RecFrame"];
       axudp -> dlq [label="RecFrame"];

       tq -> nettnc [label="NetTNC per channel"];
       tq -> serialtnc [label="SerialTNC per channel"];
       tq -> axudp [label="AXUDPChannel\nper channel"];
       tq -> dlq [label="SeizeConfirm\n(seizeConfirmer)"];
       tq -> igate [label="sendRecPacket\n(igateSender)", style=dashed];

       tones -> audio [label="AudioSink"];
       ptt -> l2r [label="MuteInput"];
       ptt -> dlq [label="ChannelBusy"];

       xmit -> tq [label="xmitQueue"];
       xmit -> ptt [label="SetPTT"];
       xmit -> l2r [label="DataDetectAny"];
       xmit -> dlq [label="SeizeConfirm"];
       xmit -> web [label="onTransmit"];
       xmit -> agw [label="onTransmit"];
       xmit -> audio;
       xmit -> tones;

       agw -> dlq [label="agwDataLink"];
       agw -> tq [label="clientTransmitQueue"];

       igate -> dlq [label="RecFrame"];
       igate -> tq [label="Append\n(frameSender)"];
       digi -> igate [label="rememberDigipeated"];
       digi -> tq [label="Append"];
       cdigi -> tq [label="Append"];

       link -> ptt [label="SetConnected"];
       link -> agw [label="linkClients"];
       link -> tq [label="linkTransmitter"];

       kiss -> xmit [label="timing setters"];
       kiss -> tq [label="clientTransmitQueue"];
       kiss -> kissnet [label="KISSCOPY peers"];
       kissnet -> kiss [label="client bytes\n(at Start)", style=dashed];

       apps -> agw;
       apps -> kissnet;

       tt -> apps [label="object reports"];
       tt -> digi [label="Remember"];
       tt -> igate [label="sendRecPacket"];
       tt -> dlq [label="RecFrame"];
       tt -> tq [label="Append"];
       tt -> l2r [label="AudioLevel"];

       beacon -> igate [label="beaconIGate"];
       beacon -> dlq [label="RecFrame"];
       beacon -> tq [label="Append"];

       recvthreads -> audio [label="SampleSource"];
       recvthreads -> l2r [label="sampleReceiver"];
       recvthreads -> tt [label="Button"];

       handler -> igate [label="sendRecPacket"];
       handler -> apps;
       handler -> web;
       handler -> digi;
       handler -> cdigi;
       handler -> tt;

       recvproc -> dlq [label="dataLinkSource"];
       recvproc -> link;
       recvproc -> handler;
   }

The diagram leaves out what is plain configuration
(the parsed configuration file and the debug options)
and the services with only one user,
which ``DirewolfMain`` hands over in the same way:
the heard list (``mheard``) to the packet filter, the IGate, the beacon service and the received packet handler;
the packet filter to the IGate and the digipeaters;
the APRS log to the beacon service and the received packet handler;
the GPS to the beacon service and the waypoint sender;
and the waypoint sender and the APRS decoder to the received packet handler.

How a received frame gets from a sample to ``recv_process``,
and what happens to it there,
is on the :doc:`receive-path` page.

Construction order
------------------

``DirewolfMain`` makes each component once what it is handed exists,
so the order of construction follows the arrows:

#. The audio devices, then the ``DataLinkQueue``,
   which almost everything feeds.
#. The demodulators (``multi_modem_init``),
   handed a ``radioSink`` that passes their frames to the ``DataLinkQueue``.
#. The network TNCs, the serial TNCs and the AXUDP channels.
   Their listeners start straight away and can hand the queue frames at once.
#. The ``TransmitQueue``,
   which sends a packet for a network TNC, serial TNC or AXUDP channel straight there
   rather than queueing it.
#. The tone generators and the PTT.
#. The web interface and the AGW server.
   The AGW server is made here,
   so that the transmit service can show its clients each frame sent,
   but only started once the transmit threads are running.
#. The ``XmitService``, which starts a transmit thread per radio channel.
#. The IGate and the two digipeaters,
   then the ``AX25Link``, the KISS handler and its transports,
   the APRStt gateway and the beacon service.
#. The audio device goroutines (``recv_init``),
   and finally ``recv_process``,
   which takes from the ``DataLinkQueue`` and hands each item to the ``AX25Link`` or the ``recPacketHandler``.

Nothing is sent or received until the goroutines that do so are started,
so a component made later in this list can still be handed to one made earlier,
by a connection made after construction, before anything starts.
The next section lists those connections.

Connections made after construction
-----------------------------------

**The PTT and the demodulators.**
The PTT mutes a channel's receiver while it transmits (``Layer2Receiver.MuteInput``),
so it is made after the demodulators.
The demodulators in turn set the PTT's DCD output (``radioSink.setDCD``)
and read its transmit inhibit input (``Layer2Receiver.txInhibited``),
so those two are set once the PTT exists.

**The transmit queue and the IGate.**
The IGate queues packets from APRS-IS for transmission,
and the transmit queue hands the IGate whatever is queued for the IGate's own channel.
The queue is made first,
and handed the IGate's ``sendRecPacket`` with ``TransmitQueue.SetIGate`` once the IGate exists.

**The KISS handler and the KISS TCP service.**
The handler copies a client's data frames to the TCP clients with KISSCOPY,
so it is made after the ``KissNetService`` and handed it.
The service is handed the handler when it is started (``KissNetService.Start``),
rather than when it is made.

**The audio devices and the demodulators.**
Each audio device's statistics report its channels' received audio levels,
which the demodulators keep,
so the devices are handed ``Layer2Receiver.AudioLevel`` with ``AudioDevices.setAudioLevel`` once the demodulators exist.
Unlike the others this is not a cycle:
the demodulators do not use the audio devices,
and are made after them only because ``DirewolfMain`` opens the devices first.

Conventions
-----------

The shapes in the diagram follow the dependency conventions in ``AGENTS.md``:

- A component that makes one call is handed a function,
  typed for what it does with it:
  ``frameReceiver``, ``frameSender``, ``igateSender``, ``seizeConfirmer``.
- A component that makes several calls is handed a small interface,
  declared beside it and holding only those calls:
  ``linkClients``, ``linkTransmitter``, ``clientTransmitQueue``, ``xmitQueue``, ``agwDataLink``.
- ``*TransmitQueue``, ``*DataLinkQueue`` and ``*AGWServer`` satisfy those interfaces without knowing about them.

References
----------

- ``internal/direwolf/direwolf.go``: ``DirewolfMain``, which makes and connects everything above,
  and ``recPacketHandler``.
- The constructors, and the functions and interfaces they take,
  in ``internal/direwolf``:
  ``multi_modem.go`` (``multi_modem_init``, ``ReceiveSink``, ``radioSink``),
  ``dlq.go`` (``frameReceiver``, ``seizeConfirmer``),
  ``tq.go`` (``NewTransmitQueue``, ``frameSender``, ``clientTransmitQueue``, ``SetIGate``),
  ``xmit.go`` (``NewXmitService``, ``xmitQueue``),
  ``ax25_link.go`` (``NewAX25Link``, ``linkClients``, ``linkTransmitter``),
  ``server.go`` (``NewAGWServer``, ``agwDataLink``),
  ``igate.go`` (``NewIGate``, ``igateSender``),
  ``kiss_frame.go`` (``NewKissHandler``),
  ``kissnet.go`` (``KissNetService.Start``),
  ``aprs_tt.go`` (``NewTTGateway``),
  ``beacon.go`` (``NewBeaconService``, ``beaconIGate``),
  ``audio.go`` (``AudioDevices.setAudioLevel``)
  and ``recv.go`` (``recv_init``, ``recv_process``).
- ``internal/ptt/ptt.go``: ``New``,
  handed each channel's configuration by ``RadioConfig.pttChannels`` (``internal/direwolf/channel_config.go``).
- ``AGENTS.md``, the "Dependencies" section.
