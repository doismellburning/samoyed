// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/eas"
	"github.com/doismellburning/samoyed/internal/il2p"
	"github.com/doismellburning/samoyed/internal/phy"
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
	multi_modem_init(audioConfig, 0, 0, first)
	require.Equal(t, 2, demodulators[0].NumSubchan())

	var pp = ax25.FromText("Q1TEST>Q2TEST:left over", true)
	require.NotNil(t, pp)
	var alevel ax25.ALevel
	multi_modem_process_rec_packet_real(0, 0, 0, pp, alevel, RETRY_NONE, fec_type_none)

	var second = new(recordingReceiveSink)
	multi_modem_init(audioConfig, 0, 0, second)

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

	multi_modem_init(audioConfig, 0, 0, new(recordingReceiveSink))

	require.NotNil(t, demodulators[0])
	assert.Equal(t, 3, demodulators[0].NumSubchan())
	assert.Same(t, demodulators[0], multiModems[0].demodulator)
	assert.Equal(t, 3, layer2Receiver.numSubchannel[0])
}

// atest hands multi_modem_init a configuration of its own, carrying its
// --il2p-version option, so the IL2P receivers have to take their settings
// from what they are given.
func TestMultiModemInitHandsIL2PItsChannelSettings(t *testing.T) {
	t.Cleanup(func() {
		multiModems = newMultiModems()
	})

	var audioConfig = newRecvTestRadioConfig(1)
	audioConfig.achan[0].il2p_version = il2p.Version04
	audioConfig.achan[0].il2p_crc = false

	multi_modem_init(audioConfig, 0, 0, new(recordingReceiveSink))

	var rx = layer2Receiver.slicer[0][0][0].il2p
	assert.Equal(t, il2p.Version04, rx.Version())
	assert.False(t, rx.CRC())
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

	multi_modem_init(audioConfig, 0, 0, new(recordingReceiveSink))
	multi_modem_init(audioConfig, 3, 0, new(recordingReceiveSink))

	for sub := range layer2Receiver.numSubchannel[0] {
		for slice := range MAX_SLICERS {
			assert.Equal(t, 3, layer2Receiver.slicer[0][sub][slice].fx25.Debug(), "subchannel %d, slice %d", sub, slice)
		}
	}
}

// Every slicer's IL2P receiver reports at the debug level multi_modem_init
// was handed - atest's -d2, say - rather than whatever an earlier caller
// asked for.
func TestMultiModemInitHandsIL2PItsDebugLevel(t *testing.T) {
	t.Cleanup(func() {
		multiModems = newMultiModems()
	})

	var audioConfig = newRecvTestRadioConfig(1)
	audioConfig.achan[0].profiles = "AB"
	audioConfig.achan[0].num_freq = 1

	multi_modem_init(audioConfig, 0, 0, new(recordingReceiveSink))
	multi_modem_init(audioConfig, 0, 2, new(recordingReceiveSink))

	for sub := range layer2Receiver.numSubchannel[0] {
		for slice := range MAX_SLICERS {
			assert.Equal(t, 2, layer2Receiver.slicer[0][sub][slice].il2p.Debug(), "subchannel %d, slice %d", sub, slice)
		}
	}
}

// BenchmarkLayer2ReceiveBit measures what each bit a demodulator hands on
// costs: undoing NRZI, then the HDLC, FX.25 and IL2P receivers that each
// look at it.  The bits are noise, as most of what a receiver hears is.
func BenchmarkLayer2ReceiveBit(b *testing.B) {
	var origReceiver = layer2Receiver

	b.Cleanup(func() {
		layer2Receiver = origReceiver
		multiModems = newMultiModems()
	})

	var audioConfig = newRecvTestRadioConfig(1)
	audioConfig.achan[0].num_freq = 1

	multi_modem_init(audioConfig, 0, 0, new(recordingReceiveSink))

	var rng = rand.New(rand.NewPCG(1, 2))

	var bits = make([]int, 4096)
	for i := range bits {
		bits[i] = rng.IntN(2)
	}

	b.ResetTimer()

	for i := range b.N {
		layer2Receiver.RecBit(0, 0, 0, bits[i%len(bits)], false, 0)
	}
}

