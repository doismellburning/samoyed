package main

//	****** PRELIMINARY - needs work ******

//
//    This file is part of Dire Wolf, an amateur radio packet TNC.
//

/*------------------------------------------------------------------
 *
 * Purpose:   	Sample application Program Interface (API) to use network TNC with AGW protocol.
 *
 * Description:	This file contains functions to attach to a TNC over a TCP socket and send
 *		commands to it.   The current list includes some of the following:
 *
 *			'C'	Connect, Start an AX.25 Connection
 *			'v'	Connect VIA, Start an AX.25 circuit thru digipeaters
 *			'c'	Connection with non-standard PID
 *			'D'	Send Connected Data
 *			'd'	Disconnect, Terminate an AX.25 Connection
 *			'X'	Register CallSign
 *			'x'	Unregister CallSign
 *			'R'	Request for version number.
 *			'G'	Ask about radio ports.
 *			'g'	Capabilities of a port.
 *			'k'	Ask to start receiving RAW AX25 frames.
 *			'm'	Ask to start receiving Monitor AX25 frames.
 *			'V'	Transmit UI data frame.
 *			'H'	Report recently heard stations.  Not implemented yet in direwolf.
 *			'K'	Transmit raw AX.25 frame.
 *			'y'	Ask Outstanding frames waiting on a Port
 *			'Y'	How many frames waiting for transmit for a particular station
 *
 *
 *		The user supplied application must supply functions to handle or ignore
 *		messages that come from the TNC.  Common examples:
 *
 *			'C'	AX.25 Connection Received
 *			'D'	Connected AX.25 Data
 *			'd'	Disconnected
 *			'R'	Reply to Request for version number.
 *			'G'	Reply to Ask about radio ports.
 *			'g'	Reply to capabilities of a port.
 *			'K'	Received AX.25 frame in raw format.  (Enabled with 'k' command.)
 *			'U'	Received AX.25 frame in monitor format.  (Enabled with 'm' command.)
 *			'y'	Outstanding frames waiting on a Port
 *			'Y'	How many frames waiting for transmit for a particular station
 *			'C'	AX.25 Connection Received
 *			'D'	Connected AX.25 Data
 *			'd'	Disconnected
 *
 *
 *
 * References:	AGWPE TCP/IP API Tutorial
 *		http://uz7ho.org.ua/includes/agwpeapi.htm
 *
 *---------------------------------------------------------------*/

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"

	direwolf "github.com/doismellburning/samoyed/src"
)

const AX25_MAX_INFO_LEN = 2048 // Duplicated from C to avoid cgo
const MAX_TOTAL_CHANS = 16     // Duplicated from C to avoid cgo

type Callsign [10]byte

type AGWPEHeader struct {
	Portx        byte
	Reserved1    byte
	Reserved2    byte
	Reserved3    byte
	DataKind     byte
	Reserved4    byte
	PID          byte
	Reserved5    byte
	CallFrom     Callsign
	CallTo       Callsign
	DataLen      uint32
	UserReserved [4]byte
}

type AGWPECommand struct {
	Header *AGWPEHeader
	Data   []byte
}

/*-------------------------------------------------------------------
 *
 * Name:        agwlib_init
 *
 * Purpose:     Attach to TNC over TCP.
 *
 * Inputs:	host	- Host name or address.  Often "localhost".
 *
 *		port	- TCP port number as text.  Usually "8000".
 *
 *		init_func - Call this function after establishing communication
 *			with the TNC.  We put it here, so that it can be done
 *			again automatically if the TNC disappears and we
 *			reattach to it.
 *			It must return 0 for success.
 *			Can be NULL if not needed.
 *
 * Description:	This starts up a thread which listens to the socket and
 *		dispatches the messages to the corresponding callback functions.
 *		It will also attempt to re-establish communication with the
 *		TNC if it goes away.
 *
 *--------------------------------------------------------------------*/

var s_tnc_host string
var s_tnc_port string
var s_tnc_sock net.Conn
var s_tnc_init_func func() error

// TODO: define macros somewhere to hide platform specifics.

