// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRecvTestAudioConfig describes a single 16 bit audio device carrying
// numChannels 1200 baud AFSK channels - the shape recv_adev_thread walks when
// it reads a device.
func newRecvTestAudioConfig(numChannels int) *audio_s {
	var audioConfig = new(audio_s)

	audioConfig.adev[0].defined = 1
	audioConfig.adev[0].num_channels = numChannels
	audioConfig.adev[0].bits_per_sample = 16
	audioConfig.adev[0].samples_per_sec = 44100

	for c := range numChannels {
		audioConfig.chan_medium[c] = MEDIUM_RADIO
		audioConfig.achan[c].modem_type = MODEM_AFSK
		audioConfig.achan[c].baud = 1200
		audioConfig.achan[c].mark_freq = 1200
		audioConfig.achan[c].space_freq = 2200
		audioConfig.achan[c].dtmf_decode = DTMF_DECODE_OFF
	}

	return audioConfig
}

// setupRecvTest initialises the demodulators for audioConfig, and returns the
// SampleSource that hands the receive thread the given samples.  The real one
// asserts on a device that was never opened, so a test brings its own.
func setupRecvTest(t *testing.T, audioConfig *audio_s, samples []byte) *readerSampleSource {
	t.Helper()

	var origAudioConfig, origPA = save_audio_config_p, save_pa

	t.Cleanup(func() {
		save_audio_config_p, save_pa = origAudioConfig, origPA
	})

	multi_modem_init(audioConfig, new(radioSink))

	return newReaderSampleSource(bytes.NewReader(samples), int32(len(samples)))
}

// silence16 is nbytes of 16 bit samples at zero.
func silence16(nbytes int) []byte {
	return make([]byte, nbytes)
}

// endlessAudio is an audio source that never runs out, so a reader of it stops
// only when it is told to.
type endlessAudio struct{}

func (endlessAudio) Read(p []byte) (int, error) {
	clear(p)

	return len(p), nil
}

// samples16 packs samples as the little-endian 16 bit values demod_get_sample
// reads.
func samples16(samples []int16) []byte {
	var buf bytes.Buffer

	for _, s := range samples {
		binary.Write(&buf, binary.LittleEndian, s) //nolint:errcheck // Writing to a bytes.Buffer cannot fail.
	}

	return buf.Bytes()
}

// An audio device that stops delivering samples is the end of the line for
// receiving, so the number of the device that failed has to reach whoever
// started it.
func TestRecvInitReportsTheDeviceWhoseInputFailed(t *testing.T) {
	var audioConfig = newRecvTestAudioConfig(1)

	var src = setupRecvTest(t, audioConfig, silence16(2000))

	var failed = recv_init(t.Context(), audioConfig, src)

	select {
	case a := <-failed:
		assert.Equal(t, 0, a, "the failure should be reported against the device that has it")
	case <-time.After(10 * time.Second):
		t.Fatal("no failure reported after the audio ran out")
	}
}

// Only devices the configuration defines get a thread; anything else would be
// reading a device that was never opened.
func TestRecvInitStartsNothingForAnUndefinedDevice(t *testing.T) {
	var audioConfig = newRecvTestAudioConfig(1)
	audioConfig.adev[0].defined = 0

	var src = setupRecvTest(t, audioConfig, silence16(2000))

	var failed = recv_init(t.Context(), audioConfig, src)

	select {
	case a := <-failed:
		t.Fatalf("device %d was read although it is not defined", a)
	case <-time.After(100 * time.Millisecond):
	}
}

// Shutting down is not an audio failure, so a cancelled thread finishes
// without reporting one.
func TestRecvAdevThreadStopsWhenCancelledWithoutReportingAFailure(t *testing.T) {
	var audioConfig = newRecvTestAudioConfig(1)

	setupRecvTest(t, audioConfig, nil)
	// Audio that never ends, so cancellation is the only way out.
	var src = newReaderSampleSource(endlessAudio{}, math.MaxInt32)

	save_pa = audioConfig

	var ctx, cancel = context.WithCancel(t.Context())
	defer cancel()

	var failed = make(chan int, MAX_ADEVS)
	var done = make(chan struct{})

	go func() {
		recv_adev_thread(ctx, 0, failed, src)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the receive thread did not finish after its context was cancelled")
	}

	assert.Empty(t, failed, "a cancelled thread should not report an audio failure")
}

