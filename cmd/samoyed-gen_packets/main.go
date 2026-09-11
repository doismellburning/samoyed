package main

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
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/doismellburning/samoyed/internal/wavwrite"
	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/spf13/pflag"
)

// noiseWriter passes generated audio through to a .WAV file, optionally adding
// random noise to each 16 bit sample on the way.
//
// The noise draws from direwolf.GenPacketsRand, the same sequence
// GenPacketsSendPacket uses for its inter-frame gaps. Both must keep drawing
// from that one generator, in call order, or the decode counts asserted by
// test-scripts/check-modem* shift.
type noiseWriter struct {
	wav        *wavwrite.Writer
	addNoise   bool
	noiseLevel float64
	amplitude  int

	// Half of a 16 bit sample, waiting for its upper byte before noise can be
	// added to it and it can be written out.
	sample16        int16
	sample16Pending bool
}

// put accepts one byte of audio. In noise mode the bytes arrive low byte
// first, so we hold the low byte back until its high byte turns up and we can
// perturb the whole sample.
func (n *noiseWriter) put(c uint8) int {
	if !n.addNoise {
		if n.wav.WriteByte(c) != nil {
			return -1
		}

		return 1
	}

	if !n.sample16Pending {
		n.sample16 = int16(c) /* save lower byte. */
		n.sample16Pending = true

		return int(c)
	}

	n.sample16 |= int16(c) << 8 /* insert upper byte. */
	n.sample16Pending = false

	var s = int32(n.sample16) // sign extend.

	/* Add random noise to the signal. */
	/* r should be in range of -1 .. +1. */

	var r = (float64(direwolf.GenPacketsRand()) - float64(direwolf.GenPacketsRandMax)/2.0) / (float64(direwolf.GenPacketsRandMax) / 2.0)

	s += int32(5 * r * n.noiseLevel * 32767)

	if s > 32767 {
		s = 32767
	}

	if s < -32767 {
		s = -32767
	}

	var written, err = n.wav.Write([]byte{byte(s & 0xff), byte(s>>8) & 0xff})
	if err != nil {
		return -1
	}

	return written
}

// setNoiseLevel picks a noise level for frame i of count, scaled so that
// roughly two thirds of the frames should still decode.
func (n *noiseWriter) setNoiseLevel(i int, count int, baud int) {
	var amplitude = float64(n.amplitude)

	switch {
	case baud < 600:
		/* e.g. 300 bps AFSK - About 2/3 should be decoded properly. */
		n.noiseLevel = amplitude * .0048 * (float64(i) / float64(count))
	case baud < 1800:
		/* e.g. 1200 bps AFSK - About 2/3 should be decoded properly. */
		n.noiseLevel = amplitude * .0023 * (float64(i) / float64(count))
	case baud < 3600:
		/* e.g. 2400 bps QPSK - T.B.D. */
		n.noiseLevel = amplitude * .0015 * (float64(i) / float64(count))
	case baud < 7200:
		/* e.g. 4800 bps - T.B.D. */
		n.noiseLevel = amplitude * .0007 * (float64(i) / float64(count))
	default:
		/* e.g. 9600 */
		n.noiseLevel = 0.33 * (amplitude / 200.0) * (float64(i) / float64(count))
		// temp test
		// n.noiseLevel = 0.20 * (amplitude / 200.0) * (float64(i) / float64(count));
	}
}

func main() {
	genPacketsMain()
}

