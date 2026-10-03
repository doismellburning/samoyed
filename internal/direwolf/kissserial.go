package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Act as a virtual KISS TNC for use by other packet radio applications.
 *		This file provides the service by good old fashioned serial port.
 *		Other files implement a pseudo terminal or TCP KISS interface.
 *
 * Description:	This implements the KISS TNC protocol as described in:
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
 *						Spec says it is obsolete but Xastir
 *						sends it and we respect it.
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
 * Platform differences:
 *
 *		This file implements KISS over a serial port.
 *		It should behave pretty much the same for both Windows and Linux.
 *
 *		When running a client application on Windows, two applications
 *		can be connected together using a a "Null-modem emulator"
 *		such as com0com from http://sourceforge.net/projects/com0com/
 *
 *		(When running a client application, on the same host, with Linux,
 *		a pseudo terminal can be used for old applications.  More modern
 *		applications will generally have AGW and/or KISS over TCP.)
 *
 *
 * version 1.5:	Split out from kiss.c, simplified, consistent for Windows and Linux.
 *		Add polling option for use with Bluetooth.
 *
 *---------------------------------------------------------------*/

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/doismellburning/samoyed/internal/serialport"
	"github.com/pkg/term"
	"github.com/sirupsen/logrus"
)

// KissSerial is a virtual KISS TNC on a serial port: the port, the state of
// the frame being decoded from it, and the configuration it was set up with.
type KissSerial struct {
	miscConfig *misc_config_s
	handler    *KissHandler // Acts on what the client sends.
	debug      int          /* Print information flowing from and to client. */

	// kf is the accumulated KISS frame and state of the decoder.  Only the
	// listening goroutine touches it once that is running.
	kf *kiss.Collector

	// mu guards fd and failed, which listenThread (reading, and in the
	// polling case reopening) and SendRecPacket (writing from the receive
	// path) share.  Once the listening goroutine is running, go through the
	// methods below rather than touching either directly.
	//
	// Unlike a socket or a pollable *os.File, a *term.Term is a bare
	// descriptor with nothing to stop it being used after it is closed - by
	// which time the number may belong to something else.  So the port is
	// only ever opened and closed by the listening goroutine, which is the
	// only one that reads it, and a write holds the lock throughout so that
	// the port cannot be closed underneath it.
	mu sync.Mutex

	fd *term.Term

	// failed is set when a write to fd fails.  The sender cannot close the
	// port itself - the listening goroutine may be blocked reading it - so it
	// stops writing to it and leaves the closing to the listener, which does
	// so before its next read.
	failed bool
}

// newKissSerial builds a KissSerial for mc with nothing opened or started.
func newKissSerial(mc *misc_config_s, handler *KissHandler, debug int) *KissSerial {
	var ks = new(KissSerial)
	ks.miscConfig = mc
	ks.handler = handler
	ks.debug = debug
	ks.kf = new(kiss.Collector)

	return ks
}

/*-------------------------------------------------------------------
 *
 * Name:        NewKissSerial
 *
 * Purpose:     Set up a serial port acting as a virtual KISS TNC.
 *
 * Inputs:	mc->
 *		    kiss_serial_port	- Name of device for real or virtual serial port.
 *		    kiss_serial_speed	- Speed, bps, or 0 meaning leave it alone.
 *		    kiss_serial_poll	- When non-zero, poll each n seconds to see if
 *					  device has appeared.
 *
 *		debug	- Print information flowing from and to client.
 *
 * Description:	(1) Open file descriptor for the device.
 *		(2) Start a new thread to listen for commands from client app
 *		    so the main application doesn't block while we wait.
 *
 *--------------------------------------------------------------------*/

func NewKissSerial(ctx context.Context, mc *misc_config_s, handler *KissHandler, debug int) *KissSerial {
	var ks = newKissSerial(mc, handler, debug)

	if mc.kiss_serial_port != "" {
		if mc.kiss_serial_poll == 0 {
			// Normal case, try to open the serial port at start up time.
			// Nothing else is running yet, so there is no lock to take.
			ks.fd = serialport.Open(mc.kiss_serial_port, mc.kiss_serial_speed)

			if ks.fd != nil {
				text_color_set(DW_COLOR_INFO)
				dw_printf("Opened %s for serial port KISS.\n", mc.kiss_serial_port)
			} else { //nolint:staticcheck
				// An error message was already displayed.
			}
		} else {
			// Polling case.   Defer until read and device not opened.
			text_color_set(DW_COLOR_INFO)
			dw_printf("Will be checking periodically for %s\n", mc.kiss_serial_port)
		}

		if mc.kiss_serial_poll != 0 || ks.fd != nil {
			go ks.listenThread(ctx)
		}
	}

	var fd, _ = ks.port()

	logrus.WithFields(logrus.Fields{
		"serial_port_open": fd != nil,
		"polling":          mc.kiss_serial_poll,
	}).Debug("end of kiss_init")

	return ks
}

