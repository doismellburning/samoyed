//nolint:gochecknoglobals
package direwolf

/*------------------------------------------------------------------
 *
 * Name:	gen_packets
 *
 * Purpose:	Test program for generating AX.25 frames.
 *
 * Description:	Given messages are converted to audio and written
 *		to a .WAV type audio file.
 *
 * Bugs:	Most options are implemented for only one audio channel.
 *
 * Examples:	Different speeds:
 *
 *			gen_packets -o z1.wav
 *			atest z1.wav
 *
 *			gen_packets -B 300 -o z3.wav
 *			atest -B 300 z3.wav
 *
 *			gen_packets -B 9600 -o z9.wav
 *			atest -B 300 z9.wav
 *
 *		User-defined content:
 *
 *			echo "WB2OSZ>APDW12:This is a test" | gen_packets -o z.wav -
 *			atest z.wav
 *
 *			echo "WB2OSZ>APDW12:Test line 1" >  z.txt
 *			echo "WB2OSZ>APDW12:Test line 2" >> z.txt
 *			echo "WB2OSZ>APDW12:Test line 3" >> z.txt
 *			gen_packets -o z.wav z.txt
 *			atest z.wav
 *
 *		With artificial noise added:
 *
 *			gen_packets -n 100 -o z2.wav
 *			atest z2.wav
 *
 *		Variable speed. e.g. 95% to 105% of normal speed.
 *		Required parameter is max % below and above normal.
 *		Optionally specify step other than 0.1%.
 *		Used to test how tolerant TNCs are to senders not
 *		not using exactly the right baud rate.
 *
 *			gen_packets -v 5
 *			gen_packets -v 5,0.5
 *
 *------------------------------------------------------------------*/

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/doismellburning/samoyed/internal/wavwrite"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
)

const MY_RAND_MAX = 0x7fffffff

var modem audio_s
var g_morse_wpm = 0 /* Send morse code at this speed. */
var g_add_noise = false
var g_noise_level float64 = 0

var genPacketsRandSeed int32 = 1

// Although the tests in `test-scripts` all call `atest` with an acceptable *range* of packets, the only way I could get
// them all to pass was by reimplementing this exact PRNG from Dire Wolf's gen_packets.c - all my attempts to use Go's
// `math/rand` resulted in decodes that would fall outside of the acceptable range. It's far from impossible that I
// somehow screwed up my use of `math/rand`, but I think it more likely that the tests depend on this exact PRNG
// implementation, which I should address at some point. /KG
// Yep, if seed is 1, tests pass; if seed is 2, test96f64 decodes 68 not 71+; if seed is 3 then test96f16 decodes 62 not 63+ /KG
func genPacketsRand() int32 {
	genPacketsRandSeed = int32((uint32(genPacketsRandSeed)*1103515245 + 12345) & MY_RAND_MAX)

	return genPacketsRandSeed
}

