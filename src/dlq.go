package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Received frame queue.
 *
 * Description: In earlier versions, the main thread read from the
 *		audio device and performed the receive demodulation/decoding.
 *
 *		Since version 1.2 we have a separate receive thread
 *		for each audio device.  This queue is used to collect
 *		received frames from all channels and process them
 *		serially.
 *
 *		In version 1.4, other types of events also go into this
 *		queue and we use it to drive the data link state machine.
 *
 *---------------------------------------------------------------*/

// #include "direwolf.h"
// #include <stdio.h>
// #include <unistd.h>
// #include <stdlib.h>
// #include <assert.h>
// #include <string.h>
// #include <errno.h>
// #include "ax25_pad.h"
// #include "textcolor.h"
// #include "audio.h"
// #include "dlq.h"
// #include "dedupe.h"
// #include "dtime_now.h"
// extern int ATEST_C;
// void dlq_rec_frame_fake (int chan, int subchan, int slice, packet_t pp, alevel_t alevel, fec_type_t fec_type, retry_t retries, char *spectrum);
import "C"

import (
	"sync"
	"time"
	"unsafe"
)

/* The queue is a linked list of these. */

var dlq_queue_head *C.struct_dlq_item_s /* Head of linked list for queue. */

var dlq_mutex sync.Mutex /* Critical section for updating queues. */

var dlq_wake_up_chan = make(chan struct{}) /* Notify received packet processing thread when queue not empty. */

var recv_thread_is_waiting bool

var was_init bool /* was initialization performed? */

var s_new_count = 0    /* To detect memory leak for queue items. */
var s_delete_count = 0 // TODO:  need to test.

var s_cdata_new_count = 0    /* To detect memory leak for connected mode data. */
var s_cdata_delete_count = 0 // TODO:  need to test.

/*-------------------------------------------------------------------
 *
 * Name:        dlq_init
 *
 * Purpose:     Initialize the queue.
 *
 * Inputs:	None.
 *
 * Outputs:
 *
 * Description:	Initialize the queue to be empty and set up other
 *		mechanisms for sharing it between different threads.
 *
 *--------------------------------------------------------------------*/

func dlq_init() {
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq_init ( )\n");
	#endif
	*/

	dlq_queue_head = nil

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq_init: pthread_cond_init...\n");
	#endif
	*/

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq_init: pthread_cond_init returns %d\n", err);
	#endif
	*/

	recv_thread_is_waiting = false

	was_init = true

} /* end dlq_init */

/*-------------------------------------------------------------------
 *
 * Name:        dlq_rec_frame
 *
 * Purpose:     Add a received packet to the end of the queue.
 *		Normally this was received over the radio but we can create
 *		our own from APRStt or beaconing.
 *
 *		This would correspond to PH-DATA Indication in the AX.25 protocol spec.
 *
 * Inputs:	chan	- Channel, 0 is first.
 *
 *		subchan	- Which modem caught it.
 *			  Special case -1 for APRStt gateway.
 *
 *		slice	- Which slice we picked.
 *
 *		pp	- Address of packet object.
 *				Caller should NOT make any references to
 *				it after this point because it could
 *				be deleted at any time.
 *
 *		alevel	- Audio level, range of 0 - 100.
 *				(Special case, use negative to skip
 *				 display of audio level line.
 *				 Use -2 to indicate DTMF message.)
 *
 *		fec_type - Was it from FX.25 or IL2P?  Need to know because
 *			  meaning of retries is different.
 *
 *		retries	- Level of correction used.
 *
 *		spectrum - Display of how well multiple decoders did.
 *
 *
 * IMPORTANT!	Don't make an further references to the packet object after
 *		giving it to dlq_append.
 *
 *--------------------------------------------------------------------*/

