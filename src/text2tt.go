package direwolf

// #include "direwolf.h"
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
	var goText = strings.Join(args, " ")
	var text = C.CString(goText)
	var _buttons [2000]C.char
	var buttons = &_buttons[0]
	var n C.int
	var cs int

	fmt.Printf("Push buttons for multi-press method:\n")
	tt_text_to_multipress(text, 0, buttons)
	cs = checksum(C.GoString(buttons))
	fmt.Printf("\"%s\"    checksum for call = %d\n", C.GoString(buttons), cs)

	fmt.Printf("Push buttons for two-key method:\n")
	tt_text_to_two_key(text, 0, buttons)
	cs = checksum(C.GoString(buttons))
	fmt.Printf("\"%s\"    checksum for call = %d\n", C.GoString(buttons), cs)

	n = tt_text_to_call10(text, 1, buttons)
	if n == 0 {
		fmt.Printf("Push buttons for fixed length 10 digit callsign:\n")
		fmt.Printf("\"%s\"\n", C.GoString(buttons))
	}

	n = tt_text_to_mhead(text, 1, buttons, C.ulong(len(_buttons)))
	if n == 0 {
		fmt.Printf("Push buttons for Maidenhead Grid Square Locator:\n")
		fmt.Printf("\"%s\"\n", C.GoString(buttons))
	}

	n = tt_text_to_satsq(text, 1, buttons, C.ulong(len(_buttons)))
	if n == 0 {
		fmt.Printf("Push buttons for satellite gridsquare:\n")
		fmt.Printf("\"%s\"\n", C.GoString(buttons))
	}
}
