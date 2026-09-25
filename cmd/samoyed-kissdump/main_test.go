package main

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dumpCaptureOutput runs dumpCapture with stdout redirected, returning what it
// printed along with the number of problems it reported.
func dumpCaptureOutput(t *testing.T, capture []byte, hexInput bool) (string, int) {
	t.Helper()

	direwolf.TextColorInit(0)
	direwolf.DecodeAPRSInit()

	var tmp, createErr = os.CreateTemp(t.TempDir(), "kissdump")
	require.NoError(t, createErr)

	var oldStdout = os.Stdout

	defer func() {
		os.Stdout = oldStdout
	}()

	os.Stdout = tmp

	var problems = dumpCapture(capture, hexInput)

	os.Stdout = oldStdout

	require.NoError(t, tmp.Close())

	var output, readErr = os.ReadFile(tmp.Name())
	require.NoError(t, readErr)

	return string(output), problems
}

// testCapture wraps an AX.25 frame in the KISS framing of a data frame.
func testCapture(t *testing.T, monitor string) []byte {
	t.Helper()

	return direwolf.KissEncapsulate(append([]byte{0x00}, direwolf.AX25Pack(direwolf.MustAX25FromText(monitor))...))
}

func Test_APRS(t *testing.T) {
	var capture = testCapture(t, "Q1TEST-9>APDW17,WIDE1-1:!4237.14NS07120.83W#PHG7130Chelmsford, MA")

	var output, problems = dumpCaptureOutput(t, capture, false)

	assert.Equal(t, 0, problems)
	assert.Contains(t, output, "command 0 (Data frame), port 0")
	assert.Contains(t, output, "Q1TEST-9>APDW17,WIDE1-1:")
	assert.Contains(t, output, "N 42°37.1400, W 071°20.8300")
	assert.Contains(t, output, "1 frame, 0 problems.")
}

// The channel is in the upper nybble of the command byte.
func Test_Port(t *testing.T) {
	var frame = direwolf.AX25Pack(direwolf.MustAX25FromText("Q1TEST>APDW17:>Testing"))

	var output, problems = dumpCaptureOutput(t, direwolf.KissEncapsulate(append([]byte{0x30}, frame...)), false)

	assert.Equal(t, 0, problems)
	assert.Contains(t, output, "command 0 (Data frame), port 3")
}

// Escaped bytes should come back as the bytes they stand for.
func Test_Escapes(t *testing.T) {
	var frame = direwolf.AX25Pack(direwolf.MustAX25FromText("Q1TEST>APDW17:>FEND <0xc0> and FESC <0xdb>"))

	var capture = direwolf.KissEncapsulate(append([]byte{0x00}, frame...))

	// The escaping should have made the frame longer than its contents.
	require.Greater(t, len(capture), len(frame)+3)

	var output, problems = dumpCaptureOutput(t, capture, false)

	assert.Equal(t, 0, problems)
	assert.Contains(t, output, "after removing the escapes")
	assert.Contains(t, output, "Q1TEST>APDW17:")
}

// Extra FENDs are padding, not empty frames.
func Test_Padding(t *testing.T) {
	var capture = []byte{direwolf.FEND, direwolf.FEND}
	capture = append(capture, testCapture(t, "Q1TEST>APDW17:>Testing")...)
	capture = append(capture, direwolf.FEND, direwolf.FEND)

	var output, problems = dumpCaptureOutput(t, capture, false)

	assert.Equal(t, 0, problems)
	assert.Contains(t, output, "1 frame, 0 problems.")
}

func Test_Unterminated(t *testing.T) {
	var capture = testCapture(t, "Q1TEST>APDW17:>Testing")

	var output, problems = dumpCaptureOutput(t, capture[:len(capture)-1], false)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "Frame is not terminated")
	// The rest of the frame should still be decoded.
	assert.Contains(t, output, "Q1TEST>APDW17:")
}

