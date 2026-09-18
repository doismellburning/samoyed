// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package textcolor is a lightweight reimplementation of Dire Wolf's
// textcolor.c: output is labelled with the kind of message it is, so it can
// eventually be coloured accordingly.
//
// It lives here, rather than in the main package, so that the packages carved
// out of that one can report to the user the same way everything else does.
//
//nolint:gochecknoglobals
package textcolor

import "fmt"

type Color int

const (
	Info    Color = iota /* black */
	Error                /* red */
	Rec                  /* green */
	Decoded              /* blue */
	Xmit                 /* magenta */
	Debug                /* dark_green */
)

var _text_color_level int

func Init(level int) {
	_text_color_level = level
}

func Set(_ Color) {
	if _text_color_level == 0 {
		return
	}

	// TODO KG
}

// Printf writes a message to the user, in whichever colour Set last selected.
func Printf(format string, a ...any) (int, error) {
	// Can't call variadic functions through cgo, so let's define our own!
	// Fortunately dw_printf doesn't do much
	return fmt.Printf(format, a...)
}
