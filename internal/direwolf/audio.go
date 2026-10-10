//nolint:gochecknoglobals
package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Interface to audio device commonly called a "sound card" for
 *		historical reasons.
 *
 *		This version uses PortAudio for cross-platform audio support.
 *
 * References:	PortAudio documentation: http://www.portaudio.com/
 *		Go bindings: https://github.com/gordonklaus/portaudio
 *
 * Credits:	Release 1.0: Fabrice FAURE contributed code for the SDR UDP interface.
 *
 *		Discussion here:  http://gqrx.dk/doc/streaming-audio-over-udp
 *
 * Major Revisions:
 *
 *		1.2 - Add ability to use more than one audio device.
 *		Go port - Replaced ALSA with PortAudio for cross-platform support.
 *
 *---------------------------------------------------------------*/

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/gordonklaus/portaudio"
)

type audio_in_type_e int

const (
	AUDIO_IN_TYPE_SOUNDCARD audio_in_type_e = iota
	AUDIO_IN_TYPE_SDR_UDP
	AUDIO_IN_TYPE_STDIN
)

type adev_param_s struct {

	/* Properties of the sound device. */

	defined int /* Was device defined?   0=no.  >0 for yes.  */
	/* First channel defaults to 2 for yes with default config. */
	/* 1 means it was defined by user. */

	copy_from int /* >=0  means copy contents from another audio device. */
	/* In this case we don't have device names, below. */
	/* Num channels, samples/sec, and bit/sample are copied from */
	/* original device and can't be changed. */
	/* -1 for normal case. */

	adevice_in string /* Name of the audio input device (or file?). Can be udp:nnn for UDP or "-" to read from stdin. */

	adevice_out string /* Name of the audio output device. Can be udp:host:port to send audio via UDP. */

	adevice_out_specified bool /* Was the output device named for transmit, rather than */
	/* defaulted or copied from the input side? */

	num_channels    int /* Should be 1 for mono or 2 for stereo. */
	samples_per_sec int /* Audio sampling rate.  Typically 11025, 22050, 44100, or 48000. */
	bits_per_sample int /* 8 (unsigned char) or 16 (signed short). */

}

// validate reports a sound device configuration that the tone generator, the
// demodulators and the .WAV writer cannot work with.
//
// Each of these settings is checked where it is read, so reaching here with a
// bad one means something downstream of that - a device copied from another
// with ADEVICE's copy_from, a default that never got filled in - has gone
// wrong.  Dire Wolf asserted them, which aborts with a C boolean expression
// for a message; say which setting it is and what is accepted instead.
func (adev *adev_param_s) validate() error {
	if adev.bits_per_sample != 8 && adev.bits_per_sample != 16 {
		return fmt.Errorf("bits per audio sample (-b) must be 8 or 16, not %d", adev.bits_per_sample)
	}

	if adev.num_channels != 1 && adev.num_channels != 2 {
		return fmt.Errorf("number of audio channels (ACHANNELS) must be 1 or 2, not %d", adev.num_channels)
	}

	if adev.samples_per_sec < MIN_SAMPLES_PER_SEC || adev.samples_per_sec > MAX_SAMPLES_PER_SEC {
		return fmt.Errorf("audio sample rate (ARATE) must be in the range %d - %d, not %d",
			MIN_SAMPLES_PER_SEC, MAX_SAMPLES_PER_SEC, adev.samples_per_sec)
	}

	return nil
}

const DEFAULT_ADEVICE = "default" // Use default device for PortAudio.

/*
 * UDP audio receiving port.  Couldn't find any standard or usage precedent.
 * Got the number from this example:   http://gqrx.dk/doc/streaming-audio-over-udp
 * Any better suggestions?
 */

const DEFAULT_UDP_AUDIO_PORT = 7355

// Maximum size of the UDP receive buffer. Generous to handle any sender.

const SDR_UDP_BUF_MAXLEN = 2000

// Maximum UDP audio output packet payload. Sized to fit within a standard
// Ethernet MTU (1500 bytes) after IP (20) and UDP (8) headers, so packets
// are not fragmented on typical LAN/loopback paths.

const UDP_AUDIO_OUT_BUF_MAXLEN = 1472

const DEFAULT_NUM_CHANNELS = 1
const DEFAULT_SAMPLES_PER_SEC = 44100 /* Very early observations.  Might no longer be valid. */
// MIN_SAMPLES_PER_SEC : 22050 works a lot better than 11025.
// 44100 works a little better than 22050.
// If you have a reasonable machine, use the highest rate.
const MIN_SAMPLES_PER_SEC = 8000

//const MAX_SAMPLES_PER_SEC	48000	/* Originally 44100.  Later increased because */
/* Software Defined Radio often uses 48000. */

const MAX_SAMPLES_PER_SEC = 192000 /* The cheap USB-audio adapters (e.g. CM108) can handle 44100 and 48000. */
/* The "soundcard" in my desktop PC can do 96kHz or even 192kHz. */
/* We will probably need to increase the sample rate to go much above 9600 baud. */

const DEFAULT_BITS_PER_SAMPLE = 16

// audioRingBuffer is a thread-safe ring buffer for audio data.
// The PortAudio callback writes to this buffer, and the main
// processing thread reads from it.
type audioRingBuffer struct {
	buf      []byte
	size     int
	readPos  int
	writePos int
	count    int // number of bytes available to read
	mu       sync.Mutex
	cond     *sync.Cond
	overflow bool // set when data is dropped due to full buffer
	closed   bool
}

func newAudioRingBuffer(size int) *audioRingBuffer {
	var rb = &audioRingBuffer{ //nolint:exhaustruct_v5
		buf:  make([]byte, size),
		size: size,
	}
	rb.cond = sync.NewCond(&rb.mu)

	return rb
}

// write adds data to the ring buffer. Called from PortAudio callback.
// Returns true if all data was written, false if some was dropped (overflow).
// Uses chunk copies (at most two) to minimise time holding the mutex.
func (rb *audioRingBuffer) write(data []byte) bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.closed {
		return false
	}

	var n = len(data)
	if n == 0 {
		return true
	}

	var overflow = false

	// If incoming data is larger than the whole buffer, keep only the most-recent rb.size bytes.
	if n >= rb.size {
		data = data[n-rb.size:]
		n = rb.size
		overflow = true
		rb.readPos = 0
		rb.writePos = 0
		rb.count = 0
	}

	// Drop oldest bytes to make room if needed.
	var free = rb.size - rb.count
	if n > free {
		var drop = n - free
		rb.readPos = (rb.readPos + drop) % rb.size
		rb.count -= drop
		overflow = true
	}

	// Write in at most two contiguous chunks to handle the ring wrap.
	var part1 = rb.size - rb.writePos
	if part1 > n {
		part1 = n
	}

	copy(rb.buf[rb.writePos:], data[:part1])

	if n > part1 {
		copy(rb.buf[0:], data[part1:])
	}

	rb.writePos = (rb.writePos + n) % rb.size
	rb.count += n

	if overflow {
		rb.overflow = true
	}

	rb.cond.Signal()

	return !overflow
}

