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
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/gordonklaus/portaudio"
)

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
	MODEM_BPSK
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

const DEFAULT_ADEVICE = "default" // Use default device for PortAudio.

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
	var rb = &audioRingBuffer{ //nolint:exhaustruct
		buf:  make([]byte, size),
		size: size,
	}
	rb.cond = sync.NewCond(&rb.mu)

	return rb
}

// write adds data to the ring buffer. Called from PortAudio callback.
// Returns true if all data was written, false if some was dropped (overflow).
// Uses chunk copies (at most two) to minimise time holding the mutex.
func (rb *audioRingBuffer) write(data []byte) bool { //nolint:unparam
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
}

var adev [MAX_ADEVS]*adev_s

// portaudioMu guards portaudioRefCount and ensures Initialize/Terminate are
// correctly paired even if audio_open/audio_close are called concurrently.
var portaudioMu sync.Mutex
var portaudioRefCount int

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
// parseALSADeviceName parses ALSA device names like "hw:Card,Dev,Sub" or
// "plughw:Card,Dev,Sub" and returns the card name and device number.
// Returns ("", -1) if the name doesn't match the ALSA pattern.
func parseALSADeviceName(name string) (cardName string, devNum int) {
	// Strip prefix: hw:, plughw:, etc.
	var lower = strings.ToLower(name)

	var idx = strings.Index(lower, "hw:")
	if idx < 0 {
		return "", -1
	}
	var rest = name[idx+3:] // after "hw:"

	// Split by comma: Card,Dev[,Sub]
	var parts = strings.SplitN(rest, ",", 3)
	if len(parts) < 2 {
		return parts[0], -1
	}

	devNum = -1
	fmt.Sscanf(parts[1], "%d", &devNum)

	return parts[0], devNum
}

func findPortAudioDevice(name string, forInput bool) *portaudio.DeviceInfo {
	// Handle default device
	if name == "" || strings.ToLower(name) == "default" {
		if forInput {
			var dev, err = portaudio.DefaultInputDevice()
			if err != nil {
				return nil
			}

			return dev
		} else {
			var dev, err = portaudio.DefaultOutputDevice()
			if err != nil {
				return nil
			}

			return dev
		}
	}

	// Search through all devices
	var devices, err = portaudio.Devices()
	if err != nil {
		return nil
	}

	var devMatchesDirection = func(dev *portaudio.DeviceInfo) bool {
		if forInput {
			return dev.MaxInputChannels > 0
		}

		return dev.MaxOutputChannels > 0
	}

	// Try exact match first
	for _, dev := range devices {
		if dev.Name == name && devMatchesDirection(dev) {
			return dev
		}
	}

	// Try substring match (check both directions)
	for _, dev := range devices {
		if devMatchesDirection(dev) {
			var nameLower = strings.ToLower(name)

			var devLower = strings.ToLower(dev.Name)
			if strings.Contains(devLower, nameLower) || strings.Contains(nameLower, devLower) {
				return dev
			}
		}
	}

	// Try ALSA-style name matching.
	// Config names like "plughw:Loopback,1,1" need to match PortAudio names
	// like "Loopback: PCM (hw:0,1)".
	// Extract card name and device number from the config, then match against
	// the card name prefix and (hw:X,Dev) pattern in PortAudio device names.
	var cardName, devNum = parseALSADeviceName(name)
	if cardName != "" {
		for _, dev := range devices {
			if !devMatchesDirection(dev) {
				continue
			}
			var devLower = strings.ToLower(dev.Name)
			// Check card name appears in PortAudio device name
			if !strings.Contains(devLower, strings.ToLower(cardName)) {
				continue
			}
			// If we have a device number, match it against (hw:X,Dev) in the name
			if devNum >= 0 {
				var target = fmt.Sprintf(",%d)", devNum)
				if strings.Contains(dev.Name, target) {
					return dev
				}
			} else {
				return dev
			}
		}
	}

	// Fall back to default
	text_color_set(DW_COLOR_ERROR)
	dw_printf("Could not match audio device '%s' to any PortAudio device, falling back to default.\n", name)

	if forInput {
		var dev, err = portaudio.DefaultInputDevice()
		if err != nil {
			return nil
		}

		return dev
	} else {
		var dev, err = portaudio.DefaultOutputDevice()
		if err != nil {
			return nil
		}

		return dev
	}
}

