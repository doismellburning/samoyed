// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wait_for_clear_channel locks the audio output device on success, and it is
// xmit_next's job to release it again.  An empty queue means there is nothing
// to send after all, which is one of the paths where that release used to be
// skipped, leaving the device locked forever and every later transmission on
// it blocked.
func TestXmitNextReleasesAudioOutDevWhenQueueIsEmpty(t *testing.T) {
	var channel = 0

	var xs = new(XmitService)
	xs.fulldup[channel] = true // Skip the channel-busy check and random wait.

	// The transmit queues are package globals shared with every other test
	// here, some of which leave entries behind, so empty this channel's rather
	// than assume they already are.
	for _, prio := range []int{TQ_PRIO_0_HI, TQ_PRIO_1_LO} {
		for transmitQueue.Remove(channel, prio) != nil {
		}
	}

	xs.xmit_next(t.Context(), channel)

	if !xs.audioOutDevMutex[ACHAN2ADEV(channel)].TryLock() {
		t.Fatal("Audio output device is still locked after xmit_next found nothing to send")
	}

	xs.audioOutDevMutex[ACHAN2ADEV(channel)].Unlock()
}

// A channel whose audio device has no output must not transmit at all: keying
// PTT to play samples that go nowhere would put an unmodulated carrier on the
// air, and mute the receiver for the duration of a half-duplex transmission.
// Frames queued for such a channel are thrown away instead.
func TestDiscardUntransmittableEmptiesTheQueue(t *testing.T) {
	var channel = 0

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[channel] = MEDIUM_RADIO

	transmitQueue.Init(audioConfig)

	var xs = new(XmitService)

	transmitQueue.Append(channel, TQ_PRIO_1_LO, newTestPacket(t))
	transmitQueue.Append(channel, TQ_PRIO_0_HI, newTestPacket(t))

	xs.discard_untransmittable(channel)

	assert.Nil(t, transmitQueue.Peek(channel, TQ_PRIO_0_HI))
	assert.Nil(t, transmitQueue.Peek(channel, TQ_PRIO_1_LO))

	// The explanation is printed once, however many frames are discarded.
	assert.True(t, xs.saidCannotTransmit[channel])
}

// The null frame TransmitQueue.LMSeizeRequest queues is a request for a transmission
// opportunity rather than something to send, and send_one_frame answers it
// with a seize confirm.  Discarding it silently would leave a connected mode
// session waiting for a confirmation that never comes, so the discard path
// answers it too.
func TestDiscardUntransmittableAnswersSeizeRequest(t *testing.T) {
	var channel = 0

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[channel] = MEDIUM_RADIO

	transmitQueue.Init(audioConfig)
	dataLinkQueue.Init()

	var xs = new(XmitService)

	transmitQueue.Append(channel, TQ_PRIO_1_LO, ax25_new()) // What TransmitQueue.LMSeizeRequest queues.
	transmitQueue.Append(channel, TQ_PRIO_1_LO, newTestPacket(t))

	xs.discard_untransmittable(channel)

	assert.Nil(t, transmitQueue.Peek(channel, TQ_PRIO_1_LO))

	var confirmed = false

	for item := dataLinkQueue.Remove(); item != nil; item = dataLinkQueue.Remove() {
		if item._type == DLQ_SEIZE_CONFIRM && item._chan == channel {
			confirmed = true
		}
	}

	assert.True(t, confirmed, "Expected a seize confirm for the discarded null frame")
}

