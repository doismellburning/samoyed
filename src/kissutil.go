package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Utility for talking to a KISS TNC.
 *
 * Description:	Convert between KISS format and usual text representation.
 *		This might also serve as the starting point for an application
 *		that uses a KISS TNC.
 *		The TNC can be attached by TCP or a serial port.
 *
 * Usage:	kissutil  [ options ]
 *
 *		Default is to connect to localhost:8001.
 *		See the "usage" functions at the bottom for details.
 *
 *---------------------------------------------------------------*/

// #include "direwolf.h"
// #include <stdlib.h>
// #include <errno.h>
// #include <sys/types.h>
// #include <sys/socket.h>
// #include <unistd.h>
// #include <stdio.h>
// #include <assert.h>
// #include <ctype.h>
// #include <stddef.h>
// #include <string.h>
// #include <getopt.h>
// #include <dirent.h>
// #include <sys/stat.h>
// #include "ax25_pad.h"
// #include "textcolor.h"
// #include "serial_port.h"
// #include "kiss_frame.h"
// #include "dwsock.h"
// #include "audio.h"		// for DEFAULT_TXDELAY, etc.
// #include "dtime_now.h"
// #define DIR_CHAR "/"
// void hex_dump (unsigned char *p, int len);
// extern int KISSUTIL;
import "C"

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unsafe"

	"github.com/spf13/pflag"
)

/* Obtained from the command line. */

var hostname = "localhost" /* -h option. */
/* DNS host name or IPv4 address. */
/* Some of the code is there for IPv6 but */
/* it needs more work. */
/* Defaults to "localhost" if not specified. */

var port = "8001" /* -p option. */
/* If it begins with a digit, it is considered */
/* a TCP port number at the hostname.  */
/* Otherwise, we treat it as a serial port name. */

var using_tcp = true /* Are we using TCP or serial port for TNC? */
/* Use corresponding one of the next two. */
/* This is derived from the first character of port. */

var server_sock net.Conn /* File descriptor for socket interface. */
/* Set to -1 if not used. */
/* (Don't use SOCKET type because it is unsigned.) */

var serial_fd = C.MYFDTYPE(-1) /* Serial port handle. */

var serial_speed = 9600 /* -s option. */
/* Serial port speed, bps. */

var verbose = false /* -v option. */
/* Display the KISS protocol in hexadecimal for troubleshooting. */

var transmit_from = "" /* -f option */
/* When specified, files are read from this directory */
/* rather than using stdin.  Each file is one or more */
/* lines in the standard monitoring format. */

var receive_output = "" /* -o option */
/* When specified, each received frame is stored as a file */
/* with a unique name here.  */
/* Directory must already exist; we won't create it. */

var timestamp_format = "" /* -T option */
/* Precede received frames with timestamp. */
/* Command line option uses "strftime" format string. */

/*------------------------------------------------------------------
 *
 * Name: 	main
 *
 * Purpose:   	Attach to KISS TNC and exchange information.
 *
 * Usage:	See "usage" functions at end.
 *
 *---------------------------------------------------------------*/

