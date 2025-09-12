package direwolf

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
