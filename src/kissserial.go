//nolint:gochecknoglobals
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

	"github.com/pkg/term"
	"github.com/sirupsen/logrus"
)

/*
 * Save Configuration for later use.
 */

var g_misc_config_p *misc_config_s

/*
 * Accumulated KISS frame and state of decoder.
 */

var kf *KISSFrame

// serialport_mu guards serialport_fd and serialport_failed, which
// kissserial_listen_thread (reading, and in the polling case reopening) and
// kissserial_send_rec_packet (writing from the receive path) share.  Once the
// listening goroutine is running, go through the functions below rather than
// touching either directly.
//
// Unlike a socket or a pollable *os.File, a *term.Term is a bare descriptor
// with nothing to stop it being used after it is closed - by which time the
// number may belong to something else.  So the port is only ever opened and
// closed by the listening goroutine, which is the only one that reads it, and
// a write holds the lock throughout so that the port cannot be closed
// underneath it.
var serialport_mu sync.Mutex

var serialport_fd *term.Term

// serialport_failed is set when a write to serialport_fd fails.  The sender
// cannot close the port itself - the listening goroutine may be blocked
// reading it - so it stops writing to it and leaves the closing to the
// listener, which does so before its next read.
var serialport_failed bool

// serialPort returns the open serial port, or nil if there isn't one, and
// whether a write to it has failed.
func serialPort() (*term.Term, bool) {
	serialport_mu.Lock()
	defer serialport_mu.Unlock()

	return serialport_fd, serialport_failed
}

// setSerialPort installs fd as the open serial port.
func setSerialPort(fd *term.Term) {
	serialport_mu.Lock()
	defer serialport_mu.Unlock()

	serialport_fd = fd
	serialport_failed = false
}

var kissserial_debug = 0 /* Print information flowing from and to client. */

func kissserial_set_debug(n int) {
	kissserial_debug = n
}

/*-------------------------------------------------------------------
 *
 * Name:        kissserial_init
 *
 * Purpose:     Set up a serial port acting as a virtual KISS TNC.
 *
 * Inputs:	mc->
 *		    kiss_serial_port	- Name of device for real or virtual serial port.
 *		    kiss_serial_speed	- Speed, bps, or 0 meaning leave it alone.
 *		    kiss_serial_poll	- When non-zero, poll each n seconds to see if
 *					  device has appeared.
 *
 * Outputs:
 *
 * Description:	(1) Open file descriptor for the device.
 *		(2) Start a new thread to listen for commands from client app
 *		    so the main application doesn't block while we wait.
 *
 *--------------------------------------------------------------------*/

func kissserial_init(ctx context.Context, mc *misc_config_s) {
	g_misc_config_p = mc
	kf = new(KISSFrame)

	if g_misc_config_p.kiss_serial_port != "" {
		if g_misc_config_p.kiss_serial_poll == 0 {
			// Normal case, try to open the serial port at start up time.
			// Nothing else is running yet, so there is no lock to take.
			serialport_failed = false
			serialport_fd = SerialPortOpen(g_misc_config_p.kiss_serial_port, g_misc_config_p.kiss_serial_speed)

			if serialport_fd != nil {
				text_color_set(DW_COLOR_INFO)
				dw_printf("Opened %s for serial port KISS.\n", g_misc_config_p.kiss_serial_port)
			} else { //nolint:staticcheck
				// An error message was already displayed.
			}
		} else {
			// Polling case.   Defer until read and device not opened.
			text_color_set(DW_COLOR_INFO)
			dw_printf("Will be checking periodically for %s\n", g_misc_config_p.kiss_serial_port)
		}

		if g_misc_config_p.kiss_serial_poll != 0 || serialport_fd != nil {
			go kissserial_listen_thread(ctx)
		}
	}

	var fd, _ = serialPort()

	logrus.WithFields(logrus.Fields{
		"serial_port_open": fd != nil,
		"polling":          g_misc_config_p.kiss_serial_poll,
	}).Debug("end of kiss_init")
}

