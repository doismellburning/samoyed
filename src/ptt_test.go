package direwolf

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// mockGPIODLine is a test double for gpiodOutputLine that records calls
// without requiring GPIO hardware or the gpio-sim kernel module.
type mockGPIODLine struct {
	value  int
	closed bool
}

func (m *mockGPIODLine) SetValue(v int) error {
	m.value = v

	return nil
}

func (m *mockGPIODLine) Close() error {
	m.closed = true

	return nil
}

// testPTT is a PTT for cfg that has not been set up, so that a test can drive
// one part of it at a time.  GPIO goes through gpioSysfsDir, which a test
// points at a temporary directory standing in for /sys/class/gpio.
func testPTT(cfg *audio_s, gpioSysfsDir string) *PTT {
	var p = new(PTT)
	p.audioConfig = cfg
	p.gpioSysfsDir = gpioSysfsDir

	return p
}

// setupGPIODChannel makes a PTT whose channel 0 OCTYPE_PTT is a GPIOD line,
// returning it and the mock standing in for the line so the caller can
// inspect it.
func setupGPIODChannel(t *testing.T, invert bool) (*PTT, *mockGPIODLine) {
	t.Helper()

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_GPIOD
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 0
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_invert = invert

	var p = testPTT(cfg, t.TempDir())

	var mock = new(mockGPIODLine)
	p.gpiodLine[0][OCTYPE_PTT] = mock

	return p, mock
}

// usePTT makes a PTT for cfg the one the rest of the package keys, for the
// duration of the test.
func usePTT(t *testing.T, cfg *audio_s) {
	t.Helper()

	var p, err = NewPTT(cfg, 0)
	require.NoError(t, err)

	var saved = pttControl

	pttControl = p

	t.Cleanup(func() {
		p.Term()

		pttControl = saved
	})
}

// TestPttSetRealGPIOD_Activate verifies that PTT-active drives the line high.
func TestPttSetRealGPIOD_Activate(t *testing.T) {
	var p, mock = setupGPIODChannel(t, false)

	p.Set(OCTYPE_PTT, 0, 1)

	assert.Equal(t, 1, mock.value, "line should be high when PTT is active")
}

// TestPttSetRealGPIOD_Deactivate verifies that PTT-inactive drives the line low.
func TestPttSetRealGPIOD_Deactivate(t *testing.T) {
	var p, mock = setupGPIODChannel(t, false)

	p.Set(OCTYPE_PTT, 0, 0)

	assert.Equal(t, 0, mock.value, "line should be low when PTT is inactive")
}

// TestPttSetRealGPIOD_Invert_Activate verifies that ptt_invert flips the level
// when PTT is active (signal=1 → line low).
func TestPttSetRealGPIOD_Invert_Activate(t *testing.T) {
	var p, mock = setupGPIODChannel(t, true)

	p.Set(OCTYPE_PTT, 0, 1)

	assert.Equal(t, 0, mock.value, "inverted line should be low when PTT is active")
}

// TestPttSetRealGPIOD_Invert_Deactivate verifies that ptt_invert flips the level
// when PTT is inactive (signal=0 → line high).
func TestPttSetRealGPIOD_Invert_Deactivate(t *testing.T) {
	var p, mock = setupGPIODChannel(t, true)

	p.Set(OCTYPE_PTT, 0, 0)

	assert.Equal(t, 1, mock.value, "inverted line should be high when PTT is inactive")
}

// TestPttSetRealGPIOD_NilLine verifies that Set does not panic when
// the GPIOD line handle has not been initialised.
func TestPttSetRealGPIOD_NilLine(t *testing.T) {
	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_GPIOD

	var p = testPTT(cfg, t.TempDir())

	require.NotPanics(t, func() {
		p.Set(OCTYPE_PTT, 0, 1)
	})
}

// TestPttTermGPIOD verifies that Term closes every open line handle and
// sets the slot to nil.
func TestPttTermGPIOD(t *testing.T) {
	var p, mock = setupGPIODChannel(t, false)

	p.Term()

	assert.True(t, mock.closed, "Term should close the line handle")
	assert.Nil(t, p.gpiodLine[0][OCTYPE_PTT], "Term should nil the line handle")
}

