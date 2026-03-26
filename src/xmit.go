package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Transmit queued up packets when channel is clear.
 *
 * Description:	Producers of packets to be transmitted call tq_append and then
 *		go merrily on their way, unconcerned about when the packet might
 *		actually get transmitted.
 *
 *		This thread waits until the channel is clear and then removes
 *		packets from the queue and transmits them.
 *
 *
 * Usage:	(1) The main application calls NewXmitService.
 *
 *			This will initialize the transmit packet queue
 *			and create a thread to empty the queue when
 *			the channel is clear.
 *
 *		(2) The application queues up packets by calling tq_append.
 *
 *			Packets that are being digipeated should go in the
 *			high priority queue so they will go out first.
 *
 *			Other packets should go into the lower priority queue.
 *
 *		(3) xmit_thread removes packets from the queue and transmits
 *			them when other signals are not being heard.
 *
 *---------------------------------------------------------------*/

import (
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/lestrrat-go/strftime"
)

const MORSE_DEFAULT_WPM = 10

/*
 * XmitService holds all transmit state.
 * Each channel can have different timing values.
 *
 * These are initialized once at application startup time
 * and some can be changed later by commands from connected applications.
 */

type XmitService struct {
	slottime [MAX_RADIO_CHANS]int /* Slot time in 10 mS units for persistence algorithm. */

	persist [MAX_RADIO_CHANS]int /* Sets probability for transmitting after each */
	/* slot time delay.  Transmit if a random number */
	/* in range of 0 - 255 <= persist value.  */
	/* Otherwise wait another slot time and try again. */

	txdelay [MAX_RADIO_CHANS]int /* After turning on the transmitter, */
	/* send "flags" for txdelay * 10 mS. */

	txtail [MAX_RADIO_CHANS]int /* Amount of time to keep transmitting after we */
	/* are done sending the data.  This is to avoid */
	/* dropping PTT too soon and chopping off the end */
	/* of the frame.  Again 10 mS units. */

	fulldup [MAX_RADIO_CHANS]bool /* Full duplex if true. */

	bits_per_sec [MAX_RADIO_CHANS]int /* Data transmission rate. */
	/* Often called baud rate which is equivalent for */
	/* 1200 & 9600 cases but could be different with other */
	/* modulation techniques. */

	debugXmitPacket bool /* print packet in hexadecimal form for debugging. */

	/*
	 * When an audio device is in stereo mode, we can have two
	 * different channels that want to transmit at the same time.
	 * We are not clever enough to multiplex them so use this
	 * so only one is active at the same time.
	 */
	audioOutDevMutex [MAX_ADEVS]sync.Mutex

	p_modem *audio_s
}

/*-------------------------------------------------------------------
 *
 * Name:        NewXmitService
 *
 * Purpose:     Initialize the transmit process.
 *
 * Inputs:	p_modem		- Structure with modem and timing parameters.
 *
 *
 * Outputs:	Returns a new XmitService with required information set up.
 *
 * Description:	Initialize the queue to be empty and set up other
 *		mechanisms for sharing it between different threads.
 *
 *		Start up xmit_thread(s) to actually send the packets
 *		at the appropriate time.
 *
 * Version 1.2:	We now allow multiple audio devices with one or two channels each.
 *		Each audio channel has its own thread.
 *
 *--------------------------------------------------------------------*/

func NewXmitService(p_modem *audio_s, debug_xmit_packet bool) *XmitService {
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("xmit_init ( ... )\n");
	#endif
	*/
	var xs = &XmitService{} //nolint:exhaustruct
	xs.p_modem = p_modem

	xs.debugXmitPacket = debug_xmit_packet

	/*
	 * Push to Talk (PTT) control.
	 */
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("xmit_init: about to call ptt_init \n");
	#endif
	*/
	ptt_init(p_modem)

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("xmit_init: back from ptt_init \n");
	#endif
	*/

	/*
	 * Save parameters for later use.
	 * TODO1.2:  Any reason to use global config rather than making a copy?
	 */

	for j := 0; j < MAX_RADIO_CHANS; j++ {
		xs.bits_per_sec[j] = p_modem.achan[j].baud
		xs.slottime[j] = p_modem.achan[j].slottime
		xs.persist[j] = p_modem.achan[j].persist
		xs.txdelay[j] = p_modem.achan[j].txdelay
		xs.txtail[j] = p_modem.achan[j].txtail
		xs.fulldup[j] = p_modem.achan[j].fulldup
	}

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("xmit_init: about to call tq_init \n");
	#endif
	*/
	tq_init(p_modem)

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("xmit_init: about to create threads \n");
	#endif
	*/

	//TODO:  xmit thread should be higher priority to avoid
	// underrun on the audio output device.

	for j := 0; j < MAX_RADIO_CHANS; j++ {
		if p_modem.chan_medium[j] == MEDIUM_RADIO {
			go xs.xmit_thread(j)
		}
	}

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("xmit_init: finished \n");
	#endif
	*/

	return xs
}