/*------------------------------------------------------------------
 *
 * Name:        audio_open
 *
 * Purpose:     Open the digital audio device.
 *
 * Inputs:      pa		- Address of structure of type audio_s.
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
 * Returns:     0 for success, -1 for failure.
 *
 *----------------------------------------------------------------*/

func audio_open(pa *audio_s) int {
	save_audio_config_p = pa

	// Initialize PortAudio, using a refcount so that multiple audio_open/
	// audio_close cycles are correctly paired with Initialize/Terminate.
	portaudioMu.Lock()

	if portaudioRefCount == 0 {
		var err = portaudio.Initialize()
		if err != nil {
			portaudioMu.Unlock()
			text_color_set(DW_COLOR_ERROR)
			dw_printf("PortAudio initialization failed: %v\n", err)

			return -1
		}
	}

	portaudioRefCount++

	portaudioMu.Unlock()

	// If audio_open fails after this point, roll back the refcount increment
	// so it stays correctly paired with audio_close calls.
	var openSucceeded = false

	defer func() {
		if !openSucceeded {
			portaudioMu.Lock()

			portaudioRefCount--
			if portaudioRefCount == 0 {
				portaudio.Terminate()
			}

			portaudioMu.Unlock()
		}
	}()

	for a := 0; a < MAX_ADEVS; a++ {
		adev[a] = new(adev_s)
		adev[a].inputStream = nil
		adev[a].outputStream = nil
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
			adev[a].inbufSizeInBytes = 0
			adev[a].inbuf = nil
			adev[a].inbufLen = 0
			adev[a].inbufNext = 0

			adev[a].outbufSizeInBytes = 0
			adev[a].outbuf = nil
			adev[a].outbufLen = 0

			// Store audio format
			adev[a].sampleRate = pa.adev[a].samples_per_sec
			adev[a].numChannels = pa.adev[a].num_channels
			adev[a].bitsPerSample = pa.adev[a].bits_per_sample
			adev[a].bytesPerFrame = pa.adev[a].num_channels * pa.adev[a].bits_per_sample / 8

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

			if audio_in_name == audio_out_name {
				dw_printf("Audio device for both receive and transmit: %s %s\n", audio_in_name, ctemp)
			} else {
				dw_printf("Audio input device for receive: %s %s\n", audio_in_name, ctemp)
				dw_printf("Audio out device for transmit: %s %s\n", audio_out_name, ctemp)
			}

			// Calculate buffer size
			var bufSizeInBytes = calcbufsize(pa.adev[a].samples_per_sec, pa.adev[a].num_channels, pa.adev[a].bits_per_sample)
			var framesPerBuffer = bufSizeInBytes / adev[a].bytesPerFrame
			adev[a].framesPerBuffer = framesPerBuffer

			/*
			 * Now attempt actual opens.
			 */

			/*
			 * Input device.
			 */

			switch adev[a].g_audio_in_type {
			/*
			 * Soundcard - PortAudio with callback mode.
			 * Callback mode is more reliable than blocking read because the
			 * callback runs on a dedicated audio thread with better timing
			 * guarantees than Go goroutines.
			 */
			case AUDIO_IN_TYPE_SOUNDCARD:
				var inputDev = findPortAudioDevice(audio_in_name, true)
				if inputDev == nil {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Could not find audio input device: %s\n", audio_in_name)

					return -1
				}

				// Create ring buffer for audio data.
				// Size it to hold ~1 second of audio for plenty of headroom.
				// This accommodates Go scheduler delays and processing latency.
				var ringBufSize = pa.adev[a].samples_per_sec * pa.adev[a].num_channels * pa.adev[a].bits_per_sample / 8
				adev[a].inputRingBuf = newAudioRingBuffer(ringBufSize)

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
				var inRingBuf = adev[a].inputRingBuf
				var err error

				if pa.adev[a].bits_per_sample == 16 {
					// Pre-allocate a scratch buffer sized for one full callback invocation
					// so the callback performs zero heap allocations at runtime.
					adev[a].inputScratchBuf = make([]byte, framesPerBuffer*pa.adev[a].num_channels*2)
					var inScratchBuf = adev[a].inputScratchBuf
					adev[a].inputStream, err = portaudio.OpenStream(
						inputParams,
						func(in []int16) {
							// Reuse the pre-allocated scratch buffer; slice to actual length.
							var scratch = inScratchBuf[:len(in)*2]
							for i, sample := range in {
								binary.LittleEndian.PutUint16(scratch[i*2:], uint16(sample))
							}

							inRingBuf.write(scratch)
						},
					)
				} else {
					adev[a].inputStream, err = portaudio.OpenStream(
						inputParams,
						func(in []uint8) {
							// Write uint8 samples directly to ring buffer
							inRingBuf.write(in)
						},
					)
				}

				if err != nil {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Could not open audio device %s for input: %v\n", audio_in_name, err)

					return -1
				}

				err = adev[a].inputStream.Start()
				if err != nil {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Could not start audio input stream: %v\n", err)

					return -1
				}

				adev[a].inbufSizeInBytes = bufSizeInBytes

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

				adev[a].inbufSizeInBytes = SDR_UDP_BUF_MAXLEN

				/*
				 * stdin.
				 */
			case AUDIO_IN_TYPE_STDIN:
				/* Do we need to adjust any properties of stdin? */
				adev[a].inbufSizeInBytes = 1024

			default:
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Internal error, invalid audio_in_type\n")

				return (-1)
			}

			/*
			 * Output device - blocking write mode.
			 * audio_flush_real fills the typed output buffer and calls Write() to
			 * send it to PortAudio. The stream is started lazily on first write
			 * and stopped in audio_wait to avoid underflows during idle periods.
			 */

			var outputDev = findPortAudioDevice(audio_out_name, false)
			if outputDev == nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Could not find audio output device: %s\n", audio_out_name)

				return -1
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
				adev[a].outputBuf16 = make([]int16, framesPerBuffer*pa.adev[a].num_channels)
				adev[a].outputStream, err = portaudio.OpenStream(outputParams, &adev[a].outputBuf16)
			} else {
				adev[a].outputBuf8 = make([]uint8, framesPerBuffer*pa.adev[a].num_channels)
				adev[a].outputStream, err = portaudio.OpenStream(outputParams, &adev[a].outputBuf8)
			}

			if err != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Could not open audio device %s for output: %v\n", audio_out_name, err)

				return -1
			}

			// Output stream is opened but NOT started here.
			// It will be started lazily on first write in audio_flush_real
			// and stopped in audio_wait, to avoid underflows during idle periods.

			adev[a].outbufSizeInBytes = bufSizeInBytes

			// Version 1.3 - after a report of this situation for Mac OSX version.
			if adev[a].inbufSizeInBytes < 256 || adev[a].inbufSizeInBytes > 32768 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Audio buffer has unexpected extreme size of %d bytes.\n", adev[a].inbufSizeInBytes)
				dw_printf("This might be caused by unusual audio device configuration values.\n")

				adev[a].inbufSizeInBytes = 2048
				dw_printf("Using %d to attempt recovery.\n", adev[a].inbufSizeInBytes)
			}

			/*
			 * Finally allocate byte-level buffers for each direction.
			 */
			adev[a].inbuf = make([]byte, adev[a].inbufSizeInBytes)
			Assert(adev[a].inbuf != nil)
			adev[a].inbufLen = 0
			adev[a].inbufNext = 0

			adev[a].outbuf = make([]byte, adev[a].outbufSizeInBytes)
			Assert(adev[a].outbuf != nil)
			adev[a].outbufLen = 0
		} /* end of audio device defined */
	} /* end of for each audio device */

	openSucceeded = true

	return (0)
} /* end audio_open */