// writeFakeGPIOExport creates the "export" file that a kernel with the sysfs
// GPIO interface would offer.
func writeFakeGPIOExport(t *testing.T, dir string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "export"), nil, 0o600))
}

// writeFakeGPIONode creates the per-line directory that udev would create
// once a line has been exported.
func writeFakeGPIONode(t *testing.T, dir string, name string) {
	t.Helper()

	require.NoError(t, os.Mkdir(filepath.Join(dir, name), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name, "direction"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name, "value"), []byte("0"), 0o600))
}

// TestGetAccessToGPIOMissing verifies that an absent GPIO node is reported to
// the caller rather than ending the process.
func TestGetAccessToGPIOMissing(t *testing.T) {
	var err = testPTT(new(audio_s), t.TempDir()).getAccessToGPIO(filepath.Join(t.TempDir(), "export"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "GPIO user interface")
}

// TestGetAccessToGPIOPresent verifies that a node we can stat is accepted.
func TestGetAccessToGPIOPresent(t *testing.T) {
	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)

	require.NoError(t, testPTT(new(audio_s), dir).getAccessToGPIO(filepath.Join(dir, "export")))
}

// TestExportGPIOOutput verifies that exporting an output line writes the line
// number to "export" and leaves the line low, i.e. off.
func TestExportGPIOOutput(t *testing.T) {
	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio25")

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25
	var p = testPTT(cfg, dir)

	require.NoError(t, p.exportGPIO(0, OCTYPE_PTT, false, 1))

	var exported, readErr = os.ReadFile(filepath.Join(dir, "export")) //nolint:gosec
	require.NoError(t, readErr)
	assert.Equal(t, "25", string(exported))

	var direction, dirErr = os.ReadFile(filepath.Join(dir, "gpio25", "direction")) //nolint:gosec
	require.NoError(t, dirErr)
	assert.Equal(t, "low", string(direction), "an output line should start off")
}

// TestExportGPIOOutputInverted verifies that an inverted output line starts
// high, which is still off.
func TestExportGPIOOutputInverted(t *testing.T) {
	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio25")

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25
	var p = testPTT(cfg, dir)

	require.NoError(t, p.exportGPIO(0, OCTYPE_PTT, true, 1))

	var direction, dirErr = os.ReadFile(filepath.Join(dir, "gpio25", "direction")) //nolint:gosec
	require.NoError(t, dirErr)
	assert.Equal(t, "high", string(direction), "an inverted output line should start off, i.e. high")
}

// TestExportGPIOInput verifies that an input line is set to "in" and takes its
// number from the input rather than the output configuration.
func TestExportGPIOInput(t *testing.T) {
	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio7")

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].ictrl[ICTYPE_TXINH].in_gpio_num = 7
	var p = testPTT(cfg, dir)

	require.NoError(t, p.exportGPIO(0, ICTYPE_TXINH, false, 0))

	var direction, dirErr = os.ReadFile(filepath.Join(dir, "gpio7", "direction")) //nolint:gosec
	require.NoError(t, dirErr)
	assert.Equal(t, "in", string(direction))
}

// TestExportGPIOSuffixedNode verifies the CubieBoard-style node names, where
// GPIO 25 appears as something like gpio25_ph11.
func TestExportGPIOSuffixedNode(t *testing.T) {
	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio25_ph11")

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25
	var p = testPTT(cfg, dir)

	require.NoError(t, p.exportGPIO(0, OCTYPE_PTT, false, 1))

	var direction, dirErr = os.ReadFile(filepath.Join(dir, "gpio25_ph11", "direction")) //nolint:gosec
	require.NoError(t, dirErr)
	assert.Equal(t, "low", string(direction))
}

