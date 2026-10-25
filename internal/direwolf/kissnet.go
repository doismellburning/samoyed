package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Provide service to other applications via KISS protocol via TCP socket.
 *
 * Input:
 *
 * Outputs:
 *
 * Description:	This provides a TCP socket for communication with a client application.
 *
 *		It implements the KISS TNS protocol as described in:
 *		http://www.ka9q.net/papers/kiss.html
 *
 * 		Briefly, a frame is composed of
 *
 *			* FEND (0xC0)
 *			* Contents - with special escape sequences so a 0xc0
 *				byte in the data is not taken as end of frame.
 *				as part of the data.
 *			* FEND
 *
 *		The first byte of the frame contains:
 *
 *			* port number in upper nybble.
 *			* command in lower nybble.
 *
 *
 *		Commands from application recognized:
 *
 *			_0	Data Frame	AX.25 frame in raw format.
 *
 *			_1	TXDELAY		See explanation in xmit.c.
 *
 *			_2	Persistence	"	"
 *
 *			_3 	SlotTime	"	"
 *
 *			_4	TXtail		"	"
 *					Spec says it is obsolete but Xastir
 *					sends it and we respect it.
 *
 *			_5	FullDuplex	Ignored.
 *
 *			_6	SetHardware	TNC specific.
 *
 *			FF	Return		Exit KISS mode.  Ignored.
 *
 *
 *		Messages sent to client application:
 *
 *			_0	Data Frame	Received AX.25 frame in raw format.
 *
 *
 *
 *
 * References:	Getting Started with Winsock
 *		http://msdn.microsoft.com/en-us/library/windows/desktop/bb530742(v=vs.85).aspx
 *
 * Future:	Originally we had:
 *			KISS over serial port.
 *			AGW over socket.
 *		This is the two of them munged together and we end up with duplicate code.
 *		It would have been better to separate out the transport and application layers.
 *		Maybe someday.
 *
 *---------------------------------------------------------------*/

/*
	Separate TCP ports per radio:

An increasing number of people are using multiple radios.
direwolf is capable of handling many radio channels and
provides cross-band repeating, etc.
Maybe a single stereo audio interface is used for 2 radios.

                   +------------+    tcp 8001, all channels
Radio A  --------  |            |  -------------------------- Application A
                   |  direwolf  |
Radio B  --------  |            |  -------------------------- Application B
                   +------------+    tcp 8001, all channels

The KISS protocol has a 4 bit field for the TNC port (which I prefer to
call channel because port has too many different meanings).
direwolf handles this fine.  However, most applications were written assuming
that a TNC could only talk to a single radio.  On reception, they ignore the
channel in the KISS frame.  For transmit, the channel is always set to 0.

Many people are using the work-around of two separate instances of direwolf.

                   +------------+    tcp 8001, KISS ch 0
Radio A  --------  |  direwolf  |  -------------------------- Application A
                   +------------+

                   +------------+    tcp 8002, KISS ch 0
Radio B  --------  |  direwolf  |  -------------------------- Application B
                   +------------+


Or they might be using a single application that knows how to talk to multiple
single port TNCs.  But they don't know how to multiplex multiple channels
thru a single KISS stream.

                   +------------+    tcp 8001, KISS ch 0
Radio A  --------  |  direwolf  |  ------------------------
                   +------------+                          \
                                                            -- Application
                   +------------+    tcp 8002, KISS ch 0   /
Radio B  --------  |  direwolf  |  ------------------------
                   +------------+

Using two different instances of direwolf means more complex configuration
and loss of cross-channel digipeating.  It is possible to use a stereo
audio interface but some ALSA magic is required to make it look like two
independent virtual mono interfaces.

In version 1.7, we add the capability of multiple KISS TCP ports, each for
a single radio channel.  e.g.

KISSPORT 8001 1
KISSPORT 8002 2

Now can use a single instance of direwolf.


                   +------------+    tcp 8001, KISS ch 0
Radio A  --------  |            |  -------------------------- Application A
                   |  direwolf  |
Radio B  --------  |            |  -------------------------- Application B
                   +------------+    tcp 8002, KISS ch 0

When receiving, the KISS channel is set to 0.
 - only radio channel 1 would be sent over tcp port 8001.
 - only radio channel 2 would be sent over tcp port 8001.

When transmitting, the KISS channel is ignored.
 - frames from tcp port 8001 are transmitted on radio channel 1.
 - frames from tcp port 8002 are transmitted on radio channel 2.

Of course, you could also use an application, capable of connecting to
multiple single radio TNCs.  Separate TCP ports actually go to the
same direwolf instance.

*/

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/sirupsen/logrus"
)

