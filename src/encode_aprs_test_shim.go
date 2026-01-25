package direwolf

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func encode_aprs_test_main(t *testing.T) {
	t.Helper()

	var result string

	/***********  Position  ***********/

	result = encode_position(false, false, 42+34.61/60, -(71 + 26.47/60), 0, G_UNKNOWN, 'D', '&',
		0, 0, 0, "", G_UNKNOWN, 0, 0, 0, 0, "")
	assert.Equal(t, "!4234.61ND07126.47W&", result)

	/* with PHG. */
	// TODO:  Need to test specifying some but not all.

	result = encode_position(false, false, 42+34.61/60, -(71 + 26.47/60), 0, G_UNKNOWN, 'D', '&',
		50, 100, 6, "N", G_UNKNOWN, 0, 0, 0, 0, "")
	assert.Equal(t, "!4234.61ND07126.47W&PHG7368", result)

	/* with freq & tone.  minus offset, no offset, explicit simplex. */

	result = encode_position(false, false, 42+34.61/60, -(71 + 26.47/60), 0, G_UNKNOWN, 'D', '&',
		0, 0, 0, "", G_UNKNOWN, 0, 146.955, 74.4, -0.6, "")
	assert.Equal(t, "!4234.61ND07126.47W&146.955MHz T074 -060 ", result)

	result = encode_position(false, false, 42+34.61/60, -(71 + 26.47/60), 0, G_UNKNOWN, 'D', '&',
		0, 0, 0, "", G_UNKNOWN, 0, 146.955, 74.4, G_UNKNOWN, "")
	assert.Equal(t, "!4234.61ND07126.47W&146.955MHz T074 ", result)

	result = encode_position(false, false, 42+34.61/60, -(71 + 26.47/60), 0, G_UNKNOWN, 'D', '&',
		0, 0, 0, "", G_UNKNOWN, 0, 146.955, 74.4, 0, "")
	assert.Equal(t, "!4234.61ND07126.47W&146.955MHz T074 +000 ", result)

	/* with course/speed, freq, and comment! */

	result = encode_position(false, false, 42+34.61/60, -(71 + 26.47/60), 0, G_UNKNOWN, 'D', '&',
		0, 0, 0, "", 180, 55, 146.955, 74.4, -0.6, "River flooding")
	assert.Equal(t, "!4234.61ND07126.47W&180/055146.955MHz T074 -060 River flooding", result)

	/* Course speed, no tone, + offset */

	result = encode_position(false, false, 42+34.61/60, -(71 + 26.47/60), 0, G_UNKNOWN, 'D', '&',
		0, 0, 0, "", 180, 55, 146.955, G_UNKNOWN, 0.6, "River flooding")
	assert.Equal(t, "!4234.61ND07126.47W&180/055146.955MHz +060 River flooding", result)

	/* Course speed, no tone, + offset + altitude */

	result = encode_position(false, false, 42+34.61/60, -(71 + 26.47/60), 0, 12345, 'D', '&',
		0, 0, 0, "", 180, 55, 146.955, G_UNKNOWN, 0.6, "River flooding")
	assert.Equal(t, "!4234.61ND07126.47W&180/055146.955MHz +060 /A=012345River flooding", result)

	result = encode_position(false, false, 42+34.61/60, -(71 + 26.47/60), 0, 12345, 'D', '&',
		0, 0, 0, "", 180, 55, 146.955, 0, 0.6, "River flooding")
	assert.Equal(t, "!4234.61ND07126.47W&180/055146.955MHz Toff +060 /A=012345River flooding", result)

	// TODO: try boundary conditions of course = 0, 359, 360

	/*********** Compressed position. ***********/

	result = encode_position(false, true, 42+34.61/60, -(71 + 26.47/60), 0, G_UNKNOWN, 'D', '&',
		0, 0, 0, "", G_UNKNOWN, 0, 0, 0, 0, "")
	assert.Equal(t, "!D8yKC<Hn[&  !", result)

	/* with PHG. In this case it is converted to precomputed radio range.  TODO: check on this.  Is 27.4 correct? */

	result = encode_position(false, true, 42+34.61/60, -(71 + 26.47/60), 0, G_UNKNOWN, 'D', '&',
		50, 100, 6, "N", G_UNKNOWN, 0, 0, 0, 0, "")
	assert.Equal(t, "!D8yKC<Hn[&{CG", result)

	/* with course/speed, freq, and comment!  Roundoff. 55 knots should be 63 MPH.  we get 62. */

	result = encode_position(false, true, 42+34.61/60, -(71 + 26.47/60), 0, G_UNKNOWN, 'D', '&',
		0, 0, 0, "", 180, 55, 146.955, 74.4, -0.6, "River flooding")
	assert.Equal(t, "!D8yKC<Hn[&NUG146.955MHz T074 -060 River flooding", result)

	// TODO:  test alt; cs+alt

	/*********** Object. ***********/

	result = encode_object("WB1GOF-C", false, time.Time{}, 42+34.61/60, -(71 + 26.47/60), 0, 'D', '&',
		0, 0, 0, "", G_UNKNOWN, 0, 0, 0, 0, "")
	assert.Equal(t, ";WB1GOF-C *111111z4234.61ND07126.47W&", result)

	// TODO: need more tests.

	/*********** Message. ***********/

	result = encode_message("N2GH", "some stuff", "")
	assert.Equal(t, ":N2GH     :some stuff", result)

	result = encode_message("N2GH", "other stuff", "12345")
	assert.Equal(t, ":N2GH     :other stuff{12345", result)

	result = encode_message("WB2OSZ-123", "other stuff", "12345")
	assert.Equal(t, ":WB2OSZ-12:other stuff{12345", result)

	dw_printf("Encode APRS test PASSED with no errors.\n")
} /* end main */