// TestExportGPIONoSuchNode verifies that a line that never appears under
// /sys/class/gpio is reported rather than ending the process.
func TestExportGPIONoSuchNode(t *testing.T) {
	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25
	var p = testPTT(cfg, dir)

	var err = p.exportGPIO(0, OCTYPE_PTT, false, 1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gpio number 25")
}

// TestExportGPIONoSysfs verifies that a system without the sysfs GPIO
// interface is reported rather than ending the process.
func TestExportGPIONoSysfs(t *testing.T) {
	var dir = t.TempDir()

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25
	var p = testPTT(cfg, dir)

	var err = p.exportGPIO(0, OCTYPE_PTT, false, 1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "GPIO user interface")
}

// TestPttInitGPIONoSysfs verifies that NewPTT reports a GPIO channel it
// cannot set up, rather than ending the process, which is what made the
// function untestable.
func TestPttInitGPIONoSysfs(t *testing.T) {
	var dir = t.TempDir()

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_GPIO
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25
	var _, err = newPTT(cfg, 0, dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "GPIO user interface")
}

// TestPttInitGPIO verifies that a GPIO channel that can be set up leaves the
// line off and reports no error.
func TestPttInitGPIO(t *testing.T) {
	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio25")

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_GPIO
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25
	var _, err = newPTT(cfg, 0, dir)
	require.NoError(t, err)

	var direction, dirErr = os.ReadFile(filepath.Join(dir, "gpio25", "direction")) //nolint:gosec
	require.NoError(t, dirErr)
	assert.Equal(t, "low", string(direction))
}

// TestPttInitGPIODRequestFailure verifies that a GPIOD line that cannot be
// requested - here because the chip does not exist - is reported to the
// caller.
func TestPttInitGPIODRequestFailure(t *testing.T) {
	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_GPIOD
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_name = "/dev/samoyed-no-such-gpiochip"
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 17

	var p = testPTT(cfg, t.TempDir())

	var err = p.init()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "/dev/samoyed-no-such-gpiochip")
	assert.Nil(t, p.gpiodLine[0][OCTYPE_PTT], "a line that could not be requested should not be recorded")
}

// TestPttInitRollsBackOnFailure verifies that a failure part way through
// setting up gives back what has been set up already: setting up is
// incremental, and the descriptors and line handles live in a PTT the caller
// never gets.
func TestPttInitRollsBackOnFailure(t *testing.T) {
	var openable = filepath.Join(t.TempDir(), "tty")
	require.NoError(t, os.WriteFile(openable, nil, 0o600))

	var cfg = new(audio_s)
	// A channel whose PTT device opens...
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_SERIAL
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_device = openable
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_line = PTT_LINE_RTS
	// ...followed by one that fails, after the first has been opened.
	cfg.chan_medium[1] = MEDIUM_RADIO
	cfg.achan[1].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_GPIOD
	cfg.achan[1].octrl[OCTYPE_PTT].out_gpio_name = "/dev/samoyed-no-such-gpiochip"

	var p = testPTT(cfg, t.TempDir())

	require.Error(t, p.init())
	assert.Nil(t, p.fd[0][OCTYPE_PTT], "a serial port opened before the failure should be closed again")
}

// TestPttInitSerialOpenFailure verifies that a serial port we cannot open is
// not fatal: the channel falls back to no PTT method, as it always has.
func TestPttInitSerialOpenFailure(t *testing.T) {
	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_SERIAL
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_device = filepath.Join(t.TempDir(), "no-such-tty")
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_line = PTT_LINE_RTS
	var _, err = newPTT(cfg, 0, t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, PTT_METHOD_NONE, cfg.achan[0].octrl[OCTYPE_PTT].ptt_method)
}

// TestGetInputRealGPIO verifies that an input line's value is read from sysfs,
// and that invert flips it.
func TestGetInputRealGPIO(t *testing.T) {
	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio7")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gpio7", "value"), []byte("1"), 0o600))

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].ictrl[ICTYPE_TXINH].method = PTT_METHOD_GPIO
	cfg.achan[0].ictrl[ICTYPE_TXINH].in_gpio_name = "gpio7"
	var p = testPTT(cfg, dir)

	assert.Equal(t, 1, p.GetInput(ICTYPE_TXINH, 0))

	cfg.achan[0].ictrl[ICTYPE_TXINH].invert = true

	assert.Equal(t, 0, p.GetInput(ICTYPE_TXINH, 0), "invert should flip the value read")
}

