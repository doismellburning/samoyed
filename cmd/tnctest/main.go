/* Test AX.25 connected mode between two TNCs */
package main

// #include "direwolf.h"
// #include <stdlib.h>
// #include <stddef.h>
// #include <netdb.h>
// #include <sys/types.h>
// #include <sys/ioctl.h>
// #include <sys/socket.h>
// #include <arpa/inet.h>
// #include <netinet/in.h>
// #include <netinet/tcp.h>
// #include <fcntl.h>
// //#include <termios.h>
// #include <sys/errno.h>
// #include <unistd.h>
// #include <stdio.h>
// #include <assert.h>
// #include <ctype.h>
// #include <string.h>
// #include <time.h>
// #include "textcolor.h"
// #include "dtime_now.h"
// #include "serial_port.h"
// #cgo CFLAGS: -I../../src -I../../external/geotranz -I../../external/misc -DMAJOR_VERSION=0 -DMINOR_VERSION=0
// #cgo LDFLAGS: -lm
import "C"

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"unicode"

	direwolf "github.com/doismellburning/samoyed/src" // Pulls this in for cgo
)

/*------------------------------------------------------------------
 *
 * Purpose:   	Test AX.25 connected mode between two TNCs.
 *
 * Description:	The first TNC will connect to the second TNC and send a bunch of data.
 *		Proper transfer of data will be verified.
 *
 * Usage:	tnctest  [options]  port0=name0  port1=name1
 *
 * Example:	tnctest  localhost:8000=direwolf  COM1=KPC-3+
 *
 *		Each port can have the following forms:
 *
 *		* host-name:tcp-port
 *		* ip-addr:tcp-port
 *		* tcp-port
 *		* serial port name (e.g.  COM1, /dev/ttyS0)
 *
 *---------------------------------------------------------------*/

/*
Quick running notes:

$ cat dw1.conf
ADEVICE plughw:Loopback,0,1 plughw:Loopback,1,0
AGWPORT 5001

$ cat dw2.conf
ADEVICE plughw:Loopback,0,0 plughw:Loopback,1,1
AGWPORT 5002

$ direwolf -t 0 -d k -c dw1.conf &
$ direwolf -t 0 -d k -c dw2.conf &

$ ./tnctest localhost:5001=dw1 localhost:5002=dw2
*/

/* We don't deal with big-endian processors here. */
/* TODO: Use agwlib (which did not exist when this was written) */
/* rather than duplicating the effort here. */

type agwpe_s struct {
	Portx        byte
	Reserved1    byte
	Reserved2    byte
	Reserved3    byte
	DataKind     byte
	Reserved4    byte
	PID          byte
	Reserved5    byte
	CallFrom     [10]byte
	CallTo       [10]byte
	DataLen      uint32
	UserReserved [4]byte
}

/*------------------------------------------------------------------
 *
 * Name: 	main
 *
 * Purpose:   	Basic test for connected AX.25 data mode between TNCs.
 *
 * Usage:	Described above.
 *
 *---------------------------------------------------------------*/

const MAX_TNC = 2 // Just 2 for now.
// Could be more later for multiple concurrent connections.

/* Obtained from the command line. */

var num_tnc int /* How many TNCs for this test? */
/* Initially only 2 but long term we might */
/* enhance it to allow multiple concurrent connections. */

var hostname [MAX_TNC]string /* DNS host name or IPv4 address. */
/* Some of the code is there for IPv6 but */
/* needs more work. */
/* Defaults to "localhost" if not specified. */

var port [MAX_TNC]string /* If it begins with a digit, it is considered */
/* a TCP port number at the hostname.  */
/* Otherwise, we treat it as a serial port name. */

var description [MAX_TNC]string /* Name used in the output. */

var using_tcp [MAX_TNC]bool /* Are we using TCP or serial port for each TNC? */
/* Use corresponding one of the next two. */

