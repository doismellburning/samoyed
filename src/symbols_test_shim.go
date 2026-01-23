package direwolf

import "C"

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

/* Quick, incomplete, unit test. */

func symbols_test_main(t *testing.T) {
	t.Helper()

	symbols_init()

	var symtab, symbol byte

	symtab, symbol, _ = symbols_from_dest_or_src('T', "W1ABC", "GPSC43")
	assert.Equal(t, byte('/'), symtab, "ERROR 1-1")
	assert.Equal(t, byte('K'), symbol, "ERROR 1-1")

	symtab, symbol, _ = symbols_from_dest_or_src('T', "W1ABC", "GPSE87")
	assert.Equal(t, byte('\\'), symtab, "ERROR 1-2")
	assert.Equal(t, byte('w'), symbol, "ERROR 1-2")

	symtab, symbol, _ = symbols_from_dest_or_src('T', "W1ABC", "SPCBL")
	assert.Equal(t, byte('/'), symtab, "ERROR 1-3")
	assert.Equal(t, byte('+'), symbol, "ERROR 1-3")

	symtab, symbol, _ = symbols_from_dest_or_src('T', "W1ABC", "SYMST")
	assert.Equal(t, byte('\\'), symtab, "ERROR 1-4")
	assert.Equal(t, byte('t'), symbol, "ERROR 1-4")

	symtab, symbol, _ = symbols_from_dest_or_src('T', "W1ABC", "GPSOD9")
	assert.Equal(t, byte('9'), symtab, "ERROR 1-5")
	assert.Equal(t, byte('#'), symbol, "ERROR 1-5")

	/*
		TODO 2025-07-18 KG Figure out correct behaviour

		https://github.com/wb2osz/direwolf/issues/580

		It looks like this might have bitrotted slightly.

		89021dd50c83a3b12b2d18b8ff8c502c3080232f ("Cleanups") changes the behaviour
		of symbols_from_dest_or_src in a way that breaks the below (instead
		treating it as another case where the outputs are left alone, so 9# per
		previous), but I'm not entirely clear which behaviour is actually correct.
		Deferring to what's actually implemented seems the most sensible though.

		--

		symbols_from_dest_or_src('T', "W1ABC-14", "XXXXXX", &symtab, &symbol)
		assert.Equal(t, C.char('/'), symtab, "ERROR 1-6")
		assert.Equal(t, C.char('k'), symbol, "ERROR 1-6")

		symbols_from_dest_or_src('T', "W1ABC", "GPS???", &symtab, &symbol)
		// Outputs are left alone if symbol can't be determined.
		assert.Equal(t, C.char('/'), symtab, "ERROR 1-7")
		assert.Equal(t, C.char('k'), symbol, "ERROR 1-7")
	*/

	var dest string

	dest, _ = symbols_into_dest('/', 'K')
	assert.Equal(t, "GPSC43", dest, "ERROR 2-1")

	dest, _ = symbols_into_dest(byte('\\'), 'w')
	assert.Equal(t, "GPSE87", dest, "ERROR 2-2")

	dest, _ = symbols_into_dest(byte('3'), 'A')
	assert.Equal(t, "GPSAA3", dest, "ERROR 2-3")

	// Expect to see this:
	//   Could not convert symbol " A" to GPSxyz destination format.
	//   Could not convert symbol "/ " to GPSxyz destination format.

	var ok bool

	dest, ok = symbols_into_dest(' ', 'A')
	assert.Equal(t, "GPS???", dest, "ERROR 2-4")
	assert.False(t, ok)

	dest, ok = symbols_into_dest('/', ' ')
	assert.Equal(t, "GPS???", dest, "ERROR 2-5")
	assert.False(t, ok)

	var description string

	description = symbols_get_description('J', 's')
	assert.Equal(t, "Jet Ski", description, "ERROR 3-1")

	description = symbols_get_description('/', 'O')
	assert.Equal(t, "Original Balloon (think Ham balloon)", description, "ERROR 3-2")

	description = symbols_get_description('\\', 'T')
	assert.Equal(t, "Thunderstorm", description, "ERROR 3-3")

	description = symbols_get_description('5', 'T')
	assert.Equal(t, "Thunderstorm w/overlay 5", description, "ERROR 3-4")

	// Expect to see this:
	//   Symbol table identifier is not '/' (primary), '\' (alternate), or valid overlay character.

	description = symbols_get_description(' ', 'T')
	assert.Equal(t, "--no-symbol--", description, "ERROR 3-5")

	description = symbols_get_description('/', ' ')
	assert.Equal(t, "--no-symbol--", description, "ERROR 3-6")

	symtab, symbol, _ = symbols_code_from_description('5', "girl scouts")
	assert.Equal(t, byte('5'), symtab, "ERROR 4-1")
	assert.Equal(t, byte(','), symbol, "ERROR 4-1")

	symtab, symbol, _ = symbols_code_from_description(' ', "scouts")
	assert.Equal(t, byte('/'), symtab, "ERROR 4-2")
	assert.Equal(t, byte(','), symbol, "ERROR 4-2")

	symtab, symbol, _ = symbols_code_from_description(' ', "girl scouts")
	assert.Equal(t, byte('\\'), symtab, "ERROR 4-3")
	assert.Equal(t, byte(','), symbol, "ERROR 4-3")

	symtab, symbol, _ = symbols_code_from_description(' ', "jet ski")
	assert.Equal(t, byte('J'), symtab, "ERROR 4-4")
	assert.Equal(t, byte('s'), symbol, "ERROR 4-4")

	symtab, symbol, _ = symbols_code_from_description(' ', "girl scouts")
	assert.Equal(t, byte('\\'), symtab, "ERROR 4-5")
	assert.Equal(t, byte(','), symbol, "ERROR 4-5")

	symtab, symbol, _ = symbols_code_from_description(' ', "yen")
	assert.Equal(t, byte('Y'), symtab, "ERROR 4-6")
	assert.Equal(t, byte('$'), symbol, "ERROR 4-6")

	symtab, symbol, _ = symbols_code_from_description(' ', "taco bell")
	assert.Equal(t, byte('T'), symtab, "ERROR 4-7")
	assert.Equal(t, byte('R'), symbol, "ERROR 4-7")
}