// TestGetInputRealGPIONoNode verifies that an input line whose sysfs node is
// missing reports an error rather than ending the process.
func TestGetInputRealGPIONoNode(t *testing.T) {
	var dir = t.TempDir()

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].ictrl[ICTYPE_TXINH].method = PTT_METHOD_GPIO
	cfg.achan[0].ictrl[ICTYPE_TXINH].in_gpio_name = "gpio7"
	var p = testPTT(cfg, dir)

	assert.Equal(t, -1, p.GetInput(ICTYPE_TXINH, 0))
}

// TestPttInitGPIOThenSet is a regression test for a GPIO line that never
// moved: exportGPIO worked out which node under /sys/class/gpio belongs to
// the configured line number, but kept that to itself, so Set built its
// path from an empty name and could not open anything.
func TestPttInitGPIOThenSet(t *testing.T) {
	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio25_ph11")

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_GPIO
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25
	var p, err = newPTT(cfg, 0, dir)
	require.NoError(t, err)
	assert.Equal(t, "gpio25_ph11", cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_name,
		"the node name found while exporting should be remembered")

	p.Set(OCTYPE_PTT, 0, 1)

	var on, onErr = os.ReadFile(filepath.Join(dir, "gpio25_ph11", "value")) //nolint:gosec
	require.NoError(t, onErr)
	assert.Equal(t, "1", string(on), "PTT on should drive the line")

	p.Set(OCTYPE_PTT, 0, 0)

	var off, offErr = os.ReadFile(filepath.Join(dir, "gpio25_ph11", "value")) //nolint:gosec
	require.NoError(t, offErr)
	assert.Equal(t, "0", string(off), "PTT off should release the line")
}

// TestPttInitGPIOInputThenGet is the same regression on the input side, where
// GetInput reads the node that exportGPIO found.
func TestPttInitGPIOInputThenGet(t *testing.T) {
	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio7_pi13")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gpio7_pi13", "value"), []byte("1"), 0o600))

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].ictrl[ICTYPE_TXINH].method = PTT_METHOD_GPIO
	cfg.achan[0].ictrl[ICTYPE_TXINH].in_gpio_num = 7
	var p, err = newPTT(cfg, 0, dir)
	require.NoError(t, err)
	assert.Equal(t, "gpio7_pi13", cfg.achan[0].ictrl[ICTYPE_TXINH].in_gpio_name,
		"the node name found while exporting should be remembered")
	assert.Equal(t, 1, p.GetInput(ICTYPE_TXINH, 0))
}

// TestPttNilBeforeStartup covers a stop signal that arrives while we are still
// starting up: the shutdown path runs cleanup before there is a PTT to put
// down, and the DCD and connected indicators can reach for it just as early.
// Used to panic, when Term dereferenced an audio configuration that audio_open
// had not installed yet.
func TestPttNilBeforeStartup(t *testing.T) {
	var p *PTT

	require.NotPanics(t, p.Term)
	require.NotPanics(t, func() { p.Set(OCTYPE_DCD, 0, 1) })
	assert.Equal(t, -1, p.GetInput(ICTYPE_TXINH, 0))
}

// The debug level decides how much the PTT code says about what it is doing,
// which is the only way to tell a miswired interface from a misconfigured one.
func TestNewPTTDebug(t *testing.T) {
	var p, err = newPTT(new(audio_s), 2, t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, 2, p.debugLevel)
}

// "-doo" prints every channel's PTT configuration at start up, and then each
// change to it.
func TestPttSetupDebugPrintsTheConfiguration(t *testing.T) {
	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO

	var output = testutils.CaptureOutput(t, func() {
		var p, err = newPTT(cfg, 2, t.TempDir())
		require.NoError(t, err)

		p.Set(OCTYPE_PTT, 0, 1)
	})

	assert.Contains(t, output, "ch=0, PTT method=")
	assert.Contains(t, output, "PTT 0 = 1")
}

// An NCHANNEL is somebody else's TNC on the far end of a socket: there is no
// PTT hardware here to key, and no configuration for it to look at either.
func TestPttSetRealNetworkChannel(t *testing.T) {
	var cfg = new(audio_s)
	var p = testPTT(cfg, t.TempDir())

	assert.NotPanics(t, func() { p.Set(OCTYPE_PTT, MAX_RADIO_CHANS, 1) })
}

