package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ax25_unwrap_third_party(t *testing.T) {
	// Taken from the original comments for the function:
	// Example: Input:      A>B,C:}D>E,F:info
	// Output:     D>E,F:info
	// (Except because we're using ax25_format_addrs, the info part isn't shown)
	var pp = ax25_from_text("A>B,C:}D>E,F:info", true)
	var pp2 = ax25_unwrap_third_party(pp)
	var addrs = ax25_format_addrs(pp2)
	assert.Equal(t, "D>E,F:", addrs)
}

func Test_ax25_set_info(t *testing.T) {
	var p = ax25_from_text("D>E,F:info", true)
	var initialInfo = ax25_get_info(p)
	assert.Equal(t, "info", string(initialInfo)) // Make sure I set this up right!

	var s = "badger"
	ax25_set_info(p, []byte(s))

	// Check info updated
	var newInfo = ax25_get_info(p)
	assert.Equal(t, s, string(newInfo))

	// Make sure we didn't break stuff along the way
	assert.Equal(t, "D>E,F:", ax25_format_addrs(p))
}
