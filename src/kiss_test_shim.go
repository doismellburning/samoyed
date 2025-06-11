package direwolf

//#include "direwolf.h"
//#include <stdio.h>
//#include <unistd.h>
//#include <stdlib.h>
//#include <ctype.h>
//#include <assert.h>
//#include <string.h>
//#include "ax25_pad.h"
//#include "textcolor.h"
//#include "kiss_frame.h"
//#include "tq.h"
//#include "xmit.h"
//#include "version.h"
//#include "kissnet.h"
import "C"

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

/* Quick unit test for encapsulate & unwrap */

func kiss_test_main(t *testing.T) {
	t.Helper()

	var din [512]C.uchar
	for k := range 512 {
		if k < 256 {
			din[k] = C.uchar(k)
		} else {
			din[k] = C.uchar(511 - k)
		}
	}

	var kissed [520]C.uchar
	var klen = C.kiss_encapsulate(&din[0], 512, &kissed[0])
	assert.Equal(t, C.int(512+6), klen)

	var dout [520]C.uchar
	var dlen = C.kiss_unwrap(&kissed[0], klen, &dout[0])
	assert.Equal(t, C.int(512), dlen)
	assert.Zero(t, C.memcmp(unsafe.Pointer(&din[0]), unsafe.Pointer(&dout[0]), 512))

	dlen = C.kiss_unwrap(&kissed[1], klen-1, &dout[0])
	assert.Equal(t, C.int(512), dlen)
	assert.Zero(t, C.memcmp(unsafe.Pointer(&din[0]), unsafe.Pointer(&dout[0]), 512))

	dw_printf("Quick KISS test passed OK.\n")
}