// Keying a channel that is not a radio is a mistake worth saying out loud.
func TestPttSetRealInvalidChannel(t *testing.T) {
	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_NONE
	var p = testPTT(cfg, t.TempDir())

	var output = testutils.CaptureOutput(t, func() { p.Set(OCTYPE_PTT, 0, 1) })

	assert.Contains(t, output, "did not expect invalid channel")
}

// serialLineChange is one change made to a serial port's control lines.
type serialLineChange struct {
	bit int
	on  bool
}

// name gives the control line a change was made to the name the configuration
// calls it by.
func (c serialLineChange) name() string {
	switch c.bit {
	case unix.TIOCM_RTS:
		return "RTS"
	case unix.TIOCM_DTR:
		return "DTR"
	default:
		return fmt.Sprintf("unknown line 0x%x", c.bit)
	}
}

// captureSerialControlLines collects the changes made to a serial port's
// control lines rather than letting them reach the port, which neither a
// pseudo terminal nor a plain file has any of.
func captureSerialControlLines(t *testing.T) *[]serialLineChange {
	t.Helper()

	var changes = new([]serialLineChange)

	serialControlCapture = func(bit int, on bool) {
		*changes = append(*changes, serialLineChange{bit: bit, on: on})
	}

	t.Cleanup(func() { serialControlCapture = nil })

	return changes
}

// openTestPTTSerialPort sets up a channel whose PTT is driven by a serial
// control line, using a file that can be opened in place of a real port, and
// collects the line changes that would have gone to it.
func openTestPTTSerialPort(t *testing.T, line ptt_line_t, line2 ptt_line_t) (*PTT, *audio_s, *[]serialLineChange) {
	t.Helper()

	var device = filepath.Join(t.TempDir(), "tty")
	require.NoError(t, os.WriteFile(device, nil, 0o600))

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_SERIAL
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_device = device
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_line = line
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_line2 = line2
	var p, err = newPTT(cfg, 0, t.TempDir())
	require.NoError(t, err)

	t.Cleanup(p.Term)

	require.NotNil(t, p.fd[0][OCTYPE_PTT], "the serial port was not opened")

	// After newPTT, which sets the initial state off and would otherwise
	// show up as a change the test did not ask for.
	return p, cfg, captureSerialControlLines(t)
}

// Both control lines a serial port can key with, in both directions, and the
// second line, which most interfaces drive in the opposite phase.
func TestPttSetRealSerialLines(t *testing.T) {
	for _, c := range []struct {
		name  string
		line  ptt_line_t
		line2 ptt_line_t
		keyed []serialLineChange
		unkey []serialLineChange
	}{
		{
			name: "RTS", line: PTT_LINE_RTS, line2: PTT_LINE_NONE,
			keyed: []serialLineChange{{unix.TIOCM_RTS, true}},
			unkey: []serialLineChange{{unix.TIOCM_RTS, false}},
		},
		{
			name: "DTR", line: PTT_LINE_DTR, line2: PTT_LINE_NONE,
			keyed: []serialLineChange{{unix.TIOCM_DTR, true}},
			unkey: []serialLineChange{{unix.TIOCM_DTR, false}},
		},
		{
			name: "RTS and DTR", line: PTT_LINE_RTS, line2: PTT_LINE_DTR,
			keyed: []serialLineChange{{unix.TIOCM_RTS, true}, {unix.TIOCM_DTR, true}},
			unkey: []serialLineChange{{unix.TIOCM_RTS, false}, {unix.TIOCM_DTR, false}},
		},
		{
			name: "DTR and RTS", line: PTT_LINE_DTR, line2: PTT_LINE_RTS,
			keyed: []serialLineChange{{unix.TIOCM_DTR, true}, {unix.TIOCM_RTS, true}},
			unkey: []serialLineChange{{unix.TIOCM_DTR, false}, {unix.TIOCM_RTS, false}},
		},
		{
			name: "neither", line: PTT_LINE_NONE, line2: PTT_LINE_NONE,
			keyed: nil,
			unkey: nil,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var p, _, changes = openTestPTTSerialPort(t, c.line, c.line2)

			p.Set(OCTYPE_PTT, 0, 1)

			assert.Equal(t, c.keyed, *changes, "keying drove the wrong lines")

			*changes = nil

			p.Set(OCTYPE_PTT, 0, 0)

			assert.Equal(t, c.unkey, *changes, "unkeying drove the wrong lines")
		})
	}
}

