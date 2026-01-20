package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:     Convert bits to AFSK for writing to .WAV sound file
 *		or a sound device.
 *
 *
 *---------------------------------------------------------------*/

// #include <stdio.h>
// #include <math.h>
// #include <unistd.h>
// #include <string.h>
// #include <stdlib.h>
// #include <assert.h>
import "C"

import (
	"fmt"
	"math"
	"os"
)

// Properties of the digitized sound stream & modem.

// TODO KG static struct audio_s *save_audio_config_p = nil;

/*
 * 8 bit samples are unsigned bytes in range of 0 .. 255.
 *
 * 16 bit samples are signed short in range of -32768 .. +32767.
 */

/* Constants after initialization. */

// TODO KG Also defined in morse.go: const TICKS_PER_CYCLE = (256.0 * 256.0 * 256.0 * 256.0)

var ticks_per_sample [MAX_RADIO_CHANS]C.int /* Same for both channels of same soundcard */
/* because they have same sample rate */
/* but less confusing to have for each channel. */

var ticks_per_bit [MAX_RADIO_CHANS]C.int
var f1_change_per_sample [MAX_RADIO_CHANS]C.uint
var f2_change_per_sample [MAX_RADIO_CHANS]C.uint
var samples_per_symbol [MAX_RADIO_CHANS]C.float

var sine_table [256]C.short

/* Accumulators. */

var tone_phase [MAX_RADIO_CHANS]C.uint // Phase accumulator for tone generation.
// Upper bits are used as index into sine table.

const PHASE_SHIFT_180 = (C.uint(128) << 24)
const PHASE_SHIFT_90 = (C.uint(64) << 24)
const PHASE_SHIFT_45 = (C.uint(32) << 24)

var bit_len_acc [MAX_RADIO_CHANS]C.int // To accumulate fractional samples per bit.

var lfsr [MAX_RADIO_CHANS]C.int // Shift register for scrambler.

var bit_count [MAX_RADIO_CHANS]C.int // Counter incremented for each bit transmitted
// on the channel.   This is only used for QPSK.
// The LSB determines if we save the bit until
// next time, or send this one with the previously saved.
// The LSB+1 position determines if we add an
// extra 180 degrees to the phase to compensate
// for having 1.5 carrier cycles per symbol time.

// For 8PSK, it has a different meaning.  It is the
// number of bits in 'save_bit' so we can accumulate
// three for each symbol.
var save_bit [MAX_RADIO_CHANS]C.int

var prev_dat [MAX_RADIO_CHANS]C.int // Previous data bit.  Used for G3RUH style.

/*------------------------------------------------------------------
 *
 * Name:        gen_tone_init
 *
 * Purpose:     Initialize for AFSK tone generation which might
 *		be used for RTTY or amateur packet radio.
 *
 * Inputs:      audio_config_p		- Pointer to modem parameter structure, modem_s.
 *
 *				The fields we care about are:
 *
 *					samples_per_sec
 *					baud
 *					mark_freq
 *					space_freq
 *					samples_per_sec
 *
 *		amp		- Signal amplitude on scale of 0 .. 100.
 *
 *				  100% uses the full 16 bit sample range of +-32k.
 *
 *		gen_packets	- True if being called from "gen_packets" utility
 *				  rather than the "direwolf" application.
 *
 * Returns:     0 for success.
 *              -1 for failure.
 *
 * Description:	 Calculate various constants for use by the direct digital synthesis
 * 		audio tone generation.
 *
 *----------------------------------------------------------------*/

var amp16bit C.int /* for 9600 baud */

