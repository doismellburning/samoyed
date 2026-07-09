package direwolf

// Thin exported wrappers around package-internal functionality needed by
// cmd/samoyed-kissutil, which lives outside this package so it can share
// the KISS/AX.25 handling with the rest of direwolf without pulling the
// whole kissutil CLI into the package.

func AX25Delete(pp *packet_t) {
	ax25_delete(pp)
}
