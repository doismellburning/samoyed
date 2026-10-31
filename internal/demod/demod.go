// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package demod

/*------------------------------------------------------------------
 *
 * Purpose:   	Common entry point for multiple types of demodulators.
 *
 * Input:	Audio samples from either a file or the "sound card."
 *
 * Outputs:	Hands each bit demodulated to its receiver's RecBit.
 *
 *---------------------------------------------------------------*/

import (
	"fmt"
	"math"
	"strings"
	"sync/atomic"

	"github.com/doismellburning/samoyed/internal/ascii"
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/metrics"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/sirupsen/logrus"
)

// A Demodulator is one radio channel's receive side: the state of each of its
// subchannels' demodulators, and whether its input is muted while it transmits.
//
// New works out its layout - how many subchannels and slicers, and
// at what sample rate - from a copy of the channel's settings, and keeps it,
// rather than writing it back into the configuration.
//
// It is driven by its audio device's goroutine; the mute is the exception, set
// by PTT.Set on the transmit thread, hence atomic.
type Demodulator struct {
	channel   int
	modemType phy.Modem

	profiles   string // Normalised: one letter per demodulator, then any "+".
	numSubchan int    // How many demodulators.
	numSlicers int    // How many slicers each demodulator has.
	decimate   int    // AFSK: how many samples are averaged into one.
	upsample   int    // G3RUH and AIS: how many samples each is made into.

	states [phy.MaxSubchans]demodulator_state_s // One per subchannel.
	muted  atomic.Bool
}

// BitReceiver is what a demodulator hands each bit it demodulates to, and
// tells when it starts or stops detecting data on a slicer.
// *Layer2Receiver is one.
type BitReceiver interface {
	RecBit(from dwcore.SlicerID, raw bool, isScrambled bool, pllNudgeTotal *int64, pllSymbolCount *int)
	DCDChange(from dwcore.SlicerID, state int)
}

// audioLevelDecimation is how many audio samples pass between pushes of the
// received audio level to the metrics endpoint: ~10Hz at a 44.1kHz sample rate.
const audioLevelDecimation = 4410

// capProfiles limits a channel's demodulator types to the number of
// demodulators there is room for.  Each letter is a demodulator of its own and
// the demodulator state is a fixed phy.MaxSubchans wide, so a longer list would run
// off the end of it - which Dire Wolf left to an assert over the subchannel
// index, taking the whole program down at startup.  Say which channel asked
// for what instead, and use as many as there are.
func capProfiles(channel int, profiles string) string {
	if len(profiles) <= phy.MaxSubchans {
		return profiles
	}

	logrus.WithFields(logrus.Fields{
		"channel":   channel,
		"profiles":  profiles,
		"requested": len(profiles),
		"available": phy.MaxSubchans,
		"using":     profiles[:phy.MaxSubchans],
	}).Error("More demodulator types than there are demodulators")

	return profiles[:phy.MaxSubchans]
}

