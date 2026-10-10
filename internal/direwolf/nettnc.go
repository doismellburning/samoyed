package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Attach to Network KISS TNC(s) for NCHANNEL config file item(s).
 *
 * Description:	Called once at application start up.
 *
 *---------------------------------------------------------------*/

import (
	"context"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/sirupsen/logrus"
)

// nettncReattachDelay is how long a NetTNC waits between attempts to
// reattach to a TNC that has gone away, unless it is told otherwise before
// it is started.
const nettncReattachDelay = 5 * time.Second

type NetTNC struct {
	channel       int // NCHANNEL channel number frames from the TNC are received on.
	host          string
	port          int
	mu            sync.Mutex    // Guards sock, since listenThread and sendPacket access it from different goroutines.
	sock          net.Conn      // Socket handle or file descriptor. nil for invalid.
	reattachDelay time.Duration // Between attempts to reattach.
	started       atomic.Bool
	debug         int
	recFrame      frameReceiver // Where frames from the TNC go, or nil to drop them.
}

/*-------------------------------------------------------------------
 *
 * Name:        NewNetTNCs
 *
 * Purpose:      Attach to Network KISS TNC(s) for NCHANNEL config file item(s).
 *
 * Inputs:	ctx             - Stops the listening threads when cancelled.
 *
 *		pa              - Address of structure of type RadioConfig.
 *
 *		debug ? TBD
 *
 *
 * Returns:	The TNC attached to each NCHANNEL channel, nil for every
 *		other channel.  Exits if one cannot be reached; if
 *		cancelled part way, returns those attached so far.
 *
 * Description:	Called once at direwolf application start up time.
 *		Calls NewNetTNC for each NCHANNEL configuration item.
 *
 *--------------------------------------------------------------------*/

func NewNetTNCs(ctx context.Context, pa *RadioConfig, recFrame frameReceiver) [MAX_TOTAL_CHANS]*NetTNC {
	var tncs [MAX_TOTAL_CHANS]*NetTNC

	for i := range MAX_TOTAL_CHANS {
		if pa.chan_medium[i] == MEDIUM_NETTNC {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Channel %d: Network TNC %s %d\n", i, pa.nettnc_addr[i], pa.nettnc_port[i])

			var nt, err = NewNetTNC(ctx, i, pa.nettnc_addr[i], pa.nettnc_port[i], recFrame)
			if err != nil {
				// A stop that cut the connection short is not a failure to
				// connect: go back and let the caller tear down.
				if ctx.Err() != nil {
					return tncs
				}

				os.Exit(1)
			}

			nt.Start(ctx)

			tncs[i] = nt
		}
	}

	return tncs
}

/*-------------------------------------------------------------------
 *
 * Name:        NewNetTNC
 *
 * Purpose:      Attach to one Network KISS TNC.
 *
 * Inputs:	channel	- channel number from NCHANNEL configuration.
 *
 *		host	- Host name or IP address.  Often "localhost".
 *
 *		port	- TCP port number.  Typically 8001.
 *
 *		recFrame - Where frames from the TNC go; nil to drop them.
 *
 *		init_func - Call this function after establishing communication //
 *			with the TNC.  We put it here, so that it can be done//
 *			again automatically if the TNC disappears and we//
 *			reattach to it.//
 *			It must return 0 for success.//
 *			Can be nil if not needed.//
 *
 * Returns:	The attached TNC, or the error connecting to it.
 *
 * Description:	Nothing is read from the TNC until Start is called.
 *
 *--------------------------------------------------------------------*/

func NewNetTNC(ctx context.Context, channel int, host string, port int, recFrame frameReceiver) (*NetTNC, error) {
	dwutil.Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	var conn, connErr = new(net.Dialer).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if connErr != nil {
		return nil, connErr
	}

	var nt = new(NetTNC)
	nt.channel = channel
	nt.host = host
	nt.port = port
	nt.reattachDelay = nettncReattachDelay
	nt.recFrame = recFrame
	nt.setSock(conn)

	// TNC initialization if specified.

	//	if (s_tnc_init_func != nil) {
	//	  e = (*s_tnc_init_func)();
	//	  return (e);
	//	}

	return nt, nil
}

