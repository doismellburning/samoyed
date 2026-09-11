//nolint:gochecknoglobals
package direwolf

/*------------------------------------------------------------------
 *
 * Name:	gen_packets_wrapper
 *
 * Purpose:	The parts of the gen_packets utility that have to live inside
 *		package direwolf.
 *
 * Description:	The CLI and the .WAV file writing live in
 *		cmd/samoyed-gen_packets; what remains here is everything that
 *		needs unexported types and functions:
 *
 *		  - the modem configuration (audio_s, MODEM_*, LAYER2_*, V26_*)
 *		  - the packet sending path (layer2_send_frame, morse_send,
 *		    eas_send)
 *		  - the audio_put / audio_flush / dcd_change dispatchers, which
 *		    core code calls unqualified and which switch on GEN_PACKETS
 *		    between the real audio device and gen_packets' file output
 *
 *		Audio bytes are handed to the caller through the sink installed
 *		by GenPacketsSetAudioSink.
 *
 *------------------------------------------------------------------*/

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var GEN_PACKETS = false // Switch between fakes and reals at runtime

var modem audio_s
var g_morse_wpm = 0 /* Send morse code at this speed. */

// genPacketsAudioSink receives each audio byte produced while GEN_PACKETS is
// set. Installed by GenPacketsSetAudioSink; nil until then.
var genPacketsAudioSink func(c uint8) int

// GenPacketsRandMax is the largest value GenPacketsRand can return, matching
// the RAND_MAX the original C code was written against.
const GenPacketsRandMax = 0x7fffffff

var genPacketsRandSeed int32 = 1

// GenPacketsRand is the pseudo-random generator shared by the inter-frame gap
// in GenPacketsSendPacket and the noise injection in cmd/samoyed-gen_packets.
// Both must draw from this one sequence, in call order, or the decode counts
// asserted by test-scripts/check-modem* shift.
//
// Although the tests in `test-scripts` all call `atest` with an acceptable *range* of packets, the only way I could get
// them all to pass was by reimplementing this exact PRNG from Dire Wolf's gen_packets.c - all my attempts to use Go's
// `math/rand` resulted in decodes that would fall outside of the acceptable range. It's far from impossible that I
// somehow screwed up my use of `math/rand`, but I think it more likely that the tests depend on this exact PRNG
// implementation, which I should address at some point. /KG
// Yep, if seed is 1, tests pass; if seed is 2, test96f64 decodes 68 not 71+; if seed is 3 then test96f16 decodes 62 not 63+ /KG
func GenPacketsRand() int32 {
	genPacketsRandSeed = int32((uint32(genPacketsRandSeed)*1103515245 + 12345) & GenPacketsRandMax)

	return genPacketsRandSeed
}

// GenPacketsOptions carries the gen_packets command line options that shape the
// modem configuration. Pure CLI validation happens in cmd/samoyed-gen_packets;
// what's left here is what needs the unexported modem internals.
type GenPacketsOptions struct {
	Bitrate            string // -B, "EAS" for Emergency Alert System
	BitrateOverride    string // -b
	G3RUH              bool   // -g
	BPSK               bool   // -k
	Direwolf15Compat   bool   // -j
	MFJ2400Compat      bool   // -J
	MarkFrequency      int    // -m, 0 for unset
	SpaceFrequency     int    // -s, 0 for unset
	AudioSampleRate    int    // -r
	EightBitsPerSample bool   // -8
	TwoSoundChannels   bool   // -2
	Amplitude          int    // -a
	MorseWPM           int    // -M, 0 for unset
	FX25CheckBytes     int    // -X, 0 for unset
	IL2PNormal         int    // -I, negative for unset
	IL2PInverted       int    // -i, negative for unset
}

/*------------------------------------------------------------------
 *
 * Name:        GenPacketsConfigure
 *
 * Purpose:     Set up the modem and tone generation from the command options.
 *
 * Returns:     An error if the options describe a configuration we can't
 *		generate; the caller is expected to report it and exit.
 *
 *----------------------------------------------------------------*/