func GenPacketsMain() {
	modem = *genPacketsDefaultAudio()

	/*
	 * Set up other default values.
	 */
	var packet_count = 0

	var modemFlags = addGenPacketsModemFlags(pflag.CommandLine)
	var noisyPacketCount = pflag.IntP("noisy-packet-count", "n", 0, "Generate specified number of frames with increasing noise.")
	var packetCount = pflag.IntP("packet-count", "N", 0, "Generate specified number of frames.")
	var amplitude = pflag.IntP("amplitude", "a", 50, "Signal amplitude in range of 0 - 200%.") // 100% is actually half of the digital signal range so we have some headroom for adding noise, etc.
	var audioSampleRate = pflag.IntP("audio-sample-rate", "r", DEFAULT_SAMPLES_PER_SEC, "Audio sample rate.")
	// var leadingZeros = pflag.IntP("leading-zeros", "z", 12, "Number of leading zero bits before frame. 12 is .01 seconds at 1200 bits/sec.")
	// -z option TODO: not implemented, should replace with txdelay frames.
	var eightBitsPerSample = pflag.BoolP("eight-bps", "8", false, "8 bit audio rather than 16.")
	var twoSoundChannels = pflag.BoolP("two-sound-channels", "2", false, "2 channels (stereo) audio rather than one channel.")
	var outputFile = pflag.StringP("output-file", "o", "", "Send output to .wav file.")
	var morseWPM = pflag.IntP("morse-wpm", "M", 0, "Send Morse at this speed.")
	var variableSpeedStr = pflag.StringP("variable-speed", "v", "", "max[,incr] Variable speed with specified maximum error and increment.")
	var help = pflag.BoolP("help", "h", false, "Display help text.")

	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s - Generate audio file for AX.25 frames.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [file]\n", os.Args[0])
		pflag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "An optional file may be specified to provide messages other than\n")
		fmt.Fprintf(os.Stderr, "the default built-in message. The format should correspond to\n")
		fmt.Fprintf(os.Stderr, "the standard packet monitoring representation such as,\n\n")
		fmt.Fprintf(os.Stderr, "    WB2OSZ-1>APDW12,WIDE2-2:!4237.14NS07120.83W#\n")
		fmt.Fprintf(os.Stderr, "User defined content can't be used with -n option.\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Example:  gen_packets -o x.wav \n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "    With all defaults, a built-in test message is generated\n")
		fmt.Fprintf(os.Stderr, "    with standard Bell 202 tones used for packet radio on ordinary\n")
		fmt.Fprintf(os.Stderr, "    VHF FM transceivers.\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Example:  gen_packets -o x.wav -g -b 9600\n")
		fmt.Fprintf(os.Stderr, "Shortcut: gen_packets -o x.wav -B 9600\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "    9600 baud mode.\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Example:  gen_packets -o x.wav -m 1600 -s 1800 -b 300\n")
		fmt.Fprintf(os.Stderr, "Shortcut: gen_packets -o x.wav -B 300\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "    200 Hz shift, 300 baud, suitable for HF SSB transceiver.\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Example:  echo -n \"WB2OSZ>WORLD:Hello, world!\" | gen_packets -a 25 -o x.wav -\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "    Read message from stdin and put quarter volume sound into the file x.wav.\n")
	}

	// !!! PARSE !!!
	pflag.Parse()

	if *help {
		pflag.Usage()
		os.Exit(1)
	}

	if *amplitude > 0 {
		fmt.Printf("Amplitude set to %d%%.\n", *amplitude)

		if *amplitude < 0 || *amplitude > 200 {
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("Amplitude must be in range of 0 to 200, not %d.\n", *amplitude)
			os.Exit(1)
		}
	}

	if *noisyPacketCount > 0 && *packetCount > 0 {
		text_color_set(DW_COLOR_ERROR)
		fmt.Printf("Cannot choose both noisy packets (-n) and noiseless (-N) packets - pick at most one.\n")
		os.Exit(1)
	} else if *noisyPacketCount > 0 {
		packet_count = *noisyPacketCount
		g_add_noise = true
	} else if *packetCount > 0 {
		packet_count = *packetCount
		g_add_noise = false
	}

	if *audioSampleRate != DEFAULT_SAMPLES_PER_SEC {
		modem.adev[0].samples_per_sec = *audioSampleRate

		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Audio sample rate set to %d samples / second.\n", modem.adev[0].samples_per_sec)

		if modem.adev[0].samples_per_sec < MIN_SAMPLES_PER_SEC || modem.adev[0].samples_per_sec > MAX_SAMPLES_PER_SEC {
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("Use a more reasonable audio sample rate in range of %d - %d, not %d.\n",
				MIN_SAMPLES_PER_SEC, MAX_SAMPLES_PER_SEC, *audioSampleRate)
			os.Exit(1)
		}
	}

	// The demodulator needs a few for the clock recovery PLL.
	// We don't want to be here all day either.
	// We can't translate to time yet because the data bit rate
	// could be changed later.
	/* Not implemented
	const MIN_LEADING_ZEROS = 8
	const MAX_LEADING_ZEROS = 12000
	if *leadingZeros < MIN_LEADING_ZEROS || *leadingZeros > MAX_LEADING_ZEROS {
		text_color_set(DW_COLOR_ERROR)
		fmt.Printf("Leading zeros should be between %d and %d, not %d.\n", MIN_LEADING_ZEROS, MAX_LEADING_ZEROS, *leadingZeros)
		os.Exit(1)
	}
	*/

	if *eightBitsPerSample {
		modem.adev[0].bits_per_sample = 8

		text_color_set(DW_COLOR_INFO)
		fmt.Printf("8 bits per audio sample rather than 16.\n")
	}

	if *twoSoundChannels {
		modem.adev[0].num_channels = 2
		modem.chan_medium[1] = MEDIUM_RADIO

		text_color_set(DW_COLOR_INFO)
		fmt.Printf("2 channels of sound rather than 1.\n")
	}

	if *morseWPM > 0 {
		//TODO: document this.
		// Why not base it on the destination field instead?
		g_morse_wpm = *morseWPM

		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Morse code speed set to %d WPM.\n", g_morse_wpm)

		if g_morse_wpm < 5 || g_morse_wpm > 50 {
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("Morse code speed must be in range of 5 to 50 WPM, not %d.\n", g_morse_wpm)
			os.Exit(1)
		}
	}

	var variable_speed_max_error float64 = 0 // both in percent
	var variable_speed_increment = 0.1

	if *variableSpeedStr != "" {
		var maxError, increment, found = strings.Cut(*variableSpeedStr, ",")

		variable_speed_max_error, _ = strconv.ParseFloat(maxError, 64)
		if found {
			variable_speed_increment, _ = strconv.ParseFloat(increment, 64)
		}
	}

	var modemErr = modemFlags.apply(&modem.achan[0])
	if modemErr != nil {
		text_color_set(DW_COLOR_ERROR)
		fmt.Fprintf(os.Stderr, "%s\n", modemErr)
		pflag.Usage()
		os.Exit(1)
	}

	/*
	 * Open the output file.
	 */

	if *outputFile == "" {
		text_color_set(DW_COLOR_ERROR)
		fmt.Printf("ERROR: The -o output file option must be specified.\n")
		pflag.Usage()
		os.Exit(1)
	}

	var adevErr = modem.adev[0].validate()
	if adevErr != nil {
		logrus.WithError(adevErr).Error("Unusable audio device configuration")
		os.Exit(1)
	}

	var sink = audio_file_open(*outputFile, &modem)

	if sink == nil {
		text_color_set(DW_COLOR_ERROR)
		fmt.Printf("ERROR - Can't open output file.\n")
		os.Exit(1)
	}

	gen_tone_init(&modem, *amplitude/2, sink)
	morse_init(&modem, *amplitude/2)
	dtmf_init(&modem, *amplitude/2)

	// We don't have -d or -q options here.
	// Just use the default of minimal information.

	FX25Init(1)
	il2p_init(0) // There are no "-d" options so far but it could be handy here.

	/*
	 * Get user packets(s) from file or stdin if specified.
	 * "-n" option is ignored in this case.
	 */

	if len(pflag.Args()) > 0 {
		if len(pflag.Args()) > 1 {
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("Warning: File(s) beyond the first are ignored.\n")
		}

		var arg = pflag.Args()[0]
		var input_fp *os.File

		if arg == "-" {
			text_color_set(DW_COLOR_INFO)
			fmt.Printf("Reading from stdin ...\n")

			input_fp = os.Stdin
		} else {
			var err error

			input_fp, err = os.Open(arg) //nolint:gosec // We expect to read from a user-supplied file from CLI
			if err != nil {
				text_color_set(DW_COLOR_ERROR)
				fmt.Printf("Can't open %s for read: %s\n", arg, err)
				os.Exit(1)
			}
			defer input_fp.Close()

			text_color_set(DW_COLOR_INFO)
			fmt.Printf("Reading from %s ...\n", arg)
		}

		var scanner = bufio.NewScanner(input_fp)
		for scanner.Scan() {
			var str = scanner.Text()

			text_color_set(DW_COLOR_REC)
			fmt.Printf("%s", str)
			send_packet(str)
		}

		audio_file_close(sink)

		return
	}

	/*
	 * Otherwise, use the built in packets.
	 */
	text_color_set(DW_COLOR_INFO)
	fmt.Printf("built in message...\n")

	//
	// Generate packets with variable speed.
	// This overrides any other number of packets or adding noise.
	//

	if variable_speed_max_error != 0 {
		var normal_speed = modem.achan[0].baud

		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Variable speed.\n")

		for speed_error := -variable_speed_max_error; speed_error <= variable_speed_max_error+0.001; speed_error += variable_speed_increment {
			// Baud is int so we get some roundoff.  Make it real?
			modem.achan[0].baud = int(float64(normal_speed) * (1. + speed_error/100.))
			gen_tone_init(&modem, *amplitude/2, sink)

			var stemp = fmt.Sprintf("WB2OSZ-15>TEST:, speed %+0.1f%%  The quick brown fox jumps over the lazy dog!", speed_error)
			send_packet(stemp)
		}
	} else if packet_count > 0 {
		/*
		 * Generate packets with increasing noise level.
		 * Would probably be better to record real noise from a radio but
		 * for now just use a random number generator.
		 */
		for i := 1; i <= packet_count; i++ {
			if modem.achan[0].baud < 600 {
				/* e.g. 300 bps AFSK - About 2/3 should be decoded properly. */
				g_noise_level = float64(*amplitude) * .0048 * (float64(i) / float64(packet_count))
			} else if modem.achan[0].baud < 1800 {
				/* e.g. 1200 bps AFSK - About 2/3 should be decoded properly. */
				g_noise_level = float64(*amplitude) * .0023 * (float64(i) / float64(packet_count))
			} else if modem.achan[0].baud < 3600 {
				/* e.g. 2400 bps QPSK - T.B.D. */
				g_noise_level = float64(*amplitude) * .0015 * (float64(i) / float64(packet_count))
			} else if modem.achan[0].baud < 7200 {
				/* e.g. 4800 bps - T.B.D. */
				g_noise_level = float64(*amplitude) * .0007 * (float64(i) / float64(packet_count))
			} else {
				/* e.g. 9600 */
				g_noise_level = 0.33 * (float64(*amplitude) / 200.0) * (float64(i) / float64(packet_count))
				// temp test
				// g_noise_level = 0.20 * (amplitude / 200.0) * (float64(i) / float64(packet_count));
			}

			var stemp = fmt.Sprintf("WB2OSZ-15>TEST:,The quick brown fox jumps over the lazy dog!  %04d of %04d", i, packet_count)
			send_packet(stemp)
		}
	} else {
		// This should send a total of 6.
		// Note that sticking in the user defined type {DE is optional.
		if modem.achan[0].modem_type == MODEM_EAS {
			send_packet("X>X-3:{DEZCZC-WXR-RWT-033019-033017-033015-033013-033011-025011-025017-033007-033005-033003-033001-025009-025027-033009+0015-1691525-KGYX/NWS-")
			send_packet("X>X-2:{DENNNN")
			send_packet("X>X:NNNN")
		} else {
			/*
			 * Builtin default 4 packets.
			 */
			send_packet("WB2OSZ-15>TEST:,The quick brown fox jumps over the lazy dog!  1 of 4")
			send_packet("WB2OSZ-15>TEST:,The quick brown fox jumps over the lazy dog!  2 of 4")
			send_packet("WB2OSZ-15>TEST:,The quick brown fox jumps over the lazy dog!  3 of 4")
			send_packet("WB2OSZ-15>TEST:,The quick brown fox jumps over the lazy dog!  4 of 4")
		}
	}

	audio_file_close(sink)
}

/*------------------------------------------------------------------
 *
 * Name:        audio_file_open
 *
 * Purpose:     Open a .WAV format file for output.
 *
 * Inputs:      fname		- Name of .WAV file to create.
 *
 *		pa		- Address of structure of type audio_s.
 *
 *				The fields that we care about are:
 *					num_channels
 *					samples_per_sec
 *					bits_per_sample
 *				If zero, reasonable defaults will be provided.
 *
 * Returns:     Where to send the samples, or nil for failure.
 *
 *----------------------------------------------------------------*/

func audio_file_open(fname string, pa *audio_s) *wavFileSink {
	/*
	 * Fill in defaults for any missing values.
	 */
	if pa.adev[0].num_channels == 0 {
		pa.adev[0].num_channels = DEFAULT_NUM_CHANNELS
	}

	if pa.adev[0].samples_per_sec == 0 {
		pa.adev[0].samples_per_sec = DEFAULT_SAMPLES_PER_SEC
	}

	if pa.adev[0].bits_per_sample == 0 {
		pa.adev[0].bits_per_sample = DEFAULT_BITS_PER_SAMPLE
	}

	/*
	 * Write the file header.  Don't know length yet.
	 */
	var format = wavwrite.Format{
		NumChannels:   pa.adev[0].num_channels,
		SamplesPerSec: pa.adev[0].samples_per_sec,
		BitsPerSample: pa.adev[0].bits_per_sample,
	}

	var w, err = wavwrite.Create(fname, format)
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		fmt.Printf("%s\n", err)

		return nil
	}

	return newWAVFileSink(w)
} /* end audio_open */

