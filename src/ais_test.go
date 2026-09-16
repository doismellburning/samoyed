// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"fmt"
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nmeaSentence wraps a body in the "*XX" checksum an AIS sentence carries.
func nmeaSentence(body string) string {
	var cs byte

	for _, p := range []byte(body[1:]) {
		cs ^= p
	}

	return fmt.Sprintf("%s*%02X", body, cs)
}

// setFieldString encodes text into a six-bit ASCII field, padding with '@' as
// a real transmitter does.
func setFieldString(t *testing.T, base []byte, start uint, length uint, s string) {
	t.Helper()
	require.LessOrEqual(t, uint(len(s))*6, length)

	for i := range length / 6 {
		var ch byte = '@'
		if i < uint(len(s)) {
			ch = s[i]
		}

		var val = strings.IndexByte(sixBitASCII, ch)
		require.GreaterOrEqual(t, val, 0)

		set_field(base, start+i*6, 6, val)
	}
}

// A frame whose length is not a multiple of 3 bytes does not end on a sextet
// boundary, so the last character of the payload is made up partly of padding.
// Reading it used to run off the end of the frame and panic - and 53 bytes is
// the length of a type 5 message, which is not exotic at all.
func Test_ais_to_nmea_frame_length_not_multiple_of_3(t *testing.T) {
	for n := 1; n <= 53; n++ {
		var nmea, err = AISToNMEA(make([]byte, n))
		require.NoError(t, err)

		var _, rest, found = strings.Cut(string(nmea), "!AIVDM,1,1,,A,")
		require.True(t, found)

		var payload, pad, _ = strings.Cut(rest, ",")

		// One character per 6 bits, rounded up, and the padding count says how
		// many of the bits in that last character were not in the frame.
		var ns = (n*8 + 5) / 6
		assert.Len(t, payload, ns, "frame of %d bytes", n)
		assert.Equal(t, fmt.Sprintf("%d*", ns*6-n*8), pad[:2], "frame of %d bytes", n)
	}
}

// The type 5 message is both a length that needs padding and the one carrying
// the ship's details, so round trip one whole.
func Test_ais_type_5_round_trip(t *testing.T) {
	var ais = make([]byte, 53) // 424 bits.

	set_field(ais, 0, 6, 5)          // Message type.
	set_field(ais, 8, 30, 366730001) // MMSI.
	setFieldString(t, ais, 70, 42, "Q1TEST")
	setFieldString(t, ais, 112, 120, "SAMOYED")
	setFieldString(t, ais, 302, 120, "LONG BEACH")

	var nmea, err = AISToNMEA(ais)
	require.NoError(t, err)

	var aisData, parseErr = AISParse(string(nmea))
	require.NoError(t, parseErr)
	assert.Equal(t, "AIS 5: Static and Voyage Related Data", aisData.Description)
	assert.Equal(t, "366730001", aisData.MMSI)
	assert.Equal(t, "SAMOYED, Q1TEST, dest. LONG BEACH", aisData.Comment)
}

// The bit vector used to be a fixed 256 bytes, which a user-defined APRS
// packet could overrun: it can carry far more payload than any real AIS
// message.
func Test_ais_parse_payload_longer_than_bit_vector(t *testing.T) {
	var payload = strings.Repeat("1", 400) // 2400 bits, against 2048 of vector.

	var aisData, err = AISParse(nmeaSentence("!AIVDM,1,1,,A," + payload + ",0"))
	require.NoError(t, err)
	assert.Equal(t, "AIS 1: Position Report Class A", aisData.Description)
}

// A short sentence is decoded as though the bits it lacks were zero, rather
// than failing or reading whatever follows the vector.
func Test_ais_parse_payload_shorter_than_message_type(t *testing.T) {
	var aisData, err = AISParse(nmeaSentence("!AIVDM,1,1,,A,5,6"))
	require.NoError(t, err)
	assert.Equal(t, "AIS 5: Static and Voyage Related Data", aisData.Description)
	assert.Equal(t, "000000000", aisData.MMSI)
}

