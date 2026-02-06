package direwolf

//
//    This file is part of Dire Wolf, an amateur radio packet TNC.
//
//    Copyright (C) 2014, 2015, 2016  John Langner, WB2OSZ
//
//    This program is free software: you can redistribute it and/or modify
//    it under the terms of the GNU General Public License as published by
//    the Free Software Foundation, either version 2 of the License, or
//    (at your option) any later version.
//
//    This program is distributed in the hope that it will be useful,
//    but WITHOUT ANY WARRANTY; without even the implied warranty of
//    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//    GNU General Public License for more details.
//
//    You should have received a copy of the GNU General Public License
//    along with this program.  If not, see <http://www.gnu.org/licenses/>.
//

/*------------------------------------------------------------------
 *
 * Module:      recv.c
 *
 * Purpose:   	Process audio input for receiving.
 *
 *		This is for all platforms.
 *
 *
 * Description:	In earlier versions, we supported a single audio device
 *		and the main program looped around processing the
 *		audio samples.  The structure looked like this:
 *
 *		main in direwolf.c:
 *
 *			audio_init()
 *			various other *_init()
 *
 *			loop forever:
 *				s = demod_get_sample.
 *				multi_modem_process_sample(s)
 *
 *
 *		When a packet is successfully decoded, somebody calls
 *		app_process_rec_frame, also in direwolf.c
 *
 *
 *		Starting in version 1.2, we support multiple audio
 *		devices at the same time.  We now have a separate
 *		thread for each audio device.   Decoded frames are
 *		sent to a single queue for serial processing.
 *
 *		The new flow looks like this:
 *
 *		main in direwolf.c:
 *
 *			audio_init()
 *			various other *_init()
 *			recv_init()
 *			recv_process()  -- does not return
 *
 *
 *		recv_init()		This starts up a separate thread
 *					for each audio device.
 *					Each thread reads audio samples and
 *					passes them to multi_modem_process_sample.
 *
 *					The difference is that app_process_rec_frame
 *					is no longer called directly.  Instead
 *					the frame is appended to a queue with dlq_rec_frame.
 *
 *					Received frames can now be processed one at
 *					a time and we don't need to worry about later
 *					processing being reentrant.
 *
 *		recv_process()  	This simply waits for something to show up
 *					in the dlq queue and calls app_process_rec_frame
 *					for each.
 *
 *---------------------------------------------------------------*/

// #include <stdio.h>
// #include <unistd.h>
// #include <stdlib.h>
// #include <string.h>
// #include <stddef.h>
// #include <sys/types.h>
// #include <assert.h>
import "C"

import (
	"os"
)

var save_pa *audio_s /* Keep pointer to audio configuration for later use. */

/*------------------------------------------------------------------
 *
 * Name:        recv_init
 *
 * Purpose:     Start up a thread for each audio device.
 *
 *
 * Inputs:      pa		- Address of structure of type audio_s.
 *
 *
 * Returns:     None.
 *
 * Errors:	Exit if error.
 *		No point in going on if we can't get audio.
 *
 *----------------------------------------------------------------*/

func recv_init(pa *audio_s) {
	save_pa = pa

	for a := range MAX_ADEVS {
		if pa.adev[a].defined > 0 {
			go recv_adev_thread(a)
		}
	}
} /* end recv_init */

