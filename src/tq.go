//nolint:gochecknoglobals
package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Transmit queue - hold packets for transmission until the channel is clear.
 *
 * Description:	Producers of packets to be transmitted call tq_append and then
 *		go merrily on their way, unconcerned about when the packet might
 *		actually get transmitted.
 *
 *		Another thread waits until the channel is clear and then removes
 *		packets from the queue and transmits them.
 *
 * Revisions:	1.2 - Enhance for multiple audio devices.
 *
 *---------------------------------------------------------------*/

import (
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/metrics"
	"github.com/lestrrat-go/strftime"
	"github.com/sirupsen/logrus"
)

const TQ_NUM_PRIO = 2 /* Number of priorities. */

const TQ_PRIO_0_HI = 0
const TQ_PRIO_1_LO = 1

var queue_head [MAX_RADIO_CHANS][TQ_NUM_PRIO]*packet_t /* Head of linked list for each queue. */

// Number of packets in each queue, maintained alongside queue_head and guarded
// by the same mutex.  tq_remove pops the head in constant time, so counting the
// list to publish the depth would make draining a long queue quadratic, under
// the one mutex every queue operation contends for.
//
// This counts what tq_count counts - real packets - so the null frame
// lm_seize_request queues to wake the transmitter is excluded.  Counting it
// would report a packet waiting when there is nothing to send, turning
// ordinary connected-mode acknowledgement into an apparent backlog.
var queue_len [MAX_RADIO_CHANS][TQ_NUM_PRIO]int

// tq_is_real_packet reports whether a queue entry is a real packet rather than
// lm_seize_request's null wake-up frame, matching tq_count_locked's own test.
func tq_is_real_packet(pp *packet_t) bool {
	return ax25_get_num_addr(pp) >= AX25_MIN_ADDRS
}

var tq_mutex sync.Mutex /* Critical section for updating queues. */
/* Just one for all queues. */

var wake_up_cond [MAX_RADIO_CHANS]*sync.Cond /* Notify transmit thread when queue not empty. */

var wake_up_mutex [MAX_RADIO_CHANS]sync.Mutex /* Required by cond_wait. */

var xmit_thread_is_waiting [MAX_RADIO_CHANS]bool

/*-------------------------------------------------------------------
 *
 * Name:        tq_init
 *
 * Purpose:     Initialize the transmit queue.
 *
 * Inputs:	audio_config_p	- Audio device configuration.
 *
 * Outputs:
 *
 * Description:	Initialize the queue to be empty and set up other
 *		mechanisms for sharing it between different threads.
 *
 *		We have different timing rules for different types of
 *		packets so they are put into different queues.
 *
 *		High Priority -
 *
 *			Packets which are being digipeated go out first.
 *			Latest recommendations are to retransmit these
 *			immdediately (after no one else is heard, of course)
 *			rather than waiting random times to avoid collisions.
 *			The KPC-3 configuration option for this is "UIDWAIT OFF".
 *
 *		Low Priority -
 *
 *			Other packets are sent after a random wait time
 *			(determined by PERSIST & SLOTTIME) to help avoid
 *			collisions.
 *
 *		Each audio channel has its own queue.
 *
 *--------------------------------------------------------------------*/

// TODO KG static struct audio_s *save_audio_config_p;

func tq_init(audio_config_p *audio_s) {
	logrus.Debug("tq_init")
	save_audio_config_p = audio_config_p

	for c := range MAX_RADIO_CHANS {
		for p := range TQ_NUM_PRIO {
			queue_head[c][p] = nil
			queue_len[c][p] = 0

			metrics.SetTxQueueDepth(c, p, 0)
		}
	}

	/*
	 * Windows and Linux have different wake up methods.
	 * Put a wrapper around this someday to hide the details.
	 */

	for c := range MAX_RADIO_CHANS {
		xmit_thread_is_waiting[c] = false

		if audio_config_p.chan_medium[c] == MEDIUM_RADIO {
			wake_up_cond[c] = sync.NewCond(&wake_up_mutex[c])
		}
	}
} /* end tq_init */