func KissUtilMain() {
	C.text_color_init(0) // Turn off text color.
	// It could interfere with trying to pipe stdout to some other application.

	C.setlinebuf(C.stdout)

	C.KISSUTIL = 1 // Change behaviour of kiss_process_msg
	KISS_PROCESS_MSG_OVERRIDE = Kissutil_kiss_process_msg

	/*
	 * Extract command line args.
	 */
	var _hostname = pflag.StringP("hostname", "h", "localhost", "Hostname of TCP KISS TNC")
	var _port = pflag.StringP("port", "p", "8001", "Port. If it does not start with a digit, it is treated as a serial port, e.g. /dev/ttyAM0")
	var _serialSpeed = pflag.IntP("serial-speed", "s", 9600, "Serial port speed")
	var _verbose = pflag.BoolP("verbose", "v", false, "Verbose. Show the KISS frame contents.")
	var _transmitFrom = pflag.StringP("transmit-from", "f", "", "Transmit files directory.  Process and delete files here.")
	var _receiveOutput = pflag.StringP("receive-output", "o", "", "Receive output queue directory.  Store received frames here.")
	var _timestampFormat = pflag.StringP("timestamp-format", "T", "", "Precede received frames with 'strftime' format time stamp.")
	var help = pflag.Bool("help", false, "Display help text.")

	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s - Utility for testing a KISS TNC.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Convert between KISS format and usual text representation.\n")
		fmt.Fprintf(os.Stderr, "The TNC can be attached by TCP or a serial port.\n")
		fmt.Fprintf(os.Stderr, "\n")
		pflag.PrintDefaults()
		usage2()
	}

	pflag.Parse()

	if *help {
		pflag.Usage()
		os.Exit(0)
	}

	// TODO Consider using the pflag binding functions?
	hostname = *_hostname
	port = *_port
	serial_speed = *_serialSpeed
	verbose = *_verbose
	transmit_from = *_transmitFrom
	receive_output = *_receiveOutput
	timestamp_format = *_timestampFormat

	/*
	 * If receive queue directory was specified, make sure that it exists.
	 */
	if len(receive_output) > 0 {
		var s, err = os.Stat(receive_output)

		if err != nil {
			fmt.Printf("Error with receive queue location %s: %s\n", receive_output, err)
			os.Exit(1)
		}

		if !s.IsDir() {
			fmt.Printf("Receive queue location, %s, is not a directory.\n", receive_output)
			os.Exit(1)
		}
	}

	/* If port begins with digit, consider it to be TCP. */
	/* Otherwise, treat as serial port name. */

	using_tcp = unicode.IsDigit(rune(port[0]))

	if using_tcp {
		go tnc_listen_net()
	} else {
		go tnc_listen_serial()
	}

	// Give the threads a little while to open the TNC connection before trying to use it.
	// This was a problem when the transmit queue already existed when starting up.

	SLEEP_MS(500)

	/*
	 * Process keyboard or other input source.
	 */
	if len(transmit_from) > 0 {
		/*
		 * Process and delete all files in specified directory.
		 * When done, sleep for a second and try again.
		 * This doesn't take them in any particular order.
		 * A future enhancement might sort by name or timestamp.
		 */

		var entries, err = os.ReadDir(transmit_from)
		if err != nil {
			panic(err)
		}

		for _, entry := range entries {
			var fname = entry.Name()
			fmt.Printf("Processing %s for transmit...\n", fname)

			var data, err = os.ReadFile(fname)
			if err != nil {
				panic(err)
			}

			process_input(string(data))

			err = os.Remove(fname)
			if err != nil {
				panic(err)
			}
		}
	} else {
		/*
		 * Using stdin.
		 */

		var scanner = bufio.NewScanner(os.Stdin)

		for scanner.Scan() {
			var stuff = scanner.Text()

			process_input(stuff)
		}
	}
} /* end main */

func parse_number(str string, de_fault int) int {
	str = strings.TrimSpace(str)

	if len(str) == 0 {
		fmt.Printf("Missing number for KISS command.  Using default %d.\n", de_fault)
		return de_fault
	}

	var n, _ = strconv.Atoi(str)
	if n < 0 || n > 255 { // must fit in a byte.
		fmt.Printf("Number for KISS command is out of range 0-255.  Using default %d.\n", de_fault)
		return de_fault
	}

	return n
}

/*-------------------------------------------------------------------
 *
 * Name:        process_input
 *
 * Purpose:     Process frames/commands from user, either interactively or from files.
 *
 * Inputs:	stuff		- A frame is in usual format like SOURCE>DEST,DIGI:whatever.
 *				  Commands begin with lower case letter.
 *				  Note that it can be modified by this function.
 *
 * Later Enhancement:	Return success/fail status.  The transmit queue processing might want
 *		to preserve files that were not processed as expected.
 *
 *--------------------------------------------------------------------*/

