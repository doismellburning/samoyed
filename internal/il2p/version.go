// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package il2p

import "strings"

/*-------------------------------------------------------------
 *
 * Purpose:	Cope with the differences between IL2P v0.4 and v0.6.
 *
 * Reference:	https://tarpn.net/t/il2p/il2p-specification_draft_v0-6.pdf
 *
 * Description:	v0.4 uses bit 7 of header byte 0 as a "FEC Level", choosing
 *		between 16 parity symbols per payload block and a smaller
 *		number based on the block size.  v0.6 mandates 16 parity
 *		symbols per block and marks that bit RESERVED, so a v0.6
 *		frame looks to a v0.4 station like a request for the weaker
 *		FEC, and its payload blocks are then the wrong size.
 *
 *		Nothing in the frame distinguishes the two, so which is
 *		spoken is a per channel configuration choice.
 *
 *--------------------------------------------------------------*/

type Version int

const (
	// Version0_6 always uses 16 parity symbols per payload block and
	// sends that header bit, which it reserves, as 0.  The default, and what
	// current NinoTNC firmware, QtSM and MMDVM-TNC speak.
	Version0_6 Version = iota

	// Version0_4 reads and writes the header bit as the FEC Level.
	Version0_4

	// VersionCompat transmits v0.4 and receives v0.6.  With the
	// maximum FEC that IL2PTX selects by default, a v0.4 frame differs from
	// a v0.6 one only in that header bit, which v0.6 stations ignore, so it
	// is understood by both.
	VersionCompat
)

/*-------------------------------------------------------------
 *
 * Name:	ParseVersion
 *
 * Purpose:	Convert a version name, as written in a configuration file or
 *		on the command line, to a version.
 *
 * Inputs:	s	- "0.4", "0.6", or "compat".
 *
 * Returns:	The version, and false if the name was not recognised.
 *
 *--------------------------------------------------------------*/

func ParseVersion(s string) (Version, bool) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "0.4", "4":
		return Version0_4, true
	case "0.6", "6":
		return Version0_6, true
	case "COMPAT":
		return VersionCompat, true
	default:
		return Version0_6, false
	}
}

func (v Version) String() string {
	switch v {
	case Version0_4:
		return "0.4"
	case VersionCompat:
		return "compat"
	default:
		return "0.6"
	}
}

/*-------------------------------------------------------------
 *
 * Name:	il2p_tx_fec
 *
 * Purpose:	Decide how to encode a frame for the configured version.
 *
 * Inputs:	version	- IL2P version spoken on this channel.
 *		max_fec	- 1 for maximum FEC, 0 for automatic.  v0.4 only.
 *
 * Returns:	fec_level - Value for the header bit which is the FEC Level
 *			  in v0.4 and RESERVED in v0.6.
 *		use_max_fec - 1 for 16 parity symbols per payload block.
 *
 *--------------------------------------------------------------*/

func il2p_tx_fec(version Version, max_fec int) (int, int) {
	if version == Version0_6 {
		return 0, 1 // Bit is RESERVED, 16 parity symbols are mandatory.
	}

	return max_fec, max_fec // v0.4, which is what VersionCompat transmits.
}

/*-------------------------------------------------------------
 *
 * Name:	rxMaxFEC
 *
 * Purpose:	Interpret the header bit which is the FEC Level in v0.4 and
 *		RESERVED in v0.6.
 *
 * Inputs:	version		- IL2P version spoken on this channel.
 *		fec_level	- The bit, as received.
 *
 * Returns:	1 if payload blocks carry 16 parity symbols, 0 if the number
 *		depends on the block size.
 *
 *--------------------------------------------------------------*/

func rxMaxFEC(version Version, fec_level int) int {
	if version == Version0_4 {
		return fec_level
	}

	return 1 // v0.6 mandates 16 parity symbols per payload block.
}