func Test_BadEscape(t *testing.T) {
	var capture = testCapture(t, "Q1TEST>APDW17:>Testing")

	// 0x41 is neither TFEND nor TFESC.
	capture = append(capture[:len(capture)-1], direwolf.FESC, 0x41, direwolf.FEND)

	var output, problems = dumpCaptureOutput(t, capture, false)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "not TFEND (0xdc) or TFESC (0xdd)")
}

func Test_TruncatedEscape(t *testing.T) {
	var output, problems = dumpCaptureOutput(t, []byte{direwolf.FEND, 0x00, direwolf.FESC, direwolf.FEND}, false)

	assert.Equal(t, 2, problems) // Truncated escape, and then too short for AX.25.
	assert.Contains(t, output, "the escaped byte is missing")
}

func Test_ShortFrame(t *testing.T) {
	var output, problems = dumpCaptureOutput(t, []byte{direwolf.FEND, 0x00, 0x82, 0xa0, direwolf.FEND}, false)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "too short for an AX.25 header of at least 15")
}

func Test_NoCommandByte(t *testing.T) {
	var output, problems = dumpCaptureOutput(t, []byte{direwolf.FEND, direwolf.FESC, direwolf.FEND}, false)

	assert.Equal(t, 2, problems) // Truncated escape, and then nothing left.
	assert.Contains(t, output, "there is no command byte")
}

// The end of address bit has to mark out whole 7 byte addresses.
func Test_MalformedAddresses(t *testing.T) {
	var frame = direwolf.AX25Pack(direwolf.MustAX25FromText("Q1TEST>APDW17:>Testing"))

	frame[13] &= ^byte(direwolf.SSID_LAST_MASK) // Clear the end of address bit.

	var output, problems = dumpCaptureOutput(t, direwolf.KissEncapsulate(append([]byte{0x00}, frame...)), false)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "The address field is malformed")
}

// A frame that stops at the end of its address field has no control byte, so
// nothing should be said about one: the octets a control and PID would have
// been are past the end of the frame.
func Test_NoControlByte(t *testing.T) {
	var frame = direwolf.AX25Pack(direwolf.MustAX25FromText("Q1TEST>Q2TEST,Q3TEST:>Testing"))

	var addressesOnly = frame[:21] // Three addresses of 7 bytes, and nothing else.

	var output, problems = dumpCaptureOutput(t, direwolf.KissEncapsulate(append([]byte{0x00}, addressesOnly...)), false)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "there is no control byte")
	assert.NotContains(t, output, "frame:")              // e.g. "I frame: n(r)=0, ..."
	assert.NotContains(t, output, "Unknown protocol id") // From the zero past the end.
}

// AX25SafePrint stops after MAXSAFE bytes, which has to be said out loud.
func Test_SetHardwareTruncated(t *testing.T) {
	var capture = []byte{direwolf.FEND, direwolf.KISS_CMD_SET_HARDWARE}
	capture = append(capture, bytes.Repeat([]byte("x"), direwolf.MAXSAFE+100)...)
	capture = append(capture, direwolf.FEND)

	var output, problems = dumpCaptureOutput(t, capture, false)

	assert.Equal(t, 0, problems)
	assert.Contains(t, output, fmt.Sprintf("(Only the first %d of %d bytes are shown above.)", direwolf.MAXSAFE, direwolf.MAXSAFE+100))
}

// A UI frame whose PID octet was lost ends before the information field.
func Test_NoPID(t *testing.T) {
	var frame = direwolf.AX25Pack(direwolf.MustAX25FromText("Q1TEST>APDW17:>Testing"))

	var output, problems = dumpCaptureOutput(t, direwolf.KissEncapsulate(append([]byte{0x00}, frame[:15]...)), false)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "it ends before the information field")
}

// Connected mode frames are AX.25 but not APRS.
func Test_NotAPRS(t *testing.T) {
	var frame = direwolf.AX25Pack(direwolf.MustAX25FromText("Q1TEST>Q2TEST:>Testing"))

	var sabm = append(frame[:14:14], 0x3f) // Addresses, then SABM with P=1.

	var output, problems = dumpCaptureOutput(t, direwolf.KissEncapsulate(append([]byte{0x00}, sabm...)), false)

	assert.Equal(t, 0, problems)
	assert.Contains(t, output, "U frame SABM")
	assert.Contains(t, output, "not decoded as APRS")
}