/*-------------------------------------------------------------------
 *
 * Name:        SendRecPacket
 *
 * Purpose:     Send a received packet to the client app.
 *
 * Inputs:	chan		- Channel number where packet was received.
 *				  0 = first, 1 = second if any.
 *
 *		kiss_cmd	- Usually kiss.CmdDataFrame but we can also have
 *				  kiss.CmdSetHardware when responding to a query.
 *
 *		frame		- Raw received frame, not including the FCS.
 *
 * Description:	Send message to client.
 *		We really don't care if anyone is listening or not.
 *		I don't even know if we can find out.
 *
 *--------------------------------------------------------------------*/

func (ks *KissSerial) SendRecPacket(channel int, kiss_cmd int, frame []byte) {
	/*
	 * Quietly discard if we don't have open connection.
	 */
	if ks == nil {
		return
	}

	var fd, failed = ks.port()
	if fd == nil || failed {
		return
	}

	ks.write(kissClientFrame(channel, kiss_cmd, frame, ks.debug, "Serial Port"))
} /* SendRecPacket */

// The serial port has one client at the far end, which is where an answer to
// anything it sends goes.

func (ks *KissSerial) reply(channel int, cmd int, frame []byte) {
	ks.SendRecPacket(channel, cmd, frame)
}

func (ks *KissSerial) prompt(text []byte) {
	var fd, failed = ks.port()
	if fd == nil || failed {
		return
	}

	if ks.debug > 0 {
		kiss_debug_print(TO_CLIENT, "Fake command prompt", text)
	}

	ks.write(text)
}

func (ks *KissSerial) radioChannel(frameChannel int) int {
	return frameChannel
}

// write sends buf, already framed or the fake command prompt, to the
// client, unless the port has gone or failed.
func (ks *KissSerial) write(buf []byte) {
	/*
	 * This write can block on Windows if using the virtual null modem
	 * and nothing is connected to the other end.
	 * The solution is found in the com0com ReadMe file:
	 *
	 *	Q. My application hangs during its startup when it sends anything to one paired
	 *	   COM port. The only way to unhang it is to start HyperTerminal, which is connected
	 *	   to the other paired COM port. I didn't have this problem with physical serial
	 *	   ports.
	 *	A. Your application can hang because receive buffer overrun is disabled by
	 *	   default. You can fix the problem by enabling receive buffer overrun for the
	 *	   receiving port. Also, to prevent some flow control issues you need to enable
	 *	   baud rate emulation for the sending port. So, if your application use port CNCA0
	 *	   and other paired port is CNCB0, then:
	 *
	 *	   1. Launch the Setup Command Prompt shortcut.
	 *	   2. Enter the change commands, for example:
	 *
	 *	      command> change CNCB0 EmuOverrun=yes
	 *	      command> change CNCA0 EmuBR=yes
	 */

	ks.mu.Lock()
	defer ks.mu.Unlock()

	// Looked at again now we hold the lock: the listening goroutine may have
	// given the port up since the caller looked.
	if ks.fd == nil || ks.failed {
		return
	}

	var n = serialport.Write(ks.fd, buf)

	if n != len(buf) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("\nError sending KISS message to client application thru serial port.\n\n")

		// Not closed here: see KissSerial.failed.
		ks.failed = true
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        get
 *
 * Purpose:     Read one byte from the KISS client app.
 *
 * Inputs:	ks.fd
 *
 * Returns:	one byte (value 0 - 255) or optional error
 *
 * Description:	There is room for improvement here.  Reading one byte
 *		at a time is inefficient.  We could read a large block
 *		into a local buffer and return a byte from that most of the time.
 *		Is it worth the effort?  I don't know.  With GHz processors and
 *		the low data rate here it might not make a noticeable difference.
 *
 *--------------------------------------------------------------------*/

// port returns the open serial port, or nil if there isn't one, and whether a
// write to it has failed.
func (ks *KissSerial) port() (*term.Term, bool) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	return ks.fd, ks.failed
}

// setPort installs fd as the open serial port.
func (ks *KissSerial) setPort(fd *term.Term) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ks.fd = fd
	ks.failed = false
}