// kissnetPollInterval is how often a KissNetService checks again for a client
// to attach, or for a client slot to come free, unless it is told otherwise
// before it is started.
const kissnetPollInterval = time.Second

// KissNetService manages KISS protocol TCP socket connections.
// Each TCP port has its own status block in a linked list.
type KissNetService struct {
	miscConfigP  *misc_config_s
	handler      *KissHandler // Acts on what a client sends; set by Start.
	allPorts     *kissport_status_s
	debug        int           /* Print information flowing from and to client. */
	pollInterval time.Duration // For a client to attach, or a slot to come free.
	started      atomic.Bool
}

// KissNetAddr is an address for the KISS TCP service to listen on, and the
// radio channel that port carries (-1 for all).
type KissNetAddr struct {
	Addr    string
	Channel int
}

// kissNetAddrs is where mc says the KISS TCP service listens: every address
// on each port configured with KISSPORT, leaving out those of 0, which are
// disabled.
func kissNetAddrs(mc *misc_config_s) []KissNetAddr {
	var addrs []KissNetAddr

	for i := range MAX_KISS_TCP_PORTS {
		if mc.kiss_port[i] != 0 {
			addrs = append(addrs, KissNetAddr{Addr: fmt.Sprintf(":%d", mc.kiss_port[i]), Channel: mc.kiss_chan[i]})
		}
	}

	return addrs
}

/*-------------------------------------------------------------------
 *
 * Name:        NewKissNetService
 *
 * Purpose:     Set up a server to listen for connection requests from
 *		an application such as Xastir or APRSIS32.
 *		This is called once from the main program.
 *
 * Inputs:	mc		- Configuration, for KISSCOPY.
 *
 *		addrs		- Where to listen, and the radio channel each
 *				  carries.  None means no KISS over TCP.
 *
 *		debug		- Print information flowing from and to
 *				  clients.
 *
 * Returns:	The service, listening on each address it could bind,
 *		and an error naming any it could not.  One port that is
 *		already in use doesn't stop the rest, so the service is
 *		returned, and usable, either way.
 *
 * Description:	The ports are bound here, so a failure is known before
 *		anything starts, and Addrs says which ports were got;
 *		nobody is let in until Start is called.  ctx bounds only
 *		the binding: the ports are given up when the context
 *		Start was given is cancelled, or by Close if the service
 *		is never started.
 *
 *--------------------------------------------------------------------*/

func NewKissNetService(ctx context.Context, mc *misc_config_s, addrs []KissNetAddr, debug int) (*KissNetService, error) {
	var kns = new(KissNetService)
	kns.miscConfigP = mc
	kns.debug = debug
	kns.pollInterval = kissnetPollInterval

	var errs []error

	for _, addr := range addrs {
		logrus.WithField("address", addr.Addr).Debug("Binding to port")

		var listener, listenErr = new(net.ListenConfig).Listen(ctx, "tcp", addr.Addr)
		if listenErr != nil {
			errs = append(errs, fmt.Errorf("KISS TCP %s: %w", addr.Addr, listenErr))

			continue
		}

		var kps = new(kissport_status_s)

		kps.listener = listener
		kps.channel = addr.Channel

		kps.tcp_port = addrPort(listener.Addr())

		// Add to list.
		kps.pnext = kns.allPorts
		kns.allPorts = kps
	}

	return kns, errors.Join(errs...)
}

// Addrs is where the service is listening, in the order the addresses were
// given, leaving out any that could not be bound.
func (kns *KissNetService) Addrs() []net.Addr {
	var addrs []net.Addr

	for _, kps := range kns.ports() {
		if kps.listener != nil {
			addrs = append(addrs, kps.listener.Addr())
		}
	}

	return addrs
}