// A UI frame with a PID other than 0xf0 is not APRS either, and the control
// byte is not the thing to blame for it.
func Test_NotAPRSByPID(t *testing.T) {
	var frame = direwolf.AX25Pack(direwolf.MustAX25FromText("Q1TEST>Q2TEST:>Testing"))

	frame[15] = 0xcf // NET/ROM rather than no layer 3 protocol.

	var output, problems = dumpCaptureOutput(t, direwolf.KissEncapsulate(append([]byte{0x00}, frame...)), false)

	assert.Equal(t, 0, problems)
	assert.Contains(t, output, "U frame UI")
	assert.Contains(t, output, "NET/ROM")
	assert.Contains(t, output, "not decoded as APRS")
	assert.NotContains(t, output, "Control 0x")
}

func Test_Parameters(t *testing.T) {
	var output, problems = dumpCaptureOutput(t, []byte{
		direwolf.FEND, direwolf.KISS_CMD_TXDELAY, 30, direwolf.FEND,
		direwolf.KISS_CMD_PERSISTENCE, 63, direwolf.FEND,
		direwolf.KISS_CMD_SLOTTIME, 10, direwolf.FEND,
		direwolf.KISS_CMD_TXTAIL, 5, direwolf.FEND,
		direwolf.KISS_CMD_FULLDUPLEX, 0, direwolf.FEND,
		direwolf.KISS_CMD_END_KISS, direwolf.FEND,
	}, false)

	assert.Equal(t, 0, problems)
	assert.Contains(t, output, "Transmit delay = 30, i.e. 300 ms.")
	assert.Contains(t, output, "Persistence = 63, i.e. p = 0.250.")
	assert.Contains(t, output, "Slot time = 10, i.e. 100 ms.")
	assert.Contains(t, output, "Transmit tail = 5, i.e. 50 ms.")
	assert.Contains(t, output, "Full duplex = 0, i.e. half duplex.")
	assert.Contains(t, output, "command 15 (Return - exit KISS mode)")
	assert.Contains(t, output, "6 frames, 0 problems.")
}

func Test_ParameterWrongLength(t *testing.T) {
	var output, problems = dumpCaptureOutput(t, []byte{direwolf.FEND, direwolf.KISS_CMD_SLOTTIME, 10, 10, direwolf.FEND}, false)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "SlotTime takes exactly one parameter byte, not 2")
}

func Test_SetHardware(t *testing.T) {
	var capture = []byte{direwolf.FEND, direwolf.KISS_CMD_SET_HARDWARE}
	capture = append(capture, []byte("TXBUF:1")...)
	capture = append(capture, direwolf.FEND)

	var output, problems = dumpCaptureOutput(t, capture, false)

	assert.Equal(t, 0, problems)
	assert.Contains(t, output, "TNC-specific: TXBUF:1")
}

func Test_XKISS(t *testing.T) {
	var output, problems = dumpCaptureOutput(t, []byte{direwolf.FEND, direwolf.XKISS_CMD_POLL, direwolf.FEND}, false)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "is an XKISS extension, which is not supported")
}

func Test_InvalidCommand(t *testing.T) {
	var output, problems = dumpCaptureOutput(t, []byte{direwolf.FEND, 0x07, direwolf.FEND}, false)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "Command 7 is not part of the KISS protocol")
}

func Test_JunkBeforeFirstFEND(t *testing.T) {
	var capture = append([]byte{0xde, 0xad}, testCapture(t, "Q1TEST>APDW17:>Testing")...)

	var output, problems = dumpCaptureOutput(t, capture, false)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "2 bytes before the first FEND")
}

func Test_NoFEND(t *testing.T) {
	var output, problems = dumpCaptureOutput(t, []byte{0x00, 0x82, 0xa0}, false)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "does not look like a KISS capture")
}

