//nolint:gochecknoglobals
package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Common entry point for multiple types of demodulators.
 *
 * Input:	Audio samples from either a file or the "sound card."
 *
 * Outputs:	Calls hdlcReceiver.RecBit() for each bit demodulated.
 *
 *---------------------------------------------------------------*/

import (
	"strings"
	"sync/atomic"
	"unicode"

	"github.com/doismellburning/samoyed/internal/metrics"
	"github.com/sirupsen/logrus"
)

// A Demodulator is one radio channel's receive side: the state of each of its
// subchannels' demodulators, and whether its input is muted while it transmits.
//
// NewDemodulator works out its layout - how many subchannels and slicers, and
// at what sample rate - from a copy of the channel's settings, and keeps it,
// rather than writing it back into the configuration.
//
// It is driven by its audio device's goroutine; the mute is the exception, set
// by PTT.Set on the transmit thread, hence atomic.
type Demodulator struct {
	channel   int
	modemType modem_t

	profiles   string // Normalised: one letter per demodulator, then any "+".
	numSubchan int    // How many demodulators.
	numSlicers int    // How many slicers each demodulator has.
	decimate   int    // AFSK: how many samples are averaged into one.
	upsample   int    // G3RUH and AIS: how many samples each is made into.

	states [MAX_SUBCHANS]demodulator_state_s // One per subchannel.
	muted  atomic.Bool
}

// demodulators holds every radio channel's Demodulator.  demod_init builds
// them, so until then, and for a channel that is not a radio, it is nil.
var demodulators [MAX_RADIO_CHANS]*Demodulator

// audioLevelDecimation is how many audio samples pass between pushes of the
// received audio level to the metrics endpoint: ~10Hz at a 44.1kHz sample rate.
const audioLevelDecimation = 4410

/*------------------------------------------------------------------
 *
 * Name:        demod_init
 *
 * Purpose:     Initialize the demodulator(s) used for reception.
 *
 * Inputs:      pa		- Pointer to audio_s structure with
 *				  various parameters for the modem(s).
 *
 * Bugs:	This doesn't do much error checking so don't give it
 *		anything crazy.
 *
 *----------------------------------------------------------------*/

// capProfiles limits a channel's demodulator types to the number of
// demodulators there is room for.  Each letter is a demodulator of its own and
// the demodulator state is a fixed MAX_SUBCHANS wide, so a longer list would run
// off the end of it - which Dire Wolf left to an assert over the subchannel
// index, taking the whole program down at startup.  Say which channel asked
// for what instead, and use as many as there are.
func capProfiles(channel int, profiles string) string {
	if len(profiles) <= MAX_SUBCHANS {
		return profiles
	}

	logrus.WithFields(logrus.Fields{
		"channel":   channel,
		"profiles":  profiles,
		"requested": len(profiles),
		"available": MAX_SUBCHANS,
		"using":     profiles[:MAX_SUBCHANS],
	}).Error("More demodulator types than there are demodulators")

	return profiles[:MAX_SUBCHANS]
}

func demod_init(pa *audio_s) {
	for channel := range MAX_RADIO_CHANS {
		demodulators[channel] = nil

		if pa.chan_medium[channel] == MEDIUM_RADIO {
			var d = NewDemodulator(channel, pa.achan[channel], pa.adev[ACHAN2ADEV(channel)].samples_per_sec)
			demodulators[channel] = d

			// atest and the received frame display still read these from
			// the configuration.
			pa.achan[channel].num_subchan = d.NumSubchan()
			pa.achan[channel].num_slicers = d.NumSlicers()
		}

		// FIXME dw_printf ("-------- end of loop for chn %d \n", channel);
	} /* for chan ... */

	// Now the virtual channels.  FIXME:  could be single loop.

	for channel := MAX_RADIO_CHANS; channel < MAX_TOTAL_CHANS; channel++ {
		// FIXME dw_printf ("-------- virtual channel loop %d \n", channel);
		if channel == pa.igate_vchannel {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Channel %d: IGate virtual channel.\n", channel)
		}
	}
} /* end demod_init */

