package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:     Generate the filters used by the demodulators.
 *
 *----------------------------------------------------------------*/

// #include <stdlib.h>
// #include <stdio.h>
// #include <math.h>
// #include <unistd.h>
// #include <string.h>
// #include <ctype.h>
// #include <assert.h>
import "C"

import (
	"math"
)

// Don't remove this.  It serves as a reminder that an experiment is underway.

/* TODO KG
#if defined(TUNE_MS_FILTER_SIZE) || defined(TUNE_MS2_FILTER_SIZE) || defined(TUNE_AGC_FAST) || defined(TUNE_LPF_BAUD) || defined(TUNE_PLL_LOCKED) || defined(TUNE_PROFILE)
#define DEBUG1 1		// Don't remove this.
#endif
*/

/*------------------------------------------------------------------
 *
 * Name:        window
 *
 * Purpose:     Filter window shape functions.
 *
 * Inputs:   	type	- BP_WINDOW_HAMMING, etc.
 *		size	- Number of filter taps.
 *		j	- Index in range of 0 to size-1.
 *
 * Returns:     Multiplier for the window shape.
 *
 *----------------------------------------------------------------*/

func window(windowType bp_window_t, _size int, _j int) float64 {

	var size = float64(_size) // Save on a lot of casting later
	var j = float64(_j)

	var center = 0.5 * (size - 1)
	var w float64

	switch windowType {

	case BP_WINDOW_COSINE:
		w = math.Cos(float64(j-center) / size * math.Pi)
		//w = math.Sin(j * math.Pi / (size - 1));

	case BP_WINDOW_HAMMING:
		w = 0.53836 - 0.46164*math.Cos((j*2*math.Pi)/(size-1))

	case BP_WINDOW_BLACKMAN:
		w = 0.42659 - 0.49656*math.Cos((j*2*math.Pi)/(size-1)) +
			0.076849*math.Cos((j*4*math.Pi)/(size-1))

	case BP_WINDOW_FLATTOP:
		w = 1.0 - 1.93*math.Cos((j*2*math.Pi)/(size-1)) +
			1.29*math.Cos((j*4*math.Pi)/(size-1)) -
			0.388*math.Cos((j*6*math.Pi)/(size-1)) +
			0.028*math.Cos((j*8*math.Pi)/(size-1))

	case BP_WINDOW_TRUNCATED:
		fallthrough
	default:
		w = 1.0
	}

	return w
}

/*------------------------------------------------------------------
 *
 * Name:        gen_lowpass
 *
 * Purpose:     Generate low pass filter kernel.
 *
 * Inputs:   	fc		- Cutoff frequency as fraction of sampling frequency.
 *		filter_size	- Number of filter taps.
 *		wtype		- Window type, BP_WINDOW_HAMMING, etc.
 *		lp_delay_fract	- Fudge factor for the delay value.
 *
 * Outputs:     lp_filter
 *
 * Returns:	Signal delay thru the filter in number of audio samples.
 *
 *----------------------------------------------------------------*/

func gen_lowpass(fc float64, lp_filter []float64, filter_size int, wtype bp_window_t) {

	/*
		#if DEBUG1
			text_color_set(DW_COLOR_DEBUG);

			dw_printf ("Lowpass, size=%d, fc=%.2f\n", filter_size, fc);
			dw_printf ("   j     shape   sinc   final\n");
		#endif
	*/

	Assert(filter_size >= 3 && filter_size <= MAX_FILTER_SIZE)

	for j := 0; j < filter_size; j++ {
		var sinc float64

		var center = 0.5 * float64(filter_size-1)

		if float64(j)-center == 0 {
			sinc = 2 * fc
		} else {
			sinc = math.Sin(2*math.Pi*(fc*(float64(j)-center))) / (math.Pi * (float64(j) - center))
		}

		var shape = window(wtype, filter_size, j)
		lp_filter[j] = sinc * shape

		/*
			#if DEBUG1
				  dw_printf ("%6d  %6.2f  %6.3f  %6.3f\n", j, shape, sinc, lp_filter[j] ) ;
			#endif
		*/
	}

	/*
	 * Normalize lowpass for unity gain at DC.
	 */
	var G float64 = 0
	for j := 0; j < filter_size; j++ {
		G += lp_filter[j]
	}
	for j := 0; j < filter_size; j++ {
		lp_filter[j] /= G
	}
} /* end gen_lowpass */

// #undef DEBUG1

/*------------------------------------------------------------------
 *
 * Name:        gen_bandpass
 *
 * Purpose:     Generate band pass filter kernel for the prefilter.
 *		This is NOT for the mark/space filters.
 *
 * Inputs:   	f1		- Lower cutoff frequency as fraction of sampling frequency.
 *		f2		- Upper cutoff frequency...
 *		filter_size	- Number of filter taps.
 *		wtype		- Window type, BP_WINDOW_HAMMING, etc.
 *
 * Outputs:     bp_filter
 *
 * Reference:	http://www.labbookpages.co.uk/audio/firWindowing.html
 *
 *		Does it need to be an odd length?
 *
 *----------------------------------------------------------------*/

