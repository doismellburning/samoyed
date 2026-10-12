package direwolf

import (
	"github.com/doismellburning/samoyed/internal/phy"
)

// MAX_NET_CLIENTS is used for both KISS and AGWPE
const MAX_NET_CLIENTS = 3

// ACHAN2ADEV is `#define ACHAN2ADEV(n) ((n)>>1)`.
func ACHAN2ADEV(n int) int {
	return n >> 1
}

// ADEVFIRSTCHAN is `#define ADEVFIRSTCHAN(n) ((n) * 2)`, which lives in
// internal/phy as ADevFirstChan; this name is kept so call sites stay
// unchanged.
func ADEVFIRSTCHAN(n int) int {
	return phy.ADevFirstChan(n)
}
