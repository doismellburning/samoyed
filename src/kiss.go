package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Act as a virtual KISS TNC for use by other packet radio applications.
 *		This file implements it with a pseudo terminal for Linux only.
 *
 * Description:	It implements the KISS TNC protocol as described in:
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
 *		For the Linux case,
 *			We supply a pseudo terminal for use by other applications.
 *
 * Version 1.5:	Split serial port version off into its own file.
 *
 *---------------------------------------------------------------*/

import (
	"context"
	"os"
	"sync"

	"github.com/creack/pty"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

// KissPT is a virtual KISS TNC on a pseudo terminal: the terminal, the state
// of the frame being decoded from it, and how much to say about the traffic.
type KissPT struct {
	debug int /* Print information flowing from and to client. */

	// kf is the accumulated KISS frame and state of the decoder.  Only the
	// listening goroutine touches it once that is running.
	kf *KISSFrame

	// mu guards master, which listenThread (reading, and giving the terminal
	// up on a read error or on the way out) and SendRecPacket (writing from
	// the receive path) share.  Once the listening goroutine is running, go
	// through ptMaster and closePT rather than touching master directly.
	//
	// The lock covers the field, not the I/O: master is in the runtime
	// poller, so a write racing a Close gets os.ErrClosed rather than a
	// descriptor that has since been reused.
	mu sync.Mutex

	master *os.File /* File descriptor for my end. */

	// slave is the pseudo terminal's far end, what a client application
	// opens.  Set by openPT before the listening goroutine starts, and not
	// changed after.
	slave *os.File
}

/*
 * Symlink to pseudo terminal name which changes.
 */

const TMP_KISSTNC_SYMLINK = "/tmp/kisstnc"

// newKissPT builds a KissPT with nothing opened or started.
func newKissPT(debug int) *KissPT {
	var kp = new(KissPT)
	kp.debug = debug
	kp.kf = new(KISSFrame)

	return kp
}

/*-------------------------------------------------------------------
 *
 * Name:        NewKissPT
 *
 * Purpose:     Set up a pseudo terminal acting as a virtual KISS TNC.
 *
 *
 * Inputs:	mc		- Configuration; enable_kiss_pt says whether to.
 *
 *		debug		- Print information flowing from and to client.
 *
 * Outputs:
 *
 * Description:	(1) Create a pseudo terminal for the client to use.
 *		(2) Start a new thread to listen for commands from client app
 *		    so the main application doesn't block while we wait.
 *
 *
 *--------------------------------------------------------------------*/

func NewKissPT(ctx context.Context, mc *misc_config_s, debug int) *KissPT {
	var kp = newKissPT(debug)

	if mc.enable_kiss_pt {
		// Nothing else is running yet, so there is no lock to take.
		kp.openPT()

		/*
		 * This reads messages from client.
		 */
		if kp.master != nil {
			go kp.listenThread(ctx)
		}
	}

	logrus.WithField("pt_master_open", kp.ptMaster() != nil).Debug("end of NewKissPT")

	return kp
}

// pollable hands back a *os.File for the same open file as f, in non-blocking
// mode so that the Go runtime's poller owns it, and closes f.
//
// pty.Open issues its ioctls through (*os.File).Fd(), which hands back a
// descriptor in blocking mode and takes it out of the poller for good.  A
// descriptor the poller does not own cannot be woken by Close, so the
// listening goroutine - which spends its life blocked in a read of the master,
// with nothing obliging the client at the far end to ever send anything -
// would have no way of ever stopping.  Duplicating rather than setting
// O_NONBLOCK on f's own descriptor keeps f's eventual finalizer away from the
// descriptor we go on using.
func pollable(f *os.File) (*os.File, error) {
	var fd, dupErr = unix.Dup(int(f.Fd()))
	if dupErr != nil {
		return nil, dupErr
	}

	f.Close()

	var nonblockErr = unix.SetNonblock(fd, true)
	if nonblockErr != nil {
		unix.Close(fd)

		return nil, nonblockErr
	}

	return os.NewFile(uintptr(fd), f.Name()), nil
}

/*-------------------------------------------------------------------
 *
 * Name:        SendRecPacket
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
 *		kps, client	- Not used for pseudo terminal.
 *				  Here so that 3 related functions all have
 *				  the same parameter list.
 *
 * Description:	Send message to client.
 *		We really don't care if anyone is listening or not.
 *		I don't even know if we can find out.
 *
 *		Safe on a nil receiver, so the receive paths need no guard
 *		before startup has got as far as the pseudo terminal.
 *
 *--------------------------------------------------------------------*/

func (kp *KissPT) SendRecPacket(channel int, kiss_cmd int, fbuf []byte, flen int, kps *kissport_status_s, client int) {
	if kp == nil {
		return
	}

	var master = kp.ptMaster()
	if master == nil {
		return
	}

	var kiss_buff []byte

	if flen < 0 {
		if kp.debug > 0 {
			kiss_debug_print(TO_CLIENT, "Fake command prompt", fbuf)
		}

		kiss_buff = fbuf
	} else {
		var stemp []byte

		if flen > AX25_MAX_PACKET_LEN {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("\nPseudo Terminal KISS buffer too small.  Truncated.\n\n")

			fbuf = fbuf[:AX25_MAX_PACKET_LEN]
		}

		stemp = []byte{byte((channel << 4) | kiss_cmd)}
		stemp = append(stemp, fbuf...)

		if kp.debug >= 2 {
			/* AX.25 frame with the CRC removed. */
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("\n")
			dw_printf("Packet content before adding KISS framing and any escapes:\n")
			HexDump(fbuf)
		}

		kiss_buff = KissEncapsulate(stemp)

		/* This has KISS framing and escapes for sending to client app. */

		if kp.debug > 0 {
			kiss_debug_print(TO_CLIENT, "", kiss_buff)
		}
	}

	var n, err = master.Write(kiss_buff)

	if n != len(kiss_buff) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("\nError sending KISS message to client application on pseudo terminal.  fd=%s, len=%d, write returned %d, err = %s\n\n",
			master.Name(), len(kiss_buff), n, err)
	} else if err != nil /* TODO KG Need to test real behaviour here: && errno == EWOULDBLOCK */ {
		text_color_set(DW_COLOR_INFO)
		dw_printf("KISS SEND - Discarding message because no one is listening.\n")
		dw_printf("This happens when you use the -p option and don't read from the pseudo terminal.\n")
	}
} /* SendRecPacket */

