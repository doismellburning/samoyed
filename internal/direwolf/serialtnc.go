package direwolf

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/doismellburning/samoyed/internal/serialport"
	"github.com/pkg/term"
	"github.com/sirupsen/logrus"
)

// serialTNCReattachDelay is how long a SerialTNC waits before trying to
// reattach to a TNC that has gone away - a USB serial device unplugged, say -
// unless it is told otherwise before it is started.
const serialTNCReattachDelay = 30 * time.Second

// serialTNC describes a KISS TNC on a serial port, for SCHANNEL.
func serialTNC() kissTNC {
	return kissTNC{source: dwcore.SourceSerialTNC, name: "serial TNC", spectrum: "Serial TNC"}
}

// SerialTNC is a KISS TNC on a serial port, standing in for a radio channel.
type SerialTNC struct {
	channel       int // SCHANNEL channel number frames from the TNC are received on.
	device        string
	baud          int
	reattachDelay time.Duration // Before trying to reattach.
	started       atomic.Bool
	debug         int
	recFrame      frameReceiver // Where frames from the TNC go, or nil to drop them.

	// mu guards fd and failed, which listenThread (reading and reattaching)
	// and sendPacket (writing, from the transmit path) share.
	//
	// As with KissSerial, a *term.Term is a bare descriptor that nothing
	// stops being used after it is closed, so only the listening goroutine
	// opens and closes it, and a write holds the lock throughout so that the
	// port cannot be closed underneath it.
	mu sync.Mutex
	fd *term.Term

	// failed is set when a write to fd fails.  The sender cannot close the
	// port itself - the listening goroutine may be blocked reading it - so it
	// stops writing to it and leaves the closing to the listener.
	failed bool
}

// NewSerialTNCs attaches a SerialTNC to each SCHANNEL channel in audio, and
// starts it listening until ctx is cancelled, its frames going to recFrame.
// debug is the "-d k" count, which shows the frames as they arrive.
// It returns the TNC for each such channel, and nil for every other, or the
// error if a TNC's port cannot be opened; the TNCs attached before it keep
// listening until ctx is cancelled.  It is called once at startup.
func NewSerialTNCs(ctx context.Context, audio *RadioConfig, recFrame frameReceiver, debug int) ([MAX_TOTAL_CHANS]*SerialTNC, error) {
	var tncs [MAX_TOTAL_CHANS]*SerialTNC

	for i := range MAX_TOTAL_CHANS {
		if audio.chan_medium[i] != MediumSerialTNC {
			continue
		}

		logrus.WithFields(logrus.Fields{"channel": i, "device": audio.serialTNCDevice[i], "baud": audio.serialTNCBaud[i]}).Debug("Serial TNC")

		var st, err = NewSerialTNC(i, audio.serialTNCDevice[i], audio.serialTNCBaud[i], recFrame, debug)
		if err != nil {
			return tncs, err
		}

		st.Start(ctx)

		tncs[i] = st
	}

	return tncs, nil
}

// NewSerialTNC attaches channel, an SCHANNEL one, to the KISS TNC on the
// serial port device, at baud bits per second.  Frames from it go to recFrame,
// or are dropped if that is nil.  debug is the "-d k" count, as for
// NewSerialTNCs.  Nothing is read from the TNC until Start is called.
func NewSerialTNC(channel int, device string, baud int, recFrame frameReceiver, debug int) (*SerialTNC, error) {
	dwutil.Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	var fd = serialport.Open(device, baud)
	if fd == nil {
		return nil, fmt.Errorf("could not open serial port %s for the channel %d serial TNC", device, channel)
	}

	var st = new(SerialTNC)
	st.channel = channel
	st.device = device
	st.baud = baud
	st.reattachDelay = serialTNCReattachDelay
	st.recFrame = recFrame
	st.debug = debug
	st.fd = fd

	return st, nil
}

// Start reads frames from the TNC, and dispatches them to the received queue,
// until ctx is cancelled.  If the TNC goes away, it tries to reattach to it.
// Anything the listening goroutine reads must be set before calling it.
//
// Starting it again would have two goroutines reading the one port, each
// getting part of what the TNC sends, so a second Start is complained about
// and ignored.
func (st *SerialTNC) Start(ctx context.Context) {
	if !st.started.CompareAndSwap(false, true) {
		logrus.WithField("channel", st.channel).Error("Serial TNC started twice; ignoring the second start")

		return
	}

	go st.listenThread(ctx)
}