func recv_adev_thread(a int) {
	/* This audio device can have one (mono) or two (stereo) channels. */
	/* Find number of the first channel and number of channels. */

	var first_chan = C.int(ADEVFIRSTCHAN(a))
	var num_chan = save_pa.adev[a].num_channels

	/*
	 * Get sound samples and decode them.
	 */
	var eof = false
	for !eof {
		for c := C.int(0); c < C.int(num_chan); c++ {
			var audio_sample = demod_get_sample(a)

			if audio_sample >= 256*256 {
				eof = true
			}

			// Future?  provide more flexible mapping.
			// i.e. for each valid channel where audio_source[] is first_chan+c.
			multi_modem_process_sample(first_chan+c, C.int(audio_sample))

			/* Originally, the DTMF decoder was always active. */
			/* It took very little CPU time and the thinking was that an */
			/* attached application might be interested in this even when */
			/* the APRStt gateway was not being used.  */

			/* Unfortunately it resulted in too many false detections of */
			/* touch tones when hearing other types of digital communications */
			/* on HF.  Starting in version 1.0, the DTMF decoder is active */
			/* only when the APRStt gateway is configured. */

			/* The test below allows us to listen to only a single channel for */
			/* for touch tone sequences.  The DTMF decoder and the accumulation */
			/* of digits into a sequence maintain separate data for each channel. */
			/* We should be able to accept touch tone sequences concurrently on */
			/* all channels.  The only issue is when a complete sequence is */
			/* sent to aprs_tt_sequence which doesn't have separate data for each */
			/* channel.  This shouldn't be a problem unless we have multiple */
			/* sequences arriving at the same instant. */

			if save_pa.achan[first_chan+c].dtmf_decode != DTMF_DECODE_OFF {
				var tt = dtmf_sample(int(first_chan+c), float64(audio_sample)/16384.)
				if tt != ' ' {
					aprs_tt_button(int(first_chan+c), tt)
				}
			}
		} // for c is just 0 or 0 then 1

		/* When a complete frame is accumulated, */
		/* dlq_rec_frame, is called. */

		/* recv_process, below, drains the queue. */
	} // while !eof on audio stream

	// What should we do now?
	// Seimply terminate the application?
	// Try to re-init the audio device a couple times before giving up?

	text_color_set(DW_COLOR_ERROR)
	dw_printf("Terminating after audio device %d input failure.\n", a)
	os.Exit(1)
}

func recv_process() {
	for {
		var timeout_value = ax25_link_get_next_timer_expiry()

		var timed_out = dlq_wait_while_empty(timeout_value)

		if timed_out {
			dl_timer_expiry()
		} else {
			var pitem = dlq_remove()

			if pitem != nil {
				switch pitem._type {
				case DLQ_REC_FRAME:
					/*
					 * This is the traditional processing.
					 * For all frames:
					 *	- Print in standard monitoring format.
					 *	- Send to KISS client applications.
					 *	- Send to AGw client applications in raw mode.
					 * For APRS frames:
					 *	- Explain what it means.
					 *	- Send to Igate.
					 *	- Digipeater.
					 */

					app_process_rec_packet(pitem._chan, pitem.subchan, pitem.slice, pitem.pp, pitem.alevel, pitem.fec_type, pitem.retries, pitem.spectrum)

					/*
					 * Link processing.
					 */
					lm_data_indication(pitem)
				case DLQ_CONNECT_REQUEST:
					dl_connect_request(pitem)
				case DLQ_DISCONNECT_REQUEST:
					dl_disconnect_request(pitem)
				case DLQ_XMIT_DATA_REQUEST:
					dl_data_request(pitem)
				case DLQ_REGISTER_CALLSIGN:
					dl_register_callsign(pitem)
				case DLQ_UNREGISTER_CALLSIGN:
					dl_unregister_callsign(pitem)
				case DLQ_OUTSTANDING_FRAMES_REQUEST:
					dl_outstanding_frames_request(pitem)
				case DLQ_CHANNEL_BUSY:
					lm_channel_busy(pitem)
				case DLQ_SEIZE_CONFIRM:
					lm_seize_confirm(pitem)
				case DLQ_CLIENT_CLEANUP:
					dl_client_cleanup(pitem)
				}

				dlq_delete(pitem)
			} else { //nolint:staticcheck
				// FIXME KG DEBUG
				// text_color_set(DW_COLOR_DEBUG);
				// dw_printf ("recv_process: spurious wakeup. (Temp debugging message - not a problem if only occasional.)\n");
			}
		}
	}
}