/*-------------------------------------------------------------------
 *
 * Name:        tq_append
 *
 * Purpose:     Add an APRS packet to the end of the specified transmit queue.
 *
 * 		Connected mode is a little different.  Use lm_data_request instead.
 *
 * Inputs:	channel	- Channel, 0 is first.
 *
 *				New in 1.7:
 *				Channel can be assigned to IGate rather than a radio.
 *
 *				New in 1.8:
 *				Channel can be assigned to a network TNC.
 *
 *		prio	- Priority, use TQ_PRIO_0_HI for digipeated or
 *				TQ_PRIO_1_LO for normal.
 *
 *		pp	- Address of packet object.
 *				Ownership is handed over to this function, so
 *				the caller should NOT make any references to
 *				it after this point.
 *
 * Outputs:
 *
 * Description:	Add packet to end of linked list.
 *		Signal the transmit thread if the queue was formerly empty.
 *
 *		Note that we have a transmit thread each audio channel.
 *		Two channels can share one audio output device.
 *
 * IMPORTANT!	Don't make an further references to the packet object after
 *		giving it to tq_append.
 *
 *--------------------------------------------------------------------*/

func tq_append(channel int, prio int, pp *packet_t) {
	Assert(prio >= 0 && prio < TQ_NUM_PRIO)

	if pp == nil {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("INTERNAL ERROR:  tq_append nil packet pointer. Please report this!\n")

		return
	}

	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		logrus.WithFields(logrus.Fields{
			"channel": channel,
			"prio":    prio,
			"info":    string(AX25GetInfo(pp)),
		}).Debug("tq_append")
	}

	/* TODO KG
	#if AX25MEMDEBUG

		if (ax25memdebug_get()) {
		  text_color_set(DW_COLOR_DEBUG);
		  dw_printf ("tq_append (channel=%d, prio=%d, seq=%d)\n", channel, prio, ax25memdebug_seq(pp));
		}
	#endif
	*/

	// New in 1.7 - A channel can be assigned to the IGate rather than a radio.
	// New in 1.8: Assign a channel to external network TNC.
	// Send somewhere else, rather than the transmit queue.

	if save_audio_config_p.chan_medium[channel] == MEDIUM_IGATE ||
		save_audio_config_p.chan_medium[channel] == MEDIUM_NETTNC {
		var ts string // optional time stamp.

		if save_audio_config_p.timestamp_format != "" {
			var formattedTime, _ = strftime.Format(save_audio_config_p.timestamp_format, time.Now())
			ts = " " + formattedTime // space after channel.
		}

		// Formated addresses.
		var stemp = AX25FormatAddrs(pp)
		var pinfo = AX25GetInfo(pp)

		text_color_set(DW_COLOR_XMIT)

		if save_audio_config_p.chan_medium[channel] == MEDIUM_IGATE {
			dw_printf("[%d>is%s] ", channel, ts)
			dw_printf("%s", stemp) /* stations followed by : */
			AX25SafePrint(pinfo, !ax25_is_aprs(pp))
			dw_printf("\n")

			igate_send_rec_packet(channel, pp)
		} else { // network TNC
			dw_printf("[%d>nt%s] ", channel, ts)
			dw_printf("%s", stemp) /* stations followed by : */
			AX25SafePrint(pinfo, !ax25_is_aprs(pp))
			dw_printf("\n")

			nettnc_send_packet(channel, pp)
		}

		return
	}

	// Normal case - put in queue for radio transmission.
	// Error if trying to transmit to a radio channel which was not configured.

	if channel < 0 || channel >= MAX_RADIO_CHANS || save_audio_config_p.chan_medium[channel] == MEDIUM_NONE {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR - Request to transmit on invalid radio channel %d.\n", channel)
		dw_printf("This is probably a client application error, not a problem with direwolf.\n")
		dw_printf("Are you using AX.25 for Linux?  It might be trying to use a modified\n")
		dw_printf("version of KISS which uses the port field differently than the\n")
		dw_printf("original KISS protocol specification.  The solution might be to use\n")
		dw_printf("a command like \"kissparms -c 1 -p radio\" to set CRC none mode.\n")
		dw_printf("\n")

		return
	}

	/*
	 * Is transmit queue out of control?
	 *
	 * There is no technical reason to limit the transmit packet queue length, it just seemed like a good
	 * warning that something wasn't right.
	 * When this was written, I was mostly concerned about APRS where packets would only be sent
	 * occasionally and they can be discarded if they can't be sent out in a reasonable amount of time.
	 *
	 * If a large file is being sent, with TCP/IP, it is perfectly reasonable to have a large number
	 * of packets waiting for transmission.
	 *
	 * Ideally, the application should be able to throttle the transmissions so the queue doesn't get too long.
	 * If using the KISS interface, there is no way to get this information from the TNC back to the client app.
	 * The AGW network interface does have a command 'y' to query about the number of frames waiting for transmission.
	 * This was implemented in version 1.2.
	 *
	 * I'd rather not take out the queue length check because it is a useful sanity check for something going wrong.
	 * Maybe the check should be performed only for APRS packets.
	 * The check would allow an unlimited number of other types.
	 *
	 * Limit was 20.  Changed to 100 in version 1.2 as a workaround.
	 */

	if ax25_is_aprs(pp) && tq_count(channel, prio, "", "", false) > 100 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Transmit packet queue for channel %d is too long.  Discarding packet.\n", channel)
		dw_printf("Perhaps the channel is so busy there is no opportunity to send.\n")

		return
	}

	logrus.Trace("tq_append: enter critical section")

	tq_mutex.Lock()

	if queue_head[channel][prio] == nil {
		queue_head[channel][prio] = pp
	} else {
		var pnext *packet_t

		var plast = queue_head[channel][prio]
		for {
			pnext = ax25_get_nextp(plast)
			if pnext == nil {
				break
			}

			plast = pnext
		}

		ax25_set_nextp(plast, pp)
	}

	if tq_is_real_packet(pp) {
		queue_len[channel][prio]++
	}

	metrics.SetTxQueueDepth(channel, prio, queue_len[channel][prio])

	tq_mutex.Unlock()

	logrus.Trace("tq_append: left critical section, about to wake up xmit thread")

	if xmit_thread_is_waiting[channel] {
		wake_up_mutex[channel].Lock()
		wake_up_cond[channel].Signal()
		wake_up_mutex[channel].Unlock()
	}
} /* end tq_append */

