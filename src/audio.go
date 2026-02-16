package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Interface to audio device commonly called a "sound card" for
 *		historical reasons.
 *
 *		This version is for Linux using gen2brain/alsa (TinyALSA wrapper).
 *
 * References:	Some tips on on using Linux sound devices.
 *
 *		http://www.oreilly.de/catalog/multilinux/excerpt/ch14-05.htm
 *		http://cygwin.com/ml/cygwin-patches/2004-q1/msg00116/devdsp.c
 *		http://manuals.opensound.com/developer/fulldup.c.html
 *
 *		"Introduction to Sound Programming with ALSA"
 *		http://www.linuxjournal.com/article/6735?page=0,1
 *
 *		http://www.alsa-project.org/main/index.php/Asoundrc
 *
 * Credits:	Release 1.0: Fabrice FAURE contributed code for the SDR UDP interface.
 *
 *		Discussion here:  http://gqrx.dk/doc/streaming-audio-over-udp
 *
 *		Release 1.1:  Gabor Berczi provided fixes for the OSS code
 *		which had fallen into decay.
 *
 * Major Revisions:
 *
 *		1.2 - Add ability to use more than one audio device.
 *
 *---------------------------------------------------------------*/

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/gen2brain/alsa"
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

const DEFAULT_ADEVICE = "default" // Use default device for ALSA.

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

/* Current state for each of the audio devices. */

type adev_s struct {
	audio_in_handle  *alsa.PCM
	audio_out_handle *alsa.PCM

	bytes_per_frame int /* number of bytes for a sample from all channels. */
	/* e.g. 4 for stereo 16 bit. */

	/* TODO KG
	   #elif USE_SNDIO
	   	struct sio_hdl *sndio_in_handle;
	   	struct sio_hdl *sndio_out_handle;

	   #else
	   	int oss_audio_device_fd;	Single device, both directions.

	   #endif
	*/

	inbuf_size_in_bytes int    /* number of bytes allocated */
	inbuf_ptr           []byte /* audio input buffer */
	inbuf_len           int    /* number byte of actual data available. */
	inbuf_next          int    /* index of next to remove. */

	outbuf_size_in_bytes int
	outbuf_ptr           []byte /* audio output buffer */
	outbuf_len           int

	g_audio_in_type audio_in_type_e

	udp_sock *net.UDPConn /* UDP socket for receiving data */

}

var adev [MAX_ADEVS]*adev_s

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

// alsaDeviceRegexp parses ALSA device names like:
//   hw:0,1      hw:1,2      plughw:0,0      plughw:Loopback,1,1
//   surround41:CARD=Fred,DEV=0    default
// It strips any plugin prefix (plughw:, hw:, surround41:, etc.) and extracts
// the card identifier and optional device/subdevice numbers.
var alsaDeviceRegexp = regexp.MustCompile(`^(?:[a-zA-Z_]+:)(?:CARD=)?([A-Za-z0-9_]+)(?:,(?:DEV=)?(\d+))?(?:,(\d+))?$`)

// resolveCardByName resolves a card name (like "Loopback") or number string
// (like "0") to the numeric card ID by reading /proc/asound/cards.
func resolveCardByName(nameOrNum string) (uint, error) {
	// If it's already a number, use it directly.
	if n, err := strconv.Atoi(nameOrNum); err == nil {
		return uint(n), nil
	}

	// Read /proc/asound/cards to find the card number for a given name.
	f, err := os.Open("/proc/asound/cards")
	if err != nil {
		return 0, fmt.Errorf("could not read /proc/asound/cards: %w", err)
	}
	defer f.Close()

	// Each card has a line like: " 0 [Loopback       ]: Loopback - Loopback"
	cardLineRe := regexp.MustCompile(`^\s*(\d+)\s+\[(\S+?)\s*\]`)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		m := cardLineRe.FindStringSubmatch(scanner.Text())
		if m != nil && strings.EqualFold(m[2], nameOrNum) {
			n, _ := strconv.Atoi(m[1])
			return uint(n), nil
		}
	}

	return 0, fmt.Errorf("ALSA card name %q not found in /proc/asound/cards", nameOrNum)
}