/*-------------------------------------------------------------------
 *
 * Name:        kissserial_send_rec_packet
 *
 * Purpose:     Send a received packet or text string to the client app.
 *
 * Inputs:	chan		- Channel number where packet was received.
 *				  0 = first, 1 = second if any.
 *
 *		kiss_cmd	- Usually KISS_CMD_DATA_FRAME but we can also have
 *				  KISS_CMD_SET_HARDWARE when responding to a query.
 *
 *		pp		- Identifier for packet object.
 *
 *		fbuf		- Address of raw received frame buffer
 *				  or a text string.
 *
 *		flen		- Length of raw received frame not including the FCS
 *				  or -1 for a text string.
 *
 *		kps
 *		client		- Not used for serial port version.
 *				  Here so that 3 related functions all have
 *				  the same parameter list.
 *
 * Description:	Send message to client.
 *		We really don't care if anyone is listening or not.
 *		I don't even know if we can find out.
 *
 *--------------------------------------------------------------------*/

func kissserial_send_rec_packet(channel int, kiss_cmd int, fbuf []byte, flen int,
	notused1 *kissport_status_s, notused2 int) {
	/*
	 * Quietly discard if we don't have open connection.
	 */
	var fd, failed = serialPort()
	if fd == nil || failed {
		return
	}

	var kiss_buff []byte

	if flen < 0 {
		if kissserial_debug > 0 {
			kiss_debug_print(TO_CLIENT, "Fake command prompt", fbuf)
		}

		kiss_buff = fbuf
	} else {
		// Truncating before the frame is assembled, rather than after: the
		// slicing below used to happen once fbuf had already been copied into
		// stemp, so the client was told the frame had been truncated and then
		// handed the whole of it anyway.
		if flen > AX25_MAX_PACKET_LEN {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("\nSerial Port KISS buffer too small.  Truncated.\n\n")

			fbuf = fbuf[:AX25_MAX_PACKET_LEN]
		}

		var leader = byte((channel << 4) | kiss_cmd)
		var stemp = append([]byte{leader}, fbuf...)

		if kissserial_debug >= 2 {
			/* AX.25 frame with the CRC removed. */
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("\n")
			dw_printf("Packet content before adding KISS framing and any escapes:\n")
			HexDump(fbuf)
		}

		kiss_buff = KissEncapsulate(stemp)

		/* This has KISS framing and escapes for sending to client app. */

		if kissserial_debug > 0 {
			kiss_debug_print(TO_CLIENT, "", kiss_buff)
		}
	}

	var kiss_len = len(kiss_buff)

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

	serialport_mu.Lock()
	defer serialport_mu.Unlock()

	// Looked at again now we hold the lock: the listening goroutine may have
	// given the port up since the check above.
	if serialport_fd == nil || serialport_failed {
		return
	}

	var n = SerialPortWrite(serialport_fd, kiss_buff)

	if n != kiss_len {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("\nError sending KISS message to client application thru serial port.\n\n")

		// Not closed here: see serialport_failed.
		serialport_failed = true
	}
} /* kissserial_send_rec_packet */

