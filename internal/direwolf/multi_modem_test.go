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
	"github.com/doismellburning/samoyed/internal/testutils"
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

// discardReceiveSink is a ReceiveSink that ignores whatever it is told.
type discardReceiveSink struct{}

func (discardReceiveSink) RecFrame(int, int, int, *ax25.Packet, ax25.ALevel, fec_type_t, BitFixLevel, string) {
}

func (discardReceiveSink) DCDChange(int, int) {}

// What multi_modem_init set up before - for atest, the file it decoded last -
// is left behind when it runs again: neither a frame still waiting to be
// picked nor the DC bias of the old audio turns up in what comes after.
func TestMultiModemInitStartsAfresh(t *testing.T) {
	// Two demodulators, so a frame waits to be compared rather than going
	// straight through.
	var audioConfig = newRecvTestRadioConfig(1)
	audioConfig.achan[0].profiles = "AB"
	audioConfig.achan[0].num_freq = 1

	var first = new(recordingReceiveSink)
	var firstReceiver = multi_modem_init(audioConfig, 0, 0, first)
	require.Equal(t, 2, firstReceiver.demods[0].NumSubchan())

	firstReceiver.ProcessSample(0, 10000)
	require.NotZero(t, firstReceiver.modems[0].dcAverage)

	var pp = ax25.FromText("Q1TEST>Q2TEST:left over", true)
	require.NotNil(t, pp)
	var alevel ax25.ALevel
	firstReceiver.recPacket(0, 0, 0, pp, alevel, RETRY_NONE, fec_type_none)

	var second = new(recordingReceiveSink)
	var secondReceiver = multi_modem_init(audioConfig, 0, 0, second)

	assert.Zero(t, secondReceiver.modems[0].dcAverage, "the old audio's DC bias")

	// Silence decodes to nothing, so long enough for any waiting frame to be
	// picked should hand on nothing at all.
	for range 2 * secondReceiver.modems[0].processAge {
		secondReceiver.ProcessSample(0, 0)
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
	var audioConfig = newRecvTestRadioConfig(1)
	audioConfig.achan[0].profiles = "ABA"
	audioConfig.achan[0].num_freq = 1

	var receiver = multi_modem_init(audioConfig, 0, 0, new(recordingReceiveSink))

	require.NotNil(t, receiver.demods[0])
	assert.Equal(t, 3, receiver.demods[0].NumSubchan())
	assert.Same(t, receiver.demods[0], receiver.modems[0].demodulator)
	assert.Equal(t, 3, receiver.numSubchannel[0])
}

// atest hands multi_modem_init a configuration of its own, carrying its
// --il2p-version option, so the IL2P receivers have to take their settings
// from what they are given.
func TestMultiModemInitHandsIL2PItsChannelSettings(t *testing.T) {
	var audioConfig = newRecvTestRadioConfig(1)
	audioConfig.achan[0].il2p_version = il2p.Version04
	audioConfig.achan[0].il2p_crc = false

	var receiver = multi_modem_init(audioConfig, 0, 0, new(recordingReceiveSink))

	var rx = receiver.slicer[0][0][0].il2p
	assert.Equal(t, il2p.Version04, rx.Version())
	assert.False(t, rx.CRC())
}

// Every slicer's FX.25 receiver reports at the debug level multi_modem_init
// was handed - atest's -dx, say - rather than whatever an earlier caller
// asked for.
func TestMultiModemInitHandsFX25ItsDebugLevel(t *testing.T) {
	var audioConfig = newRecvTestRadioConfig(1)
	audioConfig.achan[0].profiles = "AB"
	audioConfig.achan[0].num_freq = 1

	multi_modem_init(audioConfig, 0, 0, new(recordingReceiveSink))
	var receiver = multi_modem_init(audioConfig, 3, 0, new(recordingReceiveSink))

	for sub := range receiver.numSubchannel[0] {
		for slice := range MAX_SLICERS {
			assert.Equal(t, 3, receiver.slicer[0][sub][slice].fx25.Debug(), "subchannel %d, slice %d", sub, slice)
		}
	}
}

// Every slicer's IL2P receiver reports at the debug level multi_modem_init
// was handed - atest's -d2, say - rather than whatever an earlier caller
// asked for.
func TestMultiModemInitHandsIL2PItsDebugLevel(t *testing.T) {
	var audioConfig = newRecvTestRadioConfig(1)
	audioConfig.achan[0].profiles = "AB"
	audioConfig.achan[0].num_freq = 1

	multi_modem_init(audioConfig, 0, 0, new(recordingReceiveSink))
	var receiver = multi_modem_init(audioConfig, 0, 2, new(recordingReceiveSink))

	for sub := range receiver.numSubchannel[0] {
		for slice := range MAX_SLICERS {
			assert.Equal(t, 2, receiver.slicer[0][sub][slice].il2p.Debug(), "subchannel %d, slice %d", sub, slice)
		}
	}
}

// BenchmarkLayer2ReceiveBit measures what each bit a demodulator hands on
// costs: undoing NRZI, then the HDLC, FX.25 and IL2P receivers that each
// look at it.  The bits are noise, as most of what a receiver hears is.
func BenchmarkLayer2ReceiveBit(b *testing.B) {
	var audioConfig = newRecvTestRadioConfig(1)
	audioConfig.achan[0].num_freq = 1

	var receiver = multi_modem_init(audioConfig, 0, 0, new(recordingReceiveSink))

	var rng = rand.New(rand.NewPCG(1, 2))

	var bits = make([]int, 4096)
	for i := range bits {
		bits[i] = rng.IntN(2)
	}

	b.ResetTimer()

	for i := range b.N {
		receiver.RecBit(0, 0, 0, bits[i%len(bits)], false, 0)
	}
}

// An EAS channel's bits go to its EAS receiver alone: SAME is not HDLC, so
// neither the line decoder nor the HDLC, FX.25 or IL2P receivers should see
// them.  Other channels have no EAS receiver at all.
func TestLayer2ReceiverSendsEASBitsOnlyToTheEASReceiver(t *testing.T) {
	var audioConfig = newRecvTestRadioConfig(2)
	audioConfig.achan[0].num_freq = 1
	audioConfig.achan[1].num_freq = 1
	audioConfig.achan[1].modem_type = MODEM_EAS
	audioConfig.achan[1].baud = 521
	audioConfig.achan[1].mark_freq = 2083
	audioConfig.achan[1].space_freq = 1563

	var receiver = multi_modem_init(audioConfig, 0, 0, new(recordingReceiveSink))

	assert.Nil(t, receiver.slicer[0][0][0].eas)

	var s = receiver.slicer[1][0][0]
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
			receiver.RecBit(1, 0, 0, int(b>>i)&1, false, 0)
		}
	}

	assert.Equal(t, []string{"NNNN"}, got, "the EAS receiver should have been given the message")

	// "NNNN" ends on a 0, which is also where an untouched line decoder
	// starts, so end on a 1 that the decoder would remember if it saw it.
	receiver.RecBit(1, 0, 0, 1, false, 0)

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

