// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package deviceid

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

func Test_DeviceID(t *testing.T) {
	var device, comment_out string

	var d = New()

	// MIC-E Legacy (really Kenwood).

	comment_out, device = d.FromMicE(">Comment")
	assert.Equal(t, "Comment", comment_out)
	assert.Equal(t, "Kenwood TH-D7A", device)

	comment_out, device = d.FromMicE(">Comment^")
	assert.Equal(t, "Comment", comment_out)
	assert.Equal(t, "Kenwood TH-D74", device)

	comment_out, device = d.FromMicE("]Comment")
	assert.Equal(t, "Comment", comment_out)
	assert.Equal(t, "Kenwood TM-D700", device)

	comment_out, device = d.FromMicE("]Comment=")
	assert.Equal(t, "Comment", comment_out)
	assert.Equal(t, "Kenwood TM-D710", device)

	comment_out, device = d.FromMicE("]\"4V}=")
	assert.Equal(t, "\"4V}", comment_out)
	assert.Equal(t, "Kenwood TM-D710", device)

	// Modern MIC-E.

	comment_out, device = d.FromMicE("`Comment_\"")
	assert.Equal(t, "Comment", comment_out)
	assert.Equal(t, "Yaesu FTM-350", device)

	comment_out, device = d.FromMicE("`Comment_ ")
	assert.Equal(t, "Comment", comment_out)
	assert.Equal(t, "Yaesu VX-8", device)

	comment_out, device = d.FromMicE("'Comment|3")
	assert.Equal(t, "Comment", comment_out)
	assert.Equal(t, "Byonics TinyTrak3", device)

	comment_out, device = d.FromMicE("Comment")
	assert.Equal(t, "Comment", comment_out)
	assert.Equal(t, "UNKNOWN vendor/model", device)

	comment_out, device = d.FromMicE("")
	assert.Empty(t, comment_out)
	assert.Equal(t, "UNKNOWN vendor/model", device)

	// Tocall

	device = d.FromDest("APDW18")
	assert.Equal(t, "WB2OSZ DireWolf", device)

	device = d.FromDest("APD123")
	assert.Equal(t, "Open Source aprsd", device)

	// null for Vendor.
	device = d.FromDest("APAX")
	assert.Equal(t, "AFilterX", device)

	device = d.FromDest("APA123")
	assert.Equal(t, "UNKNOWN vendor/model", device)
}
