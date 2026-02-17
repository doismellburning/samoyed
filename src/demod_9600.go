package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Demodulator for baseband signal.
 *		This is used for AX.25 (with scrambling) and IL2P without.
 *
 * Input:	Audio samples from either a file or the "sound card."
 *
 * Outputs:	Calls hdlc_rec_bit() for each bit demodulated.
 *
 *---------------------------------------------------------------*/

import (
	"math"
)

var DCD_CONFIG_9600 = &DCDConfig{
	// Hysteresis: Can miss 0 out of 32 for detecting lock.
	// This is best for actual on-the-air signals.
	// Still too many brief false matches.
	DCD_THRESH_ON:  32,
	DCD_THRESH_OFF: 8,
	DCD_GOOD_WIDTH: 1024,
}

var slice_point [MAX_SUBCHANS]float64

/* Add sample to buffer and shift the rest down. */

func push_sample(val float64, buff []float64, size int) {
	copy(buff[1:], buff[:size-1])
	buff[0] = val
}

/* FIR filter kernel. */

func convolve(data, filter []float64, filter_size int) float64 {
	var sum = 0.0

	for j := range filter_size {
		sum += filter[j] * data[j]
	}

	return (sum)
}

// Automatic Gain control - used when we have a single slicer.
//
// The first step is to create an envelope for the peak and valley
// of the mark or space amplitude.  We need to keep track of the valley
// because it does not go down to zero when the tone is not present.
// We want to find the difference between tone present and not.
//
// We use an IIR filter with fast attack and slow decay which only considers the past.
// Perhaps an improvement could be obtained by looking in the future as well.
//

// Result should settle down to 1 unit peak to peak.  i.e. -0.5 to +0.5

func agc(in, fast_attack, slow_decay float64, inPeak, inValley float64) (float64, float64, float64) {

	var outPeak float64
	var outValley float64

	if in >= inPeak {
		outPeak = in*fast_attack + inPeak*(1.0-fast_attack)
	} else {
		outPeak = in*slow_decay + inPeak*(1.0-slow_decay)
	}

	if in <= inValley {
		outValley = in*fast_attack + inValley*(1.0-fast_attack)
	} else {
		outValley = in*slow_decay + inValley*(1.0-slow_decay)
	}

	if outPeak > outValley {
		return outPeak, outValley, (in - 0.5*(outPeak+outValley)) / (outPeak - outValley)
	}

	return outPeak, outValley, 0.0
}

/*------------------------------------------------------------------
 *
 * Name:        demod_9600_init
 *
 * Purpose:     Initialize the 9600 (or higher) baud demodulator.
 *
 * Inputs:      modem_type	- Determines whether scrambling is used.
 *
 *		samples_per_sec	- Number of samples per second for audio.
 *
 *		upsample	- Factor to upsample the incoming stream.
 *				  After a lot of experimentation, I discovered that
 *				  it works better if the data is upsampled.
 *				  This reduces the jitter for PLL synchronization.
 *
 *		baud		- Data rate in bits per second.
 *
 *		D		- Address of demodulator state.
 *
 * Returns:     None
 *
 *----------------------------------------------------------------*/

