package direwolf

/********************************************************************************
 *
 * Purpose:	Extract HDLC frames from a stream of bits.
 *
 *******************************************************************************/

// #include <stdio.h>
// #include <string.h>
// #include <stdint.h>          // uint64_t
import "C"

import (
	"slices"
)

/* Undo data scrambling for 9600 baud. */

func descramble(in int, state *int) int {

	var out = (in ^ (*state >> 16) ^ (*state >> 11)) & 1
	*state = (*state << 1) | (in & 1)
	return (out)
}

//#define TEST 1				/* Define for unit testing. */

//#define DEBUG3 1				/* monitor the data detect signal. */

/*
 * This is the current state of the HDLC decoder.
 *
 * It is possible to run multiple decoders concurrently by
 * having a separate set of state variables for each.
 *
 * Should have a reset function instead of initializations here.
 */

type hdlc_state_s struct {
	prev_raw bool /* Keep track of previous bit so */
	/* we can look for transitions. */

	lfsr int /* Descrambler shift register for 9600 baud. */

	prev_descram int /* Previous descrambled for 9600 baud. */

	pat_det byte /* 8 bit pattern detector shift register. */
	/* See below for more details. */

	flag4_det uint /* Last 32 raw bits to look for 4 */
	/* flag patterns in a row. */

	oacc byte /* Accumulator for building up an octet. */

	olen int /* Number of bits in oacc. */
	/* When this reaches 8, oacc is copied */
	/* to the frame buffer and olen is zeroed. */
	/* The value of -1 is a special case meaning */
	/* bits should not be accumulated. */

	frame_buf [MAX_FRAME_LEN]byte
	/* One frame is kept here. */

	frame_len int /* Number of octets in frame_buf. */
	/* Should be in range of 0 .. MAX_FRAME_LEN. */

	rrbb *rrbb_t /* Handle for bit array for raw received bits. */

	eas_acc uint64 /* Accumulate most recent 64 bits received for EAS. */

	eas_gathering bool /* Decoding in progress. */

	eas_plus_found bool /* "+" seen, indicating end of geographical area list. */

	eas_fields_after_plus int /* Number of "-" characters after the "+". */
}

var hdlc_state [MAX_RADIO_CHANS][MAX_SUBCHANS][MAX_SLICERS]*hdlc_state_s

var num_subchannel [MAX_RADIO_CHANS]int //TODO1.2 use ptr rather than copy.

var composite_dcd [MAX_RADIO_CHANS][MAX_SUBCHANS + 1][MAX_SLICERS]bool

/***********************************************************************************
 *
 * Name:	hdlc_rec_init
 *
 * Purpose:	Call once at the beginning to initialize.
 *
 * Inputs:	None.
 *
 ***********************************************************************************/

var hdlcRecWasInit = false

var g_audio_p *audio_s

func hdlc_rec_init(pa *audio_s) {

	//text_color_set(DW_COLOR_DEBUG);
	//dw_printf ("hdlc_rec_init (%p) \n", pa);

	Assert(pa != nil)
	g_audio_p = pa

	for ch := 0; ch < MAX_RADIO_CHANS; ch++ {

		if pa.chan_medium[ch] == MEDIUM_RADIO {

			num_subchannel[ch] = pa.achan[ch].num_subchan

			Assert(num_subchannel[ch] >= 1 && num_subchannel[ch] <= MAX_SUBCHANS)

			for sub := 0; sub < num_subchannel[ch]; sub++ {
				for slice := 0; slice < MAX_SLICERS; slice++ {

					var H = new(hdlc_state_s)
					hdlc_state[ch][sub][slice] = H

					H.olen = -1

					// TODO: FIX13 wasteful if not needed.
					// Should loop on number of slicers, not max.

					H.rrbb = rrbb_new(ch, sub, slice, pa.achan[ch].modem_type == MODEM_SCRAMBLE, H.lfsr, H.prev_descram)
				}
			}
		}
	}
	hdlc_rec2_init(pa)
	hdlcRecWasInit = true
}

