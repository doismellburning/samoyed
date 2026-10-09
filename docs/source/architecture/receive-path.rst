..
    SPDX-FileCopyrightText: The Samoyed Authors
    SPDX-License-Identifier: AGPL-3.0-or-later

Receive path
============

.. ai-label:: generated

How a received signal travels through ``samoyed-direwolf``:
from an audio sample,
through demodulation and frame decoding,
to the application layer and the AX.25 data link state machine.

.. graphviz::
   :alt: Flow of received audio from the sound card through the demodulators, frame decoders and data link queue to the applications and the AX.25 data link state machine

   digraph receive_path {
       rankdir=TB;
       node [shape=box, fontname="sans-serif", fontsize=10];
       edge [fontname="sans-serif", fontsize=9];
       graph [fontname="sans-serif"];

       subgraph cluster_adev {
           label="recv_adev_thread - one goroutine per audio device";
           style=rounded;

           src [label="AudioDevices\nsound card (PortAudio), SDR over UDP, stdin"];
           get [label="demod_get_sample"];
           dtmf [label="DTMFDecoder.Sample\n-> onButton (ttGateway.Button)"];
           mm [label="Layer2Receiver.ProcessSample\n-> MultiModem.ProcessSample\nDC average, fan out to subchannels"];
           demod [label="Demodulator.ProcessSample\naudio level, decimation, pick modem"];
           afsk [label="demod_afsk_process_sample"];
           psk [label="demod_psk_process_sample"];
           g96 [label="demod_9600_process_sample"];
           pll [label="nudge_pll_*\nper-slicer clock recovery,\none call per bit"];
           dcd [label="pll_dcd_each_symbol2\n-> Layer2Receiver.DCDChange"];
           l2 [label="Layer2Receiver.RecBitNew\nBER injection, per-slicer receivers"];
           line [label="linecode.Decoder.Decode\nNRZI, G3RUH descrambling"];
           eas [label="eas.Receiver.RecBit"];
           il2p [label="il2p.Receiver.RecBit"];
           fx25 [label="fx25.Receiver.RecBit\nReed-Solomon"];
           hdlc [label="hdlc.Receiver.RecBit\nflag detection, collect raw bits"];
           rec2 [label="hdlc_rec2_block / try_decode\nbit unstuffing, FCS check,\nbit-fixing retries"];
           prf [label="Layer2Receiver.recFrame\nbytes -> ax25.Packet\n(AIS, EAS wrapped as UI frames)"];
           prp [label="MultiModem.processRecPacket"];
           cand [label="candidates[subchan][slice]\naged per sample"];
           pick [label="pickBestCandidate\nprefer FEC, fewest bits fixed,\nagreement between decoders;\ndrop the rest"];
           sink [label="radioSink.RecFrame"];

           src -> get;
           get -> mm [label="per channel"];
           get -> dtmf [label="DTMF enabled"];
           mm -> demod;
           demod -> afsk [label="AFSK, EAS"];
           demod -> psk [label="BPSK, QPSK, 8PSK"];
           demod -> g96 [label="baseband,\nG3RUH, AIS"];
           afsk -> pll;
           psk -> pll;
           g96 -> pll;
           pll -> dcd [label="each symbol"];
           pll -> l2;
           l2 -> eas [label="EAS"];
           l2 -> line;
           l2 -> il2p [label="raw bit,\nnot AIS"];
           line -> fx25 [label="data bit,\nnot AIS"];
           line -> hdlc;
           hdlc -> rec2 [label="bits between flags"];
           rec2 -> prf;
           fx25 -> prf;
           eas -> prf;
           prf -> prp;
           il2p -> prp [label="already a Packet"];
           prp -> sink [label="one decoder,\nno FX.25 in progress"];
           prp -> cand [label="otherwise"];
           cand -> pick [label="old enough"];
           pick -> sink;
       }

       subgraph cluster_producers {
           label="Other sources";
           style=dashed;

           nettnc [label="Network TNC"];
           axudp [label="AXUDP channel"];
           igate_in [label="APRS-IS, as ICHANNEL"];
           beacon [label="Beacons with SENDTO=R\n(simulated reception)"];
           tt [label="APRStt gateway"];
           agw [label="AGW clients\nconnect, disconnect,\ndata, register"];
           ptt [label="PTT: channel busy"];
           xmit [label="Transmitter:\nseize confirm"];
       }

       subgraph cluster_dlq {
           label="DataLinkQueue";
           style=rounded;

           dlq_frame [label="DLQ_REC_FRAME"];
           dlq_req [label="connect, disconnect, data,\nregister, channel busy,\nseize confirm, ..."];
       }

       sink -> dlq_frame;
       nettnc -> dlq_frame;
       axudp -> dlq_frame;
       igate_in -> dlq_frame;
       beacon -> dlq_frame;
       tt -> dlq_frame;
       agw -> dlq_req;
       ptt -> dlq_req;
       xmit -> dlq_req;

       subgraph cluster_recv_process {
           label="recv_process - single goroutine";
           style=rounded;

           wait [label="WaitWhileEmpty\nuntil the next link timer expires"];
           timer [label="dl_timer_expiry\nT1, T3, TM201"];
           dispatch [label="switch on item type", shape=diamond];
           app [label="recPacketHandler\n.app_process_rec_packet"];
           lmdi [label="lm_data_indication"];
           dlreq [label="dl_connect_request,\ndl_data_request,\ndl_disconnect_request, ..."];
           lmx [label="lm_channel_busy,\nlm_seize_confirm"];

           wait -> timer [label="timed out"];
           wait -> dispatch [label="item"];
           dispatch -> app [label="received frame"];
           dispatch -> lmdi [label="received frame,\nafterwards"];
           dispatch -> dlreq [label="client request"];
           dispatch -> lmx [label="channel busy,\nseize confirm"];
       }

       dlq_frame -> wait;
       dlq_req -> wait;

       subgraph cluster_app {
           label="Applications";
           style=dashed;

           log [label="Log 'Heard' and 'Packet'"];
           aprsdec [label="aprs.Decoder\n-> APRS log, mheard, waypoints"];
           clients [label="KISS and AGW clients"];
           web [label="Web UI"];
           ichan [label="from ICHANNEL?\nyes: stop here", shape=diamond];
           ttq [label="DTMF, or\nstarts with 't'?", shape=diamond];
           ttseq [label="ttGateway.Sequence"];
           igate_out [label="IGate, RF to IS"];
           regen [label="APRS digipeater\nRegen"];
           digi [label="APRS digipeater\nDigipeat"];
           cdigi [label="Connected-mode digipeater"];
       }

       app -> log;
       app -> aprsdec [label="APRS"];
       app -> clients;
       app -> web;
       app -> ichan;
       ichan -> ttq [label="no"];
       ttq -> ttseq [label="yes"];
       ttq -> igate_out [label="no; APRS,\nclean or FEC"];
       ttq -> regen [label="no"];
       ttq -> digi [label="no; APRS,\nclean or FEC"];
       ttq -> cdigi [label="no; radio channel,\nclean or FEC"];

       subgraph cluster_dlsm {
           label="AX.25 v2.2 data link state machine";
           style=rounded;

           find [label="Skip if not yet digipeated;\nfind or create the link\nby channel, own and peer callsign;\ncreate only for SABM or SABME"];
           ftype [label="frame type", shape=diamond];
           iframe [label="i_frame"];
           sframe [label="rr_rnr_frame, rej_frame,\nsrej_frame"];
           uframe [label="sabm_e_frame, disc_frame,\ndm_frame, ua_frame,\nfrmr_frame, ui_frame"];
           xframe [label="xid_frame, test_frame"];
           states [label="Disconnected, Awaiting connection,\nAwaiting release, Connected,\nTimer recovery, Awaiting v2.2 connection"];

           find -> ftype;
           ftype -> iframe;
           ftype -> sframe;
           ftype -> uframe;
           ftype -> xframe;
           iframe -> states;
           sframe -> states;
           uframe -> states;
           xframe -> states;
       }

       lmdi -> find;
       timer -> states;
       dlreq -> states;
       lmx -> states;

       tq [label="Transmit queue\n-> transmitter, modulator"];
       agw_out [label="AGW clients"];

       states -> tq [label="frames to send"];
       states -> agw_out [label="link up and down,\nreceived data"];
   }