func dlq_rec_frame_real(channel C.int, subchannel C.int, slice C.int, pp C.packet_t, alevel C.alevel_t, fec_type C.fec_type_t, retries C.retry_t, spectrum *C.char) {

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq_rec_frame (chan=%d, pp=%p, ...)\n", channel, pp);
	#endif
	*/

	Assert(channel >= 0 && channel < MAX_TOTAL_CHANS) // TOTAL to include virtual channels.

	if pp == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("INTERNAL ERROR:  dlq_rec_frame nil packet pointer. Please report this!\n")
		return
	}

	/* TODO KG
	#if AX25MEMDEBUG

		if (ax25memdebug_get()) {
		  text_color_set(DW_COLOR_DEBUG);
		  dw_printf ("dlq_rec_frame (chan=%d.%d, seq=%d, ...)\n", channel, subchannel, ax25memdebug_seq(pp));
		}
	#endif
	*/

	/* Allocate a new queue item. */

	var pnew = new(C.struct_dlq_item_s)
	s_new_count++

	if s_new_count > s_delete_count+50 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("INTERNAL ERROR:  DLQ memory leak, new=%d, delete=%d\n", s_new_count, s_delete_count)
	}

	pnew.nextp = nil
	pnew._type = C.DLQ_REC_FRAME
	pnew._chan = channel
	pnew.slice = slice
	pnew.subchan = subchannel
	pnew.pp = pp
	pnew.alevel = alevel
	pnew.fec_type = fec_type
	pnew.retries = retries
	if spectrum == nil {
		C.strcpy(&pnew.spectrum[0], C.CString(""))
	} else {
		C.strcpy(&pnew.spectrum[0], spectrum)
	}

	/* Put it into queue. */

	append_to_queue(pnew)

} /* end dlq_rec_frame */