// readChunk copies up to len(dst) bytes from the ring buffer into dst, blocking
// until at least one byte is available. Returns the number of bytes copied and
// true on success, or 0 and false if the buffer is closed and empty.
// Uses at most two contiguous copies to minimise lock hold time.
func (rb *audioRingBuffer) readChunk(dst []byte) (int, bool) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	for rb.count == 0 && !rb.closed {
		rb.cond.Wait()
	}

	if rb.closed && rb.count == 0 {
		return 0, false
	}

	var n = len(dst)
	if n > rb.count {
		n = rb.count
	}

	var part1 = rb.size - rb.readPos
	if part1 > n {
		part1 = n
	}

	copy(dst[:part1], rb.buf[rb.readPos:])

	if n > part1 {
		copy(dst[part1:], rb.buf[0:n-part1])
	}

	rb.readPos = (rb.readPos + n) % rb.size
	rb.count -= n

	return n, true
}

// checkOverflow returns and clears the overflow flag.
func (rb *audioRingBuffer) checkOverflow() bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	var overflow = rb.overflow
	rb.overflow = false

	return overflow
}

// close signals that no more data will be written.
func (rb *audioRingBuffer) close() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.closed = true
	rb.cond.Broadcast()
}

/* Current state for each of the audio devices. */

type adev_s struct {
	// PortAudio streams (replaces ALSA handles)
	inputStream  *portaudio.Stream
	outputStream *portaudio.Stream

	// Audio format
	bytesPerFrame int
	sampleRate    int
	numChannels   int
	bitsPerSample int

	// Ring buffer for input audio (filled by callback)
	inputRingBuf *audioRingBuffer

	// Output buffers for blocking writes
	outputBuf16   []int16 // Output buffer for 16-bit blocking writes
	outputBuf8    []uint8 // Output buffer for 8-bit blocking writes
	outputStarted bool    // Whether output stream is currently started

	// Byte-level buffers (maintains existing interface for non-soundcard input)
	inbufSizeInBytes  int
	inbuf             []byte
	inbufLen          int
	inbufNext         int
	inputEnded        atomic.Bool // Standard input ran out, rather than failed.
	outbufSizeInBytes int
	outbuf            []byte
	outbufLen         int

	// Frames per buffer for PortAudio
	framesPerBuffer int

	// Pre-allocated scratch buffer for zero-allocation int16→byte conversion in the input callback.
	inputScratchBuf []byte

	// Input type
	g_audio_in_type audio_in_type_e

	// UDP socket for SDR input
	udp_sock *net.UDPConn

	// UDP connection for audio output
	udp_out_sock net.Conn

	// Stops the silence-keepalive goroutine (UDP output only), see
	// udpSilenceKeepalive.
	silenceStopCh chan struct{}

	// Closed once that goroutine has returned, so Close can wait for it: it
	// reads the device without a lock.
	silenceDoneCh chan struct{}

	// Sample rate and error statistics, reported every statisticsInterval
	// seconds.
	stats              AudioStats
	statisticsInterval int
}

// recordRead adds a read of nbytes from device a to its statistics.  A read
// of nothing counts as an error.
func (d *adev_s) recordRead(a int, nbytes int) {
	d.stats.record(a, d.numChannels, nbytes/d.bytesPerFrame, d.statisticsInterval)
}

// AudioDevices is the set of audio devices that AudioOpen opened, which
// receives from and transmits through them until Close.
type AudioDevices struct {
	dev [MAX_ADEVS]*adev_s

	// outputMu is held by the transmitter (see xmit.go) for the whole of a
	// transmission, and TryLocked by udpSilenceKeepalive, so that the two
	// never interleave on the wire.
	outputMu [MAX_ADEVS]sync.Mutex

	// portaudioHeld records whether opening these devices initialized
	// PortAudio: an all-stdin/UDP configuration, or one whose only soundcard
	// was an output we could do without, never does, and Close must not then
	// terminate it.  Initialize and Terminate are reference counted by
	// PortAudio itself, so each open that initializes it needs exactly one
	// Terminate.
	portaudioHeld bool

	// backendNoise holds what the native audio libraries most recently wrote
	// to stderr from underneath PortAudio, until something goes wrong that it
	// might explain.  Guarded by portaudioMu.
	backendNoise string
}

// portaudioMu serialises calls into PortAudio that aren't on a stream -
// Initialize, Terminate, device lookup, opening a stream - as PortAudio's own
// bookkeeping for those isn't thread-safe.  That bookkeeping is process-wide,
// whichever AudioDevices is calling, so this lock is too.  It is held by
// quietPortAudio, which all of those go through, and also guards
// AudioDevices.backendNoise.
var portaudioMu sync.Mutex

// quietPortAudio runs fn - a call into PortAudio - with whatever the native
// audio libraries write straight to stderr held back (see captureStderrFD).
// ALSA in particular is extremely chatty about sound cards it can't find, and
// none of that is worth showing while things are working; it is remembered for
// printAudioBackendNoise instead, which callers should call after reporting
// that audio didn't work, so the explanation follows the failure.
//
// The lock is held across the call so that what fn provoked is remembered
// before another call can capture anything of its own, rather than two
// overlapping opens each ending up with the other's diagnostics.
func (d *AudioDevices) quietPortAudio(fn func() error) error {
	portaudioMu.Lock()
	defer portaudioMu.Unlock()

	var err error

	var noise = captureStderrFD(func() { err = fn() })

	if noise != "" {
		d.backendNoise = noise
	}

	return err
}

// printAudioBackendNoise prints what the native audio libraries last wrote to
// stderr while we had them held back, if anything, and forgets it.  Call it
// after a message about audio not working: the complaints that were noise a
// moment ago are usually the explanation.
func (d *AudioDevices) printAudioBackendNoise() {
	portaudioMu.Lock()

	var noise = d.backendNoise
	d.backendNoise = ""

	portaudioMu.Unlock()

	if noise == "" {
		return
	}

	text_color_set(DW_COLOR_ERROR)
	dw_printf("Messages from the audio backend, which may explain this:\n%s", noise)
}

// releasePortAudio undoes the PortAudio initialization that opening the
// audio devices took, if it took one.
func (d *AudioDevices) releasePortAudio() {
	if !d.portaudioHeld {
		return
	}

	d.portaudioHeld = false

	// Not quietPortAudio: nothing Terminate says will explain a later failure.
	portaudioMu.Lock()
	captureStderrFD(func() { _ = portaudio.Terminate() })
	portaudioMu.Unlock()
}

// audioNameIsStdin reports whether an audio device name means standard input.
func audioNameIsStdin(name string) bool {
	return strings.EqualFold(name, "stdin") || name == "-"
}

