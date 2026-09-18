package direwolf

// A lightweight reimplementation of Dire Wolf's textcolor.c
//
// The implementation lives in internal/textcolor, so that the packages carved
// out of this one can report to the user the same way; what follows are the
// names the rest of this package - and Dire Wolf before it - uses for it.

import (
	"github.com/doismellburning/samoyed/internal/textcolor"
)

type dw_color_e = textcolor.Color

const (
	DW_COLOR_INFO    = textcolor.Info
	DW_COLOR_ERROR   = textcolor.Error
	DW_COLOR_REC     = textcolor.Rec
	DW_COLOR_DECODED = textcolor.Decoded
	DW_COLOR_XMIT    = textcolor.Xmit
	DW_COLOR_DEBUG   = textcolor.Debug
)

func TextColorInit(level int) {
	textcolor.Init(level)
}

func text_color_set(color dw_color_e) {
	textcolor.Set(color)
}