/*------------------------------------------------------------------
 *
 * Name:        audio_file_close
 *
 * Purpose:     Close the audio output file.
 *
 * Returns:     Normally non-negative.
 *              -1 for any type of error.
 *
 *
 * Description:	Must go back to beginning of file and fill in the
 *		size of the data.
 *
 *----------------------------------------------------------------*/

func audio_file_close(sink *wavFileSink) int { //nolint:unparam
	if sink == nil {
		return (-1)
	}

	// Close goes back and fixes up the lengths in the header for us.
	var err = sink.w.Close()
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		fmt.Printf("%s\n", err)

		return (-1)
	}

	return (0)
} /* end audio_close */

func send_packet(str string) {
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

			var n = int(float64(samples_per_symbol) * (32 + float64(genPacketsRand())/float64(MY_RAND_MAX)))

			for range n {
				gen_tone_put_sample(c, 0, 0)
			}

			layer2_preamble_postamble(c, 32, false, &modem)
			layer2_send_frame(c, pp, false, &modem)
			layer2_preamble_postamble(c, 2, true, &modem)
		}
	}
}

// wavFileSink is the AudioSink for gen_packets: it writes the samples to the
// .WAV file audio_file_open created, with noise added when asked for.
type wavFileSink struct {
	w *wavwrite.Writer

	// Half of a 16 bit sample, waiting for its upper byte before noise can be
	// added to it and it can be written out.
	sample16        int16
	sample16Pending bool
}

