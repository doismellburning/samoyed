// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package deviceid

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestFromYAML(t *testing.T) {
	var d, err = FromYAML([]byte(`
tocalls:
 - tocall: APQ1??
   vendor: Q1TEST
   model: Widget
mice:
 - suffix: "_Q"
   vendor: Q1TEST
   model: Gadget
`))

	require.NoError(t, err)
	assert.Equal(t, maybe.Just("Q1TEST Widget"), d.FromDest("APQ123"))

	var trimmed, device = d.FromMicE("`Comment_Q")
	assert.Equal(t, "Comment", trimmed)
	assert.Equal(t, maybe.Just("Q1TEST Gadget"), device)
}

func TestFromYAMLMalformed(t *testing.T) {
	var d, err = FromYAML([]byte("tocalls: [unterminated"))

	require.Error(t, err)
	assert.Equal(t, maybe.Nothing[string](), d.FromDest("APQ123"), "a malformed file gives empty tables, not nil")
}
