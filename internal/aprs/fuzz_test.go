// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package aprs

import (
	"io"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/sirupsen/logrus"
)

// APRS packets arrive from anyone on frequency, or from anyone on APRS-IS by
// way of the IGate, so the decoder here is fuzzed like the rest of the packet
// decoders.  A target's job is to make the call; the failure it is looking
// for is a panic, so there is usually nothing to assert.
//
// Seeds added here run as ordinary unit tests under "go test", so an input
// that once crashed a decoder stays checked even when nobody is fuzzing.

// The decoder complains at length about a malformed packet, and a fuzzing
// run has nobody to read it, so send logrus to the bin for the duration.
func fuzzQuietly(tb testing.TB) {
	tb.Helper()

	var saved = logrus.StandardLogger().Out
	logrus.SetOutput(io.Discard)

	tb.Cleanup(func() {
		logrus.SetOutput(saved)
	})
}

// FuzzDecodeAPRS covers the information part decoders, which is where most of
// the reading-off-the-end has been.
func FuzzDecodeAPRS(f *testing.F) {
	fuzzQuietly(f)

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

	f.Fuzz(func(t *testing.T, monitor string) {
		var pp = ax25.FromTextWithStrictness(monitor, ax25.AddrLenient)
		if pp == nil {
			return
		}

		aprsDecoder.Decode(pp, true)
	})
}
