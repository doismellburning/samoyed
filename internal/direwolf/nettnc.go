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
	"fmt"
	"net"
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

// nettncDialTimeout is how long a NetTNC waits for a TNC to answer an attempt
// to attach, rather than the minutes the operating system would otherwise
// give a host that silently drops what we send it.
const nettncDialTimeout = 10 * time.Second

type NetTNC struct {
	channel       int // NCHANNEL channel number frames from the TNC are received on.
	host          string
	port          int
	mu            sync.Mutex    // Guards sock, since listenThread and sendPacket access it from different goroutines.
	sock          net.Conn      // Socket handle or file descriptor. nil for invalid.
	reattachDelay time.Duration // Between attempts to reattach.
	writeTimeout  time.Duration // How long sending a packet waits on the TNC before giving up on it.
	dialTimeout   time.Duration // How long each attempt to reattach may take.
	started       atomic.Bool
	debug         int
	recFrame      frameReceiver // Where frames from the TNC go, or nil to drop them.

	// access is the channel access settings the TNC is given each time it is
	// attached: those from the configuration, as a KISS client has since
	// changed them.  Guarded by mu.
	access TNCChannelAccess

	// accessMu is held from reading access to having written what was read to
	// the TNC, so that the settings sent on reattaching can't overtake a
	// newer change a KISS client is sending at the same moment.
	accessMu sync.Mutex
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
 *		debug           - "-d n" count: 1 shows each KISS frame as it
 *				  arrives, 2 also what is left once unwrapped.
 *
 * Returns:	The TNC attached to each NCHANNEL channel, nil for every
 *		other channel, or the error if one cannot be reached; if
 *		cancelled part way, returns those attached so far.  The
 *		TNCs attached before an error keep listening until ctx is
 *		cancelled.
 *
 * Description:	Called once at direwolf application start up time.
 *		Calls NewNetTNC for each NCHANNEL configuration item.
 *
 *--------------------------------------------------------------------*/

func NewNetTNCs(ctx context.Context, pa *RadioConfig, recFrame frameReceiver, debug int) ([MAX_TOTAL_CHANS]*NetTNC, error) {
	var tncs [MAX_TOTAL_CHANS]*NetTNC

	for i := range MAX_TOTAL_CHANS {
		if pa.chan_medium[i] == MEDIUM_NETTNC {
			var log = logrus.WithFields(logrus.Fields{
				"channel": i,
				"host":    pa.nettnc_addr[i],
				"port":    pa.nettnc_port[i],
			})

			log.Info("Network TNC")

			var nt, err = NewNetTNC(ctx, i, pa.nettnc_addr[i], pa.nettnc_port[i], pa.tncChannelAccess[i], recFrame, debug)
			if err != nil {
				// A stop that cut the connection short is not a failure to
				// connect: go back and let the caller tear down.
				if ctx.Err() != nil {
					return tncs, nil //nolint:nilerr // A stop is not a failure to connect.
				}

				return tncs, fmt.Errorf("channel %d: %w", i, err)
			}

			nt.Start(ctx)

			tncs[i] = nt
		}
	}

	return tncs, nil
}

// dialNetTNC attaches to the network TNC at host and port, giving up once
// timeout has passed or ctx is cancelled, whichever comes first.
func dialNetTNC(ctx context.Context, host string, port int, timeout time.Duration) (net.Conn, error) {
	var dialer = new(net.Dialer)
	dialer.Timeout = timeout

	return dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
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
 *		access	- Channel access settings, sent to the TNC on
 *			  attaching, and again on each reattach.
 *
 *		recFrame - Where frames from the TNC go; nil to drop them.
 *
 *		debug	- "-d n" count, as for NewNetTNCs.
 *
 * Returns:	The attached TNC, or the error connecting to it or
 *		sending it its channel access settings.
 *
 * Description:	Nothing is read from the TNC until Start is called.
 *
 *--------------------------------------------------------------------*/

func NewNetTNC(ctx context.Context, channel int, host string, port int, access TNCChannelAccess, recFrame frameReceiver, debug int) (*NetTNC, error) {
	dwutil.Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	var conn, connErr = dialNetTNC(ctx, host, port, nettncDialTimeout)
	if connErr != nil {
		return nil, connErr
	}

	var nt = new(NetTNC)
	nt.channel = channel
	nt.host = host
	nt.port = port
	nt.reattachDelay = nettncReattachDelay
	nt.writeTimeout = nettncWriteTimeout
	nt.dialTimeout = nettncDialTimeout
	nt.recFrame = recFrame
	nt.debug = debug
	nt.access = access

	var attachErr = nt.attach(conn)
	if attachErr != nil {
		return nil, attachErr
	}

	return nt, nil
}

// writeWithDeadline writes b to conn, giving up on a TNC that hasn't taken it
// within writeTimeout.  We're on somebody else's goroutine - the transmitting
// caller's, or a KISS client's - so a TNC that has stopped reading mustn't hold
// it up for ever.
func (nt *NetTNC) writeWithDeadline(conn net.Conn, b []byte) error {
	var err = conn.SetWriteDeadline(time.Now().Add(nt.writeTimeout))
	if err == nil {
		_, err = conn.Write(b)
	}

	return err
}

// attach records conn as the connection to the TNC, and sends the TNC its
// channel access settings.  If they cannot be sent, the connection is closed,
// and the error returned.
func (nt *NetTNC) attach(conn net.Conn) error {
	nt.accessMu.Lock()
	defer nt.accessMu.Unlock()

	nt.mu.Lock()
	nt.sock = conn
	var access = nt.access
	nt.mu.Unlock()

	return nt.writeChannelAccess(conn, access)
}

// writeChannelAccess sends access's settings to the TNC on conn, with
// accessMu held.  If they cannot be sent, the connection is closed, and the
// error returned.
func (nt *NetTNC) writeChannelAccess(conn net.Conn, access TNCChannelAccess) error {
	var frames = access.kissFrames()
	if len(frames) == 0 {
		return nil
	}

	var log = logrus.WithFields(access.logFields()).WithFields(logrus.Fields{"channel": nt.channel, "host": nt.host, "port": nt.port})

	var err = nt.writeWithDeadline(conn, frames)
	if err != nil {
		log.WithError(err).Error("Error sending channel access settings to network TNC; closing connection")
		nt.closeSockIfCurrent(conn)

		return fmt.Errorf("sending channel access settings to network TNC: %w", err)
	}

	log.Info("Sent channel access settings to network TNC")

	return nil
}

// SetChannelAccess changes the TNC's channel access settings that are Just in
// update, sending them to it now if it is attached, and each time it is
// reattached.  A nil nt - a channel with no TNC - ignores it.
func (nt *NetTNC) SetChannelAccess(update TNCChannelAccess) {
	if nt == nil {
		return
	}

	nt.accessMu.Lock()
	defer nt.accessMu.Unlock()

	nt.mu.Lock()
	nt.access = nt.access.updatedBy(update)
	var conn = nt.sock
	nt.mu.Unlock()

	if conn == nil {
		logrus.WithFields(update.logFields()).WithField("channel", nt.channel).Info("Not attached to network TNC just now; will send it the channel access settings on reattaching")

		return
	}

	_ = nt.writeChannelAccess(conn, update) // Already logged.
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

			var newConn, connErr = dialNetTNC(ctx, nt.host, nt.port, nt.dialTimeout)
			if connErr == nil {
				// Whatever was left of a frame when the last connection went
				// is gone for good; don't prepend it to this one's first.
				kstate = new(kiss.Collector) // Start with clean state.

				logrus.WithFields(logrus.Fields{
					"channel": channel,
					"host":    nt.host,
					"port":    nt.port,
				}).Info("Successfully reattached to network TNC")

				// A TNC that took the connection and not the settings has
				// had it closed; give it a moment before trying again.
				if nt.attach(newConn) != nil && !dwutil.SleepCtx(ctx, nt.reattachDelay) {
					return
				}
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

// tncRecByte takes one byte from an external KISS TNC, and passes each data
// frame it completes to recFrame - as though it came from a radio channel, the
// one the TNC is attached to, whatever port the frame itself names.
// Any other command - a reply to SETHARDWARE, say - carries no received
// packet, so it's dropped.  Anything outside a frame is not ours to answer, so
// it's ignored.  With no recFrame, frames are dropped.
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
		// The dump below names the channel in the KISS frame, which the TNC
		// chooses, so say first which TNC, and which of our channels, it is.
		logrus.WithFields(logrus.Fields{"channel": channel, "tnc": tnc.name}).Debug("KISS frame from external TNC")
		kiss_debug_print(FROM_CLIENT, "", chunk.Frame)
	}

	var unwrapped = kiss.Unwrap(chunk.Frame)

	if len(unwrapped) == 0 {
		// FEND FESC FEND, say: no type byte, so nothing to pass on.
		logrus.WithFields(logrus.Fields{"channel": channel, "tnc": tnc.name}).Error("KISS frame has nothing in it once unescaped; ignoring it")

		return
	}

	// The lower nybble of the type byte is the command; the upper one, the
	// port, is not ours to go by.
	var command = unwrapped[0] & 0x0f
	if command != kiss.CmdDataFrame {
		logrus.WithFields(logrus.Fields{"channel": channel, "tnc": tnc.name, "command": command}).Debug("KISS frame from TNC is not a data frame; ignoring it")

		return
	}

	if debug >= 2 {
		/* Append CRC to this and it goes out over the radio. */
		var entry = logrus.WithField("channel", channel)

		entry.Debug("Frame content after removing KISS framing and any escapes")
		/* Don't include the "type" indicator. */
		/* It contains the port, and the command, which is always data here. */
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

	// Once the deadline passes, the write fails, and the connection is given
	// up like any other that fails.
	var err = nt.writeWithDeadline(conn, kiss_buff)
	if err != nil {
		logrus.WithError(err).WithField("channel", channel).Error("Error sending packet to KISS network TNC; closing connection")
		nt.closeSockIfCurrent(conn)

		return false
	}

	return true
} /* end sendPacket */