// audioNameIsUDP reports whether an audio device name is a UDP specification.
func audioNameIsUDP(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), "udp:")
}

// audio_out_type_e says what the transmit side of an audio device is attached to.
type audio_out_type_e int

const (
	AUDIO_OUT_TYPE_SOUNDCARD audio_out_type_e = iota
	AUDIO_OUT_TYPE_UDP
	AUDIO_OUT_TYPE_NONE
)

// audioOutType classifies the transmit side of one configured audio device.
//
// ADEVICE given a single name - and the "-" command line argument - puts that
// name on the output side as well, so a receive-only configuration such as
// "ADEVICE stdin" or "ADEVICE udp:7355" asks for standard input, or a
// listening port, as its transmit device.  Neither is something we can play
// audio into, so report that there is no output device rather than trying,
// and failing, to open one.
//
// Call this before AudioOpen rewrites the input name, so that the input and
// output names can still be compared.
func audioOutType(ad *adev_param_s) audio_out_type_e {
	if audioNameIsStdin(ad.adevice_out) {
		return AUDIO_OUT_TYPE_NONE
	}

	if audioNameIsUDP(ad.adevice_out) {
		// A transmit destination names a host and a port.  The same name on
		// both sides is a listening port copied over from the input side -
		// but only when nothing named it for transmit, since a name given for
		// transmit is a destination whatever it matches.
		if !ad.adevice_out_specified && strings.EqualFold(ad.adevice_out, ad.adevice_in) {
			return AUDIO_OUT_TYPE_NONE
		}

		return AUDIO_OUT_TYPE_UDP
	}

	return AUDIO_OUT_TYPE_SOUNDCARD
}

// audioOutputRequired reports whether failing to open a device's transmit side
// should stop us.  Naming an output device is asking for that device
// specifically, so not having it is a configuration error worth complaining
// about; an output device we merely defaulted to, or copied from the input
// side, was never asked for, so its absence leaves the station receive-only
// rather than refusing to start.
func audioOutputRequired(ad *adev_param_s) bool {
	return ad.adevice_out_specified
}

// applyCommandLineAudioSource applies a receive source named on the command
// line to a device's configuration.  The source stands in for the output name
// too, exactly as a single-name ADEVICE does, unless something named a device
// for transmit - so "samoyed-direwolf -" does not go looking for the default
// soundcard to transmit through, while "ADEVICE plughw:1,0 plughw:2,0" keeps
// transmitting on plughw:2,0.
func applyCommandLineAudioSource(ad *adev_param_s, name string) {
	ad.adevice_in = name

	if !ad.adevice_out_specified && strings.EqualFold(ad.adevice_out, DEFAULT_ADEVICE) {
		ad.adevice_out = name
	}
}

// transmitAvailable reports whether audio device a has anywhere to send
// transmitted audio.  A receive-only station - one whose output device was
// absent, or never asked for - does not, and must not key a transmitter to
// send samples that go nowhere.
//
// Read it once the devices are open, and not from a goroutine racing
// AudioOpen or Close: what it reports cannot change in between, as
// nothing reopens an output device while running.
func (d *AudioDevices) transmitAvailable(a int) bool {
	if d == nil || a < 0 || a >= MAX_ADEVS || d.dev[a] == nil {
		return false
	}

	return d.dev[a].outputStream != nil || d.dev[a].udp_out_sock != nil
}

// anyInputRequiresPortAudio reports whether any configured audio device needs
// PortAudio to receive (i.e. is a soundcard rather than stdin or UDP).
func anyInputRequiresPortAudio(pa *RadioConfig) bool {
	for a := range MAX_ADEVS {
		if pa.adev[a].defined == 0 {
			continue
		}

		var inName = pa.adev[a].adevice_in
		if !audioNameIsStdin(inName) && !audioNameIsUDP(inName) {
			return true
		}
	}

	return false
}

// anyOutputRequiresPortAudio reports whether any configured audio device needs
// PortAudio to transmit.
func anyOutputRequiresPortAudio(pa *RadioConfig) bool {
	for a := range MAX_ADEVS {
		if pa.adev[a].defined == 0 {
			continue
		}

		if audioOutType(&pa.adev[a]) == AUDIO_OUT_TYPE_SOUNDCARD {
			return true
		}
	}

	return false
}

// anyDeviceRequiresPortAudio reports whether any configured audio device needs
// PortAudio in either direction.  Used to skip portaudio.Initialize() when all
// devices are stdin/UDP, so that samoyed can run on systems with no working
// PortAudio host backend (issue #501).
func anyDeviceRequiresPortAudio(pa *RadioConfig) bool {
	return anyInputRequiresPortAudio(pa) || anyOutputRequiresPortAudio(pa)
}

// Originally 40.  Version 1.2, try 10 for lower latency.

const ONE_BUF_TIME = 10

func roundup1k(n int) int {
	return (((n) + 0x3ff) & ^0x3ff)
}

func calcbufsize(rate int, chans int, bits int) int {
	var size1 = (rate * chans * bits / 8 * ONE_BUF_TIME) / 1000
	var size2 = roundup1k(size1)

	return (size2)
}

/*
 * Find a PortAudio device by name.
 * Supports:
 *   - "default" or "" -> system default device
 *   - "hw:X,Y" style ALSA names -> search by substring
 *   - Direct device name matching
 */
func (d *AudioDevices) findPortAudioDevice(name string, forInput bool) *portaudio.DeviceInfo {
	// Handle default device
	if name == "" || strings.ToLower(name) == "default" {
		var dev *portaudio.DeviceInfo

		var err = d.quietPortAudio(func() error {
			var e error

			if forInput {
				dev, e = portaudio.DefaultInputDevice()
			} else {
				dev, e = portaudio.DefaultOutputDevice()
			}

			return e
		})
		if err != nil {
			return nil
		}

		return dev
	}

	// Search through all devices
	var devices []*portaudio.DeviceInfo

	var err = d.quietPortAudio(func() error {
		var e error
		devices, e = portaudio.Devices()

		return e
	})
	if err != nil {
		return nil
	}

	var dev = matchPortAudioDeviceByName(name, forInput, devices, alsaCardsPath)
	if dev == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Could not match audio device '%s' to any PortAudio device.\n", name)
		// The noise is left for AudioOpen to print after its own message about
		// the device, so the explanation follows the failure rather than
		// landing between the two.
	}

	return dev
}

