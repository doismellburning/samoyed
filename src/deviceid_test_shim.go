package direwolf

import (
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

	var device, comment_out string

	deviceid_init()

	// MIC-E Legacy (really Kenwood).

	comment_out, device = deviceid_decode_mice(">Comment")
	assert.Equal(t, "Comment", comment_out)
	assert.Equal(t, "Kenwood TH-D7A", device)

	comment_out, device = deviceid_decode_mice(">Comment^")
	assert.Equal(t, "Comment", comment_out)
	assert.Equal(t, "Kenwood TH-D74", device)

	comment_out, device = deviceid_decode_mice("]Comment")
	assert.Equal(t, "Comment", comment_out)
	assert.Equal(t, "Kenwood TM-D700", device)

	comment_out, device = deviceid_decode_mice("]Comment=")
	assert.Equal(t, "Comment", comment_out)
	assert.Equal(t, "Kenwood TM-D710", device)

	comment_out, device = deviceid_decode_mice("]\"4V}=")
	assert.Equal(t, "\"4V}", comment_out)
	assert.Equal(t, "Kenwood TM-D710", device)

	// Modern MIC-E.

	comment_out, device = deviceid_decode_mice("`Comment_\"")
	assert.Equal(t, "Comment", comment_out)
	assert.Equal(t, "Yaesu FTM-350", device)

	comment_out, device = deviceid_decode_mice("`Comment_ ")
	assert.Equal(t, "Comment", comment_out)
	assert.Equal(t, "Yaesu VX-8", device)

	comment_out, device = deviceid_decode_mice("'Comment|3")
	assert.Equal(t, "Comment", comment_out)
	assert.Equal(t, "Byonics TinyTrak3", device)

	comment_out, device = deviceid_decode_mice("Comment")
	assert.Equal(t, "Comment", comment_out)
	assert.Equal(t, "UNKNOWN vendor/model", device)

	comment_out, device = deviceid_decode_mice("")
	assert.Empty(t, comment_out)
	assert.Equal(t, "UNKNOWN vendor/model", device)

	// Tocall

	device = deviceid_decode_dest("APDW18")
	assert.Equal(t, "WB2OSZ DireWolf", device)

	device = deviceid_decode_dest("APD123")
	assert.Equal(t, "Open Source aprsd", device)

	// null for Vendor.
	device = deviceid_decode_dest("APAX")
	assert.Equal(t, "AFilterX", device)

	device = deviceid_decode_dest("APA123")
	assert.Equal(t, "UNKNOWN vendor/model", device)
}