func gen_tone_init(audio_config_p *audio_s, amp C.int, gen_packets C.int) C.int {

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("gen_tone_init ( audio_config_p=%p, amp=%d, gen_packets=%d )\n",
				audio_config_p, amp, gen_packets);
	#endif
	*/

	/*
	 * Save away modem parameters for later use.
	 */

	save_audio_config_p = audio_config_p

	amp16bit = ((32767 * amp) / 100)

	for channel := C.int(0); channel < MAX_RADIO_CHANS; channel++ {

		if audio_config_p.chan_medium[channel] == MEDIUM_RADIO {

			var a = ACHAN2ADEV(channel)

			/* TODO KG
			#if DEBUG
				text_color_set(DW_COLOR_DEBUG);
				dw_printf ("gen_tone_init: channel=%d, modem_type=%d, bps=%d, samples_per_sec=%d\n",
					channel,
					save_audio_config_p.achan[channel].modem_type,
					audio_config_p.achan[channel].baud,
					audio_config_p.adev[a].samples_per_sec);
			#endif
			*/

			tone_phase[channel] = 0
			bit_len_acc[channel] = 0
			lfsr[channel] = 0

			ticks_per_sample[channel] = (C.int)((TICKS_PER_CYCLE / float64(audio_config_p.adev[a].samples_per_sec)) + 0.5)

			// The terminology is all wrong here.  Didn't matter with 1200 and 9600.
			// The config speed should be bits per second rather than baud.
			// ticks_per_bit should be ticks_per_symbol.

			switch save_audio_config_p.achan[channel].modem_type {

			case MODEM_QPSK:

				audio_config_p.achan[channel].mark_freq = 1800
				audio_config_p.achan[channel].space_freq = audio_config_p.achan[channel].mark_freq // Not Used.

				// symbol time is 1 / (half of bps)
				ticks_per_bit[channel] = (C.int)((TICKS_PER_CYCLE / (float64(audio_config_p.achan[channel].baud) * 0.5)) + 0.5)
				f1_change_per_sample[channel] = (C.uint)((float64(audio_config_p.achan[channel].mark_freq) * TICKS_PER_CYCLE / float64(audio_config_p.adev[a].samples_per_sec)) + 0.5)
				f2_change_per_sample[channel] = f1_change_per_sample[channel] // Not used.
				samples_per_symbol[channel] = 2. * C.float(audio_config_p.adev[a].samples_per_sec) / C.float(audio_config_p.achan[channel].baud)

				tone_phase[channel] = PHASE_SHIFT_45 // Just to mimic first attempt.
				// ??? Why?  We are only concerned with the difference
				// from one symbol to the next.

			case MODEM_8PSK:

				audio_config_p.achan[channel].mark_freq = 1800
				audio_config_p.achan[channel].space_freq = audio_config_p.achan[channel].mark_freq // Not Used.

				// symbol time is 1 / (third of bps)
				ticks_per_bit[channel] = (C.int)((TICKS_PER_CYCLE / (float64(audio_config_p.achan[channel].baud) / 3.)) + 0.5)
				f1_change_per_sample[channel] = (C.uint)((float64(audio_config_p.achan[channel].mark_freq) * TICKS_PER_CYCLE / float64(audio_config_p.adev[a].samples_per_sec)) + 0.5)
				f2_change_per_sample[channel] = f1_change_per_sample[channel] // Not used.
				samples_per_symbol[channel] = 3. * C.float(audio_config_p.adev[a].samples_per_sec) / C.float(audio_config_p.achan[channel].baud)

			case MODEM_BASEBAND, MODEM_SCRAMBLE, MODEM_AIS:

				// Tone is half baud.
				ticks_per_bit[channel] = (C.int)((TICKS_PER_CYCLE / float64(audio_config_p.achan[channel].baud)) + 0.5)
				f1_change_per_sample[channel] = (C.uint)((float64(audio_config_p.achan[channel].baud) * 0.5 * TICKS_PER_CYCLE / float64(audio_config_p.adev[a].samples_per_sec)) + 0.5)
				samples_per_symbol[channel] = C.float(audio_config_p.adev[a].samples_per_sec) / C.float(audio_config_p.achan[channel].baud)

			case MODEM_EAS: //  EAS.

				// TODO: Proper fix would be to use float for baud, mark, space.

				ticks_per_bit[channel] = (C.int)(math.Floor((TICKS_PER_CYCLE / 520.833333333333) + 0.5))
				samples_per_symbol[channel] = C.float(audio_config_p.adev[a].samples_per_sec)/520.83333 + 0.5
				f1_change_per_sample[channel] = (C.uint)((2083.33333333333 * TICKS_PER_CYCLE / float64(audio_config_p.adev[a].samples_per_sec)) + 0.5)
				f2_change_per_sample[channel] = (C.uint)((1562.5000000 * TICKS_PER_CYCLE / float64(audio_config_p.adev[a].samples_per_sec)) + 0.5)

			default: // AFSK

				ticks_per_bit[channel] = (C.int)((TICKS_PER_CYCLE / float64(audio_config_p.achan[channel].baud)) + 0.5)
				samples_per_symbol[channel] = C.float(audio_config_p.adev[a].samples_per_sec) / C.float(audio_config_p.achan[channel].baud)
				f1_change_per_sample[channel] = (C.uint)((float64(audio_config_p.achan[channel].mark_freq) * TICKS_PER_CYCLE / float64(audio_config_p.adev[a].samples_per_sec)) + 0.5)
				f2_change_per_sample[channel] = (C.uint)((float64(audio_config_p.achan[channel].space_freq) * TICKS_PER_CYCLE / float64(audio_config_p.adev[a].samples_per_sec)) + 0.5)
			}
		}
	}

	for j := 0; j < 256; j++ {

		var a = (float64(j) / 256.0) * (2.0 * math.Pi)
		var s = C.int(math.Sin(a) * 32767 * float64(amp) / 100.0)

		/* 16 bit sound sample must fit in range of -32768 .. +32767. */

		if s < -32768 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("gen_tone_init: Excessive amplitude is being clipped.\n")
			s = -32768
		} else if s > 32767 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("gen_tone_init: Excessive amplitude is being clipped.\n")
			s = 32767
		}
		sine_table[j] = C.short(s)
	}

	return (0)

} /* end gen_tone_init */