// Padding and nothing else is not a capture anyone can learn anything from.
func Test_OnlyPadding(t *testing.T) {
	var output, problems = dumpCaptureOutput(t, []byte{direwolf.FEND, direwolf.FEND, direwolf.FEND}, false)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "No frames in the capture")
	assert.Contains(t, output, "0 frames, 1 problem.")
}

func Test_Empty(t *testing.T) {
	var output, problems = dumpCaptureOutput(t, []byte{}, false)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "The capture is empty")
}

// Hex input should describe the same capture as the bytes it stands for,
// however it happens to be laid out.
func Test_Hex(t *testing.T) {
	var capture = testCapture(t, "Q1TEST-9>APDW17,WIDE1-1:!4237.14NS07120.83W#Testing")

	var rawOutput, rawProblems = dumpCaptureOutput(t, capture, false)

	var spaced bytes.Buffer

	for i, b := range capture {
		if i > 0 {
			if i%16 == 0 {
				spaced.WriteString("\n")
			} else {
				spaced.WriteString(" ")
			}
		}

		spaced.WriteString(hexDigits(b))
	}

	var hexOutput, hexProblems = dumpCaptureOutput(t, spaced.Bytes(), true)

	assert.Equal(t, rawProblems, hexProblems)
	assert.Equal(t, rawOutput, hexOutput)
}

func hexDigits(b byte) string {
	const digits = "0123456789ABCDEF"

	return string([]byte{digits[b>>4], digits[b&0x0f]})
}

func Test_HexNotHex(t *testing.T) {
	var output, problems = dumpCaptureOutput(t, []byte("c0 00\nc0 zz\n"), true)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "line 2: 'z' (0x7a) is not a hexadecimal digit")
}

func Test_HexOddDigits(t *testing.T) {
	var output, problems = dumpCaptureOutput(t, []byte("c0 00 c"), true)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "is an odd number")
}

func Test_HexEmpty(t *testing.T) {
	var output, problems = dumpCaptureOutput(t, []byte("   \n\n"), true)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "no hexadecimal digits")
}

// Test_XKISS covers polling; data is the other XKISS command.
func Test_XKISSData(t *testing.T) {
	var output, problems = dumpCaptureOutput(t, []byte{direwolf.FEND, direwolf.XKISS_CMD_DATA, direwolf.FEND}, false)

	assert.Equal(t, 1, problems)
	assert.Contains(t, output, "Command 12 (XKISS data) is an XKISS extension, which is not supported.")
}

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

func Test_main(t *testing.T) {
	var capture = testCapture(t, "Q1TEST>APDW17:>Testing")

	t.Run("raw", func(t *testing.T) {
		var result = testutils.RunMain(t, string(capture))
		var out, status = result.Output(), result.Status

		assert.Equal(t, 0, status)
		assert.Contains(t, out, "Q1TEST>APDW17:")
		assert.Contains(t, out, "Status Report")
		assert.Contains(t, out, "1 frame, 0 problems.")
	})

	t.Run("hex", func(t *testing.T) {
		var result = testutils.RunMain(t, fmt.Sprintf("% x\n", capture), "--hex")
		var out, status = result.Output(), result.Status

		assert.Equal(t, 0, status)
		assert.Contains(t, out, "Q1TEST>APDW17:")
		assert.Contains(t, out, "1 frame, 0 problems.")
	})

	t.Run("problems", func(t *testing.T) {
		var result = testutils.RunMain(t, string(direwolf.KissEncapsulate([]byte{0x07})))
		var out, status = result.Output(), result.Status

		assert.Equal(t, 1, status)
		assert.Contains(t, out, "1 problem.")
	})

	t.Run("unexpected argument", func(t *testing.T) {
		var result = testutils.RunMain(t, "", "capture.bin")
		var out, status = result.Output(), result.Status

		assert.Equal(t, 1, status)
		assert.Contains(t, out, `Unexpected argument "capture.bin" - the capture is read from stdin.`)
	})

	t.Run("help", func(t *testing.T) {
		var result = testutils.RunMain(t, "", "--help")
		var out, status = result.Output(), result.Status

		assert.Equal(t, 0, status)
		assert.Contains(t, out, "decodes a captured KISS byte stream")
	})
}