// Inverting a line is for an interface wired the other way round: the same
// request drives the line the other way, and each line inverts on its own.
func TestPttSetRealSerialInverted(t *testing.T) {
	for _, c := range []struct {
		name            string
		invert, invert2 bool
		keyed           []serialLineChange
	}{
		{
			name: "neither", invert: false, invert2: false,
			keyed: []serialLineChange{{unix.TIOCM_RTS, true}, {unix.TIOCM_DTR, true}},
		},
		{
			name: "the first", invert: true, invert2: false,
			keyed: []serialLineChange{{unix.TIOCM_RTS, false}, {unix.TIOCM_DTR, true}},
		},
		{
			name: "the second", invert: false, invert2: true,
			keyed: []serialLineChange{{unix.TIOCM_RTS, true}, {unix.TIOCM_DTR, false}},
		},
		{
			name: "both", invert: true, invert2: true,
			keyed: []serialLineChange{{unix.TIOCM_RTS, false}, {unix.TIOCM_DTR, false}},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var p, cfg, changes = openTestPTTSerialPort(t, PTT_LINE_RTS, PTT_LINE_DTR)

			cfg.achan[0].octrl[OCTYPE_PTT].ptt_invert = c.invert
			cfg.achan[0].octrl[OCTYPE_PTT].ptt_invert2 = c.invert2

			p.Set(OCTYPE_PTT, 0, 1)

			assert.Equal(t, c.keyed, *changes, "keying drove the wrong levels")
		})
	}
}

// The line names, for a failure message that says RTS rather than 0x4.
func TestSerialLineChangeName(t *testing.T) {
	assert.Equal(t, "RTS", serialLineChange{bit: unix.TIOCM_RTS, on: true}.name())
	assert.Equal(t, "DTR", serialLineChange{bit: unix.TIOCM_DTR, on: true}.name())
	assert.Contains(t, serialLineChange{bit: 0, on: false}.name(), "unknown line")
}

// The port is closed on the way out, so that a restart can open it again.
func TestPttTermClosesTheSerialPort(t *testing.T) {
	var p, _, _ = openTestPTTSerialPort(t, PTT_LINE_RTS, PTT_LINE_NONE)

	p.Term()

	assert.Nil(t, p.fd[0][OCTYPE_PTT], "the serial port was not closed")
}

// A parallel port PTT keys its bit of the port's data register, leaving the
// others alone.  Regression test for the port never being used at all: setup
// kept the handle only when opening it failed, and opened it read-only, so
// the write that keys the bit could not have worked either.
func TestPttLPTKeysItsBit(t *testing.T) {
	var port = filepath.Join(t.TempDir(), "port")

	var contents = make([]byte, LPT_IO_ADDR+1)
	contents[LPT_IO_ADDR] = 0x01 // another channel's bit, already on
	require.NoError(t, os.WriteFile(port, contents, 0o600))

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_LPT
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_lpt_bit = 3

	var p = testPTT(cfg, t.TempDir())
	p.lptPortPath = port

	require.NoError(t, p.init())

	t.Cleanup(p.Term)

	require.NotNil(t, p.fd[0][OCTYPE_PTT], "the port was not kept")
	assert.Equal(t, PTT_METHOD_LPT, cfg.achan[0].octrl[OCTYPE_PTT].ptt_method)

	var dataRegister = func() byte {
		var b, err = os.ReadFile(port) //nolint:gosec
		require.NoError(t, err)

		return b[LPT_IO_ADDR]
	}

	p.Set(OCTYPE_PTT, 0, 1)
	assert.Equal(t, byte(0x09), dataRegister(), "keying should set bit 3 and leave bit 0")

	p.Set(OCTYPE_PTT, 0, 0)
	assert.Equal(t, byte(0x01), dataRegister(), "unkeying should clear bit 3 and leave bit 0")
}

