package direwolf

import (
	"encoding/binary"
	"io"
)

type AGWPEHeader struct {
	Portx        byte
	Reserved1    byte
	Reserved2    byte
	Reserved3    byte
	DataKind     byte
	Reserved4    byte
	PID          byte
	Reserved5    byte
	CallFrom     [10]byte
	CallTo       [10]byte
	DataLen      uint32
	UserReserved [4]byte
}

type AGWPEMessage struct {
	Header AGWPEHeader
	Data   []byte
}

// binary.Write won't send variable-length slices, and I keep forgetting that, so...
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
