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
	"sync/atomic"
	"time"

	"github.com/lestrrat-go/strftime"
)

const TQ_NUM_PRIO = 2 /* Number of priorities. */

const TQ_PRIO_0_HI = 0
const TQ_PRIO_1_LO = 1

// tqChanBuf is the buffer capacity of each per-priority Go channel queue.
// It is intentionally larger than the soft limits (100 APRS, 250 connected)
// so that sends in normal operation never block.
const tqChanBuf = 1024

type TransmitQueue struct {
	saveAudioConfigP *audio_s

	// queueHead is the actual transmit queue: one buffered channel per
	// (radio channel, priority) pair.  Sending to a channel enqueues a
	// packet; receiving dequeues it.  Go channels provide the necessary
	// thread-safety without an explicit mutex.
	queueHead [MAX_RADIO_CHANS][TQ_NUM_PRIO]chan *packet_t

	// peeked holds a packet that has been taken from queueHead but not yet
	// handed to the transmitter.  It is read and written only by the single
	// xmit goroutine that owns the channel, so no additional synchronisation
	// is required.
	peeked [MAX_RADIO_CHANS][TQ_NUM_PRIO]*packet_t

	// byteCount tracks the total frame bytes currently held in each queue,
	// including any peeked packet.  Updated atomically so that
	// tq_count(bytes=true) can be answered without draining the channel.
	byteCount [MAX_RADIO_CHANS][TQ_NUM_PRIO]atomic.Int64
}

/*-------------------------------------------------------------------
 *
 * Name:        NewTransmitQueue
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

func NewTransmitQueue(audio_config_p *audio_s) *TransmitQueue {
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("tq_init (  )\n");
	#endif
	*/
	var tq = new(TransmitQueue)
	tq.saveAudioConfigP = audio_config_p

	for c := 0; c < MAX_RADIO_CHANS; c++ {
		for p := 0; p < TQ_NUM_PRIO; p++ {
			tq.queueHead[c][p] = make(chan *packet_t, tqChanBuf)
		}
	}

	return tq
} /* end NewTransmitQueue */

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
 *			New in 1.7:
 *			Channel can be assigned to IGate rather than a radio.
 *
 *			New in 1.8:
 *			Channel can be assigned to a network TNC.
 *
 *		prio	- Priority, use TQ_PRIO_0_HI for digipeated or
 *				TQ_PRIO_1_LO for normal.
 *
 *		pp	- Address of packet object.
 *				Caller should NOT make any references to
 *				it after this point because it could
 *				be deleted at any time.
 *
 * Outputs:
 *
 * Description:	Add packet to end of queue.
 *		Signal the transmit thread if the queue was formerly empty.
 *
 *		Note that we have a transmit thread each audio channel.
 *		Two channels can share one audio output device.
 *
 * IMPORTANT!	Don't make an further references to the packet object after
 *		giving it to tq_append.
 *
 *--------------------------------------------------------------------*/

