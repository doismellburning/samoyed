//nolint:gochecknoglobals
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

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

/* A transmit or receive data block for connected mode. */

const TXDATA_MAGIC = 0x09110911

type cdata_t struct {
	magic int /* For integrity checking. */

	next *cdata_t /* Pointer to next when part of a list. */

	pid int /* Protocol id. */

	len int /* Number of bytes actually used. */

	data []byte /* Variable length data. */
}

/* Types of things that can be in queue. */

type dlq_type_t int

const (
	DLQ_REC_FRAME dlq_type_t = iota
	DLQ_CONNECT_REQUEST
	DLQ_DISCONNECT_REQUEST
	DLQ_XMIT_DATA_REQUEST
	DLQ_REGISTER_CALLSIGN
	DLQ_UNREGISTER_CALLSIGN
	DLQ_OUTSTANDING_FRAMES_REQUEST
	DLQ_CHANNEL_BUSY
	DLQ_SEIZE_CONFIRM
	DLQ_CLIENT_CLEANUP
)

type fec_type_t int

const (
	fec_type_none fec_type_t = 0
	fec_type_fx25 fec_type_t = 1
	fec_type_il2p fec_type_t = 2
)

/* A queue item. */

// TODO: call this event rather than item.
// TODO: should add fences.

type dlq_item_t struct {
	nextp *dlq_item_t /* Next item in queue. */

	_type dlq_type_t /* Type of item. */
	/* See enum definition above. */

	_chan int /* Radio channel of origin. */

	// I'm not worried about amount of memory used but this might be a
	// little clearer if a union was used for the different event types.

	// Used for received frame.

	subchan int /* Winning "subchannel" when using multiple */
	/* decoders on one channel.  */
	/* Special case, -1 means DTMF decoder. */
	/* Maybe we should have a different type in this case? */

	slice int /* Winning slicer. */

	pp *packet_t /* Pointer to frame structure. */

	alevel ALevel /* Audio level. */

	fec_type fec_type_t // Type of FEC for received signal: none, FX.25, or IL2P.

	retries BitFixLevel /* Effort expended to get a valid CRC. */
	/* Bits changed for regular AX.25. */
	/* Number of bytes fixed for FX.25. */

	spectrum string /* "Spectrum" display for multi-decoders. */

	// Used by requests from a client application, connect, etc.

	addrs [AX25_MAX_ADDRS]string

	num_addr int /* Range 2 .. 10. */

	client int

	// Used only by client request to transmit connected data.

	txdata *cdata_t

	// Used for channel activity change.
	// It is useful to know when the channel is busy either for carrier detect
	// or when we are transmitting.

	activity int /* OCTYPE_PTT for my transmission start/end. */
	/* OCTYPE_DCD if we hear someone else. */

	status int /* 1 for active or 0 for quiet. */

}

// DataLinkQueue collects the events the receive processing thread acts on:
// frames heard on every channel, requests from client applications, and
// notifications from the transmit side, so that they are handled serially
// and drive the data link state machine.
type DataLinkQueue struct {
	mu sync.Mutex /* Critical section for updating queues. */

	head *dlq_item_t /* Head of linked list for queue. */

	// wake notifies the receive processing thread when the queue is not
	// empty.  Buffered, and only ever written to with a non-blocking send,
	// so that a sender is never left holding a wake-up nobody is going to
	// take: the receive thread may have stopped waiting (woken by an
	// earlier item, or its timeout fired) between being sent one and the
	// next sender looking.
	//
	// Made once, in NewDataLinkQueue, and never replaced: a waiter
	// selecting on an old channel would never hear a send to its
	// replacement.
	wake chan struct{}

	// The leak counters are atomic because nothing else serialises them:
	// items are made by every receive thread, the AGW server's client
	// goroutines, the beacon and the IGate, and deleted by the receive
	// processing thread, while connected-mode data blocks are made by the
	// AGW server and freed by the link state machine.

	newCount    atomic.Int64 /* To detect memory leak for queue items. */
	deleteCount atomic.Int64 // TODO:  need to test.

	cdataNewCount    atomic.Int64 /* To detect memory leak for connected mode data. */
	cdataDeleteCount atomic.Int64 // TODO:  need to test.
}

