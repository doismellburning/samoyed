package direwolf

import (
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/gen2brain/malgo"
)

/*------------------------------------------------------------------
 *
 * Purpose:   	Interface to audio device commonly called a "sound card" for
 *		historical reasons.
 *
 *		Uses miniaudio (via malgo) for cross-platform audio I/O.
 *		On Linux, the ALSA backend is used to support device strings
 *		like "plughw:Loopback,1,1".
 *
 *---------------------------------------------------------------*/

/*
 * PTT control.
 */

type ptt_method_t int

const (
	PTT_METHOD_NONE   ptt_method_t = iota /* VOX or no transmit. */
	PTT_METHOD_SERIAL                     /* Serial port RTS or DTR. */
	PTT_METHOD_GPIO                       /* General purpose I/O using sysfs, deprecated after 2020, Linux only. */
	PTT_METHOD_GPIOD                      /* General purpose I/O, using libgpiod, Linux only. */
	PTT_METHOD_LPT                        /* Parallel printer port, Linux only. */
	PTT_METHOD_HAMLIB                     /* HAMLib, Linux only. */
	PTT_METHOD_CM108                      /* GPIO pin of CM108/CM119/etc.  Linux only. */
)

type ptt_line_t int

const (
	PTT_LINE_NONE ptt_line_t = iota //  Important: 0 for neither.
	PTT_LINE_RTS
	PTT_LINE_DTR
)

type audio_in_type_e int

const (
	AUDIO_IN_TYPE_SOUNDCARD audio_in_type_e = iota
	AUDIO_IN_TYPE_SDR_UDP
	AUDIO_IN_TYPE_STDIN
)

/* For option to try fixing frames with bad CRC. */

type retry_t int

const (
	RETRY_NONE           retry_t = 0
	RETRY_INVERT_SINGLE  retry_t = 1
	RETRY_INVERT_DOUBLE  retry_t = 2
	RETRY_INVERT_TRIPLE  retry_t = 3
	RETRY_INVERT_TWO_SEP retry_t = 4
	RETRY_MAX            retry_t = 5
)

// Type of communication medium associated with the channel.

type medium_e int

const (
	MEDIUM_NONE   medium_e = iota // Channel is not valid for use.
	MEDIUM_RADIO                  // Internal modem for radio.
	MEDIUM_IGATE                  // Access IGate as ordinary channel.
	MEDIUM_NETTNC                 // Remote network TNC.  (new in 1.8)
)

type sanity_t int

const (
	SANITY_APRS sanity_t = iota
	SANITY_AX25
	SANITY_NONE
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

	adevice_out string /* Name of the audio output device (or file?). */

	num_channels    int /* Should be 1 for mono or 2 for stereo. */
	samples_per_sec int /* Audio sampling rate.  Typically 11025, 22050, 44100, or 48000. */
	bits_per_sample int /* 8 (unsigned char) or 16 (signed short). */

}

type modem_t int

const (
	MODEM_AFSK modem_t = iota
	MODEM_BASEBAND
	MODEM_SCRAMBLE
	MODEM_QPSK
	MODEM_8PSK
	MODEM_OFF
	MODEM_16_QAM
	MODEM_64_QAM
	MODEM_AIS
	MODEM_EAS
)

type layer2_t int

const (
	LAYER2_AX25 layer2_t = iota
	LAYER2_FX25
	LAYER2_IL2P
)

type v26_e int

const (
	V26_UNSPECIFIED v26_e = iota
	V26_A
	V26_B
)

const V26_DEFAULT = V26_B

type dtmf_decode_t int

const (
	DTMF_DECODE_OFF dtmf_decode_t = iota
	DTMF_DECODE_ON
)

const OCTYPE_PTT = 0
const OCTYPE_DCD = 1
const OCTYPE_CON = 2

const NUM_OCTYPES = 3 /* number of values above.   i.e. last value +1. */

const MAX_GPIO_NAME_LEN = 20 // 12 would cover any case I've seen so this should be safe

/* Each channel can also have associated input lines. */
/* So far, we just have one for transmit inhibit. */

const ICTYPE_TXINH = 0

const NUM_ICTYPES = 1 /* number of values above. i.e. last value +1. */

/* Properties for each radio channel, common to receive and transmit. */
/* Can be different for each radio channel. */

