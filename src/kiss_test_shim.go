package direwolf

//#include <stdio.h>
//#include <unistd.h>
//#include <stdlib.h>
//#include <ctype.h>
//#include <assert.h>
//#include <string.h>
import "C"

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

/* Quick unit test for encapsulate & unwrap */

func kiss_test_main(t *testing.T) {
	t.Helper()

	var din = make([]byte, 512)
	for k := range 512 {
		if k < 256 {
			din[k] = byte(k)
		} else {
			din[k] = byte(511 - k)
		}
	}

	var kissed = kiss_encapsulate(din)
	assert.Len(t, kissed, (512 + 6))

	var dout = kiss_unwrap(kissed)
	assert.Len(t, dout, 512)
	assert.Equal(t, din, dout)

	dout = kiss_unwrap(kissed[1:])
	assert.Len(t, dout, 512)
	assert.Equal(t, din, dout)

	dw_printf("Quick KISS test passed OK.\n")
}
