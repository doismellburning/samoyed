package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Simple Touch Tone to Speech calculator.
 *
 * Description:	Demonstration of how Dire Wolf can be used
 *		as a DTMF / Speech interface for ham radio applications.
 *
 * Usage:	Start up direwolf with configuration:
 *			- DTMF decoder enabled.
 *			- Text-to-speech enabled.
 *			- Listening to standard port 8000 for a client application.
 *
 *		Run this in a different window.
 *
 *		User sends formulas such as:
 *
 *			2 * 3 * 4 #
 *
 *		with the touch tone pad.
 *		The result is sent back with speech, e.g. "Twenty Four."
 *
 *---------------------------------------------------------------*/

// #include "direwolf.h"
// #include <stdlib.h>
// #include <netdb.h>
// #include <sys/types.h>
// #include <sys/ioctl.h>
// #include <sys/socket.h>
// #include <arpa/inet.h>
// #include <netinet/in.h>
// #include <netinet/tcp.h>
// #include <errno.h>
// #include <unistd.h>
// #include <stdio.h>
// #include <assert.h>
// #include <ctype.h>
// #include <string.h>
// #include "direwolf.h"
// #include "ax25_pad.h"
// #cgo CFLAGS: -I../../src -DMAJOR_VERSION=0 -DMINOR_VERSION=0
import "C"

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"unicode"
	"unsafe"
)

func TTCalcMain() {
	var hostname = "localhost"
	var port = "8000"

	/*
	 * Try to attach to Dire Wolf.
	 */

	var server_sock, err = connect_to_server(hostname, port)

	if err != nil {
		os.Exit(1)
	}

	/*
	 * Send command to toggle reception of frames in raw format.
	 *
	 * Note: Monitor format is only for UI frames.
	 */

	var mon_cmd agwpe_s
	mon_cmd.DataKind = 'k'

	binary.Write(server_sock, binary.LittleEndian, mon_cmd)

	/*
	 * Print all of the monitored packets.
	 */

	for {
		var readErr = binary.Read(server_sock, binary.LittleEndian, mon_cmd)

		if readErr != nil {
			if readErr == io.EOF {
				continue
			}
			fmt.Printf("Read error, received %d.\n", readErr)
			os.Exit(1)
		}

		var data = make([]byte, mon_cmd.DataLen)
		if mon_cmd.DataLen > 0 {
			_, readErr = io.ReadFull(server_sock, data)

			if readErr != nil {
				fmt.Printf("Read error, client received %s when reading %d data bytes. Terminating.\n", readErr, mon_cmd.DataLen)
				os.Exit(1)
			}
		}

		/*
		 * Print it.
		 */

		if mon_cmd.DataKind == 'K' {
			var channel = mon_cmd.Portx
			var alevel C.alevel_t
			var pp = ax25_from_frame((*C.uchar)(&data[1]), C.int(mon_cmd.DataLen-1), alevel)

			var result [400]C.char
			ax25_format_addrs(pp, &result[0])

			var pinfo *C.uchar
			ax25_get_info(pp, &pinfo)

			fmt.Printf("[%d] %s%s\n", channel, C.GoString(&result[0]), C.GoString((*C.char)(unsafe.Pointer(pinfo))))

			/*
			 * Look for Special touch tone packet with "t" in first position of the Information part.
			 */

			if *pinfo == 't' {
				/*
				 * Send touch tone sequence to calculator and get the answer.
				 *
				 * Put your own application here instead.  Here are some ideas:
				 *
				 *  http://www.tapr.org/pipermail/aprssig/2015-January/044069.html
				 */
				var n = calculator(C.GoString((*C.char)(unsafe.Pointer(pinfo)))[1:])
				fmt.Printf("\nCalculator returns %d\n\n", n)

				/*
				 * Convert to AX.25 frame.
				 * Notice that the special destination will cause it to be spoken.
				 */
				var reply_text = fmt.Sprintf("N0CALL>SPEECH:%d", n)
				var reply_pp = ax25_from_text(C.CString(reply_text), 1)

				/*
				 * Send it to the TNC.
				 * In this example we are transmitting speech on the same channel
				 * where the tones were heard.  We could also send AX.25 frames to
				 * other radio channels.
				 */
				var hdr agwpe_s
				hdr.Portx = channel
				hdr.DataKind = 'K'

				var reply_uchars [C.AX25_MAX_PACKET_LEN]C.uchar
				var reply_len = ax25_pack(reply_pp, &reply_uchars[0])
				hdr.DataLen = 1 + uint32(reply_len)

				binary.Write(server_sock, binary.LittleEndian, hdr)
				server_sock.Write([]byte{0x0})

				var reply_bytes = make([]byte, reply_len)
				for i := range reply_len {
					reply_bytes[i] = byte(reply_uchars[i])
				}
				server_sock.Write(unsafe.Slice(&reply_bytes[0], hdr.DataLen-1))

				ax25_delete(reply_pp)
			}

			ax25_delete(pp)
		}
	}
} /* main */

/*------------------------------------------------------------------
 *
 * Name: 	calculator
 *
 * Purpose:	Simple calculator to demonstrate Touch Tone to Speech
 *		application tool kit.
 *
 * Inputs:	str	- Sequence of touch tone characters: 0-9 A-D * #
 *			  It should be terminated with #.
 *
 * Returns:	Numeric result of calculation.
 *
 * Description:	This is a simple calculator that recognizes
 *			numbers,
 *			* for multiply
 *			A for add
 *			# for equals result
 *
 *		Adding functions to B, C, and D is left as an
 *		exercise for the reader.
 *
 * Examples:	2 * 3 A 4 #			Ten
 *		5 * 1 0 0 A 3 #			Five Hundred Three
 *
 *---------------------------------------------------------------*/

const (
	NONE = iota
	ADD
	SUB
	MUL
	DIV
)

func do_lastop(lastop, result, num int) int {
	switch lastop {
	case NONE:
		result = num
	case ADD:
		result += num
	case SUB:
		result -= num
	case MUL:
		result *= num
	case DIV:
		result /= num
	}
	return result
}

func calculator(str string) int {
	var result = 0
	var num = 0
	var lastop = NONE

	for _, p := range str {
		if unicode.IsDigit(p) {
			num = num*10 + int(byte(p)-byte('0'))
		} else if p == '*' {
			result = do_lastop(lastop, result, num)
			num = 0
			lastop = MUL
		} else if p == 'A' || p == 'a' {
			result = do_lastop(lastop, result, num)
			num = 0
			lastop = ADD
		} else if p == '#' {
			result = do_lastop(lastop, result, num)
			return result
		}
	}

	panic("Should never get here!")
}

/*------------------------------------------------------------------
 *
 * Name: 	connect_to_server
 *
 * Purpose:	Connect to Dire Wolf TNC server.
 *
 * Inputs:	hostname
 *		port
 *
 * Returns:	File descriptor or -1 for error.
 *
 *---------------------------------------------------------------*/

func connect_to_server(hostname string, port string) (net.Conn, error) {
	var conn, connErr = net.Dial("tcp4", net.JoinHostPort(hostname, port))

	if connErr != nil {
		return conn, connErr
	}

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
	}

	fmt.Printf("Client app now connected to %s, port %s\n", hostname, port)

	return conn, connErr
}
