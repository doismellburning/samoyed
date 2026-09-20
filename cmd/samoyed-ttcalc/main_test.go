package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_calculator(t *testing.T) {
	assert.Equal(t, 46, calculator("12a34#"))
	assert.Equal(t, 10, calculator("2*3A4#"))
	assert.Equal(t, 503, calculator("5*100A3#"))
	assert.Equal(t, 50, calculator("6a4*5#"))
}

// Regression test for #671: the sequence arrives over the air, so it need not
// end with the "#" that terminates a well-formed one.  It used to fall out of
// the loop and panic.
func Test_calculator_unterminated(t *testing.T) {
	assert.Equal(t, 46, calculator("12a34"))
	assert.Equal(t, 10, calculator("2*3A4"))
	assert.Equal(t, 12, calculator("12"))
	assert.Equal(t, 0, calculator(""))
	assert.Equal(t, 0, calculator("#"))
}
