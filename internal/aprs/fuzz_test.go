// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package aprs

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/testutils"
)

// APRS packets arrive from anyone on frequency, or from anyone on APRS-IS by
// way of the IGate, so the decoder here is fuzzed like the rest of the packet
// decoders.  A target's job is to make the call; the failure it is looking
// for is a panic, so there is usually nothing to assert.
//
// Seeds added here run as ordinary unit tests under "go test", so an input
// that once crashed a decoder stays checked even when nobody is fuzzing.

// FuzzDecodeAPRS covers the information part decoders, which is where most of
// the reading-off-the-end has been.
func FuzzDecodeAPRS(f *testing.F) {
	testutils.DiscardLogrus(f)

	var aprsDecoder = NewDecoderFromDataFiles()

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

	// AIS user-defined data with no AIS sentence at all.
	f.Add("0>0:{DA")

	// One of each of the formats the decoder knows, for the fuzzer to work
	// from: Mic-E with an altitude and a device suffix, a compressed
	// position and object, raw NMEA, both Ultimeter forms, a reply-ack
	// message, telemetry, AIS, and a comment with a frequency and !DAO!.
	f.Add("Q1TEST>T2SP0W:`c_Vm6hk/`\"49}Q1TEST_%")
	f.Add("Q1TEST>APDW17:=/5L!!<*e7>{?!Range")
	f.Add("Q1TEST>APDW17:;Q2TEST   *092345z/5L!!<*e7OS]S")
	f.Add("Q1TEST>APDW17:$GPRMC,063909,A,3349.4302,N,11700.3721,W,43.022,89.3,291099,13.6,E*52")
	f.Add("Q1TEST>APDW17:$GPGGA,102705,5157.9762,N,00029.3256,W,1,04,2.0,75.7,M,47.6,M,,*62")
	f.Add("Q1TEST>APDW17:$ULTW0000000001110B6E27F4FFF3897B0001035E004E04DD00030000")
	f.Add("Q1TEST>APDW17:!!00000066013D000028710166--------0158053201200210")
	f.Add("Q1TEST>APDW17::Q2TEST   :Hello{AB}CD")
	f.Add("Q1TEST>APDW17::Q1TEST   :EQNS.0,0.1,0,0,1,-40,0,1,0")
	f.Add("Q1TEST>APDW17:{DA!AIVDM,1,1,,A,15MgK45P3@G?fl0E`JbR0OwT0@MS,0*4E")
	f.Add("Q1TEST>APDW17:!4903.50N/07201.75W-146.520MHz C100 -060 R25m!w\"<!")
	f.Add("Q1TEST>APDW17:}Q2TEST>APDW17,TCPIP,Q1TEST*:>IO91SX/G Status")

	f.Fuzz(func(t *testing.T, monitor string) {
		var pp = ax25.FromTextWithStrictness(monitor, ax25.AddrLenient)
		if pp == nil {
			return
		}

		aprsDecoder.Decode(pp, true)
	})
}
