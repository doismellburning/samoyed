// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import "testing"

const il2pTestText = `'... As I was saying, that seems to be done right - though I haven't time to look it over thoroughly just now - and ` +
	`that shows that there are three hundred and sixty-four days when you might get un-birthday presents -'
'Certainly,' said Alice.
'And only one for birthday presents, you know. There's glory for you!'
'I don't know what you mean by \"glory\",' Alice said.
Humpty Dumpty smiled contemptuously. 'Of course you don't - till I tell you. I meant \"there's a nice knock-down argument for you!\"'
'But \"glory\" doesn't mean \"a nice knock-down argument\",' Alice objected.
'When I use a word,' Humpty Dumpty said, in rather a scornful tone, 'it means just what I choose it to mean - neither more nor less.'
'The question is,' said Alice, 'whether you can make words mean so many different things.'
'The question is,' said Humpty Dumpty, 'which is to be master - that's all.'
`

// il2pLoopbackFrame is one frame the receiver reconstructed, as much of it as a
// test has anything to say about.
type il2pLoopbackFrame struct {
	info    []byte
	retries BitFixLevel // Symbols the Reed Solomon decoder had to correct.
}

// il2pLoopbackRecorder collects the frames that came back out of the receiver.
type il2pLoopbackRecorder struct {
	frames []il2pLoopbackFrame
}

// take returns the frames received since the last call, and forgets them.
func (r *il2pLoopbackRecorder) take() []il2pLoopbackFrame {
	var frames = r.frames
	r.frames = nil

	return frames
}

// clearIL2PReceivers forgets any partly-received frame, on every channel.  A
// decoder left part way through gathering a payload swallows the next frame it
// is given while it resynchronises, which a deliberate version mismatch is apt
// to leave behind.
func clearIL2PReceivers() {
	il2p_context = [MAX_RADIO_CHANS][MAX_SUBCHANS][MAX_SLICERS]*il2p_context_s{}
}

// il2pLoopback wires the transmitter's bit stream straight into the receiver
// and collects the frames that come back out, for the duration of the test.
// That is the same serialize and deserialize path used on the air, standing in
// for the audio hardware at one end and the data link queue at the other.
//
// The caller points save_audio_config_p at a configuration first - the receive
// path reads the channel's IL2P version from it, and its audio level from the
// demodulator state - so il2pTestChannelVersion comes before this.
func il2pLoopback(t *testing.T) *il2pLoopbackRecorder {
	t.Helper()

	var recorder = new(il2pLoopbackRecorder)

	var savedTone, savedRec = toneGenCapture, multiModemRecCapture

	t.Cleanup(func() {
		toneGenCapture, multiModemRecCapture = savedTone, savedRec
	})

	// Start from a receiver with nothing in flight, and leave one behind, so
	// this test neither inherits nor bequeaths a half-gathered frame.
	clearIL2PReceivers()
	t.Cleanup(clearIL2PReceivers)

	toneGenCapture = func(channel int, data int) {
		il2p_rec_bit(channel, 0, 0, data)
	}

	multiModemRecCapture = func(_ int, _ int, _ int, pp *packet_t, _ ALevel, retries BitFixLevel, _ fec_type_t) {
		recorder.frames = append(recorder.frames, il2pLoopbackFrame{info: AX25GetInfo(pp), retries: retries})
	}

	return recorder
}
