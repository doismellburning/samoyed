// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"io"
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// multiModemImplFrame is one frame handed to a multiModemImplSink, with
// everything that came with it.
type multiModemImplFrame struct {
	channel  int
	subchan  int
	slice    int
	pp       *ax25.Packet
	fecType  fec_type_t
	retries  BitFixLevel
	spectrum string
}

// multiModemImplSink keeps each frame handed to it, and where it came from.
type multiModemImplSink struct {
	frames []multiModemImplFrame
}

func (s *multiModemImplSink) RecFrame(channel int, subchan int, slice int, pp *ax25.Packet, _ ax25.ALevel, fecType fec_type_t, retries BitFixLevel, spectrum string) {
	var f = multiModemImplFrame{
		channel:  channel,
		subchan:  subchan,
		slice:    slice,
		pp:       pp,
		fecType:  fecType,
		retries:  retries,
		spectrum: spectrum,
	}
	s.frames = append(s.frames, f)
}

func (s *multiModemImplSink) DCDChange(int, int) {}

// newMultiModemImplTest builds a MultiModem for channel 0 whose demodulator
// reports the given layout, with no HDLC receiver behind it, so no FX.25
// reception is ever in progress.
func newMultiModemImplTest(t *testing.T, numSubchan int, numSlicers int) (*MultiModem, *multiModemImplSink) {
	t.Helper()

	var origHDLCReceiver = hdlcReceiver

	t.Cleanup(func() {
		hdlcReceiver = origHDLCReceiver
	})

	hdlcReceiver = nil

	var d = new(Demodulator)
	d.numSubchan = numSubchan
	d.numSlicers = numSlicers

	var sink = new(multiModemImplSink)

	var m = new(MultiModem)
	m.channel = 0
	m.audioConfig = new(RadioConfig)
	m.demodulator = d
	m.sink = sink

	return m, sink
}

func multiModemImplPacket(t *testing.T, info string) *ax25.Packet {
	t.Helper()

	var pp = ax25.FromText("Q1TEST>Q2TEST:"+info, true)
	require.NotNil(t, pp)

	return pp
}

func TestMultiModemImplSingleDecoderPassesStraightThrough(t *testing.T) {
	var m, sink = newMultiModemImplTest(t, 1, 1)

	var pp = multiModemImplPacket(t, "hello")
	var alevel ax25.ALevel
	m.processRecPacket(0, 0, pp, alevel, RETRY_INVERT_SINGLE, fec_type_none)

	require.Len(t, sink.frames, 1)
	assert.Same(t, pp, sink.frames[0].pp)
	assert.Equal(t, RETRY_INVERT_SINGLE, sink.frames[0].retries)
	assert.Empty(t, sink.frames[0].spectrum)
	assert.Nil(t, m.candidates[0][0].packet_p, "nothing is kept to be picked later")
}

func TestMultiModemImplNilPacketIgnored(t *testing.T) {
	var m, sink = newMultiModemImplTest(t, 2, 1)

	var alevel ax25.ALevel
	m.processRecPacket(0, 0, nil, alevel, RETRY_NONE, fec_type_none)

	assert.Empty(t, sink.frames)
	assert.Nil(t, m.candidates[0][0].packet_p)
}

func TestMultiModemImplSingleDecoderErrorRateDrops(t *testing.T) {
	var m, sink = newMultiModemImplTest(t, 1, 1)
	m.audioConfig.recv_error_rate = 100

	var alevel ax25.ALevel
	m.processRecPacket(0, 0, multiModemImplPacket(t, "dropped"), alevel, RETRY_NONE, fec_type_none)

	assert.Empty(t, sink.frames)
}

func TestMultiModemImplMultipleDecodersHoldCandidate(t *testing.T) {
	var m, sink = newMultiModemImplTest(t, 2, 1)

	var pp = multiModemImplPacket(t, "held")
	var alevel ax25.ALevel
	m.processRecPacket(1, 0, pp, alevel, RETRY_INVERT_SINGLE, fec_type_fx25)

	assert.Empty(t, sink.frames)

	var c = m.candidates[1][0]
	assert.Same(t, pp, c.packet_p)
	assert.Equal(t, fec_type_fx25, c.fec_type)
	assert.Equal(t, RETRY_INVERT_SINGLE, c.retries)
	assert.Equal(t, 0, c.age)
	assert.Equal(t, pp.MultiModemCRC(), c.crc)
}