// Start reads frames from the TNC, and dispatches them to the received queue,
// until ctx is cancelled, when it hangs up.  If the TNC goes away, it tries to
// reattach to it.  Anything the listening goroutine reads must be set before
// calling it.
//
// Starting it again would have two goroutines reading the one connection, each
// getting part of what the TNC sends, so a second Start is complained about
// and ignored.
func (nt *NetTNC) Start(ctx context.Context) {
	if !nt.started.CompareAndSwap(false, true) {
		logrus.WithField("channel", nt.channel).Error("Network TNC started twice; ignoring the second start")

		return
	}

	go nt.listenThread(ctx, nt.channel)
}

// getSock returns the current connection, or nil if not connected.
func (nt *NetTNC) getSock() net.Conn {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	return nt.sock
}

// setSock records a newly established connection.
func (nt *NetTNC) setSock(conn net.Conn) {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	nt.sock = conn
}

// closeSock closes the current connection, if there is one, and forgets it.
// Nothing reads from a network TNC once its listening goroutine has stopped,
// so it is that goroutine's to hang up on however it comes to stop - including
// when a cancellation lands between a reconnect and the next read.
func (nt *NetTNC) closeSock() {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	if nt.sock == nil {
		return
	}

	nt.sock.Close()

	nt.sock = nil
}

// closeSockIfCurrent closes conn, and also clears sock if it still refers
// to conn - i.e. it hasn't already been replaced by a newer reattached
// connection from another goroutine.
func (nt *NetTNC) closeSockIfCurrent(conn net.Conn) {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	if nt.sock == conn {
		nt.sock = nil
	}

	conn.Close()
}

/*-------------------------------------------------------------------
 *
 * Name:        listenThread
 *
 * Purpose:     Listen for anything from TNC and process it.
 *		Reconnect if something goes wrong and we got disconnected.
 *
 * Inputs:	channel	- Channel number.
 *		nt.host, nt.port	- Host & port for re-connection.
 *
 * Outputs:	nt.sock - Socket for communicating with TNC.
 *				  Will be nil if not connected.
 *
 *--------------------------------------------------------------------*/

func (nt *NetTNC) listenThread(ctx context.Context, channel int) {
	dwutil.Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	var kstate kiss.Collector // State machine to gather a KISS frame.

	defer nt.closeSock()

	for ctx.Err() == nil {
		/*
		 * Re-attach to TNC if not currently attached.
		 */
		var conn = nt.getSock()
		if conn == nil {
			text_color_set(DW_COLOR_ERROR)
			// I'm using the term "attach" here, in an attempt to
			// avoid confusion with the AX.25 connect.
			dw_printf("Attempting to reattach to network TNC...\n")

			var newConn, connErr = new(net.Dialer).DialContext(ctx, "tcp", net.JoinHostPort(nt.host, strconv.Itoa(nt.port)))
			if connErr == nil {
				nt.setSock(newConn)

				dw_printf("Successfully reattached to network TNC.\n")
			} else if !dwutil.SleepCtx(ctx, nt.reattachDelay) {
				return
			}
		} else {
			const NETTNCBUFSIZ = 2048
			var buf = make([]byte, NETTNCBUFSIZ)

			// The read below blocks until the TNC says something, which
			// could be never, so closing the socket is the only thing that
			// gets this goroutine back when we are asked to stop.
			var stopClose = dwutil.CloseOnDone(ctx, conn)
			var n, readErr = conn.Read(buf)

			stopClose()

			if ctx.Err() != nil {
				return // The deferred closeSock hangs up on the way out.
			}

			if readErr != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Lost communication with network TNC. Will try to reattach.\n")
				nt.closeSockIfCurrent(conn)

				if !dwutil.SleepCtx(ctx, nt.reattachDelay) {
					return
				}

				continue
			}

			for j := range n {
				// Separate the byte stream into KISS frame(s) and make it
				// look like this came from a radio channel.
				nettncRecByte(&kstate, buf[j], nt.debug, channel, nt.recFrame)
			}
		} // nt.sock != nil
	} // until cancelled
}

