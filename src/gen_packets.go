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

// genPacketsPRNG is the pseudo-random number generator from Dire Wolf's
// gen_packets.c, which both the quiet time between frames and the noise added
// to the samples draw from, in turn.
//
// Although the tests in `test-scripts` all call `atest` with an acceptable *range* of packets, the only way I could get
// them all to pass was by reimplementing this exact PRNG from Dire Wolf's gen_packets.c - all my attempts to use Go's
// `math/rand` resulted in decodes that would fall outside of the acceptable range. It's far from impossible that I
// somehow screwed up my use of `math/rand`, but I think it more likely that the tests depend on this exact PRNG
// implementation, which I should address at some point. /KG
// Yep, if seed is 1, tests pass; if seed is 2, test96f64 decodes 68 not 71+; if seed is 3 then test96f16 decodes 62 not 63+ /KG
type genPacketsPRNG struct {
	seed int32
}

func newGenPacketsPRNG() *genPacketsPRNG {
	var r = new(genPacketsPRNG)
	r.seed = 1

	return r
}

func (r *genPacketsPRNG) next() int32 {
	r.seed = int32((uint32(r.seed)*1103515245 + 12345) & MY_RAND_MAX)

	return r.seed
}

func GenPacketsMain() {
	var modemFlags = AddGenPacketsModemFlags(pflag.CommandLine)
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

	pflag.Usage = genPacketsUsage

	// !!! PARSE !!!
	pflag.Parse()

	if *help {
		pflag.Usage()
		os.Exit(1)
	}

	if *noisyPacketCount > 0 && *packetCount > 0 {
		fmt.Printf("Cannot choose both noisy packets (-n) and noiseless (-N) packets - pick at most one.\n")
		os.Exit(1)
	}

	var variableSpeedMaxError float64 = 0 // both in percent
	var variableSpeedIncrement = 0.1

	if *variableSpeedStr != "" {
		var maxError, increment, found = strings.Cut(*variableSpeedStr, ",")

		var err error

		variableSpeedMaxError, err = strconv.ParseFloat(maxError, 64)
		if err == nil && found {
			variableSpeedIncrement, err = strconv.ParseFloat(increment, 64)
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid variable speed %s: %s\n", *variableSpeedStr, err)
			pflag.Usage()
			os.Exit(1)
		}
	}

	if *outputFile == "" {
		fmt.Printf("ERROR: The -o output file option must be specified.\n")
		pflag.Usage()
		os.Exit(1)
	}

	var opts = new(GenPacketsOptions)
	opts.Modem = modemFlags
	opts.Amplitude = *amplitude
	opts.SampleRate = *audioSampleRate
	opts.EightBit = *eightBitsPerSample
	opts.Stereo = *twoSoundChannels
	opts.MorseWPM = *morseWPM

	var g, err = NewGenPackets(opts, *outputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		pflag.Usage()
		os.Exit(1)
	}

	fmt.Printf("Amplitude set to %d%%.\n", opts.Amplitude)

	if opts.SampleRate != DEFAULT_SAMPLES_PER_SEC {
		fmt.Printf("Audio sample rate set to %d samples / second.\n", opts.SampleRate)
	}

	if opts.EightBit {
		fmt.Printf("8 bits per audio sample rather than 16.\n")
	}

	if opts.Stereo {
		fmt.Printf("2 channels of sound rather than 1.\n")
	}

	if opts.MorseWPM > 0 {
		fmt.Printf("Morse code speed set to %d WPM.\n", opts.MorseWPM)
	}

	var sendErr = genPacketsSend(g, pflag.Args(), *noisyPacketCount, *packetCount, variableSpeedMaxError, variableSpeedIncrement)

	var closeErr = g.Close()

	for _, err := range []error{sendErr, closeErr} {
		if err != nil {
			fmt.Printf("%s\n", err)
			os.Exit(1)
		}
	}
}