/*-------------------------------------------------------------------
 *
 * Name:        SetTxdelay
 *		SetPersist
 *		SetSlottime
 *		SetTxtail
 *		SetFulldup
 *
 *
 * Purpose:     The KISS protocol, and maybe others, can specify
 *		transmit timing parameters.  If the application
 *		specifies these, they will override what was read
 *		from the configuration file.
 *
 * Inputs:	channel	- should be 0 or 1.
 *
 *		value	- time values are in 10 mSec units.
 *
 *
 * Outputs:	Remember required information for future use.
 *
 * Question:	Should we have an option to enable or disable the
 *		application changing these values?
 *
 * Bugs:	No validity checking other than array subscript out of bounds.
 *
 *--------------------------------------------------------------------*/

func (xs *XmitService) SetTxdelay(channel, value int) {
	if channel >= 0 && channel < MAX_RADIO_CHANS {
		xs.txdelay[channel] = value
	}
}

func (xs *XmitService) SetPersist(channel, value int) {
	if channel >= 0 && channel < MAX_RADIO_CHANS {
		xs.persist[channel] = value
	}
}

func (xs *XmitService) SetSlottime(channel, value int) {
	if channel >= 0 && channel < MAX_RADIO_CHANS {
		xs.slottime[channel] = value
	}
}

func (xs *XmitService) SetTxtail(channel, value int) {
	if channel >= 0 && channel < MAX_RADIO_CHANS {
		xs.txtail[channel] = value
	}
}