var server_sock [MAX_TNC]net.Conn /* File descriptor for AGW socket interface. */
/* Set to -1 if not used. */
/* (Don't use SOCKET type because it is unsigned.) */

var serial_fd [MAX_TNC]C.int /* Serial port handle. */

var busy [MAX_TNC]bool /* True when TNC busy and can't accept more data. */
/* For serial port, this is set by XON / XOFF characters. */

var XOFF = 0x13
var XON = 0x11

var tnc_address [MAX_TNC]string /* Name of the TNC used in the frames.  Originally, this */
/* was simply TNC0 and TNC1 but that can get hard to read */
/* and confusing.   Later used DW0, DW1, for direwolf */
/* so the direction of flow is easier to grasp. */

var LINE_WIDTH = 80

//#define LINE_WIDTH 120				/* If I was more ambitious I might try to get */
/* this from the terminal properties. */

var column_width int /* Line width divided by number of TNCs. */

/*
 * Current state for each TNC.
 */

var is_connected [MAX_TNC]int = [MAX_TNC]int{-1, -1} /* -1 = not yet available. */
/* 0 = not connected. */
/* 1 = not connected. */

var have_cmd_prompt [MAX_TNC]bool /* Set if "cmd:" was the last thing seen. */

var last_rec_seq [MAX_TNC]int /* Each data packet will contain a sequence number. */
/* This is used to verify that all have been */
/* received in the correct order. */

/*
 * Start time so we can print relative elapsed time.
 */

var start_dtime C.double

var max_count int

const ETX_BREAK = "\003\003\003"

