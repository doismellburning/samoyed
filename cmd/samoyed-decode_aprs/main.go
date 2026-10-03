package main

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
 *		00 82 a0 ae ae 62 60 e0 82 96 68 84 40 40 60 9c 68 b0 ae 86 40 e0 40 ae 92 88 8a 64 63 03 f0 3e 45
 *		4d 36 34 6e 65 2f 23 20 45 63 68 6f 6c 69 6e 6b 20 31 34 35 2e 33 31 30 2f 31 30 30 68 7a 20 54 6f 6e 65
 *
 *      or without spaces:
 *
 *      0082a0aeae6260e0829668844040609c68b0ae8640e040ae92888a646303f03e454d36346e652f23204563686f6c696e6b203134352e3331302f313030687a20546f6e65
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
 *		AX25FromText recognizes this representation so it can be used
 *		to decode raw data later.
 *
 * TODO:	To make it more useful,
 *			- Remove any leading timestamp.
 *			- Remove any "qA*" and following from the path.
 *			- Handle non-APRS frames properly.
 *
 *------------------------------------------------------------------*/

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/doismellburning/samoyed/internal/aprs"
)

func main() {
	var aprsDecoder = aprs.NewDecoderFromDataFiles()

	var scanner = bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var line = scanner.Text()
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			/* comment or blank line */
			fmt.Printf("%s\n", line)

			continue
		}

		aprsDecoder.DescribeLine(line)
	}

	var err = scanner.Err()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading standard input: %v\n", err)
		os.Exit(1)
	}
}