// dataLinkQueue is the queue the receive threads, client applications and
// transmit side hand their events to, and recv_process takes them from.  It
// exists from package initialisation, so it is never nil and is usable
// before Init is called.
var dataLinkQueue = NewDataLinkQueue()

// NewDataLinkQueue returns an empty queue.
func NewDataLinkQueue() *DataLinkQueue {
	var q = new(DataLinkQueue)

	q.wake = make(chan struct{}, 1)

	return q
}

/*-------------------------------------------------------------------
 *
 * Name:        Init
 *
 * Purpose:     Initialize the queue.
 *
 * Inputs:	None.
 *
 * Outputs:
 *
 * Description:	Empty the queue and discard any wake-up left over from
 *		items that were on it.
 *
 *--------------------------------------------------------------------*/

func (q *DataLinkQueue) Init() {
	logrus.Debug("dlq_init")
	q.mu.Lock()
	defer q.mu.Unlock()

	q.head = nil

	q.discardWakeUpLocked()
} /* end Init */

/*-------------------------------------------------------------------
 *
 * Name:        RecFrame
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
 *		giving it to RecFrame.
 *
 *--------------------------------------------------------------------*/

func (q *DataLinkQueue) RecFrame(channel int, subchannel int, slice int, pp *packet_t, alevel ALevel, fec_type fec_type_t, retries BitFixLevel, spectrum string) {
	logrus.WithField("channel", channel).Debug("dlq_rec_frame")
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

	var pnew = new(dlq_item_t)
	var new_count = q.newCount.Add(1)
	var delete_count = q.deleteCount.Load()

	if new_count > delete_count+50 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("INTERNAL ERROR:  DLQ memory leak, new=%d, delete=%d\n", new_count, delete_count)
	}

	pnew.nextp = nil
	pnew._type = DLQ_REC_FRAME
	pnew._chan = channel
	pnew.slice = slice
	pnew.subchan = subchannel
	pnew.pp = pp
	pnew.alevel = alevel
	pnew.fec_type = fec_type
	pnew.retries = retries
	pnew.spectrum = spectrum

	/* Put it into queue. */

	q.appendItem(pnew)
} /* end RecFrame */

/*-------------------------------------------------------------------
 *
 * Name:        ConnectRequest
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

func (q *DataLinkQueue) ConnectRequest(addrs [AX25_MAX_ADDRS]string, num_addr int, channel int, client int, pid int) {
	logrus.WithFields(logrus.Fields{
		"channel": channel,
		"client":  client,
	}).Debug("dlq_connect_request")
	Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	/* Allocate a new queue item. */

	var pnew = new(dlq_item_t)
	q.newCount.Add(1)

	pnew._type = DLQ_CONNECT_REQUEST
	pnew._chan = channel
	pnew.addrs = addrs
	pnew.num_addr = num_addr
	pnew.client = client

	/* Put it into queue. */

	q.appendItem(pnew)
} /* end ConnectRequest */

/*-------------------------------------------------------------------
 *
 * Name:        DisconnectRequest
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

func (q *DataLinkQueue) DisconnectRequest(addrs [AX25_MAX_ADDRS]string, num_addr int, channel int, client int) {
	logrus.WithFields(logrus.Fields{
		"channel": channel,
		"client":  client,
	}).Debug("dlq_disconnect_request")
	Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	/* Allocate a new queue item. */

	var pnew = new(dlq_item_t)
	q.newCount.Add(1)

	pnew._type = DLQ_DISCONNECT_REQUEST
	pnew._chan = channel
	pnew.addrs = addrs
	pnew.num_addr = num_addr
	pnew.client = client

	/* Put it into queue. */

	q.appendItem(pnew)
} /* end DisconnectRequest */