Concurrency
-----------

Everything from reading a sample to ``radioSink.RecFrame`` runs on the audio device's own goroutine,
one sample at a time.
So the HDLC, FX.25 and IL2P receivers all see each bit from a slicer as it arrives.

Everything after the ``DataLinkQueue`` runs on the single ``recv_process`` goroutine.
The data link state machine only ever runs there.

``recv_process`` waits on the queue only until the next data link timer is due,
so timer expiry is handled on the same goroutine as received frames.

Many decoders, one frame
------------------------

A channel can run several demodulators (subchannels),
each with several slicers (decision thresholds),
and each subchannel and slicer pair has its own HDLC, FX.25 and IL2P receivers.
More than one of them can decode the same frame.
``MultiModem.processRecPacket`` holds each decoded frame for a few bit times
so that ``pickBestCandidate`` can pass on only the best copy.

One queue, two consumers
------------------------

Frames received over the air,
frames from network sources such as AXUDP and the IGate,
and requests from AGW clients
all go onto the ``DataLinkQueue``.
``recv_process`` gives every received frame first to ``recPacketHandler.app_process_rec_packet``
and then to ``lm_data_indication``,
the entry point to the data link state machine.

"Clean or FEC" in the diagram means a frame decoded without any bits fixed,
or one received with FX.25 or IL2P forward error correction.
Only those are IGated or digipeated.

``app_process_rec_packet`` stops with a frame from the IGate channel
once the client applications and the web interface have it.
A touch tone sequence,
from the DTMF decoder or in a frame whose information starts with ``t``,
goes to the APRStt gateway instead of the IGate and the digipeaters.

EAS skips line decoding and HDLC framing altogether,
and AIS skips the FX.25 and IL2P receivers.
