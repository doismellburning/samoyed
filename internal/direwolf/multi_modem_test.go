// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingReceiveSink keeps each frame handed to it.
type recordingReceiveSink struct {
	frames []*ax25.Packet
}

func (s *recordingReceiveSink) RecFrame(_ int, _ int, _ int, pp *ax25.Packet, _ ax25.ALevel, _ fec_type_t, _ BitFixLevel, _ string) {
	s.frames = append(s.frames, pp)
}

func (s *recordingReceiveSink) DCDChange(int, int) {}

// A frame still waiting to be picked when multi_modem_init runs again - atest
// decoding its next file - belongs to what came before, and must not turn up
// in what comes after.
func TestMultiModemInitDropsWaitingCandidates(t *testing.T) {
	t.Cleanup(func() {
		multiModems = newMultiModems()
	})

	// Two demodulators, so a frame waits to be compared rather than going
	// straight through.
	var audioConfig = newRecvTestRadioConfig(1)
	audioConfig.achan[0].profiles = "AB"
	audioConfig.achan[0].num_freq = 1

	var first = new(recordingReceiveSink)
	multi_modem_init(audioConfig, 0, first)
	require.Equal(t, 2, demodulators[0].NumSubchan())

	var pp = ax25.FromText("Q1TEST>Q2TEST:left over", true)
	require.NotNil(t, pp)
	var alevel ax25.ALevel
	multi_modem_process_rec_packet_real(0, 0, 0, pp, alevel, RETRY_NONE, fec_type_none)

	var second = new(recordingReceiveSink)
	multi_modem_init(audioConfig, 0, second)

	// Silence decodes to nothing, so long enough for any waiting frame to be
	// picked should hand on nothing at all.
	for range 2 * multiModems[0].processAge {
		multi_modem_process_sample(0, 0)
	}

	assert.Empty(t, first.frames)
	assert.Empty(t, second.frames)
}

// The HDLC receiver needs decoders for as many subchannels as the
// demodulators run.  Were it sized from anything but the Demodulator - a copy
// of the configuration, say - it could have decoders for one subchannel while
// the demodulators ran three, which fails silently, and only on a
// multi-decoder configuration.
func TestMultiModemInitSharesSubchannelCount(t *testing.T) {
	t.Cleanup(func() {
		multiModems = newMultiModems()
	})

	var audioConfig = newRecvTestRadioConfig(1)
	audioConfig.achan[0].profiles = "ABA"
	audioConfig.achan[0].num_freq = 1

	multi_modem_init(audioConfig, 0, new(recordingReceiveSink))

	require.NotNil(t, demodulators[0])
	assert.Equal(t, 3, demodulators[0].NumSubchan())
	assert.Same(t, demodulators[0], multiModems[0].demodulator)
	assert.Equal(t, 3, hdlcReceiver.numSubchannel[0])
}

// atest hands multi_modem_init a configuration of its own, carrying its
// --il2p-version option, so the IL2P receivers have to take their settings
// from what they are given.
func TestMultiModemInitHandsIL2PItsChannelSettings(t *testing.T) {
	t.Cleanup(func() {
		multiModems = newMultiModems()
	})

	var audioConfig = newRecvTestRadioConfig(1)
	audioConfig.achan[0].il2p_version = IL2P_VERSION_0_4
	audioConfig.achan[0].il2p_crc = false

	multi_modem_init(audioConfig, 0, new(recordingReceiveSink))

	var rx = hdlcReceiver.slicer[0][0][0].il2p
	assert.Equal(t, IL2P_VERSION_0_4, rx.version)
	assert.False(t, rx.crc)
}

// Every slicer's FX.25 receiver reports at the debug level multi_modem_init
// was handed - atest's -dx, say - rather than whatever an earlier caller
// asked for.
func TestMultiModemInitHandsFX25ItsDebugLevel(t *testing.T) {
	t.Cleanup(func() {
		multiModems = newMultiModems()
	})

	var audioConfig = newRecvTestRadioConfig(1)
	audioConfig.achan[0].profiles = "AB"
	audioConfig.achan[0].num_freq = 1

	multi_modem_init(audioConfig, 0, new(recordingReceiveSink))
	multi_modem_init(audioConfig, 3, new(recordingReceiveSink))

	for sub := range hdlcReceiver.numSubchannel[0] {
		for slice := range MAX_SLICERS {
			assert.Equal(t, 3, hdlcReceiver.slicer[0][sub][slice].fx25.debug, "subchannel %d, slice %d", sub, slice)
		}
	}
}