func GenPacketsConfigure(opts GenPacketsOptions) error { //nolint:funlen,gocyclo,maintidx // Faithful port of the option handling from gen_packets.c
	GEN_PACKETS = true // Use the _fake functions

	/*
	 * Set up default values for the modem.
	 */

	modem.adev[0].defined = 1
	modem.adev[0].num_channels = DEFAULT_NUM_CHANNELS       /* -2 stereo */
	modem.adev[0].samples_per_sec = DEFAULT_SAMPLES_PER_SEC /* -r option */
	modem.adev[0].bits_per_sample = DEFAULT_BITS_PER_SAMPLE /* -8 for 8 instead of 16 bits */

	for channel := range MAX_RADIO_CHANS {
		modem.achan[channel].modem_type = MODEM_AFSK         /* change with -g */
		modem.achan[channel].mark_freq = DEFAULT_MARK_FREQ   /* -m option */
		modem.achan[channel].space_freq = DEFAULT_SPACE_FREQ /* -s option */
		modem.achan[channel].baud = DEFAULT_BAUD             /* -b option */
	}

	modem.chan_medium[0] = MEDIUM_RADIO

	if opts.AudioSampleRate != DEFAULT_SAMPLES_PER_SEC {
		modem.adev[0].samples_per_sec = opts.AudioSampleRate

		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Audio sample rate set to %d samples / second.\n", modem.adev[0].samples_per_sec)
	}

	if opts.EightBitsPerSample {
		modem.adev[0].bits_per_sample = 8

		text_color_set(DW_COLOR_INFO)
		fmt.Printf("8 bits per audio sample rather than 16.\n")
	}

	if opts.TwoSoundChannels {
		modem.adev[0].num_channels = 2
		modem.chan_medium[1] = MEDIUM_RADIO

		text_color_set(DW_COLOR_INFO)
		fmt.Printf("2 channels of sound rather than 1.\n")
	}

	if opts.MorseWPM > 0 {
		//TODO: document this.
		// Why not base it on the destination field instead?
		g_morse_wpm = opts.MorseWPM

		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Morse code speed set to %d WPM.\n", g_morse_wpm)
	}

	if opts.Bitrate != "" {
		var bitrate int
		if opts.Bitrate == "EAS" {
			bitrate = 0xEA5EA5 // Special case handled below
		} else {
			bitrate, _ = strconv.Atoi(opts.Bitrate)
		}

		modem.achan[0].baud = bitrate
		fmt.Printf("Data rate set to %d bits / second.\n", modem.achan[0].baud)

		// We have similar logic in direwolf.c, config.c, gen_packets.c, and atest.c,
		// that need to be kept in sync.  Maybe it could be a common function someday.

		if modem.achan[0].baud == 100 { // What was this for?
			modem.achan[0].modem_type = MODEM_AFSK
			modem.achan[0].mark_freq = 1615
			modem.achan[0].space_freq = 1785
		} else if modem.achan[0].baud == 0xEA5EA5 {
			modem.achan[0].baud = 521 // Fine tuned later. 520.83333
			// Proper fix is to make this float.
			modem.achan[0].modem_type = MODEM_EAS
			modem.achan[0].mark_freq = 2083 // Ideally these should be floating point.
			modem.achan[0].space_freq = 1563
		} else if modem.achan[0].baud < 600 {
			modem.achan[0].modem_type = MODEM_AFSK
			modem.achan[0].mark_freq = 1600 // Typical for HF SSB
			modem.achan[0].space_freq = 1800
		} else if modem.achan[0].baud < 1800 {
			modem.achan[0].modem_type = MODEM_AFSK
			modem.achan[0].mark_freq = DEFAULT_MARK_FREQ
			modem.achan[0].space_freq = DEFAULT_SPACE_FREQ
		} else if modem.achan[0].baud < 3600 {
			modem.achan[0].modem_type = MODEM_QPSK
			modem.achan[0].mark_freq = 0
			modem.achan[0].space_freq = 0

			fmt.Printf("Using V.26 QPSK rather than AFSK.\n")

			if modem.achan[0].baud != 2400 {
				text_color_set(DW_COLOR_ERROR)
				fmt.Printf("Bit rate should be standard 2400 rather than specified %d.\n", modem.achan[0].baud)
			}
		} else if modem.achan[0].baud < 7200 {
			modem.achan[0].modem_type = MODEM_8PSK
			modem.achan[0].mark_freq = 0
			modem.achan[0].space_freq = 0

			fmt.Printf("Using V.27 8PSK rather than AFSK.\n")

			if modem.achan[0].baud != 4800 {
				text_color_set(DW_COLOR_ERROR)
				fmt.Printf("Bit rate should be standard 4800 rather than specified %d.\n", modem.achan[0].baud)
			}
		} else {
			modem.achan[0].modem_type = MODEM_SCRAMBLE

			text_color_set(DW_COLOR_INFO)
			fmt.Printf("Using scrambled baseband signal rather than AFSK.\n")
		}

		if modem.achan[0].baud != 100 && (modem.achan[0].baud < MIN_BAUD || modem.achan[0].baud > MAX_BAUD) {
			return fmt.Errorf("use a more reasonable bit rate in range of %d - %d", MIN_BAUD, MAX_BAUD)
		}
	}

	// These must be processed after -B option.
	if opts.MarkFrequency > 0 {
		modem.achan[0].mark_freq = opts.MarkFrequency
		fmt.Printf("Mark frequency set to %d Hz.\n", modem.achan[0].mark_freq)
	}

	if opts.SpaceFrequency > 0 {
		modem.achan[0].space_freq = opts.SpaceFrequency

		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Space frequency set to %d Hz.\n", modem.achan[0].space_freq)
	}

	if opts.BitrateOverride != "" {
		var bitrateOverride, _ = strconv.Atoi(opts.BitrateOverride)
		if bitrateOverride == 0 {
			return fmt.Errorf("invalid bitrate %s", opts.BitrateOverride)
		}

		modem.achan[0].baud = bitrateOverride
		fmt.Printf("Data rate set to %d bits / second.\n", modem.achan[0].baud)
	}

	if opts.G3RUH { /* -g for g3ruh scrambling */
		modem.achan[0].modem_type = MODEM_SCRAMBLE

		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Using G3RUH mode regardless of bit rate.\n")
	}

	if opts.BPSK { /* -k for BPSK */
		modem.achan[0].modem_type = MODEM_BPSK
		modem.achan[0].mark_freq = 0
		modem.achan[0].space_freq = 0
	}

	if opts.Direwolf15Compat { /* -j V.26 compatible with earlier direwolf. */
		modem.achan[0].v26_alternative = V26_A
		modem.achan[0].modem_type = MODEM_QPSK
		modem.achan[0].mark_freq = 0
		modem.achan[0].space_freq = 0
		modem.achan[0].baud = 2400
	}

	if opts.MFJ2400Compat { /* -J V.26 compatible with MFJ-2400. */
		modem.achan[0].v26_alternative = V26_B
		modem.achan[0].modem_type = MODEM_QPSK
		modem.achan[0].mark_freq = 0
		modem.achan[0].space_freq = 0
		modem.achan[0].baud = 2400
	}

	if modem.achan[0].modem_type == MODEM_QPSK && modem.achan[0].v26_alternative == V26_UNSPECIFIED {
		return errors.New("either -j or -J must be specified when using 2400 bps QPSK")
	}

	if opts.FX25CheckBytes > 0 {
		modem.achan[0].fx25_strength = opts.FX25CheckBytes
		modem.achan[0].layer2_xmit = LAYER2_FX25
	}

	if opts.IL2PNormal >= 0 {
		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Using IL2P normal polarity.\n")

		modem.achan[0].layer2_xmit = LAYER2_IL2P
		if opts.IL2PNormal > 0 {
			modem.achan[0].il2p_max_fec = 1
		}

		modem.achan[0].il2p_invert_polarity = 0 // normal
	}

	if opts.IL2PInverted >= 0 {
		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Using IL2P inverted polarity.\n")

		modem.achan[0].layer2_xmit = LAYER2_IL2P
		if opts.IL2PInverted > 0 {
			modem.achan[0].il2p_max_fec = 1
		}

		modem.achan[0].il2p_invert_polarity = 1 // invert for transmit
		if modem.achan[0].baud == 1200 {
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("Using -i with 1200 bps is a bad idea.  Use -I instead.\n")
		}
	}

	gen_tone_init(&modem, opts.Amplitude/2, true)
	morse_init(&modem, opts.Amplitude/2)
	dtmf_init(&modem, opts.Amplitude/2)

	// We don't have -d or -q options here.
	// Just use the default of minimal information.

	FX25Init(1)
	il2p_init(0) // There are no "-d" options so far but it could be handy here.

	if modem.adev[0].bits_per_sample != 8 && modem.adev[0].bits_per_sample != 16 {
		panic("assert(modem.adev[0].bits_per_sample == 8 || modem.adev[0].bits_per_sample == 16)")
	}

	if modem.adev[0].num_channels != 1 && modem.adev[0].num_channels != 2 {
		panic("assert(modem.adev[0].num_channels == 1 || modem.adev[0].num_channels == 2)")
	}

	if modem.adev[0].samples_per_sec < MIN_SAMPLES_PER_SEC || modem.adev[0].samples_per_sec > MAX_SAMPLES_PER_SEC {
		panic("assert(modem.adev[0].samples_per_sec >= MIN_SAMPLES_PER_SEC && modem.adev[0].samples_per_sec <= MAX_SAMPLES_PER_SEC)")
	}

	return nil
}

