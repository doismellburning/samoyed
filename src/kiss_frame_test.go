package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Whatever KissEncapsulate escaped, KissUnescape should give back.
func Test_KissUnescape_RoundTrip(t *testing.T) {
	var original = []byte{0x00, 0x82, FEND, 0xa0, FESC, FEND, FESC, 0x41}

	var encapsulated = KissEncapsulate(original)

	require.Greater(t, len(encapsulated), len(original)+2) // Something was escaped.

	var unescaped, problems = KissUnescape(encapsulated[1 : len(encapsulated)-1])

	assert.Empty(t, problems)
	assert.Equal(t, original, unescaped)
}

func Test_KissUnescape_NothingEscaped(t *testing.T) {
	var unescaped, problems = KissUnescape([]byte{0x00, 0x82, 0xa0})

	assert.Empty(t, problems)
	assert.Equal(t, []byte{0x00, 0x82, 0xa0}, unescaped)
}

// An unexpected byte after FESC is taken literally so the rest of the frame
// can still be decoded.
func Test_KissUnescape_BadEscape(t *testing.T) {
	var unescaped, problems = KissUnescape([]byte{0x00, FESC, 0x41, 0x82})

	require.Len(t, problems, 1)
	assert.Contains(t, problems[0].Error(), "FESC (0xdb) at offset 1 is followed by 0x41")
	assert.Equal(t, []byte{0x00, 0x41, 0x82}, unescaped)
}

func Test_KissUnescape_TruncatedEscape(t *testing.T) {
	var unescaped, problems = KissUnescape([]byte{0x00, 0x82, FESC})

	require.Len(t, problems, 1)
	assert.Contains(t, problems[0].Error(), "frame ends with FESC (0xdb) at offset 2")
	assert.Equal(t, []byte{0x00, 0x82}, unescaped)
}

func Test_KissUnescape_Empty(t *testing.T) {
	var unescaped, problems = KissUnescape([]byte{})

	assert.Empty(t, problems)
	assert.Empty(t, unescaped)
}