/*-------------------------------------------------------------------
 *
 * Name:        OutstandingFramesRequest
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

func (q *DataLinkQueue) OutstandingFramesRequest(addrs [AX25_MAX_ADDRS]string, num_addr int, channel int, client int) {
	logrus.WithFields(logrus.Fields{
		"channel": channel,
		"client":  client,
	}).Debug("dlq_outstanding_frames_request")
	Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	/* Allocate a new queue item. */

	var pnew = new(dlq_item_t)
	q.newCount.Add(1)

	pnew._type = DLQ_OUTSTANDING_FRAMES_REQUEST
	pnew._chan = channel
	pnew.addrs = addrs
	pnew.num_addr = num_addr
	pnew.client = client

	/* Put it into queue. */

	q.appendItem(pnew)
} /* end OutstandingFramesRequest */

/*-------------------------------------------------------------------
 *
 * Name:        XmitDataRequest
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

func (q *DataLinkQueue) XmitDataRequest(addrs [AX25_MAX_ADDRS]string, num_addr int, channel int, client int, pid int, xdata []byte) {
	logrus.WithFields(logrus.Fields{
		"channel": channel,
		"client":  client,
		"pid":     pid,
	}).Debug("dlq_xmit_data_request")
	Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	/* Allocate a new queue item. */

	var pnew = new(dlq_item_t)
	q.newCount.Add(1)

	pnew._type = DLQ_XMIT_DATA_REQUEST
	pnew._chan = channel
	pnew.addrs = addrs
	pnew.num_addr = num_addr
	pnew.client = client

	/* Attach the transmit data. */

	pnew.txdata = q.NewCData(pid, xdata)

	/* Put it into queue. */

	q.appendItem(pnew)
} /* end XmitDataRequest */

/*-------------------------------------------------------------------
 *
 * Name:        RegisterCallsign
 *		UnregisterCallsign
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

func (q *DataLinkQueue) RegisterCallsign(addr string, channel int, client int) {
	logrus.WithFields(logrus.Fields{
		"addr":    addr,
		"channel": channel,
		"client":  client,
	}).Debug("dlq_register_callsign")
	Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	/* Allocate a new queue item. */

	var pnew = new(dlq_item_t)
	q.newCount.Add(1)

	pnew._type = DLQ_REGISTER_CALLSIGN
	pnew._chan = channel
	pnew.addrs[0] = addr
	pnew.num_addr = 1
	pnew.client = client

	/* Put it into queue. */

	q.appendItem(pnew)
} /* end RegisterCallsign */

func (q *DataLinkQueue) UnregisterCallsign(addr string, channel int, client int) {
	logrus.WithFields(logrus.Fields{
		"addr":    addr,
		"channel": channel,
		"client":  client,
	}).Debug("dlq_unregister_callsign")
	Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	/* Allocate a new queue item. */

	var pnew = new(dlq_item_t)
	q.newCount.Add(1)

	pnew._type = DLQ_UNREGISTER_CALLSIGN
	pnew._chan = channel
	pnew.addrs[0] = addr
	pnew.num_addr = 1
	pnew.client = client

	/* Put it into queue. */

	q.appendItem(pnew)
} /* end UnregisterCallsign */

/*-------------------------------------------------------------------
 *
 * Name:        ChannelBusy
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

func (q *DataLinkQueue) ChannelBusy(channel int, activity int, status int) {
	if activity == OCTYPE_PTT || activity == OCTYPE_DCD {
		logrus.WithFields(logrus.Fields{
			"channel":  channel,
			"activity": activity,
			"status":   status,
		}).Debug("dlq_channel_busy")

		/* Allocate a new queue item. */
		var pnew = new(dlq_item_t)
		q.newCount.Add(1)

		pnew._type = DLQ_CHANNEL_BUSY
		pnew._chan = channel
		pnew.activity = activity
		pnew.status = status

		/* Put it into queue. */

		q.appendItem(pnew)
	}
} /* end ChannelBusy */

/*-------------------------------------------------------------------
 *
 * Name:        SeizeConfirm
 *
 * Purpose:     Inform data link state machine that the transmitter is on.
 *		This is in response to TransmitQueue.LMSeizeRequest.
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

func (q *DataLinkQueue) SeizeConfirm(channel int) {
	logrus.WithField("channel", channel).Debug("dlq_seize_confirm")

	/* Allocate a new queue item. */
	var pnew = new(dlq_item_t)
	q.newCount.Add(1)

	pnew._type = DLQ_SEIZE_CONFIRM
	pnew._chan = channel

	/* Put it into queue. */

	q.appendItem(pnew)
} /* end SeizeConfirm */