func newWAVFileSink(w *wavwrite.Writer) *wavFileSink {
	var sink = new(wavFileSink)
	sink.w = w

	return sink
}

/*------------------------------------------------------------------
 *
 * Name:        Put
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

func (sink *wavFileSink) Put(_ int, c uint8) int {
	if g_add_noise {
		if !sink.sample16Pending {
			sink.sample16 = int16(c) /* save lower byte. */
			sink.sample16Pending = true

			return int(c)
		} else {
			sink.sample16 |= int16(c) << 8 /* insert upper byte. */
			sink.sample16Pending = false
			var s = int32(sink.sample16) // sign extend.

			/* Add random noise to the signal. */
			/* r should be in range of -1 .. +1. */

			var r = (float64(genPacketsRand()) - float64(MY_RAND_MAX)/2.0) / (float64(MY_RAND_MAX) / 2.0)

			s += int32(5 * r * g_noise_level * 32767)

			if s > 32767 {
				s = 32767
			}

			if s < -32767 {
				s = -32767
			}

			var n, writeErr = sink.w.Write([]byte{byte(s & 0xff), byte(s>>8) & 0xff})
			if writeErr != nil {
				return -1
			}

			return n
		}
	} else {
		var writeErr = sink.w.WriteByte(c)
		if writeErr != nil {
			return -1
		}

		return 1
	}
} /* end Put */