// resolveDevice resolves an ALSA-style device name to card and device numbers
// for gen2brain/alsa.  Supported formats include:
//   hw:0,1            plughw:0,0           plughw:Loopback,1,1
//   hw:Loopback,0     surround41:Fred,0    default
// The plugin prefix (plughw, surround41, etc.) is stripped since gen2brain/alsa
// talks directly to hardware via ioctls.  The subdevice component (third number)
// is noted but not passed to gen2brain/alsa which doesn't support it.
func resolveDevice(name string, forCapture bool) (card uint, device uint, err error) {
	matches := alsaDeviceRegexp.FindStringSubmatch(name)
	if matches != nil {
		cardID, cardErr := resolveCardByName(matches[1])
		if cardErr != nil {
			return 0, 0, cardErr
		}

		var devID uint = 0
		if matches[2] != "" {
			d, _ := strconv.Atoi(matches[2])
			devID = uint(d)
		}

		if matches[3] != "" {
			text_color_set(DW_COLOR_INFO)
			dw_printf("Note: subdevice %s in %q is ignored (not supported by audio backend)\n",
				matches[3], name)
		}

		return cardID, devID, nil
	}

	// "default" or unrecognized name: auto-detect by enumerating cards.
	cards, enumErr := alsa.EnumerateCards()
	if enumErr != nil {
		return 0, 0, fmt.Errorf("could not enumerate sound cards: %w", enumErr)
	}

	for _, sc := range cards {
		for _, dev := range sc.Devices {
			if forCapture && !dev.IsPlayback {
				return uint(sc.ID), uint(dev.ID), nil
			}
			if !forCapture && dev.IsPlayback {
				return uint(sc.ID), uint(dev.ID), nil
			}
		}
	}

	// Fallback: try card 0, device 0 if enumeration found nothing suitable.
	if len(cards) > 0 && len(cards[0].Devices) > 0 {
		return uint(cards[0].ID), uint(cards[0].Devices[0].ID), nil
	}

	return 0, 0, fmt.Errorf("no suitable audio device found for %q", name)
}

// makeAlsaConfig builds an alsa.Config for the given audio device parameters.
func makeAlsaConfig(pa *audio_s, a int) *alsa.Config {
	var format alsa.PcmFormat
	if pa.adev[a].bits_per_sample == 8 {
		format = alsa.SNDRV_PCM_FORMAT_U8
	} else {
		format = alsa.SNDRV_PCM_FORMAT_S16_LE
	}

	var buf_size_in_bytes = calcbufsize(pa.adev[a].samples_per_sec, pa.adev[a].num_channels, pa.adev[a].bits_per_sample)
	var bytesPerFrame = pa.adev[a].num_channels * pa.adev[a].bits_per_sample / 8
	var periodFrames = uint32(buf_size_in_bytes / bytesPerFrame)

	return &alsa.Config{
		Channels:    uint32(pa.adev[a].num_channels),
		Rate:        uint32(pa.adev[a].samples_per_sec),
		Format:      format,
		PeriodSize:  periodFrames,
		PeriodCount: 4,
	}
}