type achan_param_s struct {

	// Currently, we have a fixed mapping from audio sources to channel.
	//
	//		ADEVICE		CHANNEL (mono)		(stereo)
	//		0		0			0, 1
	//		1		2			2, 3
	//		2		4			4, 5
	//
	// A future feauture might allow the user to specify a different audio source.
	// This would allow multiple modems (with associated channel) to share an audio source.
	// int audio_source;	// Default would be [0,1,2,3,4,5]

	// What else should be moved out of structure and enlarged when NETTNC is implemented.  ???

	modem_type modem_t

	/* Usual AFSK. */
	/* Baseband signal. Not used yet. */
	/* Scrambled http://www.amsat.org/amsat/articles/g3ruh/109/fig03.gif */
	/* Might try MFJ-2400 / CCITT v.26 / Bell 201 someday. */
	/* No modem.  Might want this for DTMF only channel. */

	layer2_xmit layer2_t // Must keep in sync with layer2_tx, below.

	// IL2P - New for version 1.7.
	// New layer 2 with FEC.  Much less overhead than FX.25 but no longer backward compatible.
	// Only applies to transmit.
	// Listening for FEC sync word should add negligible overhead so
	// we leave reception enabled all the time as we do with FX.25.
	// TODO:  FX.25 should probably be put here rather than global for all channels.

	fx25_strength int // Strength of FX.25 FEC.
	// 16, 23, 64 for specific number of parity symbols.
	// 1 for automatic selection based on frame size.

	il2p_max_fec int // 1 for max FEC length, 0 for automatic based on size.

	il2p_invert_polarity int // 1 means invert on transmit.  Receive handles either automatically.

	v26_alternative v26_e

	// Original implementation used alternative A for 2400 bbps PSK.
	// Years later, we discover that MFJ-2400 used alternative B.
	// It's likely the others did too.  it also works a little better.
	// Default to MFJ compatible and print warning if user did not
	// pick one explicitly.

	dtmf_decode dtmf_decode_t

	/* Originally the DTMF ("Touch Tone") decoder was always */
	/* enabled because it took a negligible amount of CPU. */
	/* There were complaints about the false positives when */
	/* hearing other modulation schemes on HF SSB so now it */
	/* is enabled only when needed. */

	/* "On" will send special "t" packet to attached applications */
	/* and process as APRStt.  Someday we might want to separate */
	/* these but for now, we have a single off/on. */

	decimate int /* Reduce AFSK sample rate by this factor to */
	/* decrease computational requirements. */

	upsample int /* Upsample by this factor for G3RUH. */

	mark_freq  int /* Two tones for AFSK modulation, in Hz. */
	space_freq int /* Standard tones are 1200 and 2200 for 1200 baud. */

	baud int /* Data bits per second. */
	/* Standard rates are 1200 for VHF and 300 for HF. */
	/* This should really be called bits per second. */

	/* Next 3 come from config file or command line. */

	profiles string /* zero or more of ABC etc, optional + */

	num_freq int /* Number of different frequency pairs for decoders. */

	offset int /* Spacing between filter frequencies. */

	num_slicers int /* Number of different threshold points to decide */
	/* between mark or space. */

	/* This is derived from above by demod_init. */

	num_subchan int /* Total number of modems for each channel. */

	/* These are for dealing with imperfect frames. */

	fix_bits retry_t /* Level of effort to recover from */
	/* a bad FCS on the frame. */
	/* 0 = no effort */
	/* 1 = try fixing a single bit */
	/* 2... = more techniques... */

	sanity_test sanity_t /* Sanity test to apply when finding a good */
	/* CRC after making a change. */
	/* Must look like APRS, AX.25, or anything. */

	passall bool /* Allow thru even with bad CRC. */

	/* Additional properties for transmit. */

	/* Originally we had control outputs only for PTT. */
	/* In version 1.2, we generalize this to allow others such as DCD. */
	/* In version 1.4 we add CON for connected to another station. */
	/* Index following structure by one of these: */

	octrl [NUM_OCTYPES]struct {
		ptt_method ptt_method_t /* none, serial port, GPIO, LPT, HAMLIB, CM108. */

		ptt_device string /* Serial device name for PTT.  e.g. COM1 or /dev/ttyS0 */
		/* Also used for HAMLIB.  Could be host:port when model is 1. */
		/* For years, 20 characters was plenty then we start getting extreme names like this: */
		/* /dev/serial/by-id/usb-FTDI_Navigator__CAT___2nd_PTT__00000000-if00-port0 */
		/* /dev/serial/by-id/usb-Prolific_Technology_Inc._USB-Serial_Controller_D-if00-port0 */
		/* Issue 104, changed to 100 bytes in version 1.5. */

		/* This same field is also used for CM108/CM119 GPIO PTT which will */
		/* have a name like /dev/hidraw1 for Linux or */
		/* \\?\hid#vid_0d8c&pid_0008&mi_03#8&39d3555&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030} */
		/* for Windows.  Largest observed was 95 but add some extra to be safe. */

		ptt_line  ptt_line_t /* Control line when using serial port. PTT_LINE_RTS, PTT_LINE_DTR. */
		ptt_line2 ptt_line_t /* Optional second one:  PTT_LINE_NONE when not used. */

		out_gpio_num int /* GPIO number.  Originally this was only for PTT. */
		/* It is now more general. */
		/* octrl array is indexed by PTT, DCD, or CONnected indicator. */
		/* For CM108/CM119, this should be in range of 1-8. */

		out_gpio_name string
		/* originally, gpio number NN was assumed to simply */
		/* have the name gpioNN but this turned out not to be */
		/* the case for CubieBoard where it was longer. */
		/* This is filled in by ptt_init so we don't have to */
		/* recalculate it each time we access it. */
		/* Also GPIO chip name for GPIOD method. Looks like 'gpiochip4' */

		/* This could probably be collapsed into ptt_device instead of being separate. */

		ptt_lpt_bit int /* Bit number for parallel printer port.  */
		/* Bit 0 = pin 2, ..., bit 7 = pin 9. */

		ptt_invert  bool /* Invert the output. */
		ptt_invert2 bool /* Invert the secondary output. */

		//#ifdef USE_HAMLIB

		ptt_model int /* HAMLIB model.  -1 for AUTO.  2 for rigctld.  Others are radio model. */
		ptt_rate  int /* Serial port speed when using hamlib CAT control for PTT. */
		/* If zero, hamlib will come up with a default for pariticular rig. */
		//#endif

	}

	ictrl [NUM_ICTYPES]struct {
		method ptt_method_t /* none, serial port, GPIO, LPT. */

		in_gpio_num int /* GPIO number */

		in_gpio_name string
		/* originally, gpio number NN was assumed to simply */
		/* have the name gpioNN but this turned out not to be */
		/* the case for CubieBoard where it was longer. */
		/* This is filled in by ptt_init so we don't have to */
		/* recalculate it each time we access it. */

		invert bool /* true = active low */
	}

	/* Transmit timing. */

	dwait int /* First wait extra time for receiver squelch. */
	/* Default 0 units of 10 mS each . */

	slottime int /* Slot time in 10 mS units for persistence algorithm. */
	/* Typical value is 10 meaning 100 milliseconds. */

	persist int /* Sets probability for transmitting after each */
	/* slot time delay.  Transmit if a random number */
	/* in range of 0 - 255 <= persist value.  */
	/* Otherwise wait another slot time and try again. */
	/* Default value is 63 for 25% probability. */

	txdelay int /* After turning on the transmitter, */
	/* send "flags" for txdelay * 10 mS. */
	/* Default value is 30 meaning 300 milliseconds. */

	txtail int /* Amount of time to keep transmitting after we */
	/* are done sending the data.  This is to avoid */
	/* dropping PTT too soon and chopping off the end */
	/* of the frame.  Again 10 mS units. */
	/* At this point, I'm thinking of 10 (= 100 mS) as the default */
	/* because we're not quite sure when the soundcard audio stops. */

	fulldup bool /* Full Duplex. */

}

