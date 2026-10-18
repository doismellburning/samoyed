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
       web [label="Web interface\nhub and server"];
       agw [label="AGWServer"];
       link [label="AX25Link\ndata link state machine"];
       router [label="linkClientRouter"];
       nodeshell [label="node.Node\nnode shell"];
       igate [label="IGate"];
       digi [label="digipeater.Digipeater\nAPRS"];
       cdigi [label="digipeater.Connected\nconnected mode"];
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
       ptt -> audio [label="channelCanTransmit\n(via pttChannels)"];

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
       igate -> tq [label="Append\n(dwcore.FrameSender)"];
       digi -> igate [label="rememberDigipeated"];
       web -> igate [label="webStatus\n(at Start)", style=dashed];
       digi -> tq [label="Append"];
       cdigi -> tq [label="Append"];

       link -> ptt [label="SetConnected"];
       link -> router [label="linkClients"];
       router -> agw [label="linkClients\nAGW clients' links"];
       router -> nodeshell [label="linkClients\nnodeClient's links"];
       nodeshell -> dlq [label="node.DataLink"];
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
(the parsed configuration file and the debug options,
and the ``digipeater.Port`` for each channel that ``RadioConfig.digipeaterPorts`` takes from it for the digipeaters)
and the services with only one user,
which ``DirewolfMain`` hands over in the same way:
the heard list (``mheard``) to the packet filter, the IGate, the beacon service, the received packet handler and the node shell;
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
   The web interface binds its port as it is made,
   but only starts serving once the IGate it reports on exists;
   with its port disabled, or taken, there is no hub,
   and the frames that would have gone to it go nowhere.
   The AGW server is made here,
   so that the transmit service can show its clients each frame sent,
   but only started once the transmit threads are running.
   It binds its port when it is made,
   so a port already in use is reported before the server starts,
   rather than once it is running.
   With the port disabled or taken,
   or when ``-x`` only sends calibration tones,
   there is no AGW server:
   what it would have been handed takes a nil one as having no clients.
#. The ``XmitService``, which starts a transmit thread per radio channel.
#. The IGate and the two digipeaters,
   then the node shell, if the configuration has one,
   and its report to the node map (``nodemap.Reporter``), if the configuration asks for one,
   and the ``AX25Link``, the KISS handler and its transports,
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
It binds its ports when it is made, though,
so a port already in use is reported before the service starts,
rather than once it is running.

**The web interface and the IGate.**
The web interface's hub is handed to the transmit service and the received packet handler,
which are made before the IGate,
and its status page reports on the IGate's connection to APRS-IS.
The hub and the server are made, and the port bound, before either;
the server is handed the station's status (``webStatus``) when it is started (``webui.Server.Start``),
once the IGate exists.

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
  ``frameReceiver``, ``dwcore.FrameSender``, ``igateSender``, ``seizeConfirmer``.
- A component that makes several calls is handed a small interface,
  declared beside it and holding only those calls:
  ``linkClients``, ``linkTransmitter``, ``clientTransmitQueue``, ``xmitQueue``, ``agwDataLink``, ``node.DataLink``.
- ``*TransmitQueue``, ``*DataLinkQueue``, ``*AGWServer`` and ``*node.Node`` satisfy those interfaces without knowing about them.

References
----------

- ``internal/direwolf/direwolf.go``: ``DirewolfMain``, which makes and connects everything above,
  and ``recPacketHandler``.
- The constructors, and the functions and interfaces they take,
  in ``internal/direwolf``:
  ``multi_modem.go`` (``multi_modem_init``, ``ReceiveSink``, ``radioSink``),
  ``dlq.go`` (``frameReceiver``, ``seizeConfirmer``),
  ``tq.go`` (``NewTransmitQueue``, ``clientTransmitQueue``, ``SetIGate``),
  ``xmit.go`` (``NewXmitService``, ``xmitQueue``),
  ``ax25_link.go`` (``NewAX25Link``, ``linkClients``, ``linkTransmitter``),
  ``link_clients.go`` (``newLinkClientRouter``, ``nodeClient``),
  ``server.go`` (``NewAGWServer``, ``agwDataLink``),
  ``igate.go`` (``NewIGate``, ``igateSender``),
  ``kiss_frame.go`` (``NewKissHandler``),
  ``kissnet.go`` (``NewKissNetService``, ``KissNetService.Start``),
  ``aprs_tt.go`` (``NewTTGateway``),
  ``beacon.go`` (``NewBeaconService``, ``beaconIGate``),
  ``audio.go`` (``AudioDevices.setAudioLevel``),
  ``channel_config.go`` (``RadioConfig.digipeaterPorts``)
  and ``recv.go`` (``recv_init``, ``recv_process``).
- ``internal/direwolf/webui_source.go``: ``webui_init``, ``webui_start`` and ``webStatus``,
  and ``internal/webui/server.go``: ``NewServer`` and ``Server.Start``.
- ``internal/ptt/ptt.go``: ``New``,
  handed each channel's configuration by ``RadioConfig.pttChannels`` (``internal/direwolf/channel_config.go``),
  which asks ``AudioDevices.channelCanTransmit`` (``audio.go``) whether each channel has an audio output.
- The digipeaters' constructors, and what they take,
  in ``internal/digipeater``:
  ``digipeater.go`` (``New``, ``Port``)
  and ``cdigipeater.go`` (``NewConnected``).
- ``internal/node/node.go``: ``New``, ``DataLink``.
- ``internal/nodemap/nodemap.go``: ``New``, ``Reporter.Run``.
- ``internal/dwcore/dwcore.go``: ``FrameSender``,
  shared by everything that hands the transmit queue a frame.
- ``AGENTS.md``, the "Dependencies" section.