// openPT opens the pseudo terminal and points the symlink at it.  It is for
// before the listening goroutine starts, so it takes no lock.
func (kp *KissPT) openPT() {
	logrus.Debug("KissPT.openPT")
	var ptmx, pts, err = pty.Open()
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR - Could not create pseudo terminal for KISS TNC: %s.\n", err)

		return
	}

	var master, pollableErr = pollable(ptmx)
	if pollableErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR - Could not set up pseudo terminal for KISS TNC: %s.\n", pollableErr)
		pts.Close()

		return
	}

	kp.master = master
	kp.slave = pts

	// TODO KG Figure out the right serial settings?

	// TODO KG grantpt?
	// TODO KG unlockpt?
	// TODO KG ptsname?
	// TODO KG cfmakeraw?

	// TODO KG ts.c_cc[C.VMIN] = 1  /* wait for at least one character */
	// TODO KG ts.c_cc[C.VTIME] = 0 /* no fancy timing. */

	// TODO KG tcsetattr TCSANOW?

	/*
	 * We had a problem here since the beginning.
	 * If no one was reading from the other end of the pseudo
	 * terminal, the buffer space would eventually fill up,
	 * the write here would block, and the receive decode
	 * thread would get stuck.
	 *
	 * March 2016 - A "select" was put before the read to
	 * solve a different problem.  With that in place, we can
	 * now use non-blocking I/O and detect the buffer full
	 * condition here.
	 */

	// text_color_set(DW_COLOR_DEBUG);
	// dw_printf("Debug: Try using non-blocking mode for pseudo terminal.\n");

	/* TODO KG
	var flags = C.fcntl(fd, C.F_GETFL, 0)
	e = C.fcntl(fd, C.F_SETFL, flags|C.O_NONBLOCK)
	if e != 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Can't set pseudo terminal to nonblocking, fcntl returns %d, errno = %d\n", e, errno)
		panic("pt fcntl")
	}
	*/

	text_color_set(DW_COLOR_INFO)
	dw_printf("Virtual KISS TNC is available on %s\n", kp.slave.Name())

	// Sample code shows this. Why would we open it here?
	// On Ubuntu, the slave side disappears after a few
	// seconds if no one opens it.  Same on Raspbian which
	// is also based on Debian.
	// Need to revisit this.

	/* TODO KG
	var pt_slave_fd = C.open(pt_slave_name, C.O_RDWR|C.O_NOCTTY)

	if pt_slave_fd < 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Can't open %s\n", pt_slave_name)
		panic("")
		return -1
	}
	*/

	/*
	 * The device name is not the same every time.
	 * This is inconvenient for the application because it might
	 * be necessary to change the device name in the configuration.
	 * Create a symlink, /tmp/kisstnc, so the application configuration
	 * does not need to change when the pseudo terminal name changes.
	 */

	os.Remove(TMP_KISSTNC_SYMLINK)

	// TODO: Is this removed when application exits?

	var symlinkErr = os.Symlink(kp.slave.Name(), TMP_KISSTNC_SYMLINK)
	if symlinkErr == nil {
		logrus.WithFields(logrus.Fields{
			"symlink": TMP_KISSTNC_SYMLINK,
			"device":  kp.slave.Name(),
		}).Debug("Created KISS TNC symlink")
	} else {
		// The symlink is a convenience, so the application's configuration
		// does not have to change when the pseudo terminal name does.  The
		// TNC is usable without it.
		logrus.WithError(symlinkErr).WithFields(logrus.Fields{
			"symlink": TMP_KISSTNC_SYMLINK,
			"device":  kp.slave.Name(),
		}).Error("Could not create the KISS TNC symlink; use the device directly")
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        get
 *
 * Purpose:     Read one byte from the KISS client app.
 *
 *
 * Returns:	(byte, nil) on success, or (0, error) on failure.
 *		The caller should stop processing and return on a non-nil error.
 *
 * Description:	There is room for improvement here.  Reading one byte
 *		at a time is inefficient.  We could read a large block
 *		into a local buffer and return a byte from that most of the time.
 *		Is it worth the effort?  I don't know.  With GHz processors and
 *		the low data rate here it might not make a noticeable difference.
 *
 *--------------------------------------------------------------------*/

func (kp *KissPT) get(ctx context.Context) (byte, error) {
	for ctx.Err() == nil {
		/*
		 * Since the beginning we've always had a couple annoying problems with
		 * the pseudo terminal KISS interface.
		 * When using "kissattach" we would sometimes get the error message:
		 *
		 *	kissattach: Error setting line discipline: TIOCSETD: Device or resource busy
		 *	Are you sure you have enabled MKISS support in the kernel
		 *	or, if you made it a module, that the module is loaded?
		 *
		 * martinhpedersen came up with the interesting idea of putting in a "select"
		 * before the "read" and explained it like this:
		 *
		 *	"Reading from master fd of the pty before the client has connected leads
		 *	 to trouble with kissattach.  Use select to check if the slave has sent
		 *	 any data before trying to read from it."
		 *
		 *	"This fix resolves the issue by not reading from the pty's master fd, until
		 *	 kissattach has opened and configured the slave. This is implemented using
		 *	 select() to wait for data before reading from the master fd."
		 *
		 * The submitted code looked like this:
		 *
		 *	FD_ZERO(&fd_in);
		 *	rc = select(pt_master_fd + 1, &fd_in, NULL, &fd_in, NULL);
		 *
		 * That doesn't look right to me for a couple reasons.
		 * First, I would expect to use FD_SET for the fd.
		 * Second, using the same bit mask for two arguments doesn't seem
		 * like a good idea because select modifies them.
		 * When I tried running it, we don't get the failure message
		 * anymore but the select never returns so we can't read data from
		 * the KISS client app.
		 *
		 * I think this is what we want.
		 *
		 * Tested on Raspian (ARM) and Ubuntu (x86_64).
		 * We don't get the error from kissattach anymore.
		 */

		/* TODO KG Check how this all works with Go IO and the pty lib used..
		FD_ZERO(&fd_in)
		FD_SET(pt_master_fd, &fd_in)

		FD_ZERO(&fd_ex)
		FD_SET(pt_master_fd, &fd_ex)

		rc = _select(pt_master_fd+1, &fd_in, NULL, &fd_ex, NULL)

		if rc == 0 {
			continue // When could we get a 0?
		}

		// TODO KG Check rc == -1
		*/
		var master = kp.ptMaster()
		if master == nil {
			return 0, os.ErrClosed
		}

		var ch = make([]byte, 1)
		var n, err = master.Read(ch)

		if ctx.Err() != nil {
			kp.closePT() // Ours to close: nothing will read from it again.

			return 0, ctx.Err()
		}

		if n > 0 {
			return ch[0], nil
		}
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("\nError receiving KISS message from client application.  Closing %s. %s\n\n", kp.slave.Name(), err)

			kp.closePTIfCurrent(master)

			return 0, err
		}
	}

	return 0, ctx.Err()
}

