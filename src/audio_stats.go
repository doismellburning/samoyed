package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Print statistics for audio input stream.
 *
 * 		A common complaint is that there is no indication of
 *		audio input level until a packet is received correctly.
 *		That's true for the Windows version but the Linux version
 *		prints something like this each 100 seconds:
 *
 *		ADEVICE0: Sample rate approx. 44.1 k, 0 errors, receive audio level CH0 73
 *
 *		Some complain about the clutter but it has been a useful
 *		troubleshooting tool.  In the earlier RPi days, the sample
 *		rate was quite low due to a device driver issue.
 *		Using a USB hub on the RPi also caused audio problems.
 *		One adapter, that I tried, produces samples at the
 *		right rate but all the samples are 0.
 *
 *		Here we pull the code out of the Linux version of audio.c
 *		so we have a common function for all the platforms.
 *
 *		We also add a command line option to adjust the time
 *		between reports or turn them off entirely.
 *
 * Revisions: 	This is new in version 1.3.
 *
 *---------------------------------------------------------------*/

// #include <stdio.h>
// #include <unistd.h>
// #include <stdlib.h>
// #include <string.h>
// #include <sys/types.h>
// #include <sys/stat.h>
// #include <assert.h>
// #include <time.h>
import "C"

import (
	"time"
)

/*------------------------------------------------------------------
*
* Name:        audio_stats
*
* Purpose:     Add sample count from one buffer to the statistics.
*		Print if specified amount of time has passed.
*
* Inputs:	adev	- Audio device number:  0, 1, ..., MAX_ADEVS-1
*
		nchan	- Number of channels for this device, 1 or 2.
*
*		nsamp	- How many audio samples were read.
*
*		interval - How many seconds between reports.
*				0 to turn off.
*
* Returns:     none
*
* Description:	...
*
*----------------------------------------------------------------*/

var audioStatsLastTime [MAX_ADEVS]time.Time
var audioStatsSampleCount [MAX_ADEVS]int
var audioStatsErrorCount [MAX_ADEVS]int
var audioStatsSuppressFirst [MAX_ADEVS]bool

func audio_stats(adev C.int, nchan int, nsamp int, interval int) {

	/* Gather numbers for read from audio device. */

	if interval <= 0 {
		return
	}

	Assert(adev >= 0 && adev < MAX_ADEVS)

	/*
	 * Print information about the sample rate as a troubleshooting aid.
	 * I've never seen an issue with Windows or x86 Linux but the Raspberry Pi
	 * has a very troublesome audio input system where many samples got lost.
	 *
	 * While we are at it we can also print the current audio level(s) providing
	 * more clues if nothing is being decoded.
	 */

	if audioStatsLastTime[adev].IsZero() {
		audioStatsLastTime[adev] = time.Now()
		audioStatsSampleCount[adev] = 0
		audioStatsErrorCount[adev] = 0
		audioStatsSuppressFirst[adev] = true
		/* suppressing the first one could mean a rather */
		/* long wait for the first message.  We make the */
		/* first collection interval 3 seconds. */
		audioStatsLastTime[adev] = audioStatsLastTime[adev].Add(-1 * time.Duration(interval-3) * time.Second)
	} else {
		if nsamp > 0 {
			audioStatsSampleCount[adev] += nsamp
		} else {
			audioStatsErrorCount[adev]++
		}
		var this_time = time.Now()
		if !this_time.Before(audioStatsLastTime[adev].Add(time.Duration(interval) * time.Second)) {

			if audioStatsSuppressFirst[adev] {

				/* The issue we had is that the first time the rate */
				/* would be off considerably because we didn't start */
				/* on a second boundary.  So we will suppress printing */
				/* of the first one.  */

				audioStatsSuppressFirst[adev] = false
			} else {
				var ave_rate = (float64(audioStatsSampleCount[adev]) / 1000.0) / float64(interval)

				text_color_set(DW_COLOR_DEBUG)

				if nchan > 1 {
					var ch0 = C.int(ADEVFIRSTCHAN(int(adev)))
					var alevel0 = demod_get_audio_level(ch0, 0)
					var ch1 = C.int(ADEVFIRSTCHAN(int(adev))) + 1
					var alevel1 = demod_get_audio_level(ch1, 0)

					dw_printf("\nADEVICE%d: Sample rate approx. %.1f k, %d errors, receive audio levels CH%d %d, CH%d %d\n\n",
						adev, ave_rate, audioStatsErrorCount[adev], ch0, alevel0.rec, ch1, alevel1.rec)
				} else {
					var ch0 = C.int(ADEVFIRSTCHAN(int(adev)))
					var alevel0 = demod_get_audio_level(ch0, 0)

					dw_printf("\nADEVICE%d: Sample rate approx. %.1f k, %d errors, receive audio level CH%d %d\n\n",
						adev, ave_rate, audioStatsErrorCount[adev], ch0, alevel0.rec)
				}
			}
			audioStatsLastTime[adev] = this_time
			audioStatsSampleCount[adev] = 0
			audioStatsErrorCount[adev] = 0
		}
	}
}
