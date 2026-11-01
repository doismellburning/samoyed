// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sid

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The other BBSes' SIDs here are as their documentation gives them, not
// captured from the air (#1152).

func TestParse(t *testing.T) {
	t.Parallel()

	var tests = []struct {
		line string
		want SID
	}{
		{"[FBB-7.0-ABFHM$]", SID{Name: "FBB", Version: "7.0", Flags: "ABFHM$"}},
		{"[BPQ-6.0.24.1-B1FIHJM$]", SID{Name: "BPQ", Version: "6.0.24.1", Flags: "B1FIHJM$"}},
		{"[MBL-5.14-H$]", SID{Name: "MBL", Version: "5.14", Flags: "H$"}},
		{"[SAMBBS-0.0-H$]", SID{Name: "SAMBBS", Version: "0.0", Flags: "H$"}},
		{"[X--]", SID{Name: "X", Version: "", Flags: ""}},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()

			var got, err = Parse(tt.line)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.line, got.String())
			assert.True(t, IsSID(tt.line))
		})
	}
}

func TestParseRefuses(t *testing.T) {
	t.Parallel()

	for _, line := range []string{"", "L", "[FBB]", "FBB-7.0-ABFHM$", "[FBB-7.0-ABFHM$] extra", "[a b-1-$]", "[FBB-7.0-AB-C]"} {
		var _, err = Parse(line)

		require.ErrorIs(t, err, ErrNotSID, line)
		assert.False(t, IsSID(line), line)
	}
}

func TestHas(t *testing.T) {
	t.Parallel()

	var fbb, _ = Parse("[FBB-7.0-abfhm$]")

	assert.True(t, fbb.Has(FlagFBB))
	assert.True(t, fbb.Has(FlagHierarchical))
	assert.True(t, fbb.Has(FlagBID))
	assert.False(t, fbb.Has('X'))
}

func TestOurs(t *testing.T) {
	t.Parallel()

	var ours = Ours(1, 2, true)

	assert.Equal(t, "[SAMBBS-1.2-FH$]", ours.String())
	assert.True(t, ours.Has(FlagFBB))

	var mblOnly = Ours(1, 2, false)

	assert.Equal(t, "[SAMBBS-1.2-H$]", mblOnly.String())
	assert.False(t, mblOnly.Has(FlagFBB))
}

func TestNegotiate(t *testing.T) {
	t.Parallel()

	var withBIDs, _ = Parse("[FBB-7.0-ABFHM$]")
	var without, _ = Parse("[OLD-1.0-H]")

	assert.Equal(t, Mode{BIDs: true, FBB: true}, Negotiate(Ours(0, 0, true), withBIDs))
	assert.Equal(t, Mode{BIDs: false, FBB: false}, Negotiate(Ours(0, 0, true), without), "they don't claim F")
	assert.Equal(t, Mode{BIDs: true, FBB: false}, Negotiate(Ours(0, 0, false), withBIDs), "this BBS doesn't")
}

func FuzzParse(f *testing.F) {
	for _, seed := range []string{"[FBB-7.0-ABFHM$]", "[BPQ-6.0.24.1-B1FIHJM$]", "[X--]", "[", "]", ""} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, line string) {
		var s, err = Parse(line)
		if err != nil {
			assert.False(t, IsSID(line))

			return
		}

		assert.True(t, IsSID(line))
		assert.Equal(t, line, s.String(), "a SID parsed is the SID written")
	})
}
