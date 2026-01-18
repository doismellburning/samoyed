package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:	Use multiple modems in parallel to increase chances
 *		of decoding less than ideal signals.
 *
 * Description:	The initial motivation was for HF SSB where mistuning
 *		causes a shift in the audio frequencies.  Here, we can
 * 		have multiple modems tuned to staggered pairs of tones
 *		in hopes that one will be close enough.
 *
 *		The overall structure opens the door to other approaches
 *		as well.  For VHF FM, the tones should always have the
 *		right frequencies but we might want to tinker with other
 *		modem parameters instead of using a single compromise.
 *
 * Originally:	The the interface application is in 3 places:
 *
 *		(a) Main program (direwolf.c or atest.c) calls
 *		    demod_init to set up modem properties and
 *		    hdlc_rec_init for the HDLC decoders.
 *
 *		(b) demod_process_sample is called for each audio sample
 *		    from the input audio stream.
 *
 *	   	(c) When a valid AX.25 frame is found, process_rec_frame,
 *		    provided by the application, in direwolf.c or atest.c,
 *		    is called.  Normally this comes from hdlc_rec.c but
 *		    there are a couple other special cases to consider.
 *		    It can be called from hdlc_rec2.c if it took a long
 *  		    time to "fix" corrupted bits.  aprs_tt.c constructs
 * 		    a fake packet when a touch tone message is received.
 *
 * New in version 0.9:
 *
 *		Put an extra layer in between which potentially uses
 *		multiple modems & HDLC decoders per channel.  The tricky
 *		part is picking the best one when there is more than one
 *		success and discarding the rest.
 *
 * New in version 1.1:
 *
 *		Several enhancements provided by Fabrice FAURE:
 *
 *		Additional types of attempts to fix a bad CRC.
 *		Optimized code to reduce execution time.
 *		Improved detection of duplicate packets from
 *		different fixup attempts.
 *		Set limit on number of packets in fix up later queue.
 *
 * New in version 1.6:
 *
 *		FX.25.  Previously a delay of a couple bits (or more accurately
 *		symbols) was fine because the decoders took about the same amount of time.
 *		Now, we can have an additional delay of up to 64 check bytes and
 *		some filler in the data portion.  We can't simply wait that long.
 *		With normal AX.25 a couple frames can come and go during that time.
 *		We want to delay the duplicate removal while FX.25 block reception
 *		is going on.
 *
 *------------------------------------------------------------------*/

// #define DIGIPEATER_C		// Why?
// #include "direwolf.h"
// #include <stdlib.h>
// #include <string.h>
// #include <assert.h>
// #include <stdio.h>
// #include <unistd.h>
// #include "ax25_pad.h"
// #include "dlq.h"
// #include "version.h"
import "C"

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"unsafe"
)

// Properties of the radio channels.

// TODO KG static struct audio_s          *save_audio_config_p;

// Candidates for further processing.

type candidate_t struct {
	packet_p    C.packet_t
	alevel      C.alevel_t
	speed_error C.float
	fec_type    fec_type_t // Type of FEC: none(0), fx25, il2p
	retries     C.retry_t  // For the old "fix bits" strategy, this is the
	// number of bits that were modified to get a good CRC.
	// It would be 0 to something around 4.
	// For FX.25, it is the number of corrected.
	// This could be from 0 thru 32.
	age   C.int
	crc   C.uint
	score C.int
}

var candidate [MAX_RADIO_CHANS][C.MAX_SUBCHANS][C.MAX_SLICERS]candidate_t

//#define PROCESS_AFTER_BITS 2		// version 1.4.  Was a little short for skew of PSK with different modem types, optional pre-filter

const PROCESS_AFTER_BITS = 3

var process_age [MAX_RADIO_CHANS]C.int

/*------------------------------------------------------------------------------
 *
 * Name:	multi_modem_init
 *
 * Purpose:	Called at application start up to initialize appropriate
 *		modems and HDLC decoders.
 *
 * Input:	Modem properties structure as filled in from the configuration file.
 *
 * Outputs:
 *
 * Description:	Called once at application startup time.
 *
 *------------------------------------------------------------------------------*/

