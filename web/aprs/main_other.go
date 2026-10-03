// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !js

package main

import (
	"fmt"
	"os"
)

// main exists so the package builds, tests and lints natively; the program
// itself only does anything in a browser.
func main() {
	fmt.Fprintln(os.Stderr, "This is the in-browser APRS tool: build it with `make web`.")
	os.Exit(1)
}