func agwlib_init(host string, port string, init_func func() error) error {
	s_tnc_host = host
	s_tnc_port = port
	s_tnc_init_func = init_func

	var connErr error

	s_tnc_sock, connErr = net.Dial("tcp4", net.JoinHostPort(host, port))
	if connErr != nil {
		return connErr
	}

	/*
	 * Incoming messages are dispatched to application-supplied callback functions.
	 * If the TNC disappears, try to reestablish communication.
	 */

	go tnc_listen_thread()

	// TNC initialization if specified.

	if s_tnc_init_func != nil {
		return s_tnc_init_func()
	}

	return nil
}

/*-------------------------------------------------------------------
 *
 * Name:        tnc_listen_thread
 *
 * Purpose:     Listen for anything from TNC and process it.
 *		Reconnect if something goes wrong and we got disconnected.
 *
 * Inputs:	s_tnc_host
 *		s_tnc_port
 *
 * Outputs:	s_tnc_sock	- File descriptor for communicating with TNC.
 *
 *--------------------------------------------------------------------*/

func tnc_listen_thread() {
	for {
		/*
		 * Connect to TNC if not currently connected.
		 */
		if s_tnc_sock == nil {
			// I'm using the term "attach" here, in an attempt to
			// avoid confusion with the AX.25 connect.
			fmt.Printf("Attempting to reattach to network TNC...\n")

			var connErr error

			s_tnc_sock, connErr = net.Dial("tcp4", net.JoinHostPort(s_tnc_host, s_tnc_port))
			if connErr == nil {
				fmt.Printf("Successfully reattached to network TNC.\n")

				// Might need to run TNC initialization again.
				// For example, a server would register its callsigns.

				if s_tnc_init_func != nil {
					s_tnc_init_func() //nolint:gosec
				}
			}

			direwolf.SLEEP_SEC(5)
		} else {
			var header = new(AGWPEHeader)

			var readErr = binary.Read(s_tnc_sock, binary.LittleEndian, header)
			if readErr != nil {
				fmt.Printf("Error communicating with network TNC. Will try to reattach: %s\n", readErr)
				s_tnc_sock.Close() //nolint:gosec
				s_tnc_sock = nil

				continue
			}

			/*
			 * Take some precautions to guard against bad data which could cause problems later.
			 */
			if header.Portx >= MAX_TOTAL_CHANS {
				fmt.Printf("Invalid channel number, %d, in command '%c', from network TNC.\n", header.Portx, header.DataKind)
				header.Portx = 0 // avoid subscript out of bounds, try to keep going.
			}

			/*
			 * Call to/from fields are 10 bytes but contents must not exceed 9 characters.
			 * It's not guaranteed that unused bytes will contain 0 so we
			 * don't issue error message in this case.
			 */
			header.CallFrom[len(header.CallFrom)-1] = 0
			header.CallTo[len(header.CallTo)-1] = 0

			if header.DataLen > 0 {
				var data = make([]byte, header.DataLen)

				var n, err = io.ReadFull(s_tnc_sock, data)
				if uint32(n) != header.DataLen || err != nil { //nolint:gosec
					fmt.Printf("Error getting message data from network TNC: %s\n", err)
					fmt.Printf("Tried to read %d bytes but got only %d.\n", header.DataLen, n)
					fmt.Printf("Closing socket to network TNC.\n\n")
					s_tnc_sock.Close() //nolint:gosec
					s_tnc_sock = nil

					continue
				}

				var cmd = &AGWPECommand{
					Header: header,
					Data:   data,
				}

				process_from_tnc(cmd)
			} // additional data after command header
		}
	}
}

/*
 * The user supplied application must supply functions to handle or ignore
 * messages that come from the TNC.
 */

