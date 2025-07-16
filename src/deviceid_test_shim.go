package direwolf

// #include "deviceid.h"
import "C"

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

/*------------------------------------------------------------------
 *
 * Purpose:	A little self-test used during development.
 *
 * Description:	Read the yaml file.  Decipher a few typical values.
 *
 *------------------------------------------------------------------*/

func deviceid_test_main(t *testing.T) {
	t.Helper()

	var device [80]C.char
	var comment_out [80]C.char

	C.deviceid_init()

	// MIC-E Legacy (really Kenwood).

	deviceid_decode_mice(">Comment", &comment_out[0], len(comment_out), &device[0], len(device))
	fmt.Printf("%s %s\n", C.GoString(&comment_out[0]), C.GoString(&device[0]))
	assert.Equal(t, C.GoString(&comment_out[0]), "Comment")
	assert.Equal(t, C.GoString(&device[0]), "Kenwood TH-D7A")

	deviceid_decode_mice(">Comment^", &comment_out[0], len(comment_out), &device[0], len(device))
	fmt.Printf("%s %s\n", C.GoString(&comment_out[0]), C.GoString(&device[0]))
	assert.Equal(t, C.GoString(&comment_out[0]), "Comment")
	assert.Equal(t, C.GoString(&device[0]), "Kenwood TH-D74")

	deviceid_decode_mice("]Comment", &comment_out[0], len(comment_out), &device[0], len(device))
	fmt.Printf("%s %s\n", C.GoString(&comment_out[0]), C.GoString(&device[0]))
	assert.Equal(t, C.GoString(&comment_out[0]), "Comment")
	assert.Equal(t, C.GoString(&device[0]), "Kenwood TM-D700")

	deviceid_decode_mice("]Comment=", &comment_out[0], len(comment_out), &device[0], len(device))
	fmt.Printf("%s %s\n", C.GoString(&comment_out[0]), C.GoString(&device[0]))
	assert.Equal(t, C.GoString(&comment_out[0]), "Comment")
	assert.Equal(t, C.GoString(&device[0]), "Kenwood TM-D710")

	deviceid_decode_mice("]\"4V}=", &comment_out[0], len(comment_out), &device[0], len(device))
	fmt.Printf("%s %s\n", C.GoString(&comment_out[0]), C.GoString(&device[0]))
	assert.Equal(t, C.GoString(&comment_out[0]), "\"4V}")
	assert.Equal(t, C.GoString(&device[0]), "Kenwood TM-D710")

	// Modern MIC-E.

	deviceid_decode_mice("`Comment_\"", &comment_out[0], len(comment_out), &device[0], len(device))
	fmt.Printf("%s %s\n", C.GoString(&comment_out[0]), C.GoString(&device[0]))
	assert.Equal(t, C.GoString(&comment_out[0]), "Comment")
	assert.Equal(t, C.GoString(&device[0]), "Yaesu FTM-350")

	deviceid_decode_mice("`Comment_ ", &comment_out[0], len(comment_out), &device[0], len(device))
	fmt.Printf("%s %s\n", C.GoString(&comment_out[0]), C.GoString(&device[0]))
	assert.Equal(t, C.GoString(&comment_out[0]), "Comment")
	assert.Equal(t, C.GoString(&device[0]), "Yaesu VX-8")

	deviceid_decode_mice("'Comment|3", &comment_out[0], len(comment_out), &device[0], len(device))
	fmt.Printf("%s %s\n", C.GoString(&comment_out[0]), C.GoString(&device[0]))
	assert.Equal(t, C.GoString(&comment_out[0]), "Comment")
	assert.Equal(t, C.GoString(&device[0]), "Byonics TinyTrak3")

	deviceid_decode_mice("Comment", &comment_out[0], len(comment_out), &device[0], len(device))
	fmt.Printf("%s %s\n", C.GoString(&comment_out[0]), C.GoString(&device[0]))
	assert.Equal(t, C.GoString(&comment_out[0]), "Comment")
	assert.Equal(t, C.GoString(&device[0]), "UNKNOWN vendor/model")

	deviceid_decode_mice("", &comment_out[0], len(comment_out), &device[0], len(device))
	fmt.Printf("%s %s\n", C.GoString(&comment_out[0]), C.GoString(&device[0]))
	assert.Equal(t, C.GoString(&comment_out[0]), "")
	assert.Equal(t, C.GoString(&device[0]), "UNKNOWN vendor/model")

	// Tocall

	deviceid_decode_dest("APDW18", &device[0], len(device))
	fmt.Printf("%s\n", C.GoString(&device[0]))
	assert.Equal(t, C.GoString(&device[0]), "WB2OSZ DireWolf")

	deviceid_decode_dest("APD123", &device[0], len(device))
	fmt.Printf("%s\n", C.GoString(&device[0]))
	assert.Equal(t, C.GoString(&device[0]), "Open Source aprsd")

	// null for Vendor.
	deviceid_decode_dest("APAX", &device[0], len(device))
	fmt.Printf("%s\n", C.GoString(&device[0]))
	assert.Equal(t, C.GoString(&device[0]), "AFilterX")

	deviceid_decode_dest("APA123", &device[0], len(device))
	fmt.Printf("%s\n", C.GoString(&device[0]))
	assert.Equal(t, C.GoString(&device[0]), "UNKNOWN vendor/model")
}

func deviceid_decode_mice(comment string, trimmed *C.char, trimmed_size int, device *C.char, device_size int) {
	C.deviceid_decode_mice(C.CString(comment), trimmed, C.size_t(trimmed_size), device, C.size_t(device_size))
}

func deviceid_decode_dest(dest string, device *C.char, device_size int) {
	C.deviceid_decode_dest(C.CString(dest), device, C.size_t(device_size))
}