// nettncRecByte takes one byte from a KISS network TNC, and passes each frame
// it completes to recFrame - as though it came from a radio channel, the one
// the network TNC is attached to, whatever channel the frame itself names.
// Anything outside a frame is not ours to answer, so it's ignored.  With no
// recFrame, frames are dropped.
func nettncRecByte(kc *kiss.Collector, b byte, debug int, channel int, recFrame frameReceiver) {
	var chunk = kc.Add(b)

	if chunk.Err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("KISS frame from network TNC exceeded maximum length.  Discarding it.\n")

		return
	}

	if chunk.Frame == nil {
		return
	}

	if debug > 0 {
		/* As received over the wire from network TNC. */
		// May include escapted characters.  What about FEND?
		// FIXME: make it say Network TNC.
		kiss_debug_print(FROM_CLIENT, "", chunk.Frame)
	}

	var unwrapped = kiss.Unwrap(chunk.Frame)

	if len(unwrapped) == 0 {
		// FEND FESC FEND, say: no type byte, so nothing to pass on.
		text_color_set(DW_COLOR_ERROR)
		dw_printf("KISS frame from network TNC has nothing in it once unescaped.  Ignoring it.\n")

		return
	}

	if debug >= 2 {
		/* Append CRC to this and it goes out over the radio. */
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("\n")
		dw_printf("Frame content after removing KISS framing and any escapes:\n")
		/* Don't include the "type" indicator. */
		/* It contains the radio channel and type should always be 0 here. */
		dwutil.HexDump(unwrapped[1:])
	}

	// Convert to packet object and send to received packet queue.
	// Note that we use channel associated with the network TNC, not channel in KISS frame.

	var subchan = -3
	var slice = 0
	var alevel ax25.ALevel
	var pp = ax25.FromFrame(unwrapped[1:], alevel)

	if pp != nil {
		var fec_type = fec_type_none
		var retries BitFixLevel

		var spectrum = "Network TNC"
		if recFrame != nil {
			recFrame(channel, subchan, slice, pp, alevel, fec_type, retries, spectrum)
		}
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Failed to create packet object for KISS frame from channel %d network TNC.\n", channel)
	}
}

/*-------------------------------------------------------------------
 *
 * Name:	sendPacket
 *
 * Purpose:	Send packet to a KISS network TNC.
 *
 * Inputs:	nt	- Network TNC, or nil if the channel has none.
 *		channel	- Channel number from NCHANNEL configuration.
 *		pp	- Packet object.
 *		b	- A byte from the input stream.
 *
 * Outputs:	Packet is converted to KISS and send to network TNC.
 *
 * Returns:	none.
 *
 * Description:	This does not free the packet object; caller is responsible.
 *
 *-----------------------------------------------------------------*/

func (nt *NetTNC) sendPacket(channel int, pp *ax25.Packet) {
	if nt == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Not connected to network TNC for channel %d. Discarding packet.\n", channel)

		return
	}

	var conn = nt.getSock()
	if conn == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Not connected to network TNC for channel %d. Discarding packet.\n", channel)

		return
	}

	// First, get the on-air frame format from packet object.
	// Prepend 0 byte for KISS command and channel.
	var fbuf = pp.FrameData()

	var frame_buff = []byte{0} // For now, set channel to 0.
	frame_buff = append(frame_buff, fbuf...)

	// Next, encapsulate into KISS frame with surrounding FENDs and any escapes.

	var kiss_buff = kiss.Encapsulate(frame_buff)

	var _, err = conn.Write(kiss_buff)
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("\nError %v sending packet to KISS Network TNC for channel %d.  Closing connection.\n\n", err, channel)
		nt.closeSockIfCurrent(conn)
	}

	// Do not free packet object;  caller will take care of it.
} /* end sendPacket */