func process_from_tnc(cmd *AGWPECommand) {
	switch cmd.Header.DataKind {
	case 'C': // AX.25 Connection Received
		// agw_cb_C_connection_received (cmd.Header.Portx, cmd.Header.CallFrom, cmd.Header.CallTo, data_len, cmd.data);
		// TODO:  compute session id
		// There are two different cases to consider here.
		if string(cmd.Data[:24]) == "*** CONNECTED To Station" {
			// Incoming: Other station initiated the connect request.
			on_C_connection_received(cmd.Header.Portx, cmd.Header.CallFrom, cmd.Header.CallTo, true, cmd.Data)
		} else if string(cmd.Data[:26]) == "*** CONNECTED With Station" {
			// Outgoing: Other station accepted my connect request.
			on_C_connection_received(cmd.Header.Portx, cmd.Header.CallFrom, cmd.Header.CallTo, false, cmd.Data)
		} else { //nolint:staticcheck
			// TBD
		}
	case 'D': // Connected AX.25 Data
		// FIXME: should probably add pid here.
		agw_cb_D_connected_data(cmd.Header.Portx, cmd.Header.CallFrom, cmd.Header.CallTo, cmd.Data)

	case 'd': // Disconnected
		agw_cb_d_disconnected(cmd.Header.Portx, cmd.Header.CallFrom, cmd.Header.CallTo, cmd.Data)

	case 'R': // Reply to Request for version number.

	case 'G': // Port Information.
		// Data part should be fields separated by semicolon.
		// First field is number of ports (we call them channels).
		// Other fields are of the form "Port99 comment" where first is number 1.
		var num_chan = 1 // FIXME: FIXME: actually parse it.

		var chans = make([]string, 2)

		chans[0] = "Port1 blah blah"
		chans[1] = "Port2 blah blah"
		agw_cb_G_port_information(num_chan, chans)
		// TODO: Maybe fill in more someday.

	case 'g': // Reply to capabilities of a port.
	case 'K': // Received AX.25 frame in raw format. (Enabled with 'k' command.)
	case 'U': // Received AX.25 frame in monitor format. (Enabled with 'm' command.)
	case 'y': // Outstanding frames waiting on a Port
	case 'Y': // How many frames waiting for transmit for a particular station
		var dataStr = string(cmd.Data)

		var frameCount, _ = strconv.Atoi(dataStr)

		agw_cb_Y_outstanding_frames_for_station(cmd.Header.Portx, cmd.Header.CallFrom, cmd.Header.CallTo, frameCount)
	default:
	}
} // end process_from_tnc

/*-------------------------------------------------------------------
 *
 * Name:        agwlib_X_register_callsign
 *
 * Purpose:     Tell TNC to accept incoming connect requests to given callsign.
 *
 * Inputs:	chan		- Radio channel number, first is 0.
 *
 *		call_from	- My callsign or alias.
 *
 *--------------------------------------------------------------------*/

func agwlib_X_register_callsign(channel byte, call_from Callsign) error {
	var h = new(AGWPEHeader)

	h.Portx = channel
	h.DataKind = 'X'
	h.CallFrom = call_from

	return binary.Write(s_tnc_sock, binary.LittleEndian, h)
}

/*-------------------------------------------------------------------
 *
 * Name:        agwlib_x_unregister_callsign
 *
 * Purpose:     Tell TNC to stop accepting incoming connect requests to given callsign.
 *
 * Inputs:	chan		- Radio channel number, first is 0.
 *
 *		call_from	- My callsign or alias.
 *
 * FIXME:	question do we need channel here?
 *
 *--------------------------------------------------------------------*/

func agwlib_x_unregister_callsign(channel byte, call_from Callsign) error { //nolint:unused
	var h = new(AGWPEHeader)

	h.Portx = channel
	h.DataKind = 'x'
	h.CallFrom = call_from

	return binary.Write(s_tnc_sock, binary.LittleEndian, h)
}

/*-------------------------------------------------------------------
 *
 * Name:        agwlib_G_ask_port_information
 *
 * Purpose:     Tell TNC to stop accepting incoming connect requests to given callsign.
 *
 * Inputs:	call_from	- My callsign or alias.
 *
 *--------------------------------------------------------------------*/

func agwlib_G_ask_port_information() error {
	var h = new(AGWPEHeader)

	h.DataKind = 'G'

	return binary.Write(s_tnc_sock, binary.LittleEndian, h)
}

/*-------------------------------------------------------------------
 *
 * Name:        agwlib_C_connect
 *
 * Purpose:     Tell TNC to start sequence for connecting to remote station.
 *
 * Inputs:	chan		- Radio channel number, first is 0.
 *
 *		call_from	- My callsign.
 *
 *		call_to		- Callsign (or alias) of remote station.
 *
 * Description:	This only starts the sequence and does not wait.
 *		Success or failure will be indicated sometime later by ?
 *
 *--------------------------------------------------------------------*/

