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
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/sirupsen/logrus"
)

// nettncReattachDelay is how long a NetTNC waits between attempts to
// reattach to a TNC that has gone away, unless it is told otherwise before
// it is started.
const nettncReattachDelay = 5 * time.Second

// nettncWriteTimeout is how long sending a packet to a NetTNC waits for a TNC
// that has stopped reading before giving up on it.  Packets for an NCHANNEL
// skip the transmit queue, so the wait is on whoever is transmitting.
const nettncWriteTimeout = 10 * time.Second

type NetTNC struct {
	channel       int // NCHANNEL channel number frames from the TNC are received on.
	host          string
	port          int
	mu            sync.Mutex    // Guards sock, since listenThread and sendPacket access it from different goroutines.
	sock          net.Conn      // Socket handle or file descriptor. nil for invalid.
	reattachDelay time.Duration // Between attempts to reattach.
	writeTimeout  time.Duration // How long sending a packet waits on the TNC before giving up on it.
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
 *		pa              - Radio configuration, giving each channel's
 *				  medium and, for NCHANNEL, its host and port.
 *
 *		recFrame        - Where frames from the TNCs go; nil to drop them.
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
			var log = logrus.WithFields(logrus.Fields{
				"channel": i,
				"host":    pa.nettnc_addr[i],
				"port":    pa.nettnc_port[i],
			})

			log.Info("Network TNC")

			var nt, err = NewNetTNC(ctx, i, pa.nettnc_addr[i], pa.nettnc_port[i], recFrame)
			if err != nil {
				// A stop that cut the connection short is not a failure to
				// connect: go back and let the caller tear down.
				if ctx.Err() != nil {
					return tncs
				}

				log.WithError(err).Error("Could not attach to network TNC")
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
 * Inputs:	ctx	- Abandons connecting when cancelled.
 *
 *		channel	- channel number from NCHANNEL configuration.
 *
 *		host	- Host name or IP address.  Often "localhost".
 *
 *		port	- TCP port number.  Typically 8001.
 *
 *		recFrame - Where frames from the TNC go; nil to drop them.
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
	nt.writeTimeout = nettncWriteTimeout
	nt.recFrame = recFrame
	nt.setSock(conn)

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
 * Inputs:	ctx	- Stops listening, and hangs up, when cancelled.
 *		channel	- Channel number.
 *		nt.host, nt.port	- Host & port for re-connection.
 *
 * Outputs:	nt.sock - Socket for communicating with TNC.
 *				  Will be nil if not connected, and is
 *				  closed and cleared on return.
 *
 *--------------------------------------------------------------------*/

func (nt *NetTNC) listenThread(ctx context.Context, channel int) {
	dwutil.Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	var kstate = new(kiss.Collector) // State machine to gather a KISS frame.

	defer nt.closeSock()

	for ctx.Err() == nil {
		/*
		 * Re-attach to TNC if not currently attached.
		 */
		var conn = nt.getSock()
		if conn == nil {
			// I'm using the term "attach" here, in an attempt to
			// avoid confusion with the AX.25 connect.
			logrus.WithFields(logrus.Fields{
				"channel": channel,
				"host":    nt.host,
				"port":    nt.port,
			}).Info("Attempting to reattach to network TNC")

			var newConn, connErr = new(net.Dialer).DialContext(ctx, "tcp", net.JoinHostPort(nt.host, strconv.Itoa(nt.port)))
			if connErr == nil {
				nt.setSock(newConn)

				// Whatever was left of a frame when the last connection went
				// is gone for good; don't prepend it to this one's first.
				kstate = new(kiss.Collector) // Start with clean state.

				logrus.WithFields(logrus.Fields{
					"channel": channel,
					"host":    nt.host,
					"port":    nt.port,
				}).Info("Successfully reattached to network TNC")
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
				logrus.WithError(readErr).WithFields(logrus.Fields{
					"channel": channel,
					"host":    nt.host,
					"port":    nt.port,
				}).Error("Lost communication with network TNC; will try to reattach")
				nt.closeSockIfCurrent(conn)

				if !dwutil.SleepCtx(ctx, nt.reattachDelay) {
					return
				}

				continue
			}

			for j := range n {
				// Separate the byte stream into KISS frame(s) and make it
				// look like this came from a radio channel.
				tncRecByte(kstate, buf[j], nt.debug, channel, netTNC(), nt.recFrame)
			}
		} // nt.sock != nil
	} // until cancelled
}

// kissTNC says which sort of external KISS TNC tncRecByte is reading from.
type kissTNC struct {
	source   dwcore.SourceKind // What its frames are received as coming from.
	name     string            // As messages name it.
	spectrum string            // As the received queue shows it.
}

// netTNC describes a KISS TNC reached over TCP, for NCHANNEL.
func netTNC() kissTNC {
	return kissTNC{source: dwcore.SourceNetTNC, name: "network TNC", spectrum: "Network TNC"}
}

// tncRecByte takes one byte from an external KISS TNC, and passes each frame
// it completes to recFrame - as though it came from a radio channel, the one
// the TNC is attached to, whatever channel the frame itself names.
// Anything outside a frame is not ours to answer, so it's ignored.  With no
// recFrame, frames are dropped.
func tncRecByte(kc *kiss.Collector, b byte, debug int, channel int, tnc kissTNC, recFrame frameReceiver) {
	var chunk = kc.Add(b)

	if chunk.Err != nil {
		logrus.WithFields(logrus.Fields{"channel": channel, "tnc": tnc.name}).Error("KISS frame exceeded maximum length; discarding it")

		return
	}

	if chunk.Frame == nil {
		return
	}

	if debug > 0 {
		/* As received over the wire from the TNC. */
		// May include escaped characters.  What about FEND?
		// FIXME: make it say which TNC.
		kiss_debug_print(FROM_CLIENT, "", chunk.Frame)
	}

	var unwrapped = kiss.Unwrap(chunk.Frame)

	if len(unwrapped) == 0 {
		// FEND FESC FEND, say: no type byte, so nothing to pass on.
		logrus.WithFields(logrus.Fields{"channel": channel, "tnc": tnc.name}).Error("KISS frame has nothing in it once unescaped; ignoring it")

		return
	}

	if debug >= 2 {
		/* Append CRC to this and it goes out over the radio. */
		var entry = logrus.WithField("channel", channel)

		entry.Debug("Frame content after removing KISS framing and any escapes")
		/* Don't include the "type" indicator. */
		/* It contains the radio channel and type should always be 0 here. */
		dwutil.LogHexDump(entry, logrus.DebugLevel, unwrapped[1:])
	}

	// Convert to packet object and send to received packet queue.
	// Note that we use channel associated with the TNC, not channel in KISS frame.

	var alevel ax25.ALevel
	var pp = ax25.FromFrame(unwrapped[1:], alevel)

	if pp != nil {
		if recFrame != nil {
			recFrame(dwcore.ChannelSource(tnc.source, channel), pp, dwcore.RxInfo{ALevel: alevel, Retries: RETRY_NONE, FEC: fec_type_none}, tnc.spectrum)
		}
	} else {
		logrus.WithFields(logrus.Fields{"channel": channel, "tnc": tnc.name}).Error("Failed to create packet object for KISS frame")
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
 *
 * Outputs:	Packet is converted to KISS and sent to network TNC.
 *		On a write error, the connection is closed, for
 *		listenThread to reattach.
 *
 * Returns:	Whether the packet was sent.
 *
 *-----------------------------------------------------------------*/

func (nt *NetTNC) sendPacket(channel int, pp *ax25.Packet) bool {
	if nt == nil {
		logrus.WithField("channel", channel).Error("Not connected to network TNC; discarding packet")

		return false
	}

	var conn = nt.getSock()
	if conn == nil {
		logrus.WithField("channel", channel).Error("Not connected to network TNC; discarding packet")

		return false
	}

	// First, get the on-air frame format from packet object.
	// Prepend 0 byte for KISS command and channel.
	var fbuf = pp.FrameData()

	var frame_buff = []byte{0} // For now, set channel to 0.
	frame_buff = append(frame_buff, fbuf...)

	// Next, encapsulate into KISS frame with surrounding FENDs and any escapes.

	var kiss_buff = kiss.Encapsulate(frame_buff)

	// We're on the transmitting caller's goroutine, so a TNC that has stopped
	// reading mustn't hold it up for ever: once the deadline passes, the
	// write fails, and the connection is given up like any other that fails.
	var err = conn.SetWriteDeadline(time.Now().Add(nt.writeTimeout))
	if err == nil {
		_, err = conn.Write(kiss_buff)
	}

	if err != nil {
		logrus.WithError(err).WithField("channel", channel).Error("Error sending packet to KISS network TNC; closing connection")
		nt.closeSockIfCurrent(conn)

		return false
	}

	return true
} /* end sendPacket */
