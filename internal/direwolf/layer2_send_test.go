// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"path/filepath"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/fx25"
	"github.com/doismellburning/samoyed/internal/il2p"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/doismellburning/samoyed/internal/wav"
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

	var aloneLen = fx25.NewSender(linecode.NewEncoder(func(level int) {
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

// SendFrame sends IL2P with the trailing CRC when the channel's settings ask
// for it, and without when they don't, just as the IL2P sender itself does
// when asked.
func TestIL2PSendFrameFollowsTheChannelsCRCSetting(t *testing.T) {
	il2p.Init(0)

	var pp = newHDLCSendTestPacket(t, 16)

	var alone = map[bool][]int{}

	for _, crc := range []bool{false, true} {
		var audioConfig = newHDLCSendTestConfig(LAYER2_IL2P)
		audioConfig.achan[hdlcSendTestChannel].il2p_version = il2p.VersionCompat
		audioConfig.achan[hdlcSendTestChannel].il2p_crc = crc

		var bits = captureBits(t, audioConfig, func(s *Layer2Sender) {
			s.SendFrame(pp, false)
		})

		var sender = il2p.NewSender(linecode.NewEncoder(func(level int) {
			alone[crc] = append(alone[crc], level)
		}), 0)
		require.Positive(t, sender.SendFrame(pp, il2p.VersionCompat, 0, crc, 0))

		assert.Equal(t, alone[crc], bits, "il2p_crc = %v", crc)
	}

	assert.Greater(t, len(alone[true]), len(alone[false]), "the CRC should make the frame longer")
}

// IL2P goes out without NRZI, on the same line HDLC uses.  It must leave the
// NRZI level alone, or an HDLC frame sent after it would start from the wrong
// level and arrive with its first bit inverted.
func TestIL2PLeavesTheNRZILevelForTheNextHDLCFrame(t *testing.T) {
	il2p.Init(0)

	// A stuffed zero leaves the line at 1 after this frame, where starting
	// the next frame afresh from 0 would show.
	var first = []byte{hdlcSixtyOne, 'Q', '1', 'T', 'E', 'S', 'T'}
	var second = []byte{'Q', '2', 'T', 'E', 'S', 'T'}

	var pp = newHDLCSendTestPacket(t, 16)

	var withoutIL2P = captureBits(t, nil, func(s *Layer2Sender) {
		s.hdlc.SendFrame(first, false)
		s.hdlc.SendFrame(second, false)
	})

	for _, polarity := range []int{0, 1} {
		var firstLen, il2pLen int

		var withIL2P = captureBits(t, nil, func(s *Layer2Sender) {
			firstLen = s.hdlc.SendFrame(first, false)
			il2pLen = s.il2p.SendFrame(pp, il2p.VersionCompat, 0, false, polarity)
			s.hdlc.SendFrame(second, false)
		})

		require.Positive(t, il2pLen)
		require.Equal(t, 1, withIL2P[firstLen-1], "the first frame should leave the line at 1")

		var after = append(append([]int{}, withIL2P[:firstLen]...), withIL2P[firstLen+il2pLen:]...)

		assert.Equal(t, withoutIL2P, after, "polarity %d: the HDLC frames should be as if the IL2P frame had not been sent", polarity)
	}
}

// The EAS SAME serializer is not HDLC at all: bytes go out as they are, with
// quiet periods around and between the repeats.
func TestEASSendRepeatsTheMessageWithItsPreamble(t *testing.T) {
	var toneGenerator = setupEASSendTest(t)

	const (
		repeat  = 2
		txdelay = 100
		txtail  = 50
		gap     = 1000
	)

	var message = []byte("ZCZC-Q1TEST")

	var elapsed int

	var bits = captureBitsWithToneGenerator(t, nil, toneGenerator, func(s *Layer2Sender) {
		elapsed = s.sendEAS(message, repeat, txdelay, txtail)
	})

	var preamble = make([]byte, 16)
	for i := range preamble {
		preamble[i] = 0xAB
	}

	var oneRepeat = append(append([]byte{}, preamble...), message...)
	var expected = append(append([]byte{}, oneRepeat...), oneRepeat...)

	// No NRZI and no stuffing here, just the bytes, least significant bit
	// first.
	var sentBits = make([]bool, 0, len(bits))
	for _, bit := range bits {
		sentBits = append(sentBits, bit != 0)
	}

	assert.Equal(t, expected, packLSBFirst(t, sentBits), "each repeat is the preamble followed by the message")

	// The time to hold PTT for covers the data, the gap between the
	// repeats, and the delays at each end.
	var expectedElapsed = txdelay + int(float64(len(expected))*8*1.92) + gap + txtail
	assert.Equal(t, expectedElapsed, elapsed)
}

// setupEASSendTest gives the channel a tone generator, somewhere to put the
// quiet periods that surround an EAS message, which do not go through the bit
// capture.
func setupEASSendTest(t *testing.T) *ToneGenerator {
	t.Helper()

	var audioConfig = newHDLCSendTestConfig(LAYER2_AX25)
	audioConfig.achan[hdlcSendTestChannel].modem_type = MODEM_EAS

	// Samples go to a file rather than to an audio device.
	var w, err = wav.Create(filepath.Join(t.TempDir(), "eas.wav"), wav.Format{
		NumChannels:   audioConfig.adev[0].num_channels,
		SamplesPerSec: audioConfig.adev[0].samples_per_sec,
		BitsPerSample: audioConfig.adev[0].bits_per_sample,
	})
	require.NoError(t, err)

	t.Cleanup(func() { w.Close() })

	return NewToneGenerator(hdlcSendTestChannel, audioConfig, 100, newWAVFileSink(w))
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