/*-------------------------------------------------------------------
 *
 * Name:        tone_gen_put_bit
 *
 * Purpose:     Generate tone of proper duration for one data bit.
 *
 * Inputs:      channel	- Audio channel, 0 = first.
 *
 *		dat	- 0 for f1, 1 for f2.
 *
 * 			  	-1 inserts half bit to test data
 *				recovery PLL.
 *
 * Assumption:  fp is open to a file for write.
 *
 * Version 1.4:	Attempt to implement 2400 and 4800 bps PSK modes.
 *
 * Version 1.6: For G3RUH, rather than generating square wave and low
 *		pass filtering, generate the waveform directly.
 *		This avoids overshoot, ringing, and adding more jitter.
 *		Alternating bits come out has sine wave of baud/2 Hz.
 *
 * Version 1.6:	MFJ-2400 compatibility for V.26.
 *
 *--------------------------------------------------------------------*/

// Interpolate between two values.
// My original approximation simply jumped between phases, producing a discontinuity,
// and increasing bandwidth.
// According to multiple sources, we should transition more gently.
// Below see see a rough approximation of:
//  * A step function, immediately going to new value.
//  * Linear interpoation.
//  * Raised cosine.  Square root of cosine is also mentioned.
//
//	new	      -		    /		   --
//		      |		   /		  /
//		      |		  /		  |
//		      |		 /		  /
//	old	-------		/		--
//		step		linear		raised cosine
//
// Inputs are the old (previous value), new value, and a blending control
// 0 -> take old value
// 1 -> take new value.
// in between some sort of weighted average.

func interpol8(oldv C.float, newv C.float, bc C.float) C.float {
	// Step function.
	//return (newv);				// 78 on 11/7

	Assert(bc >= 0)
	Assert(bc <= 1.1)

	if bc < 0 {
		return (oldv)
	}
	if bc > 1 {
		return (newv)
	}

	// Linear interpolation, just for comparison.
	//return (bc * newv + (1.0f - bc) * oldv);	// 39 on 11/7

	var rc = 0.5 * C.float(math.Cos(float64(bc)*math.Pi-math.Pi)+1.0)
	/* Comparison
	var rrc = bc >= 0.5
				? 0.5 * (sqrtf(fabsf(cosf(bc * M_PI - M_PI))) + 1.0)
				: 0.5 * (-sqrtf(fabsf(cosf(bc * M_PI - M_PI))) + 1.0);
	*/

	return (rc*newv + (1.0-bc)*oldv) // 49 on 11/7
	//return (rrc * newv + (1.0f - bc) * oldv);	// 55 on 11/7
}

var gray2phase_v26 []C.uint = []C.uint{0, 1, 3, 2}
var gray2phase_v27 []C.uint = []C.uint{1, 0, 2, 3, 6, 7, 5, 4}

// #define PSKIQ 1  // not ready for prime time yet.
/* PSKIQ
#if PSKIQ
static int xmit_octant[MAX_RADIO_CHANS];	// absolute phase in 45 degree units.
static int xmit_prev_octant[MAX_RADIO_CHANS];	// from previous symbol.

// For PSK, we generate the final signal by combining fixed frequency cosine and
// sine by the following weights.
static const float ci[8] = { 1,	.7071,	0,	-.7071,	-1,	-.7071,	0,	.7071	};
static const float sq[8] = { 0,	.7071,	1,	.7071,	0,	-.7071,	-1,	-.7071	};
#endif
*/