// New sets up the demodulators for a radio channel, from its modem
// settings and the sample rate of its audio device; layer2Xmit, how the channel
// frames what it sends, is only for the summary it logs.  What it works out
// from them - how many subchannels and slicers, the normalised profiles, the
// decimation and upsampling ratios - it keeps: modem is its own copy, and the
// configuration is left as it was.
func New(channel int, modem phy.ModemConfig, layer2Xmit phy.Layer2, samplesPerSec int) (*Demodulator, error) { //nolint:funlen // Ported from Dire Wolf as one function
	dwutil.Assert(channel >= 0 && channel < phy.MaxRadioChans)

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

	switch modem.ModemType {
	case phy.Off:

	case phy.AFSK, phy.EAS:
		/*
		 * Tear apart the profile and put it back together in a normalized form:
		 *	- At least one letter, supply suitable default if necessary.
		 *	- Upper case only.
		 *	- Any plus will be at the end.
		 */
		var num_letters = 0
		var justLettersBuilder strings.Builder
		var have_plus = 0

		var profileStr = modem.Profiles
		for i, p := range profileStr {
			if ascii.IsAlpha(p) {
				var letter = ascii.ToUpper(p)

				// demod_afsk_init knows only these; leave anything else out
				// rather than have it stop the program.
				if !strings.ContainsRune("ABDE", letter) {
					logrus.WithFields(logrus.Fields{
						"channel":  channel,
						"profiles": modem.Profiles,
						"profile":  string(letter),
					}).Error("Unknown AFSK demodulator type; ignoring it")

					continue
				}

				justLettersBuilder.WriteRune(letter)
				num_letters++
			} else if p == '+' {
				have_plus = 1

				if i+1 != len(profileStr) {
					logrus.WithFields(logrus.Fields{
						"channel":  channel,
						"profiles": modem.Profiles,
					}).Error("+ option must appear at end of demodulator types")
				}
			} else if p == '-' {
				have_plus = -1

				if i+1 != len(profileStr) {
					logrus.WithFields(logrus.Fields{
						"channel":  channel,
						"profiles": modem.Profiles,
					}).Error("- option must appear at end of demodulator types")
				}
			} else {
				logrus.WithFields(logrus.Fields{
					"channel":  channel,
					"profiles": modem.Profiles,
				}).Error("Demodulator types can contain only letters and + - characters")
			}
		}

		var just_letters = justLettersBuilder.String()

		dwutil.Assert(num_letters == len(just_letters))

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
		if modem.Decimate == 0 &&
			samplesPerSec > 40000 &&
			modem.Baud < 600 {
			// Avoid enormous number of filter taps.
			modem.Decimate = 3
		}

		/*
		 * Put it back together again.
		 */
		dwutil.Assert(num_letters == len(just_letters))

		/* At this point, have_plus can have 3 values: */
		/* 	1 = turned on, either explicitly or by applied default */
		/*	-1 = explicitly turned off.  change to 0 here so it is false. */
		/* 	0 = off by default. */

		if have_plus == -1 {
			have_plus = 0
		}

		modem.Profiles = just_letters

		dwutil.Assert(len(modem.Profiles) >= 1)

		if have_plus != 0 {
			modem.Profiles += "+"
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

		if have_plus != 0 && modem.NumFreq > 1 {
			logrus.WithField("channel", channel).Error("Demodulator + option can't be combined with multiple frequencies")
			numSubchan = 1 // Will be set higher later.
			modem.NumFreq = 1
		}

		if num_letters > 1 && modem.NumFreq > 1 {
			logrus.WithField("channel", channel).Error("Multiple demodulator types can't be combined with multiple frequencies")

			modem.Profiles = string(modem.Profiles[0])
			num_letters = 1
		}

		if modem.Decimate == 0 {
			modem.Decimate = 1
			if strings.Contains(just_letters, "B") && samplesPerSec > 40000 {
				modem.Decimate = 3
			}
		}

		var fields = logrus.Fields{
			"channel":      channel,
			"baud":         modem.Baud,
			"mark":         modem.MarkFreq,
			"space":        modem.SpaceFreq,
			"profiles":     modem.Profiles,
			"sample_rate":  samplesPerSec,
			"tx":           layer2Xmit,
			"dtmf_decoder": modem.DTMFDecode != phy.DTMFDecodeOff,
		}

		if modem.Decimate != 1 {
			fields["decimate"] = modem.Decimate
		}

		logrus.WithFields(fields).Info("AFSK demodulator")

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
				logrus.WithFields(logrus.Fields{
					"channel":     channel,
					"num_subchan": numSubchan,
					"profiles":    modem.Profiles,
				}).Error("INTERNAL ERROR: num_subchan != len(profiles)")
			}

			if modem.NumFreq != 1 {
				logrus.WithFields(logrus.Fields{
					"channel":  channel,
					"num_freq": modem.NumFreq,
				}).Error("INTERNAL ERROR: num_freq != 1")
			}

			for d := range numSubchan {
				dwutil.Assert(d >= 0 && d < phy.MaxSubchans)

				var D = &demodulator.states[d]

				var profile = modem.Profiles[d]
				var mark = modem.MarkFreq
				var space = modem.SpaceFreq

				if numSubchan != 1 {
					logrus.WithFields(logrus.Fields{
						"channel": channel,
						"subchan": d,
						"profile": string(rune(profile)),
						"mark":    mark,
						"space":   space,
					}).Info("AFSK subchannel")
				}

				demod_afsk_init(samplesPerSec/modem.Decimate,
					modem.Baud,
					mark,
					space,
					profile,
					D)

				if have_plus != 0 {
					/* I'm not happy about putting this hack here. */
					/* should pass in as a parameter rather than adding on later. */
					numSlicers = phy.MaxSlicers
					D.num_slicers = phy.MaxSlicers
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
				logrus.WithFields(logrus.Fields{
					"channel":  channel,
					"profiles": just_letters,
				}).Error("INTERNAL ERROR: len(profiles) != 1")
			}

			if modem.NumFreq != 1 {
				logrus.WithFields(logrus.Fields{
					"channel":  channel,
					"num_freq": modem.NumFreq,
				}).Error("INTERNAL ERROR: num_freq != 1")
			}

			if modem.NumFreq != numSubchan {
				logrus.WithFields(logrus.Fields{
					"channel":     channel,
					"num_freq":    modem.NumFreq,
					"num_subchan": numSubchan,
				}).Error("INTERNAL ERROR: num_freq != num_subchan")
			}

			var D = &demodulator.states[0]

			/* I'm not happy about putting this hack here. */
			/* This belongs in demod_afsk_init but it doesn't have access to the audio config. */

			numSlicers = phy.MaxSlicers

			demod_afsk_init(samplesPerSec/modem.Decimate,
				modem.Baud,
				modem.MarkFreq,
				modem.SpaceFreq,
				modem.Profiles[0],
				D)

			if have_plus != 0 {
				/* I'm not happy about putting this hack here. */
				/* should pass in as a parameter rather than adding on later. */
				numSlicers = phy.MaxSlicers
				D.num_slicers = phy.MaxSlicers
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
				logrus.WithFields(logrus.Fields{
					"channel":  channel,
					"profiles": modem.Profiles,
				}).Error("INTERNAL ERROR: len(profiles) != 1")
			}

			numSubchan = modem.NumFreq

			for d := range modem.NumFreq {
				dwutil.Assert(d >= 0 && d < phy.MaxSubchans)

				var D = &demodulator.states[d]

				var profile = modem.Profiles[0]

				var k = d*modem.Offset - ((modem.NumFreq-1)*modem.Offset)/2
				var mark = modem.MarkFreq + k
				var space = modem.SpaceFreq + k

				if modem.NumFreq != 1 {
					logrus.WithFields(logrus.Fields{
						"channel": channel,
						"subchan": d,
						"profile": string(rune(profile)),
						"mark":    mark,
						"space":   space,
					}).Info("AFSK subchannel")
				}

				demod_afsk_init(samplesPerSec/modem.Decimate,
					modem.Baud,
					mark, space,
					profile,
					D)

				if have_plus != 0 {
					/* I'm not happy about putting this hack here. */
					/* should pass in as a parameter rather than adding on later. */
					numSlicers = phy.MaxSlicers
					D.num_slicers = phy.MaxSlicers
				}

				/* For signal level reporting, we want a longer term view. */

				D.quick_attack = D.agc_fast_attack * 0.2
				D.sluggish_decay = D.agc_slow_decay * 0.2
			} /* for each freq pair */
		}

	case phy.QPSK: // New for 1.4
		// settleModemOptions has normally picked one already.  Without
		// it, e.g. in a test, take the same default but leave the
		// configuration alone.
		var v26 = modem.V26Alternative
		if v26 == phy.V26Unspecified {
			v26 = phy.V26Default
		}

		// TODO: See how much CPU this takes on ARM and decide if we should have different defaults.

		if modem.Profiles == "" {
			//#if __arm__
			//	        strlcpy (achan.profiles, "R", sizeof(achan.profiles));
			//#else
			modem.Profiles = defaultPSKProfiles(phy.QPSK)
			//#endif
		}

		modem.Profiles = capProfiles(channel, modem.Profiles)
		numSubchan = len(modem.Profiles)

		logrus.WithFields(logrus.Fields{
			"channel":         channel,
			"bps":             modem.Baud,
			"profiles":        modem.Profiles,
			"sample_rate":     samplesPerSec,
			"tx":              layer2Xmit,
			"compatible_with": dwutil.IfThenElse(v26 == phy.V26B, "MFJ-2400", "earlier direwolf"),
			"dtmf_decoder":    modem.DTMFDecode != phy.DTMFDecodeOff,
		}).Info("QPSK demodulator")

		for d := range numSubchan {
			dwutil.Assert(d >= 0 && d < phy.MaxSubchans)
			var D = &demodulator.states[d]
			var profile = modem.Profiles[d]

			//text_color_set(DW_COLOR_DEBUG);
			//dw_printf ("About to call demod_psk_init for Q-PSK case, modem_type=%d, profile='%c'\n",
			//	achan.modem_type, profile);

			var pskErr = demod_psk_init(modem.ModemType,
				v26,
				samplesPerSec,
				modem.Baud,
				profile,
				D)
			if pskErr != nil {
				return nil, fmt.Errorf("channel %d, profile %c: %w", channel, profile, pskErr)
			}

			//text_color_set(DW_COLOR_DEBUG);
			//dw_printf ("Returned from demod_psk_init\n");

			/* For signal level reporting, we want a longer term view. */
			/* Guesses based on 9600.  Maybe revisit someday. */

			D.quick_attack = 0.080 * 0.2
			D.sluggish_decay = 0.00012 * 0.2
		}

	//nolint:dupl // Per-modem setup, kept parallel to upstream
	case phy.PSK8: // New for 1.4
		// TODO: See how much CPU this takes on ARM and decide if we should have different defaults.
		if modem.Profiles == "" {
			//#if __arm__
			//	        strlcpy (achan.profiles, "V", sizeof(achan.profiles));
			//#else
			modem.Profiles = defaultPSKProfiles(phy.PSK8)
			//#endif
		}

		modem.Profiles = capProfiles(channel, modem.Profiles)
		numSubchan = len(modem.Profiles)

		logrus.WithFields(logrus.Fields{
			"channel":      channel,
			"bps":          modem.Baud,
			"profiles":     modem.Profiles,
			"sample_rate":  samplesPerSec,
			"tx":           layer2Xmit,
			"dtmf_decoder": modem.DTMFDecode != phy.DTMFDecodeOff,
		}).Info("8PSK demodulator")

		for d := range numSubchan {
			dwutil.Assert(d >= 0 && d < phy.MaxSubchans)
			var D = &demodulator.states[d]
			var profile = modem.Profiles[d]

			//text_color_set(DW_COLOR_DEBUG);
			//dw_printf ("About to call demod_psk_init for 8-PSK case, modem_type=%d, profile='%c'\n",
			//	achan.modem_type, profile);

			var pskErr = demod_psk_init(modem.ModemType,
				modem.V26Alternative,
				samplesPerSec,
				modem.Baud,
				profile,
				D)
			if pskErr != nil {
				return nil, fmt.Errorf("channel %d, profile %c: %w", channel, profile, pskErr)
			}

			//text_color_set(DW_COLOR_DEBUG);
			//dw_printf ("Returned from demod_psk_init\n");

			/* For signal level reporting, we want a longer term view. */
			/* Guesses based on 9600.  Maybe revisit someday. */

			D.quick_attack = 0.080 * 0.2
			D.sluggish_decay = 0.00012 * 0.2
		}

	//nolint:dupl // Per-modem setup, kept parallel to upstream
	case phy.BPSK:
		if modem.Profiles == "" {
			modem.Profiles = defaultPSKProfiles(phy.BPSK)
		}

		modem.Profiles = capProfiles(channel, modem.Profiles)
		numSubchan = len(modem.Profiles)

		logrus.WithFields(logrus.Fields{
			"channel":      channel,
			"bps":          modem.Baud,
			"profiles":     modem.Profiles,
			"sample_rate":  samplesPerSec,
			"tx":           layer2Xmit,
			"dtmf_decoder": modem.DTMFDecode != phy.DTMFDecodeOff,
		}).Info("BPSK demodulator")

		for d := range numSubchan {
			dwutil.Assert(d >= 0 && d < phy.MaxSubchans)
			var D = &demodulator.states[d]
			var profile = modem.Profiles[d]

			var pskErr = demod_psk_init(modem.ModemType,
				phy.V26Unspecified,
				samplesPerSec,
				modem.Baud,
				profile,
				D)
			if pskErr != nil {
				return nil, fmt.Errorf("channel %d, profile %c: %w", channel, profile, pskErr)
			}

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
			if modem.Profiles == "" {
				/* Apply default if not set earlier. */
				/* Not sure if it should be on for ARM too. */
				/* Need to take a look at CPU usage and performance difference. */

				/* Version 1.5:  Remove special case for ARM. */
				/* We want higher performance to be the default. */
				/* "MODEM 9600 -" can be used on very slow CPU if necessary. */
				modem.Profiles = "+"
			}

			/*
			 * We need a minimum number of audio samples per bit time for good performance.
			 * Easier to check here because demod_9600_init might have an adjusted sample rate.
			 */

			var ratio = float64(samplesPerSec) / float64(modem.Baud)

			/*
			 * Set reasonable upsample ratio if user did not override.
			 */

			if modem.Upsample == 0 {
				if ratio < 4 {
					// This is extreme.
					// No one should be using a sample rate this low but
					// amazingly a recording with 22050 rate can be decoded.
					// 3 and 4 are the same.  Need more tests.
					modem.Upsample = 4
				} else if ratio < 5 {
					// example: 44100 / 9600 is 4.59
					// 3 is slightly better than 2 or 4.
					modem.Upsample = 3
				} else if ratio < 10 {
					// example: 48000 / 9600 = 5
					// 3 is slightly better than 2 or 4.
					modem.Upsample = 3
				} else if ratio < 15 {
					// ... guessing
					modem.Upsample = 2
				} else { // >= 15
					//
					// An example of this might be .....
					// Probably no benefit.
					modem.Upsample = 1
				}
			}

			/* TODO KG
			#ifdef TUNE_UPSAMPLE
				      achan.upsample = TUNE_UPSAMPLE;
			#endif
			*/

			logrus.WithFields(logrus.Fields{
				"channel":      channel,
				"baud":         modem.Baud,
				"profiles":     modem.Profiles,
				"sample_rate":  samplesPerSec,
				"upsample":     modem.Upsample,
				"tx":           layer2Xmit,
				"dtmf_decoder": modem.DTMFDecode != phy.DTMFDecodeOff,
			}).Info(dwutil.IfThenElse(modem.ModemType == phy.AIS, "AIS demodulator", "K9NG/G3RUH demodulator"))

			var D = &demodulator.states[0] // first subchannel

			numSubchan = 1
			numSlicers = 1

			if strings.Contains(modem.Profiles, "+") {
				/* I'm not happy about putting this hack here. */
				/* This belongs in demod_9600_init but it doesn't have access to the audio config. */
				numSlicers = phy.MaxSlicers
			}

			var ratioLog = logrus.WithFields(logrus.Fields{
				"channel":     channel,
				"sample_rate": samplesPerSec,
				"baud":        modem.Baud,
				"ratio":       math.Round(ratio*10) / 10,
			})

			if ratio < 3 {
				ratioLog.Error("Sample rate to baud ratio is too low for much hope of success; use a higher sample rate")
			} else if ratio < 5 {
				if samplesPerSec == 44100 {
					ratioLog.Warn("Sample rate to baud ratio is on the low side for best performance; can you use 48000 rather than 44100?")
				} else {
					ratioLog.Warn("Sample rate to baud ratio is on the low side for best performance; can you use a higher sample rate?")
				}
			} else if ratio < 6 {
				ratioLog.Info("Increasing the sample rate should improve decoder performance")
			} else if ratio > 15 {
				ratioLog.Info("Sample rate is more than adequate; you might lower it if CPU load is a concern")
			} else {
				ratioLog.Info("Sample rate to baud ratio is suitable for good performance")
			}

			demod_9600_init(modem.ModemType,
				samplesPerSec,
				modem.Upsample,
				modem.Baud, D)

			if strings.Contains(modem.Profiles, "+") {
				/* I'm not happy about putting this hack here. */
				/* should pass in as a parameter rather than adding on later. */
				numSlicers = phy.MaxSlicers
				D.num_slicers = phy.MaxSlicers
			}

			/* For signal level reporting, we want a longer term view. */

			D.quick_attack = D.agc_fast_attack * 0.2
			D.sluggish_decay = D.agc_slow_decay * 0.2
		}
	} /* switch on modulation type. */

	demodulator.modemType = modem.ModemType
	demodulator.profiles = modem.Profiles
	demodulator.numSubchan = numSubchan
	demodulator.numSlicers = numSlicers
	demodulator.decimate = modem.Decimate
	demodulator.upsample = modem.Upsample

	return demodulator, nil
}