/*-------------------------------------------------------------------
 *
 * Name:        ClientCleanup
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

func (q *DataLinkQueue) ClientCleanup(client int) {
	logrus.WithField("client", client).Debug("dlq_client_cleanup")

	// Assert (client >= 0 && client < MAX_NET_CLIENTS);

	/* Allocate a new queue item. */
	var pnew = new(dlq_item_t)
	q.newCount.Add(1)

	// All we care about is the client number.

	pnew._type = DLQ_CLIENT_CLEANUP
	pnew.client = client

	/* Put it into queue. */

	q.appendItem(pnew)
} /* end ClientCleanup */

/*-------------------------------------------------------------------
 *
 * Name:        WaitWhileEmpty
 *
 * Purpose:     Sleep while the received data queue is empty rather than
 *		polling periodically.
 *
 * Inputs:	ctx		- Return when this is cancelled, whatever the
 *				  state of the queue.  The caller is expected to
 *				  check it and stop rather than look for an item
 *				  that isn't there.
 *
 *		timeout		- Return at this time even if queue is empty.
 *				  Zero for no timeout.
 *
 * Returns:	True if timed out before any event arrived.
 *
 * Description:	In version 1.4, we add timeout option so we can continue after
 *		some amount of time even if no events are in the queue.
 *
 *--------------------------------------------------------------------*/

func (q *DataLinkQueue) WaitWhileEmpty(ctx context.Context, timeout time.Time) bool {
	var timed_out_result = false

	logrus.WithField("timeout", timeout).Trace("dlq_wait_while_empty")

	q.mu.Lock()

	var is_empty = q.head == nil
	if is_empty {
		// Anything in the channel now belongs to an item already taken
		// off the queue, so drop it rather than let it cut the wait
		// short.  Doing this under the lock, which appendItem also
		// holds while it sends, means we cannot discard a wake-up for an
		// item we have not seen.
		q.discardWakeUpLocked()
	}

	q.mu.Unlock()

	if is_empty {
		logrus.Trace("dlq_wait_while_empty: prepare to SLEEP...")
		if !timeout.IsZero() {
			// KG: pthread_cond_timedwait in Go...
			var timer = time.NewTimer(time.Until(timeout))
			defer timer.Stop()

			select {
			case <-q.wake:
				// Signalled
			case <-timer.C:
				timed_out_result = true
			case <-ctx.Done():
				// Shutting down.  Not a timeout: there is nothing for
				// the caller to do now except stop.
			}
		} else {
			select {
			case <-q.wake:
				// Signalled
			case <-ctx.Done():
				// Shutting down, as above.
			}
		}
	}

	logrus.WithField("timed_out", timed_out_result).Trace("dlq_wait_while_empty returns")

	return (timed_out_result)
} /* end WaitWhileEmpty */

/*-------------------------------------------------------------------
 *
 * Name:        Remove
 *
 * Purpose:     Remove an item from the head of the queue.
 *
 * Inputs:	None.
 *
 * Returns:	Pointer to a queue item.  Caller is responsible for deleting it.
 *		nil if queue is empty.
 *
 *--------------------------------------------------------------------*/

func (q *DataLinkQueue) Remove() *dlq_item_t {
	logrus.Trace("dlq_remove: enter critical section")
	q.mu.Lock()

	var result *dlq_item_t
	if q.head != nil {
		result = q.head
		q.head = q.head.nextp
	}

	q.mu.Unlock()

	logrus.Trace("dlq_remove returns")

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
 * Name:        Delete
 *
 * Purpose:     Release storage used by a queue item.
 *
 * Inputs:	pitem		- Pointer to a queue item.
 *
 *--------------------------------------------------------------------*/

func (q *DataLinkQueue) Delete(pitem *dlq_item_t) {
	if pitem == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("INTERNAL ERROR: dlq_delete()  given nil pointer.\n")

		return
	}

	q.deleteCount.Add(1)

	pitem.pp = nil

	if pitem.txdata != nil {
		q.DeleteCData(pitem.txdata)
		pitem.txdata = nil
	}
} /* end Delete */