func demod_9600_init(modem_type modem_t, original_sample_rate int, upsample int, baud int, D *demodulator_state_s) {

	if upsample < 1 {
		upsample = 1
	}
	if upsample > 4 {
		upsample = 4
	}

	*D = demodulator_state_s{} //nolint:exhaustruct

	D.modem_type = modem_type
	D.num_slicers = 1

	// Multiple profiles in future?

	//	switch (profile) {

	//	  case 'J':			// upsample x2 with filtering.
	//	  case 'K':			// upsample x3 with filtering.
	//	  case 'L':			// upsample x4 with filtering.

	D.lp_filter_width_sym = 1.0 // -U4 = 61 	4.59 samples/symbol

	// Works best with odd number in some tests.  Even is better in others.
	//D.lp_filter_taps = ((int) (0.5f * ( D.lp_filter_width_sym * (float)original_sample_rate / (float)baud ))) * 2 + 1;

	// Just round to nearest integer.
	D.lp_filter_taps = int((float64(D.lp_filter_width_sym) * float64(original_sample_rate) / float64(baud)) + 0.5)

	D.lp_window = BP_WINDOW_COSINE

	D.lpf_baud = 1.00

	D.agc_fast_attack = 0.080
	D.agc_slow_decay = 0.00012

	D.pll_locked_inertia = 0.89
	D.pll_searching_inertia = 0.67

	//	    break;
	//	}

	/* TODO KG
	   #if 0
	   	text_color_set(DW_COLOR_DEBUG);
	   	dw_printf ("----------  %s  (%d, %d)  -----------\n", __func__, samples_per_sec, baud);
	   	dw_printf ("filter_len_bits = %.2f\n", D.lp_filter_width_sym);
	   	dw_printf ("lp_filter_taps = %d\n", D.lp_filter_taps);
	   	dw_printf ("lp_window = %d\n", D.lp_window);
	   	dw_printf ("lpf_baud = %.2f\n", D.lpf_baud);
	   	dw_printf ("samples per bit = %.1f\n", (double)samples_per_sec / baud);
	   #endif
	*/

	// PLL needs to use the upsampled rate.

	D.pll_step_per_sample = int32(math.Round(float64(TICKS_PER_PLL_CYCLE * float64(baud) / float64(original_sample_rate*upsample))))

	/* TODO KG
	#ifdef TUNE_LP_WINDOW
		D.lp_window = TUNE_LP_WINDOW;
	#endif

	#if TUNE_LP_FILTER_SIZE
		D.lp_filter_taps = TUNE_LP_FILTER_SIZE;
	#endif

	#ifdef TUNE_LPF_BAUD
		D.lpf_baud = TUNE_LPF_BAUD;
	#endif

	#ifdef TUNE_AGC_FAST
		D.agc_fast_attack = TUNE_AGC_FAST;
	#endif

	#ifdef TUNE_AGC_SLOW
		D.agc_slow_decay = TUNE_AGC_SLOW;
	#endif

	#if defined(TUNE_PLL_LOCKED)
		D.pll_locked_inertia = TUNE_PLL_LOCKED;
	#endif

	#if defined(TUNE_PLL_SEARCHING)
		D.pll_searching_inertia = TUNE_PLL_SEARCHING;
	#endif
	*/

	// Initial filter (before scattering) is based on upsampled rate.

	var fc = float64(baud) * D.lpf_baud / float64(original_sample_rate*upsample)

	//dw_printf ("demod_9600_init: call gen_lowpass(fc=%.2f, , size=%d, )\n", fc, D.lp_filter_taps);

	gen_lowpass(fc, D.u.bb.lp_filter[:], D.lp_filter_taps*upsample, D.lp_window)

	// New in 1.7 -
	// Use a polyphase filter to reduce the CPU load.
	// Originally I used zero stuffing to upsample.
	// Here is the general idea.
	//
	// Suppose the input samples are 1 2 3 4 5 6 7 8 9 ...
	// Filter coefficients are a b c d e f g h i ...
	//
	// With original sampling rate, the filtering would involve multiplying and adding:
	//
	// 	1a 2b 3c 4d 5e 6f ...
	//
	// When upsampling by 3, each of these would need to be evaluated
	// for each audio sample:
	//
	//	1a 0b 0c 2d 0e 0f 3g 0h 0i ...
	//	0a 1b 0c 0d 2e 0f 0g 3h 0i ...
	//	0a 0b 1c 0d 0e 2f 0g 0h 3i ...
	//
	// 2/3 of the multiplies are always by a stuffed zero.
	// We can do this more efficiently by removing them.
	//
	//	1a       2d       3g       ...
	//	   1b       2e       3h    ...
	//	      1c       2f       3i ...
	//
	// We scatter the original filter across multiple shorter filters.
	// Each input sample cycles around them to produce the upsampled rate.
	//
	//	a d g ...
	//	b e h ...
	//	c f i ...
	//
	// There are countless sources of information DSP but this one is unique
	// in that it is a college course that mentions APRS.
	// https://www2.eecs.berkeley.edu/Courses/EE123
	//
	// Was the effort worthwhile?  Times on an RPi 3.
	//
	// command:   atest -B9600  ~/walkabout9600[abc]-compressed*.wav
	//
	// These are 3 recordings of a portable system being carried out of
	// range and back in again.  It is a real world test for weak signals.
	//
	//	options		num decoded	seconds		x realtime
	//			1.6	1.7	1.6	1.7	1.6	1.7
	//			---	---	---	---	---	---
	//	-P-		171	172	23.928	17.967	14.9	19.9
	//	-P+		180	180	54.688	48.772	6.5	7.3
	//	-P- -F1		177	178	32.686	26.517	10.9	13.5
	//
	// So, it turns out that -P+ doesn't have a dramatic improvement, only
	// around 4%, for drastically increased CPU requirements.
	// Maybe we should turn that off by default, especially for ARM.
	//

	var k = 0
	for i := 0; i < D.lp_filter_taps; i++ {
		D.u.bb.lp_polyphase_1[i] = D.u.bb.lp_filter[k]
		k++
		if upsample >= 2 {
			D.u.bb.lp_polyphase_2[i] = D.u.bb.lp_filter[k]
			k++
			if upsample >= 3 {
				D.u.bb.lp_polyphase_3[i] = D.u.bb.lp_filter[k]
				k++
				if upsample >= 4 {
					D.u.bb.lp_polyphase_4[i] = D.u.bb.lp_filter[k]
					k++
				}
			}
		}
	}

	/* Version 1.2: Experiment with different slicing levels. */
	// Really didn't help that much because we should have a symmetrical signal.

	for j := 0; j < MAX_SUBCHANS; j++ {
		slice_point[j] = 0.02 * float64(j-0.5*(MAX_SUBCHANS-1))
		//dw_printf ("slice_point[%d] = %+5.2f\n", j, slice_point[j]);
	}

} /* end fsk_demod_init */