func main() {
	// max_count = 20;
	max_count = 200
	// max_count = 6;
	max_count = 1000
	max_count = 9999

	start_dtime = C.dtime_monotonic()

	/*
	 * Extract command line args.
	 */
	num_tnc = len(os.Args) - 1

	if num_tnc < 2 || num_tnc > MAX_TNC {
		fmt.Printf("Specify minimum 2, maximum %d TNCs on the command line.\n", MAX_TNC)
		os.Exit(1)
	}

	column_width = LINE_WIDTH / num_tnc

	for j := range num_tnc {
		/* Each command line argument should be of the form "port=description." */
		var parts = strings.Split(os.Args[j+1], "=")
		if len(parts) != 2 {
			fmt.Printf("Internal error 1\n")
			os.Exit(1)
		}
		hostname[j] = "localhost"
		port[j] = parts[0]
		description[j] = parts[1]

		/* If the port contains ":" split it into hostname (or addr) and port number. */
		/* Haven't thought about IPv6 yet. */

		var portParts = strings.Split(port[j], ":")
		if len(portParts) > 1 {
			hostname[j] = portParts[0]
			port[j] = portParts[1]
		}
	}

	for j := range num_tnc {
		/* If port begins with digit, consider it to be TCP. */
		/* Otherwise, treat as serial port name. */

		using_tcp[j] = unicode.IsDigit(rune(port[j][0]))

		/* Addresses to use in the AX.25 frames. */

		if using_tcp[j] {
			tnc_address[j] = fmt.Sprintf("DW%d", j)
		} else {
			tnc_address[j] = fmt.Sprintf("TNC%d", j)
		}

		var e int
		if using_tcp[j] {
			go tnc_thread_net(j)
		} else {
			go tnc_thread_serial(j)
		}

		if e != 0 {
			fmt.Print("Internal error: Could not create TNC thread.")
			os.Exit(1)
		}
	}

	/*
	 * Wait until all TNCs are available.
	 */

	for ready := false; !ready; {
		direwolf.SLEEP_MS(100)
		ready = true
		for j := range num_tnc {
			if is_connected[j] < 0 {
				ready = false
			}
		}
	}

	fmt.Printf("Andiamo!\n")

	/*
	 * First, establish a connection from TNC number 0 to the other(s).
	 * Wait until successful.
	 */

	fmt.Printf("Trying to establish connection...\n")

	tnc_connect(0, 1)

	var timeout = 600
	for ready := false; !ready && timeout > 0; {
		direwolf.SLEEP_MS(100)
		timeout--
		ready = true
		for j := range num_tnc {
			if is_connected[j] <= 0 {
				ready = false
			}
		}
	}

	if timeout == 0 {
		fmt.Printf("ERROR: Gave up waiting for connect!\n")
		tnc_disconnect(1, 0) // Tell other TNC.
		direwolf.SLEEP_MS(5000)
		fmt.Printf("TEST FAILED!\n")
		os.Exit(1)
	}

	/*
	 * Send data.
	 */

	direwolf.SLEEP_MS(2000)
	direwolf.SLEEP_MS(2000)

	fmt.Printf("Send data...\n")

	var send_count int
	var burst_size = 1
	for send_count < max_count {
		for n := 1; n <= burst_size && send_count < max_count; n++ {
			send_count++
			var data = fmt.Sprintf("%04d send data\r", send_count)
			tnc_send_data(0, 1, data)
		}

		direwolf.SLEEP_MS(3000 + 1000*burst_size)
		// direwolf.SLEEP_MS(3000 + 500 * burst_size);		// OK for low error rate
		// direwolf.SLEEP_MS(3000 + 3000 * burst_size);
		// direwolf.SLEEP_MS(3000);

		burst_size++
	}

	/*
	 * Hang around until we get last expected reply or there is too much time with no activity.
	 */

	var last0 = last_rec_seq[0]
	var last1 = last_rec_seq[1]
	var no_activity = 0
	var INACTIVE_TIMEOUT = 120

	for last_rec_seq[0] != max_count && no_activity < INACTIVE_TIMEOUT {
		direwolf.SLEEP_MS(1000)
		no_activity++

		if last_rec_seq[0] > last0 {
			last0 = last_rec_seq[0]
			no_activity = 0
		}
		if last_rec_seq[1] > last1 {
			last1 = last_rec_seq[1]
			no_activity = 0
		}
	}

	var errors = 0

	if last_rec_seq[0] == max_count {
		fmt.Printf("Got last expected reply.\n")
	} else {
		fmt.Printf("ERROR: Timeout - No incoming activity for %d seconds.\n", no_activity)
		errors++
	}

	/*
	 * Did we get all expected replies?
	 */
	if last_rec_seq[0] != max_count {
		fmt.Printf("ERROR: Last received reply was %d when we were expecting %d.\n", last_rec_seq[0], max_count)
		errors++
	}

	/*
	 * Ask for disconnect.  Wait until complete.
	 */

	tnc_disconnect(0, 1)

	timeout = 200 // 20 sec should be generous.
	for ready := false; !ready && timeout > 0; {
		direwolf.SLEEP_MS(100)
		timeout--
		ready = true
		for j := range num_tnc {
			if is_connected[j] != 0 {
				ready = false
			}
		}
	}

	if timeout == 0 {
		fmt.Printf("ERROR: Gave up waiting for disconnect!\n")
		tnc_reset(1, 0) // Don't leave TNC in bad state for next time.
		direwolf.SLEEP_MS(10000)
		errors++
	}

	if errors != 0 {
		fmt.Printf("TEST FAILED!\n")
		os.Exit(1)
	}
	fmt.Printf("Success!\n")
	os.Exit(0)
}

/*-------------------------------------------------------------------
 *
 * Name:        process_rec_data
 *
 * Purpose:     Look for our data with text sequence numbers, not to be
 *		confused with the AX.25 I frame sequence numbers.
 *
 * Inputs:	my_index	- 0 for the call originator.
 *				  >1 for the other end which answers.
 *
 *		data		- Should look something like this:
 *				   9999 send data
 *				   9999 reply
 *
 * Global In/Out:	last_rec_seq[my_index]
 *
 * Description:	Look for expected format.
 *		Extract the sequence number.
 *		Verify that it is the next expected one.
 *		Update it.
 *
 *--------------------------------------------------------------------*/