// SetReceiver makes r where every subchannel of d hands its bits.  It is set
// once d is built, rather than handed to New, to break a cycle: the
// Layer2Receiver that receives the bits is itself built from the demodulators.
func (d *Demodulator) SetReceiver(r BitReceiver) {
	for sub := range d.states {
		d.states[sub].receiver = r
	}
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

// Channel is the radio channel the demodulator listens on.
func (d *Demodulator) Channel() int {
	return d.channel
}

// Muted says whether the channel's input is being silenced, as Mute left it.
func (d *Demodulator) Muted() bool {
	return d.muted.Load()
}

// Decimate is the decimation the demodulator settled on: how many audio
// samples AFSK averages into one.  Below 2 means none.
func (d *Demodulator) Decimate() int {
	return d.decimate
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

// Mute silences the channel's input, or stops silencing it.
func (d *Demodulator) Mute(mute bool) {
	d.muted.Store(mute)
}

// ProcessSample hands one audio sample, in the range -32768 to 32767, to each
// of the channel's demodulators.
func (d *Demodulator) ProcessSample(sam int) {
	for subchan := range d.numSubchan {
		d.processSubchanSample(subchan, sam)
	}
}

// processSubchanSample hands one audio sample to one subchannel's
// demodulator.
func (d *Demodulator) processSubchanSample(subchan int, sam int) {
	dwutil.Assert(subchan >= 0 && subchan < phy.MaxSubchans)

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

			metrics.SetAudioLevel(channel, d.AudioLevel(0).Rec)
		}
	}

	/*
	 * Select decoder based on modulation type.
	 */

	switch d.modemType {
	case phy.Off:

		// Might have channel only listening to DTMF for APRStt gateway.
		// Don't waste CPU time running a demodulator here.

	case phy.AFSK, phy.EAS:
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

	case phy.QPSK, phy.PSK8, phy.BPSK:
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

// AudioLevel reports the received audio level the subchannel's demodulator
// has seen, and for AFSK its mark and space amplitudes.
func (d *Demodulator) AudioLevel(subchan int) ax25.ALevel {
	dwutil.Assert(subchan >= 0 && subchan < phy.MaxSubchans)

	/* We have to consider two different cases here. */
	/* N demodulators, each with own slicer and HDLC decoder. */
	/* Single demodulator, multiple slicers each with own HDLC decoder. */

	if d.states[0].num_slicers > 1 {
		subchan = 0
	}

	var D = &d.states[subchan]
	var alevel ax25.ALevel

	// Take half of peak-to-peak for received audio level.

	alevel.Rec = int((D.alevel_rec_peak-D.alevel_rec_valley)*50.0 + 0.5)

	switch d.modemType {
	case phy.AFSK, phy.EAS:
		/* For AFSK, we have mark and space amplitudes. */
		alevel.Mark = (int)((D.alevel_mark_peak)*100.0 + 0.5)
		alevel.Space = (int)((D.alevel_space_peak)*100.0 + 0.5)
	case phy.QPSK, phy.PSK8, phy.BPSK:
		alevel.Mark = -1
		alevel.Space = -1
	default:
		// TODO KG #if 1
		/* Display the + and - peaks.  */
		/* Normally we'd expect them to be about the same. */
		/* However, with SDR, or other DC coupling, we could have an offset. */
		alevel.Mark = (int)((D.alevel_mark_peak)*200.0 + 0.5)
		alevel.Space = (int)((D.alevel_space_peak)*200.0 - 0.5)

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
