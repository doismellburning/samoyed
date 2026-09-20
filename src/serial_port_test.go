//go:build unix

package direwolf

import (
	"os"
	"testing"

	"github.com/creack/pty"
	"github.com/pkg/term"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openTestSerialPort gives a serial port the tests can talk to without any
// hardware: a pseudo-terminal pair, with SerialPortOpen on the slave side and
// the master returned so a test can play the other end of the wire.
func openTestSerialPort(t *testing.T, baud int) (*term.Term, *os.File) {
	t.Helper()

	var master, slave, openErr = pty.Open()
	require.NoError(t, openErr)

	// SerialPortOpen opens the slave by name, so we only need its name here.
	require.NoError(t, slave.Close())

	t.Cleanup(func() { master.Close() })

	var fd = SerialPortOpen(slave.Name(), baud)
	require.NotNil(t, fd, "SerialPortOpen(%s)", slave.Name())

	t.Cleanup(func() { serial_port_close(fd) })

	return fd, master
}

func TestSerialPortOpenNonexistentDevice(t *testing.T) {
	var fd *term.Term

	var output = CaptureOutput(t, func() {
		fd = SerialPortOpen("/dev/there-is-no-such-serial-port", 9600)
	})

	assert.Nil(t, fd)
	assert.Contains(t, output, "Could not open serial port /dev/there-is-no-such-serial-port")
}

// A speed we know about is set without comment.
func TestSerialPortOpenSupportedSpeed(t *testing.T) {
	var output = CaptureOutput(t, func() {
		openTestSerialPort(t, 9600)
	})

	assert.NotContains(t, output, "Unsupported speed")
}

// A speed of 0 means "leave whatever the device already had alone", which is
// not the same thing as an unsupported speed.
func TestSerialPortOpenSpeedZeroLeavesItAlone(t *testing.T) {
	var output = CaptureOutput(t, func() {
		openTestSerialPort(t, 0)
	})

	assert.NotContains(t, output, "Unsupported speed")
}

// Anything else says so and falls back to 4800 rather than failing the open.
func TestSerialPortOpenUnsupportedSpeed(t *testing.T) {
	var fd *term.Term

	var output = CaptureOutput(t, func() {
		fd, _ = openTestSerialPort(t, 1234)
	})

	assert.NotNil(t, fd)
	assert.Contains(t, output, "Unsupported speed 1234")
	assert.Contains(t, output, "Using 4800")
}

func TestSerialPortWrite(t *testing.T) {
	var fd, master = openTestSerialPort(t, 9600)

	var data = []byte("Q1TEST")

	assert.Equal(t, len(data), SerialPortWrite(fd, data))

	var readBack = make([]byte, len(data))
	var n, readErr = master.Read(readBack)

	require.NoError(t, readErr)
	assert.Equal(t, data, readBack[:n])
}

// A nil handle is what SerialPortOpen returns on failure, and callers hang on
// to it, so writing to one has to be an error rather than a panic.
func TestSerialPortWriteNilHandle(t *testing.T) {
	assert.Equal(t, -1, SerialPortWrite(nil, []byte("Q1TEST")))
}

func TestSerialPortWriteAfterClose(t *testing.T) {
	var fd, _ = openTestSerialPort(t, 9600)

	serial_port_close(fd)

	assert.Equal(t, -1, SerialPortWrite(fd, []byte("Q1TEST")))
}

func TestSerialPortGet1(t *testing.T) {
	var fd, master = openTestSerialPort(t, 9600)

	var _, writeErr = master.WriteString("Q2")
	require.NoError(t, writeErr)

	var first, firstErr = SerialPortGet1(fd)
	require.NoError(t, firstErr)
	assert.Equal(t, byte('Q'), first)

	var second, secondErr = SerialPortGet1(fd)
	require.NoError(t, secondErr)
	assert.Equal(t, byte('2'), second)
}

// The far end going away is reported, rather than looking like a byte of 0.
func TestSerialPortGet1AfterFarEndCloses(t *testing.T) {
	var fd, master = openTestSerialPort(t, 9600)

	require.NoError(t, master.Close())

	var b, err = SerialPortGet1(fd)

	require.Error(t, err)
	assert.Equal(t, byte(0), b)
}

func TestSerialPortCloseNilHandle(t *testing.T) {
	assert.NotPanics(t, func() { serial_port_close(nil) })
}
