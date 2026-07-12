package direwolf

// Thin exported wrappers around package-internal functionality needed by
// cmd/samoyed-decode_aprs, which lives outside this package so it can share
// the AX.25/APRS decoding with the rest of direwolf without pulling the
// whole decode_aprs CLI into the package.

func TextColorSetInfo() {
	text_color_set(DW_COLOR_INFO)
}

func KissUnwrap(in []byte) []byte {
	return kiss_unwrap(in)
}
