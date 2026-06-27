// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//nolint:gochecknoglobals
package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:	Attach to ARDOP TNC(s) for ACHANNEL config file item(s).
 *
 * Description:	Called once at application start up.
 *
 *		The ARDOP TNC host interface uses two TCP connections:
 *		  - Control port (typically 8515): ASCII text commands/responses.
 *		  - Data port (typically 8516): length-prefixed binary frames.
 *
 *		Data port frame format:
 *		  - 2-byte big-endian payload length
 *		  - Payload bytes (raw AX.25 frame)
 *
 *		Control port commands are CR-terminated ASCII, e.g.:
 *		  "INITIALIZE\r", "MYCALL K1ABC\r", "LISTEN TRUE\r"
 *
 *---------------------------------------------------------------*/

import (
	"bufio"
	"encoding/binary"
	"net"
	"os"
	"strconv"
)

var s_ardop_host [MAX_TOTAL_CHANS]string
var s_ardop_ctrl_port [MAX_TOTAL_CHANS]int
var s_ardop_data_port [MAX_TOTAL_CHANS]int
var s_ardop_ctrl_sock [MAX_TOTAL_CHANS]net.Conn
var s_ardop_data_sock [MAX_TOTAL_CHANS]net.Conn
var s_ardop_mycall [MAX_TOTAL_CHANS]string

/*-------------------------------------------------------------------
 *
 * Name:        ardop_init
 *
 * Purpose:     Attach to ARDOP TNC(s) for ACHANNEL config file item(s).
 *
 * Inputs:	pa - Address of structure of type audio_s.
 *
 * Returns:	Nothing. Exits on failure.
 *
 *--------------------------------------------------------------------*/

func ardop_init(pa *audio_s) {
	for i := range MAX_TOTAL_CHANS {
		if pa.chan_medium[i] == MEDIUM_ARDOP {
			s_ardop_mycall[i] = pa.mycall[i]

			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Channel %d: ARDOP TNC %s ctrl=%d data=%d\n",
				i, pa.ardop_addr[i], pa.ardop_ctrl_port[i], pa.ardop_data_port[i])

			var e = ardop_attach(i, pa.ardop_addr[i], pa.ardop_ctrl_port[i], pa.ardop_data_port[i])
			if e < 0 {
				os.Exit(1)
			}
		}
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        ardop_attach
 *
 * Purpose:     Attach to one ARDOP TNC.
 *
 * Inputs:	channel  - Channel number from ACHANNEL configuration.
 *		host     - Host name or IP address.
 *		ctrlPort - ARDOP TNC control TCP port (typically 8515).
 *		dataPort - ARDOP TNC data TCP port (typically 8516).
 *
 * Returns:	0 for success, -1 for failure.
 *
 *--------------------------------------------------------------------*/

func ardop_attach(channel int, host string, ctrlPort int, dataPort int) int {
	Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	s_ardop_host[channel] = host
	s_ardop_ctrl_port[channel] = ctrlPort
	s_ardop_data_port[channel] = dataPort
	s_ardop_ctrl_sock[channel] = nil
	s_ardop_data_sock[channel] = nil

	var ctrlConn, ctrlErr = net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(ctrlPort)))
	if ctrlErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Channel %d: Unable to connect to ARDOP TNC control port %s:%d: %v\n",
			channel, host, ctrlPort, ctrlErr)

		return -1
	}

	s_ardop_ctrl_sock[channel] = ctrlConn

	var dataConn, dataErr = net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(dataPort)))
	if dataErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Channel %d: Unable to connect to ARDOP TNC data port %s:%d: %v\n",
			channel, host, dataPort, dataErr)
		ctrlConn.Close()
		s_ardop_ctrl_sock[channel] = nil

		return -1
	}

	s_ardop_data_sock[channel] = dataConn

	ardop_send_ctrl(channel, "INITIALIZE")
	if s_ardop_mycall[channel] != "" {
		ardop_send_ctrl(channel, "MYCALL "+s_ardop_mycall[channel])
	}
	ardop_send_ctrl(channel, "LISTEN TRUE")

	go ardop_ctrl_thread(channel)
	go ardop_data_thread(channel)

	return 0
}

/*-------------------------------------------------------------------
 *
 * Name:        ardop_send_ctrl
 *
 * Purpose:     Send a CR-terminated command to the ARDOP TNC control port.
 *
 *--------------------------------------------------------------------*/

