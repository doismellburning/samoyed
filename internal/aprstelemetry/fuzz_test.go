// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package aprstelemetry

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
)

// Telemetry arrives from anyone on frequency, or from anyone on APRS-IS by way
// of the IGate, so the decoders here are fuzzed like the rest of the packet
// decoders in internal/direwolf.  A target's job is to make the call; the
// failure it is looking for is a panic, so there is usually nothing to assert.
//
// Seeds added here run as ordinary unit tests under "go test", so an input
// that once crashed a decoder stays checked even when nobody is fuzzing.

// FuzzDataOriginal covers the original "T#" format, complaints and all.
func FuzzDataOriginal(f *testing.F) {
	testutils.DiscardLogrus(f)

	// From the protocol spec, with and without a comment.
	f.Add("T#005,199,000,255,073,123,01101001")
	f.Add("T#005,199,000,255,073,123,01101001Comment,with,commas")

	// Not integers, not fixed width, as heard on the air.
	f.Add("T#491,4.9,0.3,25.0,0.0,1.0,00000000")

	// Unparseable sequence number and analog value, and too few values.
	f.Add("T#abc,199,000,255,073,123,01101001")
	f.Add("T#005,199,xyz,255,073,123,01101001")
	f.Add("T#005,199,000,255,073,123,0110")
	f.Add("T#")

	f.Fuzz(func(t *testing.T, info string) {
		New().DataOriginal("Q1TEST", info, false)
	})
}

// FuzzDataBase91 covers the base 91 compressed format.  The caller's regular
// expression only ever hands over an even number of 4 to 14 base 91 digits,
// but DataBase91 is exported, so it gets anything.
func FuzzDataBase91(f *testing.F) {
	testutils.DiscardLogrus(f)

	f.Add("ss11")
	f.Add("ss1122334455!$")

	// Invalid base 91 characters in the digital, analog and sequence values.
	f.Add("ss1122334455!~")
	f.Add("ss11 ~334455")
	f.Add(" ~11")

	f.Fuzz(func(t *testing.T, cdata string) {
		New().DataBase91("Q1TEST", cdata)
	})
}

// FuzzMetadata covers the four metadata messages, then decodes data with
// whatever they left behind, so odd names, units, coefficients and precisions
// all find their way into the formatting.
func FuzzMetadata(f *testing.F) {
	testutils.DiscardLogrus(f)

	// A balloon's metadata and data, from the ported unit test.
	f.Add(
		"Vbat,Vsolar,Temp,Sat",
		"V,V,C,,m",
		"0,0.001,0,0,0.001,0,0,0.1,-273.2,0,1,0,0,1,0",
		"11111111,10mW research balloon",
		"T#005,199,000,255,073,123,01101001",
	)

	// Too few of everything.
	f.Add("-", ",", ",,", "1", "T#1,2")

	f.Fuzz(func(t *testing.T, parm, unit, eqns, bits, data string) {
		var ts = New()

		ts.NameMessage("Q1TEST", parm)
		ts.UnitLabelMessage("Q1TEST", unit)
		ts.CoefficientsMessage("Q1TEST", eqns, false)
		ts.BitSenseMessage("Q1TEST", bits, false)

		ts.DataOriginal("Q1TEST", data, false)
	})
}