type audio_s struct {

	/* Previously we could handle only a single audio device. */
	/* In version 1.2, we generalize this to handle multiple devices. */
	/* This means we can now have more than 2 radio channels. */

	adev [MAX_ADEVS]adev_param_s

	/* Common to all channels. */

	tts_script string /* Script for text to speech. */

	statistics_interval int /* Number of seconds between the audio */
	/* statistics reports.  This is set by */
	/* the "-a" option.  0 to disable feature. */

	xmit_error_rate int /* For testing purposes, we can generate frames with an invalid CRC */
	/* to simulate corruption while going over the air. */
	/* This is the probability, in per cent, of randomly corrupting it. */
	/* Normally this is 0.  25 would mean corrupt it 25% of the time. */

	recv_error_rate int /* Similar but the % probability of dropping a received frame. */

	recv_ber float64 /* Receive Bit Error Rate (BER). */
	/* Probability of inverting a bit coming out of the modem. */

	fx25_auto_enable int /* Turn on FX.25 for current connected mode session */
	/* under poor conditions. */
	/* Set to 0 to disable feature. */
	/* I put it here, rather than with the rest of the link layer */
	/* parameters because it is really a part of the HDLC layer */
	/* and is part of the KISS TNC functionality rather than our data link layer. */
	/* Future: not used yet. */

	timestamp_format string /* -T option */
	/* Precede received & transmitted frames with timestamp. */
	/* Command line option uses "strftime" format string. */

	/* originally a "channel" was always connected to an internal modem. */
	/* In version 1.6, this is generalized so that a channel (as seen by client application) */
	/* can be connected to something else.  Initially, this will allow application */
	/* access to the IGate.  In version 1.8 we add network KISS TNC. */

	// Watch out for maximum number of channels.
	//	MAX_CHANS - Originally, this was 6 for internal modem adio channels. Has been phased out.
	// After adding virtual channels (IGate, network TNC), this is split into two different numbers:
	//	MAX_RADIO_CHANNELS - For internal modems.
	//	MAX_TOTAL_CHANNELS - limited by KISS channels/ports.  Needed for digipeating, filtering, etc.

	// Properties for all channels.

	mycall [MAX_TOTAL_CHANS]string /* Call associated with this radio channel. */
	/* Could all be the same or different. */

	chan_medium [MAX_TOTAL_CHANS]medium_e
	// MEDIUM_NONE for invalid.
	// MEDIUM_RADIO for internal modem.  (only possibility earlier)
	// MEDIUM_IGATE allows application access to IGate.
	// MEDIUM_NETTNC for external TNC via TCP.

	igate_vchannel int /* Virtual channel mapped to APRS-IS. */
	/* -1 for none. */
	/* Redundant but it makes things quicker and simpler */
	/* than always searching thru above. */

	// Applies only to network TNC type channels.

	nettnc_addr [MAX_TOTAL_CHANS]string // Network TNC address:  hostname or IP addr.

	nettnc_port [MAX_TOTAL_CHANS]int // Network TNC TCP port.

	achan [MAX_RADIO_CHANS]achan_param_s

	/* TODO KG
	//#ifdef USE_HAMLIB
	rigs int              // Total number of configured rigs
	rig  [MAX_RIGS]*C.RIG // HAMLib rig instances
	//#endif
	*/

}

const DEFAULT_ADEVICE = "default" // Use default device for ALSA / miniaudio.

