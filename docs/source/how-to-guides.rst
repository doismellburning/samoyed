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