func tone_gen_put_bit_real(channel C.int, dat C.int) {

	var a = ACHAN2ADEV(channel) /* device for channel. */

	Assert(save_audio_config_p != nil)

	if save_audio_config_p.chan_medium[channel] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid channel %d for tone generation.\n", channel)
		return
	}

	if dat < 0 {
		/* Hack to test receive PLL recovery. */
		bit_len_acc[channel] -= ticks_per_bit[channel]
		dat = 0
	}

	// TODO: change to switch instead of if if if

	if save_audio_config_p.achan[channel].modem_type == MODEM_QPSK {

		dat &= 1 // Keep only LSB to be extra safe.

		if (bit_count[channel] & 1) == 0 {
			save_bit[channel] = dat
			bit_count[channel]++
			return
		}

		// All zero bits should give us steady 1800 Hz.
		// All one bits should flip phase by 180 degrees each time.
		// For V.26B, add another 45 degrees.
		// This seems to work a little better.

		var dibit = (save_bit[channel] << 1) | dat

		var symbol = gray2phase_v26[dibit] // 0 .. 3 for QPSK.
		/*
			#if PSKIQ
				  // One phase shift unit is 45 degrees.
				  // Remember what it was last time and calculate new.
				  // values 0 .. 7.
				  xmit_prev_octant[channel] = xmit_octant[channel];
				  xmit_octant[channel] += symbol * 2;
				  if (save_audio_config_p.achan[channel].v26_alternative == V26_B) {
				    xmit_octant[channel] += 1;
				  }
				  xmit_octant[channel] &= 0x7;
			#else
		*/
		tone_phase[channel] += symbol * PHASE_SHIFT_90
		if save_audio_config_p.achan[channel].v26_alternative == V26_B {
			tone_phase[channel] += PHASE_SHIFT_45
		}
		//#endif
		bit_count[channel]++
	}

	if save_audio_config_p.achan[channel].modem_type == MODEM_8PSK {

		dat &= 1 // Keep only LSB to be extra safe.

		if bit_count[channel] < 2 {
			save_bit[channel] = (save_bit[channel] << 1) | dat
			bit_count[channel]++
			return
		}

		// The bit pattern 001 should give us steady 1800 Hz.
		// All one bits should flip phase by 180 degrees each time.

		var tribit = (save_bit[channel] << 1) | dat

		var symbol = gray2phase_v27[tribit]
		tone_phase[channel] += symbol * PHASE_SHIFT_45

		save_bit[channel] = 0
		bit_count[channel] = 0
	}

	// Would be logical to have MODEM_BASEBAND for IL2P rather than checking here.  But...
	// That would mean putting in at least 3 places and testing all rather than just one.
	if save_audio_config_p.achan[channel].modem_type == MODEM_SCRAMBLE &&
		save_audio_config_p.achan[channel].layer2_xmit != LAYER2_IL2P {
		var x = (dat ^ (lfsr[channel] >> 16) ^ (lfsr[channel] >> 11)) & 1
		lfsr[channel] = (lfsr[channel] << 1) | (x & 1)
		dat = x
	}
	/*
		#if PSKIQ
			int blend = 1;
		#endif
	*/
	for { /* until enough audio samples for this symbol. */

		var sam C.int

		switch save_audio_config_p.achan[channel].modem_type {

		case MODEM_AFSK:

			/* TODO KG
			#if DEBUG2
				      text_color_set(DW_COLOR_DEBUG);
				      dw_printf ("tone_gen_put_bit %d AFSK\n", __LINE__);
			#endif
			*/

			// v1.7 reversed.
			// Previously a data '1' selected the second (usually higher) tone.
			// It never really mattered before because we were using NRZI.
			// With the addition of IL2P, we need to be more careful.
			// A data '1' should be the mark tone.

			var change = f2_change_per_sample[channel]
			if dat > 0 {
				change = f1_change_per_sample[channel]
			}
			tone_phase[channel] += change
			sam = C.int(sine_table[(tone_phase[channel]>>24)&0xff])
			gen_tone_put_sample(channel, a, sam)

		case MODEM_EAS:

			var change = f2_change_per_sample[channel]
			if dat > 0 {
				change = f1_change_per_sample[channel]
			}
			tone_phase[channel] += change
			sam = C.int(sine_table[(tone_phase[channel]>>24)&0xff])
			gen_tone_put_sample(channel, a, sam)

		case MODEM_QPSK:

			/* TODO KG
			#if DEBUG2
				      text_color_set(DW_COLOR_DEBUG);
				      dw_printf ("tone_gen_put_bit %d PSK\n", __LINE__);
			#endif
			*/
			tone_phase[channel] += f1_change_per_sample[channel]
			/*
				#if PSKIQ
				#if 1  // blend JWL
					      // remove loop invariant
					      float old_i = ci[xmit_prev_octant[channel]];
					      float old_q = sq[xmit_prev_octant[channel]];

					      float new_i = ci[xmit_octant[channel]];
					      float new_q = sq[xmit_octant[channel]];

					      float b = blend / samples_per_symbol[channel];	// roughly 0 to 1
					      blend++;
					     // b = (b - 0.5) * 20 + 0.5;
					     // if (b < 0) b = 0;
					     // if (b > 1) b = 1;
						// b = b > 0.5;
						//b = 1;		// 78 decoded with this.
									// only 39 without.


					      //float blended_i = new_i * b + old_i * (1.0f - b);
					      //float blended_q = new_q * b + old_q * (1.0f - b);

					      float blended_i = interpol8 (old_i, new_i, b);
					      float blended_q = interpol8 (old_q, new_q, b);

					      sam = blended_i * sine_table[((tone_phase[channel] - PHASE_SHIFT_90) >> 24) & 0xff] +
					            blended_q * sine_table[(tone_phase[channel] >> 24) & 0xff];
				#else  // jump
					      sam = ci[xmit_octant[channel]] * sine_table[((tone_phase[channel] - PHASE_SHIFT_90) >> 24) & 0xff] +
					            sq[xmit_octant[channel]] * sine_table[(tone_phase[channel] >> 24) & 0xff];
				#endif
				#else
			*/
			sam = C.int(sine_table[(tone_phase[channel]>>24)&0xff])
			gen_tone_put_sample(channel, a, sam)

		case MODEM_8PSK:
			/* TODO KG
			#if DEBUG2
				      text_color_set(DW_COLOR_DEBUG);
				      dw_printf ("tone_gen_put_bit %d PSK\n", __LINE__);
			#endif
			*/
			tone_phase[channel] += f1_change_per_sample[channel]
			sam = C.int(sine_table[(tone_phase[channel]>>24)&0xff])
			gen_tone_put_sample(channel, a, sam)

		case MODEM_BASEBAND, MODEM_SCRAMBLE, MODEM_AIS:

			if dat != prev_dat[channel] {
				tone_phase[channel] += f1_change_per_sample[channel]
			} else {
				if tone_phase[channel]&0x80000000 > 0 {
					tone_phase[channel] = 0xc0000000 // 270 degrees.
				} else {
					tone_phase[channel] = 0x40000000 // 90 degrees.
				}
			}
			sam = C.int(sine_table[(tone_phase[channel]>>24)&0xff])
			gen_tone_put_sample(channel, a, sam)

		default:
			text_color_set(DW_COLOR_ERROR)
			dw_printf("INTERNAL ERROR: achan[%d].modem_type = %d\n",
				channel, save_audio_config_p.achan[channel].modem_type)
			os.Exit(1)
		}

		/* Enough for the bit time? */

		bit_len_acc[channel] += ticks_per_sample[channel]

		if bit_len_acc[channel] >= ticks_per_bit[channel] {
			break
		}
	}

	bit_len_acc[channel] -= ticks_per_bit[channel]

	prev_dat[channel] = dat // Only needed for G3RUH baseband/scrambled.

} /* end tone_gen_put_bit */

