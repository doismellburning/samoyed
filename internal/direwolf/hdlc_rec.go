package direwolf

/********************************************************************************
 *
 * Purpose:	Extract HDLC frames from a stream of bits.
 *
 *******************************************************************************/

import (
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/doismellburning/samoyed/internal/rrbb"
)

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

// hdlcConfig is the part of a channel's configuration the HDLC receiver uses.
type hdlcConfig struct {
	fixBits    phy.BitFixLevel // How hard to try to fix a frame with a bad FCS.
	passall    bool            // Let a frame through with a bad FCS once every fix has failed.
	ais        bool            // The channel is AIS, which checks a frame's length rather than its contents.
	sanityTest phy.Sanity      // What a frame has to look like once bits have been fixed.
}

type hdlcReceiver struct {
	config                     hdlcConfig
	audioLevel                 audioLevelFunc // For the audio level to deliver each frame with.
	sink                       frameSink      // Where each frame goes.
	channel, subchannel, slice int

	line *linecode.Decoder /* The slicer's line decoder, for the state the retries start from. */

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
}

func newHDLCReceiver(config hdlcConfig, channel int, subchannel int, slice int, scrambled bool, line *linecode.Decoder, audioLevel audioLevelFunc, sink frameSink) *hdlcReceiver {
	var s = new(hdlcReceiver)
	s.config = config
	s.audioLevel = audioLevel
	s.sink = sink
	s.channel = channel
	s.subchannel = subchannel
	s.slice = slice
	s.line = line
	s.olen = -1

	// TODO: FIX13 wasteful if not needed.
	// Should loop on number of slicers, not max.

	var descramState, prevDescram = s.line.State()
	s.rawBits = rrbb.New(channel, subchannel, slice, scrambled, descramState, prevDescram)

	return s
}

// recBit takes one bit as the demodulator heard it, and the data bit the
// slicer's line decoder made of it.
func (s *hdlcReceiver) recBit(raw bool, dbit bool, is_scrambled bool,
	pll_nudge_total *int64, pll_symbol_count *int) {
	var channel = s.channel
	var subchannel = s.subchannel
	var slice = s.slice

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

			var alevel = s.audioLevel(channel, subchannel)

			s.rawBits.SetAudioLevel(alevel)
			hdlc_rec2_block(s.rawBits, &s.config, s.sink)
			/* Handed off to hdlc_rec2_block. */
			s.rawBits = nil

			var descramState, prevDescram = s.line.State()
			s.rawBits = rrbb.New(channel, subchannel, slice, is_scrambled, descramState, prevDescram) /* Allocate a new one. */
		} else {
			//JWL - start of frame
			*pll_nudge_total = 0
			*pll_symbol_count = -1 // comes out better than using 0.

			var descramState, prevDescram = s.line.State()
			s.rawBits.Clear(is_scrambled, descramState, prevDescram)
		}

		s.olen = 0 /* Allow accumulation of octets. */
		s.frameLen = 0

		s.rawBits.AppendBit(dwutil.IfThenElse[byte](s.line.PrevRaw(), 1, 0)) /* Last bit of flag.  Needed to get first data bit. */
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

		var descramState, prevDescram = s.line.State()
		s.rawBits.Clear(is_scrambled, descramState, prevDescram)
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

/* end hdlc_rec.c */