// genPacketsSend sends the packets from the first of files, or "-" for stdin,
// or if there are no files, the built in ones: with variable speed if
// variableSpeedMaxError is set, else noisyPacketCount with increasing noise
// or packetCount without, else the default few.
func genPacketsSend(g *GenPackets, files []string, noisyPacketCount int, packetCount int, variableSpeedMaxError float64, variableSpeedIncrement float64) error {
	/*
	 * Get user packets(s) from file or stdin if specified.
	 * "-n" option is ignored in this case.
	 */

	if len(files) > 0 {
		if len(files) > 1 {
			fmt.Printf("Warning: File(s) beyond the first are ignored.\n")
		}

		var input = os.Stdin

		if files[0] == "-" {
			fmt.Printf("Reading from stdin ...\n")
		} else {
			var err error

			input, err = os.Open(files[0])
			if err != nil {
				return fmt.Errorf("can't open %s for read: %w", files[0], err)
			}
			defer input.Close()

			fmt.Printf("Reading from %s ...\n", files[0])
		}

		var scanner = bufio.NewScanner(input)
		for scanner.Scan() {
			var str = scanner.Text()

			fmt.Printf("%s", str)

			var err = g.SendPacket(str)
			if err != nil {
				fmt.Printf("%s\n", err)
			}
		}

		return scanner.Err()
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
	case variableSpeedMaxError != 0:
		fmt.Printf("Variable speed.\n")

		return g.SendVariableSpeed(variableSpeedMaxError, variableSpeedIncrement)
	case noisyPacketCount > 0:
		g.SendNumbered(noisyPacketCount, true)
	case packetCount > 0:
		g.SendNumbered(packetCount, false)
	default:
		g.SendBuiltIn()
	}

	return nil
}

