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
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

type wav_header struct { /* .WAV file header. */
	riff            [4]byte /* "RIFF" */
	filesize        int32   /* file length - 8 */
	wave            [4]byte /* "WAVE" */
	fmt             [4]byte /* "fmt " */
	fmtsize         int32   /* 16. */
	wformattag      int16   /* 1 for PCM. */
	nchannels       int16   /* 1 for mono, 2 for stereo. */
	nsamplespersec  int32   /* sampling freq, Hz. */
	navgbytespersec int32   /* = nblockalign * nsamplespersec. */
	nblockalign     int16   /* = wbitspersample / 8 * nchannels. */
	wbitspersample  int16   /* 16 or 8. */
	data            [4]byte /* "data" */
	datasize        int32   /* number of bytes following. */
}

const MY_RAND_MAX = 0x7fffffff

var GEN_PACKETS = false // Switch between fakes and reals at runtime

var modem audio_s
var g_morse_wpm = 0 /* Send morse code at this speed. */
var g_add_noise = false
var g_noise_level float64 = 0

var genPacketsOutFile *os.File

// Created in audio_file_open, used for audio_put_fake, flushed in audio_file_close
var genPacketsOutBuf *bufio.Writer

var byte_count int /* Number of data bytes written to file. Will be written to header when file is closed. */

var gen_header wav_header

var genPacketsRandSeed int32 = 1

// Although the tests in `test-scripts` all call `atest` with an acceptable *range* of packets, the only way I could get them all to pass was by reimplementing this exact PRNG from Dire Wolf's gen_packets.c - all my attempts to use Go's `math/rand` resulted in decodes that would fall outside of the acceptable range. It's far from impossible that I somehow screwed up my use of `math/rand`, but I think it more likely that the tests depend on this exact PRNG implementation, which I should address at some point. /KG
// Yep, if seed is 1, tests pass; if seed is 2, test96f64 decodes 68 not 71+; if seed is 3 then test96f16 decodes 62 not 63+ /KG
func genPacketsRand() int32 {
	genPacketsRandSeed = int32((uint32(genPacketsRandSeed)*1103515245 + 12345) & MY_RAND_MAX)
	return genPacketsRandSeed
}