func process_input(stuff string) {
	/*
	 * Remove any end of line character(s).
	 */
	stuff = strings.TrimSpace(stuff)

	/*
	 * Optional prefix, like "[9]" or "[99]" to specify channel.
	 */
	var channel int
	if stuff[0] == '[' {
		var before, after, _ = strings.Cut(stuff, "]") // Could check found, but should be fine
		before = strings.TrimPrefix(before, "[")

		var err error
		channel, err = strconv.Atoi(before)

		if err != nil {
			fmt.Printf("ERROR! Channel number and ] was expected after [ at beginning of line.\n")
			usage2()
			return
		}
		if channel < 0 || channel > 15 {
			fmt.Printf("ERROR! KISS channel number must be in range of 0 thru 15.\n")
			usage2()
			return
		}

		stuff = strings.TrimSpace(after)
	}

	/*
	 * If it starts with upper case letter or digit, assume it is an AX.25 frame in monitor format.
	 * Lower case is a command (e.g.  Persistence or set Hardware).
	 * Anything else, print explanation of what is expected.
	 */
	if unicode.IsUpper(rune(stuff[0])) || unicode.IsNumber(rune(stuff[0])) {
		// Parse the "TNC2 monitor format" and convert to AX.25 frame.
		var frame_data [C.AX25_MAX_PACKET_LEN]C.uchar
		var pp = C.ax25_from_text(C.CString(stuff), 1)
		if pp != nil {
			C.ax25_pack(pp, &frame_data[0])
			send_to_kiss_tnc(channel, C.KISS_CMD_DATA_FRAME, []byte(C.GoString((*C.char)(unsafe.Pointer(&frame_data[0])))))
			C.ax25_delete(pp)
		} else {
			fmt.Printf("ERROR! Could not convert to AX.25 frame: %s\n", stuff)
		}
	} else if unicode.IsLower(rune(stuff[0])) {
		switch stuff[0] {
		case 'd': // txDelay, 10ms units
			var value = parse_number(stuff[1:], C.DEFAULT_TXDELAY)
			send_to_kiss_tnc(channel, C.KISS_CMD_TXDELAY, []byte{byte(value)})
		case 'p': // Persistence
			var value = parse_number(stuff[1:], C.DEFAULT_PERSIST)
			send_to_kiss_tnc(channel, C.KISS_CMD_PERSISTENCE, []byte{byte(value)})
		case 's': // Slot time, 10ms units
			var value = parse_number(stuff[1:], C.DEFAULT_SLOTTIME)
			send_to_kiss_tnc(channel, C.KISS_CMD_SLOTTIME, []byte{byte(value)})
		case 't': // txTail, 10ms units
			var value = parse_number(stuff[1:], C.DEFAULT_TXTAIL)
			send_to_kiss_tnc(channel, C.KISS_CMD_TXTAIL, []byte{byte(value)})
		case 'f': // Full duplex
			var value = parse_number(stuff[1:], 0)
			send_to_kiss_tnc(channel, C.KISS_CMD_FULLDUPLEX, []byte{byte(value)})
		case 'h': // set Hardware
			var p = strings.TrimSpace(stuff[1:])
			send_to_kiss_tnc(channel, C.KISS_CMD_SET_HARDWARE, []byte(p))
		default:
			fmt.Printf("Invalid command. Must be one of d p s t f h.\n")
			usage2()
		}
	} else {
		usage2()
	}
} /* end process_input */

/*-------------------------------------------------------------------
 *
 * Name:        send_to_kiss_tnc
 *
 * Purpose:     Encapsulate the data/command, into a KISS frame, and send to the TNC.
 *
 * Inputs:	channel	- channel number.
 *
 *		cmd	- KISS_CMD_DATA_FRAME, KISS_CMD_SET_HARDWARE, etc.
 *
 *		data	- Information for KISS frame.
 *
 * Description:	Encapsulate as KISS frame and send to TNC.
 *
 *--------------------------------------------------------------------*/