// GenPacketsAudioFormat returns the audio properties the caller needs to write
// a .WAV header, as settled by GenPacketsConfigure.
func GenPacketsAudioFormat() (numChannels int, samplesPerSec int, bitsPerSample int) {
	return modem.adev[0].num_channels, modem.adev[0].samples_per_sec, modem.adev[0].bits_per_sample
}

// GenPacketsSetAudioSink installs the function that receives each generated
// audio byte. It must be called before any GenPacketsSendPacket.
func GenPacketsSetAudioSink(sink func(c uint8) int) {
	genPacketsAudioSink = sink
}

// GenPacketsBaud returns the configured data rate, which the caller needs to
// pick a noise level.
func GenPacketsBaud() int {
	return modem.achan[0].baud
}

// GenPacketsIsEAS reports whether we're generating Emergency Alert System
// audio, which uses a different set of built-in messages.
func GenPacketsIsEAS() bool {
	return modem.achan[0].modem_type == MODEM_EAS
}

// GenPacketsSetSpeed changes the data rate and re-initialises tone generation,
// for the -v variable speed sweep.
func GenPacketsSetSpeed(baud int, amplitude int) {
	modem.achan[0].baud = baud
	gen_tone_init(&modem, amplitude/2, true)
}

/*------------------------------------------------------------------
 *
 * Name:        GenPacketsSendPacket
 *
 * Purpose:     Convert one TNC2 monitor format line to audio.
 *
 * Inputs:	str	- The packet, in TNC2 monitor format.
 *
 *----------------------------------------------------------------*/

