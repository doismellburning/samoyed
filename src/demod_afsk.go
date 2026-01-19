package direwolf

// #define DEBUG1 1     /* display debugging info */

// #define DEBUG3 1	/* print carrier detect changes. */

// #define DEBUG4 1	/* capture AFSK demodulator output to log files */
/* Can be used to make nice plots. */

// #define DEBUG5 1	// Write just demodulated bit stream to file. */

/*------------------------------------------------------------------
 *
 * Purpose:   	Demodulator for Audio Frequency Shift Keying (AFSK).
 *
 * Input:	Audio samples from either a file or the "sound card."
 *
 * Outputs:	Calls hdlc_rec_bit() for each bit demodulated.
 *
 *---------------------------------------------------------------*/

// #include "direwolf.h"
// #include <stdlib.h>
// #include <stdio.h>
// #include <math.h>
// #include <unistd.h>
// #include <sys/stat.h>
// #include <string.h>
// #include <assert.h>
// #include <ctype.h>
// #include "audio.h"
import "C"

import (
	"math"
)

var DCD_CONFIG_AFSK = GenericDCDConfig()

func TUNE(envvar string, param any, name string, fmt string) {
	/*
	   TODO KG

	   	#define TUNE(envvar,param,name,fmt) { 				\
	   		char *e = getenv(envvar);				\
	   		if (e != NULL) {					\
	   		  param = atof(e);					\
	   		  text_color_set (DW_COLOR_ERROR);			\
	   		  dw_printf ("TUNE: " name " = " fmt "\n", param);	\
	   		} }
	*/
}

// Cosine table indexed by unsigned byte.
var fcos256_table [256]C.float

func fcos256(x C.uint) C.float {
	return (fcos256_table[((x)>>24)&0xff])
}

func fsin256(x C.uint) C.float {
	return (fcos256_table[(((x)>>24)-64)&0xff])
}

// K6JQ  pointed me to this wonderful article:
// Improved Automatic Threshold Correction Methods for FSK by Kok Chen, W7AY.
// http://www.w7ay.net/site/Technical/ATC/index.html
//
// The stated problem is a little different, selective fading for HF RTTY, but the
// general idea is the similar:  Compensating for imbalance of the two tones.
//
// The stronger tone probably has a better S/N ratio so we apply a larger
// weight to it.  Effectively it is comparing power rather than amplitude.
// This is the optimal method from the article referenced.
//
// Interesting idea but it did not work as well as the original AGC in this case.
// For VHF FM we are not dealing with rapid deep selective fading of one tone.
// Instead we have an imbalance which is the same for the whole frame.
// It might be interesting to try this with HF SSB packet which is much like RTTY.
//
// I use the term valley rather than noise floor.
// After a little algebra, it looks remarkably similar to the function above.
//
//	return (( x - valley ) * ( peak - valley ) - 0.5f * ( peak - valley ) * ( peak - valley ));
//	return (( x - valley ) - 0.5f * ( peak - valley )) * ( peak - valley ));
//	return (( x - 0.5f * (peak + valley )) * ( peak - valley ));

/*
 * for multi-slicer experiment.
 */

const MIN_G = 0.5
const MAX_G = 4.0

/* TODO: static */ // TODO KG float space_gain[MAX_SUBCHANS];

/*------------------------------------------------------------------
 *
 * Name:        demod_afsk_init
 *
 * Purpose:     Initialization for an AFSK demodulator.
 *		Select appropriate parameters and set up filters.
 *
 * Inputs:   	samples_per_sec
 *		baud
 *		mark_freq
 *		space_freq
 *
 *		D		- Pointer to demodulator state for given channel.
 *
 * Outputs:
 *
 * Returns:     None.
 *
 * Bugs:	This doesn't do much error checking so don't give it
 *		anything crazy.
 *
 *----------------------------------------------------------------*/

