package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Interface to serial port, hiding operating system differences.
 *
 *---------------------------------------------------------------*/

import (
	"github.com/pkg/term"
)

/*-------------------------------------------------------------------
 *
 * Name:	serial_port_open
 *
 * Purpose:	Open serial port.
 *
 * Inputs:	devicename	- For Windows, usually like COM5.
 *				  For Linux, usually /dev/tty...
 *				  "COMn" also allowed and converted to /dev/ttyS(n-1)
 *				  Could be /dev/rfcomm0 for Bluetooth.
 *
 *		baud		- Speed.  1200, 4800, 9600 bps, etc.
 *				  If 0, leave it alone.
 *
 * Returns 	Handle for serial port
 *
 *---------------------------------------------------------------*/

func serial_port_open(devicename string, baud int) *term.Term {

	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("serial_port_open ( '%s' )\n", devicename);
	#endif
	*/

	/* Translate Windows device name into Linux name. */
	/* COM1 -> /dev/ttyS0, etc. */

	/* TODO KG Why??
	strlcpy(linuxname, devicename, sizeof(linuxname))

	if strncasecmp(devicename, "COM", 3) == 0 {
		var n = atoi(devicename + 3)
		text_color_set(DW_COLOR_INFO)
		dw_printf("Converted serial port name '%s'", devicename)
		if n < 1 {
			n = 1
		}
		snprintf(linuxname, sizeof(linuxname), "/dev/ttyS%d", n-1)
		dw_printf(" to Linux equivalent '%s'\n", linuxname)
	}
	*/
	var linuxname = devicename

	var fd, err = term.Open(linuxname, term.RawMode)

	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR - Could not open serial port %s: %s.\n", linuxname, err)
		return nil
	}

	// TODO KG Confirm? ts.c_cc[VMIN] = 1  /* wait for at least one character */
	// TODO KG Confirm? ts.c_cc[VTIME] = 0 /* no fancy timing. */

	switch baud {
	case 0: /* Leave it alone. */
	case 1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200:
		fd.SetSpeed(baud)
	default:
		text_color_set(DW_COLOR_ERROR)
		dw_printf("serial_port_open: Unsupported speed %d.  Using 4800.\n", baud)
		fd.SetSpeed(4800)
	}

	/* TODO KG Confirm?
	e = tcsetattr(fd, TCSANOW, &ts)
	if e != 0 {
		perror("tcsetattr")
	}
	*/

	//text_color_set(DW_COLOR_INFO);
	//dw_printf("Successfully opened serial port %s.\n", devicename);

	return (fd)
}

/*-------------------------------------------------------------------
 *
 * Name:	serial_port_write
 *
 * Purpose:	Send characters to serial port.
 *
 * Inputs:	fd	- Handle from open.
 *		data	- Slice of bytes.
 *
 * Returns 	Number of bytes written.  Should be the same as len.
 *		-1 if error.
 *
 *---------------------------------------------------------------*/

func serial_port_write(fd *term.Term, data []byte) int {

	if fd == nil {
		return (-1)
	}

	var written, err = fd.Write(data)
	if written != len(data) || err != nil {
		// Do we want this message here?
		// Or rely on caller to check and provide something more meaningful for the usage?
		//text_color_set(DW_COLOR_ERROR);
		//dw_printf ("Error writing to serial port. err=%d\n\n", written);
		return (-1)
	}

	return written
} /* serial_port_write */

/*-------------------------------------------------------------------
 *
 * Name:        serial_port_get1
 *
 * Purpose:     Get one byte from the serial port.  Wait if not ready.
 *
 * Inputs:	fd	- Handle from open.
 *
 * Returns:	Value of byte in range of 0 to 255.
 *
 *--------------------------------------------------------------------*/

func serial_port_get1(fd *term.Term) (byte, error) {

	var bytes = make([]byte, 1)
	var n, err = fd.Read(bytes)

	if n != 1 {
		//text_color_set(DW_COLOR_DEBUG);
		//dw_printf ("serial_port_get1(%d) returns -1 for error.\n", fd);
		return 0, err
	}

	/* TODO KG
	   #if DEBUGx
	   	text_color_set(DW_COLOR_DEBUG);
	   	if (isprint(ch)) {
	   	  dw_printf ("serial_port_get1(%d) returns 0x%02x = '%c'\n", fd, ch, ch);
	   	}
	   	else {
	   	  dw_printf ("serial_port_get1(%d) returns 0x%02x\n", fd, ch);
	   	}
	   #endif
	*/

	return bytes[0], nil
}

/*-------------------------------------------------------------------
 *
 * Name:        serial_port_close
 *
 * Purpose:     Close the device.
 *
 * Inputs:	fd	- Handle from open.
 *
 * Returns:	None.
 *
 *--------------------------------------------------------------------*/

func serial_port_close(fd *term.Term) {
	if fd == nil {
		return
	}
	fd.Close()
}

/* end serial_port.c */
