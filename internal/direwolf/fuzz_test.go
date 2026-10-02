// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"bytes"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/doismellburning/samoyed/internal/mheard"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// The packet decoders are the part of Samoyed that anyone on frequency can
// reach, and they take nothing more than a []byte or a string, so they are
// cheap to fuzz.  A target's job is to make the call; the failure it is
// looking for is a panic, so there is usually nothing to assert.
//
// Seeds added here run as ordinary unit tests under "go test", so an input
// that once crashed a decoder stays checked even when nobody is fuzzing.
// Fuzzing proper is "go test ./internal/direwolf/ -run XXX -fuzz FuzzSomething".

// The decoders narrate a malformed packet at length, and a fuzzing run has
// nobody to read it, so point stdout, and logrus, at the bin for the duration.
func fuzzQuietly(tb testing.TB) {
	tb.Helper()

	var devNull, err = os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(tb, err)

	var saved = os.Stdout
	os.Stdout = devNull

	var savedLog = logrus.StandardLogger().Out
	logrus.SetOutput(io.Discard)

	tb.Cleanup(func() {
		os.Stdout = saved
		logrus.SetOutput(savedLog)
		devNull.Close()
	})
}

// FuzzAX25FromFrame covers the path every received frame takes, from the
// modem, a KISS client, a network TNC, an AGW client or IL2P: build a packet
// from the bytes off the air, then ask it the questions the receive path asks.
func FuzzAX25FromFrame(f *testing.F) {
	fuzzQuietly(f)

	// An ordinary APRS position report.
	var pp = ax25.FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)
	f.Add(pp.FrameData())

	// Addresses and a control byte, with no PID and no information part:
	// the shortest frame AX25FromFrame accepts (issue #670).
	f.Add([]byte("000000000000010"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var pp = ax25.FromFrame(data, ax25.ALevel{Rec: 50, Mark: 50, Space: 50})
		if pp == nil {
			return
		}

		pp.FormatAddrs()
		pp.Info()
		pp.FormatViaPath()
		pp.FrameType()
		pp.IsAPRS()
		pp.DedupeCRC()
		pp.DTI()
		pp.CheckAddresses(ax25.AddrLenient)
	})
}

// FuzzAX25FromText covers the other way in: a monitor-format string, as the
// APRS-IS connection and the command line tools hand us.
func FuzzAX25FromText(f *testing.F) {
	fuzzQuietly(f)

	f.Add("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#")
	f.Add("Q1TEST>APDW17::Q2TEST   :Hello")
	f.Add(">:")

	f.Fuzz(func(t *testing.T, monitor string) {
		var pp = ax25.FromTextWithStrictness(monitor, ax25.AddrLenient)
		if pp == nil {
			return
		}

		pp.FormatAddrs()
		pp.Info()
		pp.FrameType()
	})
}

// FuzzIL2PDecodeFrame covers the IL2P receive path: header FEC, descrambling
// and the payload blocks.
func FuzzIL2PDecodeFrame(f *testing.F) {
	fuzzQuietly(f)

	il2p_init(0)

	var pp = ax25.FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)

	for _, version := range []il2p_version_t{IL2P_VERSION_0_4, IL2P_VERSION_0_6} {
		var encoded, length = il2p_encode_frame(pp, version, 0)
		require.Positive(f, length)
		f.Add(encoded, int(version))
	}

	f.Add(make([]byte, 30), int(IL2P_VERSION_0_4))

	f.Fuzz(func(t *testing.T, irec []byte, version int) {
		il2p_decode_frame(irec, il2p_version_t(version))
	})
}

// kissFuzzMaxStream bounds the streams the KISS targets try.  Room for an
// overlong frame and a few hundred short ones is all the collector needs, and
// the queues they land on are only drained by threads a fuzzing run doesn't
// have, so a much longer stream spends its time walking them, not finding
// anything.
const kissFuzzMaxStream = 4 * kiss.MaxFrameLen

