// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

/********************************************************************************
 *
 * Purpose:	Extract EAS SAME transmissions from a stream of bits.
 *
 *		SAME is not HDLC: it has no flags, bit stuffing or NRZI.
 *
 *******************************************************************************/

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

func (s *hdlcState) recEasBit(raw int, future_use int) { //nolint:unparam
	//dw_printf ("slice %d = %d\n", s.slice, raw);

	// Accumulate most recent 64 bits.

	s.easAcc >>= 1
	if raw != 0 {
		s.easAcc |= 0x8000000000000000
	}

	var done = false

	if s.easAcc == PREAMBLE_ZCZC {
		//dw_printf ("ZCZC\n");
		s.olen = 0
		s.easGathering = true
		s.easPlusFound = false
		s.easFieldsAfterPlus = 0
		copy(s.frameBuf[:], []byte("ZCZC"))
		s.frameLen = 4
	} else if s.easAcc == PREAMBLE_NNNN {
		//dw_printf ("NNNN\n");
		s.olen = 0
		s.easGathering = true
		copy(s.frameBuf[:], []byte("NNNN"))
		s.frameLen = 4
		done = true
	} else if s.easGathering {
		s.olen++
		if s.olen == 8 {
			s.olen = 0
			var ch = byte(s.easAcc >> 56)
			s.frameBuf[s.frameLen] = ch
			s.frameLen++
			s.frameBuf[s.frameLen] = 0
			//dw_printf ("frameBuf = %s\n", s.frameBuf);

			// What characters are acceptable?
			// Only ASCII is allowed.  i.e. the MSB must be 0.
			// The examples show only digits but the geographical area can
			// contain anything in range of '!' to DEL or CR or LF.
			// There are no restrictions listed for the originator and
			// examples contain a slash.
			// It's not clear if a space can occur in other places.

			if (ch < ' ' || ch > 0x7f) && ch != '\r' && ch != '\n' {
				//#define DEBUG_E 1
				/*
				   #ifdef DEBUG_E
				   	      dw_printf ("reject %d invalid character = %s\n", s.slice, s.frameBuf);
				   #endif
				*/
				s.easGathering = false

				return
			}

			if s.frameLen > EAS_MAX_LEN { // FIXME: look for other places with max length
				/*
					#ifdef DEBUG_E
						      dw_printf ("reject %d too long = %s\n", s.slice, s.frameBuf);
					#endif
				*/
				s.easGathering = false

				return
			}

			if ch == '+' {
				s.easPlusFound = true
				s.easFieldsAfterPlus = 0
			}

			if s.easPlusFound && ch == '-' {
				s.easFieldsAfterPlus++
				if s.easFieldsAfterPlus == 3 {
					done = true // normal case
				}
			}
		}
	}

	if done {
		/*
			#ifdef DEBUG_E
				  dw_printf ("frameBuf %d = %s\n", s.slice, s.frameBuf);
			#endif
		*/
		var alevel = demod_get_audio_level(s.channel, s.subchannel)
		multi_modem_process_rec_frame(s.channel, s.subchannel, s.slice, s.frameBuf[:s.frameLen], alevel, 0, 0)
		s.easGathering = false
	}
}

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