/*
 * UDP audio receiving port.  Couldn't find any standard or usage precedent.
 * Got the number from this example:   http://gqrx.dk/doc/streaming-audio-over-udp
 * Any better suggestions?
 */

const DEFAULT_UDP_AUDIO_PORT = 7355

// Maximum size of the UDP buffer (for allowing IP routing, udp packets are often limited to 1472 bytes)

const SDR_UDP_BUF_MAXLEN = 2000

const DEFAULT_NUM_CHANNELS = 1
const DEFAULT_SAMPLES_PER_SEC = 44100 /* Very early observations.  Might no longer be valid. */
/* 22050 works a lot better than 11025. */
/* 44100 works a little better than 22050. */
/* If you have a reasonable machine, use the highest rate. */
const MIN_SAMPLES_PER_SEC = 8000

//const MAX_SAMPLES_PER_SEC	48000	/* Originally 44100.  Later increased because */
/* Software Defined Radio often uses 48000. */

const MAX_SAMPLES_PER_SEC = 192000 /* The cheap USB-audio adapters (e.g. CM108) can handle 44100 and 48000. */
/* The "soundcard" in my desktop PC can do 96kHz or even 192kHz. */
/* We will probably need to increase the sample rate to go much above 9600 baud. */

const DEFAULT_BITS_PER_SAMPLE = 16

const DEFAULT_FIX_BITS = RETRY_NONE // Interesting research project but even a single bit fix up
// will occasionally let corrupted packets through.

/*
 * Standard for AFSK on VHF FM.
 * Reversing mark and space makes no difference because
 * NRZI encoding only cares about change or lack of change
 * between the two tones.
 *
 * HF SSB uses 300 baud and 200 Hz shift.
 * 1600 & 1800 Hz is a popular tone pair, sometimes
 * called the KAM tones.
 */

const DEFAULT_MARK_FREQ = 1200
const DEFAULT_SPACE_FREQ = 2200
const DEFAULT_BAUD = 1200

/* Used for sanity checking in config file and command line options. */
/* 9600 baud is known to work.  */
/* TODO: Is 19200 possible with a soundcard at 44100 samples/sec or do we need a higher sample rate? */

const MIN_BAUD = 100

// const MAX_BAUD	 =	10000
const MAX_BAUD = 40000 // Anyone want to try 38.4 k baud?

/*
 * Typical transmit timings for VHF.
 */

const DEFAULT_DWAIT = 0
const DEFAULT_SLOTTIME = 10 // *10mS = 100mS
const DEFAULT_PERSIST = 63
const DEFAULT_TXDELAY = 30    // *10mS = 300mS
const DEFAULT_TXTAIL = 10     // *10mS = 100mS
const DEFAULT_FULLDUP = false // false = half duplex

/* Audio configuration. */

// TODO KG var save_audio_config_p *audio_s

/* ----------------------------------------------------------------
 * Ring buffer for bridging between malgo's callback-based I/O
 * and the synchronous byte-at-a-time audio API.
 * ---------------------------------------------------------------- */

type ringBuffer struct {
	data     []byte
	size     int
	readPos  int
	writePos int
	count    int
	mu       sync.Mutex
	hasData  *sync.Cond
	hasSpace *sync.Cond
	closed   bool
}

func newRingBuffer(size int) *ringBuffer {
	var rb = &ringBuffer{ //nolint:exhaustruct
		data: make([]byte, size),
		size: size,
	}
	rb.hasData = sync.NewCond(&rb.mu)
	rb.hasSpace = sync.NewCond(&rb.mu)
	return rb
}

// WriteNonBlocking writes as much data as possible without blocking.
// Returns the number of bytes written. Data that doesn't fit is discarded.
// Used by the capture callback to avoid blocking the audio thread.
func (rb *ringBuffer) WriteNonBlocking(data []byte) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	var space = rb.size - rb.count
	var n = len(data)
	if n > space {
		n = space
	}
	if n == 0 {
		return 0
	}

	// Copy in up to two chunks (handling wrap-around)
	var firstChunk = rb.size - rb.writePos
	if firstChunk > n {
		firstChunk = n
	}
	copy(rb.data[rb.writePos:rb.writePos+firstChunk], data[:firstChunk])
	if firstChunk < n {
		copy(rb.data[0:n-firstChunk], data[firstChunk:n])
	}
	rb.writePos = (rb.writePos + n) % rb.size
	rb.count += n

	rb.hasData.Broadcast()
	return n
}

// Write writes all data, blocking if the buffer is full.
// Used by audio_flush_real to provide backpressure from the playback rate.
func (rb *ringBuffer) Write(data []byte) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	var written = 0
	for written < len(data) && !rb.closed {
		for rb.count == rb.size && !rb.closed {
			rb.hasSpace.Wait()
		}
		if rb.closed {
			break
		}

		var space = rb.size - rb.count
		var n = len(data) - written
		if n > space {
			n = space
		}

		var firstChunk = rb.size - rb.writePos
		if firstChunk > n {
			firstChunk = n
		}
		copy(rb.data[rb.writePos:rb.writePos+firstChunk], data[written:written+firstChunk])
		if firstChunk < n {
			copy(rb.data[0:n-firstChunk], data[written+firstChunk:written+n])
		}
		rb.writePos = (rb.writePos + n) % rb.size
		rb.count += n
		written += n

		rb.hasData.Broadcast()
	}
	return written
}