// The scheduler itself, not just the discard helper, has to keep a channel
// with no transmit device away from xmit_next: that is what stops PTT being
// keyed for audio that goes nowhere.  Removing the guard in xmit_until_empty
// makes this test transmit instead of discard, and saidCannotTransmit stays
// false.
func TestXmitUntilEmptyDiscardsWithNoTransmitDevice(t *testing.T) {
	var channel = 0

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[channel] = MEDIUM_RADIO

	transmitQueue.Init(audioConfig)

	var xs = new(XmitService)
	xs.audioOutAvailable[ACHAN2ADEV(channel)] = false

	transmitQueue.Append(channel, TQ_PRIO_1_LO, newTestPacket(t))
	transmitQueue.Append(channel, TQ_PRIO_0_HI, newTestPacket(t))

	xs.xmit_until_empty(t.Context(), channel)

	assert.Nil(t, transmitQueue.Peek(channel, TQ_PRIO_0_HI))
	assert.Nil(t, transmitQueue.Peek(channel, TQ_PRIO_1_LO))
	assert.True(t, xs.saidCannotTransmit[channel], "Expected the frames to go down the discard path, not the transmit path")

	// The audio output device is never seized on the way, so nothing is left
	// holding its lock.
	if !xs.audioOutDevMutex[ACHAN2ADEV(channel)].TryLock() {
		t.Fatal("Audio output device was locked by a channel that cannot transmit")
	}

	xs.audioOutDevMutex[ACHAN2ADEV(channel)].Unlock()
}

// A transmit thread with an empty queue is parked in a condition variable
// wait, which nothing but a wake-up can reach.  Cancelling its context has to
// be one of those wake-ups, or the thread runs until the process exits.
func TestXmitThreadStopsWhenCancelled(t *testing.T) {
	var channel = 0

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[channel] = MEDIUM_RADIO

	var ctx, cancel = context.WithCancel(t.Context())

	transmitQueue.Init(audioConfig)

	var xs = new(XmitService)

	var stopped = make(chan struct{})

	go func() {
		defer close(stopped)

		xs.xmit_thread(ctx, channel)
	}()

	// Give it time to get as far as the wait, so that the cancellation below
	// has to wake it rather than just being noticed on the way in.
	time.Sleep(100 * time.Millisecond)

	cancel()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("xmit_thread did not return after its context was cancelled")
	}
}

// A transmit thread whose context is cancelled before it ever waits must not
// then sit down and wait for a broadcast that has already been and gone.
func TestXmitThreadStopsWhenCancelledBeforeStarting(t *testing.T) {
	var channel = 0

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[channel] = MEDIUM_RADIO

	var ctx, cancel = context.WithCancel(t.Context())

	transmitQueue.Init(audioConfig)
	cancel()

	var xs = new(XmitService)

	var stopped = make(chan struct{})

	go func() {
		defer close(stopped)

		xs.xmit_thread(ctx, channel)
	}()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("xmit_thread waited for a wake-up that had already happened")
	}
}

// The transmit scheduler decides what can share a transmission with what, and
// frame_flavor is where that decision starts.
func TestFrameFlavor(t *testing.T) {
	for _, c := range []struct {
		name string
		text string
		want flavor_t
	}{
		{"a new APRS frame", "Q1TEST>Q2TEST:hello", FLAVOR_APRS_NEW},
		{"one being digipeated", "Q1TEST>Q2TEST,Q3TEST*:hello", FLAVOR_APRS_DIGI},
		{"one with an unused digipeater", "Q1TEST>Q2TEST,Q3TEST:hello", FLAVOR_APRS_NEW},
		{"speech", "Q1TEST>SPEECH:hello", FLAVOR_SPEECH},
		{"morse", "Q1TEST>MORSE:hello", FLAVOR_MORSE},
		{"DTMF", "Q1TEST>DTMF:hello", FLAVOR_DTMF},
	} {
		t.Run(c.name, func(t *testing.T) {
			var pp = AX25FromText(c.text, true)
			require.NotNil(t, pp)

			assert.Equal(t, c.want, frame_flavor(pp))
		})
	}

	// Connected mode frames are not APRS at all.
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = "Q1TEST"
	addrs[AX25_SOURCE] = "Q2TEST"

	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_SABM, 0, 0, nil)
	require.NotNil(t, pp)

	assert.Equal(t, FLAVOR_OTHER, frame_flavor(pp))
}

// The priority a frame went out at is shown in the transmit line, so that a
// log says which queue it came from.
func TestPriorityToRune(t *testing.T) {
	assert.Equal(t, 'H', priorityToRune(TQ_PRIO_0_HI))
	assert.Equal(t, 'L', priorityToRune(TQ_PRIO_1_LO))
}