func multi_modem_init(pa *C.struct_audio_s) {

	/*
	 * Save audio configuration for later use.
	 */

	save_audio_config_p = pa

	demod_init(save_audio_config_p)
	hdlc_rec_init(save_audio_config_p)

	for channel := C.int(0); channel < MAX_RADIO_CHANS; channel++ {
		if save_audio_config_p.chan_medium[channel] == MEDIUM_RADIO {
			if save_audio_config_p.achan[channel].baud <= 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Internal multi_modem_init error, channel=%d\n", channel)
				save_audio_config_p.achan[channel].baud = C.DEFAULT_BAUD
			}
			var real_baud = save_audio_config_p.achan[channel].baud
			if save_audio_config_p.achan[channel].modem_type == C.MODEM_QPSK {
				real_baud = save_audio_config_p.achan[channel].baud / 2
			}
			if save_audio_config_p.achan[channel].modem_type == C.MODEM_8PSK {
				real_baud = save_audio_config_p.achan[channel].baud / 3
			}

			process_age[channel] = PROCESS_AFTER_BITS * save_audio_config_p.adev[ACHAN2ADEV(channel)].samples_per_sec / real_baud
			//crc_queue_of_last_to_app[channel] = nil;
		}
	}

}

/*------------------------------------------------------------------------------
 *
 * Name:	multi_modem_process_sample
 *
 * Purpose:	Feed the sample into the proper modem(s) for the channel.
 *
 * Inputs:	channel	- Radio channel number
 *
 *		audio_sample
 *
 * Description:	In earlier versions we always had a one-to-one mapping with
 *		demodulators and HDLC decoders.
 *		This was added so we could have multiple modems running in
 *		parallel with different mark/space tones to compensate for
 *		mistuning of HF SSB signals.
 * 		It was also possible to run multiple filters, for the same
 *		tones, in parallel (e.g. ABC).
 *
 * Version 1.2:	Let's try something new for an experiment.
 *		We will have a single mark/space demodulator but multiple
 *		slicers, using different levels, each with its own HDLC decoder.
 *		We now have a separate variable, num_demod, which could be 1
 *		while num_subchan is larger.
 *
 * Version 1.3:	Go back to num_subchan with single meaning of number of demodulators.
 *		We now have separate independent variable, num_slicers, for the
 *		mark/space imbalance compensation.
 *		num_demod, while probably more descriptive, should not exist anymore.
 *
 *------------------------------------------------------------------------------*/

var dc_average [MAX_RADIO_CHANS]C.float

func multi_modem_get_dc_average(channel C.int) C.int {
	// Scale to +- 200 so it will like the deviation measurement.

	return ((C.int)((C.float)(dc_average[channel]) * (200.0 / 32767.0)))
}

