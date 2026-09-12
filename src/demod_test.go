// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// PSK has no decimating path in demod_process_sample, so demod_init must rule
// decimation out, and say so rather than doing it silently.
func TestDemodInitRejectsPSKDecimation(t *testing.T) {
	for _, modemType := range []modem_t{MODEM_QPSK, MODEM_8PSK, MODEM_BPSK} {
		var channel = 0
		var audioConfig = newTestAudioConfig(channel, modemType, 2400, 0, 0, 44100)
		audioConfig.achan[channel].decimate = 3
		audioConfig.achan[channel].num_freq = 1

		AssertOutputContains(t, func() {
			demod_init(audioConfig)
		}, "Decimation is not supported for PSK")

		assert.Equal(t, 1, audioConfig.achan[channel].decimate)
	}
}

// AFSK, by contrast, does decimate.
func TestDemodInitKeepsAFSKDecimation(t *testing.T) {
	var channel = 1
	var audioConfig = newTestAudioConfig(channel, MODEM_AFSK, 1200, 1200, 2200, 48000)
	audioConfig.achan[channel].decimate = 3
	audioConfig.achan[channel].num_freq = 1

	demod_init(audioConfig)

	assert.Equal(t, 3, audioConfig.achan[channel].decimate)
}