// Flush has nothing to do: the file is written as the samples arrive.
func (sink *wavFileSink) Flush(_ int) int {
	return 0
}

// genPacketsModemFlags are the command line options that set up the modulator.
type genPacketsModemFlags struct {
	bitrate          *string
	bitrateOverride  *string
	g3ruh            *bool
	bpsk             *bool
	direwolf15compat *bool
	mfj2400compat    *bool
	mark             *int
	space            *int
	fx25CheckBytes   *int
	il2pNormal       *int
	il2pInverted     *int
	il2pVersion      *string
}

func addGenPacketsModemFlags(fs *pflag.FlagSet) *genPacketsModemFlags {
	var f = new(genPacketsModemFlags)
	f.bitrate = fs.StringP("bitrate", "B", strconv.Itoa(DEFAULT_BAUD), `Bits / second for data.  Proper modem automatically selected for speed.
300 bps defaults to AFSK tones of 1600 & 1800.
1200 bps uses AFSK tones of 1200 & 2200.
2400 bps uses QPSK based on V.26 standard.
4800 bps uses 8PSK based on V.27 standard.
9600 bps and up uses K9NG/G3RUH standard.
AIS for ship Automatic Identification System.
EAS for Emergency Alert System (EAS) Specific Area Message Encoding (SAME).`)
	f.bitrateOverride = fs.StringP("bitrate-override", "b", "", "Bits / second for data.")
	f.g3ruh = fs.BoolP("g3ruh", "g", false, "Use G3RUH modem rather than default for data rate.")
	f.bpsk = fs.BoolP("bpsk", "k", false, "Use BPSK modem rather than default for data rate.")
	f.direwolf15compat = fs.BoolP("direwolf-15-compat", "j", false, "2400 bps QPSK compatible with direwolf <= 1.5.")
	f.mfj2400compat = fs.BoolP("mfj-2400-compat", "J", false, "2400 bps QPSK compatible with MFJ-2400.")
	f.mark = fs.IntP("mark", "m", 0, "Mark frequency.")
	f.space = fs.IntP("space", "s", 0, "Space frequency.")
	f.fx25CheckBytes = fs.IntP("fx25-check-bytes", "X", 0, "1 to enable FX.25 transmit.  16, 32, 64 for specific number of check bytes.")
	f.il2pNormal = fs.IntP("il2p", "I", -1, "Enable IL2P transmit.  n=1 is recommended.  0 asks for weaker FEC, which only v0.4 has (see --il2p-version).")
	f.il2pInverted = fs.IntP("il2p-inverted", "i", -1, "Enable IL2P transmit, inverted polarity.  n=1 is recommended.  0 asks for weaker FEC, which only v0.4 has (see --il2p-version).")
	f.il2pVersion = fs.String("il2p-version", "0.6", `IL2P version to transmit.
    0.6     - 16 parity symbols per payload block, that bit reserved.  (default)
    0.4     - The header FEC Level bit says which FEC level is in use.
    compat  - Same as 0.4.`)

	return f
}

