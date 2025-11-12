package direwolf

// #include <stdio.h>
// #include "ais.h"
// int char_to_sextet (char ch);
// int sextet_to_char (int val);
import "C"

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func ais_test_main(t *testing.T) {
	t.Helper()

	C.setlinebuf(C.stdout) // Hacks to get dw_printf output when things go wrong

	test_sextet(t)
	test_basic_parse(t)
}

func test_sextet(t *testing.T) {
	t.Helper()

	for i := range 64 {
		assert.Equal(t, i, int(C.char_to_sextet((C.char)(C.sextet_to_char(C.int(i))))))
	}
}

func test_basic_parse(t *testing.T) {
	t.Helper()

	var ais = "!AIVDM,1,1,,A,15MgK45P3@G?fl0E`JbR0OwT0@MS,0*4E" // Example from https://www.aggsoft.com/ais-decoder.htm

	var descr, mssi, comment [256]C.char
	var lat, lon C.double
	var knots, course, alt_m C.float
	var symtab, symbol C.char

	var status = C.ais_parse(C.CString(ais), 0, &descr[0], C.int(len(descr)), &mssi[0], C.int(len(mssi)), &lat, &lon, &knots, &course, &alt_m, &symtab, &symbol, &comment[0], C.int(len(comment)))

	assert.Equal(t, C.int(0), status)
	assert.Equal(t, "AIS 1: Position Report Class A", C.GoString(&descr[0]))
	assert.Equal(t, "366730000", C.GoString(&mssi[0]))
	assert.Empty(t, C.GoString(&comment[0]))
	assert.InDelta(t, -122, float64(lon), 1)
	assert.InDelta(t, 20.8, float64(knots), 1)
	assert.InDelta(t, 51.3, float64(course), 1)
}