func send_to_kiss_tnc(channel int, cmd int, data []byte) {
	if channel < 0 || channel > 15 {
		fmt.Printf("ERROR - Invalid channel %d - must be in range 0 to 15.\n", channel)
		channel = 0
	}
	if cmd < 0 || cmd > 15 {
		fmt.Printf("ERROR - Invalid command %d - must be in range 0 to 15.\n", cmd)
		cmd = 0
	}

	var temp [C.AX25_MAX_PACKET_LEN]C.uchar // We don't limit to 256 info bytes.

	if len(data) > len(temp)-1 {
		fmt.Printf("ERROR - Invalid data length %d - must be in range 0 to %d.\n", len(data), len(temp)-1)
		data = data[:len(temp)-1]
	}

	temp[0] = C.uchar((channel << 4) | cmd)
	for i, b := range data {
		temp[i+1] = C.uchar(b)
	}

	var kissed [C.AX25_MAX_PACKET_LEN * 2]C.uchar
	var klen = C.kiss_encapsulate(&temp[0], C.int(len(data))+1, &kissed[0])

	if verbose {
		fmt.Printf("Sending to KISS TNC:\n")
		C.hex_dump(&kissed[0], klen)
	}

	if using_tcp {
		var kissedBytes = make([]byte, klen)
		for i := range klen {
			kissedBytes[i] = byte(kissed[i])
		}
		server_sock.Write(kissedBytes[:klen])
	} else {
		var rc = C.serial_port_write(serial_fd, (*C.char)(unsafe.Pointer(&kissed[0])), klen)
		if rc != klen {
			fmt.Printf("ERROR writing KISS frame to serial port.\n")
			// fmt.Printf ("DEBUG wanted %d, got %d\n", klen, rc);
		}
	}
} /* end send_to_kiss_tnc */

/*-------------------------------------------------------------------
 *
 * Name:        tnc_listen_net
 *
 * Purpose:     Connect to KISS TNC via TCP port.
 *		Print everything it sends to us.
 *
 * Global In:	host
 *		port
 *
 * Global Out:	server_sock	- Needed to send to the TNC.
 *
 *--------------------------------------------------------------------*/

func tnc_listen_net() {
	/*
	 * Connect to network KISS TNC.
	 */

	// For the IGate we would loop around and try to reconnect if the TNC
	// goes away.  We should probably do the same here.

	var conn, connErr = net.Dial("tcp", net.JoinHostPort(hostname, port))

	if connErr != nil {
		fmt.Printf("Unable to connect to %s on port %s: %s\n", hostname, port, connErr)
		os.Exit(1)
	}

	server_sock = conn

	/*
	 * Print what we get from TNC.
	 */
	var kstate C.kiss_frame_t
	for {
		var data = make([]byte, 4096)
		var length, err = server_sock.Read(data)

		if err != nil {
			fmt.Printf("Read error from TCP KISS TNC (%s).  Terminating.\n", err)
			os.Exit(1)
		}

		for j := range length {
			// Feed in one byte at a time.
			// kiss_process_msg is called when a complete frame has been accumulated.

			// When verbose is specified, we get debug output like this:
			//
			// <<< Data frame from KISS client application, port 0, total length = 46
			// 000:  c0 00 82 a0 88 ae 62 6a e0 ae 84 64 9e a6 b4 ff  ......bj...d....
			// ...
			// It says "from KISS client application" because it was written
			// on the assumption it was being used in only one direction.
			// Not worried enough about it to do anything at this time.

			var _verbose = C.int(0)
			if verbose {
				_verbose = 1
			}
			kiss_rec_byte(&kstate, C.uchar(data[j]), _verbose, nil, 0, nil)
		}
	}
} /* end tnc_listen_net */

/*-------------------------------------------------------------------
 *
 * Name:        tnc_listen_serial
 *
 * Purpose:     Connect to KISS TNC via serial port.
 *		Print everything it sends to us.
 *
 * Global In:	port
 *		serial_speed
 *
 * Global Out:	serial_fd	- Need for sending to the TNC.
 *
 *--------------------------------------------------------------------*/