// A stereo device carries two radio channels, and each one has to get its own
// side of the audio rather than both getting the same samples.
func TestRecvAdevThreadFeedsEachChannelItsOwnSideOfTheAudio(t *testing.T) {
	var audioConfig = newRecvTestAudioConfig(2)

	// Left and right differ in sign, so the running average of each channel
	// says which side reached it.
	var samples []int16
	for range 4410 {
		samples = append(samples, 8000, -8000)
	}

	var src = setupRecvTest(t, audioConfig, samples16(samples))

	dc_average[0] = 0
	dc_average[1] = 0

	var failed = recv_init(t.Context(), audioConfig, src)

	select {
	case <-failed:
	case <-time.After(10 * time.Second):
		t.Fatal("no failure reported after the audio ran out")
	}

	assert.Positive(t, dc_average[0], "channel 0 should have been fed the left samples")
	assert.Negative(t, dc_average[1], "channel 1 should have been fed the right samples")
}

// Touch tones are decoded only where the APRStt gateway is configured for the
// channel.
func TestRecvAdevThreadDecodesTouchTonesWhenConfigured(t *testing.T) {
	var audioConfig = newRecvTestAudioConfig(1)
	audioConfig.achan[0].dtmf_decode = DTMF_DECODE_ON

	var src = setupRecvTest(t, audioConfig, dtmfSamples(t, '1', 250, audioConfig.adev[0].samples_per_sec))

	var origGateway = ttGateway

	t.Cleanup(func() { ttGateway = origGateway })

	ttGateway = NewTTGateway(new(tt_config_s), 0)

	var failed = recv_init(t.Context(), audioConfig, src)

	select {
	case <-failed:
	case <-time.After(10 * time.Second):
		t.Fatal("no failure reported after the audio ran out")
	}

	assert.Equal(t, "1", ttGateway.msgStr[0], "the button press should have reached the APRStt gateway")
}

// And not otherwise: DTMF decoding off means nothing reaches the gateway, no
// matter what is on the air.
func TestRecvAdevThreadIgnoresTouchTonesWhenNotConfigured(t *testing.T) {
	var audioConfig = newRecvTestAudioConfig(1)
	audioConfig.achan[0].dtmf_decode = DTMF_DECODE_OFF

	var src = setupRecvTest(t, audioConfig, dtmfSamples(t, '1', 250, audioConfig.adev[0].samples_per_sec))

	var origGateway = ttGateway

	t.Cleanup(func() { ttGateway = origGateway })

	ttGateway = NewTTGateway(new(tt_config_s), 0)

	var failed = recv_init(t.Context(), audioConfig, src)

	select {
	case <-failed:
	case <-time.After(10 * time.Second):
		t.Fatal("no failure reported after the audio ran out")
	}

	assert.Empty(t, ttGateway.msgStr[0], "nothing should reach the gateway with DTMF decoding off")
}

// dtmfSamples is ms milliseconds of the tone pair for a button, as 16 bit
// samples.
func dtmfSamples(t *testing.T, button rune, ms int, samplesPerSec int) []byte {
	t.Helper()

	var tones = map[rune][2]float64{
		'1': {697, 1209},
		'2': {697, 1336},
		'3': {697, 1477},
	}

	var pair, ok = tones[button]
	require.True(t, ok, "no tone pair for %c", button)

	var samples []int16
	var phaseA, phaseB float64

	for range (ms * samplesPerSec) / 1000 {
		// The decoder is insensitive to amplitude, so anything comfortably
		// clear of the noise floor will do.
		samples = append(samples, int16((math.Sin(phaseA)+math.Sin(phaseB))*8000))

		phaseA += 2.0 * math.Pi * pair[0] / float64(samplesPerSec)
		phaseB += 2.0 * math.Pi * pair[1] / float64(samplesPerSec)
	}

	return samples16(samples)
}