func process_rec_data(my_index int, data string) {
	data = strings.TrimSpace(data) // Remove trailing \r
	var before, after, _ = strings.Cut(data, " ")

	if strings.HasPrefix(after, "send") {
		if my_index > 0 {
			last_rec_seq[my_index]++

			var n, _ = strconv.Atoi(before)
			if n != last_rec_seq[my_index] {
				fmt.Printf("%*s%s: Received %d when %d was expected (%s).\n", my_index*column_width, "", tnc_address[my_index], n, last_rec_seq[my_index], data)
				direwolf.SLEEP_MS(10000)
				fmt.Printf("TEST FAILED!\n")
				os.Exit(1)
			}
		}
	} else if strings.HasPrefix(after, "reply") {
		if my_index == 0 {
			last_rec_seq[my_index]++
			var n, _ = strconv.Atoi(before)
			if n != last_rec_seq[my_index] {
				fmt.Printf("%*s%s: Received %d when %d was expected.\n", my_index*column_width, "", tnc_address[my_index], n, last_rec_seq[my_index])
				direwolf.SLEEP_MS(10000)
				fmt.Printf("TEST FAILED!\n")
				os.Exit(1)
			}
		}
	} else if strings.HasPrefix(data, "A") {
		if !strings.HasPrefix("ABCDEFGHIJKLMNOPQRSTUVWXYZ", data) { //nolint:gocritic
			fmt.Printf("%*s%s: Segmentation is broken.\n", my_index*column_width, "", tnc_address[my_index])
			direwolf.SLEEP_MS(10000)
			fmt.Printf("TEST FAILED!\n")
			os.Exit(1)
		}
	} else {
		panic("Unexpected data: " + data)
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        tnc_thread_net
 *
 * Purpose:     Establish connection with a TNC via network.
 *
 * Inputs:	arg		- My instance index, 0 thru MAX_TNC-1.
 *
 * Outputs:	packets		- Received packets are put in the corresponding column
 *				  and sent to a common function to check that they
 *				  all arrived in order.
 *
 * Global Out:	is_connected	- Updated when connected/disconnected notfications are received.
 *
 * Description:	Perform any necessary configuration for the TNC then wait
 *		for responses and process them.
 *
 *--------------------------------------------------------------------*/

var MAX_HOSTS = 30

func tnc_thread_net(arg int) {
	var my_index = arg

	/*
	 * Connect to TNC server.
	 */

	var conn, connErr = net.Dial("tcp4", net.JoinHostPort(hostname[my_index], port[my_index]))

	if connErr != nil {
		fmt.Printf("TNC %d unable to connect to %s on %s, port %s: %s\n",
			my_index, description[my_index], hostname[my_index], port[my_index], connErr)
		os.Exit(1)
	}

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
	}

	server_sock[my_index] = conn

	var mon_cmd *agwpe_s

	/*
	 * Send command to toggle reception of frames in raw format.
	 */
	mon_cmd = new(agwpe_s)
	mon_cmd.DataKind = 'k'

	binary.Write(conn, binary.LittleEndian, mon_cmd)

	/*
	 * Send command to register my callsign for incoming connect request.
	 * Not really needed when we initiate the connection.
	 */

	mon_cmd = new(agwpe_s)
	mon_cmd.DataKind = 'X'
	copy(mon_cmd.CallFrom[:], tnc_address[my_index])

	binary.Write(conn, binary.LittleEndian, mon_cmd)

	/*
	 * Inform main program and observer that we are ready to go.
	 */
	fmt.Printf("TNC %d now available.  %s on %s, port %s\n",
		my_index, description[my_index], hostname[my_index], port[my_index])
	is_connected[my_index] = 0

	/*
	 * Print what we get from TNC.
	 */

	for {
		var readErr = binary.Read(conn, binary.LittleEndian, mon_cmd)

		if readErr != nil {
			if readErr == io.EOF {
				continue
			}
			fmt.Printf("Read error, TNC %d got %s.\n", my_index, readErr)
			os.Exit(1)
		}

		var data = make([]byte, mon_cmd.DataLen)
		if mon_cmd.DataLen > 0 {
			_, readErr = io.ReadFull(conn, data)
			if readErr != nil {
				fmt.Printf("Read error, TNC %d got %s reading data.\n", my_index, readErr)
				os.Exit(1)
			}
		}

		/*
		 * What did we get?
		 */

		var dnow = C.dtime_monotonic()

		switch mon_cmd.DataKind {
		case 'C': // AX.25 Connection Received
			fmt.Printf("%*s[R %.3f] *** Connected to %s ***\n", my_index*column_width, "", dnow-start_dtime, mon_cmd.CallFrom)
			is_connected[my_index] = 1
		case 'D': // Connected AX.25 Data
			fmt.Printf("%*s[R %.3f] %s\n", my_index*column_width, "", dnow-start_dtime, data)

			process_rec_data(my_index, string(data))

			var before, after, _ = strings.Cut(string(data), " ")

			if unicode.IsDigit(rune(before[0])) && unicode.IsDigit(rune(before[1])) && unicode.IsDigit(rune(before[2])) && unicode.IsDigit(rune(before[3])) &&
				strings.HasPrefix(after, "send") {
				// Expected message.   Make sure it is expected sequence and send reply.
				var n, _ = strconv.Atoi(before)
				var reply = fmt.Sprintf("%04d reply\r", n)
				tnc_send_data(my_index, 1-my_index, reply)

				// HACK!
				// It gets very confusing because N(S) and N(R) are very close.
				// Send a couple dozen I frames so they will be easier to distinguish visually.
				// Currently don't have the same in serial port version.

				// We change the length each time to test segmentation.
				// Set PACLEN to some very small number like 5.

				if n == 1 && max_count > 1 {
					for j := 1; j <= 26; j++ {
						var reply = fmt.Sprintf("%.*s\r", j, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
						tnc_send_data(my_index, 1-my_index, reply)
					}
				}
			}
		case 'd': // Disconnected
			fmt.Printf("%*s[R %.3f] *** Disconnected from %s ***\n", my_index*column_width, "", dnow-start_dtime, mon_cmd.CallFrom)
			is_connected[my_index] = 0
		case 'y': // Outstanding frames waiting on a Port
			fmt.Printf("%*s[R %.3f] *** Outstanding frames waiting %d ***\n", my_index*column_width, "", dnow-start_dtime, 123) // TODO
		default:
			// fmt.Printf("%*s[R %.3f] --- Ignoring cmd kind '%c' ---\n", my_index*column_width, "", dnow-start_dtime, mon_cmd.DataKind);
		}
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        tnc_thread_serial
 *
 * Purpose:     Establish connection with a TNC via serial port.
 *
 * Inputs:	arg		- My instance index, 0 thru MAX_TNC-1.
 *
 * Outputs:	packets		- Received packets are put in the corresponding column
 *				  and sent to a common function to check that they
 *				  all arrived in order.
 *
 * Global Out:	is_connected	- Updated when connected/disconnected notfications are received.
 *
 * Description:	Perform any necessary configuration for the TNC then wait
 *		for responses and process them.
 *
 *--------------------------------------------------------------------*/

var MYFDERROR = C.int(-1)

func tnc_thread_serial(arg int) {
	var my_index = arg

	serial_fd[my_index] = C.serial_port_open(C.CString(port[my_index]), 9600)

	if serial_fd[my_index] == MYFDERROR {
		fmt.Printf("TNC %d unable to connect to %s on %s.\n", my_index, description[my_index], port[my_index])
		os.Exit(1)
	}

	/*
	 * Make sure we are in command mode.
	 */

	var cmd string

	cmd = "\003\rreset\r"
	C.serial_port_write(serial_fd[my_index], C.CString(cmd), C.int(len(cmd)))
	direwolf.SLEEP_MS(3000)

	cmd = "echo on\r"
	C.serial_port_write(serial_fd[my_index], C.CString(cmd), C.int(len(cmd)))
	direwolf.SLEEP_MS(200)

	// do any necessary set up here. such as setting mycall

	cmd = fmt.Sprintf("mycall %s\r", tnc_address[my_index])
	C.serial_port_write(serial_fd[my_index], C.CString(cmd), C.int(len(cmd)))
	direwolf.SLEEP_MS(200)

	// Don't want to stop tty output when typing begins.

	cmd = "flow off\r"
	C.serial_port_write(serial_fd[my_index], C.CString(cmd), C.int(len(cmd)))

	cmd = "echo off\r"
	C.serial_port_write(serial_fd[my_index], C.CString(cmd), C.int(len(cmd)))

	/* Success. */

	fmt.Printf("TNC %d now available.  %s on %s\n", my_index, description[my_index], port[my_index])
	is_connected[my_index] = 0

	/*
	 * Read and print.
	 */

	for {
		var ch C.int
		var result [500]C.char
		var length int

		var done = false
		for !done {
			ch = C.serial_port_get1(serial_fd[my_index])

			if ch < 0 {
				fmt.Printf("TNC %d fatal read error.\n", my_index)
				os.Exit(1)
			}

			if ch == '\r' || ch == '\n' {
				done = true
			} else if ch == C.int(XOFF) {
				var dnow = C.dtime_monotonic()
				fmt.Printf("%*s[R %.3f] <XOFF>\n", my_index*column_width, "", dnow-start_dtime)
				busy[my_index] = true
			} else if ch == C.int(XON) {
				var dnow = C.dtime_monotonic()
				fmt.Printf("%*s[R %.3f] <XON>\n", my_index*column_width, "", dnow-start_dtime)
				busy[my_index] = false
			} else if C.isprint(ch) == 0 {
				result[length] = C.char(ch)
				length++
			} else {
				var hex = fmt.Sprintf("<x%02x>", ch)
				C.strcat(&result[0], C.CString(hex))
				length = len(result)
			}

			if C.GoString(&result[0]) == "cmd:" {
				done = true
				have_cmd_prompt[my_index] = true
			} else {
				have_cmd_prompt[my_index] = false
			}
		}

		var _result = C.GoString(&result[0])

		if length > 0 {
			var dnow = C.dtime_monotonic()

			fmt.Printf("%*s[R %.3f] %s\n", my_index*column_width, "", dnow-start_dtime, _result)

			if _result == "*** CONNECTED" {
				is_connected[my_index] = 1
			}

			if _result == "*** DISCONNECTED" {
				is_connected[my_index] = 0
			}

			if _result == "Not while connected" {
				// Not expecting this.
				// What to do?
				panic("???")
			}

			process_rec_data(my_index, _result)

			var before, after, _ = strings.Cut(_result, " ")

			if unicode.IsDigit(rune(before[0])) && unicode.IsDigit(rune(before[1])) && unicode.IsDigit(rune(before[2])) && unicode.IsDigit(rune(before[3])) &&
				strings.HasPrefix(after, "send") {
				// Expected message.   Make sure it is expected sequence and send reply.
				var n, _ = strconv.Atoi(before)
				var reply = fmt.Sprintf("%04d reply\r", n)
				tnc_send_data(my_index, 1-my_index, reply)
			}
		}
	}
}

func tnc_connect(from int, to int) {
	var dnow = C.dtime_monotonic()

	fmt.Printf("%*s[T %.3f] *** Send connect request ***\n", from*column_width, "", dnow-start_dtime)

	if using_tcp[from] {
		var cmd agwpe_s

		cmd.DataKind = 'C'
		copy(cmd.CallFrom[:], tnc_address[from])
		copy(cmd.CallTo[:], tnc_address[to])

		binary.Write(server_sock[from], binary.LittleEndian, cmd)
	} else {
		if !have_cmd_prompt[from] {
			var cmd string

			direwolf.SLEEP_MS(1500)

			cmd = ETX_BREAK
			C.serial_port_write(serial_fd[from], C.CString(cmd), C.int(len(cmd)))
			direwolf.SLEEP_MS(1500)

			cmd = "\r"
			C.serial_port_write(serial_fd[from], C.CString(cmd), C.int(len(cmd)))
			direwolf.SLEEP_MS(200)
		}

		var cmd = fmt.Sprintf("connect %s\r", tnc_address[to])
		C.serial_port_write(serial_fd[from], C.CString(cmd), C.int(len(cmd)))
	}
}

func tnc_disconnect(from int, to int) {
	var dnow = C.dtime_monotonic()

	fmt.Printf("%*s[T %.3f] *** Send disconnect request ***\n", from*column_width, "", dnow-start_dtime)

	if using_tcp[from] {
		var cmd agwpe_s

		cmd.DataKind = 'd'
		copy(cmd.CallFrom[:], tnc_address[from])
		copy(cmd.CallTo[:], tnc_address[to])

		binary.Write(server_sock[from], binary.LittleEndian, cmd)
	} else {
		if !have_cmd_prompt[from] {
			var cmd string

			direwolf.SLEEP_MS(1500)

			cmd = ETX_BREAK
			C.serial_port_write(serial_fd[from], C.CString(cmd), C.int(len(cmd)))
			direwolf.SLEEP_MS(1500)

			cmd = "\r"
			C.serial_port_write(serial_fd[from], C.CString(cmd), C.int(len(cmd)))
			direwolf.SLEEP_MS(200)
		}

		var cmd = "disconnect\r"
		C.serial_port_write(serial_fd[from], C.CString(cmd), C.int(len(cmd)))
	}
}

func tnc_reset(from int, to int) {
	var dnow = C.dtime_monotonic()

	fmt.Printf("%*s[T %.3f] *** Send reset ***\n", from*column_width, "", dnow-start_dtime)

	if using_tcp[from] {
	} else {
		var cmd string

		direwolf.SLEEP_MS(1500)

		cmd = ETX_BREAK
		C.serial_port_write(serial_fd[from], C.CString(cmd), C.int(len(cmd)))
		direwolf.SLEEP_MS(1500)

		cmd = "\r"
		C.serial_port_write(serial_fd[from], C.CString(cmd), C.int(len(cmd)))
		direwolf.SLEEP_MS(200)

		cmd = "reset\r"
		C.serial_port_write(serial_fd[from], C.CString(cmd), C.int(len(cmd)))
	}
}

func tnc_send_data(from int, to int, data string) {
	var dnow = C.dtime_monotonic()

	fmt.Printf("%*s[T %.3f] %s\n", from*column_width, "", dnow-start_dtime, data)

	if using_tcp[from] {
		var header agwpe_s

		header.DataKind = 'D'
		header.PID = 0xf0

		copy(header.CallFrom[:], tnc_address[from])
		copy(header.CallTo[:], tnc_address[to])

		header.DataLen = uint32(len(data))

		binary.Write(server_sock[from], binary.LittleEndian, header)

		server_sock[from].Write([]byte(data))
	} else {
		// The assumption is that we are in CONVERSE mode.
		// The data should be terminated by carriage return.

		var timeout = 600 // 60 sec.  I've seen it take more than 20.
		for timeout > 0 && busy[from] {
			direwolf.SLEEP_MS(100)
			timeout--
		}
		if timeout == 0 {
			fmt.Printf("ERROR: Gave up waiting while TNC busy.\n")
			tnc_disconnect(0, 1)
			direwolf.SLEEP_MS(5000)
			fmt.Printf("TEST FAILED!\n")
			os.Exit(1)
		} else {
			C.serial_port_write(serial_fd[from], C.CString(data), C.int(len(data)))
		}
	}
}