/*-------------------------------------------------------------------
 *
 * Name:        demod_9600_process_sample
 *
 * Purpose:     (1) Filter & slice the signal.
 *		(2) Descramble it.
 *		(2) Recover clock and data.
 *
 * Inputs:	chan	- Audio channel.  0 for left, 1 for right.
 *
 *		sam	- One sample of audio.
 *			  Should be in range of -32768 .. 32767.
 *
 * Returns:	None
 *
 * Descripion:	"9600 baud" packet is FSK for an FM voice transceiver.
 *		By the time it gets here, it's really a baseband signal.
 *		At one extreme, we could have a 4800 Hz square wave.
 *		A the other extreme, we could go a considerable number
 *		of bit times without any transitions.
 *
 *		The trick is to extract the digital data which has
 *		been distorted by going thru voice transceivers not
 *		intended to pass this sort of "audio" signal.
 *
 *		For G3RUH mode, data is "scrambled" to reduce the amount of DC bias.
 *		The data stream must be unscrambled at the receiving end.
 *
 *		We also have a digital phase locked loop (PLL)
 *		to recover the clock and pick out data bits at
 *		the proper rate.
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
 *		After experimentation, I found that this works better if
 *		the original signal is upsampled by 2x or even 4x.
 *
 * References:	9600 Baud Packet Radio Modem Design
 *		http://www.amsat.org/amsat/articles/g3ruh/109.html
 *
 *		The KD2BD 9600 Baud Modem
 *		http://www.amsat.org/amsat/articles/kd2bd/9k6modem/
 *
 *		9600 Baud Packet Handbook
 * 		ftp://ftp.tapr.org/general/9600baud/96man2x0.txt
 *
 *
 *--------------------------------------------------------------------*/