func demod_afsk_init(_samples_per_sec C.int, _baud C.int, mark_freq C.int,
	space_freq C.int, profile C.char, D *demodulator_state_s) {

	var samples_per_sec = C.float(_samples_per_sec)
	var baud = C.float(_baud)

	for j := 0; j < 256; j++ {
		fcos256_table[j] = C.float(math.Cos(float64(j) * 2.0 * math.Pi / 256.0))
	}

	*D = demodulator_state_s{} //nolint:exhaustruct

	D.num_slicers = 1

	/*
		#if DEBUG1
			dw_printf ("demod_afsk_init (rate=%d, baud=%d, mark=%d, space=%d, profile=%c\n",
				samples_per_sec, baud, mark_freq, space_freq, profile);
		#endif
	*/
	D.profile = profile

	switch D.profile {

	case 'A': // Official name
		fallthrough
	case 'E': // For compatibility during transition

		D.profile = 'A'

		/* New in version 1.7 */
		/* This is a simpler version of what has been used all along. */
		/* Rather than convolving each sample with a pre-computed mark and */
		/* space filter, we have two free running local oscillators.  */
		/* Also see if we can do better with a Root Raised Cosine filter */
		/* which supposedly reduces intersymbol interference. */

		D.use_prefilter = 1 /* first, a bandpass filter. */

		if baud > 600 {
			D.prefilter_baud = 0.155
			// Low cutoff below mark, high cutoff above space
			// as fraction of the symbol rate.
			// Intuitively you might expect this to be about
			// half the symbol rate, e.g. 600 Hz outside
			// the two tones of interest for 1200 baud.
			// It turns out that narrower is better.

			D.pre_filter_len_sym = 383 * 1200. / 44100. // about 8 symbols
			D.pre_window = BP_WINDOW_TRUNCATED
		} else {
			D.prefilter_baud = 0.87 // TODO: fine tune
			D.pre_filter_len_sym = 1.857
			D.pre_window = BP_WINDOW_COSINE
		}

		// Local oscillators for Mark and Space tones.

		D.u.afsk.m_osc_phase = 0
		D.u.afsk.m_osc_delta = C.uint(math.Round(math.Pow(2., 32.) * float64(mark_freq) / float64(samples_per_sec)))

		D.u.afsk.s_osc_phase = 0
		D.u.afsk.s_osc_delta = C.uint(math.Round(math.Pow(2., 32.) * float64(space_freq) / float64(samples_per_sec)))

		D.u.afsk.use_rrc = 1
		TUNE("TUNE_USE_RRC", D.u.afsk.use_rrc, "use_rrc", "%d")

		if D.u.afsk.use_rrc != 0 {
			D.u.afsk.rrc_width_sym = 2.80
			D.u.afsk.rrc_rolloff = 0.20
		} else {
			D.lpf_baud = 0.14
			D.lp_filter_width_sym = 1.388
			D.lp_window = BP_WINDOW_TRUNCATED
		}

		D.agc_fast_attack = 0.70
		D.agc_slow_decay = 0.000090

		D.pll_locked_inertia = 0.74
		D.pll_searching_inertia = 0.50

	case 'B': // official name
		fallthrough
	case 'D': // backward compatibility

		D.profile = 'B'

		// Experiment for version 1.7.
		// Up to this point, I've always used separate mark and space
		// filters and compared the amplitudes.
		// Another technique for an FM demodulator is to mix with
		// the center frequency and look for the rate of change of the phase.

		D.use_prefilter = 1 /* first, a bandpass filter. */

		if baud > 600 {
			D.prefilter_baud = 0.19
			// Low cutoff below mark, high cutoff above space
			// as fraction of the symbol rate.
			// Intuitively you might expect this to be about
			// half the symbol rate, e.g. 600 Hz outside
			// the two tones of interest for 1200 baud.
			// It turns out that narrower is better.

			D.pre_filter_len_sym = 8.163 // Filter length in symbol times.
			D.pre_window = BP_WINDOW_TRUNCATED
		} else {
			D.prefilter_baud = 0.87 // TODO: fine tune
			D.pre_filter_len_sym = 1.857
			D.pre_window = BP_WINDOW_COSINE
		}

		// Local oscillator for Center frequency.

		D.u.afsk.c_osc_phase = 0
		D.u.afsk.c_osc_delta = C.uint(math.Round(math.Pow(2., 32.) * 0.5 * float64(mark_freq+space_freq) / float64(samples_per_sec)))

		D.u.afsk.use_rrc = 1
		TUNE("TUNE_USE_RRC", D.u.afsk.use_rrc, "use_rrc", "%d")

		if D.u.afsk.use_rrc != 0 {
			D.u.afsk.rrc_width_sym = 2.00
			D.u.afsk.rrc_rolloff = 0.40
		} else {
			D.lpf_baud = 0.5
			D.lp_filter_width_sym = 1.714286 //  63 * 1200. / 44100.;
			D.lp_window = BP_WINDOW_TRUNCATED
		}

		// For scaling phase shift into normalized -1 to +1 range for mark and space.
		D.u.afsk.normalize_rpsam = C.float(1.0 / (0.5 * math.Abs(float64(mark_freq-space_freq)) * 2 * math.Pi / float64(samples_per_sec)))

		// New "B" demodulator does not use AGC but demod.c needs this to derive "quick" and
		// "sluggish" values for overall signal amplitude.  That probably should be independent
		// of these values.
		D.agc_fast_attack = 0.70
		D.agc_slow_decay = 0.000090

		D.pll_locked_inertia = 0.74
		D.pll_searching_inertia = 0.50

		D.alevel_mark_peak = -1 // Disable received signal (m/s) display.
		D.alevel_space_peak = -1

	default:

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid AFSK demodulator profile = %c\n", profile)
		exit(1)
	}

	TUNE("TUNE_PRE_BAUD", D.prefilter_baud, "prefilter_baud", "%.3f")
	TUNE("TUNE_PRE_WINDOW", D.pre_window, "pre_window", "%d")

	TUNE("TUNE_LPF_BAUD", D.lpf_baud, "lpf_baud", "%.3f")
	TUNE("TUNE_LP_WINDOW", D.lp_window, "lp_window", "%d")

	TUNE("TUNE_RRC_ROLLOFF", D.u.afsk.rrc_rolloff, "rrc_rolloff", "%.2f")
	TUNE("TUNE_RRC_WIDTH_SYM", D.u.afsk.rrc_width_sym, "rrc_width_sym", "%.2f")

	TUNE("TUNE_AGC_FAST", D.agc_fast_attack, "agc_fast_attack", "%.3f")
	TUNE("TUNE_AGC_SLOW", D.agc_slow_decay, "agc_slow_decay", "%.6f")

	TUNE("TUNE_PLL_LOCKED", D.pll_locked_inertia, "pll_locked_inertia", "%.2f")
	TUNE("TUNE_PLL_SEARCHING", D.pll_searching_inertia, "pll_searching_inertia", "%.2f")

	/*
	 * Calculate constants used for timing.
	 * The audio sample rate must be at least a few times the data rate.
	 *
	 * Baud is an integer so we hack in a fine adjustment for EAS.
	 * Probably makes no difference because the DPLL keeps it in sync.
	 *
	 * A fraction if a Hz would make no difference for the filters.
	 */
	if baud == 521 {
		D.pll_step_per_sample = C.int(math.Round((TICKS_PER_PLL_CYCLE * 520.83) / float64(samples_per_sec)))
	} else {
		D.pll_step_per_sample = C.int(math.Round((TICKS_PER_PLL_CYCLE * float64(baud)) / float64(samples_per_sec)))
	}

	/*
	 * Optionally apply a bandpass ("pre") filter to attenuate
	 * frequencies outside the range of interest.
	 */

	if D.use_prefilter != 0 {

		// odd number is a little better
		D.pre_filter_taps = C.int((D.pre_filter_len_sym * C.float(samples_per_sec) / C.float(baud))) | 1

		TUNE("TUNE_PRE_FILTER_TAPS", D.pre_filter_taps, "pre_filter_taps", "%d")

		// TODO:  Size comes out to 417 for 1200 bps with 48000 sample rate.
		// The message is upsetting.  Can we handle this better?

		if D.pre_filter_taps > MAX_FILTER_SIZE {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Warning: Calculated pre filter size of %d is too large.\n", D.pre_filter_taps)
			dw_printf("Decrease the audio sample rate or increase the decimation factor.\n")
			dw_printf("You can use -D2 or -D3, on the command line, to down-sample the audio rate\n")
			dw_printf("before demodulating.  This greatly decreases the CPU requirements with little\n")
			dw_printf("impact on the decoding performance.  This is useful for a slow ARM processor,\n")
			dw_printf("such as with a Raspberry Pi model 1.\n")
			D.pre_filter_taps = (MAX_FILTER_SIZE - 1) | 1
		}

		var f1 = C.float(min(mark_freq, space_freq)) - D.prefilter_baud*C.float(baud)
		var f2 = C.float(max(mark_freq, space_freq)) + D.prefilter_baud*C.float(baud)
		/*
			#if 0
				  text_color_set(DW_COLOR_DEBUG);
				  dw_printf ("Generating prefilter %.0f to %.0f Hz.\n", f1, f2);
			#endif
		*/
		f1 /= C.float(samples_per_sec)
		f2 /= C.float(samples_per_sec)

		gen_bandpass(f1, f2, D.pre_filter[:], D.pre_filter_taps, D.pre_window)
	}

	/*
	 * Now the lowpass filter.
	 * In version 1.7 a Root Raised Cosine filter is added as an alternative
	 * to the generic low pass filter.
	 * In both cases, lp_filter and lp_filter_taps are used but the
	 * contents will be generated differently.  Later code does not care.
	 */
	if D.u.afsk.use_rrc != 0 {

		Assert(D.u.afsk.rrc_width_sym >= 1 && D.u.afsk.rrc_width_sym <= 16)
		Assert(D.u.afsk.rrc_rolloff >= 0. && D.u.afsk.rrc_rolloff <= 1.)

		D.lp_filter_taps = C.int((D.u.afsk.rrc_width_sym * C.float(samples_per_sec) / C.float(baud))) | 1 // odd works better

		TUNE("TUNE_LP_FILTER_TAPS", D.lp_filter_taps, "lp_filter_taps (RRC)", "%d")

		if D.lp_filter_taps > MAX_FILTER_SIZE {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Calculated RRC low pass filter size of %d is too large.\n", D.lp_filter_taps)
			dw_printf("Decrease the audio sample rate or increase the decimation factor or\n")
			dw_printf("recompile the application with MAX_FILTER_SIZE larger than %d.\n", MAX_FILTER_SIZE)
			D.lp_filter_taps = (MAX_FILTER_SIZE - 1) | 1
		}

		Assert(D.lp_filter_taps > 8 && D.lp_filter_taps <= MAX_FILTER_SIZE)
		gen_rrc_lowpass(D.lp_filter[:], D.lp_filter_taps, D.u.afsk.rrc_rolloff, samples_per_sec/baud)
	} else {
		D.lp_filter_taps = C.int(math.Round(float64(D.lp_filter_width_sym * samples_per_sec / baud)))

		TUNE("TUNE_LP_FILTER_TAPS", D.lp_filter_taps, "lp_filter_taps (FIR)", "%d")

		if D.lp_filter_taps > MAX_FILTER_SIZE {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Calculated FIR low pass filter size of %d is too large.\n", D.lp_filter_taps)
			dw_printf("Decrease the audio sample rate or increase the decimation factor or\n")
			dw_printf("recompile the application with MAX_FILTER_SIZE larger than %d.\n", MAX_FILTER_SIZE)
			D.lp_filter_taps = (MAX_FILTER_SIZE - 1) | 1
		}

		Assert(D.lp_filter_taps > 8 && D.lp_filter_taps <= MAX_FILTER_SIZE)

		var fc = C.float(baud) * D.lpf_baud / samples_per_sec
		gen_lowpass(fc, D.lp_filter[:], D.lp_filter_taps, D.lp_window)
	}

	/*
	 * Starting with version 1.2
	 * try using multiple slicing points instead of the traditional AGC.
	 */
	space_gain[0] = MIN_G
	var step = math.Pow(10.0, math.Log10(MAX_G/MIN_G)/(MAX_SUBCHANS-1))
	for j := 1; j < MAX_SUBCHANS; j++ {
		space_gain[j] = space_gain[j-1] * C.float(step)
	}

} /* demod_afsk_init */