// kissFuzzSeeds are streams a KISS peer might send, well formed or not, for
// the targets that take one.
func kissFuzzSeeds(f *testing.F) {
	f.Helper()

	var pp = ax25.FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)

	var frame = kiss.Encapsulate(append([]byte{kiss.CmdDataFrame}, pp.FrameData()...))

	for _, seed := range [][]byte{
		frame,
		append([]byte("XFLOW OFF\rKISS ON\rRESTART\r"), frame...),
		{kiss.FEND, kiss.FESC, kiss.FEND}, // Nothing in it once unescaped - used to crash.
		{kiss.FEND, kiss.CmdTxDelay, 30, kiss.FEND, kiss.FEND, kiss.CmdPersistence, kiss.FEND},
		kiss.Encapsulate([]byte("\x06TNC:")),
		kiss.Encapsulate([]byte("\x06TXBUF:")),
		{kiss.FEND, 0xff, kiss.FEND},
		append(append([]byte{kiss.FEND}, bytes.Repeat([]byte{'x'}, kiss.MaxFrameLen)...), kiss.FEND),
	} {
		for debug := range byte(3) {
			f.Add(seed, debug)
		}
	}
}

// FuzzKissRecByte covers what a KISS client application sends the TNC, over
// the TCP port, the serial port or the pseudo terminal: anyone who can reach
// one of those reaches this, frame collection and command handling both.
func FuzzKissRecByte(f *testing.F) {
	fuzzQuietly(f)
	kissFuzzSeeds(f)

	f.Fuzz(func(t *testing.T, stream []byte, debug byte) {
		if len(stream) > kissFuzzMaxStream {
			t.Skip()
		}

		setupKissProcessMsg(t)

		var audioConfig = kissTestRadioConfig()
		var _, sendfun = recordingSendfun()
		var kc kiss.Collector

		for _, b := range stream {
			KissRecByte(&kc, audioConfig, b, int(debug%3), nil, -1, sendfun)
		}
	})
}

// FuzzNetTNCRecByte covers what a network TNC sends us, for the channel it is
// attached to - it is at the far end of a TCP connection, and may not be ours.
func FuzzNetTNCRecByte(f *testing.F) {
	fuzzQuietly(f)
	kissFuzzSeeds(f)

	f.Fuzz(func(t *testing.T, stream []byte, debug byte) {
		if len(stream) > kissFuzzMaxStream {
			t.Skip()
		}

		expectReceivedFrames(t)

		var kc kiss.Collector

		for _, b := range stream {
			nettncRecByte(&kc, b, int(debug%3), nettncTestChannel)
		}
	})
}

// igateFuzzMaxStream bounds what FuzzIGateServerLines feeds the IGate.  A few
// lines' worth, including one over the limit, is all the reader has to get
// right.
const igateFuzzMaxStream = 4 * igateMaxLineLen

// FuzzIGateServerLines covers what the APRS-IS server sends the IGate: lines
// gathered from its byte stream, then each one shown, remembered, or turned
// into a frame for the radio and for a client application.  The server is
// across the internet, and anything sent to it by anyone comes back out.
func FuzzIGateServerLines(f *testing.F) {
	fuzzQuietly(f)

	for _, seed := range []string{
		"# aprsc 2.1.19-g730c5c0\r\n# logresp Q1TEST verified, server T2TEST\r\n",
		"Q2TEST-1>APWW10,TCPIP*,qAC,T2TEST:>hello\r\n",
		"Q2TEST>APDW17,WIDE1-1,qAR,Q3TEST:!4237.14N/07120.83W#\r\n",
		"WHO-IS>APJIW4,TCPIP*,qAC,AE5PL-JF::Q2TEST   :Hello there{583\r\n",
		"Q2TEST>APWW10,TCPIP*,qAC,T2TEST:}Q3TEST>APDW17,TCPIP,Q2TEST*:>third party\r\n",
		"Q2TEST>APWW10,TCPIP*,qAC,T2TEST:>nul\x00inside\r\n",
		"Q2TEST>APWW10,NOGATE,qAC,T2TEST:>not for RF\r\n",
		"\r\n\n\r\n",
		strings.Repeat("A", igateMaxLineLen) + "\r\nQ2TEST>APWW10,TCPIP*,qAC,T2TEST:>after\r\n",
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, stream []byte) {
		if len(stream) > igateFuzzMaxStream {
			t.Skip()
		}

		setupIGateFromServer(t)

		var lines = new(igateLineReader)

		for _, b := range stream {
			if line, complete := lines.add(b); complete {
				igate.processServerLine(line)
			}
		}
	})
}