// NewDemodulator sets up the demodulators for a radio channel, from achan, its
// settings, and the sample rate of its audio device.  What it works out from
// them - how many subchannels and slicers, the normalised profiles, the
// decimation and upsampling ratios - it keeps: achan is its own copy, and the
// configuration is left as it was.
func NewDemodulator(channel int, achan achan_param_s, samplesPerSec int) *Demodulator {
	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	var demodulator = new(Demodulator)
	demodulator.channel = channel

	/*
	 * These are derived from config file parameters.
	 *
	 * numSubchan is number of demodulators.
	 * This can be increased by:
	 *	Multiple frequencies.
	 *	Multiple letters (not sure if I will continue this).
	 *
	 * numSlicers is set to max by the "+" option.
	 */
	var numSubchan = 1
	var numSlicers = 1

	switch achan.modem_type {
	case MODEM_OFF:

	case MODEM_AFSK, MODEM_EAS:
		/*
		 * Tear apart the profile and put it back together in a normalized form:
		 *	- At least one letter, supply suitable default if necessary.
		 *	- Upper case only.
		 *	- Any plus will be at the end.
		 */
		var num_letters = 0
		var justLettersBuilder strings.Builder
		var have_plus = 0

		var profileStr = achan.profiles
		for i, p := range profileStr {
			if unicode.IsLower(p) {
				justLettersBuilder.WriteRune(unicode.ToUpper(p))
				num_letters++
			} else if unicode.IsUpper(p) {
				justLettersBuilder.WriteRune(p)
				num_letters++
			} else if p == '+' {
				have_plus = 1

				if i+1 != len(profileStr) {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Channel %d: + option must appear at end of demodulator types \"%s\" \n",
						channel, achan.profiles)
				}
			} else if p == '-' {
				have_plus = -1

				if i+1 != len(profileStr) {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Channel %d: - option must appear at end of demodulator types \"%s\" \n",
						channel, achan.profiles)
				}
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Channel %d: Demodulator types \"%s\" can contain only letters and + - characters.\n",
					channel, achan.profiles)
			}
		}

		var just_letters = justLettersBuilder.String()

		Assert(num_letters == len(just_letters))

		/*
		 * Pick a good default demodulator if none specified.
		 * Previously, we had "D" optimized for 300 bps.
		 * Gone in 1.7 so it is always "A+".
		 */
		if num_letters == 0 {
			just_letters = "A"

			if have_plus != -1 {
				have_plus = 1 // Add as default for version 1.2
				// If not explicitly turned off.
			}
		}

		// The default above and the cap below both change just_letters,
		// so take the count from it once they are both done rather than
		// keeping the two in step by hand.
		just_letters = capProfiles(channel, just_letters)
		num_letters = len(just_letters)

		/*
		 * Special case for ARM.
		 * The higher end ARM chips have loads of power but many people
		 * are using a single core Pi Zero or similar.
		 * (I'm still using a model 1 for my digipeater/IGate!)
		 * Decreasing CPU requirement has a negligible impact on decoding performance.
		 *
		 * 	atest -PA- 01_Track_1.wav		--> 1002 packets decoded.
		 * 	atest -PA- -D3 01_Track_1.wav		--> 997 packets decoded.
		 *
		 * Someone concerned about 1/2 of one percent difference can add "-D 1"
		 */
		/* TODO KG
		#if __arm__
			      if (achan.decimate == 0) {
			        if (samplesPerSec > 40000) {
			          achan.decimate = 3;
			        }
			      }
		#endif
		*/

		/*
		 * Number of filter taps is proportional to number of audio samples in a "symbol" duration.
		 * These can get extremely large for low speeds, e.g. 300 baud.
		 * In this case, increase the decimation ration.  Crude approximation. Could be improved.
		 */
		if achan.decimate == 0 &&
			samplesPerSec > 40000 &&
			achan.baud < 600 {
			// Avoid enormous number of filter taps.
			achan.decimate = 3
		}

		/*
		 * Put it back together again.
		 */
		Assert(num_letters == len(just_letters))

		/* At this point, have_plus can have 3 values: */
		/* 	1 = turned on, either explicitly or by applied default */
		/*	-1 = explicitly turned off.  change to 0 here so it is false. */
		/* 	0 = off by default. */

		if have_plus == -1 {
			have_plus = 0
		}

		achan.profiles = just_letters

		Assert(len(achan.profiles) >= 1)

		if have_plus != 0 {
			achan.profiles += "+"
		}

		/* These can be increased later for the multi-frequency case. */

		numSubchan = num_letters
		numSlicers = 1

		/*
		 * Some error checking - Can use only one of these:
		 *
		 *	- Multiple letters.
		 *	- New + multi-slicer.
		 *	- Multiple frequencies.
		 */

		if have_plus != 0 && achan.num_freq > 1 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Channel %d: Demodulator + option can't be combined with multiple frequencies.\n", channel)
			numSubchan = 1 // Will be set higher later.
			achan.num_freq = 1
		}

		if num_letters > 1 && achan.num_freq > 1 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Channel %d: Multiple demodulator types can't be combined with multiple frequencies.\n", channel)

			achan.profiles = string(achan.profiles[0])
			num_letters = 1
		}

		if achan.decimate == 0 {
			achan.decimate = 1
			if strings.Contains(just_letters, "B") && samplesPerSec > 40000 {
				achan.decimate = 3
			}
		}

		text_color_set(DW_COLOR_DEBUG)
		dw_printf("Channel %d: %d baud, AFSK %d & %d Hz, %s, %d sample rate",
			channel, achan.baud,
			achan.mark_freq, achan.space_freq,
			achan.profiles,
			samplesPerSec)

		if achan.decimate != 1 {
			dw_printf(" / %d", achan.decimate)
		}

		dw_printf(", Tx %s", achan.layer2_xmit)

		if achan.dtmf_decode != DTMF_DECODE_OFF {
			dw_printf(", DTMF decoder enabled")
		}

		dw_printf(".\n")

		/*
		 * Initialize the demodulator(s).
		 *
		 * We have 3 cases to consider.
		 */

		// TODO1.3: revisit this logic now that it is less restrictive.

		if num_letters > 1 {
			/*
			 * Multiple letters, usually for 1200 baud.
			 * Each one corresponds to a demodulator and subchannel.
			 *
			 * An interesting experiment but probably not too useful.
			 * Can't have multiple frequency pairs.
			 * In version 1.3 this can be combined with the + option.
			 */
			numSubchan = num_letters

			if numSubchan != num_letters {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("INTERNAL ERROR, chan=%d, num_subchan(%d) != strlen(\"%s\")\n",
					channel, numSubchan, achan.profiles)
			}

			if achan.num_freq != 1 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("INTERNAL ERROR, chan=%d, num_freq(%d) != 1\n",
					channel, achan.num_freq)
			}

			for d := range numSubchan {
				Assert(d >= 0 && d < MAX_SUBCHANS)

				var D = &demodulator.states[d]

				var profile = achan.profiles[d]
				var mark = achan.mark_freq
				var space = achan.space_freq

				if numSubchan != 1 {
					text_color_set(DW_COLOR_DEBUG)
					dw_printf("        %d.%d: %c %d & %d\n", channel, d, profile, mark, space)
				}

				demod_afsk_init(samplesPerSec/achan.decimate,
					achan.baud,
					mark,
					space,
					rune(profile),
					D)

				if have_plus != 0 {
					/* I'm not happy about putting this hack here. */
					/* should pass in as a parameter rather than adding on later. */
					numSlicers = MAX_SLICERS
					D.num_slicers = MAX_SLICERS
				}

				/* For signal level reporting, we want a longer term view. */
				// TODO: Should probably move this into the init functions.

				D.quick_attack = D.agc_fast_attack * 0.2
				D.sluggish_decay = D.agc_slow_decay * 0.2
			}
		} else if have_plus != 0 {
			/*
			 * PLUS - which (formerly) implies we have only one letter and one frequency pair.
			 *
			 * One demodulator feeds multiple slicers, each a subchannel.
			 */
			if num_letters != 1 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("INTERNAL ERROR, chan=%d, strlen(\"%s\") != 1\n",
					channel, just_letters)
			}

			if achan.num_freq != 1 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("INTERNAL ERROR, chan=%d, num_freq(%d) != 1\n",
					channel, achan.num_freq)
			}

			if achan.num_freq != numSubchan {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("INTERNAL ERROR, chan=%d, num_freq(%d) != num_subchan(%d)\n",
					channel, achan.num_freq, numSubchan)
			}

			var D = &demodulator.states[0]

			/* I'm not happy about putting this hack here. */
			/* This belongs in demod_afsk_init but it doesn't have access to the audio config. */

			numSlicers = MAX_SLICERS

			demod_afsk_init(samplesPerSec/achan.decimate,
				achan.baud,
				achan.mark_freq,
				achan.space_freq,
				rune(achan.profiles[0]),
				D)

			if have_plus != 0 {
				/* I'm not happy about putting this hack here. */
				/* should pass in as a parameter rather than adding on later. */
				numSlicers = MAX_SLICERS
				D.num_slicers = MAX_SLICERS
			}

			/* For signal level reporting, we want a longer term view. */

			D.quick_attack = D.agc_fast_attack * 0.2
			D.sluggish_decay = D.agc_slow_decay * 0.2
		} else {
			/*
			 * One letter.
			 * Can be combined with multiple frequencies.
			 */
			if num_letters != 1 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("INTERNAL ERROR, chan=%d, strlen(\"%s\") != 1\n",
					channel, achan.profiles)
			}

			numSubchan = achan.num_freq

			for d := range achan.num_freq {
				Assert(d >= 0 && d < MAX_SUBCHANS)

				var D = &demodulator.states[d]

				var profile = achan.profiles[0]

				var k = d*achan.offset - ((achan.num_freq-1)*achan.offset)/2
				var mark = achan.mark_freq + k
				var space = achan.space_freq + k

				if achan.num_freq != 1 {
					text_color_set(DW_COLOR_DEBUG)
					dw_printf("        %d.%d: %c %d & %d\n", channel, d, profile, mark, space)
				}

				demod_afsk_init(samplesPerSec/achan.decimate,
					achan.baud,
					mark, space,
					rune(profile),
					D)

				if have_plus != 0 {
					/* I'm not happy about putting this hack here. */
					/* should pass in as a parameter rather than adding on later. */
					numSlicers = MAX_SLICERS
					D.num_slicers = MAX_SLICERS
				}

				/* For signal level reporting, we want a longer term view. */

				D.quick_attack = D.agc_fast_attack * 0.2
				D.sluggish_decay = D.agc_slow_decay * 0.2
			} /* for each freq pair */
		}

	case MODEM_QPSK: // New for 1.4
		// settleModemOptions has normally picked one already.  Without
		// it, e.g. in a test, take the same default but leave the
		// configuration alone.
		var v26 = achan.v26_alternative
		if v26 == V26_UNSPECIFIED {
			v26 = V26_DEFAULT
		}

		// TODO: See how much CPU this takes on ARM and decide if we should have different defaults.

		if achan.profiles == "" {
			//#if __arm__
			//	        strlcpy (achan.profiles, "R", sizeof(achan.profiles));
			//#else
			achan.profiles = "PQRS"
			//#endif
		}

		achan.profiles = capProfiles(channel, achan.profiles)
		numSubchan = len(achan.profiles)

		text_color_set(DW_COLOR_DEBUG)
		dw_printf("Channel %d: %d bps, QPSK, %s, %d sample rate",
			channel, achan.baud,
			achan.profiles,
			samplesPerSec)

		dw_printf(", Tx %s", achan.layer2_xmit)

		if v26 == V26_B {
			dw_printf(", compatible with MFJ-2400")
		} else {
			dw_printf(", compatible with earlier direwolf")
		}

		if achan.dtmf_decode != DTMF_DECODE_OFF {
			dw_printf(", DTMF decoder enabled")
		}

		dw_printf(".\n")

		for d := range numSubchan {
			Assert(d >= 0 && d < MAX_SUBCHANS)
			var D = &demodulator.states[d]
			var profile = achan.profiles[d]

			//text_color_set(DW_COLOR_DEBUG);
			//dw_printf ("About to call demod_psk_init for Q-PSK case, modem_type=%d, profile='%c'\n",
			//	achan.modem_type, profile);

			demod_psk_init(achan.modem_type,
				v26,
				samplesPerSec,
				achan.baud,
				rune(profile),
				D)

			//text_color_set(DW_COLOR_DEBUG);
			//dw_printf ("Returned from demod_psk_init\n");

			/* For signal level reporting, we want a longer term view. */
			/* Guesses based on 9600.  Maybe revisit someday. */

			D.quick_attack = 0.080 * 0.2
			D.sluggish_decay = 0.00012 * 0.2
		}

	case MODEM_8PSK: // New for 1.4
		// TODO: See how much CPU this takes on ARM and decide if we should have different defaults.
		if achan.profiles == "" {
			//#if __arm__
			//	        strlcpy (achan.profiles, "V", sizeof(achan.profiles));
			//#else
			achan.profiles = "TUVW"
			//#endif
		}

		achan.profiles = capProfiles(channel, achan.profiles)
		numSubchan = len(achan.profiles)

		text_color_set(DW_COLOR_DEBUG)
		dw_printf("Channel %d: %d bps, 8PSK, %s, %d sample rate",
			channel, achan.baud,
			achan.profiles,
			samplesPerSec)

		dw_printf(", Tx %s", achan.layer2_xmit)

		if achan.dtmf_decode != DTMF_DECODE_OFF {
			dw_printf(", DTMF decoder enabled")
		}

		dw_printf(".\n")

		for d := range numSubchan {
			Assert(d >= 0 && d < MAX_SUBCHANS)
			var D = &demodulator.states[d]
			var profile = achan.profiles[d]

			//text_color_set(DW_COLOR_DEBUG);
			//dw_printf ("About to call demod_psk_init for 8-PSK case, modem_type=%d, profile='%c'\n",
			//	achan.modem_type, profile);

			demod_psk_init(achan.modem_type,
				achan.v26_alternative,
				samplesPerSec,
				achan.baud,
				rune(profile),
				D)

			//text_color_set(DW_COLOR_DEBUG);
			//dw_printf ("Returned from demod_psk_init\n");

			/* For signal level reporting, we want a longer term view. */
			/* Guesses based on 9600.  Maybe revisit someday. */

			D.quick_attack = 0.080 * 0.2
			D.sluggish_decay = 0.00012 * 0.2
		}

	case MODEM_BPSK:
		if achan.profiles == "" {
			achan.profiles = "LMNO"
		}

		achan.profiles = capProfiles(channel, achan.profiles)
		numSubchan = len(achan.profiles)

		text_color_set(DW_COLOR_DEBUG)
		dw_printf("Channel %d: %d bps, BPSK, %s, %d sample rate",
			channel, achan.baud,
			achan.profiles,
			samplesPerSec)

		dw_printf(", Tx %s", achan.layer2_xmit)

		if achan.dtmf_decode != DTMF_DECODE_OFF {
			dw_printf(", DTMF decoder enabled")
		}

		dw_printf(".\n")

		for d := range numSubchan {
			Assert(d >= 0 && d < MAX_SUBCHANS)
			var D = &demodulator.states[d]
			var profile = achan.profiles[d]

			demod_psk_init(achan.modem_type,
				V26_UNSPECIFIED,
				samplesPerSec,
				achan.baud,
				rune(profile),
				D)

			D.quick_attack = 0.080 * 0.2
			D.sluggish_decay = 0.00012 * 0.2
		}

	//TODO: how about MODEM_OFF case?

	default: /* Not AFSK */
		/*
		   case MODEM_BASEBAND:
		   case MODEM_SCRAMBLE:
		   case MODEM_AIS:
		*/
		{
			if achan.profiles == "" {
				/* Apply default if not set earlier. */
				/* Not sure if it should be on for ARM too. */
				/* Need to take a look at CPU usage and performance difference. */

				/* Version 1.5:  Remove special case for ARM. */
				/* We want higher performance to be the default. */
				/* "MODEM 9600 -" can be used on very slow CPU if necessary. */
				achan.profiles = "+"
			}

			/*
			 * We need a minimum number of audio samples per bit time for good performance.
			 * Easier to check here because demod_9600_init might have an adjusted sample rate.
			 */

			var ratio = float64(samplesPerSec) / float64(achan.baud)

			/*
			 * Set reasonable upsample ratio if user did not override.
			 */

			if achan.upsample == 0 {
				if ratio < 4 {
					// This is extreme.
					// No one should be using a sample rate this low but
					// amazingly a recording with 22050 rate can be decoded.
					// 3 and 4 are the same.  Need more tests.
					achan.upsample = 4
				} else if ratio < 5 {
					// example: 44100 / 9600 is 4.59
					// 3 is slightly better than 2 or 4.
					achan.upsample = 3
				} else if ratio < 10 {
					// example: 48000 / 9600 = 5
					// 3 is slightly better than 2 or 4.
					achan.upsample = 3
				} else if ratio < 15 {
					// ... guessing
					achan.upsample = 2
				} else { // >= 15
					//
					// An example of this might be .....
					// Probably no benefit.
					achan.upsample = 1
				}
			}

			/* TODO KG
			#ifdef TUNE_UPSAMPLE
				      achan.upsample = TUNE_UPSAMPLE;
			#endif
			*/

			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Channel %d: %d baud, %s, %s, %d sample rate x %d",
				channel,
				achan.baud,
				IfThenElse(achan.modem_type == MODEM_AIS, "AIS", "K9NG/G3RUH"),
				achan.profiles,
				samplesPerSec,
				achan.upsample)
			dw_printf(", Tx %s", achan.layer2_xmit)

			if achan.dtmf_decode != DTMF_DECODE_OFF {
				dw_printf(", DTMF decoder enabled")
			}

			dw_printf(".\n")

			var D = &demodulator.states[0] // first subchannel

			numSubchan = 1
			numSlicers = 1

			if strings.Contains(achan.profiles, "+") {
				/* I'm not happy about putting this hack here. */
				/* This belongs in demod_9600_init but it doesn't have access to the audio config. */
				numSlicers = MAX_SLICERS
			}

			text_color_set(DW_COLOR_INFO)
			dw_printf("The ratio of audio samples per sec (%d) to data rate in baud (%d) is %.1f\n",
				samplesPerSec,
				achan.baud,
				ratio)

			if ratio < 3 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("There is little hope of success with such a low ratio.  Use a higher sample rate.\n")
			} else if ratio < 5 {
				dw_printf("This is on the low side for best performance.  Can you use a higher sample rate?\n")

				if samplesPerSec == 44100 {
					dw_printf("For example, can you use 48000 rather than 44100?\n")
				}
			} else if ratio < 6 {
				dw_printf("Increasing the sample rate should improve decoder performance.\n")
			} else if ratio > 15 {
				dw_printf("Sample rate is more than adequate.  You might lower it if CPU load is a concern.\n")
			} else {
				dw_printf("This is a suitable ratio for good performance.\n")
			}

			demod_9600_init(achan.modem_type,
				samplesPerSec,
				achan.upsample,
				achan.baud, D)

			if strings.Contains(achan.profiles, "+") {
				/* I'm not happy about putting this hack here. */
				/* should pass in as a parameter rather than adding on later. */
				numSlicers = MAX_SLICERS
				D.num_slicers = MAX_SLICERS
			}

			/* For signal level reporting, we want a longer term view. */

			D.quick_attack = D.agc_fast_attack * 0.2
			D.sluggish_decay = D.agc_slow_decay * 0.2
		}
	} /* switch on modulation type. */

	demodulator.modemType = achan.modem_type
	demodulator.profiles = achan.profiles
	demodulator.numSubchan = numSubchan
	demodulator.numSlicers = numSlicers
	demodulator.decimate = achan.decimate
	demodulator.upsample = achan.upsample

	return demodulator
}