/*-------------------------------------------------------------------
 *
 * Name:        lm_data_request
 *
 * Purpose:     Add an AX.25 frame to the end of the specified transmit queue.
 *
 *		Use tq_append instead for APRS.
 *
 * Inputs:	channel	- Channel, 0 is first.
 *
 *		prio	- Priority, use TQ_PRIO_0_HI for priority (expedited)
 *				or TQ_PRIO_1_LO for normal.
 *
 *		pp	- Address of packet object.
 *				Ownership is handed over to this function, so
 *				the caller should NOT make any references to
 *				it after this point.
 *
 * Outputs:	A packet object is added to transmit queue.
 *
 * Description:	5.4.
 *
 *		LM-DATA Request. The Data-link State Machine uses this primitive to pass
 *		frames of any type (SABM, RR, UI, etc.) to the Link Multiplexer State Machine.
 *
 *		LM-EXPEDITED-DATA Request. The data-link machine uses this primitive to
 *		request transmission of each digipeat or expedite data frame.
 *
 *		C2a.1
 *
 *		PH-DATA Request. This primitive from the Link Multiplexer State Machine
 *		provides an AX.25 frame of any type (UI, SABM, I, etc.) that is to be transmitted. An
 *		unlimited number of frames may be provided. If the transmission exceeds the 10-
 *		minute limit or the anti-hogging time limit, the half-duplex Physical State Machine
 *		automatically relinquishes the channel for use by the other stations. The
 *		transmission is automatically resumed at the next transmission opportunity
 *		indicated by the CSMA/p-persistence contention algorithm.
 *
 *		PH-EXPEDITED-DATA Request. This primitive from the Link Multiplexer State
 *		Machine provides the AX.25 frame that is to be transmitted immediately. The
 *		simplex Physical State Machine gives preference to priority frames over normal
 *		frames, and will take advantage of the PRIACK window. Priority frames can be
 *		provided by the link multiplexer at any time; a PH-SEIZE Request and subsequent
 *		PH Release Request are not employed for priority frames.
 *
 *		C3.1
 *
 *		LM-DATA Request. This primitive from the Data-link State Machine provides a
 *		AX.25 frame of any type (UI, SABM, I, etc.) that is to be transmitted. An unlimited
 *		number of frames may be provided. The Link Multiplexer State Machine
 *		accumulates the frames in a first-in, first-out queue until it is time to transmit them.
 *
 *		C4.2
 *
 *		LM-DATA Request. This primitive is used by the Data link State Machines to pass
 *		frames of any type (SABM, RR, UI, etc.) to the Link Multiplexer State Machine.
 *
 *		LM-EXPEDITED-DATA Request. This primitive is used by the Data link State
 *		Machine to pass expedited data to the link multiplexer.
 *
 *
 * Implementation: Add packet to end of linked list.
 *		Signal the transmit thread if the queue was formerly empty.
 *
 *		Note that we have a transmit thread each audio channel.
 *		Two channels can share one audio output device.
 *
 * IMPORTANT!	Don't make an further references to the packet object after
 *		giving it to lm_data_request.
 *
 *--------------------------------------------------------------------*/