// openSoundcardInput opens soundcard name to record from for audio device a,
// and starts it recording into the device's input ring buffer.
func (d *AudioDevices) openSoundcardInput(a int, pa *RadioConfig, name string, framesPerBuffer int, bufSizeInBytes int) error {
	var inputDev = d.findPortAudioDevice(name, true)
	if inputDev == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Could not find audio input device: %s\n", name)
		d.printAudioBackendNoise()

		return fmt.Errorf("no audio input device %s", name)
	}

	// Create ring buffer for audio data.
	// Size it to hold ~1 second of audio for plenty of headroom.
	// This accommodates Go scheduler delays and processing latency.
	var ringBufSize = pa.adev[a].samples_per_sec * pa.adev[a].num_channels * pa.adev[a].bits_per_sample / 8
	d.dev[a].inputRingBuf = newAudioRingBuffer(ringBufSize)

	// Create input stream parameters
	var inputParams = portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   inputDev,
			Channels: pa.adev[a].num_channels,
			Latency:  inputDev.DefaultHighInputLatency,
		},
		Output:          portaudio.StreamDeviceParameters{Device: nil, Channels: 0, Latency: 0},
		SampleRate:      float64(pa.adev[a].samples_per_sec),
		FramesPerBuffer: framesPerBuffer,
		Flags:           portaudio.NoFlag,
	}

	// Open input stream with callback.
	// The callback receives audio data and writes it to the ring buffer.
	// IMPORTANT: Capture the ring buffer pointer now, not in the closure,
	// to avoid the classic Go closure-over-loop-variable bug.
	var inRingBuf = d.dev[a].inputRingBuf
	var err error

	if pa.adev[a].bits_per_sample == 16 {
		// Pre-allocate a scratch buffer sized for one full callback invocation
		// so the callback performs zero heap allocations at runtime.
		d.dev[a].inputScratchBuf = make([]byte, framesPerBuffer*pa.adev[a].num_channels*2)
		var inScratchBuf = d.dev[a].inputScratchBuf
		err = d.quietPortAudio(func() error {
			var e error
			d.dev[a].inputStream, e = portaudio.OpenStream(
				inputParams,
				func(in []int16) {
					// Reuse the pre-allocated scratch buffer; slice to actual length.
					var scratch = inScratchBuf[:len(in)*2]
					for i, sample := range in {
						// Little-endian, lower byte first.
						scratch[i*2] = byte(sample & 0xff)
						scratch[i*2+1] = byte((sample >> 8) & 0xff)
					}

					inRingBuf.write(scratch)
				},
			)

			return e
		})
	} else {
		err = d.quietPortAudio(func() error {
			var e error
			d.dev[a].inputStream, e = portaudio.OpenStream(
				inputParams,
				func(in []uint8) {
					// Write uint8 samples directly to ring buffer
					inRingBuf.write(in)
				},
			)

			return e
		})
	}

	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Could not open audio device %s for input: %v\n", name, err)
		d.printAudioBackendNoise()

		return fmt.Errorf("opening audio device %s for input: %w", name, err)
	}

	err = d.dev[a].inputStream.Start()
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Could not start audio input stream: %v\n", err)

		return fmt.Errorf("starting audio input stream: %w", err)
	}

	d.dev[a].inbufSizeInBytes = bufSizeInBytes

	return nil
}

// openUDPInput opens the UDP port that name, "udp:port", gives for audio
// device a to receive audio on.
func (d *AudioDevices) openUDPInput(a int, name string) error {
	var udpAddr, addrErr = net.ResolveUDPAddr("udp", name[3:]) // Capture the colon onwards from "udp:$PORT"
	if addrErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Error with UDP address: %s\n", addrErr)

		return fmt.Errorf("UDP address %s: %w", name, addrErr)
	}

	var udpErr error

	d.dev[a].udp_sock, udpErr = net.ListenUDP("udp", udpAddr) // Capture the colon onwards from `udp:$PORT`
	if udpErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Couldn't create listening socket: %s\n", udpErr)

		return fmt.Errorf("listening on %s: %w", name, udpErr)
	}

	d.dev[a].inbufSizeInBytes = SDR_UDP_BUF_MAXLEN

	return nil
}

// openUDPOutput connects audio device a to the UDP destination that name,
// "udp:host:port", gives to send its audio to, and starts the silence that
// keeps the stream flowing between transmissions.  Failing to connect is only
// an error if name was given for transmit; otherwise it costs transmitting.
func (d *AudioDevices) openUDPOutput(ctx context.Context, a int, pa *RadioConfig, name string) error {
	/*
	 * UDP output - dial to the specified host:port and send audio packets.
	 */
	var outAddr = name[4:] // skip "udp:"
	var udpOutConn, dialErr = new(net.Dialer).DialContext(ctx, "udp", outAddr)
	if dialErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Could not connect to UDP output address %s: %v\n", outAddr, dialErr)

		if audioOutputRequired(&pa.adev[a]) {
			return fmt.Errorf("connecting to UDP output address %s: %w", outAddr, dialErr)
		}

		dw_printf("Transmitting will not be possible.\n")

		return nil
	}

	d.dev[a].udp_out_sock = udpOutConn
	d.dev[a].outbufSizeInBytes = UDP_AUDIO_OUT_BUF_MAXLEN
	var stop = make(chan struct{})
	var done = make(chan struct{})

	d.dev[a].silenceStopCh = stop
	d.dev[a].silenceDoneCh = done

	go func() {
		defer close(done)

		d.udpSilenceKeepalive(ctx, a, stop)
	}()

	return nil
}

// openSoundcardOutput opens soundcard name to play audio device a's output
// through.  Failing to is only an error if name was given for transmit;
// otherwise it costs transmitting.
func (d *AudioDevices) openSoundcardOutput(a int, pa *RadioConfig, name string, framesPerBuffer int, portaudioReady bool) error {
	/*
	 * Soundcard - blocking write mode.
	 * Flush fills the typed output buffer and calls Write() to
	 * send it to PortAudio. The stream is started lazily on first write
	 * and stopped in wait to avoid underflows during idle periods.
	 */

	if !portaudioReady {
		// PortAudio would not initialize, which we reported above.
		if audioOutputRequired(&pa.adev[a]) {
			return fmt.Errorf("no PortAudio for audio output device %s", name)
		}

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Transmitting will not be possible.\n")

		return nil
	}

	var outputDev = d.findPortAudioDevice(name, false)
	if outputDev == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Could not find audio output device: %s\n", name)
		d.printAudioBackendNoise()

		if audioOutputRequired(&pa.adev[a]) {
			return fmt.Errorf("no audio output device %s", name)
		}

		dw_printf("Transmitting will not be possible.\n")

		return nil
	}

	// Create output stream parameters
	var outputParams = portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{Device: nil, Channels: 0, Latency: 0},
		Output: portaudio.StreamDeviceParameters{
			Device:   outputDev,
			Channels: pa.adev[a].num_channels,
			Latency:  outputDev.DefaultHighOutputLatency,
		},
		SampleRate:      float64(pa.adev[a].samples_per_sec),
		FramesPerBuffer: framesPerBuffer,
		Flags:           portaudio.NoFlag,
	}

	// Open output stream in blocking write mode.
	// Pass a pointer to a typed buffer; Write() will send buffer contents to PortAudio.
	var err error

	if pa.adev[a].bits_per_sample == 16 {
		d.dev[a].outputBuf16 = make([]int16, framesPerBuffer*pa.adev[a].num_channels)
		err = d.quietPortAudio(func() error {
			var e error
			d.dev[a].outputStream, e = portaudio.OpenStream(outputParams, &d.dev[a].outputBuf16)

			return e
		})
	} else {
		d.dev[a].outputBuf8 = make([]uint8, framesPerBuffer*pa.adev[a].num_channels)
		err = d.quietPortAudio(func() error {
			var e error
			d.dev[a].outputStream, e = portaudio.OpenStream(outputParams, &d.dev[a].outputBuf8)

			return e
		})
	}

	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Could not open audio device %s for output: %v\n", name, err)
		d.printAudioBackendNoise()

		if audioOutputRequired(&pa.adev[a]) {
			return fmt.Errorf("opening audio device %s for output: %w", name, err)
		}

		dw_printf("Transmitting will not be possible.\n")

		d.dev[a].outputBuf16 = nil
		d.dev[a].outputBuf8 = nil
		d.dev[a].outputStream = nil

		return nil
	}

	// Output stream is opened but NOT started here.
	// It will be started lazily on first write in Flush
	// and stopped in wait, to avoid underflows during idle periods.

	return nil
}

