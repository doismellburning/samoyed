package direwolf

import (
	"github.com/doismellburning/samoyed/internal/phy"
)

// Config from direwolf.h - probably belongs elsewhere

// The limits the modems and framers share live in internal/phy, which
// explains them; these names are kept so call sites stay unchanged.
const (
	MAX_ADEVS       = phy.MaxADevs
	MAX_RADIO_CHANS = phy.MaxRadioChans
	MAX_SUBCHANS    = phy.MaxSubchans
	MAX_SLICERS     = phy.MaxSlicers
	MAX_TOTAL_CHANS = phy.MaxTotalChans
)

/*
 * Maximum number of rigs.
 */

const MAX_RIGS = MAX_RADIO_CHANS
