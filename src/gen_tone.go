package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:     Convert bits to AFSK for writing to .WAV sound file
 *		or a sound device.
 *
 *
 *---------------------------------------------------------------*/

import (
	"fmt"
	"math"
	"os"
)

// Properties of the digitized sound stream & modem.

/*
 * 8 bit samples are unsigned bytes in range of 0 .. 255.
 *
 * 16 bit samples are signed short in range of -32768 .. +32767.
 */

// TODO KG Also defined in morse.go: const TICKS_PER_CYCLE = (256.0 * 256.0 * 256.0 * 256.0)

const PHASE_SHIFT_180 = (uint(128) << 24)
const PHASE_SHIFT_90 = (uint(64) << 24)
const PHASE_SHIFT_45 = (uint(32) << 24)

var gray2phase_v26 []uint = []uint{0, 1, 3, 2}
var gray2phase_v27 []uint = []uint{1, 0, 2, 3, 6, 7, 5, 4}

// GenToneService holds state for AFSK tone generation.
type GenToneService struct {
	audioConfig       *audio_s

	// Constants after initialisation

	ticksPerSample    [MAX_RADIO_CHANS]int /* Same for both channels of same soundcard */
	/* because they have same sample rate */
	/* but less confusing to have for each channel. */
	ticksPerBit       [MAX_RADIO_CHANS]int
	f1ChangePerSample [MAX_RADIO_CHANS]uint
	f2ChangePerSample [MAX_RADIO_CHANS]uint
	samplesPerSymbol  [MAX_RADIO_CHANS]float64
	sineTable         [256]int16

	// Accumulators

	tonePhase         [MAX_RADIO_CHANS]uint // Phase accumulator for tone generation.
	// Upper bits are used as index into sine table.
	bitLenAcc [MAX_RADIO_CHANS]int // To accumulate fractional samples per bit.
	lfsr      [MAX_RADIO_CHANS]int // Shift register for scrambler.
	bitCount  [MAX_RADIO_CHANS]int // Counter incremented for each bit transmitted
	// on the channel.   This is only used for QPSK.
	// The LSB determines if we save the bit until
	// next time, or send this one with the previously saved.
	// The LSB+1 position determines if we add an
	// extra 180 degrees to the phase to compensate
	// for having 1.5 carrier cycles per symbol time.

	// For 8PSK, it has a different meaning.  It is the
	// number of bits in 'saveBit' so we can accumulate
	// three for each symbol.
	saveBit [MAX_RADIO_CHANS]int
	prevDat [MAX_RADIO_CHANS]int // Previous data bit.  Used for G3RUH style.
}

// genTone is the package-level singleton GenToneService.
var genTone *GenToneService

/*------------------------------------------------------------------
 *
 * Name:        NewGenToneService
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
 * Returns:     Initialized *GenToneService.
 *
 * Description:	 Calculate various constants for use by the direct digital synthesis
 * 		audio tone generation.
 *
 *----------------------------------------------------------------*/