// ReadByte reads a single byte, blocking until data is available.
// Used by audio_get_real for byte-at-a-time reading.
func (rb *ringBuffer) ReadByte() (byte, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	for rb.count == 0 && !rb.closed {
		rb.hasData.Wait()
	}
	if rb.count == 0 {
		return 0, io.EOF
	}

	var b = rb.data[rb.readPos]
	rb.readPos = (rb.readPos + 1) % rb.size
	rb.count--

	rb.hasSpace.Broadcast()
	return b, nil
}

// ReadNonBlocking reads up to len(buf) bytes without blocking.
// Used by the playback callback which must never block.
func (rb *ringBuffer) ReadNonBlocking(buf []byte) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	var n = len(buf)
	if n > rb.count {
		n = rb.count
	}
	if n == 0 {
		return 0
	}

	var firstChunk = rb.size - rb.readPos
	if firstChunk > n {
		firstChunk = n
	}
	copy(buf[:firstChunk], rb.data[rb.readPos:rb.readPos+firstChunk])
	if firstChunk < n {
		copy(buf[firstChunk:n], rb.data[0:n-firstChunk])
	}
	rb.readPos = (rb.readPos + n) % rb.size
	rb.count -= n

	rb.hasSpace.Broadcast()
	return n
}

// WaitEmpty blocks until the buffer is empty or closed.
// Used by audio_wait to drain output before releasing PTT.
func (rb *ringBuffer) WaitEmpty() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	for rb.count > 0 && !rb.closed {
		rb.hasSpace.Wait()
	}
}

// Close wakes all blocked readers/writers so they can exit.
func (rb *ringBuffer) Close() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.closed = true
	rb.hasData.Broadcast()
	rb.hasSpace.Broadcast()
}

/* ----------------------------------------------------------------
 * Audio device runtime state.
 * ---------------------------------------------------------------- */

// ringBufSize is the size of the ring buffers used for audio I/O.
// 64KB provides roughly 0.7 seconds of buffering at 44100 Hz, 16-bit, mono.
const ringBufSize = 65536

type adev_s struct {
	captureDevice  *malgo.Device
	playbackDevice *malgo.Device

	bytesPerFrame int

	// Soundcard input: ring buffer filled by capture callback.
	inRing *ringBuffer

	// Non-soundcard input buffer (UDP, stdin).
	inbuf     []byte
	inbufLen  int
	inbufNext int

	// Output accumulation buffer (batches single-byte writes).
	outbuf     []byte
	outbufLen  int
	outbufSize int

	// Soundcard output: ring buffer read by playback callback.
	outRing *ringBuffer

	g_audio_in_type audio_in_type_e
	udp_sock        *net.UDPConn

	// Silence byte value: 128 for U8, 0 for S16.
	silenceValue byte
}

var adev [MAX_ADEVS]*adev_s

// malgoCtx holds the miniaudio context, shared across all devices.
var malgoCtx *malgo.AllocatedContext

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

// malgoBackends specifies the audio backends to try.
// On Linux we explicitly request ALSA so that ALSA device strings
// (e.g. "plughw:Loopback,1,1") work reliably.
// For future macOS / Windows support, change this to nil (auto-detect)
// or use build tags to select the appropriate backend per platform.
var malgoBackends = []malgo.Backend{malgo.BackendAlsa}

// deviceIDFromName creates a malgo DeviceID from an ALSA device name string.
// Returns nil for the empty string or "default" (meaning use the default device).
func deviceIDFromName(name string) *malgo.DeviceID {
	if name == "" || name == "default" {
		return nil
	}
	var id malgo.DeviceID
	copy(id[:], name)
	return &id
}

/*------------------------------------------------------------------
 *
 * Name:        audio_open
 *
 * Purpose:     Open the digital audio device(s) via miniaudio (malgo).
 *
 * Inputs:      pa		- Address of structure of type audio_s.
 *
 * Outputs:	pa		- The ACTUAL values are returned here.
 *
 * Returns:     0 for success, -1 for failure.
 *
 *----------------------------------------------------------------*/

