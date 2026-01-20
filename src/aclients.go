package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Multiple concurrent APRS clients for comparing
 *		TNC demodulator performance.
 *
 * Description:	Establish connection with multiple servers and
 *		compare results side by side.
 *
 * Usage:	aclients port1=name1 port2=name2 ...
 *
 * Example:	aclients  8000=AGWPE  192.168.1.64:8002=DireWolf  COM1=D710A
 *
 *		This will connect to multiple physical or virtual
 *		TNCs, read packets from them, and display results.
 *
 *		Each port can have the following forms:
 *
 *		* host-name:tcp-port
 *		* ip-addr:tcp-port
 *		* tcp-port
 *		* serial port name (e.g.  COM1, /dev/ttyS0)
 *
 *---------------------------------------------------------------*/

// #include <stdlib.h>
// #include <netdb.h>
// #include <sys/types.h>
// #include <sys/ioctl.h>
// #include <sys/socket.h>
// #include <arpa/inet.h>
// #include <netinet/in.h>
// #include <netinet/tcp.h>
// #include <fcntl.h>
// #include <termios.h>
// #include <errno.h>
// #include <unistd.h>
// #include <stdio.h>
// #include <assert.h>
// #include <ctype.h>
// #include <stddef.h>
// #include <string.h>
// #include <time.h>
import "C"

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
	"unicode"
	"unsafe"
)

/*------------------------------------------------------------------
 *
 * Name: 	main
 *
 * Purpose:   	Start up multiple client threads listening to different
 *		TNCs.   Print packets.  Tally up statistics.
 *
 * Usage:	Described above.
 *
 *---------------------------------------------------------------*/

const MAX_CLIENTS = 6

const LINE_WIDTH = 120

var column_width int
var packet_count [MAX_CLIENTS]int

// #define PRINT_MINUTES 2
const PRINT_MINUTES = 30