/* Own copy of random number generator so we can get */
/* same predictable results on different operating systems. */
/* TODO: Consolidate multiple copies somewhere. */

var hdlcRecRandSeed int32 = 1
var hdlcRecRandMax int32 = 0x7fffffff

func hdlcRecRand() int32 {
	hdlcRecRandSeed = int32((uint32(hdlcRecRandSeed)*1103515245)+12345) & hdlcRecRandMax
	return hdlcRecRandSeed
}

/***********************************************************************************
 *
 * Name:	eas_rec_bit
 *
 * Purpose:	Extract EAS trasmissions from a stream of bits.
 *
 * Inputs:	channel	- Channel number.
 *
 *		subchannel	- This allows multiple demodulators per channel.
 *
 *		slice	- Allows multiple slicers per demodulator (subchannel).
 *
 *		raw 	- One bit from the demodulator.
 *			  should be 0 or 1.
 *
 *		future_use - Not implemented yet.  PSK already provides it.
 *
 *
 * Description:	This is called once for each received bit.
 *		For each valid transmission, process_rec_frame()
 *		is called for further processing.
 *
 ***********************************************************************************/

const PREAMBLE = 0xabababababababab
const PREAMBLE_ZCZC = 0x435a435aabababab
const PREAMBLE_NNNN = 0x4e4e4e4eabababab
const EAS_MAX_LEN = 268 // Not including preamble.  Up to 31 geographic areas.

func eas_rec_bit(channel int, subchannel int, slice int, raw int, future_use int) {

	/*
	 * Different state information for each channel / subchannel / slice.
	 */
	var H = hdlc_state[channel][subchannel][slice]

	//dw_printf ("slice %d = %d\n", slice, raw);

	// Accumulate most recent 64 bits.

	H.eas_acc >>= 1
	if raw != 0 {
		H.eas_acc |= 0x8000000000000000
	}

	var done = false

	if H.eas_acc == PREAMBLE_ZCZC {
		//dw_printf ("ZCZC\n");
		H.olen = 0
		H.eas_gathering = true
		H.eas_plus_found = false
		H.eas_fields_after_plus = 0
		copy(H.frame_buf[:], []byte("ZCZC"))
		H.frame_len = 4
	} else if H.eas_acc == PREAMBLE_NNNN {
		//dw_printf ("NNNN\n");
		H.olen = 0
		H.eas_gathering = true
		copy(H.frame_buf[:], []byte("NNNN"))
		H.frame_len = 4
		done = true
	} else if H.eas_gathering {
		H.olen++
		if H.olen == 8 {
			H.olen = 0
			var ch = byte(H.eas_acc >> 56)
			H.frame_buf[H.frame_len] = ch
			H.frame_len++
			H.frame_buf[H.frame_len] = 0
			//dw_printf ("frame_buf = %s\n", H.frame_buf);

			// What characters are acceptable?
			// Only ASCII is allowed.  i.e. the MSB must be 0.
			// The examples show only digits but the geographical area can
			// contain anything in range of '!' to DEL or CR or LF.
			// There are no restrictions listed for the originator and
			// examples contain a slash.
			// It's not clear if a space can occur in other places.

			if !((ch >= ' ' && ch <= 0x7f) || ch == '\r' || ch == '\n') {
				//#define DEBUG_E 1
				/*
				   #ifdef DEBUG_E
				   	      dw_printf ("reject %d invalid character = %s\n", slice, H.frame_buf);
				   #endif
				*/
				H.eas_gathering = false
				return
			}
			if H.frame_len > EAS_MAX_LEN { // FIXME: look for other places with max length
				/*
					#ifdef DEBUG_E
						      dw_printf ("reject %d too long = %s\n", slice, H.frame_buf);
					#endif
				*/
				H.eas_gathering = false
				return
			}
			if ch == '+' {
				H.eas_plus_found = true
				H.eas_fields_after_plus = 0
			}
			if H.eas_plus_found && ch == '-' {
				H.eas_fields_after_plus++
				if H.eas_fields_after_plus == 3 {
					done = true // normal case
				}
			}
		}
	}

	if done {
		/*
			#ifdef DEBUG_E
				  dw_printf ("frame_buf %d = %s\n", slice, H.frame_buf);
			#endif
		*/
		var alevel = demod_get_audio_level(C.int(channel), C.int(subchannel))
		multi_modem_process_rec_frame(channel, subchannel, slice, H.frame_buf[:H.frame_len], alevel, 0, 0)
		H.eas_gathering = false
	}

} // end eas_rec_bit

