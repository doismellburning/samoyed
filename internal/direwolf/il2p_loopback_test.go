// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/il2p"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	retries BitFixLevel // Symbols the Reed Solomon decoder had to correct.
}

// il2pLoopbackRecorder collects the frames that came back out of the receiver.
type il2pLoopbackRecorder struct {
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

// il2pLoopback wires the transmitter's bit stream straight into the receiver
// and collects the frames that come back out, for the duration of the test.
// That is the same serialize and deserialize path used on the air, standing in
// for the audio hardware at one end and the data link queue at the other.
//
// The receiver speaks the given version and expects a trailing CRC.  The
// transmitter adds one because its configuration asks for it.
func il2pLoopback(t *testing.T, version il2p.Version) *il2pLoopbackRecorder {
	t.Helper()

	var recorder = new(il2pLoopbackRecorder)

	var savedTone, savedRec = toneGenCapture, multiModemRecCapture

	t.Cleanup(func() {
		toneGenCapture, multiModemRecCapture = savedTone, savedRec
	})

	// A receiver of its own, so this test neither inherits nor bequeaths a
	// half-gathered frame.  A decoder left part way through gathering a payload
	// swallows the next frame it is given while it resynchronises, which a
	// deliberate version mismatch is apt to leave behind.
	recorder.rx = newIL2PReceiver(0, 0, 0, version, true, il2p_deliver_packet)

	toneGenCapture = func(channel int, data int) {
		require.Zero(t, channel)
		recorder.rx.recBit(data)
	}

	multiModemRecCapture = func(_ int, _ int, _ int, pp *ax25.Packet, _ ax25.ALevel, retries BitFixLevel, _ fec_type_t) {
		recorder.frames = append(recorder.frames, il2pLoopbackFrame{info: pp.Info(), retries: retries})
	}

	return recorder
}

// Send a frame over the fake modem and see whether the receiver, speaking the
// version it was given, makes sense of it.
func TestIL2POnAirVersions(t *testing.T) {
	il2p.Init(0)

	// Check the information part of whatever arrives against il2pTestText, so
	// build the frame directly rather than from text: the IL2P header cannot
	// represent every combination of the AX.25 address C bits, and a frame
	// that changes shape in flight fails the trailing CRC check.
	var addrs [ax25.MaxAddrs]string
	addrs[0] = "Q1TEST"
	addrs[1] = "Q2TEST"

	var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, 0xF0, []byte(il2pTestText))
	require.NotNil(t, pp)

	var testData = []struct {
		name       string
		tx_version il2p.Version
		max_fec    int
		rx_version il2p.Version
		received   bool
	}{
		{"v0.4 automatic FEC to v0.4", il2p.Version0_4, 0, il2p.Version0_4, true},
		{"v0.4 max FEC to v0.4", il2p.Version0_4, 1, il2p.Version0_4, true},
		{"v0.6 to v0.6", il2p.Version0_6, 0, il2p.Version0_6, true},
		{"v0.6 to compat", il2p.Version0_6, 0, il2p.VersionCompat, true},
		// v0.4 max FEC has the same payload sizing as v0.6 and differs only in
		// the header bit, which a v0.6 receiver ignores.
		{"v0.4 max FEC to v0.6", il2p.Version0_4, 1, il2p.Version0_6, true},
		{"v0.4 max FEC to compat", il2p.Version0_4, 1, il2p.VersionCompat, true},
		// A compatibility frame with max FEC is understood by both.
		{"compat to compat", il2p.VersionCompat, 1, il2p.VersionCompat, true},
		{"compat to v0.4", il2p.VersionCompat, 1, il2p.Version0_4, true},
		{"compat to v0.6", il2p.VersionCompat, 1, il2p.Version0_6, true},
		// These are the mismatches the version setting exists for.
		{"v0.6 to v0.4", il2p.Version0_6, 0, il2p.Version0_4, false},
		{"v0.4 automatic FEC to v0.6", il2p.Version0_4, 0, il2p.Version0_6, false},
		{"compat automatic FEC to compat", il2p.VersionCompat, 0, il2p.VersionCompat, false},
	}

	for _, testDatum := range testData {
		t.Run(testDatum.name, func(t *testing.T) {
			var recorder = il2pLoopback(t, testDatum.rx_version)

			require.Positive(t, NewHDLCSender(0, nil, 0).sendIL2PFrame(pp, testDatum.tx_version, testDatum.max_fec, true, 0))

			recorder.flush() // Extra bit to flush the state machine.

			var received = recorder.take()

			if testDatum.received {
				require.Len(t, received, 1)
				assert.Equal(t, il2pTestText, string(received[0].info))
			} else {
				assert.Empty(t, received)
			}
		})
	}
}