// openDevice opens audio device a, as pa describes it, for receiving and,
// where it can, transmitting.
func (d *AudioDevices) openDevice(ctx context.Context, a int, pa *RadioConfig, portaudioReady bool) error {
	d.dev[a].inbufSizeInBytes = 0
	d.dev[a].inbuf = nil
	d.dev[a].inbufLen = 0
	d.dev[a].inbufNext = 0

	d.dev[a].outbufSizeInBytes = 0
	d.dev[a].outbuf = nil
	d.dev[a].outbufLen = 0

	// Store audio format
	d.dev[a].sampleRate = pa.adev[a].samples_per_sec
	d.dev[a].numChannels = pa.adev[a].num_channels
	d.dev[a].bitsPerSample = pa.adev[a].bits_per_sample
	d.dev[a].bytesPerFrame = pa.adev[a].num_channels * pa.adev[a].bits_per_sample / 8
	d.dev[a].statisticsInterval = pa.statistics_interval

	/*
	 * Determine the type of audio output, while the configured input
	 * and output names can still be compared.
	 */

	var outType = audioOutType(&pa.adev[a])

	if outType == AUDIO_OUT_TYPE_NONE && audioOutputRequired(&pa.adev[a]) {
		// Named for transmit, but not a transmit device: standard
		// input, or a UDP port to listen on rather than send to.
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Audio device %s cannot transmit.\n", pa.adev[a].adevice_out)
		dw_printf("A transmit device is a soundcard, or udp:host:port.\n")

		return fmt.Errorf("audio device %s cannot transmit", pa.adev[a].adevice_out)
	}

	/*
	 * Determine the type of audio input.
	 */

	d.dev[a].g_audio_in_type = AUDIO_IN_TYPE_SOUNDCARD

	if strings.EqualFold(pa.adev[a].adevice_in, "stdin") || pa.adev[a].adevice_in == "-" {
		d.dev[a].g_audio_in_type = AUDIO_IN_TYPE_STDIN
		/* Change "-" to stdin for readability. */
		pa.adev[a].adevice_in = "stdin"
	}

	if strings.HasPrefix(strings.ToLower(pa.adev[a].adevice_in), "udp:") {
		d.dev[a].g_audio_in_type = AUDIO_IN_TYPE_SDR_UDP
		/* Supply default port if none specified. */
		if strings.EqualFold(pa.adev[a].adevice_in, "udp") ||
			strings.EqualFold(pa.adev[a].adevice_in, "udp:") {
			pa.adev[a].adevice_in = fmt.Sprintf("udp:%d", DEFAULT_UDP_AUDIO_PORT)
		}
	}

	/* Let user know what is going on. */

	/* If not specified, the device names should be "default". */

	var audio_in_name = pa.adev[a].adevice_in
	var audio_out_name = pa.adev[a].adevice_out

	var ctemp string

	if pa.adev[a].num_channels == 2 {
		ctemp = fmt.Sprintf(" (channels %d & %d)", ADEVFIRSTCHAN(a), ADEVFIRSTCHAN(a)+1)
	} else {
		ctemp = fmt.Sprintf(" (channel %d)", ADEVFIRSTCHAN(a))
	}

	text_color_set(DW_COLOR_INFO)

	switch {
	case outType == AUDIO_OUT_TYPE_NONE:
		dw_printf("Audio input device for receive: %s %s\n", audio_in_name, ctemp)
		text_color_set(DW_COLOR_ERROR)
		dw_printf("No audio output device, so transmitting is not possible.\n")
	case audio_in_name == audio_out_name:
		dw_printf("Audio device for both receive and transmit: %s %s\n", audio_in_name, ctemp)
	default:
		dw_printf("Audio input device for receive: %s %s\n", audio_in_name, ctemp)
		dw_printf("Audio out device for transmit: %s %s\n", audio_out_name, ctemp)
	}

	// Calculate buffer size
	var bufSizeInBytes = calcbufsize(pa.adev[a].samples_per_sec, pa.adev[a].num_channels, pa.adev[a].bits_per_sample)
	var framesPerBuffer = bufSizeInBytes / d.dev[a].bytesPerFrame
	d.dev[a].framesPerBuffer = framesPerBuffer

	/*
	 * Now attempt actual opens.
	 */

	/*
	 * Input device.
	 */

	switch d.dev[a].g_audio_in_type {
	/*
	 * Soundcard - PortAudio with callback mode.
	 * Callback mode is more reliable than blocking read because the
	 * callback runs on a dedicated audio thread with better timing
	 * guarantees than Go goroutines.
	 */
	case AUDIO_IN_TYPE_SOUNDCARD:
		var err = d.openSoundcardInput(a, pa, audio_in_name, framesPerBuffer, bufSizeInBytes)
		if err != nil {
			return err
		}

	/*
	 * UDP.
	 */
	case AUDIO_IN_TYPE_SDR_UDP:
		var err = d.openUDPInput(a, audio_in_name)
		if err != nil {
			return err
		}

		/*
		 * stdin.
		 */
	case AUDIO_IN_TYPE_STDIN:
		/* Do we need to adjust any properties of stdin? */
		d.dev[a].inbufSizeInBytes = 1024

	default:
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error, invalid audio_in_type\n")

		return fmt.Errorf("invalid audio input type %d", d.dev[a].g_audio_in_type)
	}

	/*
	 * Output device.
	 */

	// An output device that can't be opened costs us the ability to
	// transmit, but receiving is still useful and is all that some
	// setups - a receive-only IGate, a machine with a capture device
	// but nothing to play through - ever wanted.  Warn and carry on
	// rather than refusing to start.  Flush discards
	// anything the transmit path produces while outputStream and
	// udp_out_sock are both nil.
	d.dev[a].outbufSizeInBytes = bufSizeInBytes

	switch outType {
	case AUDIO_OUT_TYPE_NONE:
		// Nothing to open; already reported above.

	case AUDIO_OUT_TYPE_UDP:
		var err = d.openUDPOutput(ctx, a, pa, audio_out_name)
		if err != nil {
			return err
		}

	case AUDIO_OUT_TYPE_SOUNDCARD:
		var err = d.openSoundcardOutput(a, pa, audio_out_name, framesPerBuffer, portaudioReady)
		if err != nil {
			return err
		}
	}

	// Version 1.3 - after a report of this situation for Mac OSX version.
	if d.dev[a].inbufSizeInBytes < 256 || d.dev[a].inbufSizeInBytes > 32768 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Audio buffer has unexpected extreme size of %d bytes.\n", d.dev[a].inbufSizeInBytes)
		dw_printf("This might be caused by unusual audio device configuration values.\n")

		d.dev[a].inbufSizeInBytes = 2048
		dw_printf("Using %d to attempt recovery.\n", d.dev[a].inbufSizeInBytes)
	}

	/*
	 * Finally allocate byte-level buffers for each direction.
	 */
	d.dev[a].inbuf = make([]byte, d.dev[a].inbufSizeInBytes)
	dwutil.Assert(d.dev[a].inbuf != nil)
	d.dev[a].inbufLen = 0
	d.dev[a].inbufNext = 0

	d.dev[a].outbuf = make([]byte, d.dev[a].outbufSizeInBytes)
	dwutil.Assert(d.dev[a].outbuf != nil)
	d.dev[a].outbufLen = 0

	return nil
}