func (tq *TransmitQueue) tq_append(channel int, prio int, pp *packet_t) {
	/* TODO KG
	#if DEBUG
		unsigned char *pinfo;
		int info_len = ax25_get_info (pp, &pinfo);
		if (info_len > 10) info_len = 10;
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("tq_append (channel=%d, prio=%d, pp=%p) \"%*s\"\n", channel, prio, pp, info_len, (char*)pinfo);
	#endif
	*/
	Assert(prio >= 0 && prio < TQ_NUM_PRIO)

	if pp == nil {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("INTERNAL ERROR:  tq_append nil packet pointer. Please report this!\n")

		return
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

	if tq.saveAudioConfigP.chan_medium[channel] == MEDIUM_IGATE ||
		tq.saveAudioConfigP.chan_medium[channel] == MEDIUM_NETTNC {
		var ts string // optional time stamp.

		if tq.saveAudioConfigP.timestamp_format != "" {
			var formattedTime, _ = strftime.Format(tq.saveAudioConfigP.timestamp_format, time.Now())
			ts = " " + formattedTime // space after channel.
		}

		// Formated addresses.
		var stemp = ax25_format_addrs(pp)
		var pinfo = ax25_get_info(pp)

		text_color_set(DW_COLOR_XMIT)

		if tq.saveAudioConfigP.chan_medium[channel] == MEDIUM_IGATE {
			dw_printf("[%d>is%s] ", channel, ts)
			dw_printf("%s", stemp) /* stations followed by : */
			ax25_safe_print(pinfo, !ax25_is_aprs(pp))
			dw_printf("\n")

			igate_send_rec_packet(channel, pp)
		} else { // network TNC
			dw_printf("[%d>nt%s] ", channel, ts)
			dw_printf("%s", stemp) /* stations followed by : */
			ax25_safe_print(pinfo, !ax25_is_aprs(pp))
			dw_printf("\n")

			nettnc_send_packet(channel, pp)
		}

		ax25_delete(pp)

		return
	}

	// Normal case - put in queue for radio transmission.
	// Error if trying to transmit to a radio channel which was not configured.

	if channel < 0 || channel >= MAX_RADIO_CHANS || tq.saveAudioConfigP.chan_medium[channel] == MEDIUM_NONE {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR - Request to transmit on invalid radio channel %d.\n", channel)
		dw_printf("This is probably a client application error, not a problem with direwolf.\n")
		dw_printf("Are you using AX.25 for Linux?  It might be trying to use a modified\n")
		dw_printf("version of KISS which uses the port field differently than the\n")
		dw_printf("original KISS protocol specification.  The solution might be to use\n")
		dw_printf("a command like \"kissparms -c 1 -p radio\" to set CRC none mode.\n")
		dw_printf("\n")
		ax25_delete(pp)

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

	if ax25_is_aprs(pp) && tq.tq_count(channel, prio, "", "", false) > 100 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Transmit packet queue for channel %d is too long.  Discarding packet.\n", channel)
		dw_printf("Perhaps the channel is so busy there is no opportunity to send.\n")
		ax25_delete(pp)

		return
	}

	tq.byteCount[channel][prio].Add(int64(ax25_get_frame_len(pp)))
	tq.queueHead[channel][prio] <- pp
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
 *				Caller should NOT make any references to
 *				it after this point because it could
 *				be deleted at any time.
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
 * Implementation: Add packet to end of queue.
 *		Signal the transmit thread if the queue was formerly empty.
 *
 *		Note that we have a transmit thread each audio channel.
 *		Two channels can share one audio output device.
 *
 * IMPORTANT!	Don't make an further references to the packet object after
 *		giving it to lm_data_request.
 *
 *--------------------------------------------------------------------*/

func (tq *TransmitQueue) lm_data_request(channel int, prio int, pp *packet_t) {
	/* TODO KG
	#if DEBUG
		unsigned char *pinfo;
		int info_len = ax25_get_info (pp, &pinfo);
		if (info_len > 10) info_len = 10;
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("lm_data_request (channel=%d, prio=%d, pp=%p) \"%*s\"\n", channel, prio, pp, info_len, (char*)pinfo);
	#endif
	*/
	Assert(prio >= 0 && prio < TQ_NUM_PRIO)

	if pp == nil {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("INTERNAL ERROR:  lm_data_request nil packet pointer. Please report this!\n")

		return
	}

	/* TODO KG
	#if AX25MEMDEBUG

		if (ax25memdebug_get()) {
		  text_color_set(DW_COLOR_DEBUG);
		  dw_printf ("lm_data_request (channel=%d, prio=%d, seq=%d)\n", channel, prio, ax25memdebug_seq(pp));
		}
	#endif
	*/

	if channel < 0 || channel >= MAX_RADIO_CHANS || tq.saveAudioConfigP.chan_medium[channel] != MEDIUM_RADIO {
		// Connected mode is allowed only with internal modems.
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR - Request to transmit on invalid radio channel %d.\n", channel)
		dw_printf("Connected packet mode is allowed only with internal modems.\n")
		dw_printf("Why aren't external KISS modems allowed?  See\n")
		dw_printf("Why-is-9600-only-twice-as-fast-as-1200.pdf for explanation.\n")
		ax25_delete(pp)

		return
	}

	/*
	 * Is transmit queue out of control?
	 */

	if tq.tq_count(channel, prio, "", "", false) > 250 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Warning: Transmit packet queue for channel %d is extremely long.\n", channel)
		dw_printf("Perhaps the channel is so busy there is no opportunity to send.\n")
	}

	// Appendix C2a, from the Ax.25 protocol spec, says that a priority frame
	// will start transmission.  If not already transmitting, normal frames
	// will pile up until LM-SEIZE Request starts transmission.

	// Erratum: It doesn't take long for that to fail.
	// We send SABM(e) frames to the transmit queue and the transmitter doesn't get activated.

	//NO!	if (prio == TQ_PRIO_0_HI) {

	tq.byteCount[channel][prio].Add(int64(ax25_get_frame_len(pp)))
	tq.queueHead[channel][prio] <- pp

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

func (tq *TransmitQueue) lm_seize_request(channel int) {
	var prio = TQ_PRIO_1_LO

	/* TODO KG
	#if DEBUG
		unsigned char *pinfo;
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("lm_seize_request (channel=%d)\n", channel);
	#endif
	*/

	if channel < 0 || channel >= MAX_RADIO_CHANS || tq.saveAudioConfigP.chan_medium[channel] != MEDIUM_RADIO {
		// Connected mode is allowed only with internal modems.
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR - Request to transmit on invalid radio channel %d.\n", channel)
		dw_printf("Connected packet mode is allowed only with internal modems.\n")
		dw_printf("Why aren't external KISS modems allowed?  See\n")
		dw_printf("Why-is-9600-only-twice-as-fast-as-1200.pdf for explanation.\n")

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

	tq.byteCount[channel][prio].Add(int64(ax25_get_frame_len(pp)))
	tq.queueHead[channel][prio] <- pp
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

func (tq *TransmitQueue) tq_wait_while_empty(channel int) {
	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	// Return immediately if there is already a peeked packet or data in any queue.
	for p := 0; p < TQ_NUM_PRIO; p++ {
		if tq.peeked[channel][p] != nil || len(tq.queueHead[channel][p]) > 0 {
			return
		}
	}

	// All queues are empty; block until a packet arrives in any of them.
	// The received packet is stored in the peeked slot so it is not lost.
	select {
	case pp := <-tq.queueHead[channel][TQ_PRIO_0_HI]:
		tq.peeked[channel][TQ_PRIO_0_HI] = pp
	case pp := <-tq.queueHead[channel][TQ_PRIO_1_LO]:
		tq.peeked[channel][TQ_PRIO_1_LO] = pp
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
 *		Caller should destroy it with ax25_delete when finished with it.
 *
 *--------------------------------------------------------------------*/

func (tq *TransmitQueue) tq_remove(channel int, prio int) *packet_t {
	var result_p *packet_t

	if tq.peeked[channel][prio] != nil {
		result_p = tq.peeked[channel][prio]
		tq.peeked[channel][prio] = nil
	} else {
		select {
		case result_p = <-tq.queueHead[channel][prio]:
		default:
		}
	}

	if result_p != nil {
		tq.byteCount[channel][prio].Add(-int64(ax25_get_frame_len(result_p)))
	}

	/* TODO KG
	   #if AX25MEMDEBUG

	   	if (ax25memdebug_get() && result_p != nil) {
	   	  text_color_set(DW_COLOR_DEBUG);
	   	  dw_printf ("tq_remove (channel=%d, prio=%d)  seq=%d\n", channel, prio, ax25memdebug_seq(result_p));
	   	}
	   #endif
	*/
	return result_p
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
 *		Caller should NOT destroy it because it is still in the queue.
 *
 *--------------------------------------------------------------------*/

func (tq *TransmitQueue) tq_peek(channel int, prio int) *packet_t {
	if tq.peeked[channel][prio] != nil {
		return tq.peeked[channel][prio]
	}

	// Non-blocking receive: move the head packet into the peeked slot so
	// subsequent peek and remove calls see it without touching the channel again.
	select {
	case pp := <-tq.queueHead[channel][prio]:
		tq.peeked[channel][prio] = pp
		return pp
	default:
		return nil
	}
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

func (tq *TransmitQueue) tq_is_empty(channel int) bool {
	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	for p := 0; p < TQ_NUM_PRIO; p++ {
		if tq.peeked[channel][p] != nil || len(tq.queueHead[channel][p]) > 0 {
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
 *		source - Reserved for future use; pass "".
 *
 *		dest	- Reserved for future use; pass "".
 *
 *		bytes	- If true, return number of bytes rather than packets.
 *
 * Returns:	Number of items in specified queue.
 *
 *--------------------------------------------------------------------*/

func (tq *TransmitQueue) tq_count(channel int, prio int, source string, dest string, bytes bool) int {
	if prio == -1 {
		return tq.tq_count(channel, TQ_PRIO_0_HI, source, dest, bytes) + tq.tq_count(channel, TQ_PRIO_1_LO, source, dest, bytes)
	}

	if channel < 0 || channel >= MAX_RADIO_CHANS || prio < 0 || prio >= TQ_NUM_PRIO {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("INTERNAL ERROR - tq_count(%d, %d, \"%s\", \"%s\", %t)\n", channel, prio, source, dest, bytes)

		return 0
	}

	if bytes {
		return int(tq.byteCount[channel][prio].Load())
	}
	var count = len(tq.queueHead[channel][prio])
	if tq.peeked[channel][prio] != nil {
		count++
	}
	return count
} /* end tq_count */

/* end tq.c */
