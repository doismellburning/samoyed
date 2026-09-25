package direwolf

import (
	"encoding/binary"
	"io"
)

// AGWPECallsign is a callsign as an AGWPE header carries it, padded out with
// NULs.  String trims the padding, so it formats as the callsign with %s.
type AGWPECallsign [10]byte

func (c AGWPECallsign) String() string {
	return ByteArrayToString(c[:])
}

type AGWPEHeader struct {
	Portx        byte
	Reserved1    byte
	Reserved2    byte
	Reserved3    byte
	DataKind     byte
	Reserved4    byte
	PID          byte
	Reserved5    byte
	CallFrom     AGWPECallsign
	CallTo       AGWPECallsign
	DataLen      uint32
	UserReserved [4]byte
}

type AGWPEMessage struct {
	Header AGWPEHeader
	Data   []byte
}

// Write sends msg to w. binary.Write won't send variable-length slices, and I keep forgetting that, so...
func (msg *AGWPEMessage) Write(w io.Writer, order binary.ByteOrder) (int, error) {
	var headerErr = binary.Write(w, order, msg.Header)
	if headerErr != nil {
		return 0, headerErr
	}

	if msg.Header.DataLen > 0 {
		return w.Write(msg.Data)
	}

	return 0, nil
}