func gen_tone_put_sample(channel C.int, a C.int, sam C.int) {

	/* Ship out an audio sample. */
	/* 16 bit is signed, little endian, range -32768 .. +32767 */
	/* 8 bit is unsigned, range 0 .. 255 */

	Assert(save_audio_config_p != nil)

	Assert(save_audio_config_p.adev[a].num_channels == 1 || save_audio_config_p.adev[a].num_channels == 2)

	Assert(save_audio_config_p.adev[a].bits_per_sample == 16 || save_audio_config_p.adev[a].bits_per_sample == 8)

	// Bad news if we are clipping and distorting the signal.
	// We are using the full range.
	// Too late to change now because everyone would need to recalibrate their
	// transmit audio level.

	if sam < -32767 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Warning: Audio sample %d clipped to -32767.\n", sam)
		sam = -32767
	} else if sam > 32767 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Warning: Audio sample %d clipped to +32767.\n", sam)
		sam = 32767
	}

	if save_audio_config_p.adev[a].num_channels == 1 {

		/* Mono */

		if save_audio_config_p.adev[a].bits_per_sample == 8 {
			audio_put(a, ((sam+32768)>>8)&0xff)
		} else {
			audio_put(a, sam&0xff)
			audio_put(a, (sam>>8)&0xff)
		}
	} else {

		if channel == C.int(ADEVFIRSTCHAN(int(a))) {

			/* Stereo, left channel. */

			if save_audio_config_p.adev[a].bits_per_sample == 8 {
				audio_put(a, ((sam+32768)>>8)&0xff)
				audio_put(a, 0)
			} else {
				audio_put(a, sam&0xff)
				audio_put(a, (sam>>8)&0xff)

				audio_put(a, 0)
				audio_put(a, 0)
			}
		} else {

			/* Stereo, right channel. */

			if save_audio_config_p.adev[a].bits_per_sample == 8 {
				audio_put(a, 0)
				audio_put(a, ((sam+32768)>>8)&0xff)
			} else {
				audio_put(a, 0)
				audio_put(a, 0)

				audio_put(a, sam&0xff)
				audio_put(a, (sam>>8)&0xff)
			}
		}
	}
}

