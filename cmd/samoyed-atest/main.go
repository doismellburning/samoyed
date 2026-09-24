// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

/*------------------------------------------------------------------
 *
 * Purpose:	Decode AX.25 frames from .WAV files, to test the
 *		demodulators under controlled and reproducible conditions.
 *
 * Description:	The decoding is direwolf.Atest; this is its command line,
 *		the loop over the files given and the check on how many
 *		frames were decoded that makes it useful in a test script.
 *
 *------------------------------------------------------------------*/

package main

import (
	"fmt"
	"io"
	"os"
	"time"

	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/spf13/pflag"
)

// expected is the range the total number of frames decoded must fall in,
// with -1 for no limit.
type expected struct {
	atLeast int
	atMost  int
}

func main() {
	direwolf.TextColorInit(1)

	var modemFlags = direwolf.AddModemFlags(pflag.CommandLine, true)
	var fixBits = pflag.IntP("fix-bits", "F", 0, fmt.Sprintf(`Amount of effort to try fixing frames with an invalid CRC.
0 (default) = consider only correct frames.
1 = Try to fix only a single bit.
Higher values = Try modifying more bits to get a good CRC.
%d = Try everything, then hand over frames that still have a bad CRC (PASSALL).`, direwolf.BitFixPassall))
	var errorIfLessThan = pflag.IntP("error-if-less-than", "L", -1, "Error if less than this number decoded.")
	var errorIfGreaterThan = pflag.IntP("error-if-greater-than", "G", -1, "Error if greater than this number decoded.")
	var channel0 = pflag.BoolP("channel-0", "0", false, "Use channel 0 (left) of stereo audio (default).")
	var channel1 = pflag.BoolP("channel-1", "1", false, "Use channel 1 (right) of stereo audio.")
	var channel2 = pflag.BoolP("channel-2", "2", false, "Use both channels of stereo audio.")
	var hexDisplay = pflag.BoolP("hex-display", "h", false, "Print frame contents as hexadecimal bytes.")
	var bitErrorRate = pflag.Float64P("bit-error-rate", "e", 0.0, "Receive Bit Error Rate (BER).")
	var debugFlags = pflag.StringSliceP("debug", "d", []string{}, `Debug (repeat for increased verbosity).
x = FX.25
o = DCD output control
2 = IL2P`)
	var il2pVersion = pflag.String("il2p-version", "0.6", `IL2P version to receive.
    0.6     - 16 parity symbols per payload block, ignoring that reserved bit.  (default)
    0.4     - The header FEC Level bit selects the number of payload parity symbols.
    compat  - Same as 0.6.`)
	var help = pflag.Bool("help", false, "Display help text.")

	pflag.Usage = usage

	// !!! PARSE !!!
	pflag.Parse()

	if *help {
		pflag.Usage()
		os.Exit(1)
	}

	var opts = new(direwolf.AtestOptions)
	opts.Modem = modemFlags
	opts.FixBits = *fixBits
	opts.IL2PVersion = *il2pVersion
	opts.BitErrorRate = *bitErrorRate
	opts.HexDisplay = *hexDisplay

	for _, debugFlag := range *debugFlags {
		switch debugFlag {
		case "x":
			opts.DebugFX25++
		case "o":
			opts.DebugDCD++
		case "2":
			opts.DebugIL2P++
		default:
			fmt.Fprintf(os.Stderr, "Unrecognised debug flag: %s\n", debugFlag)
			pflag.Usage()
			os.Exit(1)
		}
	}

	var channelFlagCount int

	for _, b := range []bool{*channel0, *channel1, *channel2} {
		if b {
			channelFlagCount++
		}
	}

	if channelFlagCount > 1 {
		fmt.Fprintf(os.Stderr, "Exactly one of left/right/both channels must be selected.\n")
		pflag.Usage()
		os.Exit(1)
	}

	switch {
	case *channel1:
		opts.DecodeOnly = 1
	case *channel2:
		opts.DecodeOnly = 2
	default:
		opts.DecodeOnly = 0
	}

	var atest, err = direwolf.NewAtest(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		pflag.Usage()
		os.Exit(1)
	}

	if pflag.NArg() == 0 {
		fmt.Printf("Specify .WAV file name on command line.\n\n")
		pflag.Usage()
		os.Exit(1)
	}

	os.Exit(run(atest, pflag.Args(), expected{atLeast: *errorIfLessThan, atMost: *errorIfGreaterThan}, opts.DebugDCD > 0, os.Stdout))
}

func usage() {
	fmt.Fprintf(os.Stderr, "%s is a test application which decodes AX.25 frames from audio recordings.\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "This provides an easy way to test decoding performance and functionality much quicker than normal real-time.\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Usage: %s [OPTION]... <WAV FILE>...\n", os.Args[0])
	pflag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Examples:\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "$ samoyed-gen_packets -o test1.wav\n")
	fmt.Fprintf(os.Stderr, "$ samoyed-atest test1.wav\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "$ samoyed-gen_packets -B 300 -o test3.wav\n")
	fmt.Fprintf(os.Stderr, "$ samoyed-atest -B 300 test3.wav\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "$ samoyed-gen_packets -B 9600 -o test9.wav\n")
	fmt.Fprintf(os.Stderr, "$ samoyed-atest -B 9600 test9.wav\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Try different combinations of options to compare decoding performance.\n")
}

// run is main once the command line has been dealt with: it decodes files
// with atest and reports on them to out, returning the exit status.  The
// frames themselves, and what atest has to say about each file, still go to
// stdout.
func run(atest *direwolf.Atest, files []string, want expected, reportDCD bool, out io.Writer) int {
	var startTime = time.Now()
	var totalFileTime float64
	var packetsDecodedTotal = 0

	for _, name := range files {
		var result, err = atest.DecodeFile(name)
		if err != nil {
			fmt.Fprintf(out, "%s\n", err)

			return 1
		}

		totalFileTime += result.Seconds
		packetsDecodedTotal += result.PacketsDecoded
	}

	var elapsed = time.Since(startTime)

	fmt.Fprintf(out, "%d packets decoded in %.3f seconds.  %.1f x realtime\n", packetsDecodedTotal, elapsed.Seconds(), totalFileTime/elapsed.Seconds())

	if reportDCD {
		var dcdCount, dcdMissingErrors = atest.DCDStats()
		fmt.Fprintf(out, "DCD count = %d\n", dcdCount)
		fmt.Fprintf(out, "DCD missing errors = %d\n", dcdMissingErrors)
	}

	if want.atLeast != -1 && packetsDecodedTotal < want.atLeast {
		fmt.Fprintf(out, "\n * * * TEST FAILED: number decoded is less than %d * * * \n", want.atLeast)

		return 1
	}

	if want.atMost != -1 && packetsDecodedTotal > want.atMost {
		fmt.Fprintf(out, "\n * * * TEST FAILED: number decoded is greater than %d * * * \n", want.atMost)

		return 1
	}

	return 0
}