// apply sets up achan from the options, once they have been parsed.
func (f *genPacketsModemFlags) apply(achan *achan_param_s) error {
	if *f.bitrate != "" {
		var bitrate, bitrateParseErr = strconv.Atoi(*f.bitrate)
		if strings.EqualFold(*f.bitrate, "AIS") {
			bitrate = 0xA15A15 // Special cases handled below
		} else if strings.EqualFold(*f.bitrate, "EAS") {
			bitrate = 0xEA5EA5
		} else if bitrateParseErr != nil {
			return fmt.Errorf("invalid bitrate (should be an integer or 'AIS' or 'EAS'): %s", *f.bitrate)
		}

		achan.baud = bitrate
		fmt.Printf("Data rate set to %d bits / second.\n", achan.baud)

		// We have similar logic in direwolf.c, config.c, gen_packets.c, and atest.c,
		// that need to be kept in sync.  Maybe it could be a common function someday.

		if achan.baud == 0xEA5EA5 {
			achan.baud = 521 // Fine tuned later. 520.83333
			// Proper fix is to make this float.
			achan.modem_type = MODEM_EAS
			achan.mark_freq = 2083 // Ideally these should be floating point.
			achan.space_freq = 1563
			achan.profiles = "A"
		} else if achan.baud == 0xA15A15 {
			achan.baud = 9600
			achan.modem_type = MODEM_AIS
			achan.mark_freq = 0
			achan.space_freq = 0
		} else if achan.baud < 600 {
			achan.modem_type = MODEM_AFSK
			achan.mark_freq = 1600 // Typical for HF SSB
			achan.space_freq = 1800
		} else if achan.baud < 1800 {
			achan.modem_type = MODEM_AFSK
			achan.mark_freq = DEFAULT_MARK_FREQ
			achan.space_freq = DEFAULT_SPACE_FREQ
		} else if achan.baud < 3600 {
			achan.modem_type = MODEM_QPSK
			achan.mark_freq = 0
			achan.space_freq = 0

			fmt.Printf("Using V.26 QPSK rather than AFSK.\n")

			if achan.baud != 2400 {
				text_color_set(DW_COLOR_ERROR)
				fmt.Printf("Bit rate should be standard 2400 rather than specified %d.\n", achan.baud)
			}
		} else if achan.baud < 7200 {
			achan.modem_type = MODEM_8PSK
			achan.mark_freq = 0
			achan.space_freq = 0

			fmt.Printf("Using V.27 8PSK rather than AFSK.\n")

			if achan.baud != 4800 {
				text_color_set(DW_COLOR_ERROR)
				fmt.Printf("Bit rate should be standard 4800 rather than specified %d.\n", achan.baud)
			}
		} else {
			achan.modem_type = MODEM_SCRAMBLE
			achan.mark_freq = 0
			achan.space_freq = 0

			text_color_set(DW_COLOR_INFO)
			fmt.Printf("Using scrambled baseband signal rather than AFSK.\n")
		}

		if achan.baud < MIN_BAUD || achan.baud > MAX_BAUD {
			return fmt.Errorf("use a more reasonable bit rate in range of %d - %d", MIN_BAUD, MAX_BAUD)
		}
	}

	// These must be processed after -B option.
	if *f.mark > 0 {
		achan.mark_freq = *f.mark
		fmt.Printf("Mark frequency set to %d Hz.\n", achan.mark_freq)

		if achan.mark_freq < 300 || achan.mark_freq > 3000 {
			return fmt.Errorf("use a more reasonable mark frequency in range of 300 - 3000, not %d", *f.mark)
		}
	}

	if *f.space > 0 {
		achan.space_freq = *f.space

		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Space frequency set to %d Hz.\n", achan.space_freq)

		if achan.space_freq < 300 || achan.space_freq > 3000 {
			return fmt.Errorf("use a more reasonable space frequency in range of 300 - 3000, not %d", *f.space)
		}
	}

	if *f.bitrateOverride != "" {
		var bitrateOverride, _ = strconv.Atoi(*f.bitrateOverride)
		if bitrateOverride == 0 {
			return fmt.Errorf("invalid bitrate %s", *f.bitrateOverride)
		}

		achan.baud = bitrateOverride
		fmt.Printf("Data rate set to %d bits / second.\n", achan.baud)
	}

	if *f.g3ruh { /* -g for g3ruh scrambling */
		achan.modem_type = MODEM_SCRAMBLE
		achan.mark_freq = 0
		achan.space_freq = 0

		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Using G3RUH mode regardless of bit rate.\n")
	}

	if *f.bpsk { /* -k for BPSK */
		achan.modem_type = MODEM_BPSK
		achan.mark_freq = 0
		achan.space_freq = 0
	}

	if *f.direwolf15compat { /* -j V.26 compatible with earlier direwolf. */
		achan.v26_alternative = V26_A
		achan.modem_type = MODEM_QPSK
		achan.mark_freq = 0
		achan.space_freq = 0
		achan.baud = 2400
	}

	if *f.mfj2400compat { /* -J V.26 compatible with MFJ-2400. */
		achan.v26_alternative = V26_B
		achan.modem_type = MODEM_QPSK
		achan.mark_freq = 0
		achan.space_freq = 0
		achan.baud = 2400
	}

	if achan.modem_type == MODEM_QPSK && achan.v26_alternative == V26_UNSPECIFIED {
		return errors.New("either -j or -J must be specified when using 2400 bps QPSK")
	}

	if *f.fx25CheckBytes > 0 {
		if *f.il2pNormal >= 0 || *f.il2pInverted >= 0 {
			return errors.New("can't mix -X with -I or -i")
		}

		achan.fx25_strength = *f.fx25CheckBytes
		achan.layer2_xmit = LAYER2_FX25
	}

	if *f.il2pNormal >= 0 && *f.il2pInverted >= 0 {
		return errors.New("can't use both -I and -i at the same time")
	}

	var il2p_version, il2p_version_ok = il2p_parse_version(*f.il2pVersion)
	if !il2p_version_ok {
		return fmt.Errorf("invalid IL2P version %s.  Expected 0.4, 0.6, or compat", *f.il2pVersion)
	}

	achan.il2p_version = il2p_version

	if *f.il2pNormal >= 0 {
		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Using IL2P normal polarity.\n")

		achan.layer2_xmit = LAYER2_IL2P
		if *f.il2pNormal > 0 {
			achan.il2p_max_fec = 1
		}

		achan.il2p_invert_polarity = 0 // normal
	}

	if *f.il2pInverted >= 0 {
		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Using IL2P inverted polarity.\n")

		achan.layer2_xmit = LAYER2_IL2P
		if *f.il2pInverted > 0 {
			achan.il2p_max_fec = 1
		}

		achan.il2p_invert_polarity = 1 // invert for transmit
		if achan.baud == 1200 {
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("Using -i with 1200 bps is a bad idea.  Use -I instead.\n")
		}
	}

	return nil
}

// genPacketsDefaultAudio is the audio configuration gen_packets starts from, before its options.
func genPacketsDefaultAudio() *audio_s {
	var audio = new(audio_s)

	/*
	 * Set up default values for the modem.
	 */

	audio.adev[0].defined = 1
	audio.adev[0].num_channels = DEFAULT_NUM_CHANNELS       /* -2 stereo */
	audio.adev[0].samples_per_sec = DEFAULT_SAMPLES_PER_SEC /* -r option */
	audio.adev[0].bits_per_sample = DEFAULT_BITS_PER_SAMPLE /* -8 for 8 instead of 16 bits */

	for channel := range MAX_RADIO_CHANS {
		audio.achan[channel].modem_type = MODEM_AFSK         /* change with -g */
		audio.achan[channel].mark_freq = DEFAULT_MARK_FREQ   /* -m option */
		audio.achan[channel].space_freq = DEFAULT_SPACE_FREQ /* -s option */
		audio.achan[channel].baud = DEFAULT_BAUD             /* -b option */
	}

	audio.chan_medium[0] = MEDIUM_RADIO

	return audio
}