/*------------------------------------------------------------------
 *
 * Name:        audio_open
 *
 * Purpose:     Open the digital audio device.
 *		For "ALSA", the device names are hw:c,d
 *		where c is the "card" and d is the "device" within the "card."
 *
 *		New in version 1.0, we recognize "udp:" optionally
 *		followed by a port number.
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

	for a := 0; a < MAX_ADEVS; a++ {
		adev[a] = new(adev_s)
		adev[a].audio_in_handle = nil
		adev[a].audio_out_handle = nil
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

			adev[a].inbuf_size_in_bytes = 0
			adev[a].inbuf_ptr = nil
			adev[a].inbuf_len = 0
			adev[a].inbuf_next = 0

			adev[a].outbuf_size_in_bytes = 0
			adev[a].outbuf_ptr = nil
			adev[a].outbuf_len = 0

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

			/*
			 * Now attempt actual opens.
			 */

			/*
			 * Input device.
			 */

			switch adev[a].g_audio_in_type {

			/*
			 * Soundcard - ALSA via gen2brain/alsa
			 */
			case AUDIO_IN_TYPE_SOUNDCARD:

				inCard, inDev, resolveErr := resolveDevice(audio_in_name, true)
				if resolveErr != nil {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Could not resolve audio device %s for input\n%s\n",
						audio_in_name, resolveErr)
					return (-1)
				}

				cfg := makeAlsaConfig(pa, a)
				var openErr error
				adev[a].audio_in_handle, openErr = alsa.PcmOpen(inCard, inDev, alsa.PCM_IN, cfg)
				if openErr != nil {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Could not open audio device %s for input\n%s\n",
						audio_in_name, openErr)
					return (-1)
				}

				// Query actual rate from hardware and update config if different.
				actualRate := int(adev[a].audio_in_handle.Rate())
				if actualRate != pa.adev[a].samples_per_sec {
					text_color_set(DW_COLOR_INFO)
					dw_printf("Asked for %d samples/sec but got %d for %s input.\n",
						pa.adev[a].samples_per_sec, actualRate, audio_in_name)
					pa.adev[a].samples_per_sec = actualRate
				}

				adev[a].bytes_per_frame = pa.adev[a].num_channels * pa.adev[a].bits_per_sample / 8
				adev[a].inbuf_size_in_bytes = int(adev[a].audio_in_handle.PeriodSize()) * adev[a].bytes_per_frame

				/* Version 1.3 sanity check */
				if adev[a].inbuf_size_in_bytes < 256 || adev[a].inbuf_size_in_bytes > 32768 {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Audio input buffer has unexpected extreme size of %d bytes.\n", adev[a].inbuf_size_in_bytes)
					dw_printf("This might be caused by unusual audio device configuration values.\n")
					adev[a].inbuf_size_in_bytes = 2048
					dw_printf("Using %d to attempt recovery.\n", adev[a].inbuf_size_in_bytes)
				}

				/* TODO KG
				#elif USE_SNDIO
						adev[a].sndio_in_handle = sio_open (audio_in_name, SIO_REC, 0);
						if (adev[a].sndio_in_handle == nil) {
						  text_color_set(DW_COLOR_ERROR);
						  dw_printf ("Could not open audio device %s for input\n",
							audio_in_name);
						  return (-1);
						}

						adev[a].inbuf_size_in_bytes = set_sndio_params (a, adev[a].sndio_in_handle, pa, audio_in_name, "input");

						if (!sio_start (adev[a].sndio_in_handle)) {
						  text_color_set(DW_COLOR_ERROR);
						  dw_printf ("Could not start audio device %s for input\n",
							audio_in_name);
						  return (-1);
						}

				#else // OSS
					        adev[a].oss_audio_device_fd = open (pa.adev[a].adevice_in, O_RDWR);

					        if (adev[a].oss_audio_device_fd < 0) {
					          text_color_set(DW_COLOR_ERROR);
					          dw_printf ("%s:\n", pa.adev[a].adevice_in);
					          return (-1);
					        }

					        adev[a].outbuf_size_in_bytes = adev[a].inbuf_size_in_bytes = set_oss_params (a, adev[a].oss_audio_device_fd, pa);

					        if (adev[a].inbuf_size_in_bytes <= 0 || adev[a].outbuf_size_in_bytes <= 0) {
					          return (-1);
					        }
				#endif
				*/
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
				adev[a].inbuf_size_in_bytes = SDR_UDP_BUF_MAXLEN

				/*
				 * stdin.
				 */
			case AUDIO_IN_TYPE_STDIN:

				/* Do we need to adjust any properties of stdin? */

				adev[a].inbuf_size_in_bytes = 1024

			default:

				text_color_set(DW_COLOR_ERROR)
				dw_printf("Internal error, invalid audio_in_type\n")
				return (-1)
			}

			/*
			 * Output device.  Only "soundcard" is supported at this time.
			 */

			outCard, outDev, resolveErr := resolveDevice(audio_out_name, false)
			if resolveErr != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Could not resolve audio device %s for output\n%s\n",
					audio_out_name, resolveErr)
				return (-1)
			}

			cfg := makeAlsaConfig(pa, a)
			var openErr error
			adev[a].audio_out_handle, openErr = alsa.PcmOpen(outCard, outDev, alsa.PCM_OUT, cfg)
			if openErr != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Could not open audio device %s for output\n%s\n",
					audio_out_name, openErr)
				return (-1)
			}

			adev[a].bytes_per_frame = pa.adev[a].num_channels * pa.adev[a].bits_per_sample / 8
			adev[a].outbuf_size_in_bytes = int(adev[a].audio_out_handle.PeriodSize()) * adev[a].bytes_per_frame

			/* Version 1.3 sanity check */
			if adev[a].outbuf_size_in_bytes < 256 || adev[a].outbuf_size_in_bytes > 32768 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Audio output buffer has unexpected extreme size of %d bytes.\n", adev[a].outbuf_size_in_bytes)
				dw_printf("This might be caused by unusual audio device configuration values.\n")
				adev[a].outbuf_size_in_bytes = 2048
				dw_printf("Using %d to attempt recovery.\n", adev[a].outbuf_size_in_bytes)
			}

			if adev[a].inbuf_size_in_bytes <= 0 || adev[a].outbuf_size_in_bytes <= 0 {
				return (-1)
			}

			/* TODO KG
			#elif USE_SNDIO
				    adev[a].sndio_out_handle = sio_open (audio_out_name, SIO_PLAY, 0);
				    if (adev[a].sndio_out_handle == nil) {
				      text_color_set(DW_COLOR_ERROR);
				      dw_printf ("Could not open audio device %s for output\n",
						audio_out_name);
				      return (-1);
				    }

				    adev[a].outbuf_size_in_bytes = set_sndio_params (a, adev[a].sndio_out_handle, pa, audio_out_name, "output");

				    if (adev[a].inbuf_size_in_bytes <= 0 || adev[a].outbuf_size_in_bytes <= 0) {
				      return (-1);
				    }

				    if (!sio_start (adev[a].sndio_out_handle)) {
				      text_color_set(DW_COLOR_ERROR);
				      dw_printf ("Could not start audio device %s for output\n",
						audio_out_name);
				      return (-1);
				    }
			#endif
			*/

			/*
			 * Finally allocate buffer for each direction.
			 */
			adev[a].inbuf_ptr = make([]byte, adev[a].inbuf_size_in_bytes)
			adev[a].inbuf_len = 0
			adev[a].inbuf_next = 0

			adev[a].outbuf_ptr = make([]byte, adev[a].outbuf_size_in_bytes)
			adev[a].outbuf_len = 0

		} /* end of audio device defined */

	} /* end of for each audio device */

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

	var retries = 0

	Assert(adev[a].inbuf_size_in_bytes >= 100 && adev[a].inbuf_size_in_bytes <= 32768)

	switch adev[a].g_audio_in_type {

	/*
	 * Soundcard - ALSA via gen2brain/alsa
	 */
	case AUDIO_IN_TYPE_SOUNDCARD:

		for adev[a].inbuf_next >= adev[a].inbuf_len {

			Assert(adev[a].audio_in_handle != nil)

			n, err := adev[a].audio_in_handle.Read(adev[a].inbuf_ptr)

			if err == nil && n > 0 {

				/* Success */

				adev[a].inbuf_len = n * adev[a].bytes_per_frame /* convert to number of bytes */
				adev[a].inbuf_next = 0

				audio_stats(a,
					save_audio_config_p.adev[a].num_channels,
					n,
					save_audio_config_p.statistics_interval)

			} else if err == nil && n == 0 {

				/* Didn't expect this, but it's not a problem. */
				/* Wait a little while and try again. */

				text_color_set(DW_COLOR_ERROR)
				dw_printf("Audio input got zero frames\n")
				SLEEP_MS(10)

				adev[a].inbuf_len = 0
				adev[a].inbuf_next = 0
			} else {
				/* Error */
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Audio input device %d error: %s\n", a, err)

				if adev[a].audio_in_handle.State() == alsa.SNDRV_PCM_STATE_XRUN {
					dw_printf("If receiving is fine and strange things happen when transmitting, it is probably RF energy\n")
					dw_printf("getting into your audio or digital wiring. This can cause USB to lock up or PTT to get stuck on.\n")
					dw_printf("Move the radio, and especially the antenna, farther away from the computer.\n")
					dw_printf("Use shielded cable and put ferrite beads on the cables to reduce RF going where it is not wanted.\n")
					dw_printf("\n")
					dw_printf("A less likely cause is the CPU being too slow to keep up with the audio stream.\n")
					dw_printf("Use the \"top\" command, in another command window, to look at CPU usage.\n")
					dw_printf("This might be a temporary condition so we will attempt to recover a few times before giving up.\n")
					dw_printf("If using a very slow CPU, try reducing the CPU load by using -P- command\n")
					dw_printf("line option for 9600 bps or -D3 for slower AFSK .\n")
				}

				audio_stats(a,
					save_audio_config_p.adev[a].num_channels,
					0,
					save_audio_config_p.statistics_interval)

				/* Try to recover a few times and eventually give up. */
				retries++
				if retries > 10 {
					adev[a].inbuf_len = 0
					adev[a].inbuf_next = 0
					return (-1)
				}

				/* Attempt recovery via Prepare + Start */
				adev[a].audio_in_handle.Prepare()
				adev[a].audio_in_handle.Start()

				if adev[a].audio_in_handle.State() != alsa.SNDRV_PCM_STATE_XRUN {
					/* Not an XRUN, wait a bit before retrying */
					SLEEP_MS(250)
				}
			}
		}

		/* TODO KG
		#elif USE_SNDIO

			    while (adev[a].inbuf_next >= adev[a].inbuf_len) {

			      Assert (adev[a].sndio_in_handle != nil);
			      if (poll_sndio (adev[a].sndio_in_handle, POLLIN) < 0) {
				adev[a].inbuf_len = 0;
				adev[a].inbuf_next = 0;
				return (-1);
			      }

			      n = sio_read (adev[a].sndio_in_handle, adev[a].inbuf_ptr, adev[a].inbuf_size_in_bytes);
			      adev[a].inbuf_len = n;
			      adev[a].inbuf_next = 0;

			      audio_stats (a,
					save_audio_config_p.adev[a].num_channels,
					n / (save_audio_config_p.adev[a].num_channels * save_audio_config_p.adev[a].bits_per_sample / 8),
					save_audio_config_p.statistics_interval);
			    }

		#else	// begin OSS

			    while (adev[a].g_audio_in_type == AUDIO_IN_TYPE_SOUNDCARD && adev[a].inbuf_next >= adev[a].inbuf_len) {
			      Assert (adev[a].oss_audio_device_fd > 0);
			      n = read (adev[a].oss_audio_device_fd, adev[a].inbuf_ptr, adev[a].inbuf_size_in_bytes);
			      if (n < 0) {
			        text_color_set(DW_COLOR_ERROR);
			        perror("Can't read from audio device");
			        adev[a].inbuf_len = 0;
			        adev[a].inbuf_next = 0;

			        audio_stats (a,
					save_audio_config_p.adev[a].num_channels,
					0,
					save_audio_config_p.statistics_interval);

			        return (-1);
			      }
			      adev[a].inbuf_len = n;
			      adev[a].inbuf_next = 0;

			      audio_stats (a,
					save_audio_config_p.adev[a].num_channels,
					n / (save_audio_config_p.adev[a].num_channels * save_audio_config_p.adev[a].bits_per_sample / 8),
					save_audio_config_p.statistics_interval);
			    }

		#endif
		*/

		/*
		 * UDP.
		 */

	case AUDIO_IN_TYPE_SDR_UDP:

		for adev[a].inbuf_next >= adev[a].inbuf_len {

			Assert(adev[a].udp_sock != nil)
			var buf = make([]byte, adev[a].inbuf_size_in_bytes)
			var n, _, readErr = adev[a].udp_sock.ReadFromUDP(buf)

			if readErr != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Can't read from udp socket: %s", readErr)
				adev[a].inbuf_len = 0
				adev[a].inbuf_next = 0

				audio_stats(a,
					save_audio_config_p.adev[a].num_channels,
					0,
					save_audio_config_p.statistics_interval)

				return (-1)
			}

			adev[a].inbuf_ptr = buf[:n]
			adev[a].inbuf_len = n
			adev[a].inbuf_next = 0

			audio_stats(a,
				save_audio_config_p.adev[a].num_channels,
				n/(save_audio_config_p.adev[a].num_channels*save_audio_config_p.adev[a].bits_per_sample/8),
				save_audio_config_p.statistics_interval)

		}

		/*
		 * stdin.
		 */
	case AUDIO_IN_TYPE_STDIN:

		for adev[a].inbuf_next >= adev[a].inbuf_len {
			var res, readErr = os.Stdin.Read(adev[a].inbuf_ptr)
			if res <= 0 || readErr != nil {
				text_color_set(DW_COLOR_INFO)
				dw_printf("\nEnd of file on stdin.  Exiting.\n")
				exit(0)
			}

			audio_stats(a,
				save_audio_config_p.adev[a].num_channels,
				res/(save_audio_config_p.adev[a].num_channels*save_audio_config_p.adev[a].bits_per_sample/8),
				save_audio_config_p.statistics_interval)

			adev[a].inbuf_len = res
			adev[a].inbuf_next = 0
		}
	}

	var n int

	if adev[a].inbuf_next < adev[a].inbuf_len {
		n = int(adev[a].inbuf_ptr[adev[a].inbuf_next])
		adev[a].inbuf_next++
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

func audio_put_real(a int, c int) int {
	/* Should never be full at this point. */
	Assert(adev[a].outbuf_len < adev[a].outbuf_size_in_bytes)

	adev[a].outbuf_ptr[adev[a].outbuf_len] = byte(c)
	adev[a].outbuf_len++

	if adev[a].outbuf_len == adev[a].outbuf_size_in_bytes {
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

	Assert(adev[a].audio_out_handle != nil)

	/*
	 * "Prepare" it if not already in the running state.
	 * We stop it at the end of each transmitted packet.
	 */

	if adev[a].audio_out_handle.State() != alsa.SNDRV_PCM_STATE_RUNNING {
		err := adev[a].audio_out_handle.Prepare()
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Audio output start error.\n%s\n", err)
		}
	}

	var psound_offset = 0
	var remaining = adev[a].outbuf_len

	var retries = 10
	for retries > 0 {

		k, err := adev[a].audio_out_handle.Write(adev[a].outbuf_ptr[psound_offset : psound_offset+remaining])

		if err != nil {
			text_color_set(DW_COLOR_ERROR)

			state := adev[a].audio_out_handle.State()
			if state == alsa.SNDRV_PCM_STATE_XRUN {
				dw_printf("Audio output data underrun.\n")
			} else if state == alsa.SNDRV_PCM_STATE_SUSPENDED {
				dw_printf("Driver suspended, recovering\n")
			} else {
				dw_printf("Audio write error: %s\n", err)
			}

			/* Attempt recovery */
			prepErr := adev[a].audio_out_handle.Prepare()
			if prepErr != nil {
				dw_printf("Error preparing after error: %s\n", prepErr)
			}
		} else if k*adev[a].bytes_per_frame != remaining {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Audio write took %d frames rather than %d.\n",
				k, remaining/adev[a].bytes_per_frame)

			/* Go around again with the rest of it. */

			var bytesWritten = k * adev[a].bytes_per_frame
			psound_offset += bytesWritten
			remaining -= bytesWritten
		} else {
			/* Success! */
			adev[a].outbuf_len = 0
			return (0)
		}
		retries--
	}

	text_color_set(DW_COLOR_ERROR)
	dw_printf("Audio write error retry count exceeded.\n")

	adev[a].outbuf_len = 0
	return (-1)

	/* TODO KG
	#elif USE_SNDIO

		int k;
		unsigned char *ptr;
		int len;

		ptr = adev[a].outbuf_ptr;
		len = adev[a].outbuf_len;

		while (len > 0) {
		  Assert (adev[a].sndio_out_handle != nil);
		  if (poll_sndio (adev[a].sndio_out_handle, POLLOUT) < 0) {
		    text_color_set(DW_COLOR_ERROR);
		    perror("Can't write to audio device");
		    adev[a].outbuf_len = 0;
		    return (-1);
		  }

		  k = sio_write (adev[a].sndio_out_handle, ptr, len);
		  ptr += k;
		  len -= k;
		}

		adev[a].outbuf_len = 0;
		return (0);

	#else		// OSS

		int k;
		unsigned char *ptr;
		int len;

		ptr = adev[a].outbuf_ptr;
		len = adev[a].outbuf_len;

		while (len > 0) {
		  Assert (adev[a].oss_audio_device_fd > 0);
		  k = write (adev[a].oss_audio_device_fd, ptr, len);
		  if (k < 0) {
		    text_color_set(DW_COLOR_ERROR);
		    perror("Can't write to audio device");
		    adev[a].outbuf_len = 0;
		    return (-1);
		  }
		  if (k < len) {
		    usleep (10000);
		  }
		  ptr += k;
		  len -= k;
		}

		adev[a].outbuf_len = 0;
		return (0);
	#endif
	*/

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

	/* For playback, this should wait for all pending frames */
	/* to be played and then stop. */

	adev[a].audio_out_handle.Drain()

	/*
	 * When this was first implemented, I observed:
	 *
	 * 	"Experimentation reveals that snd_pcm_drain doesn't
	 * 	 actually wait.  It returns immediately.
	 * 	 However it does serve a useful purpose of stopping
	 * 	 the playback after all the queued up data is used."
	 *
	 *
	 * Now that I take a closer look at the transmit timing, for
	 * version 1.2, it seems that snd_pcm_drain DOES wait until all
	 * all pending frames have been played.
	 * Either way, the caller will now compensate for it.
	 */

	/* TODO KG
	#elif USE_SNDIO

		poll_sndio (adev[a].sndio_out_handle, POLLOUT);

	#else

		Assert (adev[a].oss_audio_device_fd > 0);

		// This caused a crash later on Cygwin.
		// Haven't tried it on other (non-Linux) Unix yet.

		// err = ioctl (adev[a].oss_audio_device_fd, SNDCTL_DSP_SYNC, nil);

	#endif
	*/

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

func audio_close() int {

	var err = 0

	for a := 0; a < MAX_ADEVS; a++ {

		if adev[a].audio_in_handle != nil && adev[a].audio_out_handle != nil {

			audio_wait(a)

			adev[a].audio_in_handle.Close()
			adev[a].audio_out_handle.Close()

			adev[a].audio_in_handle = nil
			adev[a].audio_out_handle = nil

			/* TODO KG
			#elif USE_SNDIO

				  if (adev[a].sndio_in_handle != nil && adev[a].sndio_out_handle != nil) {

				    audio_wait (a);

				    sio_stop (adev[a].sndio_in_handle);
				    sio_stop (adev[a].sndio_out_handle);
				    sio_close (adev[a].sndio_in_handle);
				    sio_close (adev[a].sndio_out_handle);

				    adev[a].sndio_in_handle = adev[a].sndio_out_handle = nil;

			#else

				  if  (adev[a].oss_audio_device_fd > 0) {

				    audio_wait (a);

				    close (adev[a].oss_audio_device_fd);

				    adev[a].oss_audio_device_fd = -1;
			#endif
			*/

			adev[a].inbuf_size_in_bytes = 0
			adev[a].inbuf_ptr = nil
			adev[a].inbuf_len = 0
			adev[a].inbuf_next = 0

			adev[a].outbuf_size_in_bytes = 0
			adev[a].outbuf_ptr = nil
			adev[a].outbuf_len = 0
		}
	}

	return (err)

} /* end audio_close */

/* end audio.c */
