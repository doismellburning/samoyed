package direwolf

import "github.com/pkg/term"

// Thin exported wrappers around package-internal functionality needed by
// cmd/samoyed-kissutil, which lives outside this package so it can share
// the KISS/AX.25 handling with the rest of direwolf without pulling the
// whole kissutil CLI into the package.

// KISSFrame is the exported alias for kiss_frame_t.
type KISSFrame = kiss_frame_t

// ALevel is the exported alias for alevel_t.
type ALevel = alevel_t

func TextColorInit(level int) {
	text_color_init(level)
}

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

func AX25FormatAddrs(pp *packet_t) string {
	return ax25_format_addrs(pp)
}

func AX25GetInfo(pp *packet_t) []byte {
	return ax25_get_info(pp)
}

func AX25SafePrint(info []byte, asciiOnly bool) {
	ax25_safe_print(info, asciiOnly)
}

func KissEncapsulate(in []byte) []byte {
	return kiss_encapsulate(in)
}

func KissRecByte(kf *KISSFrame, ch byte, debug int, kps *kissport_status_s, client int, sendfun kiss_sendfun) {
	kiss_rec_byte(kf, ch, debug, kps, client, sendfun)
}

func HexDump(p []byte) {
	hex_dump(p)
}

func SerialPortOpen(devicename string, baud int) *term.Term {
	return serial_port_open(devicename, baud)
}

func SerialPortGet1(fd *term.Term) (byte, error) {
	return serial_port_get1(fd)
}

func SerialPortWrite(fd *term.Term, data []byte) int {
	return serial_port_write(fd, data)
}