func NewGenToneService(audio_config_p *audio_s, amp int, gen_packets bool) *GenToneService { //nolint:unparam
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("gen_tone_init ( audio_config_p=%p, amp=%d, gen_packets=%d )\n",
				audio_config_p, amp, gen_packets);
	#endif
	*/

	var gts = &GenToneService{} //nolint:exhaustruct

	/*
	 * Save away modem parameters for later use.
	 */
	gts.audioConfig = audio_config_p

	for channel := 0; channel < MAX_RADIO_CHANS; channel++ {
		if audio_config_p.chan_medium[channel] == MEDIUM_RADIO {
			var a = ACHAN2ADEV(channel)

			/* TODO KG
			#if DEBUG
				text_color_set(DW_COLOR_DEBUG);
				dw_printf ("gen_tone_init: channel=%d, modem_type=%d, bps=%d, samples_per_sec=%d\n",
					channel,
					gts.audioConfig.achan[channel].modem_type,
					audio_config_p.achan[channel].baud,
					audio_config_p.adev[a].samples_per_sec);
			#endif
			*/

			gts.tonePhase[channel] = 0
			gts.bitLenAcc[channel] = 0
			gts.lfsr[channel] = 0

			gts.ticksPerSample[channel] = (int)((TICKS_PER_CYCLE / float64(audio_config_p.adev[a].samples_per_sec)) + 0.5)

			// The terminology is all wrong here.  Didn't matter with 1200 and 9600.
			// The config speed should be bits per second rather than baud.
			// ticksPerBit should be ticks_per_symbol.

			switch gts.audioConfig.achan[channel].modem_type {
			case MODEM_QPSK:
				audio_config_p.achan[channel].mark_freq = 1800
				audio_config_p.achan[channel].space_freq = audio_config_p.achan[channel].mark_freq // Not Used.

				// symbol time is 1 / (half of bps)
				gts.ticksPerBit[channel] = (int)((TICKS_PER_CYCLE / (float64(audio_config_p.achan[channel].baud) * 0.5)) + 0.5)
				gts.f1ChangePerSample[channel] = (uint)((float64(audio_config_p.achan[channel].mark_freq) * TICKS_PER_CYCLE / float64(audio_config_p.adev[a].samples_per_sec)) + 0.5)
				gts.f2ChangePerSample[channel] = gts.f1ChangePerSample[channel] // Not used.
				gts.samplesPerSymbol[channel] = 2. * float64(audio_config_p.adev[a].samples_per_sec) / float64(audio_config_p.achan[channel].baud)

				gts.tonePhase[channel] = PHASE_SHIFT_45 // Just to mimic first attempt.
				// ??? Why?  We are only concerned with the difference
				// from one symbol to the next.

			case MODEM_8PSK:
				audio_config_p.achan[channel].mark_freq = 1800
				audio_config_p.achan[channel].space_freq = audio_config_p.achan[channel].mark_freq // Not Used.

				// symbol time is 1 / (third of bps)
				gts.ticksPerBit[channel] = (int)((TICKS_PER_CYCLE / (float64(audio_config_p.achan[channel].baud) / 3.)) + 0.5)
				gts.f1ChangePerSample[channel] = (uint)((float64(audio_config_p.achan[channel].mark_freq) * TICKS_PER_CYCLE / float64(audio_config_p.adev[a].samples_per_sec)) + 0.5)
				gts.f2ChangePerSample[channel] = gts.f1ChangePerSample[channel] // Not used.
				gts.samplesPerSymbol[channel] = 3. * float64(audio_config_p.adev[a].samples_per_sec) / float64(audio_config_p.achan[channel].baud)

			case MODEM_BASEBAND, MODEM_SCRAMBLE, MODEM_AIS:
				// Tone is half baud.
				gts.ticksPerBit[channel] = (int)((TICKS_PER_CYCLE / float64(audio_config_p.achan[channel].baud)) + 0.5)
				gts.f1ChangePerSample[channel] = (uint)((float64(audio_config_p.achan[channel].baud) * 0.5 * TICKS_PER_CYCLE / float64(audio_config_p.adev[a].samples_per_sec)) + 0.5)
				gts.samplesPerSymbol[channel] = float64(audio_config_p.adev[a].samples_per_sec) / float64(audio_config_p.achan[channel].baud)

			case MODEM_EAS: //  EAS.
				// TODO: Proper fix would be to use float for baud, mark, space.
				gts.ticksPerBit[channel] = (int)(math.Floor((TICKS_PER_CYCLE / 520.833333333333) + 0.5))
				gts.samplesPerSymbol[channel] = float64(audio_config_p.adev[a].samples_per_sec)/520.83333 + 0.5
				gts.f1ChangePerSample[channel] = (uint)((2083.33333333333 * TICKS_PER_CYCLE / float64(audio_config_p.adev[a].samples_per_sec)) + 0.5)
				gts.f2ChangePerSample[channel] = (uint)((1562.5000000 * TICKS_PER_CYCLE / float64(audio_config_p.adev[a].samples_per_sec)) + 0.5)

			default: // AFSK
				gts.ticksPerBit[channel] = (int)((TICKS_PER_CYCLE / float64(audio_config_p.achan[channel].baud)) + 0.5)
				gts.samplesPerSymbol[channel] = float64(audio_config_p.adev[a].samples_per_sec) / float64(audio_config_p.achan[channel].baud)
				gts.f1ChangePerSample[channel] = (uint)((float64(audio_config_p.achan[channel].mark_freq) * TICKS_PER_CYCLE / float64(audio_config_p.adev[a].samples_per_sec)) + 0.5)
				gts.f2ChangePerSample[channel] = (uint)((float64(audio_config_p.achan[channel].space_freq) * TICKS_PER_CYCLE / float64(audio_config_p.adev[a].samples_per_sec)) + 0.5)
			}
		}
	}

	for j := 0; j < 256; j++ {
		var a = (float64(j) / 256.0) * (2.0 * math.Pi)
		var s = int(math.Sin(a) * 32767 * float64(amp) / 100.0)

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

		gts.sineTable[j] = int16(s)
	}

	return gts
} /* end NewGenToneService */

