// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const hdlcSendTestChannel = 0

// The HDLC flag, and the byte a test uses when it wants bit stuffing: six
// consecutive ones, one more than a run may be.
const (
	hdlcFlag     byte = 0x7e
	hdlcSixtyOne byte = 0x3f
)

// captureBits collects the bits sent to the modulator while fn runs, handing
// fn a new Layer2Sender so the stream starts from a known place.
func captureBits(t *testing.T, audioConfig *RadioConfig, fn func(s *Layer2Sender)) []int {
	t.Helper()

	return captureBitsWithToneGenerator(t, audioConfig, nil, fn)
}

// captureBitsWithToneGenerator is captureBits for a sender that sends to
// toneGenerator, for what goes to it other than bits: the quiet periods, and
// the flush.
func captureBitsWithToneGenerator(t *testing.T, audioConfig *RadioConfig, toneGenerator *ToneGenerator, fn func(s *Layer2Sender)) []int {
	t.Helper()

	var bits []int

	toneGenCapture = func(channel int, data int) {
		assert.Equal(t, hdlcSendTestChannel, channel, "bits should be sent on the channel they were asked for")

		bits = append(bits, data)
	}

	t.Cleanup(func() { toneGenCapture = nil })

	fn(NewLayer2Sender(hdlcSendTestChannel, audioConfig, toneGenerator, 0))

	toneGenCapture = nil

	return bits
}

// nrziDecode recovers the data bits from an NRZI stream: a one leaves the
// signal as it was, a zero inverts it.  The line starts at the level
// captureBits reset it to.
func nrziDecode(bits []int) []bool {
	var data []bool
	var previous = 0

	for _, bit := range bits {
		data = append(data, bit == previous)
		previous = bit
	}

	return data
}

// destuff drops the zero the sender inserts after five consecutive ones.
func destuff(bits []bool) []bool {
	var data []bool
	var ones = 0

	for _, bit := range bits {
		if ones == 5 {
			ones = 0

			continue // The stuffed zero, which was never data.
		}

		data = append(data, bit)

		if bit {
			ones++
		} else {
			ones = 0
		}
	}

	return data
}

// packLSBFirst reassembles bytes from bits in the order HDLC sends them.
func packLSBFirst(t *testing.T, bits []bool) []byte {
	t.Helper()

	require.Zero(t, len(bits)%8, "a whole number of bytes should have been sent")

	var out = make([]byte, 0, len(bits)/8)

	for i := 0; i < len(bits); i += 8 {
		var b byte

		for j := range 8 {
			if bits[i+j] {
				b |= 1 << j
			}
		}

		out = append(out, b)
	}

	return out
}

// packMSBFirst reassembles bytes from bits in the order IL2P sends them.
func packMSBFirst(t *testing.T, bits []int) []byte {
	t.Helper()

	require.Zero(t, len(bits)%8, "a whole number of bytes should have been sent")

	var out = make([]byte, 0, len(bits)/8)

	for i := 0; i < len(bits); i += 8 {
		var b byte

		for j := range 8 {
			if bits[i+j] != 0 {
				b |= 0x80 >> j
			}
		}

		out = append(out, b)
	}

	return out
}

// newHDLCSendTestConfig is one 1200 baud AFSK channel sending the given layer
// 2 protocol.
func newHDLCSendTestConfig(layer2 layer2_t) *RadioConfig {
	var audioConfig = new(RadioConfig)

	audioConfig.adev[0].defined = 1
	audioConfig.adev[0].num_channels = 1
	audioConfig.adev[0].bits_per_sample = 16
	audioConfig.adev[0].samples_per_sec = 44100

	audioConfig.chan_medium[hdlcSendTestChannel] = MEDIUM_RADIO
	audioConfig.achan[hdlcSendTestChannel].modem_type = MODEM_AFSK
	audioConfig.achan[hdlcSendTestChannel].baud = 1200
	audioConfig.achan[hdlcSendTestChannel].mark_freq = 1200
	audioConfig.achan[hdlcSendTestChannel].space_freq = 2200
	audioConfig.achan[hdlcSendTestChannel].layer2_xmit = layer2

	return audioConfig
}

// hdlcFrameFromBits takes the frame out of a captured stream: a flag at each
// end, and in between it the frame with the stuffing the sender added.
func hdlcFrameFromBits(t *testing.T, bits []int) []byte {
	t.Helper()

	var data = nrziDecode(bits)

	require.Greater(t, len(data), 16, "there should be a frame between the flags")

	// Flags are sent without stuffing, so they are whole bytes at each end
	// of the stream.
	assert.Equal(t, []byte{hdlcFlag}, packLSBFirst(t, data[:8]), "missing start flag")
	assert.Equal(t, []byte{hdlcFlag}, packLSBFirst(t, data[len(data)-8:]), "missing end flag")

	return packLSBFirst(t, destuff(data[8:len(data)-8]))
}

// FX.25 and AX.25 go out on the same line, so a codeblock has to start from
// the level whatever went before it left the line at, or its first bit is
// received inverted.
func TestFX25FrameCarriesOnFromTheLineLevelBeforeIt(t *testing.T) {
	// A frame and its FCS always hold an even number of zeros, so it takes
	// a stuffed zero to leave the line at 1, where starting the codeblock
	// afresh from 0 would show.
	var before = []byte{hdlcSixtyOne, 'Q', '1', 'T', 'E', 'S', 'T'}
	var fbuf = []byte{'Q', '2', 'T', 'E', 'S', 'T'}

	// The codeblock as an FX.25 sender sends it on a line of its own, which
	// starts low.
	var alone []int

	var aloneLen = NewFX25Sender(linecode.NewEncoder(func(level int) {
		alone = append(alone, level)
	}), 0, 0).SendFrame(fbuf, 16)
	require.Positive(t, aloneLen)

	var beforeLen int

	var bits = captureBits(t, nil, func(s *Layer2Sender) {
		beforeLen = s.hdlc.SendFrame(before, false)
		s.fx25.SendFrame(fbuf, 16)
	})

	require.Equal(t, 1, bits[beforeLen-1], "the frame before should leave the line at 1")

	assert.Equal(t, nrziDecode(alone), nrziDecode(bits)[beforeLen:],
		"the codeblock should carry the same data as if it had had the line to itself")
}

// newHDLCSendTestPacket is a packet with an information part of the requested
// length.
func newHDLCSendTestPacket(t *testing.T, infoLen int) *ax25.Packet {
	t.Helper()

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Destination] = "Q2TEST"
	addrs[ax25.Source] = "Q1TEST"

	var pinfo = make([]byte, infoLen)
	for i := range pinfo {
		pinfo[i] = byte('a' + i%26)
	}

	var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, 0xF0, pinfo)
	require.NotNil(t, pp)

	return pp
}