func demod_9600_process_sample(channel int, sam int, upsample int, D *demodulator_state_s) {

	/* TODO KG
	#if DEBUG4
		static FILE *demod_log_fp = NULL;
		static int log_file_seq = 0;		// Part of log file name
	#endif
	*/

	var subchan = 0

	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)
	Assert(subchan >= 0 && subchan < MAX_SUBCHANS)

	/* Scale to nice number for convenience. */
	/* Consistent with the AFSK demodulator, we'd like to use */
	/* only half of the dynamic range to have some headroom. */
	/* i.e.  input range +-16k becomes +-1 here and is */
	/* displayed in the heard line as audio level 100. */

	var fsam = float64(sam) / 16384.0

	// Low pass filter
	push_sample(fsam, D.u.bb.audio_in[:], D.lp_filter_taps)

	fsam = convolve(D.u.bb.audio_in[:], D.u.bb.lp_polyphase_1[:], D.lp_filter_taps)
	process_filtered_sample(channel, fsam, D)
	if upsample >= 2 {
		fsam = convolve(D.u.bb.audio_in[:], D.u.bb.lp_polyphase_2[:], D.lp_filter_taps)
		process_filtered_sample(channel, fsam, D)
		if upsample >= 3 {
			fsam = convolve(D.u.bb.audio_in[:], D.u.bb.lp_polyphase_3[:], D.lp_filter_taps)
			process_filtered_sample(channel, fsam, D)
			if upsample >= 4 {
				fsam = convolve(D.u.bb.audio_in[:], D.u.bb.lp_polyphase_4[:], D.lp_filter_taps)
				process_filtered_sample(channel, fsam, D)
			}
		}
	}
}

func process_filtered_sample(channel int, fsam float64, D *demodulator_state_s) {

	var subchannel = 0

	/*
	 * Version 1.2: Capture the post-filtering amplitude for display.
	 * This is similar to the AGC without the normalization step.
	 * We want decay to be substantially slower to get a longer
	 * range idea of the received audio.
	 * For AFSK, we keep mark and space amplitudes.
	 * Here we keep + and - peaks because there could be a DC bias.
	 */

	// TODO:  probably no need for this.  Just use  D.m_peak, D.m_valley

	if fsam >= D.alevel_mark_peak {
		D.alevel_mark_peak = fsam*D.quick_attack + D.alevel_mark_peak*(1.0-D.quick_attack)
	} else {
		D.alevel_mark_peak = fsam*D.sluggish_decay + D.alevel_mark_peak*(1.0-D.sluggish_decay)
	}

	if fsam <= D.alevel_space_peak {
		D.alevel_space_peak = fsam*D.quick_attack + D.alevel_space_peak*(1.0-D.quick_attack)
	} else {
		D.alevel_space_peak = fsam*D.sluggish_decay + D.alevel_space_peak*(1.0-D.sluggish_decay)
	}

	/*
	 * The input level can vary greatly.
	 * More importantly, there could be a DC bias which we need to remove.
	 *
	 * Normalize the signal with automatic gain control (AGC).
	 * This works by looking at the minimum and maximum signal peaks
	 * and scaling the results to be roughly in the -1.0 to +1.0 range.
	 */
	var demod_data bool /* Still scrambled. */

	var demod_out float64
	D.m_peak, D.m_valley, demod_out = agc(fsam, D.agc_fast_attack, D.agc_slow_decay, D.m_peak, D.m_valley)

	// TODO: There is potential for multiple decoders with one filter.

	//dw_printf ("peak=%.2f valley=%.2f fsam=%.2f norm=%.2f\n", D.m_peak, D.m_valley, fsam, norm);

	if D.num_slicers <= 1 {

		/* Normal case of one demodulator to one HDLC decoder. */
		/* Demodulator output is difference between response from two filters. */
		/* AGC should generally keep this around -1 to +1 range. */

		demod_data = demod_out > 0
		nudge_pll_9600(channel, subchannel, 0, demod_out, D)
	} else {
		/* Multiple slicers each feeding its own HDLC decoder. */

		for slice := int(0); slice < D.num_slicers; slice++ {
			demod_data = demod_out-slice_point[slice] > 0
			nudge_pll_9600(channel, subchannel, slice, demod_out-slice_point[slice], D)
		}
	}

	// demod_data is used only for debug out.
	// suppress compiler warning about it not being used.
	_ = demod_data

	/* TODO KG
	#if DEBUG4

		if (chan == 0) {

		  if (1) {
		  //if (D.slicer[slice].data_detect) {
		    char fname[30];
		    int slice = 0;

		    if (demod_log_fp == NULL) {
		      log_file_seq++;
		      snprintf (fname, sizeof(fname), "demod/%04d.csv", log_file_seq);
		      //if (log_file_seq == 1) mkdir ("demod", 0777);
		      if (log_file_seq == 1) mkdir ("demod");

		      demod_log_fp = fopen (fname, "w");
		      text_color_set(DW_COLOR_DEBUG);
		      dw_printf ("Starting demodulator log file %s\n", fname);
		      fprintf (demod_log_fp, "Audio, Filtered,  Max,  Min, Normalized, Sliced, Clock\n");
		    }

		    fprintf (demod_log_fp, "%.3f, %.3f, %.3f, %.3f, %.3f, %d, %.2f\n",
				fsam + 6,
				fsam + 4,
				D.m_peak + 4,
				D.m_valley + 4,
				demod_out + 2,
				demod_data + 2,
				(D.slicer[slice].data_clock_pll & 0x80000000) ? .5 : .0);

		    fflush (demod_log_fp);
		  } else {
		    if (demod_log_fp != NULL) {
		      fclose (demod_log_fp);
		      demod_log_fp = NULL;
		    }
		  }
		}
	#endif
	*/

} /* end demod_9600_process_sample */