func multi_modem_process_sample(channel C.int, audio_sample C.int) {

	// Accumulate an average DC bias level.
	// Shouldn't happen with a soundcard but could with mistuned SDR.

	dc_average[channel] = dc_average[channel]*0.999 + C.float(audio_sample)*0.001

	// Issue 128.  Someone ran into this.

	//assert (save_audio_config_p.achan[channel].num_subchan > 0 && save_audio_config_p.achan[channel].num_subchan <= MAX_SUBCHANS);
	//assert (save_audio_config_p.achan[channel].num_slicers > 0 && save_audio_config_p.achan[channel].num_slicers <= MAX_SLICERS);

	if save_audio_config_p.achan[channel].num_subchan <= 0 || save_audio_config_p.achan[channel].num_subchan > C.MAX_SUBCHANS ||
		save_audio_config_p.achan[channel].num_slicers <= 0 || save_audio_config_p.achan[channel].num_slicers > C.MAX_SLICERS {

		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR!  Something is seriously wrong in multi_modem_process_sample\n")
		dw_printf("channel = %d, num_subchan = %d [max %d], num_slicers = %d [max %d]\n", channel,
			save_audio_config_p.achan[channel].num_subchan, C.MAX_SUBCHANS,
			save_audio_config_p.achan[channel].num_slicers, C.MAX_SLICERS)
		dw_printf("Please report this message and include a copy of your configuration file.\n")
		os.Exit(1)
	}

	/* Formerly one loop. */
	/* 1.2: We can feed one demodulator but end up with multiple outputs. */

	/* Send same thing to all. */
	for d := C.int(0); d < save_audio_config_p.achan[channel].num_subchan; d++ {
		demod_process_sample(channel, d, audio_sample)
	}

	for subchan := C.int(0); subchan < save_audio_config_p.achan[channel].num_subchan; subchan++ {

		for slice := C.int(0); slice < save_audio_config_p.achan[channel].num_slicers; slice++ {

			if candidate[channel][subchan][slice].packet_p != nil {
				candidate[channel][subchan][slice].age++
				if candidate[channel][subchan][slice].age > process_age[channel] {
					if fx25_rec_busy(channel) > 0 {
						candidate[channel][subchan][slice].age = 0
					} else {
						pick_best_candidate(channel)
					}
				}
			}
		}
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        multi_modem_process_rec_frame
 *
 * Purpose:     This is called when we receive a frame with a valid
 *		FCS and acceptable size.
 *
 * Inputs:	channel	- Audio channel number, 0 or 1.
 *		subchan	- Which modem found it.
 *		slice	- Which slice found it.
 *		fbuf	- Pointer to first byte in HDLC frame.
 *		flen	- Number of bytes excluding the FCS.
 *		alevel	- Audio level, range of 0 - 100.
 *				(Special case, use negative to skip
 *				 display of audio level line.
 *				 Use -2 to indicate DTMF message.)
 *		retries	- Level of correction used.
 *		fec_type	- none(0), fx25, il2p
 *
 * Description:	Add to list of candidates.  Best one will be picked later.
 *
 *--------------------------------------------------------------------*/

func multi_modem_process_rec_frame(channel C.int, subchan C.int, slice C.int, fbuf *C.uchar, flen C.int, alevel C.alevel_t, retries C.retry_t, fec_type fec_type_t) {

	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)
	Assert(subchan >= 0 && subchan < C.MAX_SUBCHANS)
	Assert(slice >= 0 && slice < C.MAX_SLICERS)

	// Special encapsulation for AIS & EAS so they can be treated normally pretty much everywhere else.

	var pp C.packet_t

	switch save_audio_config_p.achan[channel].modem_type {
	case C.MODEM_AIS:
		var nmea = ais_to_nmea(C.GoBytes(unsafe.Pointer(fbuf), flen))

		// The intention is for the AIS sentences to go only to attached applications.
		// e.g. SARTrack knows how to parse the AIS sentences.

		// Put NOGATE in path so RF>IS IGates will block this.
		// TODO: Use station callsign, rather than "AIS," so we know where it is coming from,
		// if it happens to get onto RF somehow.

		var monfmt = fmt.Sprintf("AIS>%s%1d%1d,NOGATE:{%c%c%s", C.APP_TOCALL, C.MAJOR_VERSION, C.MINOR_VERSION, C.USER_DEF_USER_ID, C.USER_DEF_TYPE_AIS, string(nmea))
		pp = ax25_from_text(C.CString(monfmt), 1)

		// alevel gets in there somehow making me question why it is passed thru here.
	case C.MODEM_EAS:
		var monfmt = fmt.Sprintf("EAS>%s%1d%1d,NOGATE:{%c%c%s", C.APP_TOCALL, C.MAJOR_VERSION, C.MINOR_VERSION, C.USER_DEF_USER_ID, C.USER_DEF_TYPE_EAS, C.GoString((*C.char)(unsafe.Pointer(fbuf))))
		pp = ax25_from_text(C.CString(monfmt), 1)

		// alevel gets in there somehow making me question why it is passed thru here.
	default:
		pp = ax25_from_frame(fbuf, flen, alevel)
	}

	multi_modem_process_rec_packet(channel, subchan, slice, pp, alevel, retries, fec_type)
}

// TODO: Eliminate function above and move code elsewhere?

func multi_modem_process_rec_packet_real(channel C.int, subchan C.int, slice C.int, pp C.packet_t, alevel C.alevel_t, retries C.retry_t, fec_type fec_type_t) {

	if pp == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Unexpected internal problem in multi_modem_process_rec_packet_real\n")
		return /* oops!  why would it fail? */
	}

	/*
	 * If only one demodulator/slicer, and no FX.25 in progress,
	 * push it thru and forget about all this foolishness.
	 */
	if save_audio_config_p.achan[channel].num_subchan == 1 &&
		save_audio_config_p.achan[channel].num_slicers == 1 &&
		fx25_rec_busy(channel) == 0 {

		var drop_it = false
		if save_audio_config_p.recv_error_rate != 0 {
			var r = float64(rand.Int63n(1<<53)) / (1 << 53) // Random, 0.0 to 1.0

			//text_color_set(DW_COLOR_INFO);
			//dw_printf ("TEMP DEBUG.  recv error rate = %d\n", save_audio_config_p.recv_error_rate);

			if float64(save_audio_config_p.recv_error_rate)/100.0 > r {
				drop_it = true
				text_color_set(DW_COLOR_INFO)
				dw_printf("Intentionally dropping incoming frame.  Recv Error rate = %d per cent.\n", save_audio_config_p.recv_error_rate)
			}
		}

		if drop_it {
			ax25_delete(pp)
		} else {
			dlq_rec_frame(channel, subchan, slice, pp, alevel, fec_type, retries, C.CString(""))
		}
		return
	}

	/*
	 * Otherwise, save them up for a few bit times so we can pick the best.
	 */
	if candidate[channel][subchan][slice].packet_p != nil {
		/* Plain old AX.25: Oops!  Didn't expect it to be there. */
		/* FX.25: Quietly replace anything already there.  It will have priority. */
		ax25_delete(candidate[channel][subchan][slice].packet_p)
		candidate[channel][subchan][slice].packet_p = nil
	}

	Assert(pp != nil)

	candidate[channel][subchan][slice].packet_p = pp
	candidate[channel][subchan][slice].alevel = alevel
	candidate[channel][subchan][slice].fec_type = fec_type
	candidate[channel][subchan][slice].retries = retries
	candidate[channel][subchan][slice].age = 0
	candidate[channel][subchan][slice].crc = C.uint(ax25_m_m_crc(pp))
}