// FEC beats plain AX.25, fewer corrections beat more, and the spectrum shows
// what each decoder found.
func TestMultiModemImplPickBestCandidatePrefersFEC(t *testing.T) {
	var m, sink = newMultiModemImplTest(t, 2, 2)

	var alevel ax25.ALevel

	var best = multiModemImplPacket(t, "fx25 three")
	m.processRecPacket(0, 0, best, alevel, BitFixLevel(3), fec_type_fx25)
	m.processRecPacket(1, 0, multiModemImplPacket(t, "clean"), alevel, RETRY_NONE, fec_type_none)
	m.processRecPacket(0, 1, multiModemImplPacket(t, "il2p twelve"), alevel, BitFixLevel(12), fec_type_il2p)
	m.processRecPacket(1, 1, multiModemImplPacket(t, "passall"), alevel, BitFixPassall, fec_type_none)

	m.pickBestCandidate()

	require.Len(t, sink.frames, 1)
	var f = sink.frames[0]
	assert.Same(t, best, f.pp)
	assert.Equal(t, 0, f.subchan)
	assert.Equal(t, 0, f.slice)
	assert.Equal(t, fec_type_fx25, f.fecType)
	assert.Equal(t, "3|+.", f.spectrum)

	assert.Equal(t, [MAX_SUBCHANS][MAX_SLICERS]candidate_t{}, m.candidates, "candidates cleared for next time")
}

func TestMultiModemImplPickBestCandidatePrefersNoRetries(t *testing.T) {
	var m, sink = newMultiModemImplTest(t, 3, 1)

	var alevel ax25.ALevel

	var best = multiModemImplPacket(t, "clean")
	m.processRecPacket(0, 0, multiModemImplPacket(t, "fixed"), alevel, RETRY_INVERT_SINGLE, fec_type_none)
	m.processRecPacket(1, 0, best, alevel, RETRY_NONE, fec_type_none)

	m.pickBestCandidate()

	require.Len(t, sink.frames, 1)
	assert.Same(t, best, sink.frames[0].pp)
	assert.Equal(t, 1, sink.frames[0].subchan)
	assert.Equal(t, ":|_", sink.frames[0].spectrum)
}

// With equal effort, a frame that other decoders agree on wins.
func TestMultiModemImplPickBestCandidatePrefersAgreement(t *testing.T) {
	var m, sink = newMultiModemImplTest(t, 3, 1)

	var alevel ax25.ALevel

	m.processRecPacket(0, 0, multiModemImplPacket(t, "loner"), alevel, RETRY_INVERT_SINGLE, fec_type_none)
	m.processRecPacket(1, 0, multiModemImplPacket(t, "agreed"), alevel, RETRY_INVERT_SINGLE, fec_type_none)
	m.processRecPacket(2, 0, multiModemImplPacket(t, "agreed"), alevel, RETRY_INVERT_SINGLE, fec_type_none)

	m.pickBestCandidate()

	require.Len(t, sink.frames, 1)
	assert.Equal(t, 1, sink.frames[0].subchan)
	assert.Equal(t, "agreed", string(sink.frames[0].pp.Info()))
	assert.Equal(t, ":::", sink.frames[0].spectrum)
}

func TestMultiModemImplPickBestCandidateErrorRateDrops(t *testing.T) {
	var m, sink = newMultiModemImplTest(t, 2, 1)
	m.audioConfig.recv_error_rate = 100

	var alevel ax25.ALevel
	m.processRecPacket(0, 0, multiModemImplPacket(t, "a"), alevel, RETRY_NONE, fec_type_none)
	m.processRecPacket(1, 0, multiModemImplPacket(t, "b"), alevel, RETRY_NONE, fec_type_none)

	m.pickBestCandidate()

	assert.Empty(t, sink.frames)
	assert.Equal(t, [MAX_SUBCHANS][MAX_SLICERS]candidate_t{}, m.candidates)
}

// The trace logging describes every candidate but changes nothing about the
// pick.
func TestMultiModemImplPickBestCandidateTrace(t *testing.T) {
	var origLevel = logrus.GetLevel()
	var origOut = logrus.StandardLogger().Out

	t.Cleanup(func() {
		logrus.SetLevel(origLevel)
		logrus.SetOutput(origOut)
	})

	var buf strings.Builder
	logrus.SetOutput(&buf)
	logrus.SetLevel(logrus.TraceLevel)

	var m, sink = newMultiModemImplTest(t, 2, 1)

	var alevel ax25.ALevel
	var pp = multiModemImplPacket(t, "traced")
	m.processRecPacket(1, 0, pp, alevel, RETRY_NONE, fec_type_none)

	m.pickBestCandidate()

	logrus.SetOutput(io.Discard)

	require.Len(t, sink.frames, 1)
	assert.Same(t, pp, sink.frames[0].pp)
	assert.Equal(t, "_|", sink.frames[0].spectrum)
	assert.Contains(t, buf.String(), "candidate: no packet")
	assert.Contains(t, buf.String(), "score=")
}