// Timings are worked out in bits, because that is what the modulator counts,
// and reported in milliseconds, because that is what the configuration is in.
func TestBitsAndMilliseconds(t *testing.T) {
	var xs = new(XmitService)
	xs.bits_per_sec[0] = 1200

	assert.Equal(t, 1000, xs.bitsToMS(1200, 0))
	assert.Equal(t, 1200, xs.msToBits(1000, 0))

	// And back again, for a value that divides exactly.
	assert.Equal(t, 300, xs.msToBits(xs.bitsToMS(300, 0), 0))
}

// The transmit line can carry a timestamp, for a log that has to be lined up
// against something else.
func TestTimestampPrefix(t *testing.T) {
	var xs = new(XmitService)
	xs.p_modem = new(audio_s)

	assert.Empty(t, xs.timestampPrefix(), "no format configured means no timestamp")

	xs.p_modem.timestamp_format = "%Y"

	var prefix = xs.timestampPrefix()

	assert.Equal(t, " ", prefix[:1], "the timestamp is separated from the channel")
	assert.Equal(t, time.Now().Format("2006"), prefix[1:])
}

// setupXmitTransmission makes a transmission possible without a radio: the
// tones go to a capture function rather than an audio device, and PTT is set
// up for a channel with no hardware attached to key.
func setupXmitTransmission(t *testing.T) *XmitService {
	t.Helper()

	const channel = 0

	var origAudio, origToneGen, origGenerators, origADev = save_audio_config_p, toneGenCapture, toneGenerators, adev[0]

	t.Cleanup(func() {
		save_audio_config_p, toneGenCapture, toneGenerators, adev[0] = origAudio, origToneGen, origGenerators, origADev

		for p := range TQ_NUM_PRIO {
			for transmitQueue.Remove(channel, p) != nil { //revive:disable-line:empty-block
			}
		}

		dataLinkQueue.Init()
	})

	var audioConfig = new(audio_s)
	audioConfig.adev[0].defined = 1
	audioConfig.adev[0].num_channels = 1
	audioConfig.adev[0].samples_per_sec = 44100
	audioConfig.adev[0].bits_per_sample = 16
	audioConfig.chan_medium[channel] = MEDIUM_RADIO
	audioConfig.achan[channel].modem_type = MODEM_AFSK
	audioConfig.achan[channel].layer2_xmit = LAYER2_AX25
	audioConfig.achan[channel].baud = 1200
	audioConfig.achan[channel].mark_freq = 1200
	audioConfig.achan[channel].space_freq = 2200
	audioConfig.achan[channel].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_NONE

	require.NoError(t, ptt_init(audioConfig))

	transmitQueue.Init(audioConfig)
	dataLinkQueue.Init()

	// Sending a frame serialises it to bits; the capture takes them instead of
	// the modulator.  The device itself still has to be there, though: the
	// flushing and draining reach into it, and morse and DTMF go round the
	// modulator straight to its output buffer.  With no stream attached, what
	// lands in that buffer is discarded rather than played.
	toneGenCapture = func(int, int) {}

	adev[0] = new(adev_s)
	adev[0].outbufSizeInBytes = 4096
	adev[0].outbuf = make([]byte, adev[0].outbufSizeInBytes)

	gen_tone_init(audioConfig, 100, audioDeviceSink{})

	var xs = new(XmitService)
	xs.p_modem = audioConfig
	xs.bits_per_sec[channel] = 1200
	xs.audioOutAvailable[0] = true

	return xs
}

// A frame going out is announced on the console, in the same form a received
// one is shown in, so that a log reads as a conversation.
func TestSendOneFrame(t *testing.T) {
	var xs = setupXmitTransmission(t)

	var pp = AX25FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var bits int

	var output = CaptureOutput(t, func() { bits = xs.send_one_frame(0, TQ_PRIO_1_LO, pp) })

	assert.Positive(t, bits, "nothing was sent")
	assert.Contains(t, output, "[0L] Q1TEST>Q2TEST:hello")
}

// A connected mode frame is not self-explanatory the way an APRS one is, so
// the frame type is spelled out.
func TestSendOneFrameNonAPRS(t *testing.T) {
	var xs = setupXmitTransmission(t)

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = "Q1TEST"
	addrs[AX25_SOURCE] = "Q2TEST"

	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_SABM, 0, 0, nil)
	require.NotNil(t, pp)

	var output = CaptureOutput(t, func() { xs.send_one_frame(0, TQ_PRIO_0_HI, pp) })

	assert.Contains(t, output, "[0H]")
	assert.Contains(t, output, "(SABM")
}

