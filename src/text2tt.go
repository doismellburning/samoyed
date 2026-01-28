package direwolf

import (
	"fmt"
	"os"
	"strings"
	"unicode"
)

func checksum(tt string) int {
	var cs = 10 /* Assume leading 'A'. */
	/* Doesn't matter due to mod 10 at the end. */

	for _, p := range tt {
		if unicode.IsDigit(p) {
			cs += int(p - '0')
		} else if unicode.IsUpper(p) {
			cs += int(p-'A') + 10
		} else if unicode.IsLower(p) {
			cs += int(p-'a') + 10
		}
	}

	return (cs % 10)
}

// Utility program for testing the encoding.
func Text2TTMain() {
	if len(os.Args) < 2 {
		fmt.Printf("Supply text string on command line.\n")
		os.Exit(1)
	}

	Text2TT(os.Args[1:])
}

func Text2TT(args []string) {
	var text = strings.Join(args, " ")
	var buttons string
	var errs int
	var cs int

	fmt.Printf("Push buttons for multi-press method:\n")
	buttons, _ = tt_text_to_multipress(text, false)
	cs = checksum(buttons)
	fmt.Printf("\"%s\"    checksum for call = %d\n", buttons, cs)

	fmt.Printf("Push buttons for two-key method:\n")
	buttons, _ = tt_text_to_two_key(text, false)
	cs = checksum(buttons)
	fmt.Printf("\"%s\"    checksum for call = %d\n", buttons, cs)

	buttons, errs = tt_text_to_call10(text, true)
	if errs == 0 {
		fmt.Printf("Push buttons for fixed length 10 digit callsign:\n")
		fmt.Printf("\"%s\"\n", buttons)
	}

	buttons, errs = tt_text_to_mhead(text, true)
	if errs == 0 {
		fmt.Printf("Push buttons for Maidenhead Grid Square Locator:\n")
		fmt.Printf("\"%s\"\n", buttons)
	}

	buttons, errs = tt_text_to_satsq(text, true)
	if errs == 0 {
		fmt.Printf("Push buttons for satellite gridsquare:\n")
		fmt.Printf("\"%s\"\n", buttons)
	}
}
