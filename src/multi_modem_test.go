// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingReceiveSink keeps each frame handed to it.
type recordingReceiveSink struct {
	frames []*packet_t
}

func (s *recordingReceiveSink) RecFrame(_ int, _ int, _ int, pp *packet_t, _ ALevel, _ fec_type_t, _ BitFixLevel, _ string) {
	s.frames = append(s.frames, pp)
}

func (s *recordingReceiveSink) DCDChange(int, int) {}

// A frame still waiting to be picked when multi_modem_init runs again - atest
// decoding its next file - belongs to what came before, and must not turn up
// in what comes after.
func TestMultiModemInitDropsWaitingCandidates(t *testing.T) {
	var origAudioConfig = save_audio_config_p

	t.Cleanup(func() {
		save_audio_config_p = origAudioConfig
		multiModems = newMultiModems()
	})

	// Two demodulators, so a frame waits to be compared rather than going
	// straight through.
	var audioConfig = newRecvTestAudioConfig(1)
	audioConfig.achan[0].profiles = "AB"
	audioConfig.achan[0].num_freq = 1

	var first = new(recordingReceiveSink)
	multi_modem_init(audioConfig, first)
	require.Equal(t, 2, audioConfig.achan[0].num_subchan)

	var pp = AX25FromText("Q1TEST>Q2TEST:left over", true)
	require.NotNil(t, pp)
	var alevel ALevel
	multi_modem_process_rec_packet_real(0, 0, 0, pp, alevel, RETRY_NONE, fec_type_none)

	var second = new(recordingReceiveSink)
	multi_modem_init(audioConfig, second)

	// Silence decodes to nothing, so long enough for any waiting frame to be
	// picked should hand on nothing at all.
	for range 2 * multiModems[0].processAge {
		multi_modem_process_sample(0, 0)
	}

	assert.Empty(t, first.frames)
	assert.Empty(t, second.frames)
}