/*

EAS has no error detection.
Maybe that doesn't matter because we would normally be dealing with a reasonable
VHF FM or TV signal.
Let's see what happens when we intentionally introduce errors.
When some match and others don't, the multislice voting should give preference
to those matching others.

	$ src/atest -P+ -B EAS -e 3e-3 ../../ref-doc/EAS/same.wav
	Demodulator profile set to "+"
	96000 samples per second.  16 bits per sample.  1 audio channels.
	2079360 audio bytes in file.  Duration = 10.8 seconds.
	Fix Bits level = 0
	Channel 0: 521 baud, AFSK 2083 & 1563 Hz, D+, 96000 sample rate / 3.

case 1:  Slice 6 is different than others (EQS vs. EAS) so we want one of the others that match.
	 Slice 3 has an unexpected character (in 0120u7) so it is a mismatch.
	 At this point we are not doing validity checking other than all printable characters.

	 We are left with 0 & 4 which don't match (012057 vs. 012077).
	 So I guess we don't have any two that match so it is a toss up.

	reject 7 invalid character = ZCZC-EAS-RWT-0120▒
	reject 5 invalid character = ZCZC-ECW-RWT-012057-012081-012101-012103-012115+003
	frame_buf 6 = ZCZC-EQS-RWT-012057-012081-012101-012103-012115+0030-2780415-WTSP/TV-
	frame_buf 4 = ZCZC-EAS-RWT-012077-012081-012101-012103-012115+0030-2780415-WTSP/TV-
	frame_buf 3 = ZCZC-EAS-RWT-0120u7-012281-012101-012103-092115+0038-2780415-VTSP/TV-
	frame_buf 0 = ZCZC-EAS-RWT-012057-412081-012101-012103-012115+0030-2780415-WTSP/TV-

	DECODED[1] 0:01.313 EAS audio level = 194(106/108)     |__||_|__
	[0.0] EAS>APDW16:{DEZCZC-EAS-RWT-012057-412081-012101-012103-012115+0030-2780415-WTSP/TV-

Case 2: We have two that match so pick either one.

	reject 5 invalid character = ZCZC-EAS-RW▒
	reject 7 invalid character = ZCZC-EAS-RWT-0
	reject 3 invalid character = ZCZC-EAS-RWT-012057-012080-012101-012103-01211
	reject 0 invalid character = ZCZC-EAS-RWT-012057-012081-012101-012103-012115+0030-2780415-W▒
	frame_buf 6 = ZCZC-EAS-RWT-012057-012081-012!01-012103-012115+0030-2780415-WTSP/TV-
	frame_buf 1 = ZCZC-EAS-RWT-012057-012081-012101-012103-012115+0030-2780415-WTSP/TV-

	DECODED[2] 0:03.617 EAS audio level = 194(106/108)     _|____|__
	[0.1] EAS>APDW16:{DEZCZC-EAS-RWT-012057-012081-012101-012103-012115+0030-2780415-WTSP/TV-

Case 3: Slice 6 is a mismatch (EAs vs. EAS).
	Slice 7 has RST rather than RWT.
	2 & 4 don't match either (012141 vs. 012101).
	We have another case where no two match so there is no clear winner.


	reject 5 invalid character = ZCZC-EAS-RWT-012057-012081-012101-012103-012115+▒
	frame_buf 7 = ZCZC-EAS-RST-012057-012081-012101-012103-012115+0030-2780415-WTSP/TV-
	frame_buf 6 = ZCZC-EAs-RWT-012057-012081-012101-012103-012115+0030-2780415-WTSP/TV-
	frame_buf 4 = ZCZC-EAS-RWT-112057-012081-012101-012103-012115+0030-2780415-WTSP/TV-
	frame_buf 2 = ZCZC-EAS-RWT-012057-012081-012141-012103-012115+0030-2780415-WTSP/TV-

	DECODED[3] 0:05.920 EAS audio level = 194(106/108)     __|_|_||_
	[0.2] EAS>APDW16:{DEZCZC-EAS-RWT-012057-012081-012141-012103-012115+0030-2780415-WTSP/TV-

Conclusions:

	(1) The existing algorithm gives a higher preference to those frames matching others.
	We didn't see any cases here where that would be to our advantage.

	(2) A partial solution would be more validity checking.  (i.e. non-digit where
	digit is expected.)  But wait... We might want to keep it for consideration:

	(3) If I got REALLY ambitious, some day, we could compare all of them one column
	at a time and take the most popular (and valid for that column) character and
	use all of the most popular characters. Better yet, at the bit level.

Of course this is probably all overkill because we would normally expect to have pretty
decent signals.  The designers didn't even bother to add any sort of checksum for error checking.

The random errors injected are also not realistic. Actual noise would probably wipe out the
same bit(s) for all of the slices.

The protocol specification suggests comparing all 3 transmissions and taking the best 2 out of 3.
I think that would best be left to an external application and we just concentrate on being
a good modem here and providing a result when it is received.

*/