/*-------------------------------------------------------------------
 *
 * Name:        listenThread
 *
 * Purpose:     Read messages from pseudo terminal KISS client application.
 *
 * Description:	Reads bytes from the KISS client app and
 *		sends them to KissRecByte for processing.
 *
 *--------------------------------------------------------------------*/

// closePT closes the pseudo terminal, if it is open, and forgets it, along
// with the symlink that points at its far end.
func (kp *KissPT) closePT() {
	kp.closePTIfCurrent(kp.ptMaster())
}

// closePTIfCurrent is closePT, but only if master is still the open pseudo
// terminal, so that giving up one we read an error from cannot close anything
// that has taken its place.
func (kp *KissPT) closePTIfCurrent(master *os.File) {
	kp.mu.Lock()
	defer kp.mu.Unlock()

	if master == nil || kp.master != master {
		return
	}

	kp.master.Close()

	kp.master = nil

	os.Remove(TMP_KISSTNC_SYMLINK)
}

// ptMaster returns our end of the pseudo terminal, or nil if it is not open.
func (kp *KissPT) ptMaster() *os.File {
	kp.mu.Lock()
	defer kp.mu.Unlock()

	return kp.master
}

func (kp *KissPT) listenThread(ctx context.Context) {
	logrus.Debug("KissPT.listenThread")

	// Nothing obliges the client at the other end of the pseudo terminal to
	// send anything, so closing the master is what ends a read that would
	// otherwise never return.  Armed once here rather than around each read:
	// this goroutine reads one byte at a time, and the pseudo terminal is
	// never reopened underneath it.
	defer closeOnDone(ctx, kp.ptMaster())()

	// Nothing else tears the pseudo terminal down - cleanup has no teardown
	// for it - so it is ours to close whenever we stop, including when a
	// cancellation arrives before we ever get as far as a read.
	defer kp.closePT()

	for ctx.Err() == nil {
		var ch, err = kp.get(ctx)
		if err != nil {
			return
		}
		KissRecByte(kp.kf, ch, kp.debug, nil, -1, kp.SendRecPacket)
	}
}
