// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/doismellburning/samoyed/internal/phy"
)

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
	retries phy.BitFixLevel // Symbols the Reed Solomon decoder had to correct.
}

// il2pLoopbackRecorder holds a sender wired straight into a receiver, and
// collects the frames that come back out of the receiver.
type il2pLoopbackRecorder struct {
	sender *IL2PSender
	rx     *il2pReceiver
	frames []il2pLoopbackFrame
}

// take returns the frames received since the last call, and forgets them.
func (r *il2pLoopbackRecorder) take() []il2pLoopbackFrame {
	var frames = r.frames
	r.frames = nil

	return frames
}

// flush sends the receiver the one extra bit its state machine needs to
// finish decoding a frame whose last bit it has already seen.
func (r *il2pLoopbackRecorder) flush() {
	r.rx.recBit(0)
}

// il2pLoopback wires a sender's bit stream straight into a receiver, and
// collects the frames that come back out.  That is the same serialize and
// deserialize path used on the air, without the audio hardware at one end
// and the data link queue at the other.
//
// The receiver speaks the given version and expects a trailing CRC.  Frames
// sent with the recorder's sender should ask for one.
func il2pLoopback(t *testing.T, version il2p_version_t) *il2pLoopbackRecorder {
	t.Helper()

	var recorder = new(il2pLoopbackRecorder)

	// A receiver of its own, so this test neither inherits nor bequeaths a
	// half-gathered frame.  A decoder left part way through gathering a payload
	// swallows the next frame it is given while it resynchronises, which a
	// deliberate version mismatch is apt to leave behind.
	recorder.rx = newIL2PReceiver(0, 0, 0, version, true,
		func(int, int) ax25.ALevel { return ax25.ALevel{Rec: 0, Mark: 0, Space: 0} },
		func(_ int, _ int, _ int, pp *ax25.Packet, _ ax25.ALevel, retries phy.BitFixLevel, _ phy.FECType) {
			recorder.frames = append(recorder.frames, il2pLoopbackFrame{info: pp.Info(), retries: retries})
		})

	recorder.sender = NewIL2PSender(linecode.NewEncoder(recorder.rx.recBit), 0)

	return recorder
}
