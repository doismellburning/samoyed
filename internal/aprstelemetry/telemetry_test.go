// SPDX-FileCopyrightText: 2025 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package aprstelemetry

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// base91Pair has no value for an invalid character.  Using its result
// without checking it turned the bit pattern of the old G_UNKNOWN sentinel
// into eight "real" digital values, and would have done the same for an
// analog one.

func Test_telemetry_data_base91_invalid_character(t *testing.T) {
	var ts = New()

	assert.Equal(t,
		"Seq=7544, A1=1472, A2=1564, A3=1656, A4=1748, A5=1840, D1=1, D2=1, D3=0, D4=0, D5=0, D6=0, D7=0, D8=0",
		ts.DataBase91("Q1TEST", "ss1122334455!$"),
		"all values valid")

	// "~" is outside the base 91 range, so the digital values are unknown.

	assert.Equal(t,
		"Seq=7544, A1=1472, A2=1564, A3=1656, A4=1748, A5=1840",
		ts.DataBase91("Q1TEST", "ss1122334455!~"),
		"invalid digital value")

	// Likewise for an analog value, and for the sequence number.

	assert.Equal(t,
		"Seq=7544, A1=1472, A3=1656, A4=1748, A5=1840",
		ts.DataBase91("Q1TEST", "ss11 ~334455"),
		"invalid analog value")

	assert.Equal(t,
		"Seq=?, A1=1472",
		ts.DataBase91("Q1TEST", " ~11"),
		"invalid sequence number")
}

// strconv returns a zero alongside its error, so an unparseable sequence
// number or analog value used to be reported as a real reading of zero.

func Test_telemetry_data_original_unparseable_value(t *testing.T) {
	var ts = New()

	var result, _ = ts.DataOriginal("Q1TEST", "T#005,199,000,255,073,123,01101001", true)
	assert.Equal(t,
		"Seq=5, A1=199, A2=0, A3=255, A4=73, A5=123, D1=0, D2=1, D3=1, D4=0, D5=1, D6=0, D7=0, D8=1",
		result,
		"all values valid")

	result, _ = ts.DataOriginal("Q1TEST", "T#abc,199,000,255,073,123,01101001", true)
	assert.Equal(t,
		"Seq=?, A1=199, A2=0, A3=255, A4=73, A5=123, D1=0, D2=1, D3=1, D4=0, D5=1, D6=0, D7=0, D8=1",
		result,
		"unparseable sequence number")

	result, _ = ts.DataOriginal("Q1TEST", "T#005,199,xyz,255,073,123,01101001", true)
	assert.Equal(t,
		"Seq=5, A1=199, A3=255, A4=73, A5=123, D1=0, D2=1, D3=1, D4=0, D5=1, D6=0, D7=0, D8=1",
		result,
		"unparseable analog value")
}

// Samoyed decodes APRS on more than one goroutine - the receive path for RF,
// the IGate's for packets from APRS-IS it might send to RF - and they share one
// State, so metadata arriving on one mustn't race data decoding on the other.
// This only fails under the race detector, which CI runs with "make race".

func Test_State_concurrent(t *testing.T) {
	var ts = New()

	var wg sync.WaitGroup

	for n := range 4 {
		var station = fmt.Sprintf("Q%dTEST", n+1)

		wg.Go(func() {
			for range 100 {
				ts.NameMessage(station, "Vbat,Vsolar,Temp,Sat")
				ts.CoefficientsMessage(station, "0,0.001,0,0,0.001,0,0,0.1,-273.2,0,1,0,0,1,0", true)
			}
		})

		wg.Go(func() {
			for range 100 {
				ts.DataOriginal(station, "T#005,199,000,255,073,123,01101001", true)
				ts.DataBase91(station, "ss1122334455!$")
			}
		})
	}

	wg.Wait()

	var result, _ = ts.DataOriginal("Q1TEST", "T#005,199,000,255,073,123,01101001", true)
	assert.Equal(t,
		"Seq=5, Vbat=0.199, Vsolar=0.000, Temp=-247.7, Sat=73, A5=123, D1=0, D2=1, D3=1, D4=0, D5=1, D6=0, D7=0, D8=1",
		result)
}

// Metadata can come from any number of stations, so only the most recently
// used are kept.  One dropped to make room decodes with the defaults again.

func Test_State_drops_least_recently_used(t *testing.T) {
	var ts = New()
	ts.capacity = 2

	ts.NameMessage("Q1TEST", "One")
	ts.NameMessage("Q2TEST", "Two")

	// Decoding Q1TEST's data makes Q2TEST the least recently used...
	ts.DataOriginal("Q1TEST", "T#1,1", true)

	// ...so Q3TEST takes its place.
	ts.NameMessage("Q3TEST", "Three")

	assert.Len(t, ts.stations, 2)

	// Q2TEST last, as decoding its data stores it afresh, dropping another.
	for _, c := range []struct{ station, want string }{
		{"Q1TEST", "Seq=1, One=1"},
		{"Q3TEST", "Seq=1, Three=1"},
		{"Q2TEST", "Seq=1, A1=1"},
	} {
		var result, _ = ts.DataOriginal(c.station, "T#1,1", true)
		assert.Equal(t, c.want, result, c.station)
	}
}
