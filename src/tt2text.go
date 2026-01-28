package direwolf

import (
	"fmt"
	"os"
	"strings"
)

// Utility program for testing the decoding.
func TT2TextMain() {
	if len(os.Args) < 2 {
		fmt.Printf("Supply button sequence on command line.\n")
		os.Exit(1)
	}

	var goButtons = strings.Join(os.Args[1:], "")

	TT2Text(goButtons)
}

func TT2Text(buttons string) {

	switch tt_guess_type(buttons) {
	case TT_MULTIPRESS:
		fmt.Printf("Looks like multi-press encoding.\n")
	case TT_TWO_KEY:
		fmt.Printf("Looks like two-key encoding.\n")
	default:
		fmt.Printf("Could be either type of encoding.\n")
	}

	var text string
	var errs int

	fmt.Printf("Decoded text from multi-press method:\n")
	text, _ = tt_multipress_to_text(buttons, false)
	fmt.Printf("\"%s\"\n", text)

	fmt.Printf("Decoded text from two-key method:\n")
	text, _ = tt_two_key_to_text(buttons, false)
	fmt.Printf("\"%s\"\n", text)

	text, errs = tt_call10_to_text(buttons, true)
	if errs == 0 {
		fmt.Printf("Decoded callsign from 10 digit method:\n")
		fmt.Printf("\"%s\"\n", text)
	}

	text, errs = tt_mhead_to_text(buttons, true)
	if errs == 0 {
		fmt.Printf("Decoded Maidenhead Locator from DTMF digits:\n")
		fmt.Printf("\"%s\"\n", text)
	}

	text, errs = tt_satsq_to_text(buttons, true)
	if errs == 0 {
		fmt.Printf("Decoded satellite gridsquare from 4 DTMF digits:\n")
		fmt.Printf("\"%s\"\n", text)
	}
}
