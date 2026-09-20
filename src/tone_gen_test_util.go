// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//nolint:gochecknoglobals
package direwolf

// toneGenCapture, when set, is given the bits a frame serializes to instead of
// the tone generator.  It is how a test sees what would have gone out over the
// air without an audio device to send it to.
var toneGenCapture func(channel int, data int)

// tone_gen_put_bit hands one bit to whatever is standing in for the
// modulator.
func tone_gen_put_bit(channel int, data int) {
	switch {
	case toneGenCapture != nil:
		toneGenCapture(channel, data)
	case IL2P_TEST:
		tone_gen_put_bit_fake(channel, data)
	default:
		tone_gen_put_bit_real(channel, data)
	}
}