// TODO: FIXME:  this is a copy of tq_append.  Need to fine tune and explain why.

func lm_data_request(channel int, prio int, pp *packet_t) {
	Assert(prio >= 0 && prio < TQ_NUM_PRIO)

	if pp == nil {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("INTERNAL ERROR:  lm_data_request nil packet pointer. Please report this!\n")

		return
	}

	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		logrus.WithFields(logrus.Fields{
			"channel": channel,
			"prio":    prio,
			"info":    string(AX25GetInfo(pp)),
		}).Debug("lm_data_request")
	}

	/* TODO KG
	#if AX25MEMDEBUG

		if (ax25memdebug_get()) {
		  text_color_set(DW_COLOR_DEBUG);
		  dw_printf ("lm_data_request (channel=%d, prio=%d, seq=%d)\n", channel, prio, ax25memdebug_seq(pp));
		}
	#endif
	*/

	if channel >= 0 && channel < MAX_TOTAL_CHANS && save_audio_config_p.chan_medium[channel] == MEDIUM_NETTNC {
		// For NETTNC channels, just yeet out the packet and let the external TNC handle it - we don't have enough info to do much else
		tq_append(channel, prio, pp)

		return
	}

	if channel < 0 || channel >= MAX_RADIO_CHANS || save_audio_config_p.chan_medium[channel] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR - Request to transmit on unsupported channel %d.\n", channel)
		dw_printf("Connected packet mode requires MEDIUM_RADIO or MEDIUM_NETTNC.\n")

		return
	}

	/*
	 * Is transmit queue out of control?
	 */

	if tq_count(channel, prio, "", "", false) > 250 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Warning: Transmit packet queue for channel %d is extremely long.\n", channel)
		dw_printf("Perhaps the channel is so busy there is no opportunity to send.\n")
	}

	logrus.Trace("lm_data_request: enter critical section")

	tq_mutex.Lock()

	if queue_head[channel][prio] == nil {
		queue_head[channel][prio] = pp
	} else {
		var plast = queue_head[channel][prio]
		for {
			var pnext = ax25_get_nextp(plast)
			if pnext == nil {
				break
			}

			plast = pnext
		}

		ax25_set_nextp(plast, pp)
	}

	if tq_is_real_packet(pp) {
		queue_len[channel][prio]++
	}

	metrics.SetTxQueueDepth(channel, prio, queue_len[channel][prio])

	tq_mutex.Unlock()

	logrus.Trace("lm_data_request: left critical section")

	// Appendix C2a, from the Ax.25 protocol spec, says that a priority frame
	// will start transmission.  If not already transmitting, normal frames
	// will pile up until LM-SEIZE Request starts transmission.

	// Erratum: It doesn't take long for that to fail.
	// We send SABM(e) frames to the transmit queue and the transmitter doesn't get activated.

	//NO!	if (prio == TQ_PRIO_0_HI) {

	logrus.Trace("lm_data_request: about to wake up xmit thread")
	if xmit_thread_is_waiting[channel] {
		wake_up_mutex[channel].Lock()
		wake_up_cond[channel].Signal()
		wake_up_mutex[channel].Unlock()
	}
	//NO!	}
} /* end lm_data_request */