/*-------------------------------------------------------------------
 *
 * Name:        NewCData
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
 *		Client application calls "XmitDataRequest."
 *		A copy of the data is made with this function and attached to the queue item.
 *		The txdata block is attached to the appropriate link state machine.
 *		At the proper time, it is transmitted in an I frame.
 *		It needs to be kept around in case it needs to be retransmitted.
 *		When no longer needed, it is freed with DeleteCData.
 *
 *--------------------------------------------------------------------*/

func (q *DataLinkQueue) NewCData(pid int, data []byte) *cdata_t {
	q.cdataNewCount.Add(1)

	var cdata = new(cdata_t)

	cdata.magic = TXDATA_MAGIC
	cdata.next = nil
	cdata.pid = pid
	cdata.len = len(data)

	if data != nil {
		cdata.data = make([]byte, len(data))
		copy(cdata.data, data)
	}

	return (cdata)
} /* end NewCData */

/*-------------------------------------------------------------------
 *
 * Name:        DeleteCData
 *
 * Purpose:     Release storage used by a connected data block.
 *
 * Inputs:	cdata		- Pointer to a data block.
 *
 *--------------------------------------------------------------------*/

func (q *DataLinkQueue) DeleteCData(cdata *cdata_t) {
	if cdata == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("INTERNAL ERROR: cdata_delete()  given nil pointer.\n")

		return
	}

	if cdata.magic != TXDATA_MAGIC {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("INTERNAL ERROR: cdata_delete()  given corrupted data.\n")

		return
	}

	q.cdataDeleteCount.Add(1)

	cdata.magic = 0
} /* end DeleteCData */

/*-------------------------------------------------------------------
 *
 * Name:        CheckCDataLeak
 *
 * Purpose:     Check for memory leak of cdata items.
 *
 * Description:	This is called when we expect no outstanding allocations.
 *
 *--------------------------------------------------------------------*/

func (q *DataLinkQueue) CheckCDataLeak() {
	var new_count = q.cdataNewCount.Load()
	var delete_count = q.cdataDeleteCount.Load()

	if delete_count != new_count {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal Error, cdata_check_leak, new=%d, delete=%d\n", new_count, delete_count)
	}
} /* end CheckCDataLeak */

/*-------------------------------------------------------------------
 *
 * Name:        appendItem
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

func (q *DataLinkQueue) appendItem(pnew *dlq_item_t) {
	pnew.nextp = nil

	/* TODO
	#if DEBUG1
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("dlq append_to_queue: enter critical section\n");
	#endif
	*/
	q.mu.Lock()

	var plast *dlq_item_t
	var queue_length int

	if q.head == nil {
		q.head = pnew
		queue_length = 1
	} else {
		queue_length = 2 /* head + new one */

		plast = q.head
		for plast.nextp != nil {
			plast = plast.nextp
			queue_length++
		}

		plast.nextp = pnew
	}

	// Wake the receive thread.  Whether it is actually waiting is not
	// ours to know, so the send must not block.  Doing it while still
	// holding the lock is what pairs it with the discard in
	// WaitWhileEmpty.

	select {
	case q.wake <- struct{}{}:
	default:
		// Either a wake-up is already pending, which will do for this
		// item too, or nobody is waiting and the next waiter will find
		// the item on the queue.
	}

	q.mu.Unlock()
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
} /* end appendItem */

// discardWakeUpLocked throws away a wake-up belonging to an item that has
// since been taken off the queue, so that it cannot cut short the next wait.
// Caller must hold mu, and must have found the queue empty: a sender adds its
// item before sending the wake-up, so with the lock held and nothing queued,
// any wake-up in the channel is stale.
func (q *DataLinkQueue) discardWakeUpLocked() {
	select {
	case <-q.wake:
	default:
	}
}

/* end dlq.c */