/*-------------------------------------------------------------------
 *
 * Name:        demod_afsk_process_sample
 *
 * Purpose:     (1) Demodulate the AFSK signal.
 *		(2) Recover clock and data.
 *
 * Inputs:	channel	- Audio channel.  0 for left, 1 for right.
 *		subchannel - modem of the channel.
 *		sam	- One sample of audio.
 *			  Should be in range of -32768 .. 32767.
 *
 * Returns:	None
 *
 * Descripion:	First demodulate the AFSK signal.
 *
 *		A digital phase locked loop (PLL) recovers the symbol
 *		clock and picks out data bits at the proper rate.
 *
 *		For each recovered data bit, we call:
 *
 *			  hdlc_rec (channel, demodulated_bit);
 *
 *		to decode HDLC frames from the stream of bits.
 *
 * Future:	This could be generalized by passing in the name
 *		of the function to be called for each bit recovered
 *		from the demodulator.  For now, it's simply hard-coded.
 *
 * Evolution:	The simple version works less well when there is a substantial difference
 *		in amplitude of the two tones.  e.g. When de-emphasis cuts the
 *		higher tone down to about half the amplitude. We overcome that
 *		by boosting the space amplitude by varying amounts before slicing.
 *
 *		In version 1.7 an entirely different approach is added, an FM
 *		discriminator which produces a result proportional to the
 *		frequency.
 *
 *--------------------------------------------------------------------*/
