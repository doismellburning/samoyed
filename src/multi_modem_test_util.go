// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//nolint:gochecknoglobals
package direwolf

// multiModemRecCapture, when set, is given each frame the demodulators
// reconstruct instead of the rest of the receive path.  It is how a loopback
// test sees what came back out of a decoder, with no data link queue behind it
// and no audio device in front.
//
// The same shape as toneGenCapture, and for the same reason: a test needs to
// watch something that would otherwise disappear into the rest of the program.
var multiModemRecCapture func(channel int, subchannel int, slice int, pp *packet_t, alevel ALevel, retries BitFixLevel, fec_type fec_type_t)

// multi_modem_process_rec_packet hands one received frame to whatever is
// standing in for the rest of the receive path.
func multi_modem_process_rec_packet(channel int, subchannel int, slice int, pp *packet_t, alevel ALevel, retries BitFixLevel, fec_type fec_type_t) {
	if multiModemRecCapture != nil {
		multiModemRecCapture(channel, subchannel, slice, pp, alevel, retries, fec_type)

		return
	}

	multi_modem_process_rec_packet_real(channel, subchannel, slice, pp, alevel, retries, fec_type)
}