/***********************************************************************************
 *
 * Name:	hdlc_rec_bit
 *
 * Purpose:	Extract HDLC frames from a stream of bits.
 *
 * Inputs:	channel	- Channel number.
 *
 *		subchannel	- This allows multiple demodulators per channel.
 *
 *		slice	- Allows multiple slicers per demodulator (subchannel).
 *
 *		raw 	- One bit from the demodulator.
 *			  should be 0 or 1.
 *
 *		is_scrambled - Is the data scrambled?
 *
 *		descram_state - Current descrambler state.  (not used - remove)
 *				Not so fast - plans to add new parameter.  PSK already provides it.
 *
 *
 * Description:	This is called once for each received bit.
 *		For each valid frame, process_rec_frame()
 *		is called for further processing.
 *
 ***********************************************************************************/

var dummyll int64
var dummy int

func hdlc_rec_bit(channel int, subchannel int, slice int, raw int, is_scrambled bool, not_used_remove int) {
	hdlc_rec_bit_new(channel, subchannel, slice, raw, is_scrambled, not_used_remove, &dummyll, &dummy)
}

func hdlc_rec_bit_new(channel int, subchannel int, slice int, _raw int, is_scrambled bool, not_used_remove int,
	pll_nudge_total *int64, pll_symbol_count *int) {

	var raw = _raw != 0

	Assert(hdlcRecWasInit)

	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)
	Assert(subchannel >= 0 && subchannel < MAX_SUBCHANS)
	Assert(slice >= 0 && slice < MAX_SLICERS)

	// -e option can be used to artificially introduce the desired
	// Bit Error Rate (BER) for testing.

	if g_audio_p.recv_ber != 0 {
		var r = float64(hdlcRecRand()) / float64(hdlcRecRandMax) // calculate as double to preserve all 31 bits.
		if g_audio_p.recv_ber > r {

			// FIXME
			//text_color_set(DW_COLOR_DEBUG);
			//dw_printf ("hdlc_rec_bit randomly clobber bit, ber = %.6f\n", g_audio_p.recv_ber);

			raw = !raw
		}
	}

	// EAS does not use HDLC.

	if g_audio_p.achan[channel].modem_type == MODEM_EAS {
		eas_rec_bit(channel, subchannel, slice, IfThenElse(raw, 1, 0), not_used_remove)
		return
	}

	/*
	 * Different state information for each channel / subchannel / slice.
	 */
	var H = hdlc_state[channel][subchannel][slice]

	/*
	 * Using NRZI encoding,
	 *   A '0' bit is represented by an inversion since previous bit.
	 *   A '1' bit is represented by no change.
	 */

	var dbit bool /* Data bit after undoing NRZI. */
	if is_scrambled {
		var descram = descramble(IfThenElse(raw, 1, 0), &(H.lfsr))

		dbit = (descram == H.prev_descram)
		H.prev_descram = descram
		H.prev_raw = raw
	} else {
		dbit = (raw == H.prev_raw)

		H.prev_raw = raw
	}

	// After BER insertion, NRZI, and any descrambling, feed into FX.25 decoder as well.
	// Don't waste time on this if AIS.  EAS does not get this far.

	if g_audio_p.achan[channel].modem_type != MODEM_AIS {
		fx25_rec_bit(channel, subchannel, slice, IfThenElse(dbit, 1, 0))
		il2p_rec_bit(channel, subchannel, slice, IfThenElse(raw, 1, 0)) // Note: skip NRZI.
	}

	/*
	 * Octets are sent LSB first.
	 * Shift the most recent 8 bits thru the pattern detector.
	 */
	H.pat_det >>= 1
	if dbit {
		H.pat_det |= 0x80
	}

	H.flag4_det >>= 1
	if dbit {
		H.flag4_det |= 0x80000000
	}

	rrbb_append_bit(H.rrbb, byte(IfThenElse(raw, 1, 0)))

	if H.pat_det == 0x7e {

		rrbb_chop8(H.rrbb)

		/*
		 * The special pattern 01111110 indicates beginning and ending of a frame.
		 * If we have an adequate number of whole octets, it is a candidate for
		 * further processing.
		 *
		 * It might look odd that olen is being tested for 7 instead of 0.
		 * This is because oacc would already have 7 bits from the special
		 * "flag" pattern before it is detected here.
		 */

		/*
			#if OLD_WAY

			#if TEST
				  text_color_set(DW_COLOR_DEBUG);
				  dw_printf ("\nfound flag, olen = %d, frame_len = %d\n", olen, frame_len);
			#endif
				  if (H.olen == 7 && H.frame_len >= MIN_FRAME_LEN) {

				    unsigned short actual_fcs, expected_fcs;

			#if TEST
				    int j;
				    dw_printf ("TRADITIONAL: frame len = %d\n", H.frame_len);
				    for (j=0; j<H.frame_len; j++) {
				      dw_printf ("  %02x", H.frame_buf[j]);
				    }
				    dw_printf ("\n");

			#endif
				    // Check FCS, low byte first, and process...

				    // Alternatively, it is possible to include the two FCS bytes
				    // in the CRC calculation and look for a magic constant.
				    // That would be easier in the case where the CRC is being
				    // accumulated along the way as the octets are received.
				    // I think making a second pass over it and comparing is
				    // easier to understand.

				    actual_fcs = H.frame_buf[H.frame_len-2] | (H.frame_buf[H.frame_len-1] << 8);

				    expected_fcs = fcs_calc (H.frame_buf, H.frame_len - 2);

				    if (actual_fcs == expected_fcs) {
				      alevel_t alevel = demod_get_audio_level (channel, subchannel);

				      multi_modem_process_rec_frame (channel, subchannel, slice, H.frame_buf, H.frame_len - 2, alevel, RETRY_NONE, 0);   // len-2 to remove FCS.
				    } else {

			#if TEST
				      dw_printf ("*** actual fcs = %04x, expected fcs = %04x ***\n", actual_fcs, expected_fcs);
			#endif

				    }

				  }

			#else
		*/

		/*
		 * New way - Decode the raw bits in later step.
		 */

		/*
			#if TEST
				  text_color_set(DW_COLOR_DEBUG);
				  dw_printf ("\nfound flag, channel %d.%d, %d bits in frame\n", channel, subchannel, rrbb_get_len(H.rrbb) - 1);
			#endif
		*/
		if rrbb_get_len(H.rrbb) >= MIN_FRAME_LEN*8 {

			//JWL - end of frame

			var speed_error float64    // in percentage.
			if *pll_symbol_count > 0 { // avoid divde by 0.

				// TODO:
				// Fudged to get +-2.0 with gen_packets -b 1224 & 1176.
				// Also initialized the symbol counter to -1.

				speed_error = float64((C.double)(*pll_nudge_total)*100./(256.*256.*256.*256.)/(C.double)(*pll_symbol_count) + 0.02)

				text_color_set(DW_COLOR_DEBUG)

				// std	      dw_printf ("DEBUG: total %lld, count %d\n", *pll_nudge_total, *pll_symbol_count);
				// mingw
				//	      dw_printf ("DEBUG: total %I64d, count %d\n", *pll_nudge_total, *pll_symbol_count);
				//	      dw_printf ("DEBUG: speed error  %+0.2f%% . %+0.1f%% \n", speed_error, speed_error);
			} else {
				speed_error = 0
			}
			rrbb_set_speed_error(H.rrbb, speed_error)

			var alevel = demod_get_audio_level(C.int(channel), C.int(subchannel))

			rrbb_set_audio_level(H.rrbb, alevel)
			hdlc_rec2_block(H.rrbb)
			/* Now owned by someone else who will free it. */
			H.rrbb = nil

			H.rrbb = rrbb_new(channel, subchannel, slice, is_scrambled, H.lfsr, H.prev_descram) /* Allocate a new one. */
		} else {

			//JWL - start of frame
			*pll_nudge_total = 0
			*pll_symbol_count = -1 // comes out better than using 0.
			rrbb_clear(H.rrbb, is_scrambled, H.lfsr, H.prev_descram)
		}

		H.olen = 0 /* Allow accumulation of octets. */
		H.frame_len = 0

		rrbb_append_bit(H.rrbb, byte(IfThenElse(H.prev_raw, 1, 0))) /* Last bit of flag.  Needed to get first data bit. */
		/* Now that we are saving other initial state information, */
		/* it would be sensible to do the same for this instead */
		/* of lumping it in with the frame data bits. */

		//#define EXPERIMENT12B 1

		// #if EXPERIMENT12B

		// } else if (H.pat_det == 0xff) {

		/*
		 * Valid data will never have seven 1 bits in a row.
		 *
		 *	11111110
		 *
		 * This indicates loss of signal.
		 * But we will let it slip thru because it might diminish
		 * our single bit fixup effort.   Instead give up on frame
		 * only when we see eight 1 bits in a row.
		 *
		 *	11111111
		 *
		 * What is the impact?  No difference.
		 *
		 *  Before:	atest -P E -F 1 ../02_Track_2.wav	= 1003
		 *  After:	atest -P E -F 1 ../02_Track_2.wav	= 1003
		 */

		// #else
	} else if H.pat_det == 0xfe {

		/*
		 * Valid data will never have 7 one bits in a row.
		 *
		 *	11111110
		 *
		 * This indicates loss of signal.
		 */

		// #endif

		H.olen = -1     /* Stop accumulating octets. */
		H.frame_len = 0 /* Discard anything in progress. */

		rrbb_clear(H.rrbb, is_scrambled, H.lfsr, H.prev_descram)

	} else if (H.pat_det & 0xfc) == 0x7c {

		/*
		 * If we have five '1' bits in a row, followed by a '0' bit,
		 *
		 *	0111110xx
		 *
		 * the current '0' bit should be discarded because it was added for
		 * "bit stuffing."
		 */

	} else {

		/*
		 * In all other cases, accumulate bits into octets, and complete octets
		 * into the frame buffer.
		 */
		if H.olen >= 0 {

			H.oacc >>= 1
			if dbit {
				H.oacc |= 0x80
			}
			H.olen++

			if H.olen == 8 {
				H.olen = 0

				if H.frame_len < MAX_FRAME_LEN {
					H.frame_buf[H.frame_len] = H.oacc
					H.frame_len++
				}
			}
		}
	}
}

