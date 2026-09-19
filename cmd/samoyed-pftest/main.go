// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

/*------------------------------------------------------------------
 *
 * Purpose:	Try packet filter expressions out on packets.
 *
 * Description:	Packet filters - IGFILTER, FILTER and CFILTER in the config
 *		file - are fiddly to get right, and the only way to find out
 *		what one does is normally to run the whole TNC and watch what
 *		gets dropped.  This runs the same filter engine over packets
 *		read from stdin, one per line, in the usual monitoring format
 *		that samoyed-decode_aprs takes, and says PASS or DROP for
 *		each.
 *
 * Outputs:	The verdicts on stdout, and what went wrong with a line, or
 *		with the filter, on stderr.  The filter engine and the packet
 *		parser explain themselves on stdout as well, so a verbose run,
 *		or one with packets they object to, has their commentary
 *		interleaved with the verdicts.
 *
 *		Exit status is non-zero if the filter is invalid or any input
 *		line could not be evaluated.
 *
 *------------------------------------------------------------------*/

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/spf13/pflag"
)

// maxLineLength is how long an input line may be before the scanner gives up
// on the input altogether.
const maxLineLength = 1024 * 1024

/*
 * Verdicts, one per input line.
 */
const (
	verdictPass  = "PASS"
	verdictDrop  = "DROP"
	verdictError = "ERROR"
)

type options struct {
	filter string

	// isAPRS selects the APRS filter grammar (FILTER, IGFILTER) rather than
	// the smaller connected mode one (CFILTER).
	isAPRS bool

	// fromChannel and toChannel give context in error messages and debug
	// output; direwolf.MAX_TOTAL_CHANS means the IGate.
	fromChannel int
	toChannel   int

	// validateOnly checks the filter's syntax without reading any packets.
	validateOnly bool

	// debugLevel asks the filter engine to explain itself, 0 (quiet) to
	// direwolf.PfilterMaxDebugLevel.
	debugLevel int
}

func main() {
	var validate = pflag.Bool("validate", false, "Check the filter expression for syntax errors and exit, without reading any packets.")
	var connectedMode = pflag.BoolP(
		"connected-mode", "c", false,
		"Evaluate as a connected mode digipeater filter (CFILTER) rather than an APRS filter (FILTER or IGFILTER).",
	)
	var verbose = pflag.CountP("verbose", "v", `Explain the decision (repeat for increased verbosity).
-v    Summary line with the final result.
-vv   Result of each individual filter specification.
-vvv  Result of each logical operator.`)
	var fromChannel = pflag.Int(
		"from-channel", 0,
		fmt.Sprintf("Radio channel the packet came from, or %d for the IGate.  Only affects error messages and verbose output.", direwolf.MAX_TOTAL_CHANS),
	)
	var toChannel = pflag.Int(
		"to-channel", 0,
		fmt.Sprintf("Radio channel the packet is going to, or %d for the IGate.  Only affects error messages and verbose output.", direwolf.MAX_TOTAL_CHANS),
	)
	var help = pflag.BoolP("help", "h", false, "Display help text.")

	pflag.Usage = usage

	// !!! PARSE !!!
	pflag.Parse()

	if *help {
		pflag.Usage()
		os.Exit(0)
	}

	if pflag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "Expected exactly one filter expression, got %d.\n\n", pflag.NArg())
		pflag.Usage()
		os.Exit(1)
	}

	if *fromChannel < 0 || *fromChannel > direwolf.MAX_TOTAL_CHANS {
		fmt.Fprintf(os.Stderr, "--from-channel must be between 0 and %d.\n", direwolf.MAX_TOTAL_CHANS)
		os.Exit(1)
	}

	if *toChannel < 0 || *toChannel > direwolf.MAX_TOTAL_CHANS {
		fmt.Fprintf(os.Stderr, "--to-channel must be between 0 and %d.\n", direwolf.MAX_TOTAL_CHANS)
		os.Exit(1)
	}

	var opts = options{
		filter:       pflag.Arg(0),
		isAPRS:       !*connectedMode,
		fromChannel:  *fromChannel,
		toChannel:    *toChannel,
		validateOnly: *validate,
		debugLevel:   min(*verbose, direwolf.PfilterMaxDebugLevel),
	}

	os.Exit(run(opts, os.Stdin, os.Stdout, os.Stderr))
}

func usage() {
	fmt.Fprintf(os.Stderr, "%s tries a packet filter expression out on packets, using the same filter engine as IGFILTER, FILTER and CFILTER.\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Usage: %s [OPTION]... <FILTER>\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Packets are read from stdin in the usual monitoring format, one per line, the same input samoyed-decode_aprs takes.\n")
	fmt.Fprintf(os.Stderr, "Blank lines and lines beginning with # are passed through untouched.\n")
	fmt.Fprintf(os.Stderr, "Every other line is answered with PASS or DROP, a tab, and the line itself.\n")
	fmt.Fprintf(os.Stderr, "A line that cannot be parsed, or that the filter cannot be evaluated against, is answered with ERROR and makes the exit status non-zero.\n")
	fmt.Fprintf(os.Stderr, "\n")
	pflag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Examples:\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "$ %s 'i/60/0/51.5/-0.1/50 | b/G*' < packets.txt\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "$ %s --validate 't/m & ! d/WIDE*'\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "$ echo 'Q1TEST>APDW17:>Testing' | %s -vv 't/m'\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Note that this is not the whole TNC: \"i\" filters gate to an addressee heard recently, and that list is empty here so they pass nothing,\n")
	fmt.Fprintf(os.Stderr, "and take their default maximum digipeater hop count from IGTXVIA, which is not read either.\n")
}

// run is main once the command line has been dealt with, taking its streams as
// arguments so it can be tested.  It returns the exit status.
func run(opts options, in io.Reader, out io.Writer, errOut io.Writer) int {
	direwolf.PfilterStandaloneInit(opts.debugLevel)

	var filterErr = direwolf.PfilterValidate(opts.fromChannel, opts.toChannel, opts.filter, opts.isAPRS)
	if filterErr != nil {
		fmt.Fprintf(errOut, "Invalid filter: %v\n", filterErr)

		return 1
	}

	if opts.validateOnly {
		return 0
	}

	var exitStatus = 0

	var scanner = bufio.NewScanner(in)

	// A monitoring format line is normally a few hundred bytes, but the
	// default limit of 64 KiB abandons the rest of the input rather than
	// objecting to just the line that overran it, so give it plenty of room.
	scanner.Buffer(nil, maxLineLength)

	for scanner.Scan() {
		var line = scanner.Text()

		if len(strings.TrimSpace(line)) == 0 || strings.HasPrefix(line, "#") {
			/* comment or blank line */
			fmt.Fprintf(out, "%s\n", line)

			continue
		}

		var pass, err = direwolf.PfilterMonitorLine(opts.fromChannel, opts.toChannel, opts.filter, opts.isAPRS, line)

		var verdict string

		switch {
		case err != nil:
			fmt.Fprintf(errOut, "%v\n", err)

			exitStatus = 1
			verdict = verdictError
		case pass:
			verdict = verdictPass
		default:
			verdict = verdictDrop
		}

		fmt.Fprintf(out, "%s\t%s\n", verdict, line)
	}

	var scanErr = scanner.Err()
	if scanErr != nil {
		fmt.Fprintf(errOut, "Error reading packets: %v\n", scanErr)

		return 1
	}

	return exitStatus
}
