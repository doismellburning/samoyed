package direwolf

import (
	"github.com/doismellburning/samoyed/internal/phy"
)

// BitFixLevel and its levels now live in internal/phy; these names are kept
// so call sites, and the diff against Dire Wolf, stay small.
type BitFixLevel = phy.BitFixLevel

const (
	BitFixNone    = phy.BitFixNone
	BitFixSingle  = phy.BitFixSingle
	BitFixDouble  = phy.BitFixDouble
	BitFixTriple  = phy.BitFixTriple
	BitFixTwoSep  = phy.BitFixTwoSep
	BitFixPassall = phy.BitFixPassall
)

const BitFixLevelHighest = phy.BitFixLevelHighest

// Legacy names kept for compatibility while callers are updated.
const (
	RETRY_NONE           = BitFixNone
	RETRY_INVERT_SINGLE  = BitFixSingle
	RETRY_INVERT_DOUBLE  = BitFixDouble
	RETRY_INVERT_TRIPLE  = BitFixTriple
	RETRY_INVERT_TWO_SEP = BitFixTwoSep
)