/*-------------------------------------------------------------------
 *
 * Name:        lm_seize_request
 *
 * Purpose:     Force start of transmit even if transmit queue is empty.
 *
 * Inputs:	channel	- Channel, 0 is first.
 *
 * Description:	5.4.
 *
 *		LM-SEIZE Request. The Data-link State Machine uses this primitive to request the
 *		Link Multiplexer State Machine to arrange for transmission at the next available
 *		opportunity. The Data-link State Machine uses this primitive when an
 *		acknowledgement must be made; the exact frame in which the acknowledgement
 *		is sent will be chosen when the actual time for transmission arrives.
 *
 *		C2a.1
 *
 *		PH-SEIZE Request. This primitive requests the simplex state machine to begin
 *		transmitting at the next available opportunity. When that opportunity has been
 *		identified (according to the CSMA/p-persistence algorithm included within), the
 *		transmitter started, a parameterized window provided for the startup of a
 *		conventional repeater (if required), and a parameterized time allowed for the
 *		synchronization of the remote station's receiver (known as TXDELAY in most
 *		implementations), then a PH-SEIZE Confirm primitive is returned to the link
 *		multiplexer.
 *
 *		C3.1
 *
 *		LM-SEIZE Request. This primitive requests the Link Multiplexer State Machine to
 *		arrange for transmission at the next available opportunity. The Data-link State
 *		Machine uses this primitive when an acknowledgment must be made, but the exact
 *		frame in which the acknowledgment will be sent will be chosen when the actual
 *		time for transmission arrives. The Link Multiplexer State Machine uses the LMSEIZE
 *		Confirm primitive to indicate that the transmission opportunity has arrived.
 *		After the Data-link State Machine has provided the acknowledgment, the Data-link
 *		State Machine gives permission to stop transmission with the LM Release Request
 *		primitive.
 *
 *		C4.2
 *
 *		LM-SEIZE Request. This primitive is used by the Data link State Machine to
 *		request the Link Multiplexer State Machine to arrange for transmission at the next
 *		available opportunity. The Data link State Machine uses this primitive when an
 *		acknowledgment must be made, but the exact frame in which the acknowledgment
 *		is sent will be chosen when the actual time for transmission arrives.
 *
 *
 * Implementation: Add a null frame (i.e. length of 0) to give the process a kick.
 *		xmit.c needs to be smart enough to discard it.
 *
 *--------------------------------------------------------------------*/

func lm_seize_request(channel int) {
	var prio = TQ_PRIO_1_LO

	logrus.WithField("channel", channel).Debug("lm_seize_request")

	if channel >= 0 && channel < MAX_TOTAL_CHANS && save_audio_config_p.chan_medium[channel] == MEDIUM_NETTNC {
		// MEDIUM_NETTNC: no internal modem to seize; confirm the channel immediately.
		// See lm_data_request for the rationale for allowing MEDIUM_NETTNC.
		dlq_seize_confirm(channel)

		return
	}

	if channel < 0 || channel >= MAX_RADIO_CHANS || save_audio_config_p.chan_medium[channel] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR - Request to transmit on unsupported channel %d.\n", channel)
		dw_printf("Connected packet mode requires MEDIUM_RADIO or MEDIUM_NETTNC.\n")

		return
	}

	var pp = ax25_new()

	/* TODO KG
	#if AX25MEMDEBUG

		if (ax25memdebug_get()) {
		  text_color_set(DW_COLOR_DEBUG);
		  dw_printf ("lm_seize_request (channel=%d, seq=%d)\n", channel, ax25memdebug_seq(pp));
		}
	#endif
	*/

	logrus.Trace("lm_seize_request: enter critical section")

	tq_mutex.Lock()

	if queue_head[channel][prio] == nil {
		queue_head[channel][prio] = pp
	} else {
		var plast = queue_head[channel][prio]
		for {
			var pnext = ax25_get_nextp(plast)
			if pnext == nil {
				break
			}

			plast = pnext
		}

		ax25_set_nextp(plast, pp)
	}

	if tq_is_real_packet(pp) {
		queue_len[channel][prio]++
	}

	metrics.SetTxQueueDepth(channel, prio, queue_len[channel][prio])

	tq_mutex.Unlock()

	logrus.Trace("lm_seize_request: left critical section")

	logrus.Trace("lm_seize_request: about to wake up xmit thread")

	if xmit_thread_is_waiting[channel] {
		wake_up_mutex[channel].Lock()
		wake_up_cond[channel].Signal()
		wake_up_mutex[channel].Unlock()
	}
} /* end lm_seize_request */

/*-------------------------------------------------------------------
 *
 * Name:        tq_wait_while_empty
 *
 * Purpose:     Sleep while the transmit queue is empty rather than
 *		polling periodically.
 *
 * Inputs:	channel	- Audio device number.
 *
 * Description:	We have one transmit thread for each audio device.
 *		This handles 1 or 2 channels.
 *
 *--------------------------------------------------------------------*/

