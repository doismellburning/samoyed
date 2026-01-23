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

// #include <stdio.h>
// #include <unistd.h>
// #include <stdlib.h>
// #include <string.h>
// #include <ctype.h>
// #include <fcntl.h>
// #include <termios.h>
// #include <sys/select.h>
// #include <sys/types.h>
// #include <sys/ioctl.h>
// #include <errno.h>
import "C"

import (
	"os"

	"github.com/creack/pty"
)

/*
 * Accumulated KISS frame and state of decoder.
 */

var kisspt_kf *kiss_frame_t

/*
 * These are for a Linux pseudo terminal.
 */

var pt_master *os.File /* File descriptor for my end. */
var pt_slave *os.File  /* Pseudo terminal slave */

/*
 * Symlink to pseudo terminal name which changes.
 */

const TMP_KISSTNC_SYMLINK = "/tmp/kisstnc"

var kisspt_debug = 0 /* Print information flowing from and to client. */

func kisspt_set_debug(n int) {
	kisspt_debug = n
}

/*-------------------------------------------------------------------
 *
 * Name:        kisspt_init
 *
 * Purpose:     Set up a pseudo terminal acting as a virtual KISS TNC.
 *
 *
 * Inputs:
 *
 * Outputs:
 *
 * Description:	(1) Create a pseudo terminal for the client to use.
 *		(2) Start a new thread to listen for commands from client app
 *		    so the main application doesn't block while we wait.
 *
 *
 *--------------------------------------------------------------------*/

func kisspt_init(mc *misc_config_s) {
	/*
	 * This reads messages from client.
	 */
	pt_master = nil

	kisspt_kf = new(kiss_frame_t)

	if mc.enable_kiss_pt > 0 {
		kisspt_open_pt()

		if pt_master != nil {
			go kisspt_listen_thread()
		}
	}

	/* TODO KG
	#if DEBUG
		text_color_set (DW_COLOR_DEBUG);

		dw_printf ("end of kisspt_init: pt_master_fd = %d\n", pt_master_fd);
	#endif
	*/
}

func kisspt_open_pt() {
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("kisspt_open_pt (  )\n");
	#endif
	*/

	var ptmx, pts, err = pty.Open()
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR - Could not create pseudo terminal for KISS TNC: %s.\n", err)
		return
	}

	pt_master = ptmx
	pt_slave = pts

	// TODO KG grantpt?
	// TODO KG unlockpt?
	// TODO KG ptsname?
	// TODO KG cfmakeraw?

	// FIXME KG ts.c_cc[C.VMIN] = 1  /* wait for at least one character */
	// FIXME KG ts.c_cc[C.VTIME] = 0 /* no fancy timing. */

	// FIXME KG tcsetattr TCSANOW?

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

	/* FIXME KG
	var flags = C.fcntl(fd, C.F_GETFL, 0)
	e = C.fcntl(fd, C.F_SETFL, flags|C.O_NONBLOCK)
	if e != 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Can't set pseudo terminal to nonblocking, fcntl returns %d, errno = %d\n", e, errno)
		panic("pt fcntl")
	}
	*/

	text_color_set(DW_COLOR_INFO)
	dw_printf("Virtual KISS TNC is available on %s\n", pt_slave.Name())

	// Sample code shows this. Why would we open it here?
	// On Ubuntu, the slave side disappears after a few
	// seconds if no one opens it.  Same on Raspbian which
	// is also based on Debian.
	// Need to revisit this.

	/* FIXME KG
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

	var symlinkErr = os.Symlink(pt_slave.Name(), TMP_KISSTNC_SYMLINK)
	if symlinkErr == nil {
		dw_printf("Created symlink %s -> %s\n", TMP_KISSTNC_SYMLINK, pt_slave.Name())
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Failed to create symlink %s: %s\n", TMP_KISSTNC_SYMLINK, symlinkErr)
		panic("")
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        kisspt_send_rec_packet
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
 *--------------------------------------------------------------------*/

func kisspt_send_rec_packet(channel C.int, kiss_cmd C.int, fbuf []byte, flen C.int, kps *kissport_status_s, client C.int) {
	if pt_master == nil {
		return
	}

	var kiss_buff []byte
	if flen < 0 {
		if kisspt_debug > 0 {
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

		if kisspt_debug >= 2 {
			/* AX.25 frame with the CRC removed. */
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("\n")
			dw_printf("Packet content before adding KISS framing and any escapes:\n")
			hex_dump(fbuf)
		}

		kiss_buff = kiss_encapsulate(stemp)

		/* This has KISS framing and escapes for sending to client app. */

		if kisspt_debug > 0 {
			kiss_debug_print(TO_CLIENT, "", kiss_buff)
		}
	}

	var n, err = pt_master.Write(kiss_buff)

	if n != len(kiss_buff) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("\nError sending KISS message to client application on pseudo terminal.  fd=%s, len=%d, write returned %d, err = %s\n\n",
			pt_master.Name(), len(kiss_buff), n, err)
	} else if err != nil /* TODO KG Need to test real behaviour here: && errno == EWOULDBLOCK */ {
		text_color_set(DW_COLOR_INFO)
		dw_printf("KISS SEND - Discarding message because no one is listening.\n")
		dw_printf("This happens when you use the -p option and don't read from the pseudo terminal.\n")
	}
} /* kisspt_send_rec_packet */

/*-------------------------------------------------------------------
 *
 * Name:        kisspt_get
 *
 * Purpose:     Read one byte from the KISS client app.
 *
 * Global In:	pt_master_fd
 *
 * Returns:	one byte (value 0 - 255) or terminate thread on error.
 *
 * Description:	There is room for improvement here.  Reading one byte
 *		at a time is inefficient.  We could read a large block
 *		into a local buffer and return a byte from that most of the time.
 *		Is it worth the effort?  I don't know.  With GHz processors and
 *		the low data rate here it might not make a noticeable difference.
 *
 *--------------------------------------------------------------------*/

func kisspt_get() byte {
	var ch []byte
	var n int

	for n == 0 {
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

		ch = make([]byte, 1)
		var err error
		n, err = pt_master.Read(ch)
		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("\nError receiving KISS message from client application.  Closing %s. %s\n\n", pt_slave.Name(), err)

			pt_master.Close()

			pt_master = nil
			os.Remove(TMP_KISSTNC_SYMLINK)
			// FIXME KG pthread_exit(NULL)
			return 0 // TODO KG
		}
	}

	return ch[0]
}

/*-------------------------------------------------------------------
 *
 * Name:        kisspt_listen_thread
 *
 * Purpose:     Read messages from serial port KISS client application.
 *
 * Global In:
 *
 * Description:	Reads bytes from the KISS client app and
 *		sends them to kiss_rec_byte for processing.
 *
 *--------------------------------------------------------------------*/

func kisspt_listen_thread() {
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("kisspt_listen_thread ( %d )\n", fd);
	#endif
	*/

	for {
		var ch = kisspt_get()
		kiss_rec_byte(kisspt_kf, C.uchar(ch), C.int(kisspt_debug), nil, -1, kisspt_send_rec_packet)
	}
}