/*
 * Which tone is stronger?
 *
 * In an ideal world, simply compare.  In my first naive attempt, that
 * worked well with perfect signals. In the real world, we don't
 * have too many perfect signals.
 *
 * Here is an excellent explanation:
 * http://www.febo.com/packet/layer-one/transmit.html
 *
 * Under real conditions, we find that the higher tone usually has a
 * considerably smaller amplitude due to the passband characteristics
 * of the transmitter and receiver.  To make matters worse, it
 * varies considerably from one station to another.
 *
 * The two filters also have different amounts of DC bias.
 *
 * My solution was to apply automatic gain control (AGC) to the mark and space
 * levels.  This works by looking at the minimum and maximum outputs
 * for each filter and scaling the results to be roughly in the -0.5 to +0.5 range.
 * Results were excellent after tweaking the attack and decay times.
 *
 * 4X6IZ took a different approach.  See QEX Jul-Aug 2012.
 *
 * He ran two different demodulators in parallel.  One of them boosted the higher
 * frequency tone by 6 dB.  Any duplicates were removed.  This produced similar results.
 * He also used a bandpass filter before the mark/space filters.
 * I haven't tried this combination yet for 1200 baud.
 *
 * First, let's take a look at Track 1 of the TNC test CD.  Here the receiver
 * has a flat response.  We find the mark/space strength ratios very from 0.53 to 1.38
 * with a median of 0.81.  This is in line with expectations because most
 * transmitters add pre-emphasis to boost the higher audio frequencies.
 * Track 2 should more closely resemble what comes out of the speaker on a typical
 * transceiver.  Here we see a ratio from 1.73 to 3.81 with a median of 2.48.
 *
 * This is similar to my observations of local signals, from the speaker.
 * The amplitude ratio varies from 1.48 to 3.41 with a median of 2.70.
 */