// ports is the service's ports in the order they were given.
func (kns *KissNetService) ports() []*kissport_status_s {
	var ports []*kissport_status_s
	for kps := kns.allPorts; kps != nil; kps = kps.pnext {
		ports = append(ports, kps)
	}

	slices.Reverse(ports) // The list is newest first.

	return ports
}

// kissNetAttached is a client attached to one of the KISS TCP ports.
type kissNetAttached struct {
	port    int
	channel int // The radio channel the port carries, -1 for all.
	addr    net.Addr
}

// attachedClients is each client attached to the service, port by port in the
// order the ports were given, or none for a nil service.
func (kns *KissNetService) attachedClients() []kissNetAttached {
	if kns == nil {
		return nil
	}

	var attached []kissNetAttached

	for _, kps := range kns.ports() {
		for _, addr := range kps.clientAddrs() {
			attached = append(attached, kissNetAttached{port: kps.tcp_port, channel: kps.channel, addr: addr})
		}
	}

	return attached
}

// Close gives up the ports of a service that was never started, which would
// otherwise hold them until the process exits, and stops it ever starting.  A
// started service gives its ports up when Start's context is cancelled, and
// Close leaves it be.
func (kns *KissNetService) Close() {
	if !kns.started.CompareAndSwap(false, true) {
		return
	}

	for kps := kns.allPorts; kps != nil; kps = kps.pnext {
		if kps.listener != nil {
			kps.listener.Close()
		}
	}
}

// Start accepts clients on each port until ctx is cancelled, handing what
// each client sends to handler.  For each port it
// starts goroutines to listen for a connection from a client application, and
// for commands from each client, so the caller doesn't block while we wait for
// these.  Anything the goroutines read, such as debug, must be set before
// calling it.
//
// Starting it again would have two goroutines accepting on every port, so a
// second Start, like one after Close, is complained about and ignored.
func (kns *KissNetService) Start(ctx context.Context, handler *KissHandler) {
	if !kns.started.CompareAndSwap(false, true) {
		logrus.Error("KISS TCP service started twice, or after Close; ignoring this start")

		return
	}

	// Before any goroutine that reads it exists.
	kns.handler = handler

	// The list is newest first; start them in the order they were configured.
	var ports []*kissport_status_s
	for kps := kns.allPorts; kps != nil; kps = kps.pnext {
		ports = append(ports, kps)
	}

	for _, kps := range slices.Backward(ports) {
		kns.initOne(ctx, kps)
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        SendRecPacket
 *
 * Purpose:     Send a packet, received over the radio, to the client apps.
 *
 * Inputs:	chan		- Channel number where packet was received.
 *			  0 = first, 1 = second if any.
 *
 *		kiss_cmd	- Usually kiss.CmdDataFrame.
 *
 *		frame		- Raw received frame, not including the FCS.
 *
 * Description:	Send message to every attached client whose port carries
 *		the channel.  An answer to one client's command goes to
 *		that client alone, through its kissNetClient, instead.
 *		Disconnect from client, and notify user, if any error.
 *
 *		Safe on a nil receiver, as the other transports' are.
 *
 *--------------------------------------------------------------------*/

func (kns *KissNetService) SendRecPacket(channel int, kiss_cmd int, frame []byte) {
	if kns == nil {
		return
	}

	for kps := kns.allPorts; kps != nil; kps = kps.pnext {
		for client := range MAX_NET_CLIENTS {
			kns.sendTo(kps, client, channel, kiss_cmd, frame)
		}
	}
} /* end SendRecPacket */

// sendTo sends frame, of type cmd for radio channel, to whichever client is
// attached in a slot, if any, and its port carries that channel.
func (kns *KissNetService) sendTo(kps *kissport_status_s, client int, channel int, cmd int, frame []byte) {
	var conn = kps.clientConn(client)
	if conn == nil {
		return
	}

	kns.sendOn(kps, client, conn, channel, cmd, frame)
}

// sendOn sends frame, of type cmd for radio channel, to the client attached in
// a slot over conn, if its port carries that channel.
func (kns *KissNetService) sendOn(kps *kissport_status_s, client int, conn net.Conn, channel int, cmd int, frame []byte) {
	// New in 1.7.
	// Previously all channels were sent to everyone.
	// We now have tcp ports which carry only a single radio channel.
	// The application will see KISS channel 0 regardless of the radio channel.

	var portChannel int

	if kps.channel == -1 { //nolint:staticcheck
		// Normal case, all channels.
		portChannel = channel
	} else if kps.channel == channel {
		// Single radio channel for this port.  Application sees 0.
		portChannel = 0
	} else {
		// Skip it.
		return
	}

	kns.write(kps, client, conn, kissClientFrame(portChannel, cmd, frame, kns.debug, "TCP"))
}

// write sends buf, already framed or the fake command prompt, to client
// over conn, and hangs up on the client if that fails.
func (kns *KissNetService) write(kps *kissport_status_s, client int, conn net.Conn, buf []byte) {
	var _, err = conn.Write(buf)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"tcp_port": kps.tcp_port,
			"client":   client,
		}).Error("Error sending message to KISS client application, closing connection")
		conn.Close()
		kps.detachClientIfCurrent(client, conn)
	}
}

