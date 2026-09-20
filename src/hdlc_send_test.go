// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"path/filepath"
	"testing"

	"github.com/doismellburning/samoyed/internal/fcs"
	"github.com/doismellburning/samoyed/internal/wavwrite"
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

// captureBits collects the bits sent to the modulator while fn runs, with the
// channel's NRZI and bit stuffing state reset first so the stream starts from
// a known place.
func captureBits(t *testing.T, fn func()) []int {
	t.Helper()

	nrziBitOutput[hdlcSendTestChannel] = 0
	stuff[hdlcSendTestChannel] = 0

	var bits []int

	toneGenCapture = func(channel int, data int) {
		assert.Equal(t, hdlcSendTestChannel, channel, "bits should be sent on the channel they were asked for")

		bits = append(bits, data)
	}

	t.Cleanup(func() { toneGenCapture = nil })

	fn()

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
func newHDLCSendTestConfig(layer2 layer2_t) *audio_s {
	var audioConfig = new(audio_s)

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

// A frame goes out between flags, with its FCS appended and any run of more
// than five ones broken up.
func TestAX25FrameIsSentBetweenFlagsWithItsFCS(t *testing.T) {
	// The 0x3f bytes are six consecutive ones each, so the frame cannot go
	// out without stuffing.
	var fbuf = []byte{hdlcSixtyOne, 'Q', '1', 'T', 'E', 'S', 'T', hdlcSixtyOne}

	var sent int

	var bits = captureBits(t, func() {
		sent = ax25_only_hdlc_send_frame(hdlcSendTestChannel, fbuf, false)
	})

	assert.Equal(t, len(bits), sent, "the count returned should be the bits actually sent")

	var frameFCS = fcs.Calc(fbuf)
	var expected = append(append([]byte{}, fbuf...), byte(frameFCS)&0xff, byte(frameFCS>>8)&0xff)

	assert.Equal(t, expected, hdlcFrameFromBits(t, bits),
		"the frame between the flags should be what was asked for, plus its FCS")

	// Stuffing is what makes the frame longer than the bytes it carries.
	assert.Greater(t, len(bits), (len(expected)+2)*8, "the long runs of ones should have been broken up")
}

// The bad FCS option is for making a frame that a receiver has to reject.
func TestAX25BadFCSSendsTheComplementOfTheRealOne(t *testing.T) {
	var fbuf = []byte{'Q', '1', 'T', 'E', 'S', 'T'}

	var good = captureBits(t, func() {
		ax25_only_hdlc_send_frame(hdlcSendTestChannel, fbuf, false)
	})
	var bad = captureBits(t, func() {
		ax25_only_hdlc_send_frame(hdlcSendTestChannel, fbuf, true)
	})

	var goodData = hdlcFrameFromBits(t, good)
	var badData = hdlcFrameFromBits(t, bad)

	require.Len(t, badData, len(goodData), "only the FCS should differ")

	var frameFCS = fcs.Calc(fbuf)

	// The FCS is the last thing in the frame.
	assert.Equal(t, []byte{byte(frameFCS) & 0xff, byte(frameFCS>>8) & 0xff}, goodData[len(goodData)-2:])
	assert.Equal(t, []byte{byte(^frameFCS) & 0xff, byte((^frameFCS)>>8) & 0xff}, badData[len(badData)-2:])
	assert.Equal(t, fbuf, badData[:len(badData)-2], "the frame itself should be unchanged")
}

// A data byte gets a zero after five consecutive ones; a flag, which has six
// of them, must not, or it would no longer be a flag.
func TestOnlyDataIsBitStuffed(t *testing.T) {
	var asData = captureBits(t, func() {
		send_data_nrzi(hdlcSendTestChannel, hdlcFlag)
	})
	var asControl = captureBits(t, func() {
		send_control_nrzi(hdlcSendTestChannel, hdlcFlag)
	})

	assert.Len(t, asControl, 8, "a flag is sent as it stands")
	assert.Equal(t, []byte{hdlcFlag}, packLSBFirst(t, nrziDecode(asControl)))

	assert.Len(t, asData, 9, "the same byte as data needs a stuffed bit")
	assert.Equal(t, []byte{hdlcFlag}, packLSBFirst(t, destuff(nrziDecode(asData))))

	// However long the run, a control byte is sent as it stands.
	var allOnes = captureBits(t, func() {
		send_control_nrzi(hdlcSendTestChannel, 0xff)
	})

	assert.Len(t, allOnes, 8)
	assert.Equal(t, []byte{0xff}, packLSBFirst(t, nrziDecode(allOnes)))
}

// A run of ones long enough to need stuffing twice gets a zero each time.
func TestBitStuffingRepeatsForALongRunOfOnes(t *testing.T) {
	var bits = captureBits(t, func() {
		send_data_nrzi(hdlcSendTestChannel, 0xff)
		send_data_nrzi(hdlcSendTestChannel, 0xff)
	})

	assert.Len(t, bits, 16+3, "sixteen ones need three stuffed zeros")
	assert.Equal(t, []byte{0xff, 0xff}, packLSBFirst(t, destuff(nrziDecode(bits))))
}

// NRZI: a one leaves the signal alone, a zero inverts it.
func TestNRZIInvertsOnAZeroOnly(t *testing.T) {
	var bits = captureBits(t, func() {
		send_bit_nrzi(hdlcSendTestChannel, true)
		send_bit_nrzi(hdlcSendTestChannel, true)
		send_bit_nrzi(hdlcSendTestChannel, false)
		send_bit_nrzi(hdlcSendTestChannel, true)
		send_bit_nrzi(hdlcSendTestChannel, false)
	})

	assert.Equal(t, []int{0, 0, 1, 1, 0}, bits)
}

// Between frames the transmitter sends flags, so the receiver has something
// to keep its clock on.
func TestPreambleIsFlagsForAX25(t *testing.T) {
	var audioConfig = newHDLCSendTestConfig(LAYER2_AX25)

	var sent int

	var bits = captureBits(t, func() {
		sent = layer2_preamble_postamble(hdlcSendTestChannel, 4, false, audioConfig)
	})

	assert.Equal(t, 4*8, sent, "flags are not stuffed, so it is eight bits a byte")
	assert.Len(t, bits, sent)
	assert.Equal(t, []byte{hdlcFlag, hdlcFlag, hdlcFlag, hdlcFlag}, packLSBFirst(t, nrziDecode(bits)))
}

// The last thing sent before the transmitter drops has to be pushed out
// rather than left sitting in a buffer.
func TestPostambleFlushesTheAudioWhenItIsTheEndOfTheTransmission(t *testing.T) {
	var audioConfig = newHDLCSendTestConfig(LAYER2_AX25)

	// The flush goes to a file rather than to an audio device, there being
	// no device open here.
	var origGenPackets = GEN_PACKETS

	t.Cleanup(func() { GEN_PACKETS = origGenPackets })

	GEN_PACKETS = true

	var sent int

	var bits = captureBits(t, func() {
		sent = layer2_preamble_postamble(hdlcSendTestChannel, 2, true, audioConfig)
	})

	assert.Equal(t, 2*8, sent, "finishing does not change what goes out")
	assert.Equal(t, []byte{hdlcFlag, hdlcFlag}, packLSBFirst(t, nrziDecode(bits)))
}

// IL2P has its own filler pattern, sent MSB first and without NRZI.
func TestPreambleIsTheIL2PPatternForIL2P(t *testing.T) {
	var audioConfig = newHDLCSendTestConfig(LAYER2_IL2P)

	var sent int

	var bits = captureBits(t, func() {
		sent = layer2_preamble_postamble(hdlcSendTestChannel, 3, false, audioConfig)
	})

	assert.Equal(t, 3*8, sent)
	assert.Equal(t, []byte{IL2P_PREAMBLE, IL2P_PREAMBLE, IL2P_PREAMBLE}, packMSBFirst(t, bits))
}

// Inverted polarity is the same pattern the other way up.
func TestIL2PPolarityInvertsEveryBit(t *testing.T) {
	var upright = captureBits(t, func() {
		send_byte_msb_first(hdlcSendTestChannel, IL2P_PREAMBLE, 0)
	})
	var inverted = captureBits(t, func() {
		send_byte_msb_first(hdlcSendTestChannel, IL2P_PREAMBLE, 1)
	})

	require.Len(t, inverted, len(upright))

	for i, bit := range upright {
		assert.Equal(t, 1-bit, inverted[i], "bit %d should be inverted", i)
	}
}

// newHDLCSendTestPacket is a packet with an information part of the requested
// length.
func newHDLCSendTestPacket(t *testing.T, infoLen int) *packet_t {
	t.Helper()

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = "Q2TEST"
	addrs[AX25_SOURCE] = "Q1TEST"

	var pinfo = make([]byte, infoLen)
	for i := range pinfo {
		pinfo[i] = byte('a' + i%26)
	}

	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_UI, 0, 0xF0, pinfo)
	require.NotNil(t, pp)

	return pp
}

// An AX.25 channel sends the frame as HDLC, with nothing wrapped around it.
func TestLayer2SendFrameSendsAX25AsHDLC(t *testing.T) {
	var audioConfig = newHDLCSendTestConfig(LAYER2_AX25)
	var pp = newHDLCSendTestPacket(t, 16)

	var sent int

	var bits = captureBits(t, func() {
		sent = layer2_send_frame(hdlcSendTestChannel, pp, false, audioConfig)
	})

	assert.Equal(t, len(bits), sent)

	var fbuf = AX25Pack(pp)
	var frameFCS = fcs.Calc(fbuf)
	var expected = append(append([]byte{}, fbuf...), byte(frameFCS)&0xff, byte(frameFCS>>8)&0xff)

	assert.Equal(t, expected, hdlcFrameFromBits(t, bits))
}

// An IL2P channel sends the frame wrapped up as IL2P instead.
func TestLayer2SendFrameSendsIL2PWhenConfigured(t *testing.T) {
	var audioConfig = newHDLCSendTestConfig(LAYER2_IL2P)
	audioConfig.achan[hdlcSendTestChannel].il2p_version = IL2P_VERSION_COMPAT

	il2p_init(0)

	var pp = newHDLCSendTestPacket(t, 16)

	var sent int

	var bits = captureBits(t, func() {
		sent = layer2_send_frame(hdlcSendTestChannel, pp, false, audioConfig)
	})

	assert.Equal(t, len(bits), sent)

	var sentBytes = packMSBFirst(t, bits)

	require.Greater(t, len(sentBytes), 1+IL2P_SYNC_WORD_SIZE, "there should be a frame after the sync word")
	assert.Equal(t, []byte{
		IL2P_PREAMBLE,
		(IL2P_SYNC_WORD >> 16) & 0xff,
		(IL2P_SYNC_WORD >> 8) & 0xff,
		IL2P_SYNC_WORD & 0xff,
	}, sentBytes[:1+IL2P_SYNC_WORD_SIZE], "an IL2P frame opens with the preamble and sync word")
}

// IL2P cannot carry a frame beyond a certain size.  One that does not fit
// still has to go out, as plain AX.25.
func TestLayer2SendFrameFallsBackToAX25WhenIL2PCannotCarryTheFrame(t *testing.T) {
	var audioConfig = newHDLCSendTestConfig(LAYER2_IL2P)
	audioConfig.achan[hdlcSendTestChannel].il2p_version = IL2P_VERSION_COMPAT

	il2p_init(0)

	// One byte more of information part than IL2P can encode.
	var pp = newHDLCSendTestPacket(t, 1024)

	var viaIL2P = captureBits(t, func() {
		layer2_send_frame(hdlcSendTestChannel, pp, false, audioConfig)
	})

	var asAX25 = captureBits(t, func() {
		ax25_only_hdlc_send_frame(hdlcSendTestChannel, AX25Pack(pp), false)
	})

	assert.Equal(t, asAX25, viaIL2P, "an oversized frame should have gone out as plain AX.25")
}

// An FX.25 channel wraps the frame in a codeblock with its correlation tag.
func TestLayer2SendFrameSendsFX25WhenConfigured(t *testing.T) {
	var audioConfig = newHDLCSendTestConfig(LAYER2_FX25)
	audioConfig.achan[hdlcSendTestChannel].fx25_strength = 1

	FX25Init(0)

	var pp = newHDLCSendTestPacket(t, 16)

	var sent int

	var bits = captureBits(t, func() {
		sent = layer2_send_frame(hdlcSendTestChannel, pp, false, audioConfig)
	})

	assert.Equal(t, len(bits), sent)

	var asAX25 = captureBits(t, func() {
		ax25_only_hdlc_send_frame(hdlcSendTestChannel, AX25Pack(pp), false)
	})

	assert.NotEqual(t, asAX25, bits, "the frame should have been wrapped up as FX.25")
	assert.Greater(t, len(bits), len(asAX25), "an FX.25 codeblock carries check bytes as well as the frame")
}

// FX.25 can only wrap a frame up to a certain size.  A frame that does not
// fit still has to go out, as plain AX.25.
func TestLayer2SendFrameFallsBackToAX25WhenFX25CannotCarryTheFrame(t *testing.T) {
	var audioConfig = newHDLCSendTestConfig(LAYER2_FX25)
	audioConfig.achan[hdlcSendTestChannel].fx25_strength = 1

	FX25Init(0)

	// Comfortably more than the largest FX.25 codeblock carries.
	var pp = newHDLCSendTestPacket(t, FX25_MAX_DATA)

	var viaFX25 = captureBits(t, func() {
		layer2_send_frame(hdlcSendTestChannel, pp, false, audioConfig)
	})

	var asAX25 = captureBits(t, func() {
		ax25_only_hdlc_send_frame(hdlcSendTestChannel, AX25Pack(pp), false)
	})

	assert.Equal(t, asAX25, viaFX25, "an oversized frame should have gone out as plain AX.25")
}

// The EAS SAME serializer is not HDLC at all: bytes go out as they are, with
// quiet periods around and between the repeats.
func TestEASSendRepeatsTheMessageWithItsPreamble(t *testing.T) {
	setupEASSendTest(t)

	const (
		repeat  = 2
		txdelay = 100
		txtail  = 50
		gap     = 1000
	)

	var message = []byte("ZCZC-Q1TEST")

	var elapsed int

	var bits = captureBits(t, func() {
		elapsed = eas_send(hdlcSendTestChannel, message, repeat, txdelay, txtail)
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

// setupEASSendTest gives the channel somewhere to put the quiet periods that
// surround an EAS message, which do not go through the bit capture.
func setupEASSendTest(t *testing.T) {
	t.Helper()

	var audioConfig = newHDLCSendTestConfig(LAYER2_AX25)
	audioConfig.achan[hdlcSendTestChannel].modem_type = MODEM_EAS

	var origAudioConfig, origGenPackets, origWAV = save_audio_config_p, GEN_PACKETS, genPacketsWAV

	t.Cleanup(func() {
		save_audio_config_p, GEN_PACKETS, genPacketsWAV = origAudioConfig, origGenPackets, origWAV
	})

	// Samples go to a file rather than to an audio device.
	var w, err = wavwrite.Create(filepath.Join(t.TempDir(), "eas.wav"), wavwrite.Format{
		NumChannels:   audioConfig.adev[0].num_channels,
		SamplesPerSec: audioConfig.adev[0].samples_per_sec,
		BitsPerSample: audioConfig.adev[0].bits_per_sample,
	})
	require.NoError(t, err)

	t.Cleanup(func() { w.Close() })

	GEN_PACKETS = true
	genPacketsWAV = w

	gen_tone_init(audioConfig, 100, true)
}