/*-------------------------------------------------------------------
 *
 * Name:        PutBit
 *
 * Purpose:     Generate tone of proper duration for one data bit.
 *
 * Inputs:      channel	- Audio channel, 0 = first.
 *
 *		dat	- 0 for f1, 1 for f2.
 *
 * 		  	  	-1 inserts half bit to test data
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
//   - A step function, immediately going to new value.
//   - Linear interpoation.
//   - Raised cosine.  Square root of cosine is also mentioned.
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

func interpol8(oldv float64, newv float64, bc float64) float64 { //nolint:unused
	// Step function.
	//return (newv);			// 78 on 11/7
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

	var rc = 0.5 * (math.Cos(float64(bc)*math.Pi-math.Pi) + 1.0)
	/* Comparison
	var rrc = bc >= 0.5
				? 0.5 * (sqrtf(fabsf(cosf(bc * M_PI - M_PI))) + 1.0)
				: 0.5 * (-sqrtf(fabsf(cosf(bc * M_PI - M_PI))) + 1.0);
	*/

	return (rc*newv + (1.0-bc)*oldv) // 49 on 11/7
	//return (rrc * newv + (1.0f - bc) * oldv);	// 55 on 11/7
}

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

func (gts *GenToneService) PutBit(channel int, dat int) {
	var a = ACHAN2ADEV(channel) /* device for channel. */

	Assert(gts.audioConfig != nil)

	if gts.audioConfig.chan_medium[channel] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid channel %d for tone generation.\n", channel)

		return
	}

	if dat < 0 {
		/* Hack to test receive PLL recovery. */
		gts.bitLenAcc[channel] -= gts.ticksPerBit[channel]
		dat = 0
	}

	// TODO: change to switch instead of if if if

	if gts.audioConfig.achan[channel].modem_type == MODEM_QPSK {
		dat &= 1 // Keep only LSB to be extra safe.

		if (gts.bitCount[channel] & 1) == 0 {
			gts.saveBit[channel] = dat
			gts.bitCount[channel]++

			return
		}

		// All zero bits should give us steady 1800 Hz.
		// All one bits should flip phase by 180 degrees each time.
		// For V.26B, add another 45 degrees.
		// This seems to work a little better.

		var dibit = (gts.saveBit[channel] << 1) | dat

		var symbol = gray2phase_v26[dibit] // 0 .. 3 for QPSK.
		/*
			#if PSKIQ
				  // One phase shift unit is 45 degrees.
				  // Remember what it was last time and calculate new.
				  // values 0 .. 7.
				  xmit_prev_octant[channel] = xmit_octant[channel];
				  xmit_octant[channel] += symbol * 2;
				  if (gts.audioConfig.achan[channel].v26_alternative == V26_B) {
				    xmit_octant[channel] += 1;
				  }
				  xmit_octant[channel] &= 0x7;
			#else
		*/
		gts.tonePhase[channel] += symbol * PHASE_SHIFT_90
		if gts.audioConfig.achan[channel].v26_alternative == V26_B {
			gts.tonePhase[channel] += PHASE_SHIFT_45
		}
		//#endif
		gts.bitCount[channel]++
	}

	if gts.audioConfig.achan[channel].modem_type == MODEM_8PSK {
		dat &= 1 // Keep only LSB to be extra safe.

		if gts.bitCount[channel] < 2 {
			gts.saveBit[channel] = (gts.saveBit[channel] << 1) | dat
			gts.bitCount[channel]++

			return
		}

		// The bit pattern 001 should give us steady 1800 Hz.
		// All one bits should flip phase by 180 degrees each time.

		var tribit = (gts.saveBit[channel] << 1) | dat

		var symbol = gray2phase_v27[tribit]
		gts.tonePhase[channel] += symbol * PHASE_SHIFT_45

		gts.saveBit[channel] = 0
		gts.bitCount[channel] = 0
	}

	// Would be logical to have MODEM_BASEBAND for IL2P rather than checking here.  But...
	// That would mean putting in at least 3 places and testing all rather than just one.
	if gts.audioConfig.achan[channel].modem_type == MODEM_SCRAMBLE &&
		gts.audioConfig.achan[channel].layer2_xmit != LAYER2_IL2P {
		var x = (dat ^ (gts.lfsr[channel] >> 16) ^ (gts.lfsr[channel] >> 11)) & 1
		gts.lfsr[channel] = (gts.lfsr[channel] << 1) | (x & 1)
		dat = x
	}
	/*
		#if PSKIQ
			int blend = 1;
		#endif
	*/
	for { /* until enough audio samples for this symbol. */
		var sam int

		switch gts.audioConfig.achan[channel].modem_type {
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
			var change = gts.f2ChangePerSample[channel]
			if dat > 0 {
				change = gts.f1ChangePerSample[channel]
			}

			gts.tonePhase[channel] += change
			sam = int(gts.sineTable[(gts.tonePhase[channel]>>24)&0xff])
			gts.PutSample(channel, a, sam)

		case MODEM_EAS:
			var change = gts.f2ChangePerSample[channel]
			if dat > 0 {
				change = gts.f1ChangePerSample[channel]
			}

			gts.tonePhase[channel] += change
			sam = int(gts.sineTable[(gts.tonePhase[channel]>>24)&0xff])
			gts.PutSample(channel, a, sam)

		case MODEM_QPSK:
			/* TODO KG
			#if DEBUG2
				      text_color_set(DW_COLOR_DEBUG);
				      dw_printf ("tone_gen_put_bit %d PSK\n", __LINE__);
			#endif
			*/
			gts.tonePhase[channel] += gts.f1ChangePerSample[channel]
			/*
				#if PSKIQ
				#if 1  // blend JWL
					      // remove loop invariant
					      float old_i = ci[xmit_prev_octant[channel]];
					      float old_q = sq[xmit_prev_octant[channel]];

					      float new_i = ci[xmit_octant[channel]];
					      float new_q = sq[xmit_octant[channel]];

					      float b = blend / gts.samplesPerSymbol[channel];	// roughly 0 to 1
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

					      sam = blended_i * gts.sineTable[((gts.tonePhase[channel] - PHASE_SHIFT_90) >> 24) & 0xff] +
					            blended_q * gts.sineTable[(gts.tonePhase[channel] >> 24) & 0xff];
				#else  // jump
					      sam = ci[xmit_octant[channel]] * gts.sineTable[((gts.tonePhase[channel] - PHASE_SHIFT_90) >> 24) & 0xff] +
					            sq[xmit_octant[channel]] * gts.sineTable[(gts.tonePhase[channel] >> 24) & 0xff];
				#endif
				#else
			*/
			sam = int(gts.sineTable[(gts.tonePhase[channel]>>24)&0xff])
			gts.PutSample(channel, a, sam)

		case MODEM_8PSK:
			/* TODO KG
			#if DEBUG2
				      text_color_set(DW_COLOR_DEBUG);
				      dw_printf ("tone_gen_put_bit %d PSK\n", __LINE__);
			#endif
			*/
			gts.tonePhase[channel] += gts.f1ChangePerSample[channel]
			sam = int(gts.sineTable[(gts.tonePhase[channel]>>24)&0xff])
			gts.PutSample(channel, a, sam)

		case MODEM_BASEBAND, MODEM_SCRAMBLE, MODEM_AIS:
			if dat != gts.prevDat[channel] {
				gts.tonePhase[channel] += gts.f1ChangePerSample[channel]
			} else {
				if gts.tonePhase[channel]&0x80000000 > 0 {
					gts.tonePhase[channel] = 0xc0000000 // 270 degrees.
				} else {
					gts.tonePhase[channel] = 0x40000000 // 90 degrees.
				}
			}

			sam = int(gts.sineTable[(gts.tonePhase[channel]>>24)&0xff])
			gts.PutSample(channel, a, sam)

		default:
			text_color_set(DW_COLOR_ERROR)
			dw_printf("INTERNAL ERROR: achan[%d].modem_type = %d\n",
				channel, gts.audioConfig.achan[channel].modem_type)
			os.Exit(1)
		}

		/* Enough for the bit time? */

		gts.bitLenAcc[channel] += gts.ticksPerSample[channel]

		if gts.bitLenAcc[channel] >= gts.ticksPerBit[channel] {
			break
		}
	}

	gts.bitLenAcc[channel] -= gts.ticksPerBit[channel]

	gts.prevDat[channel] = dat // Only needed for G3RUH baseband/scrambled.
} /* end PutBit */

