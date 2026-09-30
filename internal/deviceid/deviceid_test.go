// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package deviceid

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// FromMicE needs only the MIC-E table, so a Data whose tocalls section is
// empty must still identify a MIC-E device.
func TestFromMicEWithoutTocalls(t *testing.T) {
	var m = new(mice)
	m.prefix = ">"
	m.vendor = "Kenwood"
	m.model = "TH-D7A"

	var d = new(Data)
	d.pmice = []*mice{m}

	var trimmed, device = d.FromMicE(">Comment")
	assert.Equal(t, "Comment", trimmed)
	assert.Equal(t, "Kenwood TH-D7A", device)
}
