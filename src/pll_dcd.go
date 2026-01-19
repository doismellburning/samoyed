package direwolf

// #include "fsk_demod_state.h"
import "C"

import (
	"math/bits"
)

// Primarily taken from fsk_demod_state.h from Dire Wolf

/*-------------------------------------------------------------------
 *
 * Name:        pll_dcd_signal_transition2
 *		dcd_each_symbol2
 *
 * Purpose:     New DCD strategy for 1.6.
 *
 * Inputs:	D		Pointer to demodulator state.
 *
 *		chan		Radio channel: 0 to MAX_RADIO_CHANS - 1
 *
 *		subchan		Which of multiple demodulators: 0 to MAX_SUBCHANS - 1
 *
 *		slice		Slicer number: 0 to MAX_SLICERS - 1.
 *
 *		dpll_phase	Signed 32 bit counter for DPLL phase.
 *				Wraparound is where data is sampled.
 *				Ideally transitions would occur close to 0.
 *
 * Output:	D.slicer[slice].data_detect - true when PLL is locked to incoming signal.
 *
 * Description:	From the beginning, DCD was based on finding several flag octets
 *		in a row and dropping when eight bits with no transitions.
 *		It was less than ideal but we limped along with it all these years.
 *		This fell apart when FX.25 came along and a couple of the
 *		correlation tags have eight "1" bits in a row.
 *
 * 		Our new strategy is to keep a running score of how well demodulator
 *		output transitions match to where expected.
 *
 *--------------------------------------------------------------------*/

type DCDConfig struct {
	DCD_THRESH_ON C.int

	DCD_THRESH_OFF C.int

	// No more than 1024!!!
	DCD_GOOD_WIDTH C.int
}

// These values are good for 1200 bps AFSK.
// Might want to override for other modems.
func GenericDCDConfig() *DCDConfig {
	var c = new(DCDConfig)

	// Hysteresis: Can miss 2 out of 32 for detecting lock.
	// 31 is best for TNC Test CD.  30 almost as good.
	// 30 better for 1200 regression test.
	c.DCD_THRESH_ON = 30

	c.DCD_THRESH_OFF = 6 // Might want a little more fine tuning.

	c.DCD_GOOD_WIDTH = 512

	return c
}

func pll_dcd_signal_transition2(dcdConfig *DCDConfig, D *C.struct_demodulator_state_s, slice C.int, dpll_phase C.int) {
	if dpll_phase > -dcdConfig.DCD_GOOD_WIDTH*1024*1024 && dpll_phase < dcdConfig.DCD_GOOD_WIDTH*1024*1024 {
		D.slicer[slice].good_flag = 1
	} else {
		D.slicer[slice].bad_flag = 1
	}
}

func pll_dcd_each_symbol2(dcdConfig *DCDConfig, D *C.struct_demodulator_state_s, channel C.int, subchan C.int, slice C.int) {
	D.slicer[slice].good_hist <<= 1
	D.slicer[slice].good_hist |= C.uchar(D.slicer[slice].good_flag)
	D.slicer[slice].good_flag = 0

	D.slicer[slice].bad_hist <<= 1
	D.slicer[slice].bad_hist |= C.uchar(D.slicer[slice].bad_flag)
	D.slicer[slice].bad_flag = 0

	D.slicer[slice].score <<= 1
	// 2 is to detect 'flag' patterns with 2 transitions per octet.
	var goodBits = bits.OnesCount(uint(D.slicer[slice].good_hist))
	var badBits = bits.OnesCount(uint(D.slicer[slice].bad_hist))
	if goodBits-badBits >= 2 {
		D.slicer[slice].score |= 1
	}

	var s = C.int(bits.OnesCount(uint(D.slicer[slice].score)))
	if s >= dcdConfig.DCD_THRESH_ON {
		if D.slicer[slice].data_detect == 0 {
			D.slicer[slice].data_detect = 1
			dcd_change(channel, subchan, slice, D.slicer[slice].data_detect)
		}
	} else if s <= dcdConfig.DCD_THRESH_OFF {
		if D.slicer[slice].data_detect != 0 {
			D.slicer[slice].data_detect = 0
			dcd_change(channel, subchan, slice, D.slicer[slice].data_detect)
		}
	}
}