func demod_afsk_process_sample(channel C.int, subchannel C.int, sam C.int, D *demodulator_state_s) {
	/*
		#if DEBUG4
			static FILE *demod_log_fp = NULL;
			static int seq = 0;			// for log file name
		#endif
	*/

	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)
	Assert(subchannel >= 0 && subchannel < MAX_SUBCHANS)

	/*
	 * Filters use last 'filter_taps' samples.
	 *
	 * First push the older samples down.
	 *
	 * Finally, put the most recent at the beginning.
	 *
	 * Future project?  Can we do better than shifting each time?
	 */

	/* Scale to nice number. */

	var fsam = C.float(sam) / 16384.0

	switch D.profile {

	default:
		fallthrough
	case 'E':
		fallthrough
	case 'A':
		{
			/* ========== New in Version 1.7 ========== */

			//	Cleaner & simpler than earlier 'A' thru 'E'

			if D.use_prefilter != 0 {
				push_sample(fsam, &D.raw_cb[0], D.pre_filter_taps)
				fsam = convolve(D.raw_cb[:], D.pre_filter[:], D.pre_filter_taps)
			}

			push_sample(fsam*fcos256(D.u.afsk.m_osc_phase), &D.u.afsk.m_I_raw[0], D.lp_filter_taps)
			push_sample(fsam*fsin256(D.u.afsk.m_osc_phase), &D.u.afsk.m_Q_raw[0], D.lp_filter_taps)
			D.u.afsk.m_osc_phase += D.u.afsk.m_osc_delta

			push_sample(fsam*fcos256(D.u.afsk.s_osc_phase), &D.u.afsk.s_I_raw[0], D.lp_filter_taps)
			push_sample(fsam*fsin256(D.u.afsk.s_osc_phase), &D.u.afsk.s_Q_raw[0], D.lp_filter_taps)
			D.u.afsk.s_osc_phase += D.u.afsk.s_osc_delta

			var m_I = convolve(D.u.afsk.m_I_raw[:], D.lp_filter[:], D.lp_filter_taps)
			var m_Q = convolve(D.u.afsk.m_Q_raw[:], D.lp_filter[:], D.lp_filter_taps)
			var m_amp = C.float(math.Hypot(float64(m_I), float64(m_Q)))

			var s_I = convolve(D.u.afsk.s_I_raw[:], D.lp_filter[:], D.lp_filter_taps)
			var s_Q = convolve(D.u.afsk.s_Q_raw[:], D.lp_filter[:], D.lp_filter_taps)
			var s_amp = C.float(math.Hypot(float64(s_I), float64(s_Q)))

			/*
			 * Capture the mark and space peak amplitudes for display.
			 * It uses fast attack and slow decay to get an idea of the
			 * overall amplitude.
			 */
			if m_amp >= D.alevel_mark_peak {
				D.alevel_mark_peak = m_amp*D.quick_attack + D.alevel_mark_peak*(1.0-D.quick_attack)
			} else {
				D.alevel_mark_peak = m_amp*D.sluggish_decay + D.alevel_mark_peak*(1.0-D.sluggish_decay)
			}

			if s_amp >= D.alevel_space_peak {
				D.alevel_space_peak = s_amp*D.quick_attack + D.alevel_space_peak*(1.0-D.quick_attack)
			} else {
				D.alevel_space_peak = s_amp*D.sluggish_decay + D.alevel_space_peak*(1.0-D.sluggish_decay)
			}

			if D.num_slicers <= 1 {

				// Which tone is stronger?  That's simple with an ideal signal.
				// However, we don't see too many ideal signals.
				// Due to mismatching pre-emphasis and de-emphasis, the two
				// tones will often have greatly different amplitudes so we use
				// automatic gain control (AGC) to scale each to the same range
				// before comparing.
				// This is probably over complicated and could be combined with
				// the signal amplitude measurement, above.
				// It works so let's move along to other topics.

				var m_norm, s_norm C.float
				D.m_peak, D.m_valley, m_norm = agc(m_amp, D.agc_fast_attack, D.agc_slow_decay, D.m_peak, D.m_valley)
				D.s_peak, D.s_valley, s_norm = agc(s_amp, D.agc_fast_attack, D.agc_slow_decay, D.s_peak, D.s_valley)

				// The normalized values should be around -0.5 to +0.5 so the difference
				// should work out to be around -1 to +1.
				// This is important because nudge_pll uses the demod_out amplitude to assign
				// a quality or confidence score to the symbol.

				var demod_out = m_norm - s_norm

				// Tested and it looks good.  Range of about -1 to +1.
				//printf ("JWL DEBUG demod A with agc = %6.2f\n", demod_out);

				nudge_pll_afsk(channel, subchannel, 0, demod_out, D, 1.0)

			} else {
				// Multiple slice case.
				// Rather than trying to find the best threshold location, use multiple
				// slicer thresholds in parallel.
				// The best slicing point will vary from packet to packet but should
				// remain about the same for a given packet.

				// We are not performing the AGC step here but still want the envelope
				// for calculating the confidence level (or quality) of the sample.

				D.m_peak, D.m_valley, _ = agc(m_amp, D.agc_fast_attack, D.agc_slow_decay, D.m_peak, D.m_valley)
				D.s_peak, D.s_valley, _ = agc(s_amp, D.agc_fast_attack, D.agc_slow_decay, D.s_peak, D.s_valley)

				for slice := C.int(0); slice < D.num_slicers; slice++ {
					var demod_out = m_amp - s_amp*space_gain[slice]
					var amp = 0.5 * (D.m_peak - D.m_valley + (D.s_peak-D.s_valley)*space_gain[slice])
					if amp < 0.0000001 {
						amp = 1 // avoid divide by zero with no signal.
					}

					// Tested and it looks good.  Range of about -1 to +1 relative to amp.
					// Biased one way or the other depending on the space gain.
					//printf ("JWL DEBUG demod A with slicer %d: %6.2f / %6.2f = %6.2f\n", slice, demod_out, amp, demod_out/amp);

					nudge_pll_afsk(channel, subchannel, slice, demod_out, D, amp)
				}
			}
		}

	case 'D':
		fallthrough
	case 'B':
		{
			/* ========== Version 1.7 Experiment ========== */

			// New - Convert frequency to a value proportional to frequency.

			if D.use_prefilter != 0 {
				push_sample(fsam, &D.raw_cb[0], D.pre_filter_taps)
				fsam = convolve(D.raw_cb[:], D.pre_filter[:], D.pre_filter_taps)
			}

			push_sample(fsam*fcos256(D.u.afsk.c_osc_phase), &D.u.afsk.c_I_raw[0], D.lp_filter_taps)
			push_sample(fsam*fsin256(D.u.afsk.c_osc_phase), &D.u.afsk.c_Q_raw[0], D.lp_filter_taps)
			D.u.afsk.c_osc_phase += D.u.afsk.c_osc_delta

			var c_I = convolve(D.u.afsk.c_I_raw[:], D.lp_filter[:], D.lp_filter_taps)
			var c_Q = convolve(D.u.afsk.c_Q_raw[:], D.lp_filter[:], D.lp_filter_taps)

			var phase = C.float(math.Atan2(float64(c_Q), float64(c_I)))
			var rate = phase - D.u.afsk.prev_phase
			if rate > math.Pi {
				rate -= 2 * math.Pi
			} else if rate < -math.Pi {
				rate += 2 * math.Pi
			}

			D.u.afsk.prev_phase = phase

			// Rate is radians per audio sample interval or something like that.
			// Scale scale that into -1 to +1 for expected tones.

			var norm_rate = rate * D.u.afsk.normalize_rpsam

			// We really don't have mark and space amplitudes available in this case.

			if D.num_slicers <= 1 {

				var demod_out = norm_rate
				// Tested and it looks good.  Range roughly -1 to +1.
				//printf ("JWL DEBUG demod B single = %6.2f\n", demod_out);

				nudge_pll_afsk(channel, subchannel, 0, demod_out, D, 1.0)

			} else {

				// This would be useful for HF SSB where a tuning error
				// would shift the frequency.  Multiple slicing points would
				// then compensate for differences in transmit/receive frequencies.
				//
				// Where should we set the thresholds?
				// I'm thinking something like:
				// 	-.5	-.375	-.25	-.125	0	.125	.25	.375	.5
				//
				// Assuming a 300 Hz shift, this would put slicing thresholds up
				// to +-75 Hz from the center.

				for slice := C.int(0); slice < D.num_slicers; slice++ {

					var offset = -0.5 + C.float(slice)*(1./C.float(D.num_slicers-1))
					var demod_out = norm_rate + offset

					//printf ("JWL DEBUG demod B slice %d, offset = %6.3f, demod_out = %6.2f\n", slice, offset, demod_out);

					nudge_pll_afsk(channel, subchannel, slice, demod_out, D, 1.0)
				}
			}
		}
	}

	/*
		#if DEBUG4

			if (channel == 0) {
			if (D.slicer[slice].data_detect) {
			  char fname[30];


			  if (demod_log_fp == NULL) {
			    seq++;
			    snprintf (fname, sizeof(fname), "demod/%04d.csv", seq);
			    if (seq == 1) mkdir ("demod", 0777);

			    demod_log_fp = fopen (fname, "w");
			    text_color_set(DW_COLOR_DEBUG);
			    dw_printf ("Starting demodulator log file %s\n", fname);
			    fprintf (demod_log_fp, "Audio, Mark, Space, Demod, Data, Clock\n");
			  }
			  fprintf (demod_log_fp, "%.3f, %.3f, %.3f, %.3f, %.2f, %.2f\n", fsam + 3.5, m_norm + 2, s_norm + 2,
					(m_norm - s_norm) / 2 + 1.5,
					demod_data ? .9 : .55,
					(D.data_clock_pll & 0x80000000) ? .1 : .45);
			} else {
			  if (demod_log_fp != NULL) {
			    fclose (demod_log_fp);
			    demod_log_fp = NULL;
			  }
			}
			}

		#endif
	*/

} /* end demod_afsk_process_sample */

