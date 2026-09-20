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


Check a config file without starting up
---------------------------------------

Finding out whether ``direwolf.conf`` is right by starting ``samoyed-direwolf``
means having the audio devices and ports it wants, which a build machine or a
headless box being edited over SSH generally does not.  ``--config-check``
reads the file and stops there:

.. code::

    $ samoyed-direwolf -c /etc/direwolf.conf --config-check

    Reading config file /etc/direwolf.conf
    Line 12: Invalid port number "eight thousand" for AGWPORT command.
    Config file: Unrecognized command 'MYCAL' on line 17.

    Configuration file /etc/direwolf.conf: 2 errors, 0 warnings.

Every problem in the file is reported rather than just the first, so one run
tells you everything there is to fix.  The exit status is 1 if any of them was
an error and 0 otherwise, which is what makes it useful in CI, in a git hook,
or in a systemd unit:

.. code::

    [Service]
    ExecStartPre=/usr/bin/samoyed-direwolf -c /etc/direwolf.conf --config-check
    ExecStart=/usr/bin/samoyed-direwolf -c /etc/direwolf.conf

Warnings are counted separately and do not fail the check.  They are advice
about a directive that was obeyed all the same - ``TXDELAY 3`` is accepted, and
warned about, because it is too short for most radios to key up in - so a
configuration that works today still passes its own check.  A directive that
was *not* obeyed as written, including one that fell back to a default, is an
error.

This checks the configuration file, not the command line: options such as
``-r`` or ``-B`` are not applied, so what you are checking is what the file
says.

Stop it without leaving the rig keyed
-------------------------------------

``samoyed-direwolf`` shuts down on SIGINT and SIGTERM: it closes the packet
log, puts the GPS and waypoint feeds down, and - the part that matters on the
air - unkeys every PTT it holds.  CM108 and hamlib PTT are state in the device
rather than in the process, so a stop that skips this can leave the transmitter
keyed with nothing left running to unkey it.

SIGTERM is what a supervisor sends, so ``systemctl stop``, a container runtime
and a plain ``kill`` all stop it this way.  Ctrl-C at a terminal sends SIGINT
instead, and takes the same path:

.. code::

    systemctl stop samoyed-direwolf

Shutting down takes about a second, to give everything it started a moment to
put its own resources down.  That is well inside systemd's default
``TimeoutStopSec`` of 90 seconds, and a second signal arriving during it skips
the wait and ends the process on the spot.

Try a packet filter out before putting it in the config file
------------------------------------------------------------

``IGFILTER``, ``FILTER`` and ``CFILTER`` are easy to get subtly wrong, and the
usual way to find out what one does is to run the whole TNC and watch what
disappears.  ``samoyed-pftest`` runs the same filter engine over packets read
from stdin - the monitoring format ``samoyed-decode_aprs`` takes - and says
``PASS`` or ``DROP`` for each:

.. code::

    $ samoyed-pftest 't/m & ! d/WIDE*' < packets.txt
    DROP	Q1TEST>APDW17:!4237.14NS07120.83W#PHG7130Chelmsford MA
    PASS	Q1TEST>APDW17::Q2TEST   :Hello

Blank lines and lines beginning with ``#`` are passed through untouched, so a
file of packets can carry its own commentary.  A line that cannot be parsed is
answered with ``ERROR``, and makes the exit status non-zero.  Lines copied
from an APRS-IS feed are fine as they are: the lower case ``qAR``/``qAS``
q-construct in the path draws a remark rather than a rejection, as it does in
``samoyed-decode_aprs``.

``--validate`` checks the syntax and stops there, without reading any packets,
which is what you want from a script or a pre-commit check:

.. code::

    $ samoyed-pftest --validate 't/m & ( t/w'
    Invalid filter: filter[0,0]: t/m & ( t/w
                            ^
    Expected ")" here.

``-v`` asks the filter engine to explain itself, up to three times for more
detail: once for the final result, twice for each individual filter
specification, three times for the logical operators as well.

.. code::

    $ echo 'Q1TEST>APDW17::Q2TEST   :Hello' | samoyed-pftest -vv 'b/Q2* | t/m'
       b/Q2* returns FALSE for Q1TEST
       t/m returns TRUE for : data type indicator
     Packet filter for APRS digipeater from radio channel 0 to 0 returns TRUE
    PASS	Q1TEST>APDW17::Q2TEST   :Hello

``--connected-mode`` selects the smaller grammar that ``CFILTER`` uses, and
``--from-channel``/``--to-channel`` set the channel numbers that appear in
error messages and verbose output - ``16`` there means the IGate rather than a
radio channel.

This is the filter engine on its own rather than the whole TNC, so an ``i``
filter, which gates a message only to an addressee heard recently, always
finds an empty "heard" database and so passes nothing, and takes its default
maximum digipeater hop count from an ``IGTXVIA`` that was never configured.


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

Put a password on the AGW port
--------------------------------------------

The "AGW TCPIP Socket Interface" has no authentication of its own, so anything
that can reach port 8000 can transmit through your radio.  Binding it to
localhost or firewalling it off remains the strongest protection, but where the
port has to be reachable, ``AGWLOGIN`` requires a user name and password before
anything else a client asks for is honoured:

.. code::

    AGWLOGIN Q1TEST "correct horse battery staple"

Quote either value if it contains spaces.  To change the password, edit the line
and restart.  Without an ``AGWLOGIN`` line nothing changes: clients connect and
work without logging in, as they always have.

Repeat the directive to accept more than one set of credentials, as AGWPE does,
so that each client can have its own and one of them can be withdrawn without
disturbing the rest:

.. code::

    AGWLOGIN Q1TEST "correct horse battery staple"
    AGWLOGIN Q2TEST "trombone vs mahogany"

A client may use any one of them; the user name and password are matched as a
pair, so one client's password does not unlock another client's user name.

Clients connecting from the machine Samoyed is running on are exempt and never
have to log in, matching AGWPE, whose documentation says a login "should not
bother applications running on the same machine".  Be aware of what that means
on a shared machine: anyone with a shell account on it can use the AGW port
regardless of ``AGWLOGIN``.  If that is your situation, the login is not the
control you want - restrict the port itself.

A client authenticates with the protocol's "Application Login" (``'P'``) frame -
in `pyham_pe <https://github.com/mfncooper/pyham_pe>`__, for instance,
``login(userid, password)``.  Note that the protocol has no reply to it, so a
client learns that it got the credentials wrong only by having its subsequent
commands ignored; the failure is logged at this end.  Not every client can send
the frame at all - Xastir and QtSoundModem, for example, cannot - so check yours
before turning this on.

The credentials cross the network in the clear, exactly as the AGW protocol
specifies them.  Treat this as a way to keep casual traffic off the port, not as
protection against someone who can watch it.

Decode a captured KISS byte stream
-----------------------------------

When a KISS client application misbehaves, the question is usually what was
actually on the wire.  ``samoyed-kissdump`` answers it offline: give it the
bytes and it describes each frame.

.. code::

    $ samoyed-kissdump < capture.bin

Bytes quoted in a bug report are usually hexadecimal rather than raw, so
``--hex`` reads them that way.  Whitespace is ignored, so ``c0 00 82`` and
``c00082`` are the same thing and it does not matter how the digits are laid out
across lines; anything that is not a hexadecimal digit is an error, so the
offsets and ASCII column of a hex dump have to be cut away first rather than be
read as data.

.. code::

    $ samoyed-kissdump --hex < capture.txt

For each frame it undoes the KISS framing, shows the command byte and port
number, decodes the AX.25 header (addresses, digipeater path with the H bits,
control and PID), and hands the information field to the APRS decoder where the
frame is APRS.

Being strict about malformed input is the point, since that is what is being
chased: a frame the capture never terminates, an escape sequence that is
neither ``TFEND`` nor ``TFESC``, a data frame too short to hold an AX.25
header, and an address field whose end-of-address bit is in the wrong place are
all reported rather than skipped.  The exit status is non-zero if anything was
wrong, so it can be used as a check.

Accept incoming connections (connected mode)
---------------------------------------------

Setting ``MYCALL`` does not make a station connectable.  ``MYCALL`` is the APRS
identity - it is what beacons and digipeated frames are sent as - and the
connected-mode link layer does not consult it at all.  A station with nothing
but ``MYCALL`` configured ignores incoming connect requests, so the calling
station retries until it gives up and reports something like "retry count
exceeded", with nothing on this end to explain it.

The link layer answers a SABM only for a callsign an AGW client has registered
over the AGW port, with the protocol's ``'X'`` "Register CallSign" command.
That is deliberate: a callsign nobody has claimed is not ours to answer for,
and several TNCs can share a frequency, so replying - even with a DM - to a
connect request addressed to another station would be wrong.  The connect
request is now logged at ``Info`` when it happens:

.. code::

    level=info msg="Ignoring connect request - no client has registered this callsign" channel=0 destination=Q1TEST source=Q2TEST

So to be connectable, run a client that registers the callsign.
``samoyed-appserver`` is the worked example - a small application server that
answers connections and responds to commands:

.. code::

    $ samoyed-appserver Q1TEST

It attaches to the AGW port (``--hostname`` and ``--port``, defaulting to
``localhost`` and ``8000``, so ``AGWPORT`` must not be disabled), asks the TNC
what radio channels it has, and registers the callsign on each of them.  From
then on a SABM addressed to ``Q1TEST`` on any channel gets a UA, and the
connected station is talking to the appserver rather than to Samoyed itself.
It is meant as a starting point for your own application; any AGW client that
sends ``'X'`` will do, including ones written against `pyham_pe
<https://github.com/mfncooper/pyham_pe>`__.

What the connected station gets is a greeting and a handful of commands:

.. code::

    Welcome!  Type ? for list of commands or HELP <command> for details.
    ?
    Commands:
      BYE                   Disconnect.
      HELP <command>        Describe one command.
      TEST [count [length]] Measure throughput.
      WHO                   List the stations connected now.
    Type HELP <command> for details.

``WHO`` lists the stations connected to the server, the channel each came in
on and when it connected.  ``TEST`` sends frames back and reports how long
they took and how close that came to the channel's bit rate.  ``BYE``
disconnects, once everything queued for the station has been acknowledged.
It is a demonstration rather than a BBS, and the commands are there to be
replaced by your own.

A KISS client is a different matter entirely.  Linux AX.25, and anything else
attached over KISS, runs its own link layer: it sees the raw frames and sends
its own UA, so connected mode there is configured in that software and never
reaches the path described above.  Registering a callsign over the AGW port
has no bearing on it.
