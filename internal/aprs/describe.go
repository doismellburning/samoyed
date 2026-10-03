// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package aprs

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/kiss"
)

// hexLineRegexp matches a line of raw AX.25 or KISS bytes, e.g. "DE AD BE EF" or "DEADBEEF".
var hexLineRegexp = regexp.MustCompile(`^[[:xdigit:]]{2}( ?[[:xdigit:]]{2})*$`)

// DescribeLine prints, for a person to read, everything that can be made of
// one line of input: a packet in the usual monitoring format, as a TNC,
// findu.com or aprs.fi show it, or hexadecimal bytes of a raw AX.25 frame or
// a KISS frame (which begins with 00 or C0, impossible for an AX.25 address).
// This is what samoyed-decode_aprs does with each line it reads.
func (d *Decoder) DescribeLine(line string) {
	/* Try to process it. */
	fmt.Printf("\n")
	ax25.SafePrint([]byte(line), false)
	fmt.Printf("\n")

	// Do we have monitor format, KISS, or AX.25 frame?

	line = strings.TrimLeft(line, " ")

	if hexLineRegexp.MatchString(line) {
		// Documented input format is "DE AD BE EF"
		// Go's hex.DecodeString will decode "DEADBEEF"
		// So, let's just strip spaces and use that!
		var spacelessLine = strings.ReplaceAll(line, " ", "")

		var bytes, err = hex.DecodeString(spacelessLine)
		if err != nil {
			fmt.Printf("Could not decode hexadecimal input: %v\n", err)

			return
		}

		// If we have 0xC0 at start, remove it and expect same at end.

		if bytes[0] == kiss.FEND {
			if len(bytes) < 2 || bytes[1] != 0 {
				fmt.Printf("Was expecting to find 00 after the initial C0.\n")

				return
			}

			if bytes[len(bytes)-1] == kiss.FEND {
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
			dwutil.HexDump(kiss_frame)

			// Put FEND at end to keep kiss.Unwrap happy.
			// Having one at the beginning is optional.

			kiss_frame = append(kiss_frame, kiss.FEND)

			// In the more general case, we would need to include
			// the command byte because it could be escaped.
			// Here we know it is 0, so we take a short cut and
			// remove it before, rather than after, the conversion.

			bytes = kiss.Unwrap(kiss_frame[1:])
		}

		// Treat as AX.25.

		var alevel ax25.ALevel

		var pp = ax25.FromFrame(bytes, alevel)
		if pp != nil {
			fmt.Printf("--- AX.25 frame ---\n")
			pp.HexDump()
			fmt.Printf("-------------------\n")

			var addrs = pp.FormatAddrs()
			fmt.Printf("%s", addrs)

			var info = pp.Info()
			ax25.SafePrint(info, true) // Display non-ASCII to hexadecimal.
			fmt.Printf("\n")

			var A = d.Decode(pp, false) // Extract information into structure.

			d.Print(A) // Now print it in human readable format.

			pp.CheckAddresses(ax25.AddrStrictLowerCaseWarning) // Errors for invalid addresses.
		} else {
			fmt.Printf("Could not construct AX.25 frame from bytes supplied!\n\n")
		}
	} else {
		// Normal monitoring format.
		var pp = ax25.FromTextWithStrictness(line, ax25.AddrStrictLowerCaseWarning)
		if pp != nil {
			var A = d.Decode(pp, false) // Extract information into structure.

			d.Print(A) // Now print it in human readable format.

			// This seems to be redundant because we used strict option
			// when parsing the monitoring format text.
			// (void)AX25CheckAddresses(pp, AddrStrictLowerCaseWarning);	// Errors for invalid addresses.

			// Future?  Add -d option to include hex dump and maybe KISS?
		} else {
			fmt.Printf("ERROR - Could not parse monitoring format input!\n\n")
		}
	}
}
