package direwolf

// A lightweight reimplementation of Dire Wolf's textcolor.c

type dw_color_e int

const (
	DW_COLOR_INFO    dw_color_e = iota /* black */
	DW_COLOR_ERROR                     /* red */
	DW_COLOR_REC                       /* green */
	DW_COLOR_DECODED                   /* blue */
	DW_COLOR_XMIT                      /* magenta */
	DW_COLOR_DEBUG                     /* dark_green */
)

func text_color_set(_ dw_color_e) {}