//export dlq_rec_frame
func dlq_rec_frame(channel C.int, subchannel C.int, slice C.int, pp C.packet_t, alevel C.alevel_t, fec_type C.fec_type_t, retries C.retry_t, spectrum *C.char) {
	if C.ATEST_C != 0 {
		C.dlq_rec_frame_fake(channel, subchannel, slice, pp, alevel, fec_type, retries, spectrum)
	} else {
		dlq_rec_frame_real(channel, subchannel, slice, pp, alevel, fec_type, retries, spectrum)
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        append_to_queue
 *
 * Purpose:     Append some type of event to queue.
 *		This includes frames received over the radio,
 *		requests from client applications, and notifications
 *		from the frame transmission process.
 *
 *
 * Inputs:	pnew		- Pointer to queue element structure.
 *
 * Outputs:	Information is appended to queue.
 *
 * Description:	Add item to end of linked list.
 *		Signal the receive processing thread if the queue was formerly empty.
 *
 *--------------------------------------------------------------------*/

func append_to_queue(pnew *C.struct_dlq_item_s) {

	if !was_init {
		dlq_init()
	}

	pnew.nextp = nil

	/* TODO
	#if DEBUG1
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq append_to_queue: enter critical section\n");
	#endif
	*/
	dlq_mutex.Lock()

	var plast *C.struct_dlq_item_s
	var queue_length int
	if dlq_queue_head == nil {
		dlq_queue_head = pnew
		queue_length = 1
	} else {
		queue_length = 2 /* head + new one */
		plast = dlq_queue_head
		for plast.nextp != nil {
			plast = plast.nextp
			queue_length++
		}
		plast.nextp = pnew
	}

	dlq_mutex.Unlock()
	/* TODO
	#if DEBUG1
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq append_to_queue: left critical section\n");
		dw_printf ("dlq append_to_queue (): about to wake up recv processing thread.\n");
	#endif
	*/

	/*
	 * Bug:  June 2015, version 1.2
	 *
	 * It has long been known that we will eventually block trying to write to a
	 * pseudo terminal if nothing is reading from the other end.  There is even
	 * a warning at start up time:
	 *
	 *	Virtual KISS TNC is available on /dev/pts/2
	 *	WARNING - Dire Wolf will hang eventually if nothing is reading from it.
	 *	Created symlink /tmp/kisstnc -> /dev/pts/2
	 *
	 * In earlier versions, where the audio input and demodulation was in the main
	 * thread, that would stop and it was pretty obvious something was wrong.
	 * In version 1.2, the audio in / demodulating was moved to a device specific
	 * thread.  Packet objects are appended to this queue.
	 *
	 * The main thread should wake up and process them which includes printing and
	 * forwarding to clients over multiple protocols and transport methods.
	 * Just before the 1.2 release someone reported a memory leak which only showed
	 * up after about 20 hours.  It happened to be on a Cubie Board 2, which shouldn't
	 * make a difference unless there was some operating system difference.
	 * (cubieez 2.0 is based on Debian wheezy, just like Raspian.)
	 *
	 * The debug output revealed:
	 *
	 *	It was using AX.25 for Linux (not APRS).
	 *	The pseudo terminal KISS interface was being used.
	 *	Transmitting was continuing fine.  (So something must be writing to the other end.)
	 *	Frames were being received and appended to this queue.
	 *	They were not coming out of the queue.
	 *
	 * My theory is that writing to the the pseudo terminal is blocking so the
	 * main thread is stopped.   It's not taking anything from this queue and we detect
	 * it as a memory leak.
	 *
	 * Add a new check here and complain if the queue is growing too large.
	 * That will get us a step closer to the root cause.
	 * This has been documented in the User Guide and the CHANGES.txt file which is
	 * a minimal version of Release Notes.
	 * The proper fix will be somehow avoiding or detecting the pseudo terminal filling up
	 * and blocking on a write.
	 */

	if queue_length > 10 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Received frame queue is out of control. Length=%d.\n", queue_length)
		dw_printf("Reader thread is probably frozen.\n")
		dw_printf("This can be caused by using a pseudo terminal (direwolf -p) where another\n")
		dw_printf("application is not reading the frames from the other side.\n")
	}

	if recv_thread_is_waiting {
		dlq_wake_up_chan <- struct{}{}
	}

} /* end append_to_queue */

/*-------------------------------------------------------------------
 *
 * Name:        dlq_connect_request
 *
 * Purpose:     Client application has requested connection to another station.
 *
 * Inputs:	addrs		- Source (owncall), destination (peercall),
 *				  and possibly digipeaters.
 *
 *		num_addr	- Number of addresses.  2 to 10.
 *
 *		chan		- Channel, 0 is first.
 *
 *		client		- Client application instance.  We could have multiple
 *				  applications, all on the same channel, connecting
 *				  to different stations.   We need to know which one
 *				  should get the results.
 *
 *		pid		- Protocol ID for data.  Normally 0xf0 but the API
 *				  allows the client app to use something non-standard
 *				  for special situations.
 *						TODO: remove this.   PID is only for I and UI frames.
 *
 * Outputs:	Request is appended to queue for processing by
 *		the data link state machine.
 *
 *--------------------------------------------------------------------*/

func dlq_connect_request(addrs [AX25_MAX_ADDRS][AX25_MAX_ADDR_LEN]C.char, num_addr C.int, channel C.int, client C.int, pid C.int) {

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq_connect_request (...)\n");
	#endif
	*/

	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	/* Allocate a new queue item. */

	var pnew = new(C.struct_dlq_item_s)
	s_new_count++

	pnew._type = C.DLQ_CONNECT_REQUEST
	pnew._chan = channel
	C.memcpy(unsafe.Pointer(&pnew.addrs), unsafe.Pointer(&addrs), AX25_MAX_ADDRS*AX25_MAX_ADDR_LEN)
	pnew.num_addr = num_addr
	pnew.client = client

	/* Put it into queue. */

	append_to_queue(pnew)

} /* end dlq_connect_request */

/*-------------------------------------------------------------------
 *
 * Name:        dlq_disconnect_request
 *
 * Purpose:     Client application has requested to disconnect.
 *
 * Inputs:	addrs		- Source (owncall), destination (peercall),
 *				  and possibly digipeaters.
 *
 *		num_addr	- Number of addresses.  2 to 10.
 *				  Only first two matter in this case.
 *
 *		chan		- Channel, 0 is first.
 *
 *		client		- Client application instance.  We could have multiple
 *				  applications, all on the same channel, connecting
 *				  to different stations.   We need to know which one
 *				  should get the results.
 *
 * Outputs:	Request is appended to queue for processing by
 *		the data link state machine.
 *
 *--------------------------------------------------------------------*/

func dlq_disconnect_request(addrs [AX25_MAX_ADDRS][AX25_MAX_ADDR_LEN]C.char, num_addr C.int, channel C.int, client C.int) {
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq_disconnect_request (...)\n");
	#endif
	*/

	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	/* Allocate a new queue item. */

	var pnew = new(C.struct_dlq_item_s)
	s_new_count++

	pnew._type = C.DLQ_DISCONNECT_REQUEST
	pnew._chan = channel
	C.memcpy(unsafe.Pointer(&pnew.addrs), unsafe.Pointer(&addrs), AX25_MAX_ADDRS*AX25_MAX_ADDR_LEN)
	pnew.num_addr = num_addr
	pnew.client = client

	/* Put it into queue. */

	append_to_queue(pnew)

} /* end dlq_disconnect_request */

/*-------------------------------------------------------------------
 *
 * Name:        dlq_outstanding_frames_request
 *
 * Purpose:     Client application wants to know number of outstanding information
 *		frames supplied, supplied by the client, that have not yet been
 *		delivered to the remote station.
 *
 * Inputs:	addrs		- Source (owncall), destination (peercall)
 *
 *		num_addr	- Number of addresses.  Should be 2.
 *				  If more they will be ignored.
 *
 *		chan		- Channel, 0 is first.
 *
 *		client		- Client application instance.  We could have multiple
 *				  applications, all on the same channel, connecting
 *				  to different stations.   We need to know which one
 *				  should get the results.
 *
 * Outputs:	Request is appended to queue for processing by
 *		the data link state machine.
 *
 * Description:	The data link state machine will count up all information frames
 *		for the given source(mycall) / destination(remote) / channel link.
 *		A 'Y' response will be sent back to the client application.
 *
 *--------------------------------------------------------------------*/

func dlq_outstanding_frames_request(addrs [AX25_MAX_ADDRS][AX25_MAX_ADDR_LEN]C.char, num_addr C.int, channel C.int, client C.int) {
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq_outstanding_frames_request (...)\n");
	#endif
	*/

	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	/* Allocate a new queue item. */

	var pnew = new(C.struct_dlq_item_s)
	s_new_count++

	pnew._type = C.DLQ_OUTSTANDING_FRAMES_REQUEST
	pnew._chan = channel
	C.memcpy(unsafe.Pointer(&pnew.addrs), unsafe.Pointer(&addrs), AX25_MAX_ADDRS*AX25_MAX_ADDR_LEN)
	pnew.num_addr = num_addr
	pnew.client = client

	/* Put it into queue. */

	append_to_queue(pnew)

} /* end dlq_outstanding_frames_request */

/*-------------------------------------------------------------------
 *
 * Name:        dlq_xmit_data_request
 *
 * Purpose:     Client application has requested transmission of connected
 *		data over an established link.
 *
 * Inputs:	addrs		- Source (owncall), destination (peercall),
 *				  and possibly digipeaters.
 *
 *		num_addr	- Number of addresses.  2 to 10.
 *				  First two are used to uniquely identify link.
 *				  Any digipeaters involved are remembered
 *				  from when the link was established.
 *
 *		chan		- Channel, 0 is first.
 *
 *		client		- Client application instance.
 *
 *		pid		- Protocol ID for data.  Normally 0xf0 but the API
 *				  allows the client app to use something non-standard
 *				  for special situations.
 *
 *		xdata_ptr	- Pointer to block of data.
 *
 *		xdata_len	- Length of data in bytes.
 *
 * Outputs:	Request is appended to queue for processing by
 *		the data link state machine.
 *
 *--------------------------------------------------------------------*/

func dlq_xmit_data_request(addrs [AX25_MAX_ADDRS][AX25_MAX_ADDR_LEN]C.char, num_addr C.int, channel C.int, client C.int, pid C.int, xdata_ptr *C.char, xdata_len C.int) {

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq_xmit_data_request (...)\n");
	#endif
	*/

	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	/* Allocate a new queue item. */

	var pnew = new(C.struct_dlq_item_s)
	s_new_count++

	pnew._type = C.DLQ_XMIT_DATA_REQUEST
	pnew._chan = channel
	C.memcpy(unsafe.Pointer(&pnew.addrs), unsafe.Pointer(&addrs), AX25_MAX_ADDRS*AX25_MAX_ADDR_LEN)
	pnew.num_addr = num_addr
	pnew.client = client

	/* Attach the transmit data. */

	pnew.txdata = cdata_new(pid, xdata_ptr, xdata_len)

	/* Put it into queue. */

	append_to_queue(pnew)

} /* end dlq_xmit_data_request */

/*-------------------------------------------------------------------
 *
 * Name:        dlq_register_callsign
 *		dlq_unregister_callsign
 *
 * Purpose:     Register callsigns that we will recognize for incoming connection requests.
 *
 * Inputs:	addr		- Callsign to [un]register.
 *
 *		chan		- Channel, 0 is first.
 *
 *		client		- Client application instance.
 *
 * Outputs:	Request is appended to queue for processing by
 *		the data link state machine.
 *
 * Description:	The data link state machine does not use MYCALL from the APRS configuration.
 *		For outgoing frames, the client supplies the source callsign.
 *		For incoming connection requests, we need to know what address(es) to respond to.
 *
 *		Note that one client application can register multiple callsigns for
 *		multiple channels.
 *		Different clients can register different different addresses on the same channel.
 *
 *--------------------------------------------------------------------*/

func dlq_register_callsign(addr *C.char, channel C.int, client C.int) {

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq_register_callsign (%s, chan=%d, client=%d)\n", addr, channel, client);
	#endif
	*/

	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	/* Allocate a new queue item. */

	var pnew = new(C.struct_dlq_item_s)
	s_new_count++

	pnew._type = C.DLQ_REGISTER_CALLSIGN
	pnew._chan = channel
	C.strcpy(&pnew.addrs[0][0], addr)
	pnew.num_addr = 1
	pnew.client = client

	/* Put it into queue. */

	append_to_queue(pnew)

} /* end dlq_register_callsign */

func dlq_unregister_callsign(addr *C.char, channel C.int, client C.int) {

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq_unregister_callsign (%s, chan=%d, client=%d)\n", addr, channel, client);
	#endif
	*/

	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	/* Allocate a new queue item. */

	var pnew = new(C.struct_dlq_item_s)
	s_new_count++

	pnew._type = C.DLQ_UNREGISTER_CALLSIGN
	pnew._chan = channel
	C.strcpy(&pnew.addrs[0][0], addr)
	pnew.num_addr = 1
	pnew.client = client

	/* Put it into queue. */

	append_to_queue(pnew)

} /* end dlq_unregister_callsign */

/*-------------------------------------------------------------------
 *
 * Name:        dlq_channel_busy
 *
 * Purpose:     Inform data link state machine about activity on the radio channel.
 *
 * Inputs:	chan		- Radio channel number.
 *
 *		activity	- OCTYPE_PTT or OCTYPE_DCD, as defined in audio.h.
 *				  Other values will be discarded.
 *
 *		status		- 1 for active or 0 for quiet.
 *
 * Outputs:	Request is appended to queue for processing by
 *		the data link state machine.
 *
 * Description:	Notify the link state machine about changes in carrier detect
 *		and our transmitter.
 *		This is needed for pausing some of our timers.   For example,
 *		if we transmit a frame and expect a response in 3 seconds, that
 *		might be delayed because someone else is using the channel.
 *
 *--------------------------------------------------------------------*/

//export dlq_channel_busy
func dlq_channel_busy(channel C.int, activity C.int, status C.int) {

	if activity == OCTYPE_PTT || activity == OCTYPE_DCD {
		/* TODO KG
		#if DEBUG
			  text_color_set(DW_COLOR_DEBUG);
			  dw_printf ("dlq_channel_busy (...)\n");
		#endif
		*/

		/* Allocate a new queue item. */

		var pnew = new(C.struct_dlq_item_s)
		s_new_count++

		pnew._type = C.DLQ_CHANNEL_BUSY
		pnew._chan = channel
		pnew.activity = activity
		pnew.status = status

		/* Put it into queue. */

		append_to_queue(pnew)
	}

} /* end dlq_channel_busy */

/*-------------------------------------------------------------------
 *
 * Name:        dlq_seize_confirm
 *
 * Purpose:     Inform data link state machine that the transmitter is on.
 *		This is in response to lm_seize_request.
 *
 * Inputs:	chan		- Radio channel number.
 *
 * Outputs:	Request is appended to queue for processing by
 *		the data link state machine.
 *
 * Description:	When removed from the data link state machine queue, this
 *		becomes lm_seize_confirm.
 *
 *--------------------------------------------------------------------*/

func dlq_seize_confirm(channel C.int) {

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq_seize_confirm (chan=%d)\n", channel);
	#endif
	*/

	/* Allocate a new queue item. */

	var pnew = new(C.struct_dlq_item_s)
	s_new_count++

	pnew._type = C.DLQ_SEIZE_CONFIRM
	pnew._chan = channel

	/* Put it into queue. */

	append_to_queue(pnew)

} /* end dlq_seize_confirm */

/*-------------------------------------------------------------------
 *
 * Name:        dlq_client_cleanup
 *
 * Purpose:     Client application has disappeared.
 *		i.e. The TCP connection has been broken.
 *
 * Inputs:	client		- Client application instance.
 *
 * Outputs:	Request is appended to queue for processing by
 *		the data link state machine.
 *
 * Description:	Notify the link state machine that given client has gone away.
 *		Clean up all information related to that client application.
 *
 *--------------------------------------------------------------------*/

func dlq_client_cleanup(client C.int) {
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq_client_cleanup (...)\n");
	#endif
	*/

	// Assert (client >= 0 && client < MAX_NET_CLIENTS);

	/* Allocate a new queue item. */

	var pnew = new(C.struct_dlq_item_s)
	s_new_count++

	// All we care about is the client number.

	pnew._type = C.DLQ_CLIENT_CLEANUP
	pnew.client = client

	/* Put it into queue. */

	append_to_queue(pnew)

} /* end dlq_client_cleanup */

/*-------------------------------------------------------------------
 *
 * Name:        dlq_wait_while_empty
 *
 * Purpose:     Sleep while the received data queue is empty rather than
 *		polling periodically.
 *
 * Inputs:	timeout		- Return at this time even if queue is empty.
 *				  Zero for no timeout.
 *
 * Returns:	True if timed out before any event arrived.
 *
 * Description:	In version 1.4, we add timeout option so we can continue after
 *		some amount of time even if no events are in the queue.
 *
 *--------------------------------------------------------------------*/

func dlq_wait_while_empty(timeout C.double) C.int {
	var timed_out_result C.int = 0

	/* TODO KG
	#if DEBUG1
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq_wait_while_empty (%.3f)\n", timeout);
	#endif
	*/

	if !was_init {
		dlq_init()
	}

	if dlq_queue_head == nil {

		/* TODO KG
		#if DEBUG
			  text_color_set(DW_COLOR_DEBUG);
			  dw_printf ("dlq_wait_while_empty (): prepare to SLEEP...\n");
		#endif
		*/

		recv_thread_is_waiting = true
		if timeout != 0.0 {
			var timeoutAt = time.Unix(int64(timeout), 0) // TODO KG I suspect we were dropping ns when passing in :s
			var waitFor = time.Until(timeoutAt)

			// KG: pthread_cond_timedwait in Go...
			select {
			case <-dlq_wake_up_chan:
				// Signalled
			case <-time.After(waitFor):
				timed_out_result = 1
			}
		} else {
			<-dlq_wake_up_chan
		}
		recv_thread_is_waiting = false
	}

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq_wait_while_empty () returns timedout=%d\n", timed_out_result);
	#endif
	*/
	return (timed_out_result)

} /* end dlq_wait_while_empty */

/*-------------------------------------------------------------------
 *
 * Name:        dlq_remove
 *
 * Purpose:     Remove an item from the head of the queue.
 *
 * Inputs:	None.
 *
 * Returns:	Pointer to a queue item.  Caller is responsible for deleting it.
 *		nil if queue is empty.
 *
 *--------------------------------------------------------------------*/

func dlq_remove() *C.struct_dlq_item_s {

	/* TODO KG
	#if DEBUG1
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq_remove() enter critical section\n");
	#endif
	*/

	if !was_init {
		dlq_init()
	}

	dlq_mutex.Lock()

	var result *C.struct_dlq_item_s
	if dlq_queue_head != nil {
		result = dlq_queue_head
		dlq_queue_head = dlq_queue_head.nextp
	}

	dlq_mutex.Unlock()

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq_remove()  returns \n");
	#endif
	*/

	/* TODO KG
	   #if AX25MEMDEBUG

	   	if (ax25memdebug_get() && result != nil) {
	   	  text_color_set(DW_COLOR_DEBUG);
	   	  if (result.pp != nil) {
	   // TODO: mnemonics for type.
	   	    dw_printf ("dlq_remove (type=%d, chan=%d.%d, seq=%d, ...)\n", result._type, result.channel, result.subchannel, ax25memdebug_seq(result.pp));
	   	  } else {
	   	    dw_printf ("dlq_remove (type=%d, chan=%d, ...)\n", result._type, result.channel);
	   	  }
	   	}
	   #endif
	*/

	return (result)
}

/*-------------------------------------------------------------------
 *
 * Name:        dlq_delete
 *
 * Purpose:     Release storage used by a queue item.
 *
 * Inputs:	pitem		- Pointer to a queue item.
 *
 *--------------------------------------------------------------------*/

func dlq_delete(pitem *C.struct_dlq_item_s) {
	if pitem == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("INTERNAL ERROR: dlq_delete()  given nil pointer.\n")
		return
	}

	s_delete_count++

	if pitem.pp != nil {
		C.ax25_delete(pitem.pp)
		pitem.pp = nil
	}

	if pitem.txdata != nil {
		cdata_delete(pitem.txdata)
		pitem.txdata = nil
	}
} /* end dlq_delete */