func agwlib_C_connect(channel byte, call_from Callsign, call_to Callsign) error { //nolint:unused
	var h = new(AGWPEHeader)

	h.Portx = channel
	h.DataKind = 'C'
	h.PID = 0xF0 // Shouldn't matter because this appears only in Information frame, not connect sequence.
	h.CallFrom = call_from
	h.CallTo = call_to

	return binary.Write(s_tnc_sock, binary.LittleEndian, h)
}

/*-------------------------------------------------------------------
 *
 * Name:        agwlib_d_disconnect
 *
 * Purpose:     Tell TNC to disconnect from remote station.
 *
 * Inputs:	chan		- Radio channel number, first is 0.
 *
 *		call_from	- My callsign.
 *
 *		call_to		- Callsign (or alias) of remote station.
 *
 * Description:	This only starts the sequence and does not wait.
 *		Success or failure will be indicated sometime later by ?
 *
 *--------------------------------------------------------------------*/

func agwlib_d_disconnect(channel byte, call_from Callsign, call_to Callsign) error {
	var h = new(AGWPEHeader)

	h.Portx = channel
	h.DataKind = 'd'
	h.CallFrom = call_from
	h.CallTo = call_to

	return binary.Write(s_tnc_sock, binary.LittleEndian, h)
}

/*-------------------------------------------------------------------
 *
 * Name:        agwlib_D_send_connected_data
 *
 * Purpose:     Send connected data to remote station.
 *
 * Inputs:	chan		- Radio channel number, first is 0.
 *
 *		pid		- Protocol ID.  Normally 0xFo for Ax.25.
 *
 *		call_from	- My callsign.
 *
 *		call_to		- Callsign (or alias) of remote station.
 *
 *		data		- Content for Information part.
 *
 * Description:	This should only be done when we are known to have
 *		an established link to other station.
 *
 *--------------------------------------------------------------------*/

func agwlib_D_send_connected_data(channel byte, pid byte, call_from Callsign, call_to Callsign, data []byte) error { //nolint:unparam
	var h = new(AGWPEHeader)

	h.Portx = channel
	h.DataKind = 'D'
	h.PID = pid // Normally 0xF0 but other special cases are possible.
	h.CallFrom = call_from
	h.CallTo = call_to
	h.DataLen = uint32(len(data)) //nolint:gosec

	var headerErr = binary.Write(s_tnc_sock, binary.LittleEndian, h)
	if headerErr != nil {
		return headerErr
	}

	var _, dataErr = s_tnc_sock.Write(data)

	return dataErr
}

/*-------------------------------------------------------------------
 *
 * Name:        agwlib_Y_outstanding_frames_for_station
 *
 * Purpose:     Ask how many frames remain to be sent to station on other end of link.
 *
 * Inputs:	chan		- Radio channel number, first is 0.
 *
 *		call_from	- My call [ or is it Station which initiated the link?  (sent SABM/SABME) ]
 *
 *		call_to		- Remote station call [ or is it Station which accepted the link? ]
 *
 * Description:	We expect to get a 'Y' frame response shortly.
 *
 *		This would be useful for a couple different purposes.
 *
 *		When sending bulk data, we want to keep a fair amount queued up to take
 *		advantage of large window sizes (MAXFRAME, EMAXFRAME).  On the other
 *		hand we don't want to get TOO far ahead when transferring a large file.
 *
 *		Before disconnecting from another station, it would be good to know
 *		that it actually received the last message we sent.  For this reason,
 *		I think it would be good for this to include frames that were
 *		transmitted but not yet acknowledged.  (Even if it was transmitted once,
 *		it could still be transmitted again, if lost, so you could say it is
 *		still waiting for transmission.)
 *
 *		See server.c for a more precise definition of exactly how this is defined.
 *
 *--------------------------------------------------------------------*/

func agwlib_Y_outstanding_frames_for_station(channel byte, call_from Callsign, call_to Callsign) error {
	var h = new(AGWPEHeader)

	h.Portx = channel
	h.DataKind = 'Y'
	h.CallFrom = call_from
	h.CallTo = call_to

	return binary.Write(s_tnc_sock, binary.LittleEndian, h)
}
