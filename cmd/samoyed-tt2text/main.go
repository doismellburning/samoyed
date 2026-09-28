package main

/*------------------------------------------------------------------
 *
 * Purpose:   	Utility program for testing the touch-tone text decoding.
 *
 *---------------------------------------------------------------*/

import (
	"fmt"
	"os"
	"strings"

	"github.com/doismellburning/samoyed/internal/touchtone"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Supply button sequence on command line.\n")
		os.Exit(1)
	}

	var goButtons = strings.Join(os.Args[1:], "")

	tt2text(goButtons)
}

func tt2text(buttons string) {
	switch touchtone.GuessType(buttons) {
	case touchtone.Multipress:
		fmt.Printf("Looks like multi-press encoding.\n")
	case touchtone.TwoKey:
		fmt.Printf("Looks like two-key encoding.\n")
	default:
		fmt.Printf("Could be either type of encoding.\n")
	}

	var text string
	var errs int

	fmt.Printf("Decoded text from multi-press method:\n")

	text, _ = touchtone.MultipressToText(buttons, false)
	fmt.Printf("\"%s\"\n", text)

	fmt.Printf("Decoded text from two-key method:\n")

	text, _ = touchtone.TwoKeyToText(buttons, false)
	fmt.Printf("\"%s\"\n", text)

	text, errs = touchtone.Call10ToText(buttons, true)
	if errs == 0 {
		fmt.Printf("Decoded callsign from 10 digit method:\n")
		fmt.Printf("\"%s\"\n", text)
	}

	text, errs = touchtone.MheadToText(buttons, true)
	if errs == 0 {
		fmt.Printf("Decoded Maidenhead Locator from DTMF digits:\n")
		fmt.Printf("\"%s\"\n", text)
	}

	text, errs = touchtone.SatsqToText(buttons, true)
	if errs == 0 {
		fmt.Printf("Decoded satellite gridsquare from 4 DTMF digits:\n")
		fmt.Printf("\"%s\"\n", text)
	}
}