/*-------------------------------------------------------------------
 *
 * Name:        cdata_new
 *
 * Purpose:     Allocate blocks of data for sending and receiving connected data.
 *
 * Inputs:	pid	- protocol id.
 *		data	- pointer to data.  Can be nil for segment reassembler.
 *		len	- length of data.
 *
 * Returns:	Structure with a copy of the data.
 *
 * Description:	The flow goes like this:
 *
 *		Client application establishes a connection with another station.
 *		Client application calls "dlq_xmit_data_request."
 *		A copy of the data is made with this function and attached to the queue item.
 *		The txdata block is attached to the appropriate link state machine.
 *		At the proper time, it is transmitted in an I frame.
 *		It needs to be kept around in case it needs to be retransmitted.
 *		When no longer needed, it is freed with cdata_delete.
 *
 *--------------------------------------------------------------------*/

func cdata_new(pid C.int, data *C.char, length C.int) *C.cdata_t {

	s_cdata_new_count++

	/* Round up the size to the next 128 bytes. */
	/* The theory is that a smaller number of unique sizes might be */
	/* beneficial for memory fragmentation and garbage collection. */

	var size = (length + 127) & ^0x7f

	var cdata = (*C.cdata_t)(C.malloc(C.size_t(C.sizeof_cdata_t + size)))

	cdata.magic = C.TXDATA_MAGIC
	cdata.next = nil
	cdata.pid = pid
	cdata.size = size
	cdata.len = length

	Assert(length >= 0 && length <= size)
	if data == nil {
		C.memset(unsafe.Pointer(&cdata.data), C.int('?'), C.size_t(size))
	} else {
		C.memcpy(unsafe.Pointer(&cdata.data), unsafe.Pointer(data), C.size_t(length))
	}
	return (cdata)

} /* end cdata_new */

/*-------------------------------------------------------------------
 *
 * Name:        cdata_delete
 *
 * Purpose:     Release storage used by a connected data block.
 *
 * Inputs:	cdata		- Pointer to a data block.
 *
 *--------------------------------------------------------------------*/

func cdata_delete(cdata *C.cdata_t) {
	if cdata == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("INTERNAL ERROR: cdata_delete()  given nil pointer.\n")
		return
	}

	if cdata.magic != C.TXDATA_MAGIC {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("INTERNAL ERROR: cdata_delete()  given corrupted data.\n")
		return
	}

	s_cdata_delete_count++

	cdata.magic = 0

} /* end cdata_delete */

/*-------------------------------------------------------------------
 *
 * Name:        cdata_check_leak
 *
 * Purpose:     Check for memory leak of cdata items.
 *
 * Description:	This is called when we expect no outstanding allocations.
 *
 *--------------------------------------------------------------------*/

func cdata_check_leak() {
	if s_cdata_delete_count != s_cdata_new_count {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal Error, cdata_check_leak, new=%d, delete=%d\n", s_cdata_new_count, s_cdata_delete_count)
	}

} /* end cdata_check_leak */

/* end dlq.c */