func genPacketsUsage() {
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

// GenPacketsOptions are how GenPackets should make its recording.
type GenPacketsOptions struct {
	// Modem is the modem chosen by -B, -g, -j, -J, -b, -m, -s and the FEC
	// options.  Nil leaves the default of 1200 bps AFSK.
	Modem *GenPacketsModemFlags

	// Amplitude is the signal amplitude, from 0 to 200%.  100% is half of
	// the digital signal range, so there is headroom for adding noise.
	Amplitude int

	// SampleRate is the audio sample rate, or 0 for the default.
	SampleRate int

	// EightBit records 8 bit samples rather than 16.
	EightBit bool

	// Stereo records two channels rather than one, with the same thing in
	// each.
	Stereo bool

	// MorseWPM sends Morse code at this speed, in words per minute, in place
	// of AX.25 frames.  0 sends frames.
	MorseWPM int
}

// A GenPackets turns frames into audio, writing it to a .WAV file.
type GenPackets struct {
	audio     *audio_s
	amplitude int
	morseWPM  int
	rand      *genPacketsPRNG
	sink      *wavFileSink

	// One per channel, kept for the whole run, so the NRZI line level carries
	// over from one packet to the next as it does on the air.
	hdlcSenders []*HDLCSender
}

// NewGenPackets sets up the modulator as opts asks, and creates the .WAV file
// called outputFile to record into.  The error is for an option that is out
// of range or makes no sense, or a file that can't be created.
func NewGenPackets(opts *GenPacketsOptions, outputFile string) (*GenPackets, error) {
	var audio = genPacketsDefaultAudio()

	if opts.Amplitude < 0 || opts.Amplitude > 200 {
		return nil, fmt.Errorf("amplitude must be in range of 0 to 200, not %d", opts.Amplitude)
	}

	if opts.SampleRate != 0 {
		if opts.SampleRate < MIN_SAMPLES_PER_SEC || opts.SampleRate > MAX_SAMPLES_PER_SEC {
			return nil, fmt.Errorf("use a more reasonable audio sample rate in range of %d - %d, not %d",
				MIN_SAMPLES_PER_SEC, MAX_SAMPLES_PER_SEC, opts.SampleRate)
		}

		audio.adev[0].samples_per_sec = opts.SampleRate
	}

	if opts.EightBit {
		audio.adev[0].bits_per_sample = 8
	}

	if opts.Stereo {
		audio.adev[0].num_channels = 2
		audio.chan_medium[1] = MEDIUM_RADIO
	}

	if opts.MorseWPM != 0 && (opts.MorseWPM < 5 || opts.MorseWPM > 50) {
		return nil, fmt.Errorf("morse code speed must be in range of 5 to 50 WPM, not %d", opts.MorseWPM)
	}

	if opts.Modem != nil {
		var modemErr = opts.Modem.apply(&audio.achan[0])
		if modemErr != nil {
			return nil, modemErr
		}
	}

	var adevErr = audio.adev[0].validate()
	if adevErr != nil {
		return nil, fmt.Errorf("unusable audio device configuration: %w", adevErr)
	}

	var rand = newGenPacketsPRNG()

	var sink, sinkErr = audio_file_open(outputFile, audio)
	if sinkErr != nil {
		return nil, sinkErr
	}

	sink.rand = rand

	var g = new(GenPackets)
	g.audio = audio
	g.amplitude = opts.Amplitude
	g.morseWPM = opts.MorseWPM
	g.rand = rand
	g.sink = sink

	gen_tone_init(audio, g.amplitude/2, sink)
	morse_init(audio, g.amplitude/2)

	g.hdlcSenders = make([]*HDLCSender, MAX_RADIO_CHANS)
	for c := range g.hdlcSenders {
		g.hdlcSenders[c] = NewHDLCSender(c, audio)
	}

	// We don't have -d or -q options here.
	// Just use the default of minimal information.

	FX25Init(1)
	il2p_init(0) // There are no "-d" options so far but it could be handy here.

	return g, nil
}

// Close finishes the .WAV file, filling in the lengths in its header.
func (g *GenPackets) Close() error {
	return audio_file_close(g.sink)
}

// SendBuiltIn sends the built in test message: four frames, or for EAS, a
// SAME header and two end of message markers.
func (g *GenPackets) SendBuiltIn() {
	// This should send a total of 6.
	// Note that sticking in the user defined type {DE is optional.
	if g.audio.achan[0].modem_type == MODEM_EAS {
		g.mustSendPacket("X>X-3:{DEZCZC-WXR-RWT-033019-033017-033015-033013-033011-025011-025017-033007-033005-033003-033001-025009-025027-033009+0015-1691525-KGYX/NWS-")
		g.mustSendPacket("X>X-2:{DENNNN")
		g.mustSendPacket("X>X:NNNN")
	} else {
		/*
		 * Builtin default 4 packets.
		 */
		g.mustSendPacket("WB2OSZ-15>TEST:,The quick brown fox jumps over the lazy dog!  1 of 4")
		g.mustSendPacket("WB2OSZ-15>TEST:,The quick brown fox jumps over the lazy dog!  2 of 4")
		g.mustSendPacket("WB2OSZ-15>TEST:,The quick brown fox jumps over the lazy dog!  3 of 4")
		g.mustSendPacket("WB2OSZ-15>TEST:,The quick brown fox jumps over the lazy dog!  4 of 4")
	}
}

// SendNumbered sends count copies of the test message, each numbered.  With
// noisy, each has more noise added than the one before.
func (g *GenPackets) SendNumbered(count int, noisy bool) {
	g.sink.addNoise = noisy

	/*
	 * Generate packets with increasing noise level.
	 * Would probably be better to record real noise from a radio but
	 * for now just use a random number generator.
	 */
	for i := 1; i <= count; i++ {
		var baud = g.audio.achan[0].baud
		var progress = float64(i) / float64(count)

		if baud < 600 {
			/* e.g. 300 bps AFSK - About 2/3 should be decoded properly. */
			g.sink.noiseLevel = float64(g.amplitude) * .0048 * progress
		} else if baud < 1800 {
			/* e.g. 1200 bps AFSK - About 2/3 should be decoded properly. */
			g.sink.noiseLevel = float64(g.amplitude) * .0023 * progress
		} else if baud < 3600 {
			/* e.g. 2400 bps QPSK - T.B.D. */
			g.sink.noiseLevel = float64(g.amplitude) * .0015 * progress
		} else if baud < 7200 {
			/* e.g. 4800 bps - T.B.D. */
			g.sink.noiseLevel = float64(g.amplitude) * .0007 * progress
		} else {
			/* e.g. 9600 */
			g.sink.noiseLevel = 0.33 * (float64(g.amplitude) / 200.0) * progress
			// temp test
			// g.sink.noiseLevel = 0.20 * (amplitude / 200.0) * progress;
		}

		g.mustSendPacket(fmt.Sprintf("WB2OSZ-15>TEST:,The quick brown fox jumps over the lazy dog!  %04d of %04d", i, count))
	}

	g.sink.addNoise = false
}

// SendVariableSpeed sends the test message at a range of speeds, from
// maxError percent below the bit rate to maxError percent above it, in steps
// of increment percent.  The bit rate is back where it was afterwards.
func (g *GenPackets) SendVariableSpeed(maxError float64, increment float64) error {
	if increment <= 0 {
		return fmt.Errorf("variable speed increment must be more than 0, not %g", increment)
	}

	var normal_speed = g.audio.achan[0].baud

	for speed_error := -maxError; speed_error <= maxError+0.001; speed_error += increment {
		// Baud is int so we get some roundoff.  Make it real?
		g.audio.achan[0].baud = int(float64(normal_speed) * (1. + speed_error/100.))
		gen_tone_init(g.audio, g.amplitude/2, g.sink)

		g.mustSendPacket(fmt.Sprintf("WB2OSZ-15>TEST:, speed %+0.1f%%  The quick brown fox jumps over the lazy dog!", speed_error))
	}

	g.audio.achan[0].baud = normal_speed
	gen_tone_init(g.audio, g.amplitude/2, g.sink)

	return nil
}

// mustSendPacket sends one of the built in messages, which are always valid.
func (g *GenPackets) mustSendPacket(str string) {
	var err = g.SendPacket(str)
	if err != nil {
		panic(err)
	}
}

// SendPacket sends the frame str describes in TNC-2 monitor format, as Morse
// code if asked for.  The error is for str not being in that format.
func (g *GenPackets) SendPacket(str string) error {
	if g.morseWPM > 0 {
		// Why not use the destination field instead of command line option?
		// For one thing, this is not in TNC-2 monitor format.
		morse_send(0, str, g.morseWPM, 100, 100)

		return nil
	}

	var pp = AX25FromText(str, true)
	if pp == nil {
		return fmt.Errorf("%q is not valid TNC2 monitoring format", str)
	}

	if g.audio.achan[0].modem_type == MODEM_EAS {
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
		var pinfo = AX25GetInfo(pp)
		if len(pinfo) >= 3 && strings.HasPrefix(string(pinfo), "{DE") {
			pinfo = pinfo[3:]
		}

		var repeat = ax25_get_ssid(pp, AX25_DESTINATION)
		if repeat == 0 {
			repeat = 1
		}

		eas_send(0, pinfo, repeat, 500, 500)

		return nil
	}

	// If stereo, put same thing in each channel.

	for c := range g.audio.adev[0].num_channels {
		var samples_per_symbol int

		// Insert random amount of quiet time.

		switch g.audio.achan[c].modem_type {
		case MODEM_QPSK:
			samples_per_symbol = g.audio.adev[0].samples_per_sec / (g.audio.achan[c].baud / 2)
		case MODEM_8PSK:
			samples_per_symbol = g.audio.adev[0].samples_per_sec / (g.audio.achan[c].baud / 3)
		case MODEM_BPSK:
			samples_per_symbol = g.audio.adev[0].samples_per_sec / g.audio.achan[c].baud
		default:
			samples_per_symbol = g.audio.adev[0].samples_per_sec / g.audio.achan[c].baud
		}

		// Provide enough time for the DCD to drop.
		// Then throw in a random amount of time so that receiving
		// DPLL will need to adjust to a new phase.

		var n = int(float64(samples_per_symbol) * (32 + float64(g.rand.next())/float64(MY_RAND_MAX)))

		for range n {
			gen_tone_put_sample(c, 0, 0)
		}

		g.hdlcSenders[c].SendPreamblePostamble(32, false)
		g.hdlcSenders[c].SendFrame(pp, false)
		g.hdlcSenders[c].SendPreamblePostamble(2, true)
	}

	return nil
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
 * Returns:     Where to send the samples.
 *
 *----------------------------------------------------------------*/

func audio_file_open(fname string, pa *audio_s) (*wavFileSink, error) {
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
		return nil, err
	}

	return newWAVFileSink(w), nil
} /* end audio_open */

/*------------------------------------------------------------------
 *
 * Name:        audio_file_close
 *
 * Purpose:     Close the audio output file.
 *
 * Description:	Must go back to beginning of file and fill in the
 *		size of the data.
 *
 *----------------------------------------------------------------*/

func audio_file_close(sink *wavFileSink) error {
	// Close goes back and fixes up the lengths in the header for us.
	return sink.w.Close()
} /* end audio_close */

// wavFileSink is the AudioSink for gen_packets: it writes the samples to the
// .WAV file audio_file_open created, with noise added when asked for.
type wavFileSink struct {
	w *wavwrite.Writer

	// Noise is added to each sample when addNoise is set, at noiseLevel,
	// drawing on rand.
	addNoise   bool
	noiseLevel float64
	rand       *genPacketsPRNG

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
	if sink.addNoise {
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

			var r = (float64(sink.rand.next()) - float64(MY_RAND_MAX)/2.0) / (float64(MY_RAND_MAX) / 2.0)

			s += int32(5 * r * sink.noiseLevel * 32767)

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

// GenPacketsModemFlags are the command line options that set up the modulator,
// for GenPacketsOptions.Modem.
type GenPacketsModemFlags struct {
	modem           *ModemFlags
	layer2          *layer2TxFlags
	bitrateOverride *string
	mark            *int
	space           *int
	il2pVersion     *string
}

// AddGenPacketsModemFlags registers the modulator options on fs.
func AddGenPacketsModemFlags(fs *pflag.FlagSet) *GenPacketsModemFlags {
	var f = new(GenPacketsModemFlags)
	f.modem = AddModemFlags(fs, false)
	f.layer2 = addLayer2TxFlags(fs, "--il2p-version")
	f.bitrateOverride = fs.StringP("bitrate-override", "b", "", "Bits / second for data, keeping the modem -B chose.")
	f.mark = fs.IntP("mark", "m", 0, "Mark frequency.")
	f.space = fs.IntP("space", "s", 0, "Space frequency.")
	f.il2pVersion = fs.String("il2p-version", "0.6", `IL2P version to transmit.
    0.6     - 16 parity symbols per payload block, that bit reserved.  (default)
    0.4     - The header FEC Level bit says which FEC level is in use.
    compat  - Same as 0.4.`)

	return f
}

// apply sets up achan from the options, once they have been parsed.
func (f *GenPacketsModemFlags) apply(achan *achan_param_s) error {
	var err = f.modem.apply(achan)
	if err != nil {
		return err
	}

	// These must be processed after -B option.
	if *f.mark > 0 {
		if *f.mark < 300 || *f.mark > 3000 {
			return fmt.Errorf("use a more reasonable mark frequency in range of 300 - 3000, not %d", *f.mark)
		}

		achan.mark_freq = *f.mark
		logrus.WithField("mark", achan.mark_freq).Info("Mark frequency set")
	}

	if *f.space > 0 {
		if *f.space < 300 || *f.space > 3000 {
			return fmt.Errorf("use a more reasonable space frequency in range of 300 - 3000, not %d", *f.space)
		}

		achan.space_freq = *f.space
		logrus.WithField("space", achan.space_freq).Info("Space frequency set")
	}

	if *f.bitrateOverride != "" {
		var bitrateOverride, err = strconv.Atoi(*f.bitrateOverride)
		if err != nil {
			return fmt.Errorf("invalid bitrate %s", *f.bitrateOverride)
		}

		if bitrateOverride < MIN_BAUD || bitrateOverride > MAX_BAUD {
			return fmt.Errorf("use a more reasonable bit rate in range of %d - %d", MIN_BAUD, MAX_BAUD)
		}

		achan.baud = bitrateOverride
		logrus.WithField("bitrate", achan.baud).Info("Data rate set")
	}

	// The demodulator falls back on a default V.26 alternative, with a warning;
	// gen_packets makes the recording, so insists on being told.
	if achan.modem_type == MODEM_QPSK && achan.v26_alternative == V26_UNSPECIFIED {
		return errors.New("either -j or -J must be specified when using 2400 bps QPSK")
	}

	var il2p_version, il2p_version_ok = il2p_parse_version(*f.il2pVersion)
	if !il2p_version_ok {
		return fmt.Errorf("invalid IL2P version %s.  Expected 0.4, 0.6, or compat", *f.il2pVersion)
	}

	achan.il2p_version = il2p_version

	return f.layer2.apply(achan)
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