// kissNetClient is one client application attached to a KISS TCP port: what
// it sends is for the port's radio channel, if the port has just one, and an
// answer goes to it alone, not to everyone attached.
//
// It is the connection, not just the slot: a client can go away while what it
// sent is still being acted on, and another take its slot, which must not be
// handed answers to questions it never asked.
type kissNetClient struct {
	kns    *KissNetService
	kps    *kissport_status_s
	client int
	conn   net.Conn
}

// attached reports whether the client is still attached, in its slot.
func (c kissNetClient) attached() bool {
	return c.conn != nil && c.kps.clientConn(c.client) == c.conn
}

func (c kissNetClient) Reply(channel int, cmd int, frame []byte) {
	if !c.attached() {
		return
	}

	c.kns.sendOn(c.kps, c.client, c.conn, channel, cmd, frame)
}

func (c kissNetClient) Prompt(text []byte) {
	if !c.attached() {
		return
	}

	// A client app might think it is attached to a traditional TNC.
	// It might try sending commands over and over again trying to get the TNC into KISS mode.
	// We recognize this attempt and send it something to keep it happy.
	logrus.WithFields(logrus.Fields{
		"tcp_port": c.kps.tcp_port,
		"client":   c.client,
		"hint": "Is client app treating this like an old TNC with command mode?  " +
			"This can be caused by the application sending commands to put a " +
			"traditional TNC into KISS mode.  It is usually a harmless warning.  " +
			"For best results, configure for a KISS-only TNC to avoid this.  " +
			"In the case of APRSISCE/32, use Simply(KISS) rather than KISS",
	}).Warn("Something unexpected from KISS TCP client application")

	if c.kns.debug > 0 {
		kiss_debug_print(TO_CLIENT, "Fake command prompt", text)
	}

	c.kns.write(c.kps, c.client, c.conn, text)
}

func (c kissNetClient) RadioChannel(frameChannel int) int {
	if c.kps.channel != -1 {
		// Ignore channel from KISS and substitute radio channel for that KISS TCP port.
		return c.kps.channel
	}

	return frameChannel
}

/*-------------------------------------------------------------------
 *
 * Name:        Copy
 *
 * Purpose:     Send data from one KISS client to the network ones.
 *
 * Inputs:	msg		- KISS frame data without the framing or escapes.
 *			  The first byte is channel and command (should be data).
 *
 *		chan		- Channel.  Use this instead of first byte of msg.
 *
 *		cmd		- KISS command nybble.
 *				  Should be 0 because I'm expecting this only for data.
 *
 *		from		- The client it came from, which it is not copied
 *				  back to.
 *
 *
 * Global In:	kiss_copy	- From misc. configuration.
 *				  This enables the feature.
 *
 *
 * Description:	Send message to any attached network KISS clients, other than the one where it came from.
 *		Enable this by putting KISSCOPY in the configuration file.
 *		Note that this applies only to network (TCP) KISS clients, not serial port, or pseudo terminal.
 *
 *
 *--------------------------------------------------------------------*/

