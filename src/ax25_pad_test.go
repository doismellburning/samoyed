package direwolf

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ax25_unwrap_third_party(t *testing.T) {
	// Taken from the original comments for the function:
	// Example: Input:      A>B,C:}D>E,F:info
	// Output:     D>E,F:info
	// (Except because we're using AX25FormatAddrs, the info part isn't shown)
	var pp = AX25FromText("A>B,C:}D>E,F:info", true)
	var pp2 = ax25_unwrap_third_party(pp)
	var addrs = AX25FormatAddrs(pp2)
	assert.Equal(t, "D>E,F:", addrs)
}

func Test_ax25_set_info(t *testing.T) {
	var p = AX25FromText("D>E,F:info", true)
	var initialInfo = AX25GetInfo(p)
	assert.Equal(t, "info", string(initialInfo)) // Make sure I set this up right!

	var s = "badger"
	ax25_set_info(p, []byte(s))

	// Check info updated
	var newInfo = AX25GetInfo(p)
	assert.Equal(t, s, string(newInfo))

	// Make sure we didn't break stuff along the way
	assert.Equal(t, "D>E,F:", AX25FormatAddrs(p))
}

// Regression test for #604: ax25_new used to increment a plain package-level
// counter, which both raced under -race and could hand out duplicate sequence
// numbers when packets were allocated from several goroutines at once (the
// receive, IGate, AGW/KISS client and beacon threads all do).
func Test_ax25_new_concurrent_seq(t *testing.T) {
	const goroutines = 8
	const perGoroutine = 32

	var mu sync.Mutex
	var seqs = make(map[int]bool, goroutines*perGoroutine)

	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range perGoroutine {
				var pp = ax25_new()

				mu.Lock()
				assert.False(t, seqs[pp.seq], "duplicate sequence number %d", pp.seq)
				seqs[pp.seq] = true
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	assert.Len(t, seqs, goroutines*perGoroutine)
}