// setupRecvProcessTest initialises what recv_process dispatches into: the
// transmit queue, the link state machines, and an empty received data queue.
// Nothing drains the transmit queue, so what the link layer decides to send
// stays there to be counted.
func setupRecvProcessTest(t *testing.T, frack int) {
	t.Helper()

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[0] = MEDIUM_RADIO
	// A received frame is taken to have arrived on the IGate virtual
	// channel, so that printing it and passing it to the client
	// applications is as far as it goes: digipeating and IGating it would
	// need configuration that has nothing to do with the receive thread.
	audioConfig.igate_vchannel = 0

	var origAudioConfig, origLogger, origMheard = audio_config, packetLogger, mheardDB

	t.Cleanup(func() {
		audio_config, packetLogger, mheardDB = origAudioConfig, origLogger, origMheard
	})

	audio_config = audioConfig
	// A received frame is logged and remembered on its way through, so
	// both need to be there even with no log file to write to.
	packetLogger = NewPacketLogger(false, "")
	mheardDB = NewMHeardDB(0)

	usePTT(t, audioConfig)
	transmitQueue.Init(audioConfig)

	var miscConfig = new(misc_config_s)
	miscConfig.paclen = AX25_N1_PACLEN_DEFAULT
	miscConfig.retry = AX25_N2_RETRY_DEFAULT
	miscConfig.frack = frack
	miscConfig.maxframe_basic = AX25_K_MAXFRAME_BASIC_DEFAULT
	miscConfig.maxframe_extended = AX25_K_MAXFRAME_EXTENDED_DEFAULT

	ax25_link_init(miscConfig, 1)

	// A received frame goes out to the attached client applications; with
	// no KISS TCP ports configured there are none, but the service still
	// has to be there to say so.
	var origKissNetSvc = kissNetSvc

	t.Cleanup(func() { kissNetSvc = origKissNetSvc })

	kissNetSvc = NewKissNetService(t.Context(), miscConfig)

	ax25Link.listHead = nil
	ax25Link.regCallsignList = nil

	dataLinkQueue.Init()

	// Leave nothing behind for whatever test runs next: a link still
	// retrying its connection, or the frames it queued, would turn up
	// there.  This runs last, once recv_process has stopped.
	t.Cleanup(func() {
		ax25Link.listHead = nil
		ax25Link.regCallsignList = nil

		for c := range MAX_RADIO_CHANS {
			for p := range TQ_NUM_PRIO {
				for transmitQueue.Remove(c, p) != nil { //revive:disable-line:empty-block
				}
			}
		}

		dataLinkQueue.Init()
	})
}

// startRecvProcess runs recv_process until the test ends, failing the test if
// it does not then finish.
func startRecvProcess(t *testing.T) {
	t.Helper()

	var ctx, cancel = context.WithCancel(t.Context())
	var done = make(chan struct{})

	go func() {
		recv_process(ctx)
		close(done)
	}()

	t.Cleanup(func() {
		cancel()

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("recv_process did not finish after its context was cancelled")
		}
	})
}

// The queue is where the receive threads hand work over, so an item put on it
// has to be dispatched to the handler for its type.
func TestRecvProcessDispatchesAQueuedItem(t *testing.T) {
	setupRecvProcessTest(t, AX25_T1V_FRACK_DEFAULT)

	startRecvProcess(t)

	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = "Q1TEST"
	addrs[PEERCALL] = "Q2TEST"

	dataLinkQueue.ConnectRequest(addrs, 2, 0, 0, 0)

	// Connecting starts with a SABM, so something reaching the transmit
	// queue says the request was dispatched rather than merely dequeued.
	assert.Eventually(t, func() bool {
		return transmitQueue.Count(0, -1, "", "", false) > 0
	}, 10*time.Second, 10*time.Millisecond, "the connect request was not acted on")
}