func genPacketsMain() { //nolint:funlen,gocyclo,maintidx // Faithful port of the CLI handling from gen_packets.c
	var packet_count = 0

	var bitrateStr = pflag.StringP("bitrate", "B", strconv.Itoa(direwolf.DEFAULT_BAUD), `Bits / second for data.  Proper modem automatically selected for speed.
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
	var audioSampleRate = pflag.IntP("audio-sample-rate", "r", direwolf.DEFAULT_SAMPLES_PER_SEC, "Audio sample rate.")
	// var leadingZeros = pflag.IntP("leading-zeros", "z", 12, "Number of leading zero bits before frame. 12 is .01 seconds at 1200 bits/sec.")
	// -z option TODO: not implemented, should replace with txdelay frames.
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

	var addNoise = false

	if *amplitude > 0 {
		fmt.Printf("Amplitude set to %d%%.\n", *amplitude)

		if *amplitude < 0 || *amplitude > 200 {
			failf("Amplitude must be in range of 0 to 200, not %d.\n", *amplitude)
		}
	}

	if *noisyPacketCount > 0 && *packetCount > 0 {
		failf("Cannot choose both noisy packets (-n) and noiseless (-N) packets - pick at most one.\n")
	} else if *noisyPacketCount > 0 {
		packet_count = *noisyPacketCount
		addNoise = true
	} else if *packetCount > 0 {
		packet_count = *packetCount
		addNoise = false
	}

	if *audioSampleRate < direwolf.MIN_SAMPLES_PER_SEC || *audioSampleRate > direwolf.MAX_SAMPLES_PER_SEC {
		failf("Use a more reasonable audio sample rate in range of %d - %d, not %d.\n",
			direwolf.MIN_SAMPLES_PER_SEC, direwolf.MAX_SAMPLES_PER_SEC, *audioSampleRate)
	}

	// The demodulator needs a few for the clock recovery PLL.
	// We don't want to be here all day either.
	// We can't translate to time yet because the data bit rate
	// could be changed later.
	/* Not implemented
	const MIN_LEADING_ZEROS = 8
	const MAX_LEADING_ZEROS = 12000
	if *leadingZeros < MIN_LEADING_ZEROS || *leadingZeros > MAX_LEADING_ZEROS {
		failf("Leading zeros should be between %d and %d, not %d.\n", MIN_LEADING_ZEROS, MAX_LEADING_ZEROS, *leadingZeros)
	}
	*/

	if *morseWPM > 0 && (*morseWPM < 5 || *morseWPM > 50) {
		failf("Morse code speed must be in range of 5 to 50 WPM, not %d.\n", *morseWPM)
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

	if *markFrequency > 0 && (*markFrequency < 300 || *markFrequency > 3000) {
		failf("Use a more reasonable value in range of 300 - 3000, not %d.\n", *markFrequency)
	}

	if *spaceFrequency > 0 && (*spaceFrequency < 300 || *spaceFrequency > 3000) {
		failf("Use a more reasonable value in range of 300 - 3000, not %d.\n", *spaceFrequency)
	}

	if *fx25CheckBytes > 0 && (*il2pNormal >= 0 || *il2pInverted >= 0) {
		failf("Can't mix -X with -I or -i.\n")
	}

	if *il2pNormal >= 0 && *il2pInverted >= 0 {
		failf("Can't use both -I and -i at the same time.\n")
	}

	if *outputFile == "" {
		fmt.Printf("ERROR: The -o output file option must be specified.\n")
		pflag.Usage()
		os.Exit(1)
	}

	var configureErr = direwolf.GenPacketsConfigure(direwolf.GenPacketsOptions{
		Bitrate:            *bitrateStr,
		BitrateOverride:    *bitrateOverrideStr,
		G3RUH:              *g3ruh,
		BPSK:               *bpsk,
		Direwolf15Compat:   *direwolf15compat,
		MFJ2400Compat:      *mfj2400compat,
		MarkFrequency:      *markFrequency,
		SpaceFrequency:     *spaceFrequency,
		AudioSampleRate:    *audioSampleRate,
		EightBitsPerSample: *eightBitsPerSample,
		TwoSoundChannels:   *twoSoundChannels,
		Amplitude:          *amplitude,
		MorseWPM:           *morseWPM,
		FX25CheckBytes:     *fx25CheckBytes,
		IL2PNormal:         *il2pNormal,
		IL2PInverted:       *il2pInverted,
	})
	if configureErr != nil {
		fmt.Printf("ERROR: %s\n", configureErr)
		pflag.Usage()
		os.Exit(1)
	}

	/*
	 * Open the output file.
	 */

	var numChannels, samplesPerSec, bitsPerSample = direwolf.GenPacketsAudioFormat()

	var wav, wavErr = wavwrite.Create(*outputFile, wavwrite.Format{
		NumChannels:   numChannels,
		SamplesPerSec: samplesPerSec,
		BitsPerSample: bitsPerSample,
	})
	if wavErr != nil {
		failf("%s\n", wavErr)
	}

	var writer = new(noiseWriter)
	writer.wav = wav
	writer.addNoise = addNoise
	writer.amplitude = *amplitude

	direwolf.GenPacketsSetAudioSink(writer.put)

	/*
	 * Get user packets(s) from file or stdin if specified.
	 * "-n" option is ignored in this case.
	 */

	if len(pflag.Args()) > 0 {
		sendFromFile(pflag.Args())
		closeWAV(wav)

		return
	}

	/*
	 * Otherwise, use the built in packets.
	 */
	fmt.Printf("built in message...\n")

	//
	// Generate packets with variable speed.
	// This overrides any other number of packets or adding noise.
	//

	switch {
	case variable_speed_max_error != 0:
		var normal_speed = direwolf.GenPacketsBaud()

		fmt.Printf("Variable speed.\n")

		for speed_error := -variable_speed_max_error; speed_error <= variable_speed_max_error+0.001; speed_error += variable_speed_increment {
			// Baud is int so we get some roundoff.  Make it real?
			direwolf.GenPacketsSetSpeed(int(float64(normal_speed)*(1.+speed_error/100.)), *amplitude)

			var stemp = fmt.Sprintf("WB2OSZ-15>TEST:, speed %+0.1f%%  The quick brown fox jumps over the lazy dog!", speed_error)
			direwolf.GenPacketsSendPacket(stemp)
		}
	case packet_count > 0:
		/*
		 * Generate packets with increasing noise level.
		 * Would probably be better to record real noise from a radio but
		 * for now just use a random number generator.
		 */
		for i := 1; i <= packet_count; i++ {
			writer.setNoiseLevel(i, packet_count, direwolf.GenPacketsBaud())

			var stemp = fmt.Sprintf("WB2OSZ-15>TEST:,The quick brown fox jumps over the lazy dog!  %04d of %04d", i, packet_count)
			direwolf.GenPacketsSendPacket(stemp)
		}
	case direwolf.GenPacketsIsEAS():
		// This should send a total of 3.
		// Note that sticking in the user defined type {DE is optional.
		direwolf.GenPacketsSendPacket("X>X-3:{DEZCZC-WXR-RWT-033019-033017-033015-033013-033011-025011-025017-033007-033005-033003-033001-025009-025027-033009+0015-1691525-KGYX/NWS-")
		direwolf.GenPacketsSendPacket("X>X-2:{DENNNN")
		direwolf.GenPacketsSendPacket("X>X:NNNN")
	default:
		/*
		 * Builtin default 4 packets.
		 */
		direwolf.GenPacketsSendPacket("WB2OSZ-15>TEST:,The quick brown fox jumps over the lazy dog!  1 of 4")
		direwolf.GenPacketsSendPacket("WB2OSZ-15>TEST:,The quick brown fox jumps over the lazy dog!  2 of 4")
		direwolf.GenPacketsSendPacket("WB2OSZ-15>TEST:,The quick brown fox jumps over the lazy dog!  3 of 4")
		direwolf.GenPacketsSendPacket("WB2OSZ-15>TEST:,The quick brown fox jumps over the lazy dog!  4 of 4")
	}

	closeWAV(wav)
}

// sendFromFile converts each line of the first named file, or stdin if that
// name is "-", to audio.
func sendFromFile(args []string) {
	if len(args) > 1 {
		fmt.Printf("Warning: File(s) beyond the first are ignored.\n")
	}

	var arg = args[0]
	var input_fp *os.File

	if arg == "-" {
		fmt.Printf("Reading from stdin ...\n")

		input_fp = os.Stdin
	} else {
		var err error

		input_fp, err = os.Open(arg) //nolint:gosec // We expect to read from a user-supplied file from CLI
		if err != nil {
			failf("Can't open %s for read: %s\n", arg, err)
		}

		defer input_fp.Close()

		fmt.Printf("Reading from %s ...\n", arg)
	}

	var scanner = bufio.NewScanner(input_fp)
	for scanner.Scan() {
		var str = scanner.Text()

		fmt.Printf("%s", str)
		direwolf.GenPacketsSendPacket(str)
	}
}

// closeWAV finishes off the output file, reporting any failure to fix up its
// header as a fatal error - a truncated .WAV is not worth returning success for.
func closeWAV(wav *wavwrite.Writer) {
	var err = wav.Close()
	if err != nil {
		failf("ERROR - Can't close output file: %s\n", err)
	}
}

// fail reports a fatal error and exits.
func failf(format string, a ...any) {
	fmt.Printf(format, a...)
	os.Exit(1)
}
