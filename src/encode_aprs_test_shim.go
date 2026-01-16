package direwolf

// #include "direwolf.h"
// #include <stdlib.h>
// #include <string.h>
// #include <stdio.h>
// #include <unistd.h>
// #include <ctype.h>
// #include <time.h>
// #include <math.h>
// #include <assert.h>
// #include "latlong.h"
import "C"

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Save a lot of re-encoding...
func _encode_position(
	messaging C.int, compressed C.int, lat C.double, lon C.double, ambiguity C.int, alt_ft C.int,
	symtab C.char, symbol C.char, power C.int, height C.int, gain C.int, dir string, course C.int, speed C.int, freq C.float, tone C.float,
	offset C.float, comment string) string {
	return encode_position(
		messaging,
		compressed,
		lat,
		lon,
		ambiguity,
		alt_ft,
		symtab,
		symbol,
		power,
		height,
		gain,
		C.CString(dir),
		course,
		speed,
		freq,
		tone,
		offset,
		C.CString(comment),
	)
}

func _encode_object(
	name string, compressed C.int, thyme C.time_t, lat C.double, lon C.double, ambiguity C.int,
	symtab C.char, symbol C.char, power C.int, height C.int, gain C.int, dir string, course C.int, speed C.int, freq C.float, tone C.float,
	offset C.float, comment string) string {
	return encode_object(
		C.CString(name),
		compressed,
		thyme,
		lat,
		lon,
		ambiguity,
		symtab,
		symbol,
		power,
		height,
		gain,
		C.CString(dir),
		course,
		speed,
		freq,
		tone,
		offset,
		C.CString(comment),
	)
}

func _encode_message(addressee string, text string, id string) string {
	return encode_message(C.CString(addressee), C.CString(text), C.CString(id))
}

func encode_aprs_test_main(t *testing.T) {
	t.Helper()

	var result string

	/***********  Position  ***********/

	result = _encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		0, 0, 0, "", C.G_UNKNOWN, 0, 0, 0, 0, "")
	assert.Equal(t, "!4234.61ND07126.47W&", result)

	/* with PHG. */
	// TODO:  Need to test specifying some but not all.

	result = _encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		50, 100, 6, "N", C.G_UNKNOWN, 0, 0, 0, 0, "")
	assert.Equal(t, "!4234.61ND07126.47W&PHG7368", result)

	/* with freq & tone.  minus offset, no offset, explicit simplex. */

	result = _encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		0, 0, 0, "", C.G_UNKNOWN, 0, 146.955, 74.4, -0.6, "")
	assert.Equal(t, "!4234.61ND07126.47W&146.955MHz T074 -060 ", result)

	result = _encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		0, 0, 0, "", C.G_UNKNOWN, 0, 146.955, 74.4, C.G_UNKNOWN, "")
	assert.Equal(t, "!4234.61ND07126.47W&146.955MHz T074 ", result)

	result = _encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		0, 0, 0, "", C.G_UNKNOWN, 0, 146.955, 74.4, 0, "")
	assert.Equal(t, "!4234.61ND07126.47W&146.955MHz T074 +000 ", result)

	/* with course/speed, freq, and comment! */

	result = _encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		0, 0, 0, "", 180, 55, 146.955, 74.4, -0.6, "River flooding")
	assert.Equal(t, "!4234.61ND07126.47W&180/055146.955MHz T074 -060 River flooding", result)

	/* Course speed, no tone, + offset */

	result = _encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		0, 0, 0, "", 180, 55, 146.955, C.G_UNKNOWN, 0.6, "River flooding")
	assert.Equal(t, "!4234.61ND07126.47W&180/055146.955MHz +060 River flooding", result)

	/* Course speed, no tone, + offset + altitude */

	result = _encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, 12345, 'D', '&',
		0, 0, 0, "", 180, 55, 146.955, C.G_UNKNOWN, 0.6, "River flooding")
	assert.Equal(t, "!4234.61ND07126.47W&180/055146.955MHz +060 /A=012345River flooding", result)

	result = _encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, 12345, 'D', '&',
		0, 0, 0, "", 180, 55, 146.955, 0, 0.6, "River flooding")
	assert.Equal(t, "!4234.61ND07126.47W&180/055146.955MHz Toff +060 /A=012345River flooding", result)

	// TODO: try boundary conditions of course = 0, 359, 360

	/*********** Compressed position. ***********/

	result = _encode_position(0, 1, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		0, 0, 0, "", C.G_UNKNOWN, 0, 0, 0, 0, "")
	assert.Equal(t, "!D8yKC<Hn[&  !", result)

	/* with PHG. In this case it is converted to precomputed radio range.  TODO: check on this.  Is 27.4 correct? */

	result = _encode_position(0, 1, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		50, 100, 6, "N", C.G_UNKNOWN, 0, 0, 0, 0, "")
	assert.Equal(t, "!D8yKC<Hn[&{CG", result)

	/* with course/speed, freq, and comment!  Roundoff. 55 knots should be 63 MPH.  we get 62. */

	result = _encode_position(0, 1, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		0, 0, 0, "", 180, 55, 146.955, 74.4, -0.6, "River flooding")
	assert.Equal(t, "!D8yKC<Hn[&NUG146.955MHz T074 -060 River flooding", result)

	// TODO:  test alt; cs+alt

	/*********** Object. ***********/

	result = _encode_object("WB1GOF-C", 0, 0, 42+34.61/60, -(71 + 26.47/60), 0, 'D', '&',
		0, 0, 0, "", C.G_UNKNOWN, 0, 0, 0, 0, "")
	assert.Equal(t, ";WB1GOF-C *111111z4234.61ND07126.47W&", result)

	// TODO: need more tests.

	/*********** Message. ***********/

	result = _encode_message("N2GH", "some stuff", "")
	assert.Equal(t, ":N2GH     :some stuff", result)

	result = _encode_message("N2GH", "other stuff", "12345")
	assert.Equal(t, ":N2GH     :other stuff{12345", result)

	result = _encode_message("WB2OSZ-123", "other stuff", "12345")
	assert.Equal(t, ":WB2OSZ-12:other stuff{12345", result)

	dw_printf("Encode APRS test PASSED with no errors.\n")
} /* end main */