func GenPacketsMain() {
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

	/*
	 * Set up other default values.
	 */
	var packet_count = 0

	var bitrateStr = pflag.StringP("bitrate", "B", strconv.Itoa(DEFAULT_BAUD), `Bits / second for data.  Proper modem automatically selected for speed.
300 bps defaults to AFSK tones of 1600 & 1800.
1200 bps uses AFSK tones of 1200 & 2200.
2400 bps uses QPSK based on V.26 standard.
4800 bps uses 8PSK based on V.27 standard.
9600 bps and up uses K9NG/G3RUH standard.
AIS for ship Automatic Identification System.
EAS for Emergency Alert System (EAS) Specific Area Message Encoding (SAME).`)
	var bitrateOverrideStr = pflag.StringP("bitrate-override", "b", "", "Bits / second for data.")
	var g3ruh = pflag.BoolP("g3ruh", "g", false, "Use G3RUH modem rather than default for data rate.")
	var bpsk = pflag.BoolP("bpsk", "k", false, "Use BPSK modem rather than default for data rate.")
	var direwolf15compat = pflag.BoolP("direwolf-15-compat", "j", false, "2400 bps QPSK compatible with direwolf <= 1.5.")
	var mfj2400compat = pflag.BoolP("mfj-2400-compat", "J", false, "2400 bps QPSK compatible with MFJ-2400.")
	var markFrequency = pflag.IntP("mark", "m", 0, "Mark frequency.")
	var spaceFrequency = pflag.IntP("space", "s", 0, "Space frequency.")
	var noisyPacketCount = pflag.IntP("noisy-packet-count", "n", 0, "Generate specified number of frames with increasing noise.")
	var packetCount = pflag.IntP("packet-count", "N", 0, "Generate specified number of frames.")
	var amplitude = pflag.IntP("amplitude", "a", 50, "Signal amplitude in range of 0 - 200%.") // 100% is actually half of the digital signal range so we have some headroom for adding noise, etc.
	var audioSampleRate = pflag.IntP("audio-sample-rate", "r", DEFAULT_SAMPLES_PER_SEC, "Audio sample rate.")
	// var leadingZeros = pflag.IntP("leading-zeros", "z", 12, "Number of leading zero bits before frame. 12 is .01 seconds at 1200 bits/sec.") // -z option TODO: not implemented, should replace with txdelay frames.
	var eightBitsPerSample = pflag.BoolP("eight-bps", "8", false, "8 bit audio rather than 16.")
	var twoSoundChannels = pflag.BoolP("two-sound-channels", "2", false, "2 channels (stereo) audio rather than one channel.")
	var outputFile = pflag.StringP("output-file", "o", "", "Send output to .wav file.")
	var morseWPM = pflag.IntP("morse-wpm", "M", 0, "Send Morse at this speed.")
	var fx25CheckBytes = pflag.IntP("fx25-check-bytes", "X", 0, "1 to enable FX.25 transmit.  16, 32, 64 for specific number of check bytes.")
	var il2pNormal = pflag.IntP("il2p", "I", -1, "Enable IL2P transmit.  n=1 is recommended.  0 uses weaker FEC.")
	var il2pInverted = pflag.IntP("il2p-inverted", "i", -1, "Enable IL2P transmit, inverted polarity.  n=1 is recommended.  0 uses weaker FEC.")
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

	if *bitrateStr != "" {
		var bitrate int
		if *bitrateStr == "EAS" {
			bitrate = 0xEA5EA5 // Special case handled below
		} else {
			bitrate, _ = strconv.Atoi(*bitrateStr)
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
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("Use a more reasonable bit rate in range of %d - %d.\n", MIN_BAUD, MAX_BAUD)
			os.Exit(1)
		}
	}

	// These must be processed after -B option.
	if *markFrequency > 0 {
		modem.achan[0].mark_freq = *markFrequency
		fmt.Printf("Mark frequency set to %d Hz.\n", modem.achan[0].mark_freq)

		if modem.achan[0].mark_freq < 300 || modem.achan[0].mark_freq > 3000 {
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("Use a more reasonable value in range of 300 - 3000, not %d.\n", *markFrequency)
			os.Exit(1)
		}
	}

	if *spaceFrequency > 0 {
		modem.achan[0].space_freq = *spaceFrequency

		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Space frequency set to %d Hz.\n", modem.achan[0].space_freq)

		if modem.achan[0].space_freq < 300 || modem.achan[0].space_freq > 3000 {
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("Use a more reasonable value in range of 300 - 3000, not %d.\n", *spaceFrequency)
			os.Exit(1)
		}
	}

	if *bitrateOverrideStr != "" {
		var bitrateOverride, _ = strconv.Atoi(*bitrateOverrideStr)
		if bitrateOverride == 0 {
			fmt.Fprintf(os.Stderr, "Invalid bitrate %s\n", *bitrateOverrideStr)
			pflag.Usage()
			os.Exit(1)
		}

		modem.achan[0].baud = bitrateOverride
		fmt.Printf("Data rate set to %d bits / second.\n", modem.achan[0].baud)
	}

	if *g3ruh { /* -g for g3ruh scrambling */
		modem.achan[0].modem_type = MODEM_SCRAMBLE

		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Using G3RUH mode regardless of bit rate.\n")
	}

	if *bpsk { /* -k for BPSK */
		modem.achan[0].modem_type = MODEM_BPSK
		modem.achan[0].mark_freq = 0
		modem.achan[0].space_freq = 0
	}

	if *direwolf15compat { /* -j V.26 compatible with earlier direwolf. */
		modem.achan[0].v26_alternative = V26_A
		modem.achan[0].modem_type = MODEM_QPSK
		modem.achan[0].mark_freq = 0
		modem.achan[0].space_freq = 0
		modem.achan[0].baud = 2400
	}

	if *mfj2400compat { /* -J V.26 compatible with MFJ-2400. */
		modem.achan[0].v26_alternative = V26_B
		modem.achan[0].modem_type = MODEM_QPSK
		modem.achan[0].mark_freq = 0
		modem.achan[0].space_freq = 0
		modem.achan[0].baud = 2400
	}

	if modem.achan[0].modem_type == MODEM_QPSK && modem.achan[0].v26_alternative == V26_UNSPECIFIED {
		text_color_set(DW_COLOR_ERROR)
		fmt.Printf("ERROR: Either -j or -J must be specified when using 2400 bps QPSK.\n")
		pflag.Usage()
		os.Exit(1)
	}

	if *fx25CheckBytes > 0 {
		if *il2pNormal >= 0 || *il2pInverted >= 0 {
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("Can't mix -X with -I or -i.\n")
			os.Exit(1)
		}

		modem.achan[0].fx25_strength = *fx25CheckBytes
		modem.achan[0].layer2_xmit = LAYER2_FX25
	}

	if *il2pNormal >= 0 && *il2pInverted >= 0 {
		text_color_set(DW_COLOR_ERROR)
		fmt.Printf("Can't use both -I and -i at the same time.\n")
		os.Exit(1)
	}

	if *il2pNormal >= 0 {
		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Using IL2P normal polarity.\n")

		modem.achan[0].layer2_xmit = LAYER2_IL2P
		if *il2pNormal > 0 {
			modem.achan[0].il2p_max_fec = 1
		}

		modem.achan[0].il2p_invert_polarity = 0 // normal
	}

	if *il2pInverted >= 0 {
		text_color_set(DW_COLOR_INFO)
		fmt.Printf("Using IL2P inverted polarity.\n")

		modem.achan[0].layer2_xmit = LAYER2_IL2P
		if *il2pInverted > 0 {
			modem.achan[0].il2p_max_fec = 1
		}

		modem.achan[0].il2p_invert_polarity = 1 // invert for transmit
		if modem.achan[0].baud == 1200 {
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("Using -i with 1200 bps is a bad idea.  Use -I instead.\n")
		}
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

	var err = audio_file_open(*outputFile, &modem)

	if err < 0 {
		text_color_set(DW_COLOR_ERROR)
		fmt.Printf("ERROR - Can't open output file.\n")
		os.Exit(1)
	}

	gen_tone_init(&modem, *amplitude/2, true)
	morse_init(&modem, *amplitude/2)
	dtmf_init(&modem, *amplitude/2)

	// We don't have -d or -q options here.
	// Just use the default of minimal information.

	fx25_init(1)
	il2p_init(0) // There are no "-d" options so far but it could be handy here.

	if !(modem.adev[0].bits_per_sample == 8 || modem.adev[0].bits_per_sample == 16) { //nolint:staticcheck
		panic("assert(modem.adev[0].bits_per_sample == 8 || modem.adev[0].bits_per_sample == 16)")
	}

	if !(modem.adev[0].num_channels == 1 || modem.adev[0].num_channels == 2) { //nolint:staticcheck
		panic("assert(modem.adev[0].num_channels == 1 || modem.adev[0].num_channels == 2)")
	}

	if !(modem.adev[0].samples_per_sec >= MIN_SAMPLES_PER_SEC && modem.adev[0].samples_per_sec <= MAX_SAMPLES_PER_SEC) { //nolint:staticcheck
		panic("assert(modem.adev[0].samples_per_sec >= MIN_SAMPLES_PER_SEC && modem.adev[0].samples_per_sec <= MAX_SAMPLES_PER_SEC)")
	}

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

		audio_file_close()

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
			gen_tone_init(&modem, *amplitude/2, true)

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

	audio_file_close()
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
 * Returns:     0 for success, -1 for failure.
 *
 *----------------------------------------------------------------*/

func audio_file_open(fname string, pa *audio_s) int {
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
	var openErr error

	genPacketsOutFile, openErr = os.Create(fname) //nolint:gosec // We expect to write to a user-supplied file from CLI
	if openErr != nil {
		text_color_set(DW_COLOR_ERROR)
		fmt.Printf("Couldn't open %s for write: %s\n", fname, openErr)

		return (-1)
	}

	// TODO KG Can't get memcpy to work, so just stuff it in manually
	// C.memcpy(unsafe.Pointer(&gen_header.riff[0]), unsafe.Pointer(C.CString("RIFF")), 4)
	gen_header.riff[0] = 'R'
	gen_header.riff[1] = 'I'
	gen_header.riff[2] = 'F'
	gen_header.riff[3] = 'F'
	gen_header.filesize = 0
	// C.memcpy(unsafe.Pointer(&gen_header.wave[0]), unsafe.Pointer(C.CString("WAVE")), 4)
	gen_header.wave[0] = 'W'
	gen_header.wave[1] = 'A'
	gen_header.wave[2] = 'V'
	gen_header.wave[3] = 'E'
	// C.memcpy(unsafe.Pointer(&gen_header.fmt[0]), unsafe.Pointer(C.CString("fmt ")), 4)
	gen_header.fmt[0] = 'f'
	gen_header.fmt[1] = 'm'
	gen_header.fmt[2] = 't'
	gen_header.fmt[3] = ' '
	gen_header.fmtsize = 16   // Always 16.
	gen_header.wformattag = 1 // 1 for PCM.

	gen_header.nchannels = int16(pa.adev[0].num_channels)
	gen_header.nsamplespersec = int32(pa.adev[0].samples_per_sec)
	gen_header.wbitspersample = int16(pa.adev[0].bits_per_sample)

	gen_header.nblockalign = gen_header.wbitspersample / 8 * gen_header.nchannels
	gen_header.navgbytespersec = int32(gen_header.nblockalign) * gen_header.nsamplespersec
	// C.memcpy(unsafe.Pointer(&gen_header.data[0]), unsafe.Pointer(C.CString("data")), 4)
	gen_header.data[0] = 'd'
	gen_header.data[1] = 'a'
	gen_header.data[2] = 't'
	gen_header.data[3] = 'a'
	gen_header.datasize = 0

	if !(gen_header.nchannels == 1 || gen_header.nchannels == 2) { //nolint:staticcheck
		panic("assert(gen_header.nchannels == 1 || gen_header.nchannels == 2)")
	}

	var writeErr = binary.Write(genPacketsOutFile, binary.LittleEndian, gen_header)
	if writeErr != nil {
		text_color_set(DW_COLOR_ERROR)
		fmt.Printf("Couldn't write header to %s: %s\n", fname, writeErr)
		genPacketsOutFile.Close()
		genPacketsOutFile = nil

		return (-1)
	}

	/*
	 * Number of bytes written will be filled in later.
	 */
	byte_count = 0

	genPacketsOutBuf = bufio.NewWriter(genPacketsOutFile)

	return (0)
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

func audio_file_close() int { //nolint:unparam
	/*
	 * Go back and fix up lengths in header.
	 */
	gen_header.filesize = int32(byte_count + binary.Size(new(wav_header)) - 8)
	gen_header.datasize = int32(byte_count)

	if genPacketsOutFile == nil {
		return (-1)
	}

	var flushErr = genPacketsOutBuf.Flush()
	if flushErr != nil {
		return -1
	}

	var _, seekErr = genPacketsOutFile.Seek(0, io.SeekStart)
	if seekErr != nil {
		text_color_set(DW_COLOR_ERROR)
		fmt.Printf("Couldn't seek in audio file: %s\n", seekErr)
		genPacketsOutFile.Close()
		genPacketsOutFile = nil
		genPacketsOutBuf = nil

		return (-1)
	}

	var writeErr = binary.Write(genPacketsOutFile, binary.LittleEndian, gen_header)
	if writeErr != nil {
		text_color_set(DW_COLOR_ERROR)
		fmt.Printf("Couldn't write header to audio file: %s\n", writeErr)
		genPacketsOutFile.Close()
		genPacketsOutFile = nil
		genPacketsOutBuf = nil

		return (-1)
	}

	genPacketsOutFile.Close()
	genPacketsOutFile = nil
	genPacketsOutBuf = nil

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
		var pp = ax25_from_text(str, true)
		if pp == nil {
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("\"%s\" is not valid TNC2 monitoring format.\n", str)

			return
		}

		var pinfo = ax25_get_info(pp)
		if len(pinfo) >= 3 && strings.HasPrefix(string(pinfo), "{DE") {
			pinfo = pinfo[3:]
		}

		var repeat = ax25_get_ssid(pp, AX25_DESTINATION)
		if repeat == 0 {
			repeat = 1
		}

		eas_send(0, pinfo, repeat, 500, 500)
		ax25_delete(pp)
	} else {
		var pp = ax25_from_text(str, true)
		if pp == nil {
			text_color_set(DW_COLOR_ERROR)
			fmt.Printf("\"%s\" is not valid TNC2 monitoring format.\n", str)

			return
		}

		// If stereo, put same thing in each channel.

		for c := 0; c < modem.adev[0].num_channels; c++ {
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

		ax25_delete(pp)
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

var sample16 int16

func audio_put_fake(_ int, c uint8) int {
	if g_add_noise {
		if (byte_count & 1) == 0 {
			sample16 = int16(c) /* save lower byte. */
			byte_count++

			return int(c)
		} else {
			sample16 |= int16(c) << 8 /* insert upper byte. */
			byte_count++
			var s = int32(sample16) // sign extend.

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

			var n, writeErr = genPacketsOutBuf.Write([]byte{byte(s & 0xff), byte(s>>8) & 0xff})
			if writeErr != nil {
				return -1
			}

			return n
		}
	} else {
		byte_count++

		var n, writeErr = genPacketsOutBuf.Write([]byte{c})
		if writeErr != nil {
			return -1
		}

		return n
	}
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
		dcd_change_real(channel, subchan, slice, state)
	}
}
