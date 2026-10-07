// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

//nolint:gochecknoglobals
package il2p

import (
	"fmt"

	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/reedsolomon"
	"github.com/sirupsen/logrus"
)

const MAX_NROOTS = 16

const NTAB = 5

// il2pTabEntry is one of the Reed-Solomon codes IL2P uses, and its codec.
type il2pTabEntry struct {
	symsize uint               // Symbol size, bits (1-8).  Always 8 for this application.
	genpoly uint               // Field generator polynomial coefficients.
	fcs     uint               // First root of RS code generator polynomial, index form. FX.25 uses 1 but IL2P uses 0.
	prim    uint               // Primitive element to generate polynomial roots.
	nroots  uint               // RS code generator polynomial degree (number of roots). Same as number of check bytes added.
	rs      *reedsolomon.Codec // RS codec control block.
}

// il2pTab is built once, when the package is initialised, and only read after
// that, so every IL2P sender and receiver can share it without a lock.
var il2pTab = [NTAB]il2pTabEntry{
	newIL2PTabEntry(2),  // 2 parity
	newIL2PTabEntry(4),  // 4 parity
	newIL2PTabEntry(6),  // 6 parity
	newIL2PTabEntry(8),  // 8 parity
	newIL2PTabEntry(16), // 16 parity
}

// newIL2PTabEntry sets up the codec for IL2P's Reed-Solomon code with nroots
// parity symbols.  IL2P's codes differ only in that, so the rest are fixed
// here.  The parameters are all constants, so a failure is a bug, not
// something a user can do anything about.
func newIL2PTabEntry(nroots uint) il2pTabEntry {
	dwutil.Assert(nroots <= MAX_NROOTS)

	const symsize, genpoly, fcs, prim = 8, 0x11d, 0, 1

	var rs, err = reedsolomon.New(symsize, genpoly, fcs, prim, nroots)
	if err != nil {
		logrus.WithError(err).Fatal("IL2P internal error: Could not set up Reed-Solomon codec")
	}

	return il2pTabEntry{symsize: symsize, genpoly: genpoly, fcs: fcs, prim: prim, nroots: nroots, rs: rs}
}

// Find RS codec control block for specified number of parity symbols.

func il2p_find_rs(nparity int) (*reedsolomon.Codec, error) {
	for n := range NTAB {
		if il2pTab[n].nroots == uint(nparity) {
			return il2pTab[n].rs, nil
		}
	}

	return nil, fmt.Errorf("no RS control block for nparity = %d", nparity)
}

/*-------------------------------------------------------------
 *
 * Name:	il2p_encode_rs
 *
 * Purpose:	Add parity symbols to a block of data.
 *
 * Inputs:	tx_data		Header or other data to transmit.
 *		data_size	Number of data bytes in above.
 *		num_parity	Number of parity symbols to add.
 *				Maximum of IL2P_MAX_PARITY_SYMBOLS.
 *
 * Outputs:	parity_out	Specified number of parity symbols
 *
 * Restriction:	data_size + num_parity <= 255 which is the RS block size.
 *		The caller must ensure this.
 *
 *--------------------------------------------------------------*/

func il2p_encode_rs(tx_data []byte, num_parity int) ([]byte, error) {
	var data_size = len(tx_data)

	dwutil.Assert(data_size >= 1)

	dwutil.Assert(num_parity == 2 || num_parity == 4 || num_parity == 6 || num_parity == 8 || num_parity == 16)
	dwutil.Assert(data_size+num_parity <= 255)

	var rs, err = il2p_find_rs(num_parity)
	if err != nil {
		return nil, err
	}

	var rs_block [IL2P_RS_BLOCK_SIZE]byte
	copy(rs_block[len(rs_block)-data_size-num_parity:], tx_data)

	return rs.Encode(rs_block[:len(rs_block)-num_parity]), nil
}

/*-------------------------------------------------------------
 *
 * Name:	il2p_decode_rs
 *
 * Purpose:	Check and attempt to fix block with FEC.
 *
 * Inputs:	rec_block	Received block composed of data and parity.
 *				Total size is sum of following two parameters.
 *		rec_block	data_size + num_parity bytes.
 *		num_parity	Number of parity symbols (bytes) in above.
 *		debug		IL2P's debug level.
 *
 * Returns:	out		Original with possible corrections applied.
 *				data_size bytes.
 *
 * Returns:	-1 for unrecoverable.
 *		>= 0 for success.  Number of symbols corrected.
 *
 *--------------------------------------------------------------*/

func il2p_decode_rs(rec_block []byte, num_parity int, debug int) ([]byte, int) {
	var data_size = len(rec_block) - num_parity

	//  Use zero padding in front if data size is too small.

	var n = data_size + num_parity // total size in.

	var rs_block [IL2P_RS_BLOCK_SIZE]byte

	copy(rs_block[len(rs_block)-n:], rec_block)

	if debug >= 3 {
		var logEntry = logrus.WithFields(logrus.Fields{
			"filler": len(rs_block) - n,
			"data":   data_size,
			"parity": num_parity,
		})
		logEntry.Debug("il2p_decode_rs")
		dwutil.LogHexDump(logEntry, logrus.DebugLevel, rs_block[:])
	}

	var rs, err = il2p_find_rs(num_parity)
	if err != nil {
		logrus.WithError(err).Error("Cannot check an IL2P block")

		return make([]byte, data_size), -1
	}

	var derrlocs, decodeErr = rs.Decode(rs_block[:], nil)

	var derrors = len(derrlocs)
	if decodeErr != nil {
		derrors = -1
	}
	var out = make([]byte, data_size)
	copy(out, rs_block[len(rs_block)-n:len(rs_block)-n+data_size])

	if debug >= 3 && derrors >= 0 {
		var logEntry = logrus.WithFields(logrus.Fields{
			"errors":    derrors,
			"positions": derrlocs,
		})
		logEntry.Debug("il2p_decode_rs: RS block errors fixed")

		if derrors > 0 {
			dwutil.LogHexDump(logEntry, logrus.DebugLevel, rs_block[:])
		}
	}

	// It is possible to have a situation where too many errors are
	// present but the algorithm could get a good code block by "fixing"
	// one of the padding bytes that should be 0.

	for i := 0; i < derrors; i++ {
		if derrlocs[i] < len(rs_block)-n {
			if debug >= 3 {
				logrus.WithFields(logrus.Fields{
					"position": derrlocs[i],
					"value":    fmt.Sprintf("0x%02x", rs_block[derrlocs[i]]),
				}).Debug("il2p_decode_rs: RS DECODE ERROR!  Padding position should be 0")
			}

			derrors = -1

			break
		}
	}

	if debug >= 3 {
		logrus.WithField("derrors", derrors).Debug("il2p_decode_rs returns")
	}

	return out, derrors
}