// An XID frame's information field is a set of negotiated parameters, which
// are shown decoded rather than as bytes.
func TestSendOneFrameXID(t *testing.T) {
	var xs = setupXmitTransmission(t)

	var param xid_param_s

	var info = xid_encode(&param, cr_cmd)

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = "Q1TEST"
	addrs[AX25_SOURCE] = "Q2TEST"

	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_XID, 0, 0, info)
	require.NotNil(t, pp)

	var output = CaptureOutput(t, func() { xs.send_one_frame(0, TQ_PRIO_0_HI, pp) })

	assert.Contains(t, output, "(XID")
	assert.Contains(t, output, "Half-Duplex", "the XID parameters should be shown decoded")
}

// The null frame is a request for a transmission opportunity rather than
// something to send: nothing goes on the air, and the data link state machine
// is told the opportunity has arrived.
func TestSendOneFrameNullFrame(t *testing.T) {
	var xs = setupXmitTransmission(t)

	assert.Equal(t, 0, xs.send_one_frame(0, TQ_PRIO_1_LO, ax25_new()))

	var confirmed = false

	for item := dataLinkQueue.Remove(); item != nil; item = dataLinkQueue.Remove() {
		if item._type == DLQ_SEIZE_CONFIRM {
			confirmed = true
		}
	}

	assert.True(t, confirmed, "the transmission opportunity was not confirmed")
}

// "-x" sends deliberately corrupted frames, for testing a receiver.  At 100
// per cent every frame is corrupted, and says so.
func TestSendOneFrameDeliberateBadFCS(t *testing.T) {
	var xs = setupXmitTransmission(t)

	xs.p_modem.xmit_error_rate = 100

	var pp = AX25FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var output = CaptureOutput(t, func() { xs.send_one_frame(0, TQ_PRIO_1_LO, pp) })

	assert.Contains(t, output, "Intentionally sending invalid CRC")
}

// "-d p" adds a hex dump of what went out, for comparing against what a
// receiver made of it.
func TestSendOneFrameDebugHexDump(t *testing.T) {
	var xs = setupXmitTransmission(t)

	xs.debugXmitPacket = true

	var pp = AX25FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	var output = CaptureOutput(t, func() { xs.send_one_frame(0, TQ_PRIO_1_LO, pp) })

	assert.Contains(t, output, "------")
}

// A transmission is PTT on, a preamble, the frames, a postamble, PTT off - and
// more than one frame can share it when the queue has more waiting.
func TestXmitAX25FramesBundles(t *testing.T) {
	var xs = setupXmitTransmission(t)

	var first = AX25FromText("Q1TEST>Q2TEST:first", true)
	require.NotNil(t, first)

	var second = AX25FromText("Q1TEST>Q2TEST:second", true)
	require.NotNil(t, second)

	transmitQueue.Append(0, TQ_PRIO_1_LO, second)

	var output = CaptureOutput(t, func() { xs.xmit_ax25_frames(0, TQ_PRIO_1_LO, first, 7) })

	assert.Contains(t, output, ":first")
	assert.Contains(t, output, ":second", "the queued frame should have shared the transmission")
	assert.Nil(t, transmitQueue.Peek(0, TQ_PRIO_1_LO), "the bundled frame should have left the queue")
}

// A digipeated APRS frame gets a transmission to itself: bundling it behind
// something else is what makes a digipeater step on the frames after it.
func TestXmitAX25FramesDoesNotBundleDigipeated(t *testing.T) {
	var xs = setupXmitTransmission(t)

	var first = AX25FromText("Q1TEST>Q2TEST:first", true)
	require.NotNil(t, first)

	var digipeated = AX25FromText("Q1TEST>Q2TEST,Q3TEST*:repeated", true)
	require.NotNil(t, digipeated)

	transmitQueue.Append(0, TQ_PRIO_1_LO, digipeated)

	var output = CaptureOutput(t, func() { xs.xmit_ax25_frames(0, TQ_PRIO_1_LO, first, 7) })

	assert.Contains(t, output, ":first")
	assert.NotContains(t, output, ":repeated")
	assert.NotNil(t, transmitQueue.Peek(0, TQ_PRIO_1_LO), "the digipeated frame should still be waiting its own turn")
}