// fillAudioDefaults fills in any audio device and modem settings that the
// configuration left unset.
func fillAudioDefaults(pa *RadioConfig) {
	for a := range MAX_ADEVS {
		if pa.adev[a].num_channels == 0 {
			pa.adev[a].num_channels = DEFAULT_NUM_CHANNELS
		}

		if pa.adev[a].samples_per_sec == 0 {
			pa.adev[a].samples_per_sec = DEFAULT_SAMPLES_PER_SEC
		}

		if pa.adev[a].bits_per_sample == 0 {
			pa.adev[a].bits_per_sample = DEFAULT_BITS_PER_SAMPLE
		}

		for channel := range MAX_RADIO_CHANS {
			if pa.achan[channel].mark_freq == 0 {
				pa.achan[channel].mark_freq = DEFAULT_MARK_FREQ
			}

			if pa.achan[channel].space_freq == 0 {
				pa.achan[channel].space_freq = DEFAULT_SPACE_FREQ
			}

			if pa.achan[channel].baud == 0 {
				pa.achan[channel].baud = DEFAULT_BAUD
			}
		}
	}
}

/*------------------------------------------------------------------
 *
 * Name:        AudioOpen
 *
 * Purpose:     Open the digital audio device.
 *
 * Inputs:      pa		- Address of structure of type RadioConfig.
 *
 *				Using a structure, rather than separate arguments
 *				seemed to make sense because we often pass around
 *				the same set of parameters various places.
 *
 *				The fields that we care about are:
 *					num_channels
 *					samples_per_sec
 *					bits_per_sample
 *				If zero, reasonable defaults will be provided.
 *
 * Outputs:	pa		- The ACTUAL values are returned here.
 *
 * Returns:     The open devices, or an error once whatever went wrong has
 *		been reported.  Nothing is left open after an error.
 *
 *----------------------------------------------------------------*/

func AudioOpen(ctx context.Context, pa *RadioConfig) (*AudioDevices, error) {
	var d = new(AudioDevices)

	// If AudioOpen fails, close whatever it had opened so far, and give back
	// the PortAudio initialization so it stays correctly paired with
	// Terminate.
	var openSucceeded = false

	defer func() {
		if !openSucceeded {
			d.Close()
		}
	}()

	// Initialize PortAudio only if at least one configured device needs a
	// soundcard.  Pure stdin/UDP configurations must work on systems with no
	// working PortAudio host backend (issue #501).
	var portaudioReady = false
	var inputNeedsPortAudio = anyInputRequiresPortAudio(pa)

	if inputNeedsPortAudio || anyOutputRequiresPortAudio(pa) {
		var err = d.quietPortAudio(portaudio.Initialize)
		if err == nil {
			d.portaudioHeld = true
			portaudioReady = true
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("PortAudio initialization failed: %v\n", err)
			d.printAudioBackendNoise()

			// Without a soundcard we can't receive, so there is nothing left
			// to do.  Needing one only to transmit is survivable: carry on
			// receive-only, which openSoundcardOutput reports.
			if inputNeedsPortAudio {
				return nil, fmt.Errorf("initializing PortAudio: %w", err)
			}
		}
	}

	for a := range MAX_ADEVS {
		d.dev[a] = new(adev_s)
		d.dev[a].inputStream = nil
		d.dev[a].outputStream = nil
	}

	fillAudioDefaults(pa)

	/*
	 * Open audio device(s).
	 */

	for a := range MAX_ADEVS {
		if pa.adev[a].defined != 0 {
			var err = d.openDevice(ctx, a, pa, portaudioReady)
			if err != nil {
				return nil, err
			}
		}
	}

	openSucceeded = true

	return d, nil
} /* end AudioOpen */

// setAudioLevel has each device's statistics report its channels' received
// audio levels from audioLevel.  It is for before anything reads a device.
func (d *AudioDevices) setAudioLevel(audioLevel func(channel int, subchan int) ax25.ALevel) {
	for _, dev := range d.dev {
		if dev != nil {
			dev.stats.audioLevel = audioLevel
		}
	}
}

// inputEnded says whether device a's input ran out - standard input reaching
// its end - rather than failed, for DirewolfMain to tell the two apart when
// the receive thread reports the device giving no more.
func (d *AudioDevices) inputEnded(a int) bool {
	return d.dev[a] != nil && d.dev[a].inputEnded.Load()
}

/*------------------------------------------------------------------
 *
 * Name:        GetByte
 *
 * Purpose:     Get one byte from the audio device.
 *
 * Inputs:	a	- Our number for audio device.
 *
 * Returns:     0 - 255 for a valid sample.
 *              -1 for any type of error.
 *
 * Description:	The caller must deal with the details of mono/stereo
 *		and number of bytes per sample.
 *
 *		This will wait if no data is currently available.
 *
 *----------------------------------------------------------------*/

