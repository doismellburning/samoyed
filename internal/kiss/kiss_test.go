// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package kiss

import (
	"strings"
	"testing"

	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Whatever Encapsulate escaped, Unescape should give back.
func Test_Unescape_RoundTrip(t *testing.T) {
	var original = []byte{0x00, 0x82, FEND, 0xa0, FESC, FEND, FESC, 0x41}

	var encapsulated = Encapsulate(original)

	require.Greater(t, len(encapsulated), len(original)+2) // Something was escaped.

	var unescaped, problems = Unescape(encapsulated[1 : len(encapsulated)-1])

	assert.Empty(t, problems)
	assert.Equal(t, original, unescaped)
}

func Test_Unescape_NothingEscaped(t *testing.T) {
	var unescaped, problems = Unescape([]byte{0x00, 0x82, 0xa0})

	assert.Empty(t, problems)
	assert.Equal(t, []byte{0x00, 0x82, 0xa0}, unescaped)
}

// An unexpected byte after FESC is taken literally so the rest of the frame
// can still be decoded.
func Test_Unescape_BadEscape(t *testing.T) {
	var unescaped, problems = Unescape([]byte{0x00, FESC, 0x41, 0x82})

	require.Len(t, problems, 1)
	assert.Contains(t, problems[0].Error(), "FESC (0xdb) at offset 1 is followed by 0x41")
	assert.Equal(t, []byte{0x00, 0x41, 0x82}, unescaped)
}

func Test_Unescape_TruncatedEscape(t *testing.T) {
	var unescaped, problems = Unescape([]byte{0x00, 0x82, FESC})

	require.Len(t, problems, 1)
	assert.Contains(t, problems[0].Error(), "frame ends with FESC (0xdb) at offset 2")
	assert.Equal(t, []byte{0x00, 0x82}, unescaped)
}

func Test_Unescape_Empty(t *testing.T) {
	var unescaped, problems = Unescape([]byte{})

	assert.Empty(t, problems)
	assert.Empty(t, unescaped)
}

// Unwrap takes the escapes and framing back out, complaining about
// anything malformed but carrying on - a live TNC has to do something with
// what it was given.

func Test_Unwrap_too_short(t *testing.T) {
	var logs = logged(t, func() {
		assert.Empty(t, Unwrap([]byte{FEND}))
	})

	assert.Contains(t, logs, "less than minimum length")
}

func Test_Unwrap_no_trailing_fend(t *testing.T) {
	var unwrapped []byte

	var logs = logged(t, func() {
		unwrapped = Unwrap([]byte{FEND, 0x00, 'h', 'i'})
	})

	assert.Contains(t, logs, "should end with FEND")
	assert.Equal(t, []byte{0x00, 'h', 'i'}, unwrapped)
}

func Test_Unwrap_fend_in_the_middle(t *testing.T) {
	var logs = logged(t, func() {
		Unwrap([]byte{FEND, 0x00, FEND, 'h', FEND})
	})

	assert.Contains(t, logs, "should not have FEND in the middle")
}

// An escape followed by something that is not one of the two transposed bytes
// is a protocol error; the byte is dropped rather than guessed at.
func Test_Unwrap_bad_escape(t *testing.T) {
	var unwrapped []byte

	var logs = logged(t, func() {
		unwrapped = Unwrap([]byte{0x00, FESC, 'x', 'y', FEND})
	})

	assert.Contains(t, logs, "Found 0x78 after FESC")
	assert.Equal(t, []byte{0x00, 'y'}, unwrapped)
}

// The leading FEND is optional, so a frame without one unwraps the same way.
func Test_Unwrap_without_leading_fend(t *testing.T) {
	assert.Equal(t, []byte{0x00, 'h', 'i'}, Unwrap([]byte{0x00, 'h', 'i', FEND}))
}

// logged runs f and returns everything it logged, one entry per line.
func logged(t *testing.T, f func()) string {
	t.Helper()

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	f()

	var messages []string

	for _, entry := range hook.AllEntries() {
		messages = append(messages, entry.Message)
	}

	return strings.Join(messages, "\n")
}