/*------------------------------------------------------------------
 *
 * Name:        audio_get
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
	Assert(adev[a].inbufSizeInBytes >= 100 && adev[a].inbufSizeInBytes <= 32768)

	switch adev[a].g_audio_in_type {
	/*
	 * Soundcard - PortAudio callback mode.
	 * Audio data is written to the ring buffer by the callback.
	 * We just read one byte from it here.
	 */
	case AUDIO_IN_TYPE_SOUNDCARD:
		Assert(adev[a].inputRingBuf != nil)

		// Check for overflow (data was dropped in the callback)
		if adev[a].inputRingBuf.checkOverflow() {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Audio input overflow on device %d - some samples lost\n", a)
			dw_printf("If receiving is fine and strange things happen when transmitting, it is probably RF energy\n")
			dw_printf("getting into your audio or digital wiring.\n")

			audio_stats(a,
				save_audio_config_p.adev[a].num_channels,
				0,
				save_audio_config_p.statistics_interval)
		}

		// Drain the ring buffer into inbuf in a single bulk read when exhausted.
		// This acquires the ring buffer mutex only once per inbuf-worth of data
		// rather than once per byte.
		for adev[a].inbufNext >= adev[a].inbufLen {
			var n, ok = adev[a].inputRingBuf.readChunk(adev[a].inbuf)
			if !ok {
				// Ring buffer was closed - stream ended
				return -1
			}

			if n > 0 {
				adev[a].inbufLen = n
				adev[a].inbufNext = 0

				audio_stats(a,
					save_audio_config_p.adev[a].num_channels,
					n/(save_audio_config_p.adev[a].num_channels*save_audio_config_p.adev[a].bits_per_sample/8),
					save_audio_config_p.statistics_interval)
			}
		}

		var b = adev[a].inbuf[adev[a].inbufNext]
		adev[a].inbufNext++

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

				return (-1)
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
			if err != nil {
				if errors.Is(err, io.EOF) {
					text_color_set(DW_COLOR_INFO)
					dw_printf("\nEnd of file on stdin.  Exiting.\n")
					exit(0)
				}

				text_color_set(DW_COLOR_ERROR)
				dw_printf("Error reading from stdin: %v\n", err)

				return -1
			}

			audio_stats(a,
				save_audio_config_p.adev[a].num_channels,
				n/(save_audio_config_p.adev[a].num_channels*save_audio_config_p.adev[a].bits_per_sample/8),
				save_audio_config_p.statistics_interval)

			adev[a].inbufLen = n
			adev[a].inbufNext = 0
		}
	}

	var n int

	if adev[a].inbufNext < adev[a].inbufLen {
		n = int(adev[a].inbuf[adev[a].inbufNext])
		adev[a].inbufNext++
		//No data to read, avoid reading outside buffer
	} else {
		n = 0
	}

	return (n)
} /* end audio_get */