// recv_process is the only place the queue is served, so every kind of item
// has to have somewhere to go from it.
func TestRecvProcessDispatchesEveryItemType(t *testing.T) {
	setupRecvProcessTest(t, AX25_T1V_FRACK_DEFAULT)

	startRecvProcess(t)

	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = "Q1TEST"
	addrs[PEERCALL] = "Q2TEST"

	var pp = ax25_from_text("Q2TEST>Q1TEST:>Testing", addrLenient)
	require.NotNil(t, pp)

	var alevel ALevel

	dataLinkQueue.RecFrame(0, 0, 0, pp, alevel, fec_type_none, RETRY_NONE, "")
	dataLinkQueue.RegisterCallsign("Q1TEST", 0, 0)
	// Connecting first, so that the data request behind it has a link with
	// agreed parameters to be queued on.
	dataLinkQueue.ConnectRequest(addrs, 2, 0, 0, 0)
	dataLinkQueue.OutstandingFramesRequest(addrs, 2, 0, 0)
	dataLinkQueue.XmitDataRequest(addrs, 2, 0, 0, 0xF0, []byte("Testing"))
	dataLinkQueue.ChannelBusy(0, OCTYPE_DCD, 1)
	dataLinkQueue.ChannelBusy(0, OCTYPE_DCD, 0)
	dataLinkQueue.SeizeConfirm(0)
	dataLinkQueue.DisconnectRequest(addrs, 2, 0, 0)
	dataLinkQueue.UnregisterCallsign("Q1TEST", 0, 0)
	dataLinkQueue.ClientCleanup(0)

	// Everything is served in turn, so a SABM for a second station, asked
	// for at the back of the queue, says the whole queue was dispatched.
	var otherAddrs = addrs
	otherAddrs[PEERCALL] = "Q3TEST"

	dataLinkQueue.ConnectRequest(otherAddrs, 2, 0, 0, 0)

	assert.Eventually(t, func() bool {
		return transmitQueue.Count(0, -1, "", "Q3TEST", false) > 0
	}, 10*time.Second, 10*time.Millisecond, "the queue was not served to the end")
}

// Nothing on the queue and nothing to do is not an error - it happens whenever
// a sender's wake-up outlives the item that prompted it - but it is worth
// knowing about.
func TestRecvProcessLogsASpuriousWakeUp(t *testing.T) {
	setupRecvProcessTest(t, AX25_T1V_FRACK_DEFAULT)

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	var previousLevel = logrus.GetLevel()

	logrus.SetLevel(logrus.DebugLevel)

	t.Cleanup(func() { logrus.SetLevel(previousLevel) })

	startRecvProcess(t)

	// Wake the queue without putting anything on it.
	assert.Eventually(t, func() bool {
		select {
		case dataLinkQueue.wake <- struct{}{}:
		default:
		}

		for _, entry := range hook.AllEntries() {
			if entry.Message == "recv_process: spurious wakeup. (Temp debugging message - not a problem if only occasional.)" {
				return true
			}
		}

		return false
	}, 10*time.Second, 10*time.Millisecond, "a wake-up with an empty queue went unremarked")
}

// Waiting for something to arrive has to end in time for the connected mode
// timers: a link whose acknowledgement never comes gets nowhere unless T1 is
// allowed to expire.
func TestRecvProcessRunsTheLinkTimersWhileTheQueueIsEmpty(t *testing.T) {
	// One second of T1, so the retry does not hold the test up for long.
	setupRecvProcessTest(t, 1)

	startRecvProcess(t)

	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = "Q1TEST"
	addrs[PEERCALL] = "Q2TEST"

	dataLinkQueue.ConnectRequest(addrs, 2, 0, 0, 0)

	// Nothing is answering, so the second SABM can only come from T1
	// expiring while the queue sits empty.
	assert.Eventually(t, func() bool {
		return transmitQueue.Count(0, -1, "", "", false) > 1
	}, 30*time.Second, 10*time.Millisecond, "the connect attempt was never retried")
}