// GetByte makes AudioDevices a SampleSource.
func (d *AudioDevices) GetByte(a int) int {
	dwutil.Assert(d.dev[a].inbufSizeInBytes >= 100 && d.dev[a].inbufSizeInBytes <= 32768)

	switch d.dev[a].g_audio_in_type {
	/*
	 * Soundcard - PortAudio callback mode.
	 * Audio data is written to the ring buffer by the callback.
	 * We just read one byte from it here.
	 */
	case AUDIO_IN_TYPE_SOUNDCARD:
		dwutil.Assert(d.dev[a].inputRingBuf != nil)

		// Check for overflow (data was dropped in the callback)
		if d.dev[a].inputRingBuf.checkOverflow() {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Audio input overflow on device %d - some samples lost\n", a)
			dw_printf("If receiving is fine and strange things happen when transmitting, it is probably RF energy\n")
			dw_printf("getting into your audio or digital wiring.\n")

			d.dev[a].recordRead(a, 0)
		}

		// Drain the ring buffer into inbuf in a single bulk read when exhausted.
		// This acquires the ring buffer mutex only once per inbuf-worth of data
		// rather than once per byte.
		for d.dev[a].inbufNext >= d.dev[a].inbufLen {
			var n, ok = d.dev[a].inputRingBuf.readChunk(d.dev[a].inbuf)
			if !ok {
				// Ring buffer was closed - stream ended
				return -1
			}

			if n > 0 {
				d.dev[a].inbufLen = n
				d.dev[a].inbufNext = 0

				d.dev[a].recordRead(a, n)
			}
		}

		var b = d.dev[a].inbuf[d.dev[a].inbufNext]
		d.dev[a].inbufNext++

		return int(b)

		/*
		 * UDP.
		 */

	case AUDIO_IN_TYPE_SDR_UDP:
		for d.dev[a].inbufNext >= d.dev[a].inbufLen {
			dwutil.Assert(d.dev[a].udp_sock != nil)

			var n, _, readErr = d.dev[a].udp_sock.ReadFromUDP(d.dev[a].inbuf)
			if readErr != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Can't read from udp socket: %s", readErr)

				d.dev[a].inbufLen = 0
				d.dev[a].inbufNext = 0

				d.dev[a].recordRead(a, 0)

				return (-1)
			}

			d.dev[a].inbufLen = n
			d.dev[a].inbufNext = 0

			d.dev[a].recordRead(a, n)
		}

		/*
		 * stdin.
		 */
	case AUDIO_IN_TYPE_STDIN:
		// Once it has run out there is no more to read, nor to say about it:
		// the other channel of a stereo device asks too, before the receive
		// thread stops.
		if d.dev[a].inputEnded.Load() && d.dev[a].inbufNext >= d.dev[a].inbufLen {
			return -1
		}

		for d.dev[a].inbufNext >= d.dev[a].inbufLen {
			var n, err = os.Stdin.Read(d.dev[a].inbuf)
			if err != nil {
				if errors.Is(err, io.EOF) {
					// The end of the run, which DirewolfMain makes, through
					// the teardown, once the receive thread reports the
					// device as giving no more.
					text_color_set(DW_COLOR_INFO)
					dw_printf("\nEnd of file on stdin.  Exiting.\n")

					d.dev[a].inputEnded.Store(true)

					return -1
				}

				text_color_set(DW_COLOR_ERROR)
				dw_printf("Error reading from stdin: %v\n", err)

				return -1
			}

			d.dev[a].recordRead(a, n)

			d.dev[a].inbufLen = n
			d.dev[a].inbufNext = 0
		}
	}

	var n int

	if d.dev[a].inbufNext < d.dev[a].inbufLen {
		n = int(d.dev[a].inbuf[d.dev[a].inbufNext])
		d.dev[a].inbufNext++
		//No data to read, avoid reading outside buffer
	} else {
		n = 0
	}

	return (n)
} /* end GetByte */

/*------------------------------------------------------------------
 *
 * Name:        Put
 *
 * Purpose:     Send one byte to the audio device.
 *
 * Inputs:	a
 *
 *		c	- One byte in range of 0 - 255.
 *
 * Returns:     Normally non-negative.
 *              -1 for any type of error.
 *
 * Description:	The caller must deal with the details of mono/stereo
 *		and number of bytes per sample.
 *
 * See Also:	Flush
 *		wait
 *
 *----------------------------------------------------------------*/

// Put makes AudioDevices, with Flush, an AudioSink.
func (d *AudioDevices) Put(a int, c uint8) int {
	/* Should never be full at this point. */
	dwutil.Assert(d.dev[a].outbufLen < d.dev[a].outbufSizeInBytes)

	d.dev[a].outbuf[d.dev[a].outbufLen] = c
	d.dev[a].outbufLen++

	if d.dev[a].outbufLen == d.dev[a].outbufSizeInBytes {
		return d.Flush(a)
	}

	return (0)
} /* end Put */

/*------------------------------------------------------------------
 *
 * Name:        Flush
 *
 * Purpose:     Push out any partially filled output buffer.
 *
 * Returns:     Normally non-negative.
 *              -1 for any type of error.
 *
 * See Also:	Flush
 *		wait
 *
 *----------------------------------------------------------------*/

func (d *AudioDevices) Flush(a int) int {
	if d.dev[a].outbufLen == 0 {
		return 0
	}

	if d.dev[a].udp_out_sock != nil {
		var toWrite = d.dev[a].outbufLen
		var n, err = d.dev[a].udp_out_sock.Write(d.dev[a].outbuf[:toWrite])
		d.dev[a].outbufLen = 0

		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Audio UDP output write error: %v\n", err)

			return -1
		}

		if n != toWrite {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Audio UDP output short write: wrote %d of %d bytes\n", n, toWrite)

			return -1
		}

		return 0
	}

	if d.dev[a].outputStream == nil {
		d.dev[a].outbufLen = 0

		return -1
	}

	if d.dev[a].outputBuf16 != nil {
		var nSamples = d.dev[a].outbufLen / 2
		for i := range nSamples {
			var lo = d.dev[a].outbuf[i*2]
			var hi = d.dev[a].outbuf[i*2+1]
			d.dev[a].outputBuf16[i] = int16(hi)<<8 | int16(lo)
		}

		for i := nSamples; i < len(d.dev[a].outputBuf16); i++ {
			d.dev[a].outputBuf16[i] = 0
		}
	} else if d.dev[a].outputBuf8 != nil {
		copy(d.dev[a].outputBuf8, d.dev[a].outbuf[:d.dev[a].outbufLen])

		for i := d.dev[a].outbufLen; i < len(d.dev[a].outputBuf8); i++ {
			d.dev[a].outputBuf8[i] = 128
		}
	}

	// Start the output stream lazily on first write.
	if !d.dev[a].outputStarted {
		var err = d.dev[a].outputStream.Start()
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Could not start audio output stream: %v\n", err)

			return -1
		}

		d.dev[a].outputStarted = true
	}

	var err = d.dev[a].outputStream.Write()
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Audio output write error: %v\n", err)

		var stopErr = d.dev[a].outputStream.Stop()
		if stopErr != nil {
			dw_printf("Audio output stream stop error: %v\n", stopErr)
		}

		d.dev[a].outputStarted = false
		d.dev[a].outbufLen = 0

		return -1
	}

	d.dev[a].outbufLen = 0

	return 0
} /* end Flush */