func (gts *GenToneService) PutSample(channel int, a int, sam int) {
	/* Ship out an audio sample. */
	/* 16 bit is signed, little endian, range -32768 .. +32767 */
	/* 8 bit is unsigned, range 0 .. 255 */
	Assert(gts.audioConfig != nil)

	Assert(gts.audioConfig.adev[a].num_channels == 1 || gts.audioConfig.adev[a].num_channels == 2)

	Assert(gts.audioConfig.adev[a].bits_per_sample == 16 || gts.audioConfig.adev[a].bits_per_sample == 8)

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

	if gts.audioConfig.adev[a].num_channels == 1 {
		/* Mono */
		if gts.audioConfig.adev[a].bits_per_sample == 8 {
			audio_put(a, uint8(((sam+32768)>>8)&0xff))
		} else {
			audio_put(a, uint8(sam&0xff))
			audio_put(a, uint8((sam>>8)&0xff))
		}
	} else {
		if channel == ADEVFIRSTCHAN(a) {
			/* Stereo, left channel. */
			if gts.audioConfig.adev[a].bits_per_sample == 8 {
				audio_put(a, uint8(((sam+32768)>>8)&0xff))
				audio_put(a, 0)
			} else {
				audio_put(a, uint8(sam&0xff))
				audio_put(a, uint8((sam>>8)&0xff))

				audio_put(a, 0)
				audio_put(a, 0)
			}
		} else {
			/* Stereo, right channel. */
			if gts.audioConfig.adev[a].bits_per_sample == 8 {
				audio_put(a, 0)
				audio_put(a, uint8(((sam+32768)>>8)&0xff))
			} else {
				audio_put(a, 0)
				audio_put(a, 0)

				audio_put(a, uint8(sam&0xff))
				audio_put(a, uint8((sam>>8)&0xff))
			}
		}
	}
}