func AClientsMain() {
	/*
	 * Extract command line args.
	 */
	var num_clients = len(os.Args) - 1

	if num_clients < 1 || num_clients > MAX_CLIENTS {
		fmt.Printf("Specify up to %d TNCs on the command line.\n", MAX_CLIENTS)
		os.Exit(1)
	}

	column_width = LINE_WIDTH / num_clients

	var hostname [MAX_CLIENTS]string /* DNS host name or IPv4 address. */
	/* Some of the code is there for IPv6 but */
	/* needs more work. */
	/* Defaults to "localhost" if not specified. */

	var port [MAX_CLIENTS]string /* If it begins with a digit, it is considered */
	/* a TCP port number at the hostname.  */
	/* Otherwise, we treat it as a serial port name. */

	var description [MAX_CLIENTS]string /* Name used in the output. */

	for j := range num_clients {
		/* Each command line argument should be of the form "port=description." */

		var arg = os.Args[j+1]
		var _port, _description, descriptionFound = strings.Cut(arg, "=")

		if !descriptionFound {
			fmt.Printf("Missing description after = in '%s'.\n", arg)
			os.Exit(1)
		}

		description[j] = _description
		port[j] = _port

		var _hostname, _port2, colonFound = strings.Cut(_port, ":")

		if colonFound {
			hostname[j] = _hostname
			port[j] = _port2
		} else {
			hostname[j] = "localhost"
		}
	}

	var packetChans = make([]chan string, num_clients)

	for j := range num_clients {
		var ch = make(chan string)
		packetChans[j] = ch

		/* If port begins with digit, consider it to be TCP. */
		/* Otherwise, treat as serial port name. */

		if unicode.IsDigit(rune(port[j][0])) {
			go client_thread_net(j, hostname[j], port[j], description[j], ch)
		} else {
			go client_thread_serial(j, port[j], description[j], ch)
		}
	}

	var start_time = time.Now()
	var next_print_time = start_time.Add(PRINT_MINUTES * time.Minute)

	/*
	 * Print results from clients.
	 */
	for {
		SLEEP_MS(100)

		var something = false
		var results = make([]string, len(packetChans))
		for i, ch := range packetChans {
			select {
			case msg := <-ch:
				something = true
				results[i] = msg
			default:
			}
		}

		if something {
			for _, result := range results {
				fmt.Printf("%*s", column_width, result)
			}
			fmt.Printf("\n")
		}

		var now = time.Now()
		if now.After(next_print_time) {
			next_print_time = now.Add(PRINT_MINUTES * time.Minute)

			fmt.Printf("\nTotals after %d minutes", int(now.Sub(start_time).Minutes()))

			for j := range num_clients {
				fmt.Printf(", %s %d", description[j], packet_count[j])
			}
			fmt.Printf("\n\n")
		}
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        client_thread_net
 *
 * Purpose:     Establish connection with a TNC via network.
 *
 * Inputs:	arg		- My instance index, 0 thru MAX_CLIENTS-1.
 *
 * Outputs:	packets		- Received packets are put in the corresponding column.
 *
 *--------------------------------------------------------------------*/

func client_thread_net(my_index int, hostname string, port string, description string, packetChan chan<- string) {
	var conn, connErr = net.Dial("tcp4", net.JoinHostPort(hostname, port))

	if connErr != nil {
		fmt.Printf("Client %d unable to connect to %s on %s, port %s\n",
			my_index, description, hostname, port)
		os.Exit(1)
	}

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
	}

	/*
	 * Send command to toggle reception of frames in raw format.
	 *
	 * Note: Monitor format is only for UI frames.
	 * It also discards the via path.
	 */

	var mon_cmd = new(AGWPEHeader)
	mon_cmd.DataKind = 'k'

	binary.Write(conn, binary.LittleEndian, mon_cmd)

	/*
	 * Print all of the monitored packets.
	 */

	var use_chan = -1
	for {
		var readErr = binary.Read(conn, binary.LittleEndian, mon_cmd)

		if readErr != nil {
			if readErr == io.EOF {
				continue
			}
			fmt.Printf("Read error, client %d got %s.\n", my_index, readErr)
			os.Exit(1)
		}

		var data = make([]byte, mon_cmd.DataLen)
		if mon_cmd.DataLen > 0 {
			_, readErr = io.ReadFull(conn, data)
			if readErr != nil {
				fmt.Printf("Read error, client %d got %s reading data.\n", my_index, readErr)
				os.Exit(1)
			}
		}

		/*
		 * Print it and add to counter.
		 * The AGWPE score was coming out double the proper value because
		 * we were getting the same thing from ports 2 and 3.
		 * 'use_chan' is the first channel we hear from.
		 * Listen only to that one.
		 */

		if mon_cmd.DataKind == 'K' && (use_chan == -1 || byte(use_chan) == mon_cmd.Portx) {
			// printf ("server %d, portx = %d\n", my_index, mon_cmd.portx);

			use_chan = int(mon_cmd.Portx)
			var alevel alevel_t
			var dataUChar = byteSliceToCUChars(data[1:])
			var pp = ax25_from_frame(&dataUChar[0], C.int(mon_cmd.DataLen-1), alevel)
			var result [400]C.char
			ax25_format_addrs(pp, &result[0])
			var pinfo *C.uchar
			var info_len = ax25_get_info(pp, &pinfo)
			_ = info_len

			var fullResult = C.GoString(&result[0]) + C.GoString((*C.char)(unsafe.Pointer(pinfo)))
			packetChan <- fullResult
			ax25_delete(pp)
			packet_count[my_index]++
		}
	}
} /* end client_thread_net */

/*-------------------------------------------------------------------
 *
 * Name:        client_thread_serial
 *
 * Purpose:     Establish connection with a TNC via serial port.
 *
 * Inputs:	arg		- My instance index, 0 thru MAX_CLIENTS-1.
 *
 * Outputs:	packets		- Received packets are put in the corresponding column.
 *
 *--------------------------------------------------------------------*/

func client_thread_serial(my_index int, port string, description string, packetChan chan<- string) {
	var fd = serial_port_open(port, 9600)

	if fd == nil {
		fmt.Printf("Client %d unable to connect to %s on %s.\n", my_index, description, port)
		os.Exit(1)
	}

	/* Success. */

	fmt.Printf("Client %d now connected to %s on %s\n", my_index, description, port)

	/*
	 * Assume we are already in monitor mode.
	 */

	/*
	 * Print all of the monitored packets.
	 */

	for {
		var length int
		var done = false
		var result [500]C.char
		for !done {
			var ch, err = serial_port_get1(fd)

			if err != nil {
				fmt.Printf("Client %d fatal read error: %s.\n", my_index, err)
				os.Exit(1)
			}

			/*
			 * Try to build one line for each packet.
			 * The KPC3+ breaks a packet into two lines like this:
			 *
			 *	KB1ZXL-1>T2QY5P,W1MHL*,WIDE2-1: <<UI>>:
			 *	`c0+!h4>/]"4a}146.520MHz Listening, V-Alert & WLNK-1=
			 *
			 *	N8VIM>BEACON,W1XM,WB2OSZ-1,WIDE2*: <UI>:
			 * 	!4240.85N/07133.99W_PHG72604/ Pepperell, MA. WX. 442.9+ PL100
			 *
			 * Don't know why some are <<UI>> and some <UI>.
			 *
			 * Anyhow, ignore the return character if preceded by >:
			 */
			if ch == '\r' {
				if length >= 10 && result[length-2] == '>' && result[length-1] == ':' {
					continue
				}
				done = true
				continue
			}
			if ch == '\n' {
				continue
			}
			result[length] = C.char(ch)
			length++
		}
		result[length] = 0

		/*
		 * Print it and add to counter.
		 */
		if length > 0 {
			packetChan <- C.GoString(&result[0])
		}
	}
}