// port returns the open serial port, or nil if there isn't one, and whether a
// write to it has failed.
func (st *SerialTNC) port() (*term.Term, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()

	return st.fd, st.failed
}

// setPort installs fd as the open serial port.
func (st *SerialTNC) setPort(fd *term.Term) {
	st.mu.Lock()
	defer st.mu.Unlock()

	st.fd = fd
	st.failed = false
}

// closePort closes the serial port, if it is open, and forgets it.  Only the
// listening goroutine, the one that reads the port, may call it - or anything
// else once that goroutine has stopped.
func (st *SerialTNC) closePort() {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.fd == nil {
		return
	}

	serialport.Close(st.fd)

	st.fd = nil
	st.failed = false
}

// listenThread reads frames from the TNC, and reattaches to it if the port is
// lost, until ctx is cancelled.
//
// A cancellation is noticed between reads rather than during one.  As with
// KissSerial, the port cannot be closed out from under a blocked reader to get
// it back: github.com/pkg/term reads the descriptor directly, so it is not one
// the runtime can interrupt.  A TNC that says nothing therefore holds this
// goroutine until it does, or until the process exits.
func (st *SerialTNC) listenThread(ctx context.Context) {
	var log = logrus.WithFields(logrus.Fields{"channel": st.channel, "device": st.device})

	var collector = new(kiss.Collector) // Gathers KISS frames from the bytes read.

	defer st.closePort()

	for ctx.Err() == nil {
		var fd, failed = st.port()

		if failed {
			// sendPacket has already said why.
			st.closePort()

			fd = nil
		}

		// Reattach to the TNC if not currently attached.  "Attach", rather
		// than "connect", to avoid confusion with the AX.25 connect.
		if fd == nil {
			log.Info("Attempting to reattach to serial TNC")

			fd = serialport.Open(st.device, st.baud)
			if fd == nil {
				log.WithField("delay", st.reattachDelay).Error("Failed to reattach to serial TNC; will try again")

				if !dwutil.SleepCtx(ctx, st.reattachDelay) {
					return
				}

				continue
			}

			st.setPort(fd)

			collector = new(kiss.Collector) // Start with clean state.

			log.Info("Successfully reattached to serial TNC")
		}

		var ch, err = serialport.Get1(fd)

		if ctx.Err() != nil {
			return // The deferred closePort lets go of the port on the way out.
		}

		if err != nil {
			log.WithError(err).WithField("delay", st.reattachDelay).Error("Lost communication with serial TNC; will try to reattach")
			st.closePort()

			if !dwutil.SleepCtx(ctx, st.reattachDelay) {
				return
			}

			continue
		}

		// Separate the byte stream into KISS frames, each received as though
		// heard on the channel.
		tncRecByte(collector, ch, st.debug, st.channel, serialTNC(), st.recFrame)
	}
}

// sendPacket sends pp to the TNC as a KISS frame.  A nil st - a channel with
// no TNC - or one not attached just now drops the packet, saying so against
// channel.  It reports whether pp was sent, and does not free pp.
func (st *SerialTNC) sendPacket(channel int, pp *ax25.Packet) bool {
	var log = logrus.WithField("channel", channel)

	if st == nil {
		log.Error("Not attached to serial TNC; discarding packet")

		return false
	}

	// The on-air frame behind a type byte of 0: a data frame, for the TNC's
	// only radio, KISS channel 0.
	var frame = kiss.Encapsulate(append([]byte{0}, pp.FrameData()...))

	st.mu.Lock()
	defer st.mu.Unlock()

	if st.fd == nil || st.failed {
		log.Error("Not attached to serial TNC; discarding packet")

		return false
	}

	if serialport.Write(st.fd, frame) <= 0 {
		log.WithField("device", st.device).Error("Error sending packet to serial TNC; closing it")

		st.failed = true

		return false
	}

	return true
}