// A limit of one frame per transmission is what the bundling rules ask for in
// some cases, and it is respected whatever is waiting.
func TestXmitAX25FramesRespectsMaxBundle(t *testing.T) {
	var xs = setupXmitTransmission(t)

	var first = AX25FromText("Q1TEST>Q2TEST:first", true)
	require.NotNil(t, first)

	var second = AX25FromText("Q1TEST>Q2TEST:second", true)
	require.NotNil(t, second)

	transmitQueue.Append(0, TQ_PRIO_1_LO, second)

	var output = CaptureOutput(t, func() { xs.xmit_ax25_frames(0, TQ_PRIO_1_LO, first, 1) })

	assert.NotContains(t, output, ":second")
	assert.NotNil(t, transmitQueue.Peek(0, TQ_PRIO_1_LO))
}

// A frame waiting at high priority is taken before one at low priority, even
// once the transmission is already under way.
func TestXmitAX25FramesTakesHighPriorityFirst(t *testing.T) {
	var xs = setupXmitTransmission(t)

	var first = AX25FromText("Q1TEST>Q2TEST:first", true)
	require.NotNil(t, first)

	var low = AX25FromText("Q1TEST>Q2TEST:low", true)
	require.NotNil(t, low)

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = "Q1TEST"
	addrs[AX25_SOURCE] = "Q2TEST"

	var high = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_SABM, 0, 0, nil)
	require.NotNil(t, high)

	transmitQueue.Append(0, TQ_PRIO_1_LO, low)
	transmitQueue.Append(0, TQ_PRIO_0_HI, high)

	CaptureOutput(t, func() { xs.xmit_ax25_frames(0, TQ_PRIO_1_LO, first, 2) })

	assert.Nil(t, transmitQueue.Peek(0, TQ_PRIO_0_HI), "the high priority frame should have gone first")
	assert.NotNil(t, transmitQueue.Peek(0, TQ_PRIO_1_LO), "and used up the bundle, leaving the low priority one")
}

// A frame addressed to SPEECH is spoken rather than modulated, by a script the
// configuration names.
func TestXmitSpeech(t *testing.T) {
	var xs = setupXmitTransmission(t)

	var said = filepath.Join(t.TempDir(), "said")

	var script = filepath.Join(t.TempDir(), "speak")
	//nolint:gosec // Executable because the thing under test runs it.
	require.NoError(t, os.WriteFile(script,
		[]byte("#!/bin/sh\nprintf '%s' \"$2\" > "+said+"\n"), 0o700))

	xs.p_modem.tts_script = script

	var pp = AX25FromText("Q1TEST>SPEECH:Hello there", true)
	require.NotNil(t, pp)

	var output = CaptureOutput(t, func() { xs.xmit_speech(t.Context(), 0, pp) })

	assert.Contains(t, output, `[0.speech] "Hello there"`)

	var spoken, readErr = os.ReadFile(said) //nolint:gosec // A path this test made up.
	require.NoError(t, readErr)
	assert.Equal(t, "Hello there", string(spoken), "the script should have been given what to say")
}

// Without a script there is nothing to say it with, and keying the transmitter
// to broadcast silence would be worse than saying so.
func TestXmitSpeechWithoutAScript(t *testing.T) {
	var xs = setupXmitTransmission(t)

	var pp = AX25FromText("Q1TEST>SPEECH:Hello there", true)
	require.NotNil(t, pp)

	var output = CaptureOutput(t, func() { xs.xmit_speech(t.Context(), 0, pp) })

	assert.Contains(t, output, "Text-to-speech script has not been configured")
}

// A script that cannot be run is reported, along with where we looked for it,
// because getting this wrong is a configuration mistake rather than a fault.
func TestXmitSpeakItScriptFails(t *testing.T) {
	var err error

	var output = CaptureOutput(t, func() {
		err = xmit_speak_it(t.Context(), filepath.Join(t.TempDir(), "no-such-script"), 0, "hello")
	})

	require.Error(t, err)
	assert.Contains(t, output, "Failed to run text-to-speech script")
	assert.Contains(t, output, "PATH = ")
}