/*-------------------------------------------------------------------
 *
 * Name:        nudge_pll
 *
 * Purpose:	Update the PLL state for each audio sample.
 *
 *		(2) Descramble it.
 *		(2) Recover clock and data.
 *
 * Inputs:	chan	- Audio channel.  0 for left, 1 for right.
 *
 *		subchan	- Which demodulator.  We could have several running in parallel.
 *
 *		slice	- Determines which Slicing level & HDLC decoder to use.
 *
 *		demod_out_f - Demodulator output, possibly shifted by slicing level
 *				It will be compared with 0.0 to bit binary value out.
 *
 *		D	- Demodulator state for this channel / subchannel.
 *
 * Returns:	None
 *
 * Description:	A PLL is used to sample near the centers of the data bits.
 *
 *		D.data_clock_pll is a SIGNED 32 bit variable.
 *		When it overflows from a large positive value to a negative value, we
 *		sample a data bit from the demodulated signal.
 *
 *		Ideally, the the demodulated signal transitions should be near
 *		zero we we sample mid way between the transitions.
 *
 *		Nudge the PLL by removing some small fraction from the value of
 *		data_clock_pll, pushing it closer to zero.
 *
 *		This adjustment will never change the sign so it won't cause
 *		any erratic data bit sampling.
 *
 *		If we adjust it too quickly, the clock will have too much jitter.
 *		If we adjust it too slowly, it will take too long to lock on to a new signal.
 *
 *		I don't think the optimal value will depend on the audio sample rate
 *		because this happens for each transition from the demodulator.
 *
 * Version 1.4:	Previously, we would always pull the PLL phase toward 0 after
 *		after a zero crossing was detetected.  This adds extra jitter,
 *		especially when the ratio of audio sample rate to baud is low.
 *		Now, we interpolate between the two samples to get an estimate
 *		on when the zero crossing happened.  The PLL is pulled toward
 *		this point.
 *
 *		Results???  TBD
 *
 * Version 1.6:	New experiment where filter size to extract clock is not the same
 *		as filter to extract the data bit value.
 *
 *--------------------------------------------------------------------*/

