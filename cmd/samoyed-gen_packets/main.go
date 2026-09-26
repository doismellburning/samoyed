// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

/*------------------------------------------------------------------
 *
 * Purpose:	Generate AX.25 frames as audio in a .WAV file, to test
 *		the demodulators.
 *
 * Description:	The generating is direwolf.GenPackets; this is its command
 *		line, and the choice of which frames to send.
 *
 * Examples:	Different speeds:
 *
 *			samoyed-gen_packets -o z1.wav
 *			samoyed-atest z1.wav
 *
 *			samoyed-gen_packets -B 300 -o z3.wav
 *			samoyed-atest -B 300 z3.wav
 *
 *			samoyed-gen_packets -B 9600 -o z9.wav
 *			samoyed-atest -B 9600 z9.wav
 *
 *		User-defined content:
 *
 *			echo "WB2OSZ>APDW12:This is a test" | samoyed-gen_packets -o z.wav -
 *			samoyed-atest z.wav
 *
 *			echo "WB2OSZ>APDW12:Test line 1" >  z.txt
 *			echo "WB2OSZ>APDW12:Test line 2" >> z.txt
 *			echo "WB2OSZ>APDW12:Test line 3" >> z.txt
 *			samoyed-gen_packets -o z.wav z.txt
 *			samoyed-atest z.wav
 *
 *		With artificial noise added:
 *
 *			samoyed-gen_packets -n 100 -o z2.wav
 *			samoyed-atest z2.wav
 *
 *		Variable speed. e.g. 95% to 105% of normal speed.
 *		Required parameter is max % below and above normal.
 *		Optionally specify step other than 0.1%.
 *		Used to test how tolerant TNCs are to senders not
 *		not using exactly the right baud rate.
 *
 *			samoyed-gen_packets -v 5 -o z.wav
 *			samoyed-gen_packets -v 5,0.5 -o z.wav
 *
 *------------------------------------------------------------------*/

package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/spf13/pflag"
)

func main() {
	var modemFlags = direwolf.AddGenPacketsModemFlags(pflag.CommandLine)
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
	var variableSpeedStr = pflag.StringP("variable-speed", "v", "", "max[,incr] Variable speed with specified maximum error and increment.")
	var help = pflag.BoolP("help", "h", false, "Display help text.")

	pflag.Usage = usage

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

	var opts = new(direwolf.GenPacketsOptions)
	opts.Modem = modemFlags
	opts.Amplitude = *amplitude
	opts.SampleRate = *audioSampleRate
	opts.EightBit = *eightBitsPerSample
	opts.Stereo = *twoSoundChannels
	opts.MorseWPM = *morseWPM

	var g, err = direwolf.NewGenPackets(opts, *outputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		pflag.Usage()
		os.Exit(1)
	}

	fmt.Printf("Amplitude set to %d%%.\n", opts.Amplitude)

	if opts.SampleRate != direwolf.DEFAULT_SAMPLES_PER_SEC {
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

	var sendErr = send(g, pflag.Args(), *noisyPacketCount, *packetCount, variableSpeedMaxError, variableSpeedIncrement)

	var closeErr = g.Close()

	for _, err := range []error{sendErr, closeErr} {
		if err != nil {
			fmt.Printf("%s\n", err)
			os.Exit(1)
		}
	}
}

// send sends the packets from the first of files, or "-" for stdin,
// or if there are no files, the built in ones: with variable speed if
// variableSpeedMaxError is set, else noisyPacketCount with increasing noise
// or packetCount without, else the default few.
func send(g *direwolf.GenPackets, files []string, noisyPacketCount int, packetCount int, variableSpeedMaxError float64, variableSpeedIncrement float64) error {
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

func usage() {
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
	fmt.Fprintf(os.Stderr, "Example:  samoyed-gen_packets -o x.wav \n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "    With all defaults, a built-in test message is generated\n")
	fmt.Fprintf(os.Stderr, "    with standard Bell 202 tones used for packet radio on ordinary\n")
	fmt.Fprintf(os.Stderr, "    VHF FM transceivers.\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Example:  samoyed-gen_packets -o x.wav -g -b 9600\n")
	fmt.Fprintf(os.Stderr, "Shortcut: samoyed-gen_packets -o x.wav -B 9600\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "    9600 baud mode.\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Example:  samoyed-gen_packets -o x.wav -m 1600 -s 1800 -b 300\n")
	fmt.Fprintf(os.Stderr, "Shortcut: samoyed-gen_packets -o x.wav -B 300\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "    200 Hz shift, 300 baud, suitable for HF SSB transceiver.\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Example:  echo -n \"WB2OSZ>WORLD:Hello, world!\" | samoyed-gen_packets -a 25 -o x.wav -\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "    Read message from stdin and put quarter volume sound into the file x.wav.\n")
}