func (gts *GenToneService) PutQuietMs(channel int, time_ms int) {
	var a = ACHAN2ADEV(channel) /* device for channel. */
	var sam = 0

	var nsamples = int((float64(time_ms) * float64(gts.audioConfig.adev[a].samples_per_sec) / 1000.) + 0.5)

	for j := 0; j < nsamples; j++ {
		gts.PutSample(channel, a, sam)
	}

	// Avoid abrupt change when it starts up again.
	gts.tonePhase[channel] = 0
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
	my_audio_config.adev[0].adevice_in = DEFAULT_ADEVICE
	my_audio_config.adev[0].adevice_out = DEFAULT_ADEVICE
	my_audio_config.chan_medium[0] = MEDIUM_RADIO // TODO KG ??

	audio_open(&my_audio_config)
	var gts = NewGenToneService(&my_audio_config, 100, false)

	for range 2 {
		for n := 0; n < my_audio_config.achan[0].baud*2; n++ {
			gts.PutBit(chan1, 1)
		}

		for n := 0; n < my_audio_config.achan[0].baud*2; n++ {
			gts.PutBit(chan1, 0)
		}
	}

	audio_close()

	/* Now try stereo. */

	my_audio_config = audio_s{} //nolint:exhaustruct
	my_audio_config.adev[0].adevice_in = DEFAULT_ADEVICE
	my_audio_config.adev[0].adevice_out = DEFAULT_ADEVICE
	my_audio_config.adev[0].num_channels = 2

	audio_open(&my_audio_config)
	gts = NewGenToneService(&my_audio_config, 100, false)

	for range 4 {
		for n := 0; n < my_audio_config.achan[0].baud*2; n++ {
			gts.PutBit(chan1, 1)
		}

		for n := 0; n < my_audio_config.achan[0].baud*2; n++ {
			gts.PutBit(chan1, 0)
		}

		for n := 0; n < my_audio_config.achan[1].baud*2; n++ {
			gts.PutBit(chan2, 1)
		}

		for n := 0; n < my_audio_config.achan[1].baud*2; n++ {
			gts.PutBit(chan2, 0)
		}
	}

	audio_close()
}
