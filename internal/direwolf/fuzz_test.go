// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/kiss"
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

// FuzzDecodeAPRS covers the information part decoders, which is where most of
// the reading-off-the-end has been.
func FuzzDecodeAPRS(f *testing.F) {
	fuzzQuietly(f)

	var aprsDecoder = NewAPRSDecoderFromDataFiles()

	f.Add("Q1TEST>APDW17:!4237.14N/07120.83W#")
	f.Add("Q1TEST>APDW17:;Q2TEST   *111111z4237.14N/07120.83W#")
	f.Add("Q1TEST>APDW17:_10090556c220s004g005t077r000p000P000h50b09900")

	// A Mic-E report whose destination is too short to hold a latitude
	// (issue #670).
	f.Add("0>0:'0000000000000000000")

	// A course and speed data extension with nothing after it, so no
	// bearing and no NRQ.
	f.Add("0>0:!0000000000000000000000/000")

	// User-defined data that stops before its user ID.
	f.Add("0>0:{")

	// A general query whose footprint is not the three comma-separated
	// fields the parser goes on to read.
	f.Add("0>0:?X?0")

	// A message with nothing after the addressee, and one too short to
	// hold an "ack" or "rej".
	f.Add("0>0::000000000:")
	f.Add("0>0::000000000:ab")

	// A status report too short for the 6 character Maidenhead locator
	// form, so the 4 character one is tried with exactly 4 bytes.
	f.Add("Q1TEST>APDW17:>IO91/#  ")

	f.Fuzz(func(t *testing.T, monitor string) {
		var pp = ax25.FromTextWithStrictness(monitor, ax25.AddrLenient)
		if pp == nil {
			return
		}

		aprsDecoder.Decode(pp, true)
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

		var audioConfig = kissTestAudioConfig()
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
