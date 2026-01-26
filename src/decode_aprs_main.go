package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:	Main program for standalone application to parse and explain APRS packets.
 *
 * Inputs:	stdin for raw data to decode.
 *		This is in the usual display format either from
 *		a TNC, findu.com, aprs.fi, etc.  e.g.
 *
 *		N1EDF-9>T2QT8Y,W1CLA-1,WIDE1*,WIDE2-2,00000:`bSbl!Mv/`"4%}_ <0x0d>
 *
 *		WB2OSZ-1>APN383,qAR,N1EDU-2:!4237.14NS07120.83W#PHG7130Chelmsford, MA
 *
 *		New for 1.5:
 *
 *		Also allow hexadecimal bytes for raw AX.25 or KISS.  e.g.
 *
 *		00 82 a0 ae ae 62 60 e0 82 96 68 84 40 40 60 9c 68 b0 ae 86 40 e0 40 ae 92 88 8a 64 63 03 f0 3e 45 4d 36 34 6e 65 2f 23 20 45 63 68 6f 6c 69 6e 6b 20 31 34 35 2e 33 31 30 2f 31 30 30 68 7a 20 54 6f 6e 65
 *
 *		If it begins with 00 or C0 (which would be impossible for AX.25 address) process as KISS.
 *		Also print these formats.
 *
 * Outputs:	stdout
 *
 * Description:	./decode_aprs < decode_aprs.txt
 *
 *		aprs.fi precedes raw data with a time stamp which you
 *		would need to remove first.
 *
 *		cut -c26-999 tmp/kj4etp-9.txt | decode_aprs.exe
 *
 *
 * Restriction:	MIC-E message type can be problematic because it
 *		it can use unprintable characters in the information field.
 *
 *		Dire Wolf and aprs.fi print it in hexadecimal.  Example:
 *
 *		KB1KTR-8>TR3U6T,KB1KTR-9*,WB2OSZ-1*,WIDE2*,qAR,W1XM:`c1<0x1f>l!t>/>"4^}
 *		                                                       ^^^^^^
 *		                                                       ||||||
 *		What does findu.com do in this case?
 *
 *		ax25_from_text recognizes this representation so it can be used
 *		to decode raw data later.
 *
 * TODO:	To make it more useful,
 *			- Remove any leading timestamp.
 *			- Remove any "qA*" and following from the path.
 *			- Handle non-APRS frames properly.
 *
 *------------------------------------------------------------------*/

// #include <stdio.h>
// #include <time.h>
// #include <assert.h>
// #include <stdlib.h>	/* for atof */
// #include <string.h>	/* for strtok */
// #include <math.h>	/* for pow */
// #include <ctype.h>	/* for isdigit */
// #include <fcntl.h>
// #include "regex.h"
// #cgo CFLAGS: -I../../src -DMAJOR_VERSION=0 -DMINOR_VERSION=0 -O0
import "C"

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unsafe"
)

func byteSliceToCUChars(data []byte) []C.uchar {
	var chars = make([]C.uchar, len(data))

	for i, b := range data {
		chars[i] = C.uchar(b)
	}

	return chars
}

func DecodeAPRSMain() {
	DECODE_APRS_UTIL = true // DECAMAIN define replacement

	text_color_init(0)
	text_color_set(DW_COLOR_INFO)
	deviceid_init()

	var scanner = bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var line = scanner.Text()
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			/* comment or blank line */
			fmt.Printf("%s\n", line)
			continue
		}

		DecodeAPRSLine(line)
	}
}

func DecodeAPRSLine(line string) {
	/* Try to process it. */

	fmt.Printf("\n")
	ax25_safe_print(C.CString(line), -1, 0)
	fmt.Printf("\n")

	// Do we have monitor format, KISS, or AX.25 frame?

	line = strings.TrimLeft(line, " ")

	var r = regexp.MustCompile("^[[:xdigit:]]{2}( [[:xdigit:]]{2})*$")

	if r.MatchString(line) {
		// Documented input format is "DE AD BE EF"
		// Go's hex.DecodeString will decode "DEADBEEF"
		// So, let's just strip spaces and use that!

		var spacelessLine = strings.ReplaceAll(line, " ", "")
		var bytes, err = hex.DecodeString(spacelessLine)

		if err != nil {
			panic(err)
		}

		// If we have 0xC0 at start, remove it and expect same at end.

		if bytes[0] == FEND {
			if len(bytes) < 2 || bytes[1] != 0 {
				fmt.Printf("Was expecting to find 00 after the initial C0.\n")
				return
			}

			if bytes[len(bytes)-1] == FEND {
				fmt.Printf("Removing KISS FEND characters at beginning and end.\n")
				bytes = bytes[1 : len(bytes)-1]
			} else {
				fmt.Printf("Removing KISS FEND character at beginning.  Was expecting another at end.\n")
				bytes = bytes[1:]
			}
		}

		if bytes[0] == 0 {
			// Treat as KISS.  Undo any KISS encoding.
			var kiss_frame = bytes

			fmt.Printf("--- KISS frame ---\n")
			hex_dump(kiss_frame)

			// Put FEND at end to keep kiss_unwrap happy.
			// Having one at the beginning is optional.

			kiss_frame = append(kiss_frame, FEND)

			// In the more general case, we would need to include
			// the command byte because it could be escaped.
			// Here we know it is 0, so we take a short cut and
			// remove it before, rather than after, the conversion.

			bytes = kiss_unwrap(kiss_frame[1:])
		}

		// Treat as AX.25.

		var alevel alevel_t

		var pp = ax25_from_frame((*C.uchar)(C.CBytes(bytes)), C.int(len(bytes)), alevel)
		if pp != nil {
			var addrs [120]C.char
			var pinfo *C.uchar

			fmt.Printf("--- AX.25 frame ---\n")
			ax25_hex_dump(pp)
			fmt.Printf("-------------------\n")

			ax25_format_addrs(pp, &addrs[0])
			fmt.Printf("%s", C.GoString(&addrs[0]))

			var info_len = ax25_get_info(pp, &pinfo)
			ax25_safe_print((*C.char)(unsafe.Pointer(pinfo)), info_len, 1) // Display non-ASCII to hexadecimal.
			fmt.Printf("\n")

			var A decode_aprs_t
			decode_aprs(&A, pp, 0, nil) // Extract information into structure.

			decode_aprs_print(&A) // Now print it in human readable format.

			ax25_check_addresses(pp) // Errors for invalid addresses.

			ax25_delete(pp)
		} else {
			fmt.Printf("Could not construct AX.25 frame from bytes supplied!\n\n")
		}
	} else {
		// Normal monitoring format.

		var pp = ax25_from_text(line, true)
		if pp != nil {
			var A decode_aprs_t

			decode_aprs(&A, pp, 0, nil) // Extract information into structure.

			decode_aprs_print(&A) // Now print it in human readable format.

			// This seems to be redundant because we used strict option
			// when parsing the monitoring format text.
			// (void)ax25_check_addresses(pp);	// Errors for invalid addresses.

			// Future?  Add -d option to include hex dump and maybe KISS?

			ax25_delete(pp)
		} else {
			fmt.Printf("ERROR - Could not parse monitoring format input!\n\n")
		}
	}
}