func GenPacketsSendPacket(str string) {
	if g_morse_wpm > 0 {
		// Why not use the destination field instead of command line option?
		// For one thing, this is not in TNC-2 monitor format.
		morse_send(0, str, g_morse_wpm, 100, 100)
	} else if modem.achan[0].modem_type == MODEM_EAS {
		// Generate EAS SAME signal FOR RESEARCH AND TESTING ONLY!!!
		// There could be legal consequences for sending unauhorized SAME
		// over the radio so don't do it!

		// I'm expecting to see TNC 2 monitor format.
		// The source and destination are ignored.
		// The optional destination SSID is the number of times to repeat.
		// The user defined data type indicator can optionally be used
		// for compatibility with how it is received and presented to client apps.
		// Examples:
		//	X>X-3:{DEZCZC-WXR-RWT-033019-033017-033015-033013-033011-025011-025017-033007-033005-033003-033001-025009-025027-033009+0015-1691525-KGYX/NWS-
		//	X>X:NNNN
		var pp = AX25FromText(str, true)
		if pp == nil {
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("\"%s\" is not valid TNC2 monitoring format.\n", str)

			return
		}

		var pinfo = AX25GetInfo(pp)
		if len(pinfo) >= 3 && strings.HasPrefix(string(pinfo), "{DE") {
			pinfo = pinfo[3:]
		}

		var repeat = ax25_get_ssid(pp, AX25_DESTINATION)
		if repeat == 0 {
			repeat = 1
		}

		eas_send(0, pinfo, repeat, 500, 500)
		AX25Delete(pp)
	} else {
		var pp = AX25FromText(str, true)
		if pp == nil {
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("\"%s\" is not valid TNC2 monitoring format.\n", str)

			return
		}

		// If stereo, put same thing in each channel.

		for c := range modem.adev[0].num_channels {
			var samples_per_symbol int

			// Insert random amount of quiet time.

			switch modem.achan[c].modem_type {
			case MODEM_QPSK:
				samples_per_symbol = modem.adev[0].samples_per_sec / (modem.achan[c].baud / 2)
			case MODEM_8PSK:
				samples_per_symbol = modem.adev[0].samples_per_sec / (modem.achan[c].baud / 3)
			case MODEM_BPSK:
				samples_per_symbol = modem.adev[0].samples_per_sec / modem.achan[c].baud
			default:
				samples_per_symbol = modem.adev[0].samples_per_sec / modem.achan[c].baud
			}

			// Provide enough time for the DCD to drop.
			// Then throw in a random amount of time so that receiving
			// DPLL will need to adjust to a new phase.

			var n = int(float64(samples_per_symbol) * (32 + float64(GenPacketsRand())/float64(GenPacketsRandMax)))

			for range n {
				gen_tone_put_sample(c, 0, 0)
			}

			layer2_preamble_postamble(c, 32, false, &modem)
			layer2_send_frame(c, pp, false, &modem)
			layer2_preamble_postamble(c, 2, true, &modem)
		}

		AX25Delete(pp)
	}
}