func nudge_pll_9600(channel int, subchannel int, slice int, demod_out_f float64, D *demodulator_state_s) {
	D.slicer[slice].prev_d_c_pll = D.slicer[slice].data_clock_pll

	// Perform the add as unsigned to avoid signed overflow error.
	D.slicer[slice].data_clock_pll = (int32)((uint32)(D.slicer[slice].data_clock_pll) + (uint32)(D.pll_step_per_sample))

	if D.slicer[slice].prev_d_c_pll > 1000000000 && D.slicer[slice].data_clock_pll < -1000000000 {

		/* Overflow.  Was large positive, wrapped around, now large negative. */

		hdlc_rec_bit_new(channel, subchannel, slice, IfThenElse(demod_out_f > 0, 1, 0), D.modem_type == MODEM_SCRAMBLE, D.slicer[slice].lfsr,
			&(D.slicer[slice].pll_nudge_total), &(D.slicer[slice].pll_symbol_count))
		D.slicer[slice].pll_symbol_count++

		pll_dcd_each_symbol2(DCD_CONFIG_9600, D, channel, subchannel, slice)
	}

	/*
	 * Zero crossing?
	 */
	if (D.slicer[slice].prev_demod_out_f < 0 && demod_out_f > 0) ||
		(D.slicer[slice].prev_demod_out_f > 0 && demod_out_f < 0) {

		// Note:  Test for this demodulator, not overall for channel.

		pll_dcd_signal_transition2(DCD_CONFIG_9600, D, slice, int(D.slicer[slice].data_clock_pll))

		var target = float64(D.pll_step_per_sample) * demod_out_f / (demod_out_f - D.slicer[slice].prev_demod_out_f)

		var before = D.slicer[slice].data_clock_pll // Treat as signed.
		if D.slicer[slice].data_detect != 0 {
			D.slicer[slice].data_clock_pll = int32(float64(D.slicer[slice].data_clock_pll)*D.pll_locked_inertia + target*(1.0-D.pll_locked_inertia))
		} else {
			D.slicer[slice].data_clock_pll = int32(float64(D.slicer[slice].data_clock_pll)*D.pll_searching_inertia + target*(1.0-D.pll_searching_inertia))
		}
		D.slicer[slice].pll_nudge_total += int64(D.slicer[slice].data_clock_pll) - int64(before)
	}

	/* TODO KG
	#if DEBUG5

		//if (chan == 0) {
		if (D.slicer[slice].data_detect) {

		  char fname[30];


		  if (demod_log_fp == NULL) {
		    seq++;
		    snprintf (fname, sizeof(fname), "demod96/%04d.csv", seq);
		    if (seq == 1) mkdir ("demod96"
	#ifndef __WIN32__
						, 0777
	#endif
							);

		    demod_log_fp = fopen (fname, "w");
		    text_color_set(DW_COLOR_DEBUG);
		    dw_printf ("Starting 9600 decoder log file %s\n", fname);
		    fprintf (demod_log_fp, "Audio, Peak, Valley, Demod, SData, Descram, Clock\n");
		  }
		  fprintf (demod_log_fp, "%.3f, %.3f, %.3f, %.3f, %.2f, %.2f, %.2f\n",
				0.5f * fsam + 3.5,
				0.5f * D.m_peak + 3.5,
				0.5f * D.m_valley + 3.5,
				0.5f * demod_out + 2.0,
				demod_data ? 1.35 : 1.0,
				descram ? .9 : .55,
				(D.data_clock_pll & 0x80000000) ? .1 : .45);
		} else {
		  if (demod_log_fp != NULL) {
		    fclose (demod_log_fp);
		    demod_log_fp = NULL;
		  }
		}
		//}

	#endif
	*/

	/*
	 * Remember demodulator output (pre-descrambling) so we can compare next time
	 * for the DPLL sync.
	 */
	D.slicer[slice].prev_demod_out_f = demod_out_f

} /* end nudge_pll */

/* end demod_9600.c */