func (kns *KissNetService) Copy(msg []byte, channel int, cmd int, from kissClient) {
	if kns == nil {
		return // No TCP clients to copy to.
	}

	if !kns.miscConfigP.kiss_copy {
		return
	}

	for kps := kns.allPorts; kps != nil; kps = kps.pnext {
		for client := range MAX_NET_CLIENTS {
			var conn = kps.clientConn(client)

			// To all but origin.
			if conn != nil && from != kissClient(kissNetClient{kns: kns, kps: kps, client: client, conn: conn}) {
				// msg[0] is the channel and command it came with; sendOn
				// works out what this client's port shows instead.
				kns.sendOn(kps, client, conn, channel, cmd, msg[1:])
			}
		}
	}
} /* end Copy */

/*-------------------------------------------------------------------
 *
 * Name:        listenThread
 *
 * Purpose:     Wait for KISS messages from an application.
 *
 * Inputs:	arg		- client number, 0 .. MAX_NET_CLIENTS-1
 *
 * Outputs:	client_sock[n]	- File descriptor for communicating with client app.
 *
 * Description:	Process messages from the client application.
 *		Note that the client can go away and come back again and
 *		re-establish communication without restarting this application.
 *
 *--------------------------------------------------------------------*/

/* Return one byte (value 0 - 255) */

// get returns the next byte from a client, and the connection it came over
// and the frame decoder state it belongs to.  It reports false instead if ctx was cancelled, in which case
// there is no byte and the caller should stop.
func (kns *KissNetService) get(ctx context.Context, kps *kissport_status_s, client int) (byte, net.Conn, *kiss.Collector, bool) {
	for ctx.Err() == nil {
		var conn, frame = kps.connAndFrame(client)
		for conn == nil {
			if !dwutil.SleepCtx(ctx, kns.pollInterval) { /* Not connected.  Try again later. */
				return 0, nil, nil, false
			}

			conn, frame = kps.connAndFrame(client)
		}

		/* Just get one byte at a time. */

		// A client that connects and then says nothing leaves the read below
		// blocked indefinitely.  What gets us back is the port's stop(),
		// registered against ctx in initOne, which hangs up on every attached
		// client - so there is nothing to arm here, once per byte, of our own.
		var ch = make([]byte, 1)
		var n, _ = conn.Read(ch)

		if ctx.Err() != nil {
			return 0, nil, nil, false
		}

		if n == 1 {
			if logrus.IsLevelEnabled(logrus.TraceLevel) {
				logrus.WithField("ch", fmt.Sprintf("%02x", ch[0])).Trace("kissnet get")
			}

			return ch[0], conn, frame, true
		}

		conn.Close()

		// Only clear the slot if conn is still the current connection for
		// this client. If it isn't, connectListenThread has already
		// accepted a newer connection here (e.g. the client reconnected)
		// and we must not clobber it out from under that newer connection.
		if kps.detachClientIfCurrent(client, conn) {
			logrus.WithFields(logrus.Fields{
				"tcp_port": kps.tcp_port,
				"client":   client,
			}).Info("KISS client application has gone away")
		}
	}

	return 0, nil, nil, false
}

func (kns *KissNetService) listenThread(ctx context.Context, kps *kissport_status_s, client int) {
	dwutil.Assert(client >= 0 && client < MAX_NET_CLIENTS)

	logrus.WithFields(logrus.Fields{
		"tcp_port": kps.tcp_port,
		"client":   client,
	}).Debug("kissnet_listen_thread")

	// The client might think it is attached to a traditional TNC.  It might try
	// sending commands over and over again trying to get the TNC into KISS mode.
	// To keep it happy, we recognize this attempt and send it something to keep it
	// happy.  In the case of a serial port or pseudo terminal, there is only one
	// potential client, but here there can be several attached, and we wouldn't
	// want to send the response to all of them - so the handler is told which
	// client this is, and answers that one.   Actually, we should be providing
	// only "Simply KISS" as some call it.

	// Built once per connection, rather than for every byte.
	var lastConn net.Conn

	var from kissClient

	for {
		var ch, conn, frame, ok = kns.get(ctx, kps, client)
		if !ok {
			return // Cancelled.
		}

		if conn != lastConn {
			lastConn = conn
			from = kissNetClient{kns: kns, kps: kps, client: client, conn: conn}
		}

		kns.handler.RecByte(frame, ch, kns.debug, from)
	}
} /* end listenThread */