/*------------------------------------------------------------------
 *
 * Name:        audio_put
 *
 * Purpose:     Send one byte to the audio output file.
 *
 * Inputs:	c	- One byte in range of 0 - 255.
 *
 * Returns:     Normally non-negative.
 *              -1 for any type of error.
 *
 * Description:	The caller must deal with the details of mono/stereo
 *		and number of bytes per sample.
 *
 *----------------------------------------------------------------*/

func audio_put_fake(_ int, c uint8) int {
	if genPacketsAudioSink == nil {
		return -1
	}

	return genPacketsAudioSink(c)
} /* end audio_put */

func audio_put(a int, c uint8) int { //nolint:unparam
	if GEN_PACKETS {
		return audio_put_fake(a, c)
	} else {
		return audio_put_real(a, c)
	}
}

func audio_flush_fake(a int) int {
	return 0
}

func audio_flush(a int) int {
	if GEN_PACKETS {
		return audio_flush_fake(a)
	} else {
		return audio_flush_real(a)
	}
}

// To keep dtmf.c happy.
func dcd_change_fake(channel int, subchan int, slice int, state int) {
}

func dcd_change(channel int, subchan int, slice int, state int) {
	if GEN_PACKETS {
		dcd_change_fake(channel, subchan, slice, state)
	} else {
		hdlcReceiver.DCDChange(channel, subchan, slice, state)
	}
}