func audio_open(pa *audio_s) int {

	save_audio_config_p = pa

	// Initialize miniaudio context.
	var err error
	malgoCtx, err = malgo.InitContext(malgoBackends, malgo.ContextConfig{}, nil) //nolint:exhaustruct
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Could not initialize audio context: %s\n", err)
		return -1
	}

	for a := 0; a < MAX_ADEVS; a++ {
		adev[a] = new(adev_s)
	}

	/*
	 * Fill in defaults for any missing values.
	 */

	for a := 0; a < MAX_ADEVS; a++ {

		if pa.adev[a].num_channels == 0 {
			pa.adev[a].num_channels = DEFAULT_NUM_CHANNELS
		}

		if pa.adev[a].samples_per_sec == 0 {
			pa.adev[a].samples_per_sec = DEFAULT_SAMPLES_PER_SEC
		}

		if pa.adev[a].bits_per_sample == 0 {
			pa.adev[a].bits_per_sample = DEFAULT_BITS_PER_SAMPLE
		}

		for channel := 0; channel < MAX_RADIO_CHANS; channel++ {
			if pa.achan[channel].mark_freq == 0 {
				pa.achan[channel].mark_freq = DEFAULT_MARK_FREQ
			}

			if pa.achan[channel].space_freq == 0 {
				pa.achan[channel].space_freq = DEFAULT_SPACE_FREQ
			}

			if pa.achan[channel].baud == 0 {
				pa.achan[channel].baud = DEFAULT_BAUD
			}

			if pa.achan[channel].num_subchan == 0 {
				pa.achan[channel].num_subchan = 1
			}
		}
	}

	/*
	 * Open audio device(s).
	 */

	for a := 0; a < MAX_ADEVS; a++ {
		if pa.adev[a].defined != 0 {

			// Pin any DeviceID pointers we pass into malgo/cgo.
			// They only need to live through the InitDevice calls.
			var pinner runtime.Pinner
			defer pinner.Unpin()

			adev[a].bytesPerFrame = pa.adev[a].num_channels * pa.adev[a].bits_per_sample / 8

			// Compute output accumulation buffer size.
			adev[a].outbufSize = calcbufsize(pa.adev[a].samples_per_sec, pa.adev[a].num_channels, pa.adev[a].bits_per_sample)
			if adev[a].outbufSize < 256 || adev[a].outbufSize > 32768 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Audio buffer has unexpected extreme size of %d bytes.\n", adev[a].outbufSize)
				dw_printf("This might be caused by unusual audio device configuration values.\n")
				adev[a].outbufSize = 2048
				dw_printf("Using %d to attempt recovery.\n", adev[a].outbufSize)
			}
			adev[a].outbuf = make([]byte, adev[a].outbufSize)
			adev[a].outbufLen = 0

			// Silence value depends on audio format.
			if pa.adev[a].bits_per_sample == 8 {
				adev[a].silenceValue = 128
			} else {
				adev[a].silenceValue = 0
			}

			// Determine malgo sample format.
			var format malgo.FormatType
			if pa.adev[a].bits_per_sample == 8 {
				format = malgo.FormatU8
			} else {
				format = malgo.FormatS16
			}

			/*
			 * Determine the type of audio input.
			 */

			adev[a].g_audio_in_type = AUDIO_IN_TYPE_SOUNDCARD

			if strings.EqualFold(pa.adev[a].adevice_in, "stdin") || pa.adev[a].adevice_in == "-" {
				adev[a].g_audio_in_type = AUDIO_IN_TYPE_STDIN
				/* Change "-" to stdin for readability. */
				pa.adev[a].adevice_in = "stdin"
			}
			if strings.HasPrefix(strings.ToLower(pa.adev[a].adevice_in), "udp:") {
				adev[a].g_audio_in_type = AUDIO_IN_TYPE_SDR_UDP
				/* Supply default port if none specified. */
				if strings.EqualFold(pa.adev[a].adevice_in, "udp") ||
					strings.EqualFold(pa.adev[a].adevice_in, "udp:") {
					pa.adev[a].adevice_in = fmt.Sprintf("udp:%d", DEFAULT_UDP_AUDIO_PORT)
				}
			}

			/* Let user know what is going on. */

			var audio_in_name = pa.adev[a].adevice_in
			var audio_out_name = pa.adev[a].adevice_out

			var ctemp string

			if pa.adev[a].num_channels == 2 {
				ctemp = fmt.Sprintf(" (channels %d & %d)", ADEVFIRSTCHAN(a), ADEVFIRSTCHAN(a)+1)
			} else {
				ctemp = fmt.Sprintf(" (channel %d)", ADEVFIRSTCHAN(a))
			}

			text_color_set(DW_COLOR_INFO)

			if audio_in_name == audio_out_name {
				dw_printf("Audio device for both receive and transmit: %s %s\n", audio_in_name, ctemp)
			} else {
				dw_printf("Audio input device for receive: %s %s\n", audio_in_name, ctemp)
				dw_printf("Audio out device for transmit: %s %s\n", audio_out_name, ctemp)
			}

			/*
			 * Now attempt actual opens.
			 */

			/*
			 * Input device.
			 */

			switch adev[a].g_audio_in_type {

			/*
			 * Soundcard via miniaudio.
			 */
			case AUDIO_IN_TYPE_SOUNDCARD:

				adev[a].inRing = newRingBuffer(ringBufSize)

				var captureConfig = malgo.DefaultDeviceConfig(malgo.Capture)
				captureConfig.Capture.Format = format
				captureConfig.Capture.Channels = uint32(pa.adev[a].num_channels)
				captureConfig.SampleRate = uint32(pa.adev[a].samples_per_sec)
				captureConfig.PeriodSizeInMilliseconds = ONE_BUF_TIME

				var captureID = deviceIDFromName(audio_in_name)
				if captureID != nil {
					pinner.Pin(captureID)
					captureConfig.Capture.DeviceID = unsafe.Pointer(captureID) //nolint:gosec // Unsafe part of the API - worth it
				}

				var deviceIdx = a                             // capture loop variable for closure
				var captureCallbacks = malgo.DeviceCallbacks{ //nolint:exhaustruct
					Data: func(pOutput, pInput []byte, frameCount uint32) {
						if len(pInput) == 0 {
							return
						}
						var n = adev[deviceIdx].inRing.WriteNonBlocking(pInput)
						if n < len(pInput) {
							text_color_set(DW_COLOR_ERROR)
							dw_printf("Audio input overrun on device %d\n", deviceIdx)
						}

						audio_stats(deviceIdx,
							save_audio_config_p.adev[deviceIdx].num_channels,
							int(frameCount),
							save_audio_config_p.statistics_interval)
					},
				}

				var captureDevice, captureErr = malgo.InitDevice(malgoCtx.Context, captureConfig, captureCallbacks)
				if captureErr != nil {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Could not open audio device %s for input\n%s\n",
						audio_in_name, captureErr)
					return -1
				}
				adev[a].captureDevice = captureDevice

				var startErr = captureDevice.Start()
				if startErr != nil {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Could not start audio capture on %s\n%s\n",
						audio_in_name, startErr)
					return -1
				}

				/*
				 * UDP.
				 */
			case AUDIO_IN_TYPE_SDR_UDP:

				var udpAddr, addrErr = net.ResolveUDPAddr("udp", audio_in_name[3:]) // Capture the colon onwards from "udp:$PORT"
				if addrErr != nil {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Error with UDP address: %s\n", addrErr)
					return -1
				}

				var udpErr error
				adev[a].udp_sock, udpErr = net.ListenUDP("udp", udpAddr) // Capture the colon onwards from `udp:$PORT`

				if udpErr != nil {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Couldn't create listening socket: %s\n", udpErr)
					return -1
				}
				adev[a].inbuf = make([]byte, SDR_UDP_BUF_MAXLEN)

				/*
				 * stdin.
				 */
			case AUDIO_IN_TYPE_STDIN:

				/* Do we need to adjust any properties of stdin? */

				adev[a].inbuf = make([]byte, 1024)

			default:

				text_color_set(DW_COLOR_ERROR)
				dw_printf("Internal error, invalid audio_in_type\n")
				return -1
			}

			/*
			 * Output device.  Only "soundcard" is supported at this time.
			 */

			adev[a].outRing = newRingBuffer(ringBufSize)

			var playbackConfig = malgo.DefaultDeviceConfig(malgo.Playback)
			playbackConfig.Playback.Format = format
			playbackConfig.Playback.Channels = uint32(pa.adev[a].num_channels)
			playbackConfig.SampleRate = uint32(pa.adev[a].samples_per_sec)
			playbackConfig.PeriodSizeInMilliseconds = ONE_BUF_TIME

			var playbackID = deviceIDFromName(audio_out_name)
			if playbackID != nil {
				pinner.Pin(playbackID)
				playbackConfig.Playback.DeviceID = unsafe.Pointer(playbackID) //nolint:gosec // Unsafe part of the API - worth it
			}

			var deviceIdx = a                              // capture loop variable for closure
			var playbackCallbacks = malgo.DeviceCallbacks{ //nolint:exhaustruct
				Data: func(pOutput, pInput []byte, frameCount uint32) {
					var n = adev[deviceIdx].outRing.ReadNonBlocking(pOutput)
					// Fill remaining output with silence.
					var silence = adev[deviceIdx].silenceValue
					for i := n; i < len(pOutput); i++ {
						pOutput[i] = silence
					}
				},
			}

			var playbackDevice, playbackErr = malgo.InitDevice(malgoCtx.Context, playbackConfig, playbackCallbacks)
			if playbackErr != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Could not open audio device %s for output\n%s\n",
					audio_out_name, playbackErr)
				return -1
			}
			adev[a].playbackDevice = playbackDevice

			var startErr = playbackDevice.Start()
			if startErr != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Could not start audio playback on %s\n%s\n",
					audio_out_name, startErr)
				return -1
			}

		} /* end of audio device defined */

	} /* end of for each audio device */

	return 0

} /* end audio_open */

