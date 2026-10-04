// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"path/filepath"
	"testing"

	"github.com/doismellburning/samoyed/internal/wav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