/*-------------------------------------------------------------------
 *
 * Name:        pick_best_candidate
 *
 * Purpose:     This is called when we have one or more candidates
 *		available for a certain amount of time.
 *
 * Description:	Pick the best one and send it up to the application.
 *		Discard the others.
 *
 * Rules:	We prefer one received perfectly but will settle for
 *		one where some bits had to be flipped to get a good CRC.
 *
 *--------------------------------------------------------------------*/

/* This is a suitable order for interleaved "G" demodulators. */
/* Opposite order would be suitable for multi-frequency although */
/* multiple slicers are of questionable value for HF SSB. */

// #define subchan_from_n(x) ((x) % save_audio_config_p.achan[channel].num_subchan)
func subchan_from_n(channel C.int, x C.int) C.int {
	return ((x) % save_audio_config_p.achan[channel].num_subchan)
}

// #define slice_from_n(x)   ((x) / save_audio_config_p.achan[channel].num_subchan)
func slice_from_n(channel C.int, x C.int) C.int {
	return ((x) / save_audio_config_p.achan[channel].num_subchan)
}

func pick_best_candidate(channel C.int) {

	if save_audio_config_p.achan[channel].num_slicers < 1 {
		save_audio_config_p.achan[channel].num_slicers = 1
	}
	var num_bars = save_audio_config_p.achan[channel].num_slicers * save_audio_config_p.achan[channel].num_subchan

	var spectrum [C.MAX_SUBCHANS*C.MAX_SLICERS + 1]C.char

	for n := C.int(0); n < num_bars; n++ {
		var j = subchan_from_n(channel, n)
		var k = slice_from_n(channel, n)

		/* Build the spectrum display. */

		if candidate[channel][j][k].packet_p == nil {
			spectrum[n] = '_'
		} else if candidate[channel][j][k].fec_type != fec_type_none { // FX.25 or IL2P
			// FIXME: using retries both as an enum and later int too.
			if (int)(candidate[channel][j][k].retries) <= 9 {
				spectrum[n] = '0' + C.char(candidate[channel][j][k].retries)
			} else {
				spectrum[n] = '+'
			}
		} else if candidate[channel][j][k].retries == C.RETRY_NONE { // AX.25 below
			spectrum[n] = '|'
		} else if candidate[channel][j][k].retries == C.RETRY_INVERT_SINGLE {
			spectrum[n] = ':'
		} else {
			spectrum[n] = '.'
		}

		/* Beginning score depends on effort to get a valid frame CRC. */

		if candidate[channel][j][k].packet_p == nil {
			candidate[channel][j][k].score = 0
		} else {
			if candidate[channel][j][k].fec_type != fec_type_none {
				candidate[channel][j][k].score = 9000 - 100*C.int(candidate[channel][j][k].retries) // has FEC
			} else {
				/* Originally, this produced 0 for the PASSALL case. */
				/* This didn't work so well when looking for the best score. */
				/* Around 1.3 dev H, we add an extra 1 in here so the minimum */
				/* score should now be 1 for anything received.  */

				candidate[channel][j][k].score = C.RETRY_MAX*1000 - C.int(candidate[channel][j][k].retries*1000) + 1
			}
		}
	}

	// FIXME: IL2p & FX.25 don't have CRC calculated. Must fill it in first.

	/* Bump it up slightly if others nearby have the same CRC. */

	for n := C.int(0); n < num_bars; n++ {
		var j = subchan_from_n(channel, n)
		var k = slice_from_n(channel, n)

		if candidate[channel][j][k].packet_p != nil {

			for m := C.int(0); m < num_bars; m++ {

				var mj = subchan_from_n(channel, m)
				var mk = slice_from_n(channel, m)

				if m != n && candidate[channel][mj][mk].packet_p != nil {
					if candidate[channel][j][k].crc == candidate[channel][mj][mk].crc {
						candidate[channel][j][k].score += (num_bars + 1) - C.int(math.Abs(float64(m-n)))
					}
				}
			}
		}
	}

	var best_n C.int = 0
	var best_score C.int = 0

	for n := C.int(0); n < num_bars; n++ {
		var j = subchan_from_n(channel, n)
		var k = slice_from_n(channel, n)

		if candidate[channel][j][k].packet_p != nil {
			if candidate[channel][j][k].score > best_score {
				best_score = candidate[channel][j][k].score
				best_n = n
			}
		}
	}

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("\n%s\n", spectrum);

		for (n = 0; n < num_bars; n++) {
		  j = subchan_from_n(n);
		  k = slice_from_n(n);

		  if (candidate[channel][j][k].packet_p == nil) {
		    dw_printf ("%d.%d.%d: ptr=%p\n", channel, j, k,
			candidate[channel][j][k].packet_p);
		  } else {
		    dw_printf ("%d.%d.%d: ptr=%p, fec_type=%d, retry=%d, age=%3d, crc=%04x, score=%d  %s\n", channel, j, k,
			candidate[channel][j][k].packet_p,
			(int)(candidate[channel][j][k].fec_type),
			(int)(candidate[channel][j][k].retries),
			candidate[channel][j][k].age,
			candidate[channel][j][k].crc,
			candidate[channel][j][k].score,
			(n == best_n) ? "***" : "");
		  }
		}
	#endif
	*/

	if best_score == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Unexpected internal problem in pick_best_candidate.  How can best score be zero?\n")
	}

	/*
	 * send the best one along.
	 */

	/* Delete those not chosen. */

	for n := C.int(0); n < num_bars; n++ {
		var j = subchan_from_n(channel, n)
		var k = slice_from_n(channel, n)
		if n != best_n && candidate[channel][j][k].packet_p != nil {
			ax25_delete(candidate[channel][j][k].packet_p)
			candidate[channel][j][k].packet_p = nil
		}
	}

	/* Pass along one. */

	var j = subchan_from_n(channel, best_n)
	var k = slice_from_n(channel, best_n)

	var drop_it = false
	if save_audio_config_p.recv_error_rate != 0 {
		var r = float64(rand.Int63n(1<<53)) / (1 << 53) // Random, 0.0 to 1.0

		//text_color_set(DW_COLOR_INFO);
		//dw_printf ("TEMP DEBUG.  recv error rate = %d\n", save_audio_config_p.recv_error_rate);

		if float64(save_audio_config_p.recv_error_rate)/100.0 > r {
			drop_it = true
			text_color_set(DW_COLOR_INFO)
			dw_printf("Intentionally dropping incoming frame.  Recv Error rate = %d per cent.\n", save_audio_config_p.recv_error_rate)
		}
	}

	if drop_it {
		ax25_delete(candidate[channel][j][k].packet_p)
		candidate[channel][j][k].packet_p = nil
	} else {
		Assert(candidate[channel][j][k].packet_p != nil)
		dlq_rec_frame(channel, j, k,
			candidate[channel][j][k].packet_p,
			candidate[channel][j][k].alevel,
			candidate[channel][j][k].fec_type,
			(candidate[channel][j][k].retries),
			&spectrum[0])

		/* Someone else owns it now and will delete it later. */
		candidate[channel][j][k].packet_p = nil
	}

	/* Clear in preparation for next time. */

	candidate[channel] = [C.MAX_SUBCHANS][C.MAX_SLICERS]candidate_t{} // TODO KG Gotta be a nicer way to do this

} /* end pick_best_candidate */

/* end multi_modem.c */