func tnc_listen_serial() {
	var serial_fd = C.serial_port_open(C.CString(port), C.int(serial_speed))

	if serial_fd == C.MYFDERROR {
		fmt.Printf("Unable to connect to KISS TNC serial port %s.\n", port)
		// More detail such as "permission denied" or "no such device"
		os.Exit(1)
	}

	/*
	 * Read and print.
	 */
	var kstate C.kiss_frame_t
	for {
		var ch = C.serial_port_get1(serial_fd)

		if ch < 0 {
			fmt.Printf("Read error from serial port KISS TNC.\n")
			os.Exit(1)
		}

		// Feed in one byte at a time.
		// kiss_process_msg is called when a complete frame has been accumulated.

		var _verbose = C.int(0)
		if verbose {
			_verbose = 1
		}
		kiss_rec_byte(&kstate, C.uchar(ch), _verbose, nil, 0, nil)
	}
} /* end tnc_listen_serial */

/*-------------------------------------------------------------------
 *
 * Name:        kiss_process_msg
 *
 * Purpose:     Process a frame from the KISS TNC.
 *		This is called when a complete frame has been accumulated.
 *		In this case, we simply print it.
 *
 * Inputs:	kiss_msg	- Kiss frame with FEND and escapes removed.
 *				  The first byte contains channel and command.
 *
 *		kiss_len	- Number of bytes including the command.
 *
 *-----------------------------------------------------------------*/

func Kissutil_kiss_process_msg(_kiss_msg unsafe.Pointer, _kiss_len int) {
	var alevel C.alevel_t

	// Hacks to work around the dodgy C/Go/callback bodges I had to do
	var kiss_len = C.int(_kiss_len)
	var kiss_msg = unsafe.Slice((*C.uchar)(_kiss_msg), kiss_len)

	var channel = (kiss_msg[0] >> 4) & 0xf
	var cmd = kiss_msg[0] & 0xf

	switch cmd {
	case C.KISS_CMD_DATA_FRAME: /* 0 = Data Frame */
		var pp = C.ax25_from_frame(&kiss_msg[1], kiss_len-1, alevel)
		if pp == nil {
			fmt.Printf("ERROR - Invalid KISS data frame from TNC.\n")
		} else {
			var prefix string // Channel and optional timestamp.
			// Like [0] or [2 12:34:56]

			if len(timestamp_format) > 0 {
				prefix = fmt.Sprintf("[%d %s]", channel, time.Now().Format(timestamp_format)) // TODO Go's formatting is not strftime-y, so this breaks compatibility slightly, but eh
			} else {
				prefix = fmt.Sprintf("[%d]", channel)
			}

			var addrs [C.AX25_MAX_ADDRS * C.AX25_MAX_ADDR_LEN]C.char // Like source>dest,digi,...,digi:

			C.ax25_format_addrs(pp, &addrs[0])

			var pinfo *C.uchar
			var info_len = C.ax25_get_info(pp, &pinfo)

			fmt.Printf("%s %s", prefix, C.GoString(&addrs[0])) // [channel] Addresses followed by :

			// Safe print will replace any unprintable characters with
			// hexadecimal representation.

			C.ax25_safe_print((*C.char)(unsafe.Pointer(pinfo)), info_len, 0)
			fmt.Printf("\n")

			/*
			 * Add to receive queue directory if specified.
			 * File name will be based on current local time.
			 * If you want UTC, just set an environment variable like this:
			 *
			 *	TZ=UTC kissutil ...
			 */
			if len(receive_output) > 0 {
				var filename = timestamp_filename()
				var fullpath = filepath.Join(receive_output, filename)

				fmt.Printf("Save received frame to %s\n", fullpath)

				var content = fmt.Sprintf("%s %s%s\n", prefix, C.GoString(&addrs[0]), C.GoString((*C.char)(unsafe.Pointer(pinfo))))
				var err = os.WriteFile(fullpath, []byte(content), 0644)

				if err != nil {
					fmt.Printf("Unable to open for write: %s\n", fullpath)
				}
			}

			C.ax25_delete(pp)
		}

	case C.KISS_CMD_SET_HARDWARE: /* 6 = TNC specific */
		// Display as "h ..." for in/out symmetry.
		// Use safe print here?
		fmt.Printf("[%d] h %s\n", channel, C.GoString((*C.char)(unsafe.Pointer(&kiss_msg[1]))))
	/*
	 * The rest should only go TO the TNC and not come FROM it.
	 */
	default:
		fmt.Printf("Unexpected KISS command %d, channel %d\n", cmd, channel)
	}
} /* end kiss_process_msg */