// An APRS frame being digipeated gets a transmission to itself, which is what
// keeps a digipeater from stepping on the frames bundled behind it.
func TestXmitNextDoesNotBundleBehindADigipeatedFrame(t *testing.T) {
	var xs = setupXmitTransmission(t)

	xs.fulldup[0] = true // Skip the channel-busy check and random wait.

	var digipeated = AX25FromText("Q1TEST>Q2TEST,Q3TEST*:repeated", true)
	require.NotNil(t, digipeated)

	var other = AX25FromText("Q1TEST>Q2TEST:other", true)
	require.NotNil(t, other)

	transmitQueue.Append(0, TQ_PRIO_0_HI, digipeated)
	transmitQueue.Append(0, TQ_PRIO_1_LO, other)

	var output = CaptureOutput(t, func() { xs.xmit_next(t.Context(), 0) })

	assert.Contains(t, output, ":repeated")
	assert.NotContains(t, output, ":other")
	assert.NotNil(t, transmitQueue.Peek(0, TQ_PRIO_1_LO), "the other frame should still be waiting its own turn")
}

// Anything else can share a transmission, so emptying the queue takes one
// turn on the air rather than one per frame.
func TestXmitNextBundlesOrdinaryFrames(t *testing.T) {
	var xs = setupXmitTransmission(t)

	xs.fulldup[0] = true

	for _, text := range []string{"Q1TEST>Q2TEST:first", "Q1TEST>Q2TEST:second"} {
		var pp = AX25FromText(text, true)
		require.NotNil(t, pp)

		transmitQueue.Append(0, TQ_PRIO_1_LO, pp)
	}

	var output = CaptureOutput(t, func() { xs.xmit_next(t.Context(), 0) })

	assert.Contains(t, output, ":first")
	assert.Contains(t, output, ":second")
	assert.Nil(t, transmitQueue.Peek(0, TQ_PRIO_1_LO))
}

// The audio output device is locked for the duration of a transmission, so
// that two channels sharing a stereo device cannot talk over each other, and
// released however the transmission ends.
func TestXmitNextReleasesAudioOutDev(t *testing.T) {
	var xs = setupXmitTransmission(t)

	xs.fulldup[0] = true

	var pp = AX25FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	transmitQueue.Append(0, TQ_PRIO_1_LO, pp)

	CaptureOutput(t, func() { xs.xmit_next(t.Context(), 0) })

	require.True(t, xs.audioOutDevMutex[ACHAN2ADEV(0)].TryLock(),
		"the audio output device is still locked after the transmission")

	xs.audioOutDevMutex[ACHAN2ADEV(0)].Unlock()
}

// A frame addressed to MORSE is keyed rather than modulated, for a repeater
// identification or a beacon somebody might be listening to by ear.
func TestXmitMorse(t *testing.T) {
	var xs = setupXmitTransmission(t)

	var pp = AX25FromText("Q1TEST>MORSE:HI", true)
	require.NotNil(t, pp)

	var output = CaptureOutput(t, func() { xs.xmit_morse(0, pp, MORSE_DEFAULT_WPM) })

	assert.Contains(t, output, `[0.morse] "HI"`)
}

// A frame addressed to DTMF is sent as touch tones, which is how APRStt
// answers back to a user pressing buttons.
func TestXmitDTMF(t *testing.T) {
	var xs = setupXmitTransmission(t)

	var pp = AX25FromText("Q1TEST>DTMF:12", true)
	require.NotNil(t, pp)

	var output = CaptureOutput(t, func() { xs.xmit_dtmf(0, pp, 10) })

	assert.Contains(t, output, `[0.dtmf] "12"`)
}