// A frame waits processAge samples for others to turn up, then goes on.
func TestMultiModemImplProcessSamplePicksAfterAge(t *testing.T) {
	var origHDLCReceiver = hdlcReceiver

	t.Cleanup(func() {
		hdlcReceiver = origHDLCReceiver
		multiModems = newMultiModems()
	})

	var audioConfig = newRecvTestRadioConfig(1)
	audioConfig.achan[0].profiles = "AB"
	audioConfig.achan[0].num_freq = 1

	var sink = new(multiModemImplSink)
	multi_modem_init(audioConfig, 0, sink)

	var m = multiModems[0]
	require.Equal(t, 3*44100/1200, m.processAge)

	var pp = multiModemImplPacket(t, "waiting")
	var alevel ax25.ALevel
	multi_modem_process_rec_packet_real(0, 1, 0, pp, alevel, RETRY_NONE, fec_type_none)

	for range m.processAge {
		multi_modem_process_sample(0, 0)
	}

	assert.Empty(t, sink.frames, "still waiting for others")
	assert.Equal(t, m.processAge, m.candidates[1][0].age)

	multi_modem_process_sample(0, 0)

	require.Len(t, sink.frames, 1)
	assert.Same(t, pp, sink.frames[0].pp)
	assert.Equal(t, 1, sink.frames[0].subchan)
	assert.Equal(t, "_|", sink.frames[0].spectrum)
}

// Baud rates for QPSK and 8PSK are in bits, but the wait is in symbols.
func TestMultiModemImplInitProcessAgeInSymbols(t *testing.T) {
	var origHDLCReceiver = hdlcReceiver

	t.Cleanup(func() {
		hdlcReceiver = origHDLCReceiver
		multiModems = newMultiModems()
	})

	var tests = []struct {
		name      string
		modemType modem_t
		baud      int
		symbols   int
	}{
		{"QPSK", MODEM_QPSK, 2400, 1200},
		{"8PSK", MODEM_8PSK, 4800, 1600},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var audioConfig = newRecvTestRadioConfig(1)
			audioConfig.achan[0].modem_type = tc.modemType
			audioConfig.achan[0].baud = tc.baud
			audioConfig.achan[0].mark_freq = 1800
			audioConfig.achan[0].space_freq = 0

			multi_modem_init(audioConfig, 0, new(multiModemImplSink))

			assert.Equal(t, PROCESS_AFTER_BITS*44100/tc.symbols, multiModems[0].processAge)
		})
	}
}

// multiModemImplRecFrame hands fbuf to multi_modem_process_rec_frame on a
// channel of the given modem type, and returns the packet made from it.
func multiModemImplRecFrame(t *testing.T, modemType modem_t, fbuf []byte) *ax25.Packet {
	t.Helper()

	var origCapture = multiModemRecCapture

	t.Cleanup(func() {
		multiModemRecCapture = origCapture
		multiModems = newMultiModems()
	})

	var audioConfig = new(RadioConfig)
	audioConfig.achan[0].modem_type = modemType
	multiModems[0].audioConfig = audioConfig

	var got *ax25.Packet

	multiModemRecCapture = func(channel int, subchannel int, slice int, pp *ax25.Packet, _ ax25.ALevel, retries BitFixLevel, fecType fec_type_t) {
		assert.Equal(t, 0, channel)
		assert.Equal(t, 1, subchannel)
		assert.Equal(t, 2, slice)
		assert.Equal(t, RETRY_INVERT_SINGLE, retries)
		assert.Equal(t, fec_type_none, fecType)

		got = pp
	}

	var alevel ax25.ALevel
	multi_modem_process_rec_frame(0, 1, 2, fbuf, alevel, RETRY_INVERT_SINGLE, fec_type_none)

	require.NotNil(t, got)

	return got
}

func TestMultiModemImplRecFrameAX25(t *testing.T) {
	var pp = multiModemImplRecFrame(t, MODEM_AFSK, multiModemImplPacket(t, "plain").Pack())

	assert.Equal(t, "Q1TEST", pp.AddrWithSSID(ax25.Source))
	assert.Equal(t, "plain", string(pp.Info()))
}

func TestMultiModemImplRecFrameAIS(t *testing.T) {
	var pp = multiModemImplRecFrame(t, MODEM_AIS, make([]byte, 21))

	assert.Equal(t, "AIS", pp.AddrWithSSID(ax25.Source))
	assert.Equal(t, "NOGATE", pp.AddrWithSSID(ax25.Repeater1))
	assert.True(t, strings.HasPrefix(string(pp.Info()), "{DA!AIVDM,1,1,,A,"), string(pp.Info()))
}

func TestMultiModemImplRecFrameEAS(t *testing.T) {
	var pp = multiModemImplRecFrame(t, MODEM_EAS, []byte("ZCZC-WXR-RWT"))

	assert.Equal(t, "EAS", pp.AddrWithSSID(ax25.Source))
	assert.Equal(t, "NOGATE", pp.AddrWithSSID(ax25.Repeater1))
	assert.Equal(t, "{DEZCZC-WXR-RWT", string(pp.Info()))
}