// NumSubchan is how many demodulators the channel has, each with its own
// subchannel.
func (d *Demodulator) NumSubchan() int {
	return d.numSubchan
}

// NumSlicers is how many slicers each of the channel's demodulators has.
func (d *Demodulator) NumSlicers() int {
	return d.numSlicers
}

/*------------------------------------------------------------------
 *
 * Name:        demod_get_sample
 *
 * Purpose:     Get one audio sample from the specified sound input source.
 *
 * Inputs:	a	- Index for audio device.  0 = first.
 *
 * Returns:     -32768 .. 32767 for a valid audio sample.
 *              256*256 for end of file or other error.
 *
 * Inputs:	a	- Audio device number.
 *
 *		src	- Where the sample data comes from.
 *
 * Global In:	save_audio_config_p.adev[ACHAN2ADEV(channel)].bits_per_sample - So we know whether to
 *			read 1 or 2 bytes from audio stream.
 *
 * Description:	Grab 1 or two bytes depending on data source.
 *
 *		When processing stereo, the caller will call this
 *		at twice the normal rate to obtain alternating left
 *		and right samples.
 *
 *----------------------------------------------------------------*/

const FSK_READ_ERR = (256 * 256)

// A SampleSource is where the audio the demodulators work on comes from: one
// byte of sample data at a time, or -1 when there is no more.
//
// audioDeviceSource is the one a running Samoyed uses; samoyed-atest reads a
// .WAV file instead, and a test hands over bytes of its own.
type SampleSource interface {
	GetByte(adev int) int
}

