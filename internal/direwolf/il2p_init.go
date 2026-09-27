//nolint:gochecknoglobals
package direwolf

import (
	"fmt"

	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/reedsolomon"
	"github.com/sirupsen/logrus"
)

const MAX_NROOTS = 16

const NTAB = 5

type TabType struct {
	symsize uint               // Symbol size, bits (1-8).  Always 8 for this application.
	genpoly uint               // Field generator polynomial coefficients.
	fcs     uint               // First root of RS code generator polynomial, index form. FX.25 uses 1 but IL2P uses 0.
	prim    uint               // Primitive element to generate polynomial roots.
	nroots  uint               // RS code generator polynomial degree (number of roots). Same as number of check bytes added.
	rs      *reedsolomon.Codec // RS codec control block.  Filled in at init time.
}

var Tab = [NTAB]TabType{
	{8, 0x11d, 0, 1, 2, nil},  // 2 parity
	{8, 0x11d, 0, 1, 4, nil},  // 4 parity
	{8, 0x11d, 0, 1, 6, nil},  // 6 parity
	{8, 0x11d, 0, 1, 8, nil},  // 8 parity
	{8, 0x11d, 0, 1, 16, nil}, // 16 parity
}

var g_il2p_debug = 0

/*-------------------------------------------------------------
 *
 * Name:	il2p_init
 *
 * Purpose:	This must be called at application start up time.
 *		It sets up tables for the Reed-Solomon functions.
 *
 * Inputs:	debug	- Enable debug output.
 *
 *--------------------------------------------------------------*/

func il2p_init(il2p_debug int) {
	g_il2p_debug = il2p_debug

	for i := range NTAB {
		dwutil.Assert(Tab[i].nroots <= MAX_NROOTS)

		var rs, err = reedsolomon.New(Tab[i].symsize, Tab[i].genpoly, Tab[i].fcs, Tab[i].prim, Tab[i].nroots)
		if err != nil {
			logrus.WithError(err).Fatal("IL2P internal error: Could not set up Reed-Solomon codec")
		}

		Tab[i].rs = rs
	}
}

func il2p_get_debug() int {
	return g_il2p_debug
}

// Find RS codec control block for specified number of parity symbols.

func il2p_find_rs(nparity int) (*reedsolomon.Codec, error) {
	for n := range NTAB {
		if Tab[n].nroots == uint(nparity) {
			if Tab[n].rs == nil {
				return nil, fmt.Errorf("RS control block for nparity = %d is not set up; il2p_init has not been called", nparity)
			}

			return Tab[n].rs, nil
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

	var rs_block [FX25_BLOCK_SIZE]byte
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
 *
 * Returns:	out		Original with possible corrections applied.
 *				data_size bytes.
 *
 * Returns:	-1 for unrecoverable.
 *		>= 0 for success.  Number of symbols corrected.
 *
 *--------------------------------------------------------------*/

func il2p_decode_rs(rec_block []byte, num_parity int) ([]byte, int) {
	var data_size = len(rec_block) - num_parity

	//  Use zero padding in front if data size is too small.

	var n = data_size + num_parity // total size in.

	var rs_block [FX25_BLOCK_SIZE]byte

	copy(rs_block[len(rs_block)-n:], rec_block)

	if il2p_get_debug() >= 3 {
		logrus.WithFields(logrus.Fields{
			"filler": len(rs_block) - n,
			"data":   data_size,
			"parity": num_parity,
		}).Debug("il2p_decode_rs")
		dwutil.HexDump(rs_block[:])
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

	if il2p_get_debug() >= 3 {
		if derrors == 0 {
			logrus.Debug("No errors reported for RS block")
		} else if derrors > 0 {
			logrus.WithField("positions", derrlocs).Debug("Errors fixed in RS block")
			dwutil.HexDump(rs_block[:])
		}
	}

	// It is possible to have a situation where too many errors are
	// present but the algorithm could get a good code block by "fixing"
	// one of the padding bytes that should be 0.

	for i := 0; i < derrors; i++ {
		if derrlocs[i] < len(rs_block)-n {
			if il2p_get_debug() >= 3 {
				logrus.WithFields(logrus.Fields{
					"position": derrlocs[i],
					"value":    fmt.Sprintf("%02x", rs_block[derrlocs[i]]),
				}).Debug("RS decode error: Padding position should be 0")
			}

			derrors = -1

			break
		}
	}

	if il2p_get_debug() >= 3 {
		logrus.WithField("derrors", derrors).Debug("il2p_decode_rs returns")
	}

	return out, derrors
}
