// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package il2p

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// receiveTestRecorder collects what a Receiver hands its sink.
type receiveTestRecorder struct {
	packets   []*ax25.Packet
	corrected []int
}

func (r *receiveTestRecorder) sink(_ int, _ int, _ int, pp *ax25.Packet, corrected int) {
	r.packets = append(r.packets, pp)
	r.corrected = append(r.corrected, corrected)
}

// receiveTestBits is what goes over the air for an encoded frame: the
// preamble, the sync word and the frame, most significant bit first, inverted
// for reverse polarity.
func receiveTestBits(encoded []byte, invert bool) []int {
	var data = append([]byte{Preamble, (SyncWord >> 16) & 0xff, (SyncWord >> 8) & 0xff, SyncWord & 0xff}, encoded...)

	var out []int

	for _, b := range data {
		for i := 7; i >= 0; i-- {
			var bit = int(b>>i) & 1
			if invert {
				bit ^= 1
			}

			out = append(out, bit)
		}
	}

	return append(out, 0) // One more to flush the state machine.
}

// receiveTestPacket builds its frame directly rather than from text: the IL2P
// header cannot represent every combination of the AX.25 address C bits, and
// a frame that changes shape in flight fails the trailing CRC check.
func receiveTestPacket(t *testing.T) *ax25.Packet {
	t.Helper()

	var addrs [ax25.MaxAddrs]string
	addrs[0] = "Q1TEST"
	addrs[1] = "Q2TEST"

	var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, 0xF0, []byte("Hello, IL2P"))
	require.NotNil(t, pp)

	return pp
}

func TestReceiver(t *testing.T) {
	Init(0)

	var pp = receiveTestPacket(t)

	for _, testDatum := range []struct {
		name    string
		version Version
		crc     bool
		invert  bool
		damage  bool
	}{
		{"v0.6", Version0_6, false, false, false},
		{"v0.6 with CRC", Version0_6, true, false, false},
		{"v0.4 with CRC", Version0_4, true, false, false},
		{"reverse polarity", Version0_6, true, true, false},
		{"damaged", Version0_6, true, false, true},
	} {
		t.Run(testDatum.name, func(t *testing.T) {
			var encoded, n = EncodeFrame(pp, testDatum.version, 1, testDatum.crc)
			require.Positive(t, n)

			if testDatum.damage {
				encoded[headerSize+headerParity+3] ^= 0xff
			}

			var recorder = new(receiveTestRecorder)
			var rx = NewReceiver(1, 2, 3, testDatum.version, testDatum.crc, recorder.sink)

			for _, bit := range receiveTestBits(encoded, testDatum.invert) {
				rx.RecBit(bit)
			}

			require.Len(t, recorder.packets, 1)
			assert.Equal(t, pp.FrameData(), recorder.packets[0].FrameData())

			if testDatum.damage {
				assert.Equal(t, 1, recorder.corrected[0])
			} else {
				assert.Zero(t, recorder.corrected[0])
			}
		})
	}
}

// A receiver expecting a trailing CRC drops a frame whose CRC does not match.
func TestReceiverDropsBadCRC(t *testing.T) {
	Init(0)

	var pp = receiveTestPacket(t)

	var encoded, n = EncodeFrame(pp, Version0_6, 1, true)
	require.Positive(t, n)

	// The CRC is Hamming coded, so flip enough of a byte that it can't be corrected.
	encoded[len(encoded)-1] ^= 0x0f

	var recorder = new(receiveTestRecorder)
	var rx = NewReceiver(0, 0, 0, Version0_6, true, recorder.sink)

	for _, bit := range receiveTestBits(encoded, false) {
		rx.RecBit(bit)
	}

	assert.Empty(t, recorder.packets)
}

// FuzzReceiver feeds the receiver arbitrary bits, as anyone on frequency can.
// The failure looked for is a panic.
func FuzzReceiver(f *testing.F) {
	Init(0)

	var pp = ax25.FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)

	for _, version := range []Version{Version0_4, Version0_6} {
		var encoded, n = EncodeFrame(pp, version, 0, true)
		require.Positive(f, n)
		f.Add(append([]byte{Preamble, (SyncWord >> 16) & 0xff, (SyncWord >> 8) & 0xff, SyncWord & 0xff}, encoded...), int(version), true)
	}

	f.Add([]byte{(SyncWord >> 16) & 0xff, (SyncWord >> 8) & 0xff, SyncWord & 0xff, 0xff, 0xff}, int(Version0_4), false)

	f.Fuzz(func(t *testing.T, in []byte, version int, crc bool) {
		var rx = NewReceiver(0, 0, 0, Version(version), crc, func(int, int, int, *ax25.Packet, int) {})

		for _, b := range in {
			for i := 7; i >= 0; i-- {
				rx.RecBit(int(b>>i) & 1)
			}
		}
	})
}
