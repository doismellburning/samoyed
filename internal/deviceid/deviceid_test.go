// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package deviceid

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus/hooks/test"
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
	assert.Equal(t, maybe.Just("Kenwood TH-D7A"), device)
}

// New reports a missing tocalls.yaml once; the lookups, which run for every
// packet heard, must not report it again each time.
func TestLookupsWithoutTablesDoNotLogPerPacket(t *testing.T) {
	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	var d = new(Data)

	assert.Equal(t, maybe.Nothing[string](), d.FromDest("APDW18"))

	var trimmed, device = d.FromMicE(">Comment")
	assert.Equal(t, ">Comment", trimmed)
	assert.Equal(t, maybe.Nothing[string](), device)

	assert.Empty(t, hook.AllEntries())
}
