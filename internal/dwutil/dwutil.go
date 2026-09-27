// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package dwutil holds small helpers that code ported from Dire Wolf's C leans
// on in place of C idioms Go lacks - assert, the ?: operator - so that packages
// split out of src can share them.
package dwutil

import (
	"fmt"
	"runtime"
)

// Assert panics, naming the caller's file and line, when t is false - C's
// assert. It can't be named "assert" because of conflicts with
// stretchr/testify/assert, but otherwise, it's compatible enough.
func Assert(t bool) {
	if !t {
		_, file, line, _ := runtime.Caller(1)
		panic(fmt.Sprintf("Assertion failed at %s:%d", file, line))
	}
}

// IfThenElse exists because sometimes it's really convenient to have C's ternary ?:.
func IfThenElse[T any](x bool, a T, b T) T {
	if x {
		return a
	} else {
		return b
	}
}

// HexDump prints p to stdout, 16 bytes to a line: the offset, the bytes in
// hexadecimal, and those that are printable ASCII as themselves.
func HexDump(p []byte) {
	var offset = 0
	var length = len(p)

	for length > 0 {
		var n = min(length, 16)

		fmt.Printf("  %03x: ", offset)

		for i := range n {
			fmt.Printf(" %02x", p[i])
		}

		for i := n; i < 16; i++ {
			fmt.Print("   ")
		}

		fmt.Print("  ")

		for i := range n {
			if p[i] >= 0x20 && p[i] <= 0x7E {
				fmt.Printf("%c", p[i])
			} else {
				fmt.Print(".")
			}
		}

		fmt.Println()

		p = p[n:]
		offset += n
		length -= n
	}
}
