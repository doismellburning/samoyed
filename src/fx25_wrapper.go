package direwolf

// FX25Init is an exported wrapper around fx25_init, for use by cmd/samoyed-fxsend and cmd/samoyed-fxrec.
func FX25Init(debug_level int) {
	fx25_init(debug_level)
}

// FX25SendFrame is an exported wrapper around fx25_send_frame, for use by cmd/samoyed-fxsend.
func FX25SendFrame(channel int, fbuf []byte, fx_mode int, test_mode bool) int {
	return fx25_send_frame(channel, fbuf, fx_mode, test_mode)
}

// FX25RecBit is an exported wrapper around fx25_rec_bit, for use by cmd/samoyed-fxrec.
func FX25RecBit(channel int, subchannel int, slice int, dbit int) {
	fx25_rec_bit(channel, subchannel, slice, dbit)
}

// FX25TestCount exposes fx25_test_count, for use by cmd/samoyed-fxrec.
func FX25TestCount() int {
	return fx25_test_count
}