// A channel with no demodulator - the other side of a stereo device that is
// not a radio, say, which the audio statistics still ask about, or one that
// transmit calibration keys - has heard nothing, and has nothing to mute.
func TestReceiverChannelWithoutDemodulator(t *testing.T) {
	var r = NewLayer2Receiver(new(RadioConfig), [MAX_RADIO_CHANS]*Demodulator{}, 0, 0, new(discardReceiveSink))

	var zero ax25.ALevel

	assert.Equal(t, zero, r.AudioLevel(0, 0))
	assert.NotPanics(t, func() { r.MuteInput(0, true) })
}

// Muting a channel mutes its demodulator, and unmuting unmutes it.
func TestMuteInputMutesTheChannelsDemodulator(t *testing.T) {
	var audioConfig = newRecvTestRadioConfig(1)

	var r = multi_modem_init(audioConfig, 0, 0, new(discardReceiveSink))

	r.MuteInput(0, true)
	assert.True(t, r.demods[0].muted.Load())

	r.MuteInput(0, false)
	assert.False(t, r.demods[0].muted.Load())
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

// dcdRecordingSink is a ReceiveSink that records the DCD changes it is told
// of, and ignores frames.
type dcdRecordingSink struct {
	discardReceiveSink

	changes []string
}

func (s *dcdRecordingSink) DCDChange(channel int, state int) {
	s.changes = append(s.changes, fmt.Sprintf("%d=%d", channel, state))
}

// A touch-tone button being held makes its channel busy, as a packet being
// heard does, so nothing is transmitted over it, and raises the channel's
// DCD indicator.  The DTMF decoder reports itself as subchannel
// MAX_SUBCHANS, after the demodulators' subchannels.
func TestDTMFButtonMakesChannelBusy(t *testing.T) {
	var sink = new(dcdRecordingSink)
	var r = NewLayer2Receiver(new(RadioConfig), [MAX_RADIO_CHANS]*Demodulator{}, 0, 0, sink)

	r.DCDChange(0, MAX_SUBCHANS, 0, 1)

	assert.Equal(t, 1, r.DataDetectAny(0), "a button is held, so busy")
	assert.Equal(t, 0, r.DataDetectAny(1), "another channel hears no button")

	r.DCDChange(0, MAX_SUBCHANS, 0, 0)

	assert.Equal(t, 0, r.DataDetectAny(0), "the button is released, so not busy")
	assert.Equal(t, []string{"0=1", "0=0"}, sink.changes)
}

// The DCD indicator shows the data heard on a channel, whether or not its
// transmit inhibit is set.  Were it held where it was while the channel is
// inhibited, a packet ending under the inhibit would leave it lit.
func TestDCDFollowsDataUnderTransmitInhibit(t *testing.T) {
	var sink = new(dcdRecordingSink)
	var r = NewLayer2Receiver(new(RadioConfig), [MAX_RADIO_CHANS]*Demodulator{}, 0, 0, sink)
	r.numSubchannel[0] = 1

	var inhibited = 0

	r.getInput = func(int, int) int { return inhibited }

	r.DCDChange(0, 0, 0, 1)

	inhibited = 1

	r.DCDChange(0, 0, 0, 0)

	assert.Equal(t, []string{"0=1", "0=0"}, sink.changes)
	assert.Equal(t, 1, r.DataDetectAny(0), "still inhibited, so still busy")
}

// The receive threads change a channel's DCD while the transmit side asks
// whether it is busy.  Under the race detector, this fails if the two are
// not synchronised.
func TestDCDChangeWhileTransmitterAsks(t *testing.T) {
	var r = NewLayer2Receiver(new(RadioConfig), [MAX_RADIO_CHANS]*Demodulator{}, 0, 0, new(discardReceiveSink))
	r.numSubchannel[0] = 1

	var done = make(chan struct{})

	go func() {
		defer close(done)

		for i := range 1000 {
			r.DCDChange(0, 0, 0, i&1)
		}
	}()

	for range 1000 {
		r.DataDetectAny(0)
	}

	<-done

	assert.Equal(t, 1, r.DataDetectAny(0), "the last change set DCD")
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
	var audioConfig = newRecvTestRadioConfig(1)
	audioConfig.achan[0].num_freq = 1

	var layer2 = multi_modem_init(audioConfig, 0, 0, new(recordingReceiveSink))

	var receiver = new(countingBitReceiver)
	layer2.demods[0].setReceiver(receiver)

	var rng = rand.New(rand.NewPCG(1, 2))

	for range audioConfig.adev[0].samples_per_sec / 10 {
		layer2.ProcessSample(0, rng.IntN(20000)-10000)
	}

	assert.Positive(t, receiver.bits)
}

// A channel whose demodulators can't be fed - here, it has none - says so,
// for the receive thread to give up on its device and DirewolfMain to end the
// run through the teardown, rather than ending the process itself.
func TestProcessSampleWithoutDemodulatorSaysSo(t *testing.T) {
	var m = new(MultiModem)

	var ok bool

	var output = testutils.CaptureOutput(t, func() { ok = m.ProcessSample(0) })

	assert.False(t, ok)
	assert.Contains(t, output, "Something is seriously wrong")
}