/*------------------------------------------------------------------
 *
 * Name:        audio_put
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
 * See Also:	audio_flush
 *		audio_wait
 *
 *----------------------------------------------------------------*/

func audio_put_real(a int, c uint8) int {
	/* Should never be full at this point. */
	Assert(adev[a].outbufLen < adev[a].outbufSizeInBytes)

	adev[a].outbuf[adev[a].outbufLen] = c
	adev[a].outbufLen++

	if adev[a].outbufLen == adev[a].outbufSizeInBytes {
		return audio_flush(a)
	}

	return (0)
} /* end audio_put */

/*------------------------------------------------------------------
 *
 * Name:        audio_flush
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
	if adev[a].outbufLen == 0 {
		return 0
	}

	if adev[a].outputStream == nil {
		adev[a].outbufLen = 0
		return -1
	}

	if adev[a].outputBuf16 != nil {
		var nSamples = adev[a].outbufLen / 2
		for i := 0; i < nSamples; i++ {
			var lo = adev[a].outbuf[i*2]
			var hi = adev[a].outbuf[i*2+1]
			adev[a].outputBuf16[i] = int16(uint16(lo) | uint16(hi)<<8)
		}

		for i := nSamples; i < len(adev[a].outputBuf16); i++ {
			adev[a].outputBuf16[i] = 0
		}
	} else if adev[a].outputBuf8 != nil {
		copy(adev[a].outputBuf8, adev[a].outbuf[:adev[a].outbufLen])

		for i := adev[a].outbufLen; i < len(adev[a].outputBuf8); i++ {
			adev[a].outputBuf8[i] = 128
		}
	}

	// Start the output stream lazily on first write.
	if !adev[a].outputStarted {
		var err = adev[a].outputStream.Start()
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Could not start audio output stream: %v\n", err)

			return -1
		}

		adev[a].outputStarted = true
	}

	var err = adev[a].outputStream.Write()
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Audio output write error: %v\n", err)

		var stopErr = adev[a].outputStream.Stop()
		if stopErr != nil {
			dw_printf("Audio output stream stop error: %v\n", stopErr)
		}

		adev[a].outputStarted = false
		adev[a].outbufLen = 0

		return -1
	}

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
 *----------------------------------------------------------------*/

