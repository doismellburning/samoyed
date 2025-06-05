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
// #include "encode_aprs.h"
// #include "latlong.h"
// #include "textcolor.h"
import "C"

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Save a lot of re-encoding...
func encode_position(
	messaging C.int, compressed C.int, lat C.double, lon C.double, ambiguity C.int, alt_ft C.int,
	symtab C.char, symbol C.char, power C.int, height C.int, gain C.int, dir string, course C.int, speed C.int, freq C.float, tone C.float,
	offset C.float, comment string, presult *C.char, result_size C.size_t) {
	C.encode_position(
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
		presult,
		result_size,
	)
}

func encode_object(
	name string, compressed C.int, thyme C.time_t, lat C.double, lon C.double, ambiguity C.int,
	symtab C.char, symbol C.char, power C.int, height C.int, gain C.int, dir string, course C.int, speed C.int, freq C.float, tone C.float,
	offset C.float, comment string, presult *C.char, result_size C.size_t) {
	C.encode_object(
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
		presult,
		result_size,
	)
}

func encode_message(addressee string, text string, id string, presult *C.char, result_size C.size_t) {
	C.encode_message(C.CString(addressee), C.CString(text), C.CString(id), presult, result_size)
}

func encode_aprs_test_main(t *testing.T) {
	t.Helper()

	var resultAlloc [100]C.char
	var result = &resultAlloc[0]

	/***********  Position  ***********/

	encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		0, 0, 0, "", C.G_UNKNOWN, 0, 0, 0, 0, "", result, 100)
	assert.Equal(t, C.GoString(result), "!4234.61ND07126.47W&")

	/* with PHG. */
	// TODO:  Need to test specifying some but not all.

	encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		50, 100, 6, "N", C.G_UNKNOWN, 0, 0, 0, 0, "", result, 100)
	assert.Equal(t, C.GoString(result), "!4234.61ND07126.47W&PHG7368")

	/* with freq & tone.  minus offset, no offset, explicit simplex. */

	encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		0, 0, 0, "", C.G_UNKNOWN, 0, 146.955, 74.4, -0.6, "", result, 100)
	assert.Equal(t, C.GoString(result), "!4234.61ND07126.47W&146.955MHz T074 -060 ")

	encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		0, 0, 0, "", C.G_UNKNOWN, 0, 146.955, 74.4, C.G_UNKNOWN, "", result, 100)
	assert.Equal(t, C.GoString(result), "!4234.61ND07126.47W&146.955MHz T074 ")

	encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		0, 0, 0, "", C.G_UNKNOWN, 0, 146.955, 74.4, 0, "", result, 100)
	assert.Equal(t, C.GoString(result), "!4234.61ND07126.47W&146.955MHz T074 +000 ")

	/* with course/speed, freq, and comment! */

	encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		0, 0, 0, "", 180, 55, 146.955, 74.4, -0.6, "River flooding", result, 100)
	assert.Equal(t, C.GoString(result), "!4234.61ND07126.47W&180/055146.955MHz T074 -060 River flooding")

	/* Course speed, no tone, + offset */

	encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		0, 0, 0, "", 180, 55, 146.955, C.G_UNKNOWN, 0.6, "River flooding", result, 100)
	assert.Equal(t, C.GoString(result), "!4234.61ND07126.47W&180/055146.955MHz +060 River flooding")

	/* Course speed, no tone, + offset + altitude */

	encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, 12345, 'D', '&',
		0, 0, 0, "", 180, 55, 146.955, C.G_UNKNOWN, 0.6, "River flooding", result, 100)
	assert.Equal(t, C.GoString(result), "!4234.61ND07126.47W&180/055146.955MHz +060 /A=012345River flooding")

	encode_position(0, 0, 42+34.61/60, -(71 + 26.47/60), 0, 12345, 'D', '&',
		0, 0, 0, "", 180, 55, 146.955, 0, 0.6, "River flooding", result, 100)
	assert.Equal(t, C.GoString(result), "!4234.61ND07126.47W&180/055146.955MHz Toff +060 /A=012345River flooding")

	// TODO: try boundary conditions of course = 0, 359, 360

	/*********** Compressed position. ***********/

	encode_position(0, 1, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		0, 0, 0, "", C.G_UNKNOWN, 0, 0, 0, 0, "", result, 100)
	assert.Equal(t, C.GoString(result), "!D8yKC<Hn[&  !")

	/* with PHG. In this case it is converted to precomputed radio range.  TODO: check on this.  Is 27.4 correct? */

	encode_position(0, 1, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		50, 100, 6, "N", C.G_UNKNOWN, 0, 0, 0, 0, "", result, 100)
	assert.Equal(t, C.GoString(result), "!D8yKC<Hn[&{CG")

	/* with course/speed, freq, and comment!  Roundoff. 55 knots should be 63 MPH.  we get 62. */

	encode_position(0, 1, 42+34.61/60, -(71 + 26.47/60), 0, C.G_UNKNOWN, 'D', '&',
		0, 0, 0, "", 180, 55, 146.955, 74.4, -0.6, "River flooding", result, 100)
	assert.Equal(t, C.GoString(result), "!D8yKC<Hn[&NUG146.955MHz T074 -060 River flooding")

	// TODO:  test alt; cs+alt

	/*********** Object. ***********/

	encode_object("WB1GOF-C", 0, 0, 42+34.61/60, -(71 + 26.47/60), 0, 'D', '&',
		0, 0, 0, "", C.G_UNKNOWN, 0, 0, 0, 0, "", result, 100)
	assert.Equal(t, C.GoString(result), ";WB1GOF-C *111111z4234.61ND07126.47W&")

	// TODO: need more tests.

	/*********** Message. ***********/

	encode_message("N2GH", "some stuff", "", result, 100)
	assert.Equal(t, C.GoString(result), ":N2GH     :some stuff")

	encode_message("N2GH", "other stuff", "12345", result, 100)
	assert.Equal(t, C.GoString(result), ":N2GH     :other stuff{12345")

	encode_message("WB2OSZ-123", "other stuff", "12345", result, 100)
	assert.Equal(t, C.GoString(result), ":WB2OSZ-12:other stuff{12345")

	dw_printf("Encode APRS test PASSED with no errors.\n")
} /* end main */
