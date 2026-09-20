// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// The packet decoders are the part of Samoyed that anyone on frequency can
// reach, and they take nothing more than a []byte or a string, so they are
// cheap to fuzz.  A target's job is to make the call; the failure it is
// looking for is a panic, so there is usually nothing to assert.
//
// Seeds added here run as ordinary unit tests under "go test", so an input
// that once crashed a decoder stays checked even when nobody is fuzzing.
// Fuzzing proper is "go test ./src/ -run XXX -fuzz FuzzSomething".

// The decoders narrate a malformed packet at length, and a fuzzing run has
// nobody to read it, so point stdout at the bin for the duration.
func fuzzQuietly(tb testing.TB) {
	tb.Helper()

	var devNull, err = os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(tb, err)

	var saved = os.Stdout
	os.Stdout = devNull

	tb.Cleanup(func() {
		os.Stdout = saved
		devNull.Close()
	})
}

// FuzzAX25FromFrame covers the path every received frame takes, from the
// modem, a KISS client, a network TNC, an AGW client or IL2P: build a packet
// from the bytes off the air, then ask it the questions the receive path asks.
func FuzzAX25FromFrame(f *testing.F) {
	fuzzQuietly(f)

	// An ordinary APRS position report.
	var pp = AX25FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)
	f.Add(ax25_get_frame_data(pp))

	// Addresses and a control byte, with no PID and no information part:
	// the shortest frame AX25FromFrame accepts (issue #670).
	f.Add([]byte("000000000000010"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var pp = AX25FromFrame(data, ALevel{rec: 50, mark: 50, space: 50})
		if pp == nil {
			return
		}

		AX25FormatAddrs(pp)
		AX25GetInfo(pp)
		ax25_format_via_path(pp)
		ax25_frame_type(pp)
		ax25_is_aprs(pp)
		ax25_dedupe_crc(pp)
		ax25_get_dti(pp)
		ax25_check_addresses(pp, addrLenient)
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
		var pp = ax25_from_text(monitor, addrLenient)
		if pp == nil {
			return
		}

		AX25FormatAddrs(pp)
		AX25GetInfo(pp)
		ax25_frame_type(pp)
	})
}

// FuzzDecodeAPRS covers the information part decoders, which is where most of
// the reading-off-the-end has been.
func FuzzDecodeAPRS(f *testing.F) {
	fuzzQuietly(f)

	deviceIDData = NewDeviceIDData()

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

	f.Fuzz(func(t *testing.T, monitor string) {
		var pp = ax25_from_text(monitor, addrLenient)
		if pp == nil {
			return
		}

		decode_aprs(pp, true, "")
	})
}

// FuzzKISSUnwrap covers the KISS framing, which any client on the KISS TCP
// port or the serial KISS device can feed.
func FuzzKISSUnwrap(f *testing.F) {
	fuzzQuietly(f)

	f.Add([]byte{0x00, 'h', 'e', 'l', 'l', 'o', FEND})
	f.Add([]byte{0x00, FESC, TFEND, FESC, TFESC, FEND})
	f.Add([]byte{FEND})

	f.Fuzz(func(t *testing.T, in []byte) {
		kiss_unwrap(in)
	})
}

// FuzzIL2PDecodeFrame covers the IL2P receive path: header FEC, descrambling
// and the payload blocks.
func FuzzIL2PDecodeFrame(f *testing.F) {
	fuzzQuietly(f)

	il2p_init(0)

	var pp = AX25FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
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
