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
)

const MAX_TOTAL_CHANS = 16 // v1.7 allows additional virtual channels which are connected
// to something other than radio modems.
// Total maximum channels is based on the 4 bit KISS field.
// Someone with very unusual requirements could increase this and
// use only the AGW network protocol.

/*
 * Maximum number of rigs.
 */

const MAX_RIGS = MAX_RADIO_CHANS