// setupIGateFromServer gives FuzzIGateServerLines an IGate that will pass
// what it hears from the server both to the radio and to ICHANNEL, with
// nothing connected - processServerLine doesn't need the socket.
func setupIGateFromServer(t *testing.T) {
	t.Helper()

	var origIGate, origMheard = igate, mheardDB

	var audioConfig = new(RadioConfig)
	audioConfig.chan_medium[0] = MEDIUM_RADIO
	audioConfig.mycall[0] = "Q1TEST"
	audioConfig.igate_vchannel = 1

	var igateConfig = new(igate_config_s)
	igateConfig.tx_chan = 0
	igateConfig.tx_limit_1 = IGATE_TX_LIMIT_1_DEFAULT
	igateConfig.tx_limit_5 = IGATE_TX_LIMIT_5_DEFAULT
	igateConfig.igmsp = 1

	igate = NewIGate(audioConfig, igateConfig, new(digi_config_s), NewPacketFilter(igateConfig, nil, 0), 0)
	mheardDB = mheard.New(0)

	transmitQueue.Init(audioConfig)
	dataLinkQueue.Init()

	t.Cleanup(func() {
		igate, mheardDB = origIGate, origMheard

		for p := range TQ_NUM_PRIO {
			for transmitQueue.Remove(0, p) != nil { //revive:disable-line:empty-block
			}
		}

		dataLinkQueue.Init()
	})
}

// fx25FuzzMaxStream bounds the bytes, each eight received bits, the FX.25
// target feeds the receiver.  The largest codeblock, tag and all, is under
// 300 bytes, so this is room for a handful of them back to back.
const fx25FuzzMaxStream = 2048

// FuzzFX25RecBit covers the FX.25 receive path: hunting the bit stream for a
// correlation tag, gathering the codeblock it announces, then the
// Reed-Solomon decoder and the HDLC unstuffing of what it hands back.  Anyone
// transmitting on the channel controls those bits.
func FuzzFX25RecBit(f *testing.F) {
	fuzzQuietly(f)

	var pp = ax25.FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)

	// A real codeblock for each correlation tag, so the fuzzer starts from
	// something the receiver accepts.  Only the short test frame fits the
	// smallest of them.
	for ctag := CTAG_MIN; ctag <= CTAG_MAX; ctag++ {
		var ctagNum, data, check = fx25_encode_frame(0, slices.Clone(fxTestFrame), 100+ctag, 0)
		require.Equal(f, ctag, ctagNum)
		f.Add(fxTestBlock(ctagNum, data, check), byte(0))
	}

	// The APRS frame, as an ordinary FX.25 configuration would send it.
	for _, checkBytes := range []int{16, 32, 64} {
		var ctagNum, data, check = fx25_encode_frame(0, pp.FrameData(), checkBytes, 0)
		require.Positive(f, ctagNum)

		var block = fxTestBlock(ctagNum, data, check)
		var frames, _ = fxTestReceive(block)
		require.Len(f, frames, 1, "the seed should be one the receiver accepts")
		f.Add(block, byte(2))

		// More damage than the check bytes can repair.
		var damaged = slices.Clone(block)
		for j := 24; j < 24+checkBytes; j++ {
			damaged[j] ^= 0xff
		}

		f.Add(damaged, byte(3))
	}

	f.Fuzz(func(t *testing.T, stream []byte, debug byte) {
		if len(stream) > fx25FuzzMaxStream {
			t.Skip()
		}

		// What the receive path does next with a frame, short of queueing it.
		var sink = func(channel int, subchannel int, slice int, frame []byte, derrors int) {
			ax25.FromFrame(frame, ax25.ALevel{Rec: 50, Mark: 50, Space: 50})
		}

		var rx = newFX25Receiver(0, 0, 0, int(debug%4), sink)

		for _, b := range stream {
			for imask := byte(0x01); imask != 0; imask <<= 1 {
				rx.recBit(int(b & imask))
			}
		}
	})
}
