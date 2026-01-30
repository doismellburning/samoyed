package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func ais_test_main(t *testing.T) {
	t.Helper()

	test_sextet(t)
	test_basic_parse(t)
}

func test_sextet(t *testing.T) {
	t.Helper()

	for i := range 64 {
		assert.Equal(t, i, char_to_sextet(sextet_to_char(i)))
	}
}

func test_basic_parse(t *testing.T) {
	t.Helper()

	var ais = "!AIVDM,1,1,,A,15MgK45P3@G?fl0E`JbR0OwT0@MS,0*4E" // Example from https://www.aggsoft.com/ais-decoder.htm

	var aisData, status = ais_parse(ais, false)

	assert.Equal(t, 0, status)
	assert.Equal(t, "AIS 1: Position Report Class A", aisData.description)
	assert.Equal(t, "366730000", aisData.mssi)
	assert.Empty(t, aisData.comment)
	assert.InDelta(t, -122, aisData.lon, 1)
	assert.InDelta(t, 20.8, aisData.knots, 1)
	assert.InDelta(t, 51.3, aisData.course, 1)
}