func (xs *XmitService) SetFulldup(channel int, value bool) {
	if channel >= 0 && channel < MAX_RADIO_CHANS {
		xs.fulldup[channel] = value
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        frame_flavor
 *
 * Purpose:     Separate frames into different flavors so we can decide
 *		which can be bundled into a single transmission and which should
 *		be sent separately.
 *
 * Inputs:	pp	- Packet object.
 *
 * Returns:	Flavor, one of:
 *
 *		FLAVOR_SPEECH		- Destination address is SPEECH.
 *		FLAVOR_MORSE		- Destination address is MORSE.
 *		FLAVOR_DTMF		- Destination address is DTMF.
 *		FLAVOR_APRS_NEW		- APRS original, i.e. not digipeating.
 *		FLAVOR_APRS_DIGI	- APRS digipeating.
 *		FLAVOR_OTHER		- Anything left over, i.e. connected mode.
 *
 *--------------------------------------------------------------------*/

type flavor_t int

const (
	FLAVOR_APRS_NEW flavor_t = iota
	FLAVOR_APRS_DIGI
	FLAVOR_SPEECH
	FLAVOR_MORSE
	FLAVOR_DTMF
	FLAVOR_OTHER
)

func frame_flavor(pp *packet_t) flavor_t {
	if ax25_is_aprs(pp) { // UI frame, PID 0xF0.
		// It's unfortunate APRS did not use its own special PID.
		var dest = ax25_get_addr_no_ssid(pp, AX25_DESTINATION)

		if dest == "SPEECH" {
			return (FLAVOR_SPEECH)
		}

		if dest == "MORSE" {
			return (FLAVOR_MORSE)
		}

		if dest == "DTMF" {
			return (FLAVOR_DTMF)
		}

		/* Is there at least one digipeater AND has first one been used? */
		/* I could be the first in the list or later.  Doesn't matter. */

		if ax25_get_num_repeaters(pp) >= 1 && ax25_get_h(pp, AX25_REPEATER_1) > 0 {
			return (FLAVOR_APRS_DIGI)
		}

		return (FLAVOR_APRS_NEW)
	}

	return (FLAVOR_OTHER)
} /* end frame_flavor */

/*-------------------------------------------------------------------
 *
 * Name:        xmit_thread
 *
 * Purpose:     Process transmit queue for one channel.
 *
 * Inputs:	transmit packet queue.
 *
 * Outputs:
 *
 * Description:	We have different timing rules for different types of
 *		packets so they are put into different queues.
 *
 *		High Priority -
 *
 *			Packets which are being digipeated go out first.
 *			Latest recommendations are to retransmit these
 *			immdediately (after no one else is heard, of course)
 *			rather than waiting random times to avoid collisions.
 *			The KPC-3 configuration option for this is "UIDWAIT OFF".  (?)
 *
 *			AX.25 connected mode also has a couple cases
 *			where "expedited" frames are sent.
 *
 *		Low Priority -
 *
 *			Other packets are sent after a random wait time
 *			(determined by PERSIST & SLOTTIME) to help avoid
 *			collisions.
 *
 *		If more than one audio channel is being used, a separate
 *		pair of transmit queues is used for each channel.
 *
 *
 *
 * Version 1.2:	Allow more than one audio device.
 * 		each channel has its own thread.
 *		Add speech capability.
 *
 * Version 1.4:	Rearranged logic for bundling multiple frames into a single transmission.
 *
 *		The rule is that Speech, Morse Code, DTMF, and APRS digipeated frames
 *		are all sent separately.  The rest can be bundled.
 *
 *--------------------------------------------------------------------*/

func (xs *XmitService) xmit_thread(channel int) {
	for {
		tq_wait_while_empty(channel)
		/* TODO KG
		#if DEBUG
			  text_color_set(DW_COLOR_DEBUG);
			  dw_printf ("xmit_thread, channel %d: woke up\n", chan);
		#endif
		*/

		// Does this extra loop offer any benefit?
		for tq_peek(channel, TQ_PRIO_0_HI) != nil || tq_peek(channel, TQ_PRIO_1_LO) != nil {
			/*
			 * Wait for the channel to be clear.
			 * If there is something in the high priority queue, begin transmitting immediately.
			 * Otherwise, wait a random amount of time, in hopes of minimizing collisions.
			 */
			var ok = xs.wait_for_clear_channel(channel, xs.slottime[channel], xs.persist[channel], xs.fulldup[channel])

			var prio = TQ_PRIO_1_LO

			var pp = tq_remove(channel, TQ_PRIO_0_HI)
			if pp != nil {
				prio = TQ_PRIO_0_HI
			} else {
				pp = tq_remove(channel, TQ_PRIO_1_LO)
			}

			/* TODO KG
			#if DEBUG
				    text_color_set(DW_COLOR_DEBUG);
				    dw_printf ("xmit_thread: tq_remove(channel=%d, prio=%d) returned %p\n", channel, prio, pp);
			#endif
			*/
			// Shouldn't have nil here but be careful.

			if pp != nil {
				if ok {
					/*
					 * Channel is clear and we have lock on output device.
					 *
					 * If destination is "SPEECH" send info part to speech synthesizer.
					 * If destination is "MORSE" send as morse code.
					 * If destination is "DTMF" send as Touch Tones.
					 */
					switch frame_flavor(pp) {
					case FLAVOR_SPEECH:
						xs.xmit_speech(channel, pp)

					case FLAVOR_MORSE:
						var ssid = ax25_get_ssid(pp, AX25_DESTINATION)

						var wpm = MORSE_DEFAULT_WPM
						if ssid > 0 {
							wpm = ssid * 2
						}

						// This is a bit of a hack so we don't respond too quickly for APRStt.
						// It will be sent in high priority queue while a beacon wouldn't.
						// Add a little delay so user has time release PTT after sending #.
						// This and default txdelay would give us a second.

						if prio == TQ_PRIO_0_HI {
							//text_color_set(DW_COLOR_DEBUG);
							//dw_printf ("APRStt morse xmit delay hack...\n");
							SLEEP_MS(700)
						}

						xs.xmit_morse(channel, pp, wpm)

					case FLAVOR_DTMF:
						var speed = ax25_get_ssid(pp, AX25_DESTINATION)
						if speed == 0 {
							speed = 5 // default half of maximum
						}

						if speed > 10 {
							speed = 10
						}

						xs.xmit_dtmf(channel, pp, speed)

					case FLAVOR_APRS_DIGI:
						xs.xmit_ax25_frames(channel, prio, pp, 1) /* 1 means don't bundle */
						// I don't know if this in some official specification
						// somewhere, but it is generally agreed that APRS digipeaters
						// should send only one frame at a time rather than
						// bundling multiple frames into a single transmission.
						// Discussion here:  http://lists.tapr.org/pipermail/aprssig_lists.tapr.org/2021-September/049034.html

					default:
						xs.xmit_ax25_frames(channel, prio, pp, 256)
					}

					// Corresponding lock is in wait_for_clear_channel.

					xs.audioOutDevMutex[ACHAN2ADEV(channel)].Unlock()
				} else {
					/*
					 * Timeout waiting for clear channel.
					 * Discard the packet.
					 * Display with ERROR color rather than XMIT color.
					 */
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Waited too long for clear channel.  Discarding packet below.\n")

					var stemp = ax25_format_addrs(pp)

					var pinfo = ax25_get_info(pp)

					text_color_set(DW_COLOR_INFO)
					dw_printf("[%d%c] ", channel, priorityToRune(prio))

					dw_printf("%s", stemp) /* stations followed by : */
					ax25_safe_print(pinfo, !ax25_is_aprs(pp))
					dw_printf("\n")
					ackmode_discard(pp)
					ax25_delete(pp)
				} /* wait for clear channel error. */
			} /* Have pp */
		} /* while queue not empty */
	} /* while 1 */
} /* end xmit_thread */

func priorityToRune(prio int) rune {
	if prio == TQ_PRIO_0_HI {
		return 'H'
	} else {
		return 'L'
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        xmit_ax25_frames
 *
 * Purpose:     After we have a clear channel, and possibly waited a random time,
 *		we transmit one or more frames.
 *
 * Inputs:	chan	- Channel number.
 *
 *		prio	- Priority of the first frame.
 *			  Subsequent frames could be different.
 *
 *		pp	- Packet object pointer.
 *			  It will be deleted so caller should not try
 *			  to reference it after this.
 *
 *		max_bundle - Max number of frames to bundle into one transmission.
 *
 * Description:	Turn on transmitter.
 *		Send flags for TXDELAY time.
 *		Send the first packet, given by pp.
 *		Possibly send more packets from either queue.
 *		Send flags for TXTAIL time.
 *		Turn off transmitter.
 *
 *
 * How many frames in one transmission?  (for APRS)
 *
 *		Should we send multiple frames in one transmission if we
 *		have more than one sitting in the queue?  At first I was thinking
 *		this would help reduce channel congestion.  I don't recall seeing
 *		anything in the APRS specifications allowing or disallowing multiple
 *		frames in one transmission.  I can think of some scenarios
 *		where it might help.  I can think of some where it would
 *		definitely be counter productive.
 *
 * What to others have to say about this topic?
 *
 *	"For what it is worth, the original APRSdos used a several second random
 *	generator each time any kind of packet was generated... This is to avoid
 *	bundling. Because bundling, though good for connected packet, is not good
 *	on APRS. Sometimes the digi begins digipeating the first packet in the
 *	bundle and steps all over the remainder of them. So best to make sure each
 *	packet is isolated in time from others..."
 *
 *		Bob, WB4APR
 *
 *
 * Version 0.9:	Earlier versions always sent one frame per transmission.
 *		This was fine for APRS but more and more people are now
 *		using this as a KISS TNC for connected protocols.
 *		Rather than having a configuration file item,
 *		we try setting the maximum number automatically.
 *		1 for digipeated frames, 7 for others.
 *
 * Version 1.4: Lift the limit.  We could theoretically have a window size up to 127.
 *		If another section pumps out that many quickly we shouldn't
 *		break it up here.  Empty out both queues with some exceptions.
 *
 *		Digipeated APRS, Speech, and Morse code should have
 *		their own separate transmissions.
 *		Everything else can be bundled together.
 *		Different priorities can share a single transmission.
 *		Once we have control of the channel, we might as well keep going.
 *		[High] Priority frames will always go to head of the line,
 *
 * Version 1.5:	Add full duplex option.
 *
 *--------------------------------------------------------------------*/

func (xs *XmitService) xmit_ax25_frames(channel int, prio int, pp *packet_t, max_bundle int) {
	/*
	 * These are for timing of a transmission.
	 * All are in usual unix time (seconds since 1/1/1970) but higher resolution
	 */
	var time_ptt = time.Now()

	/*
	 * Turn on transmitter.
	 * Start sending leading flag bytes.
	 */

	// TODO: This was written assuming bits/sec = baud.
	// Does it is need to be scaled differently for PSK?

	/* TODO KG
	   #if DEBUG
	   	text_color_set(DW_COLOR_DEBUG);
	   	dw_printf ("xmit_thread: t=%.3f, Turn on PTT now for channel %d. speed = %d\n", dtime_now()-time_ptt, chan, xs.bits_per_sec[chan]);
	   #endif
	*/
	ptt_set(OCTYPE_PTT, channel, 1)

	// Inform data link state machine that we are now transmitting.

	dlq_seize_confirm(channel) // C4.2.  "This primitive indicates, to the Data-link State
	// machine, that the transmission opportunity has arrived."

	var pre_flags = xs.msToBits(xs.txdelay[channel]*10, channel) / 8

	/* Total number of bits in transmission including all flags and bit stuffing. */
	var num_bits = layer2_preamble_postamble(channel, pre_flags, false, xs.p_modem)

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("xmit_thread: t=%.3f, txdelay=%d [*10], pre_flags=%d, num_bits=%d\n", dtime_now()-time_ptt, xs.txdelay[channel], pre_flags, num_bits);
		double presleep = dtime_now();
	#endif
	*/

	SLEEP_MS(10) // Give data link state machine a chance to
	// to stuff more frames into the transmit queue,
	// in response to dlq_seize_confirm, so
	// we don't run off the end too soon.

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		// How long did sleep last?
		dw_printf ("xmit_thread: t=%.3f, Should be 0.010 second after the above.\n", dtime_now()-time_ptt);
		double naptime = dtime_now() - presleep;
		if (naptime > 0.015) {
		  text_color_set(DW_COLOR_ERROR);
		  dw_printf ("Sleep for 10 ms actually took %.3f second!\n", naptime);
		}
	#endif
	*/

	var numframe = 0 /* Number of frames sent during this transmission. */

	/*
	 * Transmit the frame.
	 */

	var nb = xs.send_one_frame(channel, prio, pp)

	num_bits += nb
	if nb > 0 {
		numframe++
	}
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("xmit_thread: t=%.3f, nb=%d, num_bits=%d, numframe=%d\n", dtime_now()-time_ptt, nb, num_bits, numframe);
	#endif
	*/
	ackmode_notify_sent(pp)
	ax25_delete(pp)

	/*
	 * See if we can bundle additional frames into this transmission.
	 */

	var done = false
	for numframe < max_bundle && !done {
		/*
		 * Peek at what is available.
		 * Don't remove from queue yet because it might not be eligible.
		 */
		prio = TQ_PRIO_1_LO

		pp = tq_peek(channel, TQ_PRIO_0_HI)
		if pp != nil {
			prio = TQ_PRIO_0_HI
		} else {
			pp = tq_peek(channel, TQ_PRIO_1_LO)
		}

		if pp != nil {
			switch frame_flavor(pp) {
			default:
				done = true // not eligible for bundling.

			case FLAVOR_APRS_NEW, FLAVOR_OTHER:
				pp = tq_remove(channel, prio)
				/* TODO KG
				#if DEBUG
					        text_color_set(DW_COLOR_DEBUG);
					        dw_printf ("xmit_thread: t=%.3f, tq_remove(channel=%d, prio=%d) returned %p\n", dtime_now()-time_ptt, channel, prio, pp);
				#endif
				*/

				nb = xs.send_one_frame(channel, prio, pp)

				num_bits += nb
				if nb > 0 {
					numframe++
				}
				/* TODO KG
				#if DEBUG
					        text_color_set(DW_COLOR_DEBUG);
					        dw_printf ("xmit_thread: t=%.3f, nb=%d, num_bits=%d, numframe=%d\n", dtime_now()-time_ptt, nb, num_bits, numframe);
				#endif
				*/
				ackmode_notify_sent(pp)
				ax25_delete(pp)
			}
		} else {
			done = true
		}
	}

	/*
	 * Need TXTAIL because we don't know exactly when the sound is done.
	 */

	var post_flags = xs.msToBits(xs.txtail[channel]*10, channel) / 8
	nb = layer2_preamble_postamble(channel, post_flags, true, xs.p_modem)
	num_bits += nb
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("xmit_thread: t=%.3f, txtail=%d [*10], post_flags=%d, nb=%d, num_bits=%d\n", dtime_now()-time_ptt, xs.txtail[channel], post_flags, nb, num_bits);
	#endif
	*/

	/*
	 * While demodulating is CPU intensive, generating the tones is not.
	 * Example: on the RPi model 1, with 50% of the CPU taken with two receive
	 * channels, a transmission of more than a second is generated in
	 * about 40 mS of elapsed real time.
	 */

	audio_wait(ACHAN2ADEV(channel))

	/*
	 * Ideally we should be here just about the time when the audio is ending.
	 * However, the innards of "audio_wait" are not satisfactory in all cases.
	 *
	 * Calculate how long the frame(s) should take in milliseconds.
	 */

	var durationMS = xs.bitsToMS(num_bits, channel)

	/*
	 * See how long it has been since PTT was turned on.
	 * Wait additional time if necessary.
	 */

	var already = time.Since(time_ptt)
	var wait_more = time.Duration(durationMS)*time.Millisecond - already

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("xmit_thread: t=%.3f, xmit duration=%d, %d already elapsed since PTT, wait %d more\n", dtime_now()-time_ptt, duration, already, wait_more );
	#endif
	*/

	if wait_more > 0 {
		SLEEP_MS(int(wait_more.Milliseconds()))
	} else if wait_more < -100*time.Millisecond {
		/* If we run over by 10 mSec or so, it's nothing to worry about. */
		/* However, if PTT is still on about 1/10 sec after audio */
		/* should be done, something is wrong. */

		/* Looks like a bug with the RPi audio system. Never an issue with Ubuntu.  */
		/* This runs over randomly sometimes. TODO:  investigate more fully sometime. */
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Transmit timing error: PTT is on %d mSec too long.\n", -wait_more.Milliseconds())
	}

	/*
	 * Turn off transmitter.
	 */
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		time_now = dtime_now();
		dw_printf ("xmit_thread: t=%.3f, Turn off PTT now. Actual time on was %d mS, vs. %d desired\n", dtime_now()-time_ptt, (int) ((time_now - time_ptt) * 1000.), duration);
	#endif
	*/

	ptt_set(OCTYPE_PTT, channel, 0)
} /* end xmit_ax25_frames */

/*-------------------------------------------------------------------
 *
 * Name:        send_one_frame
 *
 * Purpose:     Send one AX.25 frame.
 *
 * Inputs:	c	- Channel number.
 *
 *		p	- Priority.
 *
 *		pp	- Packet object pointer.  Caller will delete it.
 *
 * Returns:	Number of bits transmitted.
 *
 * Description:	Caller is responsible for activiating PTT, TXDELAY,
 *		deciding how many frames can be in one transmission,
 *		deactivating PTT.
 *
 *--------------------------------------------------------------------*/

func (xs *XmitService) send_one_frame(c int, p int, pp *packet_t) int {
	if ax25_is_null_frame(pp) {
		// Issue 132 - We could end up in a situation where:
		// Transmitter is already on.
		// Application wants to send a frame.
		// dl_seize_request turns into this null frame.
		// It was being ignored here so the data got stuck in the queue.
		// I think the solution is to send back a seize confirm here.
		// It shouldn't hurt if we send it redundantly.
		// Added for 1.5 beta test 4.
		dlq_seize_confirm(c) // C4.2.  "This primitive indicates, to the Data-link State
		// machine, that the transmission opportunity has arrived."

		SLEEP_MS(10) // Give data link state machine a chance to
		// to stuff more frames into the transmit queue,
		// in response to dlq_seize_confirm, so
		// we don't run off the end too soon.

		return (0)
	}

	var ts = xs.timestampPrefix()

	var stemp = ax25_format_addrs(pp)

	var pinfo = ax25_get_info(pp)

	text_color_set(DW_COLOR_XMIT)
	/*
		#if 0						// FIXME - enable this?
			dw_printf ("[%d%c%s%s] ", c,
					p==TQ_PRIO_0_HI ? 'H' : 'L',
					xs.p_modem.achan[c].fx25_strength ? "F" : "",
					ts);
		#else
	*/
	dw_printf("[%d%c%s] ", c, priorityToRune(p), ts)
	/* #endif */
	dw_printf("%s", stemp) /* stations followed by : */

	/* Demystify non-APRS.  Use same format for received frames in direwolf.c. */

	if !ax25_is_aprs(pp) {
		var _, desc, _, _, _, ftype = ax25_frame_type(pp)

		dw_printf("(%s)", desc)

		if ftype == frame_type_U_XID {
			var _, info2text, _ = xid_parse(pinfo)
			dw_printf(" %s\n", info2text)
		} else {
			ax25_safe_print(pinfo, !ax25_is_aprs(pp))
			dw_printf("\n")
		}
	} else {
		ax25_safe_print(pinfo, !ax25_is_aprs(pp))
		dw_printf("\n")
	}

	ax25_check_addresses(pp)

	/* Optional hex dump of packet. */

	if xs.debugXmitPacket {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("------\n")
		ax25_hex_dump(pp)
		dw_printf("------\n")
	}

	/*
	 * Transmit the frame.
	 */
	var send_invalid_fcs2 = false

	if xs.p_modem.xmit_error_rate != 0 {
		// https://cs.opensource.google/go/go/+/refs/tags/go1.22.0:src/math/rand/rand.go;l=189
		// rand.Float64 excludes 1.0 so let's just use the internal implementation
		var r = float64(rand.Int63n(1<<53)) / (1 << 53) // Random, 0.0 to 1.0

		if float64(xs.p_modem.xmit_error_rate)/100.0 > r {
			send_invalid_fcs2 = true

			text_color_set(DW_COLOR_INFO)
			dw_printf("Intentionally sending invalid CRC for frame above.  Xmit Error rate = %d per cent.\n", xs.p_modem.xmit_error_rate)
		}
	}

	var nb = layer2_send_frame(c, pp, send_invalid_fcs2, xs.p_modem)

	// Optionally send confirmation to AGW client app if monitoring enabled.

	server_send_monitored(c, pp, 1)

	return nb
} /* end send_one_frame */

/*-------------------------------------------------------------------
 *
 * Name:        xmit_speech
 *
 * Purpose:     After we have a clear channel, and possibly waited a random time,
 *		we transmit information part of frame as speech.
 *
 * Inputs:	c	- Channel number.
 *
 *		pp	- Packet object pointer.
 *			  It will be deleted so caller should not try
 *			  to reference it after this.
 *
 * Description:	Turn on transmitter.
 *		Invoke the text-to-speech script.
 *		Turn off transmitter.
 *
 *--------------------------------------------------------------------*/

func (xs *XmitService) xmit_speech(c int, pp *packet_t) {
	/*
	 * Print spoken packet.  Prefix by channel.
	 */
	var ts = xs.timestampPrefix()

	var pinfo = ax25_get_info(pp)

	text_color_set(DW_COLOR_XMIT)
	dw_printf("[%d.speech%s] \"%s\"\n", c, ts, string(pinfo))

	if xs.p_modem.tts_script == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Text-to-speech script has not been configured.\n")
		ax25_delete(pp)

		return
	}

	/*
	 * Turn on transmitter.
	 */
	ptt_set(OCTYPE_PTT, c, 1)

	/*
	 * Invoke the speech-to-text script.
	 */

	xmit_speak_it(xs.p_modem.tts_script, c, string(pinfo))

	/*
	 * Turn off transmitter.
	 */

	ptt_set(OCTYPE_PTT, c, 0)
	ax25_delete(pp)
} /* end xmit_speech */

/* Broken out into separate function so configuration can validate it. */

func xmit_speak_it(script string, c int, msg string) error {
	var cmd = exec.Command(script, strconv.Itoa(c), msg) //nolint:gosec // Trust the user-supplied config

	var err = cmd.Run()
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Failed to run text-to-speech script, %s\n", script)

		var cwd, _ = os.Getwd()
		dw_printf("CWD = %s\n", cwd)

		dw_printf("PATH = %s\n", os.Getenv("PATH"))
	}

	return (err)
}

func (xs *XmitService) timestampPrefix() string {
	if xs.p_modem.timestamp_format != "" {
		var formattedTime, _ = strftime.Format(xs.p_modem.timestamp_format, time.Now())
		return " " + formattedTime // space after channel.
	}

	return ""
}

/*-------------------------------------------------------------------
 *
 * Name:        xmit_morse
 *
 * Purpose:     After we have a clear channel, and possibly waited a random time,
 *		we transmit information part of frame as Morse code.
 *
 * Inputs:	c	- Channel number.
 *
 *		pp	- Packet object pointer.
 *			  It will be deleted so caller should not try
 *			  to reference it after this.
 *
 *		wpm	- Speed in words per minute.
 *
 * Description:	Turn on transmitter.
 *		Send text as Morse code.
 *		A small amount of quiet padding will appear at start and end.
 *		Turn off transmitter.
 *
 *--------------------------------------------------------------------*/

func (xs *XmitService) xmit_morse(c int, pp *packet_t, wpm int) {
	var ts = xs.timestampPrefix()

	var pinfo = ax25_get_info(pp)

	text_color_set(DW_COLOR_XMIT)
	dw_printf("[%d.morse%s] \"%s\"\n", c, ts, string(pinfo))

	ptt_set(OCTYPE_PTT, c, 1)
	var start_ptt = time.Now()

	// make txdelay at least 300 and txtail at least 250 ms.

	var _length_ms = morse_send(c, string(pinfo), wpm, max(xs.txdelay[c]*10, 300), max(xs.txtail[c]*10, 250))
	var waitDuration = time.Duration(_length_ms) * time.Millisecond

	// there is probably still sound queued up in the output buffers.

	var wait_until = start_ptt.Add(waitDuration)

	var timeToWait = time.Until(wait_until)
	if timeToWait.Milliseconds() > 0 {
		SLEEP_MS(int(timeToWait.Milliseconds()))
	}

	ptt_set(OCTYPE_PTT, c, 0)
	ax25_delete(pp)
} /* end xmit_morse */

/*-------------------------------------------------------------------
 *
 * Name:        xmit_dtmf
 *
 * Purpose:     After we have a clear channel, and possibly waited a random time,
 *		we transmit information part of frame as DTMF tones.
 *
 * Inputs:	c	- Channel number.
 *
 *		pp	- Packet object pointer.
 *			  It will be deleted so caller should not try
 *			  to reference it after this.
 *
 *		speed	- Button presses per second.
 *
 * Description:	Turn on transmitter.
 *		Send text as touch tones.
 *		A small amount of quiet padding will appear at start and end.
 *		Turn off transmitter.
 *
 *--------------------------------------------------------------------*/

func (xs *XmitService) xmit_dtmf(c int, pp *packet_t, speed int) {
	var ts = xs.timestampPrefix()

	var pinfo = ax25_get_info(pp)

	text_color_set(DW_COLOR_XMIT)
	dw_printf("[%d.dtmf%s] \"%s\"\n", c, ts, string(pinfo))

	ptt_set(OCTYPE_PTT, c, 1)
	var start_ptt = time.Now()

	// make txdelay at least 300 and txtail at least 250 ms.

	var _length_ms = dtmf_send(c, string(pinfo), speed, max(xs.txdelay[c]*10, 300), max(xs.txtail[c]*10, 250))
	var waitDuration = time.Duration(_length_ms) * time.Millisecond

	// there is probably still sound queued up in the output buffers.

	var wait_until = start_ptt.Add(waitDuration)

	var timeToWait = time.Until(wait_until)
	if timeToWait.Milliseconds() > 0 {
		SLEEP_MS(int(timeToWait.Milliseconds()))
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Oops.  CPU too slow to keep up with DTMF generation.\n")
	}

	ptt_set(OCTYPE_PTT, c, 0)
	ax25_delete(pp)
} /* end xmit_dtmf */

/*-------------------------------------------------------------------
 *
 * Name:        wait_for_clear_channel
 *
 * Purpose:     Wait for the radio channel to be clear and any
 *		additional time for collision avoidance.
 *
 * Inputs:	chan	-	Radio channel number.
 *
 *		slottime - 	Amount of time to wait for each iteration
 *				of the waiting algorithm.  10 mSec units.
 *
 *		persist -	Probability of transmitting.
 *
 *		fulldup -	Full duplex.  Just start sending immediately.
 *
 * Returns:	True for OK.  False for timeout.
 *
 * Description:	New in version 1.2: also obtain a lock on audio out device.
 *
 *		New in version 1.5: full duplex.
 *		Just start transmitting rather than waiting for clear channel.
 *		This would only be appropriate when transmit and receive are
 *		using different radio frequencies.  e.g.  VHF up, UHF down satellite.
 *
 * Transmit delay algorithm:
 *
 *		Wait for channel to be clear.
 *		If anything in high priority queue, bail out of the following.
 *
 *		Wait slottime * 10 milliseconds.
 *		Generate an 8 bit random number in range of 0 - 255.
 *		If random number <= persist value, return.
 *		Otherwise repeat.
 *
 * Example:
 *
 *		For typical values of slottime=10 and persist=63,
 *
 *		Delay		Probability
 *		-----		-----------
 *		100		.25					= 25%
 *		200		.75 * .25				= 19%
 *		300		.75 * .75 * .25				= 14%
 *		400		.75 * .75 * .75 * .25			= 11%
 *		500		.75 * .75 * .75 * .75 * .25		= 8%
 *		600		.75 * .75 * .75 * .75 * .75 * .25	= 6%
 *		700		.75 * .75 * .75 * .75 * .75 * .75 * .25	= 4%
 *		etc.		...
 *
 *--------------------------------------------------------------------*/

/* Give up if we can't get a clear channel in a minute. */
/* That's a long time to wait for APRS. */
/* Might need to revisit some day for connected mode file transfers. */

const WAIT_TIMEOUT_MS = 60 * 1000
const WAIT_CHECK_EVERY_MS = 10

func (xs *XmitService) wait_for_clear_channel(channel int, slottime int, persist int, fulldup bool) bool {
	/*
	 * For full duplex we skip the channel busy check and random wait.
	 * We still need to wait if operating in stereo and the other audio
	 * half is busy.
	 */
	var n = 0

	if !fulldup {
	start_over_again:

		for hdlc_rec_data_detect_any(channel) > 0 {
			SLEEP_MS(WAIT_CHECK_EVERY_MS)

			n++
			if n > (WAIT_TIMEOUT_MS / WAIT_CHECK_EVERY_MS) {
				return false
			}
		}

		//TODO:  rethink dwait.

		/*
		 * Added in version 1.2 - for transceivers that can't
		 * turn around fast enough when using squelch and VOX.
		 */

		if xs.p_modem.achan[channel].dwait > 0 {
			SLEEP_MS(xs.p_modem.achan[channel].dwait * 10)
		}

		if hdlc_rec_data_detect_any(channel) > 0 {
			goto start_over_again
		}

		/*
		 * Wait random time.
		 * Proceed to transmit sooner if anything shows up in high priority queue.
		 */
		for tq_peek(channel, TQ_PRIO_0_HI) == nil {
			SLEEP_MS(slottime * 10)

			if hdlc_rec_data_detect_any(channel) > 0 {
				goto start_over_again
			}

			var r = rand.Int() & 0xff
			if r <= persist {
				break
			}
		}
	}

	/*
	 * This is to prevent two channels from transmitting at the same time
	 * thru a stereo audio device.
	 * We are not clever enough to combine two audio streams.
	 * They must go out one at a time.
	 * Documentation recommends using separate audio device for each channel rather than stereo.
	 * That also allows better use of multiple cores for receiving.
	 */

	// TODO: review this.

	for !xs.audioOutDevMutex[ACHAN2ADEV(channel)].TryLock() {
		SLEEP_MS(WAIT_CHECK_EVERY_MS)

		n++
		if n > (WAIT_TIMEOUT_MS / WAIT_CHECK_EVERY_MS) {
			return false
		}
	}

	return true
} /* end wait_for_clear_channel */

func (xs *XmitService) bitsToMS(b, ch int) int {
	return b * 1000 / xs.bits_per_sec[ch]
}

func (xs *XmitService) msToBits(ms, ch int) int {
	return ms * xs.bits_per_sec[ch] / 1000
}

/* end xmit.c */