func gen_tone_put_quiet_ms(channel C.int, time_ms C.int) {

	var a = ACHAN2ADEV(channel) /* device for channel. */
	var sam C.int = 0

	var nsamples = C.int((float64(time_ms) * float64(save_audio_config_p.adev[a].samples_per_sec) / 1000.) + 0.5)

	for j := C.int(0); j < nsamples; j++ {
		gen_tone_put_sample(channel, a, sam)
	}

	// Avoid abrupt change when it starts up again.
	tone_phase[channel] = 0
}

/*-------------------------------------------------------------------
 *
 * Name:        main
 *
 * Purpose:     Quick test program for generating tones
 *
 *--------------------------------------------------------------------*/

func GenToneMain() {
	fmt.Println("Warning, known to fail with an assertion error, needs debugging and fixing.")

	const chan1 = 0
	const chan2 = 1

	/* to sound card */
	/* one channel.  2 times:  one second of each tone. */

	var my_audio_config audio_s
	C.strcpy(&my_audio_config.adev[0].adevice_in[0], C.CString(DEFAULT_ADEVICE))
	C.strcpy(&my_audio_config.adev[0].adevice_out[0], C.CString(DEFAULT_ADEVICE))
	my_audio_config.chan_medium[0] = MEDIUM_RADIO // TODO KG ??

	audio_open(&my_audio_config)
	gen_tone_init(&my_audio_config, 100, 0)

	for range 2 {
		for n := C.int(0); n < my_audio_config.achan[0].baud*2; n++ {
			tone_gen_put_bit(chan1, 1)
		}

		for n := C.int(0); n < my_audio_config.achan[0].baud*2; n++ {
			tone_gen_put_bit(chan1, 0)
		}
	}

	audio_close()

	/* Now try stereo. */

	my_audio_config = audio_s{} //nolint:exhaustruct
	C.strcpy(&my_audio_config.adev[0].adevice_in[0], C.CString(DEFAULT_ADEVICE))
	C.strcpy(&my_audio_config.adev[0].adevice_out[0], C.CString(DEFAULT_ADEVICE))
	my_audio_config.adev[0].num_channels = 2

	audio_open(&my_audio_config)
	gen_tone_init(&my_audio_config, 100, 0)

	for range 4 {
		for n := C.int(0); n < my_audio_config.achan[0].baud*2; n++ {
			tone_gen_put_bit(chan1, 1)
		}

		for n := C.int(0); n < my_audio_config.achan[0].baud*2; n++ {
			tone_gen_put_bit(chan1, 0)
		}

		for n := C.int(0); n < my_audio_config.achan[1].baud*2; n++ {
			tone_gen_put_bit(chan2, 1)
		}

		for n := C.int(0); n < my_audio_config.achan[1].baud*2; n++ {
			tone_gen_put_bit(chan2, 0)
		}
	}

	audio_close()
}
