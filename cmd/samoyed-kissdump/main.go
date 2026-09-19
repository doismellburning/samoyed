/*
 * Decode a captured KISS byte stream.
 *
 * When a third party KISS client misbehaves, the question is what was actually
 * on the wire.  Given a capture of that, this says what it contains: the KISS
 * framing, the command byte and port number, the AX.25 header, and the
 * information field decoded as APRS where that applies.
 *
 * Anything malformed is reported rather than quietly skipped - a frame that
 * never ended, an escape sequence that isn't one, or a frame too short to hold
 * an AX.25 header is exactly the bug being chased.
 */
package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/spf13/pflag"
)

func main() {
	var hexInput = pflag.Bool("hex", false, "Input is hexadecimal digits rather than raw bytes, e.g. \"c0 00 82\" or \"c00082\".  Whitespace is ignored, anything else is an error.")
	var help = pflag.Bool("help", false, "Display help text.")

	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s decodes a captured KISS byte stream, read from stdin.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Malformed framing is reported rather than skipped, and the exit status is non-zero if anything was wrong.\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTION]... < CAPTURE\n", os.Args[0])
		pflag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "$ %s < capture.bin\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "$ %s --hex < capture.txt\n", os.Args[0])
	}

	pflag.Parse()

	if *help {
		pflag.Usage()
		os.Exit(0)
	}

	if pflag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Unexpected argument \"%s\" - the capture is read from stdin.\n", pflag.Arg(0))
		pflag.Usage()
		os.Exit(1)
	}

	direwolf.TextColorInit(0)
	direwolf.DecodeAPRSInit()

	var capture, readErr = io.ReadAll(os.Stdin)
	if readErr != nil {
		fmt.Fprintf(os.Stderr, "Could not read the capture from stdin: %s\n", readErr)
		os.Exit(1)
	}

	if dumpCapture(capture, *hexInput) > 0 {
		os.Exit(1)
	}
}

/*
 * Describe a whole capture, returning the number of problems found so the
 * caller can exit non-zero when it contained something malformed.
 */

func dumpCapture(capture []byte, hexInput bool) int {
	if hexInput {
		var decoded, err = fromHex(capture)
		if err != nil {
			fmt.Printf("ERROR: %s\n", err)

			return 1
		}

		capture = decoded
	}

	if len(capture) == 0 {
		fmt.Printf("ERROR: The capture is empty.\n")

		return 1
	}

	var problems = 0

	/*
	 * A frame is FEND, contents, FEND.  The closing FEND of one frame doubles
	 * as the opening FEND of the next, and a TNC is free to pad with extra
	 * FENDs, so anything empty between two of them is skipped without comment.
	 */

	var first = bytes.IndexByte(capture, direwolf.FEND)

	switch {
	case first < 0:
		fmt.Printf("ERROR: No FEND (0x%02x) in %d bytes - this does not look like a KISS capture.\n", direwolf.FEND, len(capture))

		return 1
	case first > 0:
		fmt.Printf("ERROR: %s before the first FEND (0x%02x) not part of any frame.\n", plural(first, "byte"), direwolf.FEND)
		direwolf.HexDump(capture[:first])

		problems++
	}

	var number = 0

	for pos := first; pos < len(capture); {
		var end = bytes.IndexByte(capture[pos+1:], direwolf.FEND)
		var unterminated = end < 0
		var contents []byte

		if unterminated {
			contents = capture[pos+1:]
		} else {
			contents = capture[pos+1 : pos+1+end]
		}

		if len(contents) > 0 {
			number++
			problems += dumpFrame(number, pos, contents, unterminated)
		}

		pos += 1 + len(contents)
	}

	if number == 0 {
		/*
		 * All FENDs and nothing between them.  A TNC pads with those, but a
		 * capture made only of padding is not one anybody wanted to look at.
		 */
		fmt.Printf("ERROR: No frames in the capture - %s of FEND (0x%02x) padding and nothing else.\n", plural(len(capture), "byte"), direwolf.FEND)

		problems++
	}

	fmt.Printf("\n%s, %s.\n", plural(number, "frame"), plural(problems, "problem"))

	return problems
}

/*
 * Convert hexadecimal digits, such as those quoted in a bug report, back into
 * the bytes they describe.  Whitespace is ignored, so how the digits are laid
 * out across lines does not matter, but everything else has to be a digit:
 * the offsets and ASCII column of a hex dump would otherwise be read as data.
 */

func fromHex(in []byte) ([]byte, error) {
	var digits strings.Builder

	var line = 1

	for _, b := range in {
		switch {
		case b == '\n':
			line++
		case b == ' ' || b == '\t' || b == '\r' || b == '\v' || b == '\f':
			/* Ignored. */
		case isHexDigit(b):
			digits.WriteByte(b)
		default:
			return nil, fmt.Errorf("line %d: '%c' (0x%02x) is not a hexadecimal digit", line, b, b)
		}
	}

	if digits.Len() == 0 {
		return nil, fmt.Errorf("no hexadecimal digits in %d bytes of input", len(in))
	}

	if digits.Len()%2 != 0 {
		return nil, fmt.Errorf("%d hexadecimal digits is an odd number - one byte is missing a digit", digits.Len())
	}

	return hex.DecodeString(digits.String())
}

func isHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

