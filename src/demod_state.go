package direwolf

// #include <stdint.h>          // int64_t
// #include "audio.h"		// for enum modem_t
import "C"

/*
 * Demodulator state.
 * Different copy is required for each channel & subchannel being processed concurrently.
 */

// TODO1.2:  change prefix from BP_ to DSP_

type bp_window_t int

const (
	BP_WINDOW_TRUNCATED bp_window_t = iota
	BP_WINDOW_COSINE
	BP_WINDOW_HAMMING
	BP_WINDOW_BLACKMAN
	BP_WINDOW_FLATTOP
)

// Experimental low pass filter to detect DC bias or low frequency changes.
// IIR behaves like an analog R-C filter.
// Intuitively, it seems like FIR would be better because it is based on a finite history.
// However, it would require MANY taps and a LOT of computation for a low frequency.
// We can use a little trick here to keep a running average.
// This would be equivalent to convolving with an array of all 1 values.
// That would eliminate the need to multiply.
// We can also eliminate the need to add them all up each time by keeping a running total.
// Add a sample to the total when putting it in our array of recent samples.
// Subtract it from the total when it gets pushed off the end.
// We can also eliminate the need to shift them all down by using a circular buffer.

const CIC_LEN_MAX = 4000

type cic_t struct {
	len C.int // Number of elements used.
	// Might want to dynamically allocate.
	in    [CIC_LEN_MAX]C.short // Samples coming in.
	sum   C.int                // Running sum.
	inext C.int                // Next position to fill.
}

const MAX_FILTER_SIZE = 480 /* 401 is needed for profile A, 300 baud & 44100. Revisit someday. */
// Size comes out to 417 for 1200 bps with 48000 sample rate
// v1.7 - Was 404.  Bump up to 480.

const TICKS_PER_PLL_CYCLE = (256.0 * 256.0 * 256.0 * 256.0)