func tq_wait_while_empty(channel int) {
	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		logrus.WithField("channel", channel).Trace("tq_wait_while_empty: enter critical section")
	}

	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	tq_mutex.Lock()

	var is_empty = tq_is_empty(channel)

	tq_mutex.Unlock()

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		logrus.WithField("channel", channel).Trace("tq_wait_while_empty: left critical section")
		logrus.WithFields(logrus.Fields{
			"channel":  channel,
			"is_empty": is_empty,
		}).Trace("tq_wait_while_empty")
	}

	if is_empty {
		if logrus.IsLevelEnabled(logrus.TraceLevel) {
			logrus.WithField("channel", channel).Trace("tq_wait_while_empty: SLEEP - about to call cond wait")
		}

		wake_up_mutex[channel].Lock()
		xmit_thread_is_waiting[channel] = true
		wake_up_cond[channel].Wait()
		xmit_thread_is_waiting[channel] = false

		if logrus.IsLevelEnabled(logrus.TraceLevel) {
			logrus.WithField("channel", channel).Trace("tq_wait_while_empty: WOKE UP - returned from cond wait")
		}

		wake_up_mutex[channel].Unlock()
	}

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		logrus.WithField("channel", channel).Trace("tq_wait_while_empty returns")
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        tq_remove
 *
 * Purpose:     Remove a packet from the head of the specified transmit queue.
 *
 * Inputs:	channel	- Channel, 0 is first.
 *
 *		prio	- Priority, use TQ_PRIO_0_HI or TQ_PRIO_1_LO.
 *
 * Returns:	Pointer to packet object.
 *
 *--------------------------------------------------------------------*/

func tq_remove(channel int, prio int) *packet_t {
	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		logrus.WithFields(logrus.Fields{
			"channel": channel,
			"prio":    prio,
		}).Trace("tq_remove: enter critical section")
	}
	tq_mutex.Lock()

	var result_p *packet_t

	if queue_head[channel][prio] == nil {
		result_p = nil
	} else {
		result_p = queue_head[channel][prio]
		queue_head[channel][prio] = ax25_get_nextp(result_p)
		ax25_set_nextp(result_p, nil)

		if tq_is_real_packet(result_p) {
			queue_len[channel][prio]--
		}
	}

	metrics.SetTxQueueDepth(channel, prio, queue_len[channel][prio])

	tq_mutex.Unlock()

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		logrus.WithFields(logrus.Fields{
			"channel":  channel,
			"prio":     prio,
			"result_p": result_p,
		}).Trace("tq_remove: leave critical section")
	}

	/* TODO KG
	   #if AX25MEMDEBUG

	   	if (ax25memdebug_get() && result_p != nil) {
	   	  text_color_set(DW_COLOR_DEBUG);
	   	  dw_printf ("tq_remove (channel=%d, prio=%d)  seq=%d\n", channel, prio, ax25memdebug_seq(result_p));
	   	}
	   #endif
	*/
	return (result_p)
} /* end tq_remove */

/*-------------------------------------------------------------------
 *
 * Name:        tq_peek
 *
 * Purpose:     Take a peek at the next frame in the queue but don't remove it.
 *
 * Inputs:	channel	- Channel, 0 is first.
 *
 *		prio	- Priority, use TQ_PRIO_0_HI or TQ_PRIO_1_LO.
 *
 * Returns:	Pointer to packet object or nil.
 *
 *		The packet stays in the queue and belongs to it, so the caller
 *		may inspect it but must not modify it or retain the pointer
 *		beyond the decision of whether to tq_remove it.
 *
 *--------------------------------------------------------------------*/

func tq_peek(channel int, prio int) *packet_t {
	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		logrus.WithFields(logrus.Fields{
			"channel": channel,
			"prio":    prio,
		}).Trace("tq_peek: enter critical section")
	}

	// I don't think we need critical region here.
	//dw_mutex_lock (&tq_mutex);
	var result_p = queue_head[channel][prio]
	// Just take a peek at the head.  Don't remove it.

	//dw_mutex_unlock (&tq_mutex);

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		logrus.WithFields(logrus.Fields{
			"channel":  channel,
			"prio":     prio,
			"result_p": result_p,
		}).Trace("tq_peek: leave critical section")
	}

	/* TODO KG
	   #if AX25MEMDEBUG

	   	if (ax25memdebug_get() && result_p != nil) {
	   	  text_color_set(DW_COLOR_DEBUG);
	   	  dw_printf ("tq_remove (channel=%d, prio=%d)  seq=%d\n", channel, prio, ax25memdebug_seq(result_p));
	   	}
	   #endif
	*/
	return (result_p)
} /* end tq_peek */

