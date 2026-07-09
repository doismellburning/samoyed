package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

/* Quick unit test for encapsulate & unwrap */

func Test_KISS(t *testing.T) {
	var din = make([]byte, 512)

	for k := range 512 {
		if k < 256 {
			din[k] = byte(k)
		} else {
			din[k] = byte(511 - k)
		}
	}

	var kissed = KissEncapsulate(din)
	assert.Len(t, kissed, (512 + 6))

	var dout = kiss_unwrap(kissed)
	assert.Len(t, dout, 512)
	assert.Equal(t, din, dout)

	dout = kiss_unwrap(kissed[1:])
	assert.Len(t, dout, 512)
	assert.Equal(t, din, dout)

	dw_printf("Quick KISS test passed OK.\n")
}