func demod_get_sample(a int, src SampleSource) int {
	Assert(save_audio_config_p.adev[a].bits_per_sample == 8 || save_audio_config_p.adev[a].bits_per_sample == 16)

	// TODO KG Originally this was a C signed short with the comment "short to force sign extension" - forcing via int16 seems to do the right thing...
	var sam int16

	if save_audio_config_p.adev[a].bits_per_sample == 8 {
		var x1 = src.GetByte(a)
		if x1 < 0 {
			return (FSK_READ_ERR)
		}

		Assert(x1 >= 0 && x1 <= 255)

		/* Scale 0..255 into -32k..+32k */

		sam = int16(x1-128) * 256
	} else {
		var x1 = src.GetByte(a) /* lower byte first */
		if x1 < 0 {
			return (FSK_READ_ERR)
		}

		var x2 = src.GetByte(a)
		if x2 < 0 {
			return (FSK_READ_ERR)
		}

		Assert(x1 >= 0 && x1 <= 255)
		Assert(x2 >= 0 && x2 <= 255)

		sam = int16(x2<<8) | int16(x1)
	}

	return int(sam)
}

/*-------------------------------------------------------------------
 *
 * Name:        Demodulator.ProcessSample
 *
 * Purpose:     (1) Demodulate the AFSK signal.
 *		(2) Recover clock and data.
 *
 * Inputs:	chan	- Audio channel.  0 for left, 1 for right.
 *		subchan - modem of the channel.
 *		sam	- One sample of audio.
 *			  Should be in range of -32768 .. 32767.
 *
 * Returns:	None
 *
 * Descripion:	We start off with two bandpass filters tuned to
 *		the given frequencies.  In the case of VHF packet
 *		radio, this would be 1200 and 2200 Hz.
 *
 *		The bandpass filter amplitudes are compared to
 *		obtain the demodulated signal.
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
 *--------------------------------------------------------------------*/

