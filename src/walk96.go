package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Quick hack to read GPS location and send very frequent
 *		position reports frames to a KISS TNC.
 *
 *---------------------------------------------------------------*/

// #include "direwolf.h"
// #include <stdio.h>
// #include <unistd.h>
// #include <stdlib.h>
// #include <assert.h>
// #include <string.h>
// #include <math.h>
// #include "ax25_pad.h"
import "C"

import (
	"fmt"
	"os"
	"unsafe"

	"github.com/pkg/term"
)

const HOWLONG = 20 /* Run for 20 seconds then quit. */

var MYCALL string

var tnc *term.Term

func Walk96Main() {
	// Quick and dirty CLI parsing
	var tncSerialPort string
	var gpsSerialPort string

	if len(os.Args) != 4 {
		fmt.Printf("Syntax: %s <CALLSIGN> <TNC Serial Port> <GPS Serial Port>\n", os.Args[0])
		os.Exit(1)
	} else {
		MYCALL = os.Args[1]
		tncSerialPort = os.Args[2]
		gpsSerialPort = os.Args[3]
	}

	tnc = serial_port_open(tncSerialPort, 9600)
	if tnc == nil {
		fmt.Printf("Can't open serial port to KISS TNC.\n")
		os.Exit(1)
	}

	var cmd = "\r\rhbaud 9600\rkiss on\rrestart\r"
	serial_port_write(tnc, []byte(cmd))

	var config misc_config_s
	C.strcpy(&config.gpsnmea_port[0], C.CString(gpsSerialPort))

	var debug_gps C.int = 0
	dwgps_init(&config, debug_gps)

	SLEEP_SEC(1) /* Wait for sample before reading. */

	for range HOWLONG {
		var info dwgps_info_t
		var fix = dwgps_read(&info)

		if fix > DWFIX_2D {
			walk96(int(fix), float64(info.dlat), float64(info.dlon), float64(info.speed_knots), float64(info.track), float64(info.altitude))
		} else if fix < 0 {
			fmt.Printf("Can't communicate with GPS receiver.\n")
			os.Exit(1)
		} else {
			fmt.Printf("GPS fix not available.\n")
		}
		SLEEP_SEC(1)
	}

	// Exit out of KISS mode.

	serial_port_write(tnc, []byte("\xc0\xff\xc0"))

	SLEEP_MS(100)
}

var sequence = 0

/* Should be called once per second. */

func walk96(fix int, lat float64, lon float64, knots float64, course float64, alt float64) {
	/*
		char comment[50];
	*/

	sequence++
	var comment = fmt.Sprintf("Sequence number %04d", sequence)

	/*
	 * Construct the packet in normal monitoring format.
	 */

	/* FIXME KG
	int messaging = 0;
	int compressed = 0;

	char info[AX25_MAX_INFO_LEN];
	char position_report[AX25_MAX_PACKET_LEN];
	*/

	// TODO (high, bug):    Why do we see !4237.13N/07120.84W=PHG0000...   when all values set to unknown.

	var messaging C.int = 0
	var compressed C.int = 0

	var info = encode_position(messaging, compressed,
		C.double(lat), C.double(lon), 0, C.int(DW_METERS_TO_FEET(alt)),
		'/', '=',
		G_UNKNOWN, G_UNKNOWN, G_UNKNOWN, C.CString(""), // PHGd
		C.int(course), C.int(knots),
		445.925, 0, 0,
		C.CString(comment))

	var position_report = fmt.Sprintf("%s>WALK96:%s", MYCALL, info)

	fmt.Printf("%s\n", position_report)

	/*
	 * Convert it into AX.25 frame.
	 */

	var pp = ax25_from_text(C.CString(position_report), 1)

	if pp == nil {
		fmt.Printf("Unexpected error in ax25_from_text.  Quitting.\n")
		os.Exit(1)
	}

	var ax25_frame [C.AX25_MAX_PACKET_LEN]C.uchar
	ax25_frame[0] = 0 // Insert channel before KISS encapsulation.

	var frame_len = ax25_pack(pp, &ax25_frame[1])
	ax25_delete(pp)

	/*
	 * Encapsulate as KISS and send to TNC.
	 */

	var kiss_frame = kiss_encapsulate(C.GoBytes(unsafe.Pointer(&ax25_frame[0]), frame_len+1))

	// kiss_debug_print (1, NULL, kiss_frame, kiss_len);

	serial_port_write(tnc, kiss_frame)
}