/*
 * Finally, a PLL is used to sample near the centers of the data bits.
 *
 * D points to a demodulator for a channel/subchannel pair so we don't
 * have to keep recalculating it.
 *
 * D.data_clock_pll is a SIGNED 32 bit variable.
 * When it overflows from a large positive value to a negative value, we
 * sample a data bit from the demodulated signal.
 *
 * Ideally, the the demodulated signal transitions should be near
 * zero we we sample mid way between the transitions.
 *
 * Nudge the PLL by removing some small fraction from the value of
 * data_clock_pll, pushing it closer to zero.
 *
 * This adjustment will never change the sign so it won't cause
 * any erratic data bit sampling.
 *
 * If we adjust it too quickly, the clock will have too much jitter.
 * If we adjust it too slowly, it will take too long to lock on to a new signal.
 *
 * Be a little more aggressive about adjusting the PLL
 * phase when searching for a signal.  Don't change it as much when
 * locked on to a signal.
 *
 * I don't think the optimal value will depend on the audio sample rate
 * because this happens for each transition from the demodulator.
 */

func nudge_pll_afsk(channel C.int, subchannel C.int, slice C.int, demod_out C.float, D *demodulator_state_s, amplitude C.float) {

	D.slicer[slice].prev_d_c_pll = D.slicer[slice].data_clock_pll

	// Perform the add as unsigned to avoid signed overflow error.
	D.slicer[slice].data_clock_pll = (C.int)((C.uint)(D.slicer[slice].data_clock_pll) + (C.uint)(D.pll_step_per_sample))

	//text_color_set(DW_COLOR_DEBUG);
	// dw_printf ("prev = %lx, new data clock pll = %lx\n" D.prev_d_c_pll, D.data_clock_pll);

	if D.slicer[slice].data_clock_pll < 0 && D.slicer[slice].prev_d_c_pll > 0 {

		/* Overflow - this is where we sample. */
		// Assign it a confidence level or quality, 0 to 100, based on the amplitude.
		// Those very close to 0 are suspect.  We'll get back to this later.

		var quality = C.int(math.Abs(float64(demod_out)) * 100.0 / float64(amplitude))
		if quality > 100 {
			quality = 100
		}

		/*
			#if DEBUG5
				  // Write bit stream to a file.

				  static FILE *bsfp = NULL;
				  static int bcount = 0;
				  if (channel == 0 && subchannel == 0 && slice == 0) {
				    if (bsfp == NULL) {
				       bsfp = fopen ("bitstream.txt", "w");
				    }
				    fprintf (bsfp, "%d", demod_out > 0);
				    bcount++;
				    if (bcount % 64 == 0) {
				      fprintf (bsfp, "\n");
				    }
				  }

			#endif
		*/

		// #if 1
		hdlc_rec_bit(channel, subchannel, slice, C.int(IfThenElse(demod_out > 0, 1, 0)), 0, quality)
		/*
			#else  // TODO: new feature to measure data speed error.
			// Maybe hdlc_rec_bit could provide indication when frame starts.
				  hdlc_rec_bit_new (channel, subchannel, slice, demod_out > 0, 0, quality,
						&(D.slicer[slice].pll_nudge_total), &(D.slicer[slice].pll_symbol_count));
				  D.slicer[slice].pll_symbol_count++;
			#endif
		*/
		pll_dcd_each_symbol2(DCD_CONFIG_AFSK, D, channel, subchannel, slice)
	}

	// Transitions nudge the DPLL phase toward the incoming signal.

	var demod_data = demod_out > 0
	if demod_data != (D.slicer[slice].prev_demod_data != 0) {

		pll_dcd_signal_transition2(DCD_CONFIG_AFSK, D, slice, D.slicer[slice].data_clock_pll)

		// TODO:	  signed int before = (signed int)(D.slicer[slice].data_clock_pll);	// Treat as signed.
		if D.slicer[slice].data_detect != 0 {
			D.slicer[slice].data_clock_pll = C.int(C.float(D.slicer[slice].data_clock_pll) * D.pll_locked_inertia)
		} else {
			D.slicer[slice].data_clock_pll = C.int(C.float(D.slicer[slice].data_clock_pll) * D.pll_searching_inertia)
		}
		// TODO:	  D.slicer[slice].pll_nudge_total += (int64_t)((signed int)(D.slicer[slice].data_clock_pll)) - (int64_t)before;
	}

	/*
	 * Remember demodulator output so we can compare next time.
	 */
	D.slicer[slice].prev_demod_data = C.int(IfThenElse(demod_data, 1, 0))

} /* end nudge_pll */