/*-------------------------------------------------------------------
 *
 * Name:        tq_is_empty
 *
 * Purpose:     Test if queues for specified channel are empty.
 *
 * Inputs:	channel		Channel
 *
 * Returns:	True if nothing in the queue.
 *
 *--------------------------------------------------------------------*/

func tq_is_empty(channel int) bool {
	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	for p := range TQ_NUM_PRIO {
		Assert(p >= 0 && p < TQ_NUM_PRIO)

		if queue_head[channel][p] != nil {
			return false
		}
	}

	return true
} /* end tq_is_empty */

/*-------------------------------------------------------------------
 *
 * Name:        tq_count
 *
 * Purpose:     Return count of the number of packets (or bytes) in the specified transmit queue.
 *		This is used only for queries from KISS or AWG client applications.
 *
 * Inputs:	channel	- Channel, 0 is first.
 *
 *		prio	- Priority, use TQ_PRIO_0_HI or TQ_PRIO_1_LO.
 *			  Specify -1 for total of both.
 *
 *		source - If specified, count only those with this source address.
 *
 *		dest	- If specified, count only those with this destination address.
 *
 *		bytes	- If true, return number of bytes rather than packets.
 *
 * Returns:	Number of items in specified queue.
 *
 *--------------------------------------------------------------------*/

//#define DEBUG2 1

func tq_count(channel int, prio int, source string, dest string, bytes bool) int {
	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		logrus.WithFields(logrus.Fields{
			"channel": channel,
			"prio":    prio,
			"source":  source,
			"dest":    dest,
			"bytes":   bytes,
		}).Trace("tq_count")
	}
	if prio == -1 {
		return (tq_count(channel, TQ_PRIO_0_HI, source, dest, bytes) + tq_count(channel, TQ_PRIO_1_LO, source, dest, bytes))
	}

	// Don't want lists being rearranged while we are traversing them.

	tq_mutex.Lock()
	defer tq_mutex.Unlock()

	var n = tq_count_locked(channel, prio, source, dest, bytes)

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		logrus.WithFields(logrus.Fields{
			"channel": channel,
			"prio":    prio,
			"source":  source,
			"dest":    dest,
			"bytes":   bytes,
			"n":       n,
		}).Trace("tq_count returns")
	}

	return (n)
} /* end tq_count */

// tq_count_locked is tq_count's traversal, for callers that already hold
// tq_mutex.  Counting a queue and publishing that count have to happen under
// the same lock acquisition: two separately-locked operations can straddle
// another goroutine's enqueue and publish observations out of order, leaving
// the gauge describing a queue depth that never existed.
func tq_count_locked(channel int, prio int, source string, dest string, bytes bool) int {
	// Array bounds check.  FIXME: TODO:  should have internal error instead of dying.

	if channel < 0 || channel >= MAX_RADIO_CHANS || prio < 0 || prio >= TQ_NUM_PRIO {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("INTERNAL ERROR - tq_count_locked(%d, %d, \"%s\", \"%s\", %t)\n", channel, prio, source, dest, bytes)

		return (0)
	}

	var n = 0 // Result.  Number of bytes or packets.
	var pp = queue_head[channel][prio]

	for pp != nil {
		if ax25_get_num_addr(pp) >= AX25_MIN_ADDRS {
			// Consider only real packets.
			var count_it = 1

			if source != "" {
				var frame_source = ax25_get_addr_with_ssid(pp, AX25_SOURCE)
				if logrus.IsLevelEnabled(logrus.TraceLevel) {
					logrus.WithField("frame_source", frame_source).Trace("tq_count: compare to frame source")
				}
				if source != frame_source {
					count_it = 0
				}
			}

			if count_it > 0 && dest != "" {
				var frame_dest = ax25_get_addr_with_ssid(pp, AX25_DESTINATION)
				if logrus.IsLevelEnabled(logrus.TraceLevel) {
					logrus.WithField("frame_dest", frame_dest).Trace("tq_count: compare to frame destination")
				}
				if dest != frame_dest {
					count_it = 0
				}
			}

			if count_it > 0 {
				if bytes {
					n += ax25_get_frame_len(pp)
				} else {
					n++
				}
			}
		}

		pp = ax25_get_nextp(pp)
	}

	return (n)
} /* end tq_count_locked */

/* end tq.c */