func gen_bandpass(f1 float64, f2 float64, bp_filter []float64, filter_size int, wtype bp_window_t) {

	var center = 0.5 * float64(filter_size-1)

	/*
		#if DEBUG1
			text_color_set(DW_COLOR_DEBUG);

			dw_printf ("Bandpass, size=%d\n", filter_size);
			dw_printf ("   j     shape   sinc   final\n");
		#endif
	*/

	Assert(filter_size >= 3 && filter_size <= MAX_FILTER_SIZE)

	for j := 0; j < filter_size; j++ {
		var sinc float64

		if float64(j)-center == 0 {
			sinc = 2 * (f2 - f1)
		} else {
			sinc = math.Sin(2*math.Pi*f2*(float64(j)-center))/(math.Pi*(float64(j)-center)) -
				math.Sin(2*math.Pi*f1*(float64(j)-center))/(math.Pi*(float64(j)-center))
		}

		var shape = window(wtype, filter_size, j)
		bp_filter[j] = sinc * shape

		/*
			#if DEBUG1
				  dw_printf ("%6d  %6.2f  %6.3f  %6.3f\n", j, shape, sinc, bp_filter[j] ) ;
			#endif
		*/
	}

	/*
	 * Normalize bandpass for unity gain in middle of passband.
	 * Can't use same technique as for lowpass.
	 * Instead compute gain in middle of passband.
	 * See http://dsp.stackexchange.com/questions/4693/fir-filter-gain
	 */
	var w = 2 * math.Pi * (f1 + f2) / 2
	var G float64 = 0
	for j := 0; j < filter_size; j++ {
		G += 2 * bp_filter[j] * math.Cos((float64(j)-center)*w) // is this correct?
	}

	/*
		#if DEBUG1
			dw_printf ("Before normalizing, G=%.3f\n", G);
		#endif
	*/
	for j := 0; j < filter_size; j++ {
		bp_filter[j] /= G
	}

} /* end gen_bandpass */

/*------------------------------------------------------------------
 *
 * Name:        gen_ms
 *
 * Purpose:     Generate mark and space filters.
 *
 * Inputs:   	fc		- Tone frequency, i.e. mark or space.
 *		sps		- Samples per second.
 *		filter_size	- Number of filter taps.
 *		wtype		- Window type, BP_WINDOW_HAMMING, etc.
 *
 * Outputs:     bp_filter
 *
 * Reference:	http://www.labbookpages.co.uk/audio/firWindowing.html
 *
 *		Does it need to be an odd length?
 *
 *----------------------------------------------------------------*/

func gen_ms(fc int, sps int, sin_table []float64, cos_table []float64, filter_size int, wtype bp_window_t) {

	var Gs float64 = 0
	var Gc float64 = 0

	for j := 0; j < filter_size; j++ {

		var center = 0.5 * float64(filter_size-1)
		var am = ((float64(j) - center) / (float64)(sps)) * (float64(fc)) * (2.0 * (math.Pi))

		var shape = window(wtype, filter_size, j)

		sin_table[j] = math.Sin(float64(am)) * shape
		cos_table[j] = math.Cos(float64(am)) * shape

		Gs += sin_table[j] * math.Sin(float64(am))
		Gc += cos_table[j] * math.Cos(float64(am))

		/*
			#if DEBUG1
				  dw_printf ("%6d  %6.2f  %6.2f  %6.2f\n", j, shape, sin_table[j], cos_table[j]) ;
			#endif
		*/
	}

	/* Normalize for unity gain */

	/*
	   #if DEBUG1

	   	dw_printf ("Before normalizing, Gs = %.2f, Gc = %.2f\n", Gs, Gc) ;

	   #endif
	*/
	for j := 0; j < filter_size; j++ {
		sin_table[j] /= Gs
		cos_table[j] /= Gc
	}

} /* end gen_ms */

/*------------------------------------------------------------------
 *
 * Name:        rrc
 *
 * Purpose:     Root Raised Cosine function.
 *		Why do they call it that?
 *		It's mostly the sinc function with cos windowing to taper off edges faster.
 *
 * Inputs:      t		- Time in units of symbol duration.
 *				  i.e. The centers of two adjacent symbols would differ by 1.
 *
 *		a		- Roll off factor, between 0 and 1.
 *
 * Returns:	Basically the sinc  (sin(x)/x) function with edges decreasing faster.
 *		Should be 1 for t = 0 and 0 at all other integer values of t.
 *
 *----------------------------------------------------------------*/

func rrc(t float64, a float64) float64 {

	var sinc, window, result float64

	if t > -0.001 && t < 0.001 {
		sinc = 1
	} else {
		sinc = math.Sin(math.Pi*t) / (math.Pi * t)
	}

	if math.Abs(a*t) > 0.499 && math.Abs(a*t) < 0.501 {
		window = math.Pi / 4
	} else {
		window = math.Cos(math.Pi*float64(a)*float64(t)) / (1 - math.Pow(2*float64(a)*float64(t), 2))
		// This made nicer looking waveforms for generating signal.
		//window = math.Cos(math.Pi * a * t);

		// Do we want to let it go negative?
		// I think this would happen when a > 0.5 / (filter width in symbol times)
		/*
			if window < 0 {
				//printf ("'a' is too large for range of 't'.\n");
				//window = 0;
			}
		*/
	}

	result = sinc * window

	/*
		#if DEBUGRRC
			// t should vary from - to + half of filter size in symbols.
			// Result should be 1 at t=0 and 0 at all other integer values of t.

			printf ("%.3f, %.3f, %.3f, %.3f\n", t, sinc, window, result);
		#endif
	*/
	return (result)
}

// The Root Raised Cosine (RRC) low pass filter is suppposed to minimize Intersymbol Interference (ISI).

func gen_rrc_lowpass(pfilter []float64, filter_taps int, rolloff float64, samples_per_symbol float64) {
	var t float64

	for k := 0; k < filter_taps; k++ {
		t = (float64(k) - ((float64(filter_taps) - 1.0) / 2.0)) / samples_per_symbol
		pfilter[k] = rrc(t, rolloff)
	}

	// Scale it for unity gain.

	t = 0
	for k := 0; k < filter_taps; k++ {
		t += pfilter[k]
	}
	for k := 0; k < filter_taps; k++ {
		pfilter[k] /= t
	}
}

/* end dsp.c */
