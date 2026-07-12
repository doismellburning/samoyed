package direwolf

// Thin exported wrappers around package-internal functionality needed by
// cmd/samoyed-decode_aprs, which lives outside this package so it can share
// the AX.25/APRS decoding with the rest of direwolf without pulling the
// whole decode_aprs CLI into the package.

func TextColorSetInfo() {
	text_color_set(DW_COLOR_INFO)
}

func DecodeAndPrintAPRS(pp *packet_t, quiet bool, thirdPartySrc string) {
	var a = decode_aprs(pp, quiet, thirdPartySrc)

	decode_aprs_print(a)
}

func AX25CheckAddresses(pp *packet_t) bool {
	return ax25_check_addresses(pp)
}

func AX25HexDump(pp *packet_t) {
	ax25_hex_dump(pp)
}

func KissUnwrap(in []byte) []byte {
	return kiss_unwrap(in)
}