/*
 * Describe one frame: the bytes between two FENDs, still containing any escape
 * sequences.  offset is where its opening FEND was in the capture, and
 * unterminated says the capture ended without a closing one.
 */

func dumpFrame(number int, offset int, contents []byte, unterminated bool) int {
	var problems = 0

	fmt.Printf("\n--- KISS frame %d, %s at offset %d ---\n", number, plural(len(contents), "byte"), offset)
	direwolf.HexDump(contents)

	if unterminated {
		fmt.Printf("ERROR: Frame is not terminated - the capture ends without a closing FEND (0x%02x).\n", direwolf.FEND)

		problems++
	}

	var frame, escapeProblems = direwolf.KissUnescape(contents)

	for _, problem := range escapeProblems {
		fmt.Printf("ERROR: %s.\n", problem)
	}

	problems += len(escapeProblems)

	if len(frame) == 0 {
		fmt.Printf("ERROR: Nothing left after removing the escapes, so there is no command byte.\n")

		return problems + 1
	}

	if len(frame) != len(contents) {
		fmt.Printf("%s after removing the escapes:\n", plural(len(frame), "byte"))
		direwolf.HexDump(frame)
	}

	var command = frame[0] & 0x0f
	var port = (frame[0] >> 4) & 0x0f

	fmt.Printf("KISS command byte 0x%02x: command %d (%s), port %d\n", frame[0], command, commandName(command), port)

	return problems + dumpCommand(command, frame[1:])
}

/* Text description of the command in the lower nybble of the command byte. */

func commandName(command byte) string {
	switch command {
	case direwolf.KISS_CMD_DATA_FRAME:
		return "Data frame"
	case direwolf.KISS_CMD_TXDELAY:
		return "TXDELAY"
	case direwolf.KISS_CMD_PERSISTENCE:
		return "Persistence"
	case direwolf.KISS_CMD_SLOTTIME:
		return "SlotTime"
	case direwolf.KISS_CMD_TXTAIL:
		return "TXtail"
	case direwolf.KISS_CMD_FULLDUPLEX:
		return "FullDuplex"
	case direwolf.KISS_CMD_SET_HARDWARE:
		return "SetHardware"
	case direwolf.XKISS_CMD_DATA:
		return "XKISS data"
	case direwolf.XKISS_CMD_POLL:
		return "XKISS poll"
	case direwolf.KISS_CMD_END_KISS:
		return "Return - exit KISS mode"
	default:
		return "invalid"
	}
}

/* Describe everything after the command byte. */

func dumpCommand(command byte, payload []byte) int {
	switch command {
	case direwolf.KISS_CMD_DATA_FRAME:
		return direwolf.DescribeAX25Frame(payload)

	case direwolf.KISS_CMD_TXDELAY, direwolf.KISS_CMD_PERSISTENCE, direwolf.KISS_CMD_SLOTTIME, direwolf.KISS_CMD_TXTAIL, direwolf.KISS_CMD_FULLDUPLEX:
		if len(payload) != 1 {
			fmt.Printf("ERROR: %s takes exactly one parameter byte, not %d.\n", commandName(command), len(payload))

			return 1
		}

		dumpParameter(command, payload[0])

		return 0

	case direwolf.KISS_CMD_SET_HARDWARE:
		if len(payload) == 0 {
			fmt.Printf("ERROR: SetHardware has no payload - there is nothing to set.\n")

			return 1
		}

		fmt.Printf("TNC-specific: ")
		direwolf.AX25SafePrint(payload, true)
		fmt.Printf("\n")
		direwolf.NoteSafePrintTruncation(len(payload))

		return 0

	case direwolf.KISS_CMD_END_KISS:
		if len(payload) != 0 {
			fmt.Printf("ERROR: Return takes no parameters, but %s follows the command byte.\n", plural(len(payload), "byte"))

			return 1
		}

		return 0

	case direwolf.XKISS_CMD_DATA, direwolf.XKISS_CMD_POLL:
		fmt.Printf("ERROR: Command %d (%s) is an XKISS extension, which is not supported.\n", command, commandName(command))

		return 1

	default:
		fmt.Printf("ERROR: Command %d is not part of the KISS protocol.\n", command)

		return 1
	}
}

/* What the single parameter byte of a channel setting means. */

func dumpParameter(command byte, value byte) {
	switch command {
	case direwolf.KISS_CMD_TXDELAY:
		fmt.Printf("Transmit delay = %d, i.e. %d ms.\n", value, int(value)*10)
	case direwolf.KISS_CMD_PERSISTENCE:
		fmt.Printf("Persistence = %d, i.e. p = %.3f.\n", value, float64(int(value)+1)/256)
	case direwolf.KISS_CMD_SLOTTIME:
		fmt.Printf("Slot time = %d, i.e. %d ms.\n", value, int(value)*10)
	case direwolf.KISS_CMD_TXTAIL:
		fmt.Printf("Transmit tail = %d, i.e. %d ms.\n", value, int(value)*10)
	case direwolf.KISS_CMD_FULLDUPLEX:
		fmt.Printf("Full duplex = %d, i.e. %s.\n", value, direwolf.IfThenElse(value == 0, "half duplex", "full duplex"))
	}
}

/* "1 frame", "2 frames", and so on. */

func plural(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, noun)
	}

	return fmt.Sprintf("%d %ss", count, noun)
}