func audio_wait(a int) {
	audio_flush(a)

	// Stop the output stream — Pa_StopStream drains remaining buffers
	// before returning. It will be restarted lazily on next write.
	if adev[a].outputStream != nil && adev[a].outputStarted {
		var err = adev[a].outputStream.Stop()
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("audio_wait: failed to stop output stream for device %d: %v\n", a, err)
		}

		adev[a].outputStarted = false
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
	var err = 0

	for a := 0; a < MAX_ADEVS; a++ {
		if adev[a] != nil && (adev[a].inputStream != nil || adev[a].outputStream != nil) {
			audio_wait(a)

			if adev[a].inputStream != nil {
				adev[a].inputStream.Stop()
				adev[a].inputStream.Close()
				adev[a].inputStream = nil
			}

			// Close output stream (already stopped by audio_wait above)
			if adev[a].outputStream != nil {
				if adev[a].outputStarted {
					adev[a].outputStream.Stop()
					adev[a].outputStarted = false
				}

				adev[a].outputStream.Close()
				adev[a].outputStream = nil
			}

			// Then close ring buffers
			if adev[a].inputRingBuf != nil {
				adev[a].inputRingBuf.close()
				adev[a].inputRingBuf = nil
			}

			adev[a].outputBuf16 = nil
			adev[a].outputBuf8 = nil

			if adev[a].udp_sock != nil {
				adev[a].udp_sock.Close()
				adev[a].udp_sock = nil
			}

			adev[a].inbufSizeInBytes = 0
			adev[a].inbuf = nil
			adev[a].inbufLen = 0
			adev[a].inbufNext = 0

			adev[a].outbufSizeInBytes = 0
			adev[a].outbuf = nil
			adev[a].outbufLen = 0
		}
	}

	// Terminate PortAudio when the last audio device is closed.
	portaudioMu.Lock()

	if portaudioRefCount > 0 {
		portaudioRefCount--
		if portaudioRefCount == 0 {
			portaudio.Terminate()
		}
	}

	portaudioMu.Unlock()

	return (err)
} /* end audio_close */

/* end audio.go */