// A parallel port that cannot be opened is not fatal: the channel falls back
// to no PTT method, as a serial port does, rather than keeping a nil handle.
func TestPttLPTOpenFailure(t *testing.T) {
	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_LPT

	var p = testPTT(cfg, t.TempDir())
	p.lptPortPath = filepath.Join(t.TempDir(), "no-such-port")

	var output = testutils.CaptureOutput(t, func() { require.NoError(t, p.init()) })

	assert.Contains(t, output, "Can't open")
	assert.Equal(t, PTT_METHOD_NONE, cfg.achan[0].octrl[OCTYPE_PTT].ptt_method)
	assert.Nil(t, p.fd[0][OCTYPE_PTT])
}

// Two channels keying different lines of the same serial port share the one
// open device: it cannot be opened twice.
func TestPttSetupSharesOneSerialPortBetweenChannels(t *testing.T) {
	var device = filepath.Join(t.TempDir(), "tty")
	require.NoError(t, os.WriteFile(device, nil, 0o600))

	var cfg = new(audio_s)

	for _, ch := range []int{0, 1} {
		cfg.chan_medium[ch] = MEDIUM_RADIO
		cfg.achan[ch].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_SERIAL
		cfg.achan[ch].octrl[OCTYPE_PTT].ptt_device = device
	}

	cfg.achan[0].octrl[OCTYPE_PTT].ptt_line = PTT_LINE_RTS
	cfg.achan[1].octrl[OCTYPE_PTT].ptt_line = PTT_LINE_DTR

	var p, err = newPTT(cfg, 0, t.TempDir())
	require.NoError(t, err)

	t.Cleanup(p.Term)

	assert.Same(t, p.fd[0][OCTYPE_PTT], p.fd[1][OCTYPE_PTT],
		"the same device should have been opened once and shared")
}

// A Windows-style port name is translated, because the configuration file is
// the same on both and people copy each other's.
func TestPttSetupTranslatesCOMPortNames(t *testing.T) {
	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_SERIAL
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_device = "COM3"
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_line = PTT_LINE_RTS

	var p *PTT

	var output = testutils.CaptureOutput(t, func() {
		var err error

		p, err = newPTT(cfg, 0, t.TempDir())
		require.NoError(t, err)
	})

	t.Cleanup(p.Term)

	assert.Contains(t, output, "to Linux equivalent '/dev/ttyS2'")
	assert.Equal(t, "/dev/ttyS2", cfg.achan[0].octrl[OCTYPE_PTT].ptt_device)
}

// COM0 is not a thing; it is treated as COM1 rather than as /dev/ttyS-1.
func TestPttSetupTranslatesCOM0(t *testing.T) {
	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_SERIAL
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_device = "com0"
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_line = PTT_LINE_RTS

	var p *PTT

	testutils.CaptureOutput(t, func() {
		var err error

		p, err = newPTT(cfg, 0, t.TempDir())
		require.NoError(t, err)
	})

	t.Cleanup(p.Term)

	assert.Equal(t, "/dev/ttyS0", cfg.achan[0].octrl[OCTYPE_PTT].ptt_device)
}

// Reading an input line from a channel that is not a radio is the same kind
// of mistake as keying one.
func TestGetInputRealInvalidChannel(t *testing.T) {
	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_NONE
	var p = testPTT(cfg, t.TempDir())

	var result int

	var output = testutils.CaptureOutput(t, func() { result = p.GetInput(ICTYPE_TXINH, 0) })

	assert.Equal(t, -1, result)
	assert.Contains(t, output, "did not expect invalid channel")
}

// A CM108 audio adapter's HID node is owned by root, and the advice about
// what to do is worth more than the error on its own.  Anything else is not a
// permission problem, and gets no advice.
func TestCM108PermissionAdvice(t *testing.T) {
	var output = testutils.CaptureOutput(t, func() {
		cm108_print_permission_advice("/dev/hidraw0", fs.ErrPermission)
	})

	assert.NotEmpty(t, output)
	assert.Contains(t, output, "/dev/hidraw0")

	output = testutils.CaptureOutput(t, func() {
		cm108_print_permission_advice("/dev/hidraw0", fs.ErrNotExist)
	})

	assert.Empty(t, output, "only a permission problem gets the advice")
}