/*------------------------------------------------------------------
 *
 * Name:        audio_get_real
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

func audio_get_real(a int) int {

	switch adev[a].g_audio_in_type {

	/*
	 * Soundcard - read from ring buffer filled by capture callback.
	 */
	case AUDIO_IN_TYPE_SOUNDCARD:

		var b, err = adev[a].inRing.ReadByte()
		if err != nil {
			return -1
		}
		return int(b)

		/*
		 * UDP.
		 */
	case AUDIO_IN_TYPE_SDR_UDP:

		for adev[a].inbufNext >= adev[a].inbufLen {

			Assert(adev[a].udp_sock != nil)

			var n, _, readErr = adev[a].udp_sock.ReadFromUDP(adev[a].inbuf)

			if readErr != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Can't read from udp socket: %s", readErr)
				adev[a].inbufLen = 0
				adev[a].inbufNext = 0

				audio_stats(a,
					save_audio_config_p.adev[a].num_channels,
					0,
					save_audio_config_p.statistics_interval)

				return -1
			}

			adev[a].inbufLen = n
			adev[a].inbufNext = 0

			audio_stats(a,
				save_audio_config_p.adev[a].num_channels,
				n/(save_audio_config_p.adev[a].num_channels*save_audio_config_p.adev[a].bits_per_sample/8),
				save_audio_config_p.statistics_interval)

		}

		/*
		 * stdin.
		 */
	case AUDIO_IN_TYPE_STDIN:

		for adev[a].inbufNext >= adev[a].inbufLen {
			var n, err = os.Stdin.Read(adev[a].inbuf)
			if err != nil || n <= 0 {
				text_color_set(DW_COLOR_INFO)
				dw_printf("\nEnd of file on stdin.  Exiting.\n")
				exit(0)
			}

			audio_stats(a,
				save_audio_config_p.adev[a].num_channels,
				n/(save_audio_config_p.adev[a].num_channels*save_audio_config_p.adev[a].bits_per_sample/8),
				save_audio_config_p.statistics_interval)

			adev[a].inbufLen = n
			adev[a].inbufNext = 0
		}
	}

	// For non-soundcard types, read from the simple input buffer.
	if adev[a].inbufNext < adev[a].inbufLen {
		var b = adev[a].inbuf[adev[a].inbufNext]
		adev[a].inbufNext++
		return int(b)
	}

	return 0

} /* end audio_get */