func ardop_send_ctrl(channel int, cmd string) {
	if s_ardop_ctrl_sock[channel] == nil {
		return
	}

	var _, err = s_ardop_ctrl_sock[channel].Write([]byte(cmd + "\r"))
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Channel %d: Error sending command to ARDOP TNC control port: %v\n", channel, err)
		s_ardop_ctrl_sock[channel].Close()
		s_ardop_ctrl_sock[channel] = nil
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        ardop_ctrl_thread
 *
 * Purpose:     Read status lines from the ARDOP TNC control port and log them.
 *		Attempt to reconnect if the connection is lost.
 *
 *--------------------------------------------------------------------*/

func ardop_ctrl_thread(channel int) {
	Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	for {
		if s_ardop_ctrl_sock[channel] == nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Channel %d: Attempting to reattach to ARDOP TNC control port...\n", channel)

			var conn, err = net.Dial("tcp", net.JoinHostPort(
				s_ardop_host[channel],
				strconv.Itoa(s_ardop_ctrl_port[channel]),
			))
			if err != nil {
				SLEEP_SEC(5)

				continue
			}

			s_ardop_ctrl_sock[channel] = conn

			ardop_send_ctrl(channel, "INITIALIZE")
			if s_ardop_mycall[channel] != "" {
				ardop_send_ctrl(channel, "MYCALL "+s_ardop_mycall[channel])
			}
			ardop_send_ctrl(channel, "LISTEN TRUE")

			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Channel %d: Successfully reattached to ARDOP TNC control port.\n", channel)
		} else {
			var scanner = bufio.NewScanner(s_ardop_ctrl_sock[channel])
			for scanner.Scan() {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("ARDOP TNC ch%d ctrl: %s\n", channel, scanner.Text())
			}

			// Scanner stopped — connection lost.
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Channel %d: Lost connection to ARDOP TNC control port. Will try to reattach.\n", channel)
			s_ardop_ctrl_sock[channel].Close()
			s_ardop_ctrl_sock[channel] = nil

			SLEEP_SEC(5)
		}
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        ardop_data_thread
 *
 * Purpose:     Read length-prefixed frames from the ARDOP TNC data port,
 *		decode each payload as a raw AX.25 frame, and dispatch it
 *		as a received packet.  Reconnects if the connection is lost.
 *
 * Frame format (ardopc data port):
 *   2 bytes  - big-endian payload length
 *   N bytes  - AX.25 frame bytes
 *
 *--------------------------------------------------------------------*/

func ardop_data_thread(channel int) {
	Assert(channel >= 0 && channel < MAX_TOTAL_CHANS)

	for {
		if s_ardop_data_sock[channel] == nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Channel %d: Attempting to reattach to ARDOP TNC data port...\n", channel)

			var conn, err = net.Dial("tcp", net.JoinHostPort(
				s_ardop_host[channel],
				strconv.Itoa(s_ardop_data_port[channel]),
			))
			if err != nil {
				SLEEP_SEC(5)

				continue
			}

			s_ardop_data_sock[channel] = conn

			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Channel %d: Successfully reattached to ARDOP TNC data port.\n", channel)
		} else {
			var lenBuf [2]byte
			var err = readFull(s_ardop_data_sock[channel], lenBuf[:])
			if err != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Channel %d: Lost connection to ARDOP TNC data port. Will try to reattach.\n", channel)
				s_ardop_data_sock[channel].Close()
				s_ardop_data_sock[channel] = nil

				SLEEP_SEC(5)

				continue
			}

			var payloadLen = int(binary.BigEndian.Uint16(lenBuf[:]))
			if payloadLen == 0 {
				continue
			}

			var payload = make([]byte, payloadLen)
			err = readFull(s_ardop_data_sock[channel], payload)
			if err != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Channel %d: Error reading ARDOP TNC data frame. Will try to reattach.\n", channel)
				s_ardop_data_sock[channel].Close()
				s_ardop_data_sock[channel] = nil

				SLEEP_SEC(5)

				continue
			}

			// Dispatch as a received AX.25 packet on this channel.
			var alevel alevel_t
			var pp = ax25_from_frame(payload, alevel)
			if pp != nil {
				var fec_type = fec_type_none
				var retries BitFixLevel
				var spectrum = "ARDOP TNC"

				dlq_rec_frame(channel, -3, 0, pp, alevel, fec_type, retries, spectrum)
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Channel %d: Failed to create packet object for ARDOP TNC data frame.\n", channel)
			}
		}
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        ardop_send_packet
 *
 * Purpose:     Send an AX.25 packet to the ARDOP TNC data port.
 *
 * Inputs:	channel - Channel number from ACHANNEL configuration.
 *		pp      - Packet object.
 *
 * Description: Encodes the packet as a length-prefixed frame and writes
 *		it to the ARDOP TNC data port.  Does not free the packet
 *		object; caller is responsible.
 *
 *--------------------------------------------------------------------*/

func ardop_send_packet(channel int, pp *packet_t) {
	var fbuf = ax25_get_frame_data(pp)

	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(fbuf)))

	var frame = append(lenBuf[:], fbuf...)

	var _, err = s_ardop_data_sock[channel].Write(frame)
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("\nChannel %d: Error sending packet to ARDOP TNC data port. Closing connection.\n\n", channel)
		s_ardop_data_sock[channel].Close()
		s_ardop_data_sock[channel] = nil
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        readFull
 *
 * Purpose:     Read exactly len(buf) bytes from conn, blocking until all
 *		bytes are received or an error occurs.
 *
 *--------------------------------------------------------------------*/

func readFull(conn net.Conn, buf []byte) error {
	var total = 0
	for total < len(buf) {
		var n, err = conn.Read(buf[total:])
		total += n
		if err != nil {
			return err
		}
	}

	return nil
}