// timeXmitNext sends one queued frame and says how long the transmission
// took.
//
// xmit_morse and xmit_dtmf both hold PTT until the sound they asked for is
// over, so the elapsed time is the length the generator reported - which is
// what the speed actually changes.
func timeXmitNext(t *testing.T, xs *XmitService, text string) (time.Duration, string) {
	t.Helper()

	var pp = AX25FromText(text, true)
	require.NotNil(t, pp)

	transmitQueue.Append(0, TQ_PRIO_1_LO, pp)

	var started = time.Now()

	var output = CaptureOutput(t, func() { xs.xmit_next(t.Context(), 0) })

	return time.Since(started), output
}

// The destination's SSID sets the speed: for morse it is half the words per
// minute, and xmit_next is where that is worked out.
func TestXmitNextMorseSpeedFromSSID(t *testing.T) {
	var xs = setupXmitTransmission(t)

	xs.fulldup[0] = true

	// What the two speeds would take.  morse_send generates the sound and
	// says how long it is, without waiting for it, so asking it costs
	// nothing - and comparing against its own answer means this does not
	// depend on how morse timing is worked out.
	const message = "OS" // Long enough for the speed to dominate the padding.

	var atDefault = morse_send(0, message, MORSE_DEFAULT_WPM, 300, 250)
	var atDouble = morse_send(0, message, MORSE_DEFAULT_WPM*2, 300, 250)

	require.Less(t, atDouble, atDefault/2+atDefault/4,
		"the two speeds are too close together for this to show anything")

	var elapsed, output = timeXmitNext(t, xs, "Q1TEST>MORSE-"+strconv.Itoa(MORSE_DEFAULT_WPM)+":"+message)

	assert.Contains(t, output, `[0.morse] "`+message+`"`)

	// The bounds are one-sided, because the error can only go one way: a
	// transmission can overrun, on a busy machine, but it cannot finish
	// before the sound it is sending is over.  SSID 10 means 20 wpm.
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(atDouble)-100,
		"the transmission finished sooner than %d wpm could have", MORSE_DEFAULT_WPM*2)
	assert.Less(t, elapsed.Milliseconds(), int64(atDefault)-250,
		"the SSID was ignored and the default speed used")
}

// For DTMF the SSID is button presses per second, bounded at both ends: zero
// means "use the default" and anything over ten is faster than a receiver can
// follow.
func TestXmitNextDTMFSpeedFromSSID(t *testing.T) {
	var xs = setupXmitTransmission(t)

	xs.fulldup[0] = true

	// Long enough that the speed, rather than the fixed padding either side,
	// decides how long the transmission takes, and that the speeds are far
	// enough apart to tell from a wall clock on a busy machine.
	const message = "1234567890"

	const (
		defaultSpeed = 5  // What no SSID means.
		maximumSpeed = 10 // What anything faster is held down to.
		askedFor     = 15 // More than we will go.
	)

	// dtmf_send generates the sound and says how long it is, without waiting
	// for it, so asking it costs nothing - and comparing against its own
	// answers means this does not depend on how DTMF timing is worked out.
	var atDefault = dtmf_send(0, message, defaultSpeed, 300, 250)
	var atMaximum = dtmf_send(0, message, maximumSpeed, 300, 250)
	var atAskedFor = dtmf_send(0, message, askedFor, 300, 250)

	require.Less(t, atMaximum, atDefault-400,
		"the default and maximum speeds are too close together for this to show anything")
	require.Less(t, atAskedFor, atMaximum-200,
		"the maximum and the asked-for speed are too close together for this to show anything")

	// No SSID: the default rather than zero presses per second.
	var elapsed, output = timeXmitNext(t, xs, "Q1TEST>DTMF:"+message)

	assert.Contains(t, output, `[0.dtmf] "`+message+`"`)

	// One-sided, as above: a transmission can overrun but cannot finish
	// before the sound it is sending is over.
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(atDefault)-100,
		"the transmission finished sooner than the default speed could have")

	// An SSID faster than we will go is held down to the maximum rather than
	// taken at face value.
	elapsed, _ = timeXmitNext(t, xs, "Q1TEST>DTMF-"+strconv.Itoa(askedFor)+":"+message)

	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(atMaximum)-100,
		"the transmission finished sooner than the maximum speed could have, so the SSID was taken at face value")
	assert.Less(t, elapsed.Milliseconds(), int64(atDefault)-400,
		"the SSID was ignored and the default speed used")
}