type demodulator_state_s struct {
	/*
	 * These are set once during initialization.
	 */
	modem_type C.enum_modem_t // MODEM_AFSK, MODEM_8PSK, etc.

	//	enum v26_e v26_alt;			// Which alternative when V.26.

	profile C.char // 'A', 'B', etc.	Upper case.
	// Only needed to see if we are using 'F' to take fast path.

	pll_step_per_sample C.int // PLL is advanced by this much each audio sample.
	// Data is sampled when it overflows.

	/*
	 * Window type for the various filters.
	 */

	lp_window bp_window_t

	/*
	 * Alternate Low pass filters.
	 * First is arbitrary number for quick IIR.
	 * Second is frequency as ratio to baud rate for FIR.
	 */
	lpf_use_fir C.int /* 0 for IIR, 1 for FIR. */

	lpf_iir C.float /* Only if using IIR. */

	lpf_baud C.float /* Cutoff frequency as fraction of baud. */
	/* Intuitively we'd expect this to be somewhere */
	/* in the range of 0.5 to 1. */
	/* In practice, it turned out a little larger */
	/* for profiles B, C, D. */

	lp_filter_width_sym C.float /* Length in number of symbol times. */

	// TODO KG #define lp_filter_len_bits lp_filter_width_sym	// FIXME: temp hack

	lp_filter_taps C.int /* Size of Low Pass filter, in audio samples. */

	// TODO KG #define lp_filter_size lp_filter_taps		// FIXME: temp hack

	/*
	 * Automatic gain control.  Fast attack and slow decay factors.
	 */
	agc_fast_attack C.float
	agc_slow_decay  C.float

	/*
	 * Use a longer term view for reporting signal levels.
	 */
	quick_attack   C.float
	sluggish_decay C.float

	/*
	 * Hysteresis before final demodulator 0 / 1 decision.
	 */
	hysteresis  C.float
	num_slicers C.int /* >1 for multiple slicers. */

	/*
	 * Phase Locked Loop (PLL) inertia.
	 * Larger number means less influence by signal transitions.
	 * It is more resistant to change when locked on to a signal.
	 */
	pll_locked_inertia    C.float
	pll_searching_inertia C.float

	/*
	 * Optional band pass pre-filter before mark/space detector.
	 */
	use_prefilter C.int /* True to enable it. */

	prefilter_baud C.float /* Cutoff frequencies, as fraction of */
	/* baud rate, beyond tones used.  */
	/* Example, if we used 1600/1800 tones at */
	/* 300 baud, and this was 0.5, the cutoff */
	/* frequencies would be: */
	/* lower = min(1600,1800) - 0.5 * 300 = 1450 */
	/* upper = max(1600,1800) + 0.5 * 300 = 1950 */

	pre_filter_len_sym C.float // Length in number of symbol times.
	// TODO KG #define pre_filter_len_bits pre_filter_len_sym 		// temp until all references changed.

	pre_window bp_window_t // Window type for filter shaping.

	pre_filter_taps C.int // Calculated number of filter taps.
	// TODO KG #define pre_filter_size pre_filter_taps		// temp until all references changed.

	pre_filter [MAX_FILTER_SIZE]C.float

	raw_cb [MAX_FILTER_SIZE]C.float // audio in,  need better name.

	/*
	 * The rest are continuously updated.
	 */

	lo_phase C.uint /* Local oscillator for PSK. */

	/*
	 * Use half of the AGC code to get a measure of input audio amplitude.
	 * These use "quick" attack and "sluggish" decay while the
	 * AGC uses "fast" attack and "slow" decay.
	 */

	alevel_rec_peak   C.float
	alevel_rec_valley C.float
	alevel_mark_peak  C.float
	alevel_space_peak C.float

	/*
	 * Outputs from the mark and space amplitude detection,
	 * used as inputs to the FIR lowpass filters.
	 * Kernel for the lowpass filters.
	 */

	lp_filter [MAX_FILTER_SIZE]C.float

	m_peak, s_peak         C.float
	m_valley, s_valley     C.float
	m_amp_prev, s_amp_prev C.float

	/*
	 * For the PLL and data bit timing.
	 * starting in version 1.2 we can have multiple slicers for one demodulator.
	 * Each slicer has its own PLL and HDLC decoder.
	 */

	/*
	 * Version 1.3: Clean up subchan vs. slicer.
	 *
	 * Originally some number of CHANNELS (originally 2, later 6)
	 * which can have multiple parallel demodulators called SUB-CHANNELS.
	 * This was originally for staggered frequencies for HF SSB.
	 * It can also be used for multiple demodulators with the same
	 * frequency but other differing parameters.
	 * Each subchannel has its own demodulator and HDLC decoder.
	 *
	 * In version 1.2 we added multiple SLICERS.
	 * The data structure, here, has multiple slicers per
	 * demodulator (subchannel).  Due to fuzzy thinking or
	 * expediency, the multiple slicers got mapped into subchannels.
	 * This means we can't use both multiple decoders and
	 * multiple slicers at the same time.
	 *
	 * Clean this up in 1.3 and keep the concepts separate.
	 * This means adding a third variable many places
	 * we are passing around the origin.
	 *
	 */
	slicer [MAX_SLICERS]struct {
		data_clock_pll C.int // PLL for data clock recovery.
		// It is incremented by pll_step_per_sample
		// for each audio sample.
		// Must be 32 bits!!!
		// So far, this is the case for every compiler used.

		prev_d_c_pll C.int // Previous value of above, before
		// incrementing, to detect overflows.

		pll_symbol_count C.int     // Number symbols during time nudge_total is accumulated.
		pll_nudge_total  C.int64_t // Sum of DPLL nudge amounts.
		// Both of these are cleared at start of frame.
		// At end of frame, we can see if incoming
		// baud rate is a little off.

		prev_demod_data C.int // Previous data bit detected.
		// Used to look for transitions.
		prev_demod_out_f C.float

		/* This is used only for "9600" baud data. */

		lfsr C.int // Descrambler shift register.

		// This is for detecting phase lock to incoming signal.

		good_flag C.int // Set if transition is near where expected,
		// i.e. at a good time.
		bad_flag C.int // Set if transition is not where expected,
		// i.e. at a bad time.
		good_hist C.uchar // History of good transitions for past octet.
		bad_hist  C.uchar // History of bad transitions for past octet.
		score     C.uint  // History of whether good triumphs over bad
		// for past 32 symbols.
		data_detect C.int // True when locked on to signal.

	} // Actual number in use is num_slicers.
	// Should be in range 1 .. MAX_SLICERS,
	/*
	 * Version 1.6:
	 *
	 *	This has become quite disorganized and messy with different combinations of
	 *	fields used for different demodulator types.  Start to reorganize it into a common
	 *	part (with things like the DPLL for clock recovery), and separate sections
	 *	for each of the demodulator types.
	 *	Still a lot to do here.
	 */

	u struct {

		//////////////////////////////////////////////////////////////////////////////////
		//										//
		//			AFSK only - new method in 1.7				//
		//										//
		//////////////////////////////////////////////////////////////////////////////////

		afsk struct {
			m_osc_phase C.uint // Phase for Mark local oscillator.
			m_osc_delta C.uint // How much to change for each audio sample.

			s_osc_phase C.uint // Phase for Space local oscillator.
			s_osc_delta C.uint // How much to change for each audio sample.

			c_osc_phase C.uint // Phase for Center frequency local oscillator.
			c_osc_delta C.uint // How much to change for each audio sample.

			// Need two mixers for profile "A".

			m_I_raw [MAX_FILTER_SIZE]C.float
			m_Q_raw [MAX_FILTER_SIZE]C.float

			s_I_raw [MAX_FILTER_SIZE]C.float
			s_Q_raw [MAX_FILTER_SIZE]C.float

			// Only need one mixer for profile "B".  Reuse the same storage?

			//#define c_I_raw m_I_raw
			//#define c_Q_raw m_Q_raw
			c_I_raw [MAX_FILTER_SIZE]C.float
			c_Q_raw [MAX_FILTER_SIZE]C.float

			use_rrc C.int // Use RRC rather than generic low pass.

			rrc_width_sym C.float /* Width of RRC filter in number of symbols.  */

			rrc_rolloff C.float /* Rolloff factor for RRC.  Between 0 and 1. */

			prev_phase C.float // To see phase shift between samples for FM demod.

			normalize_rpsam C.float // Normalize to -1 to +1 for expected tones.

		}

		//////////////////////////////////////////////////////////////////////////////////
		//										//
		//				Baseband only, AKA G3RUH			//
		//										//
		//////////////////////////////////////////////////////////////////////////////////

		// TODO: Continue experiments with root raised cosine filter.
		// Either switch to that or take out all the related stuff.

		bb struct {
			rrc_width_sym C.float /* Width of RRC filter in number of symbols. */

			rrc_rolloff C.float /* Rolloff factor for RRC.  Between 0 and 1. */

			rrc_filter_taps C.int // Number of elements used in the next two.

			// FIXME: TODO: reevaluate max size needed.

			audio_in [MAX_FILTER_SIZE]C.float

			lp_filter [MAX_FILTER_SIZE]C.float

			// New in 1.7 - Polyphase filter to reduce CPU requirements.

			lp_polyphase_1 [MAX_FILTER_SIZE]C.float
			lp_polyphase_2 [MAX_FILTER_SIZE]C.float
			lp_polyphase_3 [MAX_FILTER_SIZE]C.float
			lp_polyphase_4 [MAX_FILTER_SIZE]C.float

			lp_1_iir_param C.float // very low pass filters to get DC offset.
			lp_1_out       C.float

			lp_2_iir_param C.float
			lp_2_out       C.float

			agc_1_fast_attack C.float // Signal envelope detection.
			agc_1_slow_decay  C.float
			agc_1_peak        C.float
			agc_1_valley      C.float

			agc_2_fast_attack C.float
			agc_2_slow_decay  C.float
			agc_2_peak        C.float
			agc_2_valley      C.float

			agc_3_fast_attack C.float
			agc_3_slow_decay  C.float
			agc_3_peak        C.float
			agc_3_valley      C.float

			// CIC low pass filters to detect DC bias or low frequency changes.
			// IIR behaves like an analog R-C filter.
			// Intuitively, it seems like FIR would be better because it is based on a finite history.
			// However, it would require MANY taps and a LOT of computation for a low frequency.
			// We can use a little trick here to keep a running average.
			// This would be equivalent to convolving with an array of all 1 values.
			// That would eliminate the need to multiply.
			// We can also eliminate the need to add them all up each time by keeping a running total.
			// Add a sample to the total when putting it in our array of recent samples.
			// Subtract it from the total when it gets pushed off the end.
			// We can also eliminate the need to shift them all down by using a circular buffer.
			// This only works with integers because float would have cumulated round off errors.

			cic_center1 cic_t
			cic_above   cic_t
			cic_below   cic_t
		}

		//////////////////////////////////////////////////////////////////////////////////
		//										//
		//					PSK only.				//
		//										//
		//////////////////////////////////////////////////////////////////////////////////

		psk struct {
			v26_alt C.enum_v26_e // Which alternative when V.26.

			sin_table256 [256]C.float // Precomputed sin table for speed.

			// Optional band pass pre-filter before phase detector.

			// TODO? put back into common section?
			// TODO? Why was I thinking that?

			use_prefilter C.int // True to enable it.

			prefilter_baud C.float // Cutoff frequencies, as fraction of baud rate, beyond tones used.
			// In the case of PSK, we use only a single tone of 1800 Hz.
			// If we were using 2400 bps (= 1200 baud), this would be
			// the fraction of 1200 for the cutoff below and above 1800.

			pre_filter_width_sym C.float /* Length in number of symbol times. */

			pre_filter_taps C.int /* Size of pre filter, in audio samples. */

			pre_window bp_window_t

			audio_in   [MAX_FILTER_SIZE]C.float
			pre_filter [MAX_FILTER_SIZE]C.float

			// Use local oscillator or correlate with previous sample.

			psk_use_lo C.int /* Use local oscillator rather than self correlation. */

			lo_step C.uint /* How much to advance the local oscillator */
			/* phase for each audio sample. */

			lo_phase C.uint /* Local oscillator phase accumulator for PSK. */

			// After mixing with LO before low pass filter.

			I_raw [MAX_FILTER_SIZE]C.float
			Q_raw [MAX_FILTER_SIZE]C.float

			// Number of delay line taps into previous symbol.
			// They are one symbol period and + or - 45 degrees of the carrier frequency.

			boffs C.int /* symbol length based on sample rate and baud. */
			coffs C.int /* to get cos component of previous symbol. */
			soffs C.int /* to get sin component of previous symbol. */

			delay_line_width_sym C.float
			delay_line_taps      C.int // In audio samples.

			delay_line [MAX_FILTER_SIZE]C.float

			// Low pass filter Second is frequency as ratio to baud rate for FIR.

			// TODO? put back into common section?
			// TODO? What are the tradeoffs?
			lpf_baud C.float /* Cutoff frequency as fraction of baud. */
			/* Intuitively we'd expect this to be somewhere */
			/* in the range of 0.5 to 1. */

			lp_filter_width_sym C.float /* Length in number of symbol times. */

			lp_filter_taps C.int /* Size of Low Pass filter, in audio samples (i.e. filter taps). */

			lp_window bp_window_t

			lp_filter [MAX_FILTER_SIZE]C.float
		}
	} // end of union for different demodulator types.

}
