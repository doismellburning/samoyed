package direwolf

/********************************************************************************
 *
 * Purpose:	Extract HDLC frames from a stream of bits.
 *
 *******************************************************************************/

import (
	"slices"

	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/metrics"
	"github.com/doismellburning/samoyed/internal/rrbb"
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

type hdlcState struct {
	receiver                   *HDLCReceiver
	channel, subchannel, slice int

	prevRaw bool /* Keep track of previous bit so */
	/* we can look for transitions. */

	lfsr int /* Descrambler shift register for 9600 baud. */

	prevDescram int /* Previous descrambled for 9600 baud. */

	patDet byte /* 8 bit pattern detector shift register. */
	/* See below for more details. */

	flag4Det uint /* Last 32 raw bits to look for 4 */
	/* flag patterns in a row. */

	oacc byte /* Accumulator for building up an octet. */

	olen int /* Number of bits in oacc. */
	/* When this reaches 8, oacc is copied */
	/* to the frame buffer and olen is zeroed. */
	/* The value of -1 is a special case meaning */
	/* bits should not be accumulated. */

	frameBuf [MAX_FRAME_LEN]byte
	/* One frame is kept here. */

	frameLen int /* Number of octets in frameBuf. */
	/* Should be in range of 0 .. MAX_FRAME_LEN. */

	rawBits *rrbb.Buffer /* Handle for bit array for raw received bits. */

	easAcc uint64 /* Accumulate most recent 64 bits received for EAS. */

	easGathering bool /* Decoding in progress. */

	easPlusFound bool /* "+" seen, indicating end of geographical area list. */

	easFieldsAfterPlus int /* Number of "-" characters after the "+". */

	fx25 *fx25Receiver /* FX.25 decoder fed the same data bits. */

	il2p *il2pReceiver /* IL2P decoder fed the same raw bits. */
}

// HDLCReceiver holds the HDLC bit-decoder state for every (channel, subchannel, slicer)
// combination, along with the aggregated DCD/receive state shared across them.
type HDLCReceiver struct {
	slicer        [MAX_RADIO_CHANS][MAX_SUBCHANS][MAX_SLICERS]*hdlcState
	numSubchannel [MAX_RADIO_CHANS]int //TODO1.2 use ptr rather than copy.
	compositeDCD  [MAX_RADIO_CHANS][MAX_SUBCHANS + 1][MAX_SLICERS]bool
	audio         *RadioConfig
	fx25Debug     int // FX.25's debug level, for every slicer's FX.25 receiver.
	sink          ReceiveSink

	// Own copy of random number generator so we can get
	// same predictable results on different operating systems.
	// TODO: Consolidate multiple copies somewhere.
	randSeed int32
}

const hdlcRecRandMax int32 = 0x7fffffff

func newHDLCState(r *HDLCReceiver, channel int, subchannel int, slice int, scrambled bool) *hdlcState {
	var s = new(hdlcState)
	s.receiver = r
	s.channel = channel
	s.subchannel = subchannel
	s.slice = slice
	s.olen = -1

	// TODO: FIX13 wasteful if not needed.
	// Should loop on number of slicers, not max.

	s.rawBits = rrbb.New(channel, subchannel, slice, scrambled, s.lfsr, s.prevDescram)

	s.fx25 = newFX25Receiver(channel, subchannel, slice, r.fx25Debug, fx25_deliver_frame)
	s.il2p = newIL2PReceiver(channel, subchannel, slice, r.audio.achan[channel].il2p_version, r.audio.achan[channel].il2p_crc)

	return s
}

/***********************************************************************************
 *
 * Name:	NewHDLCReceiver
 *
 * Purpose:	Call once at the beginning to initialize.
 *
 * Inputs:	pa	- Audio configuration.
 *
 *		demods	- Each radio channel's demodulators, which say how
 *			  many subchannels it has; nil for any other channel.
 *
 *		fx25Debug - FX.25's debug level, for every slicer's FX.25
 *			  receiver.
 *
 *		sink	- Where a change in the channel's data carrier detect
 *			  state is reported.
 *
 ***********************************************************************************/

func NewHDLCReceiver(pa *RadioConfig, demods [MAX_RADIO_CHANS]*Demodulator, fx25Debug int, sink ReceiveSink) *HDLCReceiver {
	//text_color_set(DW_COLOR_DEBUG);
	//dw_printf ("NewHDLCReceiver (%p) \n", pa);

	var r = new(HDLCReceiver)
	r.audio = pa
	r.fx25Debug = fx25Debug
	r.sink = sink
	r.randSeed = 1

	for ch, d := range demods {
		if d != nil {
			r.numSubchannel[ch] = d.NumSubchan()

			for sub := range r.numSubchannel[ch] {
				for slice := range MAX_SLICERS {
					r.slicer[ch][sub][slice] = newHDLCState(r, ch, sub, slice, pa.achan[ch].modem_type == MODEM_SCRAMBLE)
				}
			}
		}
	}

	return r
}

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

func (r *HDLCReceiver) RecBit(channel int, subchannel int, slice int, raw int, is_scrambled bool, not_used_remove int) {
	var dummyll int64
	var dummy int
	r.RecBitNew(channel, subchannel, slice, raw, is_scrambled, not_used_remove, &dummyll, &dummy)
}

func (r *HDLCReceiver) RecBitNew(channel int, subchannel int, slice int, _raw int, is_scrambled bool, not_used_remove int,
	pll_nudge_total *int64, pll_symbol_count *int) {
	var raw = _raw != 0

	// -e option can be used to artificially introduce the desired
	// Bit Error Rate (BER) for testing.

	if r.audio.recv_ber != 0 {
		var p = float64(r.rand()) / float64(hdlcRecRandMax) // calculate as double to preserve all 31 bits.
		if r.audio.recv_ber > p {
			// FIXME
			//text_color_set(DW_COLOR_DEBUG);
			//dw_printf ("hdlc_rec_bit randomly clobber bit, ber = %.6f\n", r.audio.recv_ber);
			raw = !raw
		}
	}

	var s = r.slicer[channel][subchannel][slice]

	// EAS does not use HDLC.

	if r.audio.achan[channel].modem_type == MODEM_EAS {
		s.recEasBit(dwutil.IfThenElse(raw, 1, 0), not_used_remove)

		return
	}

	s.recBitNew(raw, is_scrambled, pll_nudge_total, pll_symbol_count)
}

func (s *hdlcState) recBitNew(raw bool, is_scrambled bool,
	pll_nudge_total *int64, pll_symbol_count *int) {
	var channel = s.channel
	var subchannel = s.subchannel
	var slice = s.slice
	var r = s.receiver

	/*
	 * Using NRZI encoding,
	 *   A '0' bit is represented by an inversion since previous bit.
	 *   A '1' bit is represented by no change.
	 */

	var dbit bool /* Data bit after undoing NRZI. */

	if is_scrambled {
		var descram = descramble(dwutil.IfThenElse(raw, 1, 0), &(s.lfsr))

		dbit = (descram == s.prevDescram)
		s.prevDescram = descram
		s.prevRaw = raw
	} else {
		dbit = (raw == s.prevRaw)

		s.prevRaw = raw
	}

	// After BER insertion, NRZI, and any descrambling, feed into FX.25 decoder as well.
	// Don't waste time on this if AIS.  EAS does not get this far.

	if r.audio.achan[channel].modem_type != MODEM_AIS {
		s.fx25.recBit(dwutil.IfThenElse(dbit, 1, 0))
		s.il2p.recBit(dwutil.IfThenElse(raw, 1, 0)) // Note: skip NRZI.
	}

	/*
	 * Octets are sent LSB first.
	 * Shift the most recent 8 bits thru the pattern detector.
	 */
	s.patDet >>= 1
	if dbit {
		s.patDet |= 0x80
	}

	s.flag4Det >>= 1
	if dbit {
		s.flag4Det |= 0x80000000
	}

	s.rawBits.AppendBit(dwutil.IfThenElse[byte](raw, 1, 0))

	if s.patDet == 0x7e {
		s.rawBits.Chop8()

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
				      ALevel alevel = demod_get_audio_level (channel, subchannel);

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
				  dw_printf ("\nfound flag, channel %d.%d, %d bits in frame\n", channel, subchannel, H.rawBits.Len() - 1);
			#endif
		*/
		if s.rawBits.Len() >= MIN_FRAME_LEN*8 {
			//JWL - end of frame
			var speed_error float64    // in percentage.
			if *pll_symbol_count > 0 { // avoid divde by 0.
				// TODO:
				// Fudged to get +-2.0 with gen_packets -b 1224 & 1176.
				// Also initialized the symbol counter to -1.
				speed_error = float64(*pll_nudge_total)*100./(256.*256.*256.*256.)/float64(*pll_symbol_count) + 0.02

				// std	      dw_printf ("DEBUG: total %lld, count %d\n", *pll_nudge_total, *pll_symbol_count);
				// mingw
				//	      dw_printf ("DEBUG: total %I64d, count %d\n", *pll_nudge_total, *pll_symbol_count);
				//	      dw_printf ("DEBUG: speed error  %+0.2f%% . %+0.1f%% \n", speed_error, speed_error);
			} else {
				speed_error = 0
			}

			s.rawBits.SetSpeedError(speed_error)

			var alevel = demod_get_audio_level(channel, subchannel)

			s.rawBits.SetAudioLevel(alevel)
			hdlc_rec2_block(s.rawBits, &s.receiver.audio.achan[channel])
			/* Handed off to hdlc_rec2_block. */
			s.rawBits = nil

			s.rawBits = rrbb.New(channel, subchannel, slice, is_scrambled, s.lfsr, s.prevDescram) /* Allocate a new one. */
		} else {
			//JWL - start of frame
			*pll_nudge_total = 0
			*pll_symbol_count = -1 // comes out better than using 0.

			s.rawBits.Clear(is_scrambled, s.lfsr, s.prevDescram)
		}

		s.olen = 0 /* Allow accumulation of octets. */
		s.frameLen = 0

		s.rawBits.AppendBit(dwutil.IfThenElse[byte](s.prevRaw, 1, 0)) /* Last bit of flag.  Needed to get first data bit. */
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
	} else if s.patDet == 0xfe {
		/*
		 * Valid data will never have 7 one bits in a row.
		 *
		 *	11111110
		 *
		 * This indicates loss of signal.
		 */

		// #endif
		s.olen = -1    /* Stop accumulating octets. */
		s.frameLen = 0 /* Discard anything in progress. */

		s.rawBits.Clear(is_scrambled, s.lfsr, s.prevDescram)
	} else if (s.patDet & 0xfc) == 0x7c {

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
		if s.olen >= 0 {
			s.oacc >>= 1
			if dbit {
				s.oacc |= 0x80
			}

			s.olen++

			if s.olen == 8 {
				s.olen = 0

				if s.frameLen < MAX_FRAME_LEN {
					s.frameBuf[s.frameLen] = s.oacc
					s.frameLen++
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

func (r *HDLCReceiver) DCDChange(channel int, subchannel int, slice int, state int) {
	/*
		#if DEBUG3
			text_color_set(DW_COLOR_DEBUG);
			dw_printf ("DCD %d.%d.%d = %d \n", channel, subchannel, slice, state);
		#endif
	*/

	var old = r.DataDetectAny(channel)

	if state != 0 {
		r.compositeDCD[channel][subchannel][slice] = true
	} else {
		r.compositeDCD[channel][subchannel][slice] = false
	}

	var newVal = r.DataDetectAny(channel)

	if newVal != old {
		r.sink.DCDChange(channel, newVal)
		metrics.SetDCD(channel, newVal != 0)
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

func (r *HDLCReceiver) DataDetectAny(channel int) int {
	for sc := range r.numSubchannel[channel] {
		if slices.Contains(r.compositeDCD[channel][sc][:], true) {
			return (1)
		}
	}

	if pttControl.GetInput(ICTYPE_TXINH, channel) == 1 {
		return (1)
	}

	return (0)
} /* end DataDetectAny */

func (r *HDLCReceiver) rand() int32 {
	r.randSeed = (r.randSeed*1103515245 + 12345) & hdlcRecRandMax // Wraps on overflow, as intended.

	return r.randSeed
}

/* end hdlc_rec.c */