// closePort closes the serial port, if it is open, and forgets it.  Only the
// listening goroutine, the one that reads the port, may call it - or anything
// else once that goroutine has stopped.
func (ks *KissSerial) closePort() {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if ks.fd == nil {
		return
	}

	serialport.Close(ks.fd)

	ks.fd = nil
	ks.failed = false
}

// giveUpPortIfFailed closes the serial port if a write to it has failed, and
// says whether it did.  It is the listening goroutine's to call, before it
// reads: the sender cannot close the port itself.
func (ks *KissSerial) giveUpPortIfFailed() bool {
	var fd, failed = ks.port()
	if fd == nil || !failed {
		return false
	}

	text_color_set(DW_COLOR_ERROR)
	dw_printf("\nSerial Port KISS write error. Closing connection.\n\n")
	ks.closePort()

	return true
}

// get returns the next byte from the serial port.
//
// A cancellation is noticed between reads rather than during one.  Unlike a
// socket, the port cannot be closed out from under a blocked reader to get it
// back: github.com/pkg/term reads the descriptor directly, with blocking mode
// set explicitly at open, so it is not one the runtime can interrupt - and
// closing a descriptor another thread is reading is a way to have it read
// whatever gets that number next.  A port that says nothing therefore holds
// this goroutine until it does, or until the process exits.
func (ks *KissSerial) get(ctx context.Context) (byte, error) {
	if ks.miscConfig.kiss_serial_poll == 0 {
		/*
		 * Normal case, was opened at start up time.
		 */
		// In the normal case nothing reopens a port once it is given up.
		if ks.giveUpPortIfFailed() {
			return 0, os.ErrClosed
		}

		var fd, _ = ks.port()
		if fd == nil {
			return 0, os.ErrClosed
		}

		var ch, err = serialport.Get1(fd)

		if ctx.Err() != nil {
			return 0, ctx.Err()
		}

		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("\nSerial Port KISS read error. Closing connection.\n\n")
			ks.closePort()

			return ch, err
		}

		if logrus.IsLevelEnabled(logrus.TraceLevel) {
			logrus.WithField("ch", fmt.Sprintf("0x%02x", ch)).Trace("KissSerial.get")
		}

		return ch, nil
	}

	/*
	 * Polling case.  Wait until device is present and open.
	 */
	for ctx.Err() == nil {
		ks.giveUpPortIfFailed()

		var fd, _ = ks.port()

		if fd != nil {
			// Open, try to read.
			var ch, err = serialport.Get1(fd)

			if ctx.Err() != nil {
				return 0, ctx.Err()
			}

			if err == nil {
				return ch, nil
			}

			text_color_set(DW_COLOR_ERROR)
			dw_printf("\nSerial Port KISS read error. Closing connection.\n\n")
			ks.closePort()
		} else {
			// Not open.  Wait for it to appear and try opening.
			if !dwutil.SleepSecCtx(ctx, ks.miscConfig.kiss_serial_poll) {
				return 0, ctx.Err()
			}

			var _, statErr = os.Stat(ks.miscConfig.kiss_serial_port)
			if statErr == nil {
				// It's there now.  Try to open.
				var fd = serialport.Open(ks.miscConfig.kiss_serial_port, ks.miscConfig.kiss_serial_speed)

				if fd != nil {
					text_color_set(DW_COLOR_INFO)
					dw_printf("\nOpened %s for serial port KISS.\n\n", ks.miscConfig.kiss_serial_port)

					ks.kf = new(kiss.Collector) // Start with clean state.

					ks.setPort(fd)
				} else { //nolint:staticcheck
					// An error message was already displayed.
				}
			}
		}
	}

	return 0, ctx.Err()
} /* end get */

/*-------------------------------------------------------------------
 *
 * Name:        listenThread
 *
 * Purpose:     Read messages from serial port KISS client application.
 *
 * Inputs:	ks.fd
 *
 * Description:	Reads bytes from the serial port KISS client app and
 *		sends them to the KissHandler for processing, which
 *		all 3 KISS interfaces share: serial port, pseudo terminal,
 *		and TCP.
 *
 *--------------------------------------------------------------------*/

func (ks *KissSerial) listenThread(ctx context.Context) {
	logrus.Debug("KissSerial.listenThread")

	// Ours to close whenever we stop, including when a cancellation lands
	// between the polling case opening the port and the next look at ctx.
	defer ks.closePort()

	for ctx.Err() == nil {
		var ch, err = ks.get(ctx)
		if err != nil {
			return
		}

		ks.handler.RecByte(ks.kf, ch, ks.debug, ks)
	}
}