func usage() {
	fmt.Printf("\n")
	fmt.Printf("kissutil  -  Utility for testing a KISS TNC.\n")
	fmt.Printf("\n")
	fmt.Printf("Convert between KISS format and usual text representation.\n")
	fmt.Printf("The TNC can be attached by TCP or a serial port.\n")
	fmt.Printf("\n")
	fmt.Printf("Usage:	kissutil  [ options ]\n")
	fmt.Printf("\n")
	fmt.Printf("	-h	hostname of TCP KISS TNC, default localhost.\n")
	fmt.Printf("	-p	port, default 8001.\n")
	fmt.Printf("		If it does not start with a digit, it is\n")
	fmt.Printf("		a serial port.  e.g.  /dev/ttyAMA0 or COM3.\n")
	fmt.Printf("	-s	Serial port speed, default 9600.\n")
	fmt.Printf("	-v	Verbose.  Show the KISS frame contents.\n")
	fmt.Printf("	-f	Transmit files directory.  Process and delete files here.\n")
	fmt.Printf("	-o	Receive output queue directory.  Store received frames here.\n")
	fmt.Printf("	-T	Precede received frames with 'strftime' format time stamp.\n")
	usage2()
}

// Used as both CLI help message and in-usage error reminder
func usage2() {
	fmt.Printf("\n")
	fmt.Printf("Input, starting with upper case letter or digit, is assumed\n")
	fmt.Printf("to be an AX.25 frame in the usual TNC2 monitoring format.\n")
	fmt.Printf("\n")
	fmt.Printf("Input, starting with a lower case letter is a command.\n")
	fmt.Printf("Whitespace, as shown in examples, is optional.\n")
	fmt.Printf("\n")
	fmt.Printf("	letter	meaning			example\n")
	fmt.Printf("	------	-------			-------\n")
	fmt.Printf("	d	txDelay, 10ms units	d 30\n")
	fmt.Printf("	p	Persistence		p 63\n")
	fmt.Printf("	s	Slot time, 10ms units	s 10\n")
	fmt.Printf("	t	txTail, 10ms units	t 5\n")
	fmt.Printf("	f	Full duplex		f 0\n")
	fmt.Printf("	h	set Hardware 		h TNC:\n")
	fmt.Printf("\n")
	fmt.Printf("	Lines may be preceded by the form \"[9]\" to indicate a\n")
	fmt.Printf("	channel other than the default 0.\n")
	fmt.Printf("\n")
}

/*------------------------------------------------------------------
 *
 * Name:	timestamp_filename
 *
 * Purpose:   	Generate unique file name based on the current time.
 *		The format will be:
 *
 *			YYYYMMDD-HHMMSS-mmm
 *
 * Output:	result		- Result is placed here.
 *
 * Description:	This is for the kissutil "-r" option which places
 *		each received frame in a new file.  It is possible to
 *		have two packets arrive in less than a second so we
 *		need more than one second resolution.
 *
 *		What if someone wants UTC, rather than local time?
 *		You can simply set an environment variable like this:
 *
 *			TZ=UTC direwolf
 *
 *		so it's probably not worth the effort to add another
 *		option.
 *
 *---------------------------------------------------------------*/
func timestamp_filename() string {
	var t = time.Now()

	var s = t.Format("20060102-150405")           // KG: I do not enjoy Go's time formatting
	s += fmt.Sprintf("-%03d", t.UnixMilli()%1000) // Ditto

	return s
} /* end timestamp_filename */