// The checksum is over the bytes of the sentence.  Summing its runes instead
// rejected any sentence carrying a byte above 0x7f, having decoded it to
// something else entirely.
func Test_ais_parse_checksum_is_over_bytes(t *testing.T) {
	var aisData, err = AISParse(nmeaSentence("!AIVDM,1,1,,\xc3,15MgK45P3@G?fl0E`JbR0OwT0@MS,0"))
	require.NoError(t, err)
	assert.Equal(t, "366730000", aisData.MMSI)
}

// A checksum field that is not two hexadecimal digits is an error, rather than
// being quietly read as zero or accepted for the value it happens to hold.
func Test_ais_parse_malformed_checksum(t *testing.T) {
	// The first sentence checksums to 0x4e and the second to 0x02, so each of
	// these fields is either unparseable or the right value at the wrong width.
	var testCases = []struct {
		body     string
		checksum string
	}{
		{"!AIVDM,1,1,,A,15MgK45P3@G?fl0E`JbR0OwT0@MS,0", ""},
		{"!AIVDM,1,1,,A,15MgK45P3@G?fl0E`JbR0OwT0@MS,0", "zz"},
		{"!AIVDM,1,1,,A,15MgK45P3@G?fl0E`JbR0OwT0@MS,0", "-4E"},
		{"!AIVDM,1,1,,A,15MgK45P3@G?fl0E`JbR0OwT0@MS,0", "14E"},
		{"!AIVDM,1,1,,A,15MgK45P3@G?fl0E`JbR0OwT0@MS,0", "004E"},
		{"!AIVDM,1,1,,A,15@`,0", "2"},
	}

	for _, tc := range testCases {
		var aisData, err = AISParse(tc.body + "*" + tc.checksum)
		require.Error(t, err, "checksum %q", tc.checksum)
		assert.Nil(t, aisData, "checksum %q", tc.checksum)
	}

	// The same short sentence, correctly checksummed, is fine.
	var aisData, err = AISParse("!AIVDM,1,1,,A,15@`,0*02")
	require.NoError(t, err)
	assert.Equal(t, "AIS 1: Position Report Class A", aisData.Description)
}

// Latitude and longitude are two's complement, so the southern and western
// hemispheres depend on the field being sign extended from its own width.
func Test_get_field_signed(t *testing.T) {
	var testCases = []struct {
		length   uint
		raw      int
		expected int
	}{
		{6, 0b011111, 31},
		{6, 0b100000, -32},
		{6, 0b111111, -1},
		{27, 91 * 600000, 91 * 600000},
		{27, (1 << 27) - 1, -1},
		{28, int(-71.06*600000) & ((1 << 28) - 1), int(-71.06 * 600000)},
		{31, (1 << 30) - 1, (1 << 30) - 1},
		{31, 1 << 30, -(1 << 30)},
	}

	for _, tc := range testCases {
		var base = make([]byte, 8)
		set_field(base, 1, tc.length, tc.raw) // Offset 1 to catch byte-aligned assumptions.
		assert.Equal(t, tc.expected, get_field_signed(base, 1, tc.length), "%d bits of %b", tc.length, tc.raw)
	}
}

// The altitude of a SAR aircraft is 12 bits, and the all-ones value means it
// is not available - which was being reported as a position 4095 metres up.
func Test_ais_type_9_altitude_not_available(t *testing.T) {
	var altitude = func(raw int) maybe.Maybe[float64] {
		var ais = make([]byte, 21) // 168 bits.

		set_field(ais, 0, 6, 9)          // Message type.
		set_field(ais, 8, 30, 366730001) // MMSI.
		set_field(ais, 38, 12, raw)      // Altitude, metres.

		var nmea, err = AISToNMEA(ais)
		require.NoError(t, err)

		var aisData, parseErr = AISParse(string(nmea))
		require.NoError(t, parseErr)
		assert.Equal(t, "AIS 9: SAR Aircraft Position Report", aisData.Description)

		return aisData.AltM
	}

	assert.Equal(t, maybe.Just(1500.0), altitude(1500))
	assert.Equal(t, maybe.Just(4094.0), altitude(4094))
	assert.Equal(t, maybe.Nothing[float64](), altitude(4095))
}
