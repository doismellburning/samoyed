package direwolf

// Thin exported wrappers around package-internal functionality needed by
// cmd/samoyed-kissutil, which lives outside this package so it can share
// the KISS/AX.25 handling with the rest of direwolf without pulling the
// whole kissutil CLI into the package.

func AX25FromText(monitor string, strict bool) *packet_t {
	return ax25_from_text(monitor, strict)
}

func AX25Pack(pp *packet_t) []byte {
	return ax25_pack(pp)
}

func AX25Delete(pp *packet_t) {
	ax25_delete(pp)
}

func AX25FromFrame(data []byte, alevel ALevel) *packet_t {
	return ax25_from_frame(data, alevel)
}

func AX25GetInfo(pp *packet_t) []byte {
	return ax25_get_info(pp)
}

func AX25SafePrint(info []byte, asciiOnly bool) {
	ax25_safe_print(info, asciiOnly)
}