// An EAS channel's bits go to its EAS receiver alone: SAME is not HDLC, so
// neither the line decoder nor the HDLC, FX.25 or IL2P receivers should see
// them.  Other channels have no EAS receiver at all.
func TestLayer2ReceiverSendsEASBitsOnlyToTheEASReceiver(t *testing.T) {
	var origReceiver = layer2Receiver

	t.Cleanup(func() {
		layer2Receiver = origReceiver
		multiModems = newMultiModems()
	})

	var audioConfig = newRecvTestRadioConfig(2)
	audioConfig.achan[0].num_freq = 1
	audioConfig.achan[1].num_freq = 1
	audioConfig.achan[1].modem_type = MODEM_EAS
	audioConfig.achan[1].baud = 521
	audioConfig.achan[1].mark_freq = 2083
	audioConfig.achan[1].space_freq = 1563

	multi_modem_init(audioConfig, 0, 0, new(recordingReceiveSink))

	assert.Nil(t, layer2Receiver.slicer[0][0][0].eas)

	var s = layer2Receiver.slicer[1][0][0]
	require.NotNil(t, s.eas)

	// An EAS receiver of the same kind, but one whose messages this test can
	// see.
	var got []string

	s.eas = eas.NewReceiver(1, 0, 0,
		func(int, int) ax25.ALevel { return ax25.ALevel{Rec: 0, Mark: 0, Space: 0} },
		func(_ int, _ int, _ int, frame []byte, _ ax25.ALevel, _ phy.BitFixLevel, _ phy.FECType) {
			got = append(got, string(frame))
		})

	// Without its other receivers, the slicer would panic if it handed any
	// of them a bit.
	s.hdlc = nil
	s.fx25 = nil
	s.il2p = nil

	// The SAME preamble, then "NNNN", the end of message, least significant
	// bit first.
	for _, b := range []byte{0xab, 0xab, 0xab, 0xab, 'N', 'N', 'N', 'N'} {
		for i := range 8 {
			layer2Receiver.RecBit(1, 0, 0, int(b>>i)&1, false, 0)
		}
	}

	assert.Equal(t, []string{"NNNN"}, got, "the EAS receiver should have been given the message")

	// "NNNN" ends on a 0, which is also where an untouched line decoder
	// starts, so end on a 1 that the decoder would remember if it saw it.
	layer2Receiver.RecBit(1, 0, 0, 1, false, 0)

	assert.False(t, s.line.PrevRaw(), "the line decoder should not have been given any bits")
}

// A radio channel's DCD output follows the data detected on it.
func TestRadioSinkDCDChangeSetsDCD(t *testing.T) {
	var set []string

	var sink = new(radioSink)
	sink.setOutput = func(ot int, channel int, state int) {
		set = append(set, fmt.Sprintf("%s %d=%d", octypeName(ot), channel, state))
	}

	sink.DCDChange(1, 1)
	sink.DCDChange(1, 0)

	assert.Equal(t, []string{"DCD 1=1", "DCD 1=0"}, set)
}

// A channel whose transmit inhibit input is set counts as busy, data or no
// data, so nothing is transmitted on it.
func TestDataDetectAnyHonoursTransmitInhibit(t *testing.T) {
	var r = NewLayer2Receiver(new(RadioConfig), [MAX_RADIO_CHANS]*Demodulator{}, 0, 0, new(discardReceiveSink))

	assert.Equal(t, 0, r.DataDetectAny(0), "nothing heard and no inputs, so not busy")

	r.getInput = func(it int, channel int) int {
		if it == ICTYPE_TXINH && channel == 0 {
			return 1
		}

		return 0
	}

	assert.Equal(t, 1, r.DataDetectAny(0), "transmit inhibited, so busy")
	assert.Equal(t, 0, r.DataDetectAny(1), "another channel is not inhibited")
}

// countingBitReceiver counts the bits handed to it, and ignores the rest.
type countingBitReceiver struct {
	bits int
}

func (r *countingBitReceiver) RecBit(int, int, int, int, bool, int) { r.bits++ }

func (r *countingBitReceiver) RecBitNew(int, int, int, int, bool, int, *int64, *int) { r.bits++ }

func (r *countingBitReceiver) DCDChange(int, int, int, int) {}

// What a demodulator demodulates goes to the receiver it was given - noise
// decodes to bits like anything else.
func TestDemodulatorHandsBitsToItsReceiver(t *testing.T) {
	var origDemodulators = demodulators

	t.Cleanup(func() {
		demodulators = origDemodulators
		multiModems = newMultiModems()
	})

	var audioConfig = newRecvTestRadioConfig(1)
	audioConfig.achan[0].num_freq = 1

	multi_modem_init(audioConfig, 0, 0, new(recordingReceiveSink))

	var receiver = new(countingBitReceiver)
	demodulators[0].setReceiver(receiver)

	var rng = rand.New(rand.NewPCG(1, 2))

	for range audioConfig.adev[0].samples_per_sec / 10 {
		multi_modem_process_sample(0, rng.IntN(20000)-10000)
	}

	assert.Positive(t, receiver.bits)
}