/*-------------------------------------------------------------------
 *
 * Name:        kissserial_get
 *
 * Purpose:     Read one byte from the KISS client app.
 *
 * Global In:	serialport_fd
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

// closeSerialPortKISS closes the serial port, if it is open, and forgets it.
// Only the listening goroutine, the one that reads the port, may call it - or
// anything else once that goroutine has stopped.
func closeSerialPortKISS() {
	serialport_mu.Lock()
	defer serialport_mu.Unlock()

	if serialport_fd == nil {
		return
	}

	serial_port_close(serialport_fd)

	serialport_fd = nil
	serialport_failed = false
}

// giveUpSerialPortIfFailed closes the serial port if a write to it has failed,
// and says whether it did.  It is the listening goroutine's to call, before it
// reads: the sender cannot close the port itself.
func giveUpSerialPortIfFailed() bool {
	var fd, failed = serialPort()
	if fd == nil || !failed {
		return false
	}

	text_color_set(DW_COLOR_ERROR)
	dw_printf("\nSerial Port KISS write error. Closing connection.\n\n")
	closeSerialPortKISS()

	return true
}

// kissserial_get returns the next byte from the serial port.
//
// A cancellation is noticed between reads rather than during one.  Unlike a
// socket, the port cannot be closed out from under a blocked reader to get it
// back: github.com/pkg/term reads the descriptor directly, with blocking mode
// set explicitly at open, so it is not one the runtime can interrupt - and
// closing a descriptor another thread is reading is a way to have it read
// whatever gets that number next.  A port that says nothing therefore holds
// this goroutine until it does, or until the process exits.
func kissserial_get(ctx context.Context) (byte, error) {
	if g_misc_config_p.kiss_serial_poll == 0 {
		/*
		 * Normal case, was opened at start up time.
		 */
		// In the normal case nothing reopens a port once it is given up.
		if giveUpSerialPortIfFailed() {
			return 0, os.ErrClosed
		}

		var fd, _ = serialPort()
		if fd == nil {
			return 0, os.ErrClosed
		}

		var ch, err = SerialPortGet1(fd)

		if ctx.Err() != nil {
			return 0, ctx.Err()
		}

		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("\nSerial Port KISS read error. Closing connection.\n\n")
			closeSerialPortKISS()

			return ch, err
		}

		if logrus.IsLevelEnabled(logrus.TraceLevel) {
			logrus.WithField("ch", fmt.Sprintf("0x%02x", ch)).Trace("kissserial_get")
		}

		return ch, nil
	}

	/*
	 * Polling case.  Wait until device is present and open.
	 */
	for ctx.Err() == nil {
		giveUpSerialPortIfFailed()

		var fd, _ = serialPort()

		if fd != nil {
			// Open, try to read.
			var ch, err = SerialPortGet1(fd)

			if ctx.Err() != nil {
				return 0, ctx.Err()
			}

			if err == nil {
				return ch, nil
			}

			text_color_set(DW_COLOR_ERROR)
			dw_printf("\nSerial Port KISS read error. Closing connection.\n\n")
			closeSerialPortKISS()
		} else {
			// Not open.  Wait for it to appear and try opening.
			if !sleepSecCtx(ctx, g_misc_config_p.kiss_serial_poll) {
				return 0, ctx.Err()
			}

			var _, statErr = os.Stat(g_misc_config_p.kiss_serial_port)
			if statErr == nil {
				// It's there now.  Try to open.
				var fd = SerialPortOpen(g_misc_config_p.kiss_serial_port, g_misc_config_p.kiss_serial_speed)

				if fd != nil {
					text_color_set(DW_COLOR_INFO)
					dw_printf("\nOpened %s for serial port KISS.\n\n", g_misc_config_p.kiss_serial_port)

					kf = new(KISSFrame) // Start with clean state.

					setSerialPort(fd)
				} else { //nolint:staticcheck
					// An error message was already displayed.
				}
			}
		}
	}

	return 0, ctx.Err()
} /* end kissserial_get */

/*-------------------------------------------------------------------
 *
 * Name:        kissserial_listen_thread
 *
 * Purpose:     Read messages from serial port KISS client application.
 *
 * Global In:	serialport_fd
 *
 * Description:	Reads bytes from the serial port KISS client app and
 *		sends them to KissRecByte for processing.
 *		KissRecByte is a common function used by all 3 KISS
 *		interfaces: serial port, pseudo terminal, and TCP.
 *
 *--------------------------------------------------------------------*/

func kissserial_listen_thread(ctx context.Context) {
	logrus.Debug("kissserial_listen_thread")

	// Ours to close whenever we stop, including when a cancellation lands
	// between the polling case opening the port and the next look at ctx.
	defer closeSerialPortKISS()

	for ctx.Err() == nil {
		var ch, err = kissserial_get(ctx)
		if err != nil {
			return
		}

		KissRecByte(kf, ch, kissserial_debug, nil, -1, kissserial_send_rec_packet)
	}
}