// TODO:  Data Carrier Detect (DCD) is now based on DPLL lock
// rather than data patterns found here.
// It would make sense to move the next 2 functions to demod.c
// because this is done at the modem level, rather than HDLC decoder.

/*-------------------------------------------------------------------
 *
 * Name:        dcd_change
 *
 * Purpose:     Combine DCD states of all subchannels/ into an overall
 *		state for the channel.
 *
 * Inputs:	channel
 *
 *		subchannel		0 to MAX_SUBCHANS-1 for HDLC.
 *				SPECIAL CASE --> MAX_SUBCHANS for DTMF decoder.
 *
 *		slice		slicer number, 0 .. MAX_SLICERS - 1.
 *
 *		state		1 for active, 0 for not.
 *
 * Returns:	None.  Use hdlc_rec_data_detect_any to retrieve result.
 *
 * Description:	DCD for the channel is active if ANY of the subchannels/slices
 *		are active.  Update the DCD indicator.
 *
 * version 1.3:	Add DTMF detection into the final result.
 *		This is now called from dtmf.c too.
 *
 *--------------------------------------------------------------------*/

func dcd_change_real(channel int, subchannel int, slice int, state int) {

	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)
	Assert(subchannel >= 0 && subchannel <= MAX_SUBCHANS)
	Assert(slice >= 0 && slice < MAX_SLICERS)
	Assert(state == 0 || state == 1)

	/*
		#if DEBUG3
			text_color_set(DW_COLOR_DEBUG);
			dw_printf ("DCD %d.%d.%d = %d \n", channel, subchannel, slice, state);
		#endif
	*/

	var old = hdlc_rec_data_detect_any(channel)

	if state != 0 {
		composite_dcd[channel][subchannel][slice] = true
	} else {
		composite_dcd[channel][subchannel][slice] = false
	}

	var newVal = hdlc_rec_data_detect_any(channel)

	if newVal != old {
		ptt_set(OCTYPE_DCD, channel, newVal)
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        hdlc_rec_data_detect_any
 *
 * Purpose:     Determine if the radio channel is currently busy
 *		with packet data.
 *		This version doesn't care about voice or other sounds.
 *		This is used by the transmit logic to transmit only
 *		when the channel is clear.
 *
 * Inputs:	channel	- Audio channel.
 *
 * Returns:	True if channel is busy (data detected) or
 *		false if OK to transmit.
 *
 *
 * Description:	We have two different versions here.
 *
 *		hdlc_rec_data_detect_any sees if ANY of the decoders
 *		for this channel are receiving a signal.   This is
 *		used to determine whether the channel is clear and
 *		we can transmit.  This would apply to the 300 baud
 *		HF SSB case where we have multiple decoders running
 *		at the same time.  The channel is busy if ANY of them
 *		thinks the channel is busy.
 *
 * Version 1.3: New option for input signal to inhibit transmit.
 *
 *--------------------------------------------------------------------*/

func hdlc_rec_data_detect_any(channel int) int {

	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	for sc := 0; sc < num_subchannel[channel]; sc++ {
		if slices.Contains(composite_dcd[channel][sc][:], true) {
			return (1)
		}
	}

	if get_input(ICTYPE_TXINH, C.int(channel)) == 1 {
		return (1)
	}

	return (0)

} /* end hdlc_rec_data_detect_any */

/* end hdlc_rec.c */
