package direwolf

import (
	"fmt"
)

// BitFixLevel represents the level of bit-error correction applied when
// recovering a frame with a bad CRC. It is used both as a configuration
// (how hard to try) and as a result (which technique succeeded).
type BitFixLevel int

const (
	BitFixNone     BitFixLevel = 0
	BitFixSingle   BitFixLevel = 1 // invert one bit
	BitFixDouble   BitFixLevel = 2 // invert two adjacent bits
	BitFixTriple   BitFixLevel = 3 // invert three adjacent bits
	BitFixTwoSep   BitFixLevel = 4 // invert two separate bits
	BitFixLevelMax BitFixLevel = 5
)

// retry_t is a legacy alias for BitFixLevel; callers should migrate to BitFixLevel.
type retry_t = BitFixLevel

// Legacy names kept for compatibility while callers are updated.
const (
	RETRY_NONE           = BitFixNone
	RETRY_INVERT_SINGLE  = BitFixSingle
	RETRY_INVERT_DOUBLE  = BitFixDouble
	RETRY_INVERT_TRIPLE  = BitFixTriple
	RETRY_INVERT_TWO_SEP = BitFixTwoSep
	RETRY_MAX            = BitFixLevelMax
)

func (bfl BitFixLevel) String() string {
	switch bfl {
	case BitFixNone:
		return "NONE"
	case BitFixSingle:
		return "SINGLE"
	case BitFixDouble:
		return "DOUBLE"
	case BitFixTriple:
		return "TRIPLE"
	case BitFixTwoSep:
		return "TWO_SEP"
	case BitFixLevelMax:
		return "PASSALL"
	}

	return fmt.Sprintf("(Unknown BitFixLevel %d)", bfl)
}