// silenceKeepaliveInterval controls how often udpSilenceKeepalive sends
// a chunk of silence to a UDP audio output peer.
const silenceKeepaliveInterval = 20 * time.Millisecond

// udpSilenceKeepalive keeps a UDP audio output stream flowing during
// gaps between transmissions.
//
// UDP audio output only carries samples while a packet is actually being
// transmitted; direwolf sends nothing at all the rest of the time. A real
// sound card or SDR stream instead delivers samples continuously, silence
// included, which is what lets the receiving demodulator's channel-busy
// (DCD) state decay back to "clear" between transmissions. Without that, a
// peer fed purely by bursty UDP audio can block forever in its own
// ReadFromUDP once a burst ends, leaving DCD latched "busy" and never
// getting a chance to transmit its own reply (see "Waited too long for
// clear channel" in xmit.go). This streams silence whenever the device
// isn't actively mid-transmission, using the same per-device mutex xmit.go
// already holds for the duration of a real transmission so the two never
// interleave on the wire.
func (d *AudioDevices) udpSilenceKeepalive(ctx context.Context, a int, stop chan struct{}) {
	var ticker = time.NewTicker(silenceKeepaliveInterval)
	defer ticker.Stop()

	var fill byte
	if d.dev[a].bitsPerSample == 8 {
		fill = 128 // Silence for 8-bit unsigned samples is mid-scale.
	} // Silence for 16-bit signed samples is zero, the byte slice's zero value.

	var frames = int(float64(d.dev[a].sampleRate) * silenceKeepaliveInterval.Seconds())
	var chunkLen = frames * d.dev[a].bytesPerFrame

	// UDP_AUDIO_OUT_BUF_MAXLEN is sized to avoid UDP fragmentation; cap the
	// keepalive payload to it too, rounding down to a whole number of
	// frames so the receiver stays frame-aligned.
	if chunkLen > UDP_AUDIO_OUT_BUF_MAXLEN {
		chunkLen = (UDP_AUDIO_OUT_BUF_MAXLEN / d.dev[a].bytesPerFrame) * d.dev[a].bytesPerFrame
	}

	var chunk = make([]byte, chunkLen)
	for i := range chunk {
		chunk[i] = fill
	}

	for {
		select {
		case <-ctx.Done():
			// Shutting down.  Close closes stop as well, but it
			// only runs if somebody gets as far as calling it.
			return
		case <-stop:
			return
		case <-ticker.C:
			if !d.outputMu[a].TryLock() {
				continue
			}

			// Close tears down udp_out_sock under the same lock, so
			// re-check for nil here rather than assuming it's still open.
			if d.dev[a].udp_out_sock != nil {
				_, _ = d.dev[a].udp_out_sock.Write(chunk)
			}

			d.outputMu[a].Unlock()
		}
	}
}

/*------------------------------------------------------------------
 *
 * Name:        wait
 *
 * Purpose:	Finish up audio output before turning PTT off.
 *
 * Inputs:	a		- Index for audio device (not channel!)
 *
 * Returns:     None.
 *
 * Description:	Flush out any partially filled audio output buffer.
 *		Wait until all the queued up audio out has been played.
 *		Take any other necessary actions to stop audio output.
 *
 *----------------------------------------------------------------*/

func (d *AudioDevices) wait(a int) {
	d.Flush(a)

	// For UDP output, packets are sent immediately on flush — nothing more to do.
	if d.dev[a].udp_out_sock != nil {
		return
	}

	// Stop the output stream — Pa_StopStream drains remaining buffers
	// before returning. It will be restarted lazily on next write.
	if d.dev[a].outputStream != nil && d.dev[a].outputStarted {
		var err = d.dev[a].outputStream.Stop()
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Failed to stop audio output stream for device %d: %v\n", a, err)
		}

		d.dev[a].outputStarted = false
	}
} /* end wait */

/*------------------------------------------------------------------
 *
 * Name:        Close
 *
 * Purpose:     Close the audio device(s).
 *
 *----------------------------------------------------------------*/

func (d *AudioDevices) Close() {
	for a := range MAX_ADEVS {
		if d.dev[a] != nil && (d.dev[a].inputStream != nil || d.dev[a].outputStream != nil || d.dev[a].udp_sock != nil || d.dev[a].udp_out_sock != nil) {
			d.wait(a)

			if d.dev[a].inputStream != nil {
				d.dev[a].inputStream.Stop()
				d.dev[a].inputStream.Close()
				d.dev[a].inputStream = nil
			}

			// Close output stream (already stopped by wait above)
			if d.dev[a].outputStream != nil {
				if d.dev[a].outputStarted {
					d.dev[a].outputStream.Stop()
					d.dev[a].outputStarted = false
				}

				d.dev[a].outputStream.Close()
				d.dev[a].outputStream = nil
			}

			// Then close ring buffers
			if d.dev[a].inputRingBuf != nil {
				d.dev[a].inputRingBuf.close()
				d.dev[a].inputRingBuf = nil
			}

			d.dev[a].outputBuf16 = nil
			d.dev[a].outputBuf8 = nil

			if d.dev[a].udp_sock != nil {
				d.dev[a].udp_sock.Close()
				d.dev[a].udp_sock = nil
			}

			if d.dev[a].udp_out_sock != nil {
				if d.dev[a].silenceStopCh != nil {
					close(d.dev[a].silenceStopCh)
					d.dev[a].silenceStopCh = nil

					// Wait for it to go: it reads the device unlocked,
					// so returning while it is still running would leave
					// it racing with whatever touches that next.  It only ever
					// TryLocks, so it can't be stuck on the lock below.
					// With no done channel there is no goroutine to wait
					// for, and a receive from nil would never return.
					if d.dev[a].silenceDoneCh != nil {
						<-d.dev[a].silenceDoneCh
						d.dev[a].silenceDoneCh = nil
					}
				}

				// Nil the socket under the same lock udpSilenceKeepalive
				// takes before writing, so it never observes a closed socket.
				d.outputMu[a].Lock()

				var sock = d.dev[a].udp_out_sock
				d.dev[a].udp_out_sock = nil

				d.outputMu[a].Unlock()

				sock.Close()
			}

			d.dev[a].inbufSizeInBytes = 0
			d.dev[a].inbuf = nil
			d.dev[a].inbufLen = 0
			d.dev[a].inbufNext = 0

			d.dev[a].outbufSizeInBytes = 0
			d.dev[a].outbuf = nil
			d.dev[a].outbufLen = 0
		}
	}

	// Terminate PortAudio, if opening these devices initialized it.
	d.releasePortAudio()
} /* end Close */

/* end audio.go */