// New in 1.7.
// A few people have a really bad audio cross talk situation where they receive their own transmissions.
// It usually doesn't cause a problem but it is confusing to look at.
// "half duplex" setting applied only to the transmit logic.  i.e. wait for clear channel before sending.
// Receiving was still active.
// I think the simplest solution is to mute/unmute the audio input at this point if not full duplex.
// This is called from PTT.Set for half duplex.

func demod_mute_input(channel int, mute_during_xmit int) {
	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	// Transmit calibration, for one, keys a channel that may not be listening.
	var d = demodulators[channel]
	if d == nil {
		return
	}

	d.Mute(mute_during_xmit != 0)
}

// Mute silences the channel's input, or stops silencing it.
func (d *Demodulator) Mute(mute bool) {
	d.muted.Store(mute)
}

// ProcessSample hands one audio sample, in the range -32768 to 32767, to the
// subchannel's demodulator.
func (d *Demodulator) ProcessSample(subchan int, sam int) {
	Assert(subchan >= 0 && subchan < MAX_SUBCHANS)

	var channel = d.channel

	if d.muted.Load() {
		sam = 0
	}

	var D = &d.states[subchan]

	/* Scale to nice number, actually -2.0 to +2.0 for extra headroom */

	var fsam = float64(sam) / 16384.0

	/*
	 * Accumulate measure of the input signal level.
	 */

	/*
	 * Version 1.2: Try new approach to capturing the amplitude.
	 * This is same as the later AGC without the normalization step.
	 * We want decay to be substantially slower to get a longer
	 * range idea of the received audio.
	 */

	if fsam >= D.alevel_rec_peak {
		D.alevel_rec_peak = fsam*D.quick_attack + D.alevel_rec_peak*(1.0-D.quick_attack)
	} else {
		D.alevel_rec_peak = fsam*D.sluggish_decay + D.alevel_rec_peak*(1.0-D.sluggish_decay)
	}

	if fsam <= D.alevel_rec_valley {
		D.alevel_rec_valley = fsam*D.quick_attack + D.alevel_rec_valley*(1.0-D.quick_attack)
	} else {
		D.alevel_rec_valley = fsam*D.sluggish_decay + D.alevel_rec_valley*(1.0-D.sluggish_decay)
	}

	if subchan == 0 {
		D.alevel_metric_countdown--
		if D.alevel_metric_countdown <= 0 {
			D.alevel_metric_countdown = audioLevelDecimation

			metrics.SetAudioLevel(channel, demod_get_audio_level(channel, 0).rec)
		}
	}

	/*
	 * Select decoder based on modulation type.
	 */

	switch d.modemType {
	case MODEM_OFF:

		// Might have channel only listening to DTMF for APRStt gateway.
		// Don't waste CPU time running a demodulator here.

	case MODEM_AFSK, MODEM_EAS:
		if d.decimate > 1 {
			D.decimate_sum += sam

			D.decimate_count++
			if D.decimate_count >= d.decimate {
				var decimated = D.decimate_sum / d.decimate

				D.decimate_sum = 0
				D.decimate_count = 0

				demod_afsk_process_sample(channel, subchan, decimated, D)
			}
		} else {
			demod_afsk_process_sample(channel, subchan, sam, D)
		}

	case MODEM_QPSK, MODEM_8PSK, MODEM_BPSK:
		// Decimation would probably work but hasn't been thought about or
		// tested yet, so PSK is always demodulated at the full sample rate.
		demod_psk_process_sample(channel, subchan, sam, D)

	default:
		/*
		  case MODEM_BASEBAND:
		  case MODEM_SCRAMBLE:
		  case MODEM_AIS:
		*/
		demod_9600_process_sample(channel, sam, d.upsample, D)
	} /* switch modem_type */
} /* end ProcessSample */

