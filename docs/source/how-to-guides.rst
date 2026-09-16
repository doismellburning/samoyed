How-To Guides
=============

Various recipes for using Samoyed.

Generate sample packet data encoded as audio
--------------------------------------------

.. code::

    $ samoyed-gen_packets --output-file data.wav


Decode packet data from audio
-----------------------------

.. code::

    $ samoyed-atest --bitrate 300 data.wav


Decode frames that fail their CRC check
---------------------------------------

``FIX_BITS`` controls how much effort goes into rescuing a frame whose CRC does
not match, and it sets two separate things:

.. code::

    FIX_BITS n [ APRS | AX25 | NONE ] [ PASSALL ]

* ``n`` is a level of effort, from 0 (the default - consider only correct
  frames) up to 4 (invert two separated bits), each level also doing what the
  levels below it do.  Anything above 1 is not recommended for normal
  operation: it was "an interesting experiment but turned out to be a bad
  idea", and the higher levels are expensive enough to stop you keeping up with
  the audio stream.
* ``PASSALL`` is not a level of effort.  It hands over frames that still fail
  the CRC check once the attempts asked for by ``n`` have been exhausted, so
  what reaches your application is whatever happened to arrive, random garbage
  included.

``samoyed-atest`` takes the same levels as ``--fix-bits``/``-F``, plus one more
value that means the highest level and then ``PASSALL``:

.. code::

    $ samoyed-atest --fix-bits 5 data.wav

Frames that ``PASSALL`` forwarded are reported as ``[PASSALL]``, rather than
naming a bit inversion that worked, and are left out of the frame metrics
described below.


Use the only sound card without configuring it
----------------------------------------------

A machine with one sound card - a USB interface on an otherwise silent
Raspberry Pi, say - needs no ``ADEVICE`` at all.  With nothing configured,
Samoyed looks for the one card that can capture, transmits through that same
card when it can also play, and says which card it settled on:

.. code::

    Automatically selected the only audio device available: USB Audio CODEC: USB Audio (hw:1,0)
    Audio device for both receive and transmit: USB Audio CODEC: USB Audio (hw:1,0)  (channel 0)

``ADEVICE auto`` asks for the same thing explicitly, and reads more clearly in
a configuration file than leaving the line out.

Detection only acts on an unambiguous answer.  It stands aside, leaving the
device where it always was - whatever the sound system offers as its default -
when:

- more than one sound card can serve that direction.  Transmit is the usual
  case: a Pi with a USB interface also has its headphone jack, so the card
  being received on is used for transmit rather than picking between the two;

- the card cannot be opened as configured - it is busy, or does not do mono, or
  does not do the configured sample rate.  The default device is usually a
  plugin or sound server that converts whatever it is given, so it remains the
  better bet;

- the configuration defines more than one audio device.  That configuration is
  choosing devices by hand, and the card detection would find is probably the
  one another ``ADEVICE`` already names.

Anything named in the configuration is used as named, and anything that is not
a sound card - ``stdin``, ``udp:``, a UDP transmit destination - is untouched.
Where only one side is left at the default it is the only side detected, so

.. code::

    ADEVICE udp:7355 auto     # Receive from an SDR, transmit through the only card


Receive without an audio output device
--------------------------------------

A transmit device is optional.  A receive-only station - an IGate, a monitor,
a machine whose sound card can capture but not play - starts normally without
one, reports

.. code::

    No audio output device, so transmitting is not possible.

and then decodes as usual.  That covers a soundcard that captures but cannot
play, a system with no playback device at all, and a single-name ``ADEVICE``
naming an input-only source, where there is no output device to speak of:

.. code::

    ADEVICE stdin     # Or udp:7355, or "-"

Anything such a station would have transmitted - beacons, digipeated frames,
APRStt responses - is discarded before it reaches the transmitter, so give a
receive-only station a configuration to match.  The transmitter is never
keyed: sending samples that go nowhere would put an unmodulated carrier on the
air, and mute the receiver for the duration of a half-duplex transmission, so
PTT stays off and ``-x`` calibration tones are refused outright.

An audio source given on the command line is treated the same way as that
single name, so ``samoyed-direwolf -`` is receive-only unless the
configuration names a transmit device:

.. code::

    $ samoyed-direwolf -c rx.conf -   # Receive-only, whatever rx.conf leaves at the default

Naming a transmit device explicitly is asking for that device, so Samoyed
still stops if it is not there, and a command-line source does not take it
away:

.. code::

    ADEVICE plughw:1,0 plughw:2,0   # Stops if plughw:2,0 cannot be opened
    PAODEVICE Some Sound Card       # Likewise
    ADEVICE - default               # Read standard input, transmit on the default device

An audio *input* device is also still required: there is nothing to do without
one, so Samoyed stops if it cannot be opened.


Run two instances talking to each other via ALSA loopback (Linux)
-----------------------------------------------------------------

Useful for testing and experimentation.

See https://radio.doismellburning.co.uk/projects/direwolf-with-alsa-loopback-devices/ for more detail.

.. code::

    $ sudo modprobe snd_aloop  # Load the loopback soundcard module

    $ cat dw1.conf
    ADEVICE plughw:Loopback,0,1 plughw:Loopback,1,0

    MYCALL Q1TEST-1
    CBEACON delay=0:10 every=0:10 info=0  # Basic heartbeat to confirm connection

    $ cat dw2.conf
    ADEVICE plughw:Loopback,0,0 plughw:Loopback,1,1

    $ samoyed-direwolf --config-file dw1.conf  # Run this in one session / terminal / etc.
    $ samoyed-direwolf --config-file dw2.conf  # Run this in another


Monitor a station with Prometheus / Grafana
--------------------------------------------

Add ``METRICSPORT`` to your config file to expose a Prometheus-format
``/metrics`` endpoint (disabled by default).  The port is yours to choose;
avoid 9090, which is where the Prometheus server itself listens if you run one
on the same host:

.. code::

    METRICSPORT 9099

.. code::

    $ curl http://localhost:9099/metrics

Per-channel series (frame counts, DCD, audio level, queue depth) are created at
startup for every configured radio channel, so a quiet station reports zeroes
rather than nothing at all.

Frame counts describe what the demodulator accepted off the air, so packets
injected on an ICHANNEL, taken from a network TNC, generated by APRStt, or
beaconed back to the receive path are not included, and neither are frames
``PASSALL`` forwards after a failed FCS check.

Note that ``samoyed_corrected_symbols_total`` counts Reed-Solomon symbols
corrected by FX.25 and IL2P only.  Frames recovered by the HDLC bit-fix logic
are counted by ``samoyed_frames_bit_corrected_total`` instead, labelled with the
bit inversion strategy that succeeded - that path reports which technique
worked, not how many bits it had to touch, so the two are not comparable
quantities and are not summed together.

A starter Grafana dashboard is provided at ``conf/grafana-dashboard.json``.

Talk IL2P to v0.4 and v0.6 stations
------------------------------------

IL2P v0.6 mandates 16 Reed-Solomon parity symbols per payload block and marks
the header bit that v0.4 used as its "FEC Level" as reserved.  A v0.6 frame
therefore looks to a v0.4 station like a request for the weaker FEC, and its
payload blocks come out the wrong size.  Nothing in the frame says which
version it is, so the choice is a per-channel configuration setting:

.. code::

    IL2PVERSION 0.6

v0.6 is the default, and is what live IL2P largely is - NinoTNC firmware, QtSM
and MMDVM-TNC all speak it.  The other two settings are for reaching stations
that do not:

``0.4``
    The header bit says which FEC level is in use, on transmit and receive.
    Needed to receive a v0.4 station sending the weaker FEC (Dire Wolf's
    ``-I 0``, or ``IL2PTX 0`` here), at the cost of no longer reading v0.6.

``compat``
    Transmit v0.4, receive v0.6.  With the maximum FEC that ``IL2PTX 1``
    selects by default, a v0.4 frame differs from a v0.6 one only in that
    header bit, which v0.6 stations ignore - so transmissions are readable by
    everyone.  Use this on a channel shared with Dire Wolf stations, which
    implement v0.4 and cannot read our v0.6 transmissions otherwise.

``samoyed-gen_packets`` and ``samoyed-atest`` take the same choice as
``--il2p-version``, for generating and decoding test audio.