/*------------------------------------------------------------------
 *
 * Name:        audio_put_real
 *
 * Purpose:     Send one byte to the audio device.
 *
 * Inputs:	a	- Audio device number.
 *
 *		c	- One byte in range of 0 - 255.
 *
 * Returns:     Normally non-negative.
 *              -1 for any type of error.
 *
 * Description:	The caller must deal with the details of mono/stereo
 *		and number of bytes per sample.
 *
 * See Also:	audio_flush
 *		audio_wait
 *
 *----------------------------------------------------------------*/

func audio_put_real(a int, c uint8) int {
	/* Should never be full at this point. */
	Assert(adev[a].outbufLen < adev[a].outbufSize)

	adev[a].outbuf[adev[a].outbufLen] = c
	adev[a].outbufLen++

	if adev[a].outbufLen == adev[a].outbufSize {
		return audio_flush(a)
	}

	return 0

} /* end audio_put */

/*------------------------------------------------------------------
 *
 * Name:        audio_flush_real
 *
 * Purpose:     Push out any partially filled output buffer.
 *
 * Returns:     Normally non-negative.
 *              -1 for any type of error.
 *
 * See Also:	audio_flush
 *		audio_wait
 *
 *----------------------------------------------------------------*/

func audio_flush_real(a int) int {

	if adev[a] == nil || adev[a].outbufLen == 0 {
		return 0
	}

	adev[a].outRing.Write(adev[a].outbuf[:adev[a].outbufLen])
	adev[a].outbufLen = 0
	return 0

} /* end audio_flush */

/*------------------------------------------------------------------
 *
 * Name:        audio_wait
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
 * In an ideal world:
 *
 *		We would like to ask the hardware when all the queued
 *		up sound has actually come out the speaker.
 *
 * In reality:
 *
 * 		This has been found to be less than reliable in practice.
 *
 *		Caller does the following:
 *
 *		(1) Make note of when PTT is turned on.
 *		(2) Calculate how long it will take to transmit the
 *			frame including TXDELAY, frame (including
 *			"flags", data, FCS and bit stuffing), and TXTAIL.
 *		(3) Call this function, which might or might not wait long enough.
 *		(4) Add (1) and (2) resulting in when PTT should be turned off.
 *		(5) Take difference between current time and desired PPT off time
 *			and wait for additional time if required.
 *
 *----------------------------------------------------------------*/

func audio_wait(a int) {

	audio_flush(a)

	if adev[a] != nil && adev[a].outRing != nil {
		adev[a].outRing.WaitEmpty()
	}

} /* end audio_wait */

/*------------------------------------------------------------------
 *
 * Name:        audio_close
 *
 * Purpose:     Close the audio device(s).
 *
 * Returns:     Normally non-negative.
 *              -1 for any type of error.
 *
 *
 *----------------------------------------------------------------*/

func audio_close() int { //nolint:unparam

	for a := 0; a < MAX_ADEVS; a++ {

		if adev[a] == nil {
			continue
		}

		// Only close devices that were opened (have at least one device handle).
		if adev[a].captureDevice != nil || adev[a].playbackDevice != nil {

			audio_wait(int(a))

			if adev[a].captureDevice != nil {
				adev[a].captureDevice.Stop()
			}
			if adev[a].playbackDevice != nil {
				adev[a].playbackDevice.Stop()
			}

			// Close ring buffers to unblock any waiting goroutines.
			if adev[a].inRing != nil {
				adev[a].inRing.Close()
			}
			if adev[a].outRing != nil {
				adev[a].outRing.Close()
			}

			if adev[a].captureDevice != nil {
				adev[a].captureDevice.Uninit()
				adev[a].captureDevice = nil
			}
			if adev[a].playbackDevice != nil {
				adev[a].playbackDevice.Uninit()
				adev[a].playbackDevice = nil
			}

			if adev[a].udp_sock != nil {
				adev[a].udp_sock.Close()
			}

			adev[a].inRing = nil
			adev[a].outRing = nil
			adev[a].inbuf = nil
			adev[a].inbufLen = 0
			adev[a].inbufNext = 0
			adev[a].outbuf = nil
			adev[a].outbufLen = 0
		}
	}

	if malgoCtx != nil {
		malgoCtx.Free()
		malgoCtx = nil
	}

	return 0

} /* end audio_close */

/* end audio.go */