func (kns *KissNetService) initOne(ctx context.Context, kps *kissport_status_s) {
	logrus.WithFields(logrus.Fields{
		"tcp_port": kps.tcp_port,
		"channel":  kps.channel,
	}).Debug("kissnet_init")
	for client := range MAX_NET_CLIENTS {
		kps.client_sock[client] = nil
		kps.kf[client] = new(kiss.Collector)
	}

	// Hang up on whoever is attached when we are asked to stop.
	context.AfterFunc(ctx, kps.stop)

	/*
	 * This waits for a client to connect and sets client_sock[n].
	 */
	go kns.connectListenThread(ctx, kps)

	/*
	 * These read messages from client when client_sock[n] is valid.
	 * Currently we start up a separate thread for each potential connection.
	 * Possible later refinement.  Start one now, others only as needed.
	 */
	for client := range MAX_NET_CLIENTS {
		go kns.listenThread(ctx, kps, client)
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        connectListenThread
 *
 * Purpose:     Wait for a connection request from an application.
 *
 * Inputs:	kps		- KISS port status block, whose listener is
 *				  already bound.
 *
 * Outputs:	client_sock	- File descriptor for communicating with client app.
 *
 * Description:	Wait for connection request from client and establish
 *		communication.
 *		Note that the client can go away and come back again and
 *		re-establish communication without restarting this application.
 *
 *--------------------------------------------------------------------*/

func (kns *KissNetService) connectListenThread(ctx context.Context, kps *kissport_status_s) {
	var listener = kps.listener

	// As in server.go: Go's net package sets SO_REUSEADDR on a Unix TCP
	// listener for us, and setting it through TCPListener.File puts the
	// socket into blocking mode, after which Close can no longer interrupt a
	// goroutine waiting in Accept.

	logrus.WithField("tcp_port", kps.tcp_port).Debug("opened KISS TCP socket for stream i/o")

	// Accept below blocks until a client turns up, which may be never, so
	// closing the listener is what gets us back when we are asked to stop -
	// and it gives the port up there and then, rather than holding it until
	// the process exits.
	defer dwutil.CloseOnDone(ctx, listener)()

	for ctx.Err() == nil {
		var client = kps.findFreeClient()

		if client >= 0 {
			logrus.WithFields(logrus.Fields{
				"tcp_port": kps.tcp_port,
				"client":   client,
				"channel":  kps.channel,
			}).Debug("Ready to accept KISS TCP client application")

			var conn, acceptErr = listener.Accept()
			if acceptErr != nil {
				if ctx.Err() != nil {
					return // We closed the listener ourselves on the way out.
				}

				logrus.WithError(acceptErr).WithField("tcp_port", kps.tcp_port).Error("Accept failed")

				continue
			}

			// Atomically install this client's connection along with a
			// freshly reset frame decoder state. Previously these were two
			// separate, unsynchronized steps: client_sock was published
			// before kf was reset, so a fast sender's bytes could reach
			// listenThread's read loop and start building a frame in the
			// old *kiss.Collector before this goroutine swapped it out from
			// under it, silently corrupting the frame. And the reset
			// touched every client's slot rather than just the one that
			// (re)connected, which could just as easily clobber a frame
			// already in progress on another attached client.
			if !kps.attachClient(client, conn) {
				// Cancelled while this connection sat in the accept queue,
				// so the hanging up has already happened and nothing will
				// ever read from it.  Do it here instead of attaching a
				// client nobody is listening to.
				conn.Close()

				return
			}

			logrus.WithFields(logrus.Fields{
				"tcp_port": kps.tcp_port,
				"client":   client,
				"channel":  kps.channel,
			}).Info("Attached to KISS TCP client application")
		} else if !dwutil.SleepCtx(ctx, kns.pollInterval) { /* wait then check again if more clients allowed. */
			return
		}
	}
}

// Each KISS TCP port has its own status block.
type kissport_status_s struct {
	pnext *kissport_status_s // To next in list.

	arg2 int //nolint:unused // temp for passing second arg into
	// kissnet_listen_thread

	tcp_port int // default 8001; the one listener is bound to.

	listener net.Listener // Bound by NewKissNetService; only connectListenThread accepts on it.

	channel int // Radio channel for this tcp port.
	// -1 for all.

	// mu guards client_sock and kf below, which are read and written from
	// connectListenThread (accepting new connections), listenThread/get
	// (reading from a connected client and detecting disconnection), and
	// SendRecPacket/Copy (writing to connected clients). Once those
	// goroutines are running, always go through the kissport_status_s
	// methods below rather than touching these fields directly. The one
	// exception is initOne, which sets them directly during single-threaded
	// startup, before any goroutine that shares mu exists.
	mu sync.Mutex

	client_sock [MAX_NET_CLIENTS]net.Conn

	kf [MAX_NET_CLIENTS]*kiss.Collector
	/* Accumulated KISS frame and state of decoder. */

	// Set by stop, and guarded by mu along with everything above, so that a
	// connection accepted at the same moment cannot be attached after the
	// hanging up has been and gone.
	stopped bool
}

// clientAddrs is where each attached client is connected from.
func (kps *kissport_status_s) clientAddrs() []net.Addr {
	kps.mu.Lock()
	defer kps.mu.Unlock()

	var addrs []net.Addr

	for _, conn := range kps.client_sock {
		if conn != nil {
			addrs = append(addrs, conn.RemoteAddr())
		}
	}

	return addrs
}

// clientConn returns the currently connected socket for client, or nil if
// not connected.
func (kps *kissport_status_s) clientConn(client int) net.Conn {
	kps.mu.Lock()
	defer kps.mu.Unlock()

	return kps.client_sock[client]
}

// attachClient installs conn as client's socket, along with a freshly reset
// decoder state, atomically with respect to clientConn/detachClientIfCurrent.
// This ensures listenThread can never observe a "live" socket for client
// paired with decoder state left over from a previous connection, or from
// another client's slot.
//
// It returns false, having attached nothing, once the port has been stopped:
// the connection is then the caller's to close.  A connection completed by the
// kernel sits in the listening socket's accept queue whether or not anybody is
// still listening, so this can be the first anyone hears of it.
func (kps *kissport_status_s) attachClient(client int, conn net.Conn) bool {
	kps.mu.Lock()
	defer kps.mu.Unlock()

	if kps.stopped {
		return false
	}

	kps.kf[client] = new(kiss.Collector)
	kps.client_sock[client] = conn

	return true
}

// detachClientIfCurrent clears client's socket, but only if it still equals
// conn. This guards against a goroutine which detected an error/EOF on a
// now-stale conn racing with, and clobbering, a newer connection that
// connectListenThread has already installed in the same slot. Returns
// whether it cleared the slot.
func (kps *kissport_status_s) detachClientIfCurrent(client int, conn net.Conn) bool {
	kps.mu.Lock()
	defer kps.mu.Unlock()

	if kps.client_sock[client] != conn {
		return false
	}

	kps.client_sock[client] = nil

	return true
}

// findFreeClient returns the index of the first client slot with no
// connected socket, or -1 if all are in use.
func (kps *kissport_status_s) findFreeClient() int {
	kps.mu.Lock()
	defer kps.mu.Unlock()

	for c := range MAX_NET_CLIENTS {
		if kps.client_sock[c] == nil {
			return c
		}
	}

	return -1
}

// stop hangs up on every attached client and refuses any further attachment.
//
// A client whose TNC is shutting down should find its connection closed there
// and then, rather than holding one that will never say anything again; and
// the per-client read goroutines only close the socket they are actually
// blocked on, which leaves a client that connected while one of them was
// between reads still attached.
func (kps *kissport_status_s) stop() {
	kps.mu.Lock()
	defer kps.mu.Unlock()

	kps.stopped = true

	for c := range MAX_NET_CLIENTS {
		if kps.client_sock[c] != nil {
			kps.client_sock[c].Close()
			kps.client_sock[c] = nil
		}
	}
}

// connAndFrame returns client's currently connected socket (or nil) together
// with its decoder state, fetched under a single lock/unlock. Callers that
// need to pair a connection with its decoder state (e.g. before reading a
// byte from it) must use this rather than clientConn/frame separately: two
// independently-locked calls could observe an attachClient in between them,
// pairing a conn with a *kiss.Collector that belongs to a different connection.
func (kps *kissport_status_s) connAndFrame(client int) (net.Conn, *kiss.Collector) {
	kps.mu.Lock()
	defer kps.mu.Unlock()

	return kps.client_sock[client], kps.kf[client]
}

/* end kissnet.go */