/* Doesn't seem right.  Need to revisit this. */
/* Resulting scale is 0 to almost 100. */
/* Cranking up the input level produces no more than 97 or 98. */
/* We currently produce a message when this goes over 90. */

func demod_get_audio_level(channel int, subchan int) ALevel {
	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	// audio_stats asks after both of a stereo device's channels, whether or
	// not demod_init set them up.
	var d = demodulators[channel]
	if d == nil {
		var alevel ALevel

		return alevel
	}

	return d.AudioLevel(subchan)
}

// AudioLevel reports the received audio level the subchannel's demodulator
// has seen, and for AFSK its mark and space amplitudes.
func (d *Demodulator) AudioLevel(subchan int) ALevel {
	Assert(subchan >= 0 && subchan < MAX_SUBCHANS)

	/* We have to consider two different cases here. */
	/* N demodulators, each with own slicer and HDLC decoder. */
	/* Single demodulator, multiple slicers each with own HDLC decoder. */

	if d.states[0].num_slicers > 1 {
		subchan = 0
	}

	var D = &d.states[subchan]
	var alevel ALevel

	// Take half of peak-to-peak for received audio level.

	alevel.rec = int((D.alevel_rec_peak-D.alevel_rec_valley)*50.0 + 0.5)

	switch d.modemType {
	case MODEM_AFSK, MODEM_EAS:
		/* For AFSK, we have mark and space amplitudes. */
		alevel.mark = (int)((D.alevel_mark_peak)*100.0 + 0.5)
		alevel.space = (int)((D.alevel_space_peak)*100.0 + 0.5)
	case MODEM_QPSK, MODEM_8PSK, MODEM_BPSK:
		alevel.mark = -1
		alevel.space = -1
	default:
		// TODO KG #if 1
		/* Display the + and - peaks.  */
		/* Normally we'd expect them to be about the same. */
		/* However, with SDR, or other DC coupling, we could have an offset. */
		alevel.mark = (int)((D.alevel_mark_peak)*200.0 + 0.5)
		alevel.space = (int)((D.alevel_space_peak)*200.0 - 0.5)

		/* TODO KG
		#else
			  // Here we have + and - peaks after filtering.
			  // Take half of the peak to peak.
			  // The "5/6" factor worked out right for the current low pass filter.
			  // Will it need to be different if the filter is tweaked?

			  alevel.mark = (int) ((D.alevel_mark_peak - D.alevel_space_peak) * 100.0 * 5.0/6.0 + 0.5);
			  alevel.space = -1;		// to print one number inside of ( )
		#endif
		*/
	}

	return (alevel)
}

/* end demod.c */
