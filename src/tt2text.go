package direwolf

// #include <stdlib.h>
// #include <stdio.h>
// #include <string.h>
// #include <ctype.h>
// #include <assert.h>
// #include <stdarg.h>
import "C"

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

func TT2Text(goButtons string) {
	var buttons = C.CString(goButtons)

	switch tt_guess_type(buttons) {
	case TT_MULTIPRESS:
		fmt.Printf("Looks like multi-press encoding.\n")
	case TT_TWO_KEY:
		fmt.Printf("Looks like two-key encoding.\n")
	default:
		fmt.Printf("Could be either type of encoding.\n")
	}

	var _text [1000]C.char
	var text = &_text[0]
	var n C.int

	fmt.Printf("Decoded text from multi-press method:\n")
	tt_multipress_to_text(buttons, 0, text)
	fmt.Printf("\"%s\"\n", C.GoString(text))

	fmt.Printf("Decoded text from two-key method:\n")
	tt_two_key_to_text(buttons, 0, text)
	fmt.Printf("\"%s\"\n", C.GoString(text))

	n = tt_call10_to_text(buttons, 1, text)
	if n == 0 {
		fmt.Printf("Decoded callsign from 10 digit method:\n")
		fmt.Printf("\"%s\"\n", C.GoString(text))
	}

	n = tt_mhead_to_text(buttons, 1, text, C.ulong(len(_text)))
	if n == 0 {
		fmt.Printf("Decoded Maidenhead Locator from DTMF digits:\n")
		fmt.Printf("\"%s\"\n", C.GoString(text))
	}

	n = tt_satsq_to_text(buttons, 1, text)
	if n == 0 {
		fmt.Printf("Decoded satellite gridsquare from 4 DTMF digits:\n")
		fmt.Printf("\"%s\"\n", C.GoString(text))
	}
}
