// SPDX-FileCopyrightText: 2025 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// two_base91_to_i has no value for an invalid character.  Using its result
// without checking it turned the bit pattern of the old G_UNKNOWN sentinel
// into eight "real" digital values, and would have done the same for an
// analog one.

func Test_telemetry_data_base91_invalid_character(t *testing.T) {
	var ts = NewTelemetryState()

	assert.Equal(t,
		"Seq=7544, A1=1472, A2=1564, A3=1656, A4=1748, A5=1840, D1=1, D2=1, D3=0, D4=0, D5=0, D6=0, D7=0, D8=0",
		ts.telemetry_data_base91("Q1TEST", "ss1122334455!$"),
		"all values valid")

	// "~" is outside the base 91 range, so the digital values are unknown.

	assert.Equal(t,
		"Seq=7544, A1=1472, A2=1564, A3=1656, A4=1748, A5=1840",
		ts.telemetry_data_base91("Q1TEST", "ss1122334455!~"),
		"invalid digital value")

	// Likewise for an analog value, and for the sequence number.

	assert.Equal(t,
		"Seq=7544, A1=1472, A3=1656, A4=1748, A5=1840",
		ts.telemetry_data_base91("Q1TEST", "ss11 ~334455"),
		"invalid analog value")

	assert.Equal(t,
		"Seq=?, A1=1472",
		ts.telemetry_data_base91("Q1TEST", " ~11"),
		"invalid sequence number")
}
