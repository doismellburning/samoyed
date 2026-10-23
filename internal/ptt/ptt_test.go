// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package ptt

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/cm108"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
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
func testPTT(cfg *[phy.MaxRadioChans]ChannelConfig, gpioSysfsDir string) *PTT {
	var p = new(PTT)
	p.channels = *cfg
	p.gpioSysfsDir = gpioSysfsDir
	p.serialControl = setSerialControlLine

	return p
}

// setupGPIODChannel makes a PTT whose channel 0 OutputPTT is a GPIOD line,
// returning it and the mock standing in for the line so the caller can
// inspect it.
func setupGPIODChannel(t *testing.T, invert bool) (*PTT, *mockGPIODLine) {
	t.Helper()

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].Method = MethodGPIOD
	cfg[0].Outputs[OutputPTT].GPIONum = 0
	cfg[0].Outputs[OutputPTT].Invert = invert

	var p = testPTT(cfg, t.TempDir())

	var mock = new(mockGPIODLine)
	p.gpiodLine[0][OutputPTT] = mock

	return p, mock
}

// TestPttSetRealGPIOD_Activate verifies that PTT-active drives the line high.
func TestPttSetRealGPIOD_Activate(t *testing.T) {
	t.Parallel()

	var p, mock = setupGPIODChannel(t, false)

	p.Set(OutputPTT, 0, 1)

	assert.Equal(t, 1, mock.value, "line should be high when PTT is active")
}

// TestPttSetRealGPIOD_Deactivate verifies that PTT-inactive drives the line low.
func TestPttSetRealGPIOD_Deactivate(t *testing.T) {
	t.Parallel()

	var p, mock = setupGPIODChannel(t, false)

	p.Set(OutputPTT, 0, 0)

	assert.Equal(t, 0, mock.value, "line should be low when PTT is inactive")
}

// TestPttSetRealGPIOD_Invert_Activate verifies that Invert flips the level
// when PTT is active (signal=1 → line low).
func TestPttSetRealGPIOD_Invert_Activate(t *testing.T) {
	t.Parallel()

	var p, mock = setupGPIODChannel(t, true)

	p.Set(OutputPTT, 0, 1)

	assert.Equal(t, 0, mock.value, "inverted line should be low when PTT is active")
}

// TestPttSetRealGPIOD_Invert_Deactivate verifies that Invert flips the level
// when PTT is inactive (signal=0 → line high).
func TestPttSetRealGPIOD_Invert_Deactivate(t *testing.T) {
	t.Parallel()

	var p, mock = setupGPIODChannel(t, true)

	p.Set(OutputPTT, 0, 0)

	assert.Equal(t, 1, mock.value, "inverted line should be high when PTT is inactive")
}

// TestPttSetRealGPIOD_NilLine verifies that Set does not panic when
// the GPIOD line handle has not been initialised.
func TestPttSetRealGPIOD_NilLine(t *testing.T) {
	t.Parallel()

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].Method = MethodGPIOD

	var p = testPTT(cfg, t.TempDir())

	require.NotPanics(t, func() {
		p.Set(OutputPTT, 0, 1)
	})
}

// TestPttTermGPIOD verifies that Term closes every open line handle and
// sets the slot to nil.
func TestPttTermGPIOD(t *testing.T) {
	t.Parallel()

	var p, mock = setupGPIODChannel(t, false)

	p.Term()

	assert.True(t, mock.closed, "Term should close the line handle")
	assert.Nil(t, p.gpiodLine[0][OutputPTT], "Term should nil the line handle")
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
	t.Parallel()

	var err = testPTT(new([phy.MaxRadioChans]ChannelConfig), t.TempDir()).getAccessToGPIO(filepath.Join(t.TempDir(), "export"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "GPIO user interface")
}

// TestGetAccessToGPIOPresent verifies that a node we can stat is accepted.
func TestGetAccessToGPIOPresent(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)

	require.NoError(t, testPTT(new([phy.MaxRadioChans]ChannelConfig), dir).getAccessToGPIO(filepath.Join(dir, "export")))
}

// TestExportGPIOOutput verifies that exporting an output line writes the line
// number to "export" and leaves the line low, i.e. off.
func TestExportGPIOOutput(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio25")

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].GPIONum = 25
	var p = testPTT(cfg, dir)

	require.NoError(t, p.exportGPIO(0, int(OutputPTT), false, 1))

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
	t.Parallel()

	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio25")

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].GPIONum = 25
	var p = testPTT(cfg, dir)

	require.NoError(t, p.exportGPIO(0, int(OutputPTT), true, 1))

	var direction, dirErr = os.ReadFile(filepath.Join(dir, "gpio25", "direction")) //nolint:gosec
	require.NoError(t, dirErr)
	assert.Equal(t, "high", string(direction), "an inverted output line should start off, i.e. high")
}

// TestExportGPIOInput verifies that an input line is set to "in" and takes its
// number from the input rather than the output configuration.
func TestExportGPIOInput(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio7")

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Inputs[InputTxInhibit].GPIONum = 7
	var p = testPTT(cfg, dir)

	require.NoError(t, p.exportGPIO(0, InputTxInhibit, false, 0))

	var direction, dirErr = os.ReadFile(filepath.Join(dir, "gpio7", "direction")) //nolint:gosec
	require.NoError(t, dirErr)
	assert.Equal(t, "in", string(direction))
}

// TestExportGPIOSuffixedNode verifies the CubieBoard-style node names, where
// GPIO 25 appears as something like gpio25_ph11.
func TestExportGPIOSuffixedNode(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio25_ph11")

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].GPIONum = 25
	var p = testPTT(cfg, dir)

	require.NoError(t, p.exportGPIO(0, int(OutputPTT), false, 1))

	var direction, dirErr = os.ReadFile(filepath.Join(dir, "gpio25_ph11", "direction")) //nolint:gosec
	require.NoError(t, dirErr)
	assert.Equal(t, "low", string(direction))
}

// TestExportGPIONoSuchNode verifies that a line that never appears under
// /sys/class/gpio is reported rather than ending the process.
func TestExportGPIONoSuchNode(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].GPIONum = 25
	var p = testPTT(cfg, dir)

	var err = p.exportGPIO(0, int(OutputPTT), false, 1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gpio number 25")
}

// TestExportGPIONoSysfs verifies that a system without the sysfs GPIO
// interface is reported rather than ending the process.
func TestExportGPIONoSysfs(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].GPIONum = 25
	var p = testPTT(cfg, dir)

	var err = p.exportGPIO(0, int(OutputPTT), false, 1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "GPIO user interface")
}

// TestPttInitGPIONoSysfs verifies that New reports a GPIO channel it
// cannot set up, rather than ending the process, which is what made the
// function untestable.
func TestPttInitGPIONoSysfs(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].Method = MethodGPIO
	cfg[0].Outputs[OutputPTT].GPIONum = 25
	var _, err = newPTT(*cfg, nil, nil, 0, dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "GPIO user interface")
}

// TestPttInitGPIO verifies that a GPIO channel that can be set up leaves the
// line off and reports no error.
func TestPttInitGPIO(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio25")

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].Method = MethodGPIO
	cfg[0].Outputs[OutputPTT].GPIONum = 25
	var _, err = newPTT(*cfg, nil, nil, 0, dir)
	require.NoError(t, err)

	var direction, dirErr = os.ReadFile(filepath.Join(dir, "gpio25", "direction")) //nolint:gosec
	require.NoError(t, dirErr)
	assert.Equal(t, "low", string(direction))
}

// TestPttInitGPIODRequestFailure verifies that a GPIOD line that cannot be
// requested - here because the chip does not exist - is reported to the
// caller.
func TestPttInitGPIODRequestFailure(t *testing.T) {
	t.Parallel()

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].Method = MethodGPIOD
	cfg[0].Outputs[OutputPTT].GPIOName = "/dev/samoyed-no-such-gpiochip"
	cfg[0].Outputs[OutputPTT].GPIONum = 17

	var p = testPTT(cfg, t.TempDir())

	var err = p.init()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "/dev/samoyed-no-such-gpiochip")
	assert.Nil(t, p.gpiodLine[0][OutputPTT], "a line that could not be requested should not be recorded")
}

// TestPttInitRollsBackOnFailure verifies that a failure part way through
// setting up gives back what has been set up already: setting up is
// incremental, and the descriptors and line handles live in a PTT the caller
// never gets.
func TestPttInitRollsBackOnFailure(t *testing.T) {
	t.Parallel()

	var openable = filepath.Join(t.TempDir(), "tty")
	require.NoError(t, os.WriteFile(openable, nil, 0o600))

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	// A channel whose PTT device opens...
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].Method = MethodSerial
	cfg[0].Outputs[OutputPTT].Device = openable
	cfg[0].Outputs[OutputPTT].Line = LineRTS
	// ...followed by one that fails, after the first has been opened.
	cfg[1].Radio = true
	cfg[1].Outputs[OutputPTT].Method = MethodGPIOD
	cfg[1].Outputs[OutputPTT].GPIOName = "/dev/samoyed-no-such-gpiochip"

	var p = testPTT(cfg, t.TempDir())

	require.Error(t, p.init())
	assert.Nil(t, p.fd[0][OutputPTT], "a serial port opened before the failure should be closed again")
}

// TestPttInitSerialOpenFailure verifies that a serial port we cannot open is
// not fatal: the channel falls back to no PTT method, as it always has.
func TestPttInitSerialOpenFailure(t *testing.T) {
	t.Parallel()

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].Method = MethodSerial
	cfg[0].Outputs[OutputPTT].Device = filepath.Join(t.TempDir(), "no-such-tty")
	cfg[0].Outputs[OutputPTT].Line = LineRTS
	var p, err = newPTT(*cfg, nil, nil, 0, t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, MethodNone, p.channels[0].Outputs[OutputPTT].Method)
}

// TestGetInputRealGPIO verifies that an input line's value is read from sysfs,
// and that invert flips it.
func TestGetInputRealGPIO(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio7")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gpio7", "value"), []byte("1"), 0o600))

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Inputs[InputTxInhibit].Method = MethodGPIO
	cfg[0].Inputs[InputTxInhibit].GPIOName = "gpio7"
	var p = testPTT(cfg, dir)

	assert.Equal(t, 1, p.GetInput(InputTxInhibit, 0))

	p.channels[0].Inputs[InputTxInhibit].Invert = true

	assert.Equal(t, 0, p.GetInput(InputTxInhibit, 0), "invert should flip the value read")
}

// TestGetInputRealGPIONoNode verifies that an input line whose sysfs node is
// missing reports an error rather than ending the process.
func TestGetInputRealGPIONoNode(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Inputs[InputTxInhibit].Method = MethodGPIO
	cfg[0].Inputs[InputTxInhibit].GPIOName = "gpio7"
	var p = testPTT(cfg, dir)

	assert.Equal(t, -1, p.GetInput(InputTxInhibit, 0))
}

// TestPttInitGPIOThenSet is a regression test for a GPIO line that never
// moved: exportGPIO worked out which node under /sys/class/gpio belongs to
// the configured line number, but kept that to itself, so Set built its
// path from an empty name and could not open anything.
func TestPttInitGPIOThenSet(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio25_ph11")

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].Method = MethodGPIO
	cfg[0].Outputs[OutputPTT].GPIONum = 25
	var p, err = newPTT(*cfg, nil, nil, 0, dir)
	require.NoError(t, err)
	assert.Equal(t, "gpio25_ph11", p.channels[0].Outputs[OutputPTT].GPIOName,
		"the node name found while exporting should be remembered")

	p.Set(OutputPTT, 0, 1)

	var on, onErr = os.ReadFile(filepath.Join(dir, "gpio25_ph11", "value")) //nolint:gosec
	require.NoError(t, onErr)
	assert.Equal(t, "1", string(on), "PTT on should drive the line")

	p.Set(OutputPTT, 0, 0)

	var off, offErr = os.ReadFile(filepath.Join(dir, "gpio25_ph11", "value")) //nolint:gosec
	require.NoError(t, offErr)
	assert.Equal(t, "0", string(off), "PTT off should release the line")
}

// TestPttInitGPIOInputThenGet is the same regression on the input side, where
// GetInput reads the node that exportGPIO found.
func TestPttInitGPIOInputThenGet(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio7_pi13")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gpio7_pi13", "value"), []byte("1"), 0o600))

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Inputs[InputTxInhibit].Method = MethodGPIO
	cfg[0].Inputs[InputTxInhibit].GPIONum = 7
	var p, err = newPTT(*cfg, nil, nil, 0, dir)
	require.NoError(t, err)
	assert.Equal(t, "gpio7_pi13", p.channels[0].Inputs[InputTxInhibit].GPIOName,
		"the node name found while exporting should be remembered")
	assert.Equal(t, 1, p.GetInput(InputTxInhibit, 0))
}

// TestPttNilBeforeStartup covers a stop signal that arrives while we are still
// starting up: the shutdown path runs cleanup before there is a PTT to put
// down, and the DCD and connected indicators can reach for it just as early.
// Used to panic, when Term dereferenced an audio configuration that AudioOpen
// had not installed yet.
func TestPttNilBeforeStartup(t *testing.T) {
	t.Parallel()

	var p *PTT

	require.NotPanics(t, p.Term)
	require.NotPanics(t, func() { p.Set(OutputDCD, 0, 1) })
	assert.Equal(t, -1, p.GetInput(InputTxInhibit, 0))
}

// The debug level decides how much the PTT code says about what it is doing,
// which is the only way to tell a miswired interface from a misconfigured one.
func TestNewPTTDebug(t *testing.T) {
	t.Parallel()

	var p, err = newPTT([phy.MaxRadioChans]ChannelConfig{}, nil, nil, 2, t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, 2, p.debugLevel)
}

// "-doo" prints every channel's PTT configuration at start up, and then each
// change to it.
//
//nolint:paralleltest // Serial: changes logrus's global level and records it through a hook.
func TestPttSetupDebugPrintsTheConfiguration(t *testing.T) {
	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true

	var hook = capturePTTLog(t)

	var p, err = newPTT(*cfg, nil, nil, 2, t.TempDir())
	require.NoError(t, err)

	p.Set(OutputPTT, 0, 1)

	var configuration = pttLogEntry(t, hook, "Output control configuration")
	assert.Equal(t, 0, configuration.Data["channel"])
	assert.Equal(t, "PTT", configuration.Data["output"])

	var set = pttLogEntry(t, hook, "Set output")
	assert.Equal(t, 0, set.Data["channel"])
	assert.Equal(t, 1, set.Data["state"])
}

// An NCHANNEL is somebody else's TNC on the far end of a socket: there is no
// PTT hardware here to key, and no configuration for it to look at either.
func TestPttSetRealNetworkChannel(t *testing.T) {
	t.Parallel()

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	var p = testPTT(cfg, t.TempDir())

	assert.NotPanics(t, func() { p.Set(OutputPTT, phy.MaxRadioChans, 1) })
}

// Keying a channel that is not a radio is a mistake worth saying out loud.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestPttSetRealInvalidChannel(t *testing.T) {
	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = false
	var p = testPTT(cfg, t.TempDir())

	var hook = capturePTTLog(t)

	p.Set(OutputPTT, 0, 1)

	assert.Equal(t, logrus.ErrorLevel, pttLogEntry(t, hook, "Internal error, PTT.Set did not expect a channel that isn't a radio").Level)
}

// capturePTTLog records what PTT logs, at every level up to Debug, until t
// finishes.
func capturePTTLog(t *testing.T) *test.Hook {
	t.Helper()

	var savedLevel = logrus.GetLevel()
	logrus.SetLevel(logrus.DebugLevel)
	t.Cleanup(func() { logrus.SetLevel(savedLevel) })

	var savedHooks = logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))
	t.Cleanup(func() { logrus.StandardLogger().ReplaceHooks(savedHooks) })

	return test.NewGlobal()
}

// pttLogEntry is the first entry hook recorded with message, failing t if
// there isn't one.
func pttLogEntry(t *testing.T, hook *test.Hook, message string) *logrus.Entry {
	t.Helper()

	for _, entry := range hook.AllEntries() {
		if entry.Message == message {
			return entry
		}
	}

	require.Failf(t, "no log entry", "nothing logged with message %q", message)

	return nil
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
func captureSerialControlLines(t *testing.T, p *PTT) *[]serialLineChange {
	t.Helper()

	var changes = new([]serialLineChange)

	p.serialControl = func(_ int, bit int, on bool) error {
		*changes = append(*changes, serialLineChange{bit: bit, on: on})

		return nil
	}

	return changes
}

// openTestPTTSerialPort sets up a channel whose PTT is driven by a serial
// control line, using a file that can be opened in place of a real port, and
// collects the line changes that would have gone to it.
func openTestPTTSerialPort(t *testing.T, line Line, line2 Line) (*PTT, *[]serialLineChange) {
	t.Helper()

	var device = filepath.Join(t.TempDir(), "tty")
	require.NoError(t, os.WriteFile(device, nil, 0o600))

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].Method = MethodSerial
	cfg[0].Outputs[OutputPTT].Device = device
	cfg[0].Outputs[OutputPTT].Line = line
	cfg[0].Outputs[OutputPTT].Line2 = line2
	var p, err = newPTT(*cfg, nil, nil, 0, t.TempDir())
	require.NoError(t, err)

	t.Cleanup(p.Term)

	require.NotNil(t, p.fd[0][OutputPTT], "the serial port was not opened")

	// After newPTT, which sets the initial state off and would otherwise
	// show up as a change the test did not ask for.
	return p, captureSerialControlLines(t, p)
}

// Both control lines a serial port can key with, in both directions, and the
// second line, which most interfaces drive in the opposite phase.
func TestPttSetRealSerialLines(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name  string
		line  Line
		line2 Line
		keyed []serialLineChange
		unkey []serialLineChange
	}{
		{
			name: "RTS", line: LineRTS, line2: LineNone,
			keyed: []serialLineChange{{unix.TIOCM_RTS, true}},
			unkey: []serialLineChange{{unix.TIOCM_RTS, false}},
		},
		{
			name: "DTR", line: LineDTR, line2: LineNone,
			keyed: []serialLineChange{{unix.TIOCM_DTR, true}},
			unkey: []serialLineChange{{unix.TIOCM_DTR, false}},
		},
		{
			name: "RTS and DTR", line: LineRTS, line2: LineDTR,
			keyed: []serialLineChange{{unix.TIOCM_RTS, true}, {unix.TIOCM_DTR, true}},
			unkey: []serialLineChange{{unix.TIOCM_RTS, false}, {unix.TIOCM_DTR, false}},
		},
		{
			name: "DTR and RTS", line: LineDTR, line2: LineRTS,
			keyed: []serialLineChange{{unix.TIOCM_DTR, true}, {unix.TIOCM_RTS, true}},
			unkey: []serialLineChange{{unix.TIOCM_DTR, false}, {unix.TIOCM_RTS, false}},
		},
		{
			name: "neither", line: LineNone, line2: LineNone,
			keyed: nil,
			unkey: nil,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			var p, changes = openTestPTTSerialPort(t, c.line, c.line2)

			p.Set(OutputPTT, 0, 1)

			assert.Equal(t, c.keyed, *changes, "keying drove the wrong lines")

			*changes = nil

			p.Set(OutputPTT, 0, 0)

			assert.Equal(t, c.unkey, *changes, "unkeying drove the wrong lines")
		})
	}
}

// Inverting a line is for an interface wired the other way round: the same
// request drives the line the other way, and each line inverts on its own.
func TestPttSetRealSerialInverted(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			var p, changes = openTestPTTSerialPort(t, LineRTS, LineDTR)

			p.channels[0].Outputs[OutputPTT].Invert = c.invert
			p.channels[0].Outputs[OutputPTT].Invert2 = c.invert2

			p.Set(OutputPTT, 0, 1)

			assert.Equal(t, c.keyed, *changes, "keying drove the wrong levels")
		})
	}
}

// The line names, for a failure message that says RTS rather than 0x4.
func TestSerialLineChangeName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "RTS", serialLineChange{bit: unix.TIOCM_RTS, on: true}.name())
	assert.Equal(t, "DTR", serialLineChange{bit: unix.TIOCM_DTR, on: true}.name())
	assert.Contains(t, serialLineChange{bit: 0, on: false}.name(), "unknown line")
}

// The port is closed on the way out, so that a restart can open it again.
func TestPttTermClosesTheSerialPort(t *testing.T) {
	t.Parallel()

	var p, _ = openTestPTTSerialPort(t, LineRTS, LineNone)

	p.Term()

	assert.Nil(t, p.fd[0][OutputPTT], "the serial port was not closed")
}

// Term can run while another goroutine is still keying: the cleanup after a
// stop signal puts the hardware down while the transmit and receive threads
// are winding up.  Regression test for Term closing and clearing the handles
// with nothing to stop Set using them at the same time: a Set is held part
// way through driving the port, and Term has to wait for it.
func TestPttTermWaitsForSet(t *testing.T) {
	t.Parallel()

	var device = filepath.Join(t.TempDir(), "tty")
	require.NoError(t, os.WriteFile(device, nil, 0o600))

	// The connected indicator, rather than PTT or DCD, because keying it
	// neither mutes the demodulator nor tells the data link state machine.
	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputCon].Method = MethodSerial
	cfg[0].Outputs[OutputCon].Device = device
	cfg[0].Outputs[OutputCon].Line = LineRTS

	var p, err = newPTT(*cfg, nil, nil, 0, t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, p.fd[0][OutputCon], "the serial port was not opened")

	// The first change to the control lines is held until the test lets it
	// go.  Any after it, such as Term's own unkeying, pass straight through.
	var inSet = make(chan struct{})
	var release = make(chan struct{})
	var held atomic.Bool

	p.serialControl = func(int, int, bool) error {
		if held.CompareAndSwap(false, true) {
			close(inSet)
			<-release
		}

		return nil
	}

	var setDone = make(chan struct{})

	go func() {
		defer close(setDone)

		p.Set(OutputCon, 0, 1)
	}()

	<-inSet

	var termDone = make(chan struct{})

	go func() {
		defer close(termDone)

		p.Term()
	}()

	select {
	case <-termDone:
		close(release)
		t.Fatal("Term closed the port while a Set was still driving it")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	<-setDone
	<-termDone

	assert.Nil(t, p.fd[0][OutputCon], "the serial port was not closed")
}

// A parallel port PTT keys its bit of the port's data register, leaving the
// others alone.  Regression test for the port never being used at all: setup
// kept the handle only when opening it failed, and opened it read-only, so
// the write that keys the bit could not have worked either.
func TestPttLPTKeysItsBit(t *testing.T) {
	t.Parallel()

	var port = filepath.Join(t.TempDir(), "port")

	var contents = make([]byte, LPT_IO_ADDR+1)
	contents[LPT_IO_ADDR] = 0x01 // another channel's bit, already on
	require.NoError(t, os.WriteFile(port, contents, 0o600))

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].Method = MethodLPT
	cfg[0].Outputs[OutputPTT].LPTBit = 3

	var p = testPTT(cfg, t.TempDir())
	p.lptPortPath = port

	require.NoError(t, p.init())

	t.Cleanup(p.Term)

	require.NotNil(t, p.fd[0][OutputPTT], "the port was not kept")
	assert.Equal(t, MethodLPT, p.channels[0].Outputs[OutputPTT].Method)

	var dataRegister = func() byte {
		var b, err = os.ReadFile(port) //nolint:gosec
		require.NoError(t, err)

		return b[LPT_IO_ADDR]
	}

	p.Set(OutputPTT, 0, 1)
	assert.Equal(t, byte(0x09), dataRegister(), "keying should set bit 3 and leave bit 0")

	p.Set(OutputPTT, 0, 0)
	assert.Equal(t, byte(0x01), dataRegister(), "unkeying should clear bit 3 and leave bit 0")
}

// A parallel port that cannot be opened is not fatal: the channel falls back
// to no PTT method, as a serial port does, rather than keeping a nil handle.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestPttLPTOpenFailure(t *testing.T) {
	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].Method = MethodLPT

	var p = testPTT(cfg, t.TempDir())
	p.lptPortPath = filepath.Join(t.TempDir(), "no-such-port")

	var hook = capturePTTLog(t)

	require.NoError(t, p.init())

	assert.Equal(t, p.lptPortPath, pttLogEntry(t, hook, "Can't open parallel printer port for PTT control").Data["device"])
	assert.Equal(t, MethodNone, p.channels[0].Outputs[OutputPTT].Method)
	assert.Nil(t, p.fd[0][OutputPTT])
}

// Two channels keying different lines of the same serial port share the one
// open device: it cannot be opened twice.
func TestPttSetupSharesOneSerialPortBetweenChannels(t *testing.T) {
	t.Parallel()

	var device = filepath.Join(t.TempDir(), "tty")
	require.NoError(t, os.WriteFile(device, nil, 0o600))

	var cfg = new([phy.MaxRadioChans]ChannelConfig)

	for _, ch := range []int{0, 1} {
		cfg[ch].Radio = true
		cfg[ch].Outputs[OutputPTT].Method = MethodSerial
		cfg[ch].Outputs[OutputPTT].Device = device
	}

	cfg[0].Outputs[OutputPTT].Line = LineRTS
	cfg[1].Outputs[OutputPTT].Line = LineDTR

	var p, err = newPTT(*cfg, nil, nil, 0, t.TempDir())
	require.NoError(t, err)

	t.Cleanup(p.Term)

	assert.Same(t, p.fd[0][OutputPTT], p.fd[1][OutputPTT],
		"the same device should have been opened once and shared")
}

// A Windows-style port name is translated, because the configuration file is
// the same on both and people copy each other's.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestPttSetupTranslatesCOMPortNames(t *testing.T) {
	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].Method = MethodSerial
	cfg[0].Outputs[OutputPTT].Device = "COM3"
	cfg[0].Outputs[OutputPTT].Line = LineRTS

	var hook = capturePTTLog(t)

	var p, err = newPTT(*cfg, nil, nil, 0, t.TempDir())
	require.NoError(t, err)

	t.Cleanup(p.Term)

	var converted = pttLogEntry(t, hook, "Converted device to Linux equivalent")
	assert.Equal(t, "COM3", converted.Data["from"])
	assert.Equal(t, "/dev/ttyS2", converted.Data["to"])
	assert.Equal(t, "/dev/ttyS2", p.channels[0].Outputs[OutputPTT].Device)
}

// COM0 is not a thing; it is treated as COM1 rather than as /dev/ttyS-1.
//
//nolint:paralleltest // Serial: sends logrus's global output to the bin.
func TestPttSetupTranslatesCOM0(t *testing.T) {
	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].Outputs[OutputPTT].Method = MethodSerial
	cfg[0].Outputs[OutputPTT].Device = "com0"
	cfg[0].Outputs[OutputPTT].Line = LineRTS

	testutils.DiscardLogrus(t)

	var p, err = newPTT(*cfg, nil, nil, 0, t.TempDir())
	require.NoError(t, err)

	t.Cleanup(p.Term)

	assert.Equal(t, "/dev/ttyS0", p.channels[0].Outputs[OutputPTT].Device)
}

// Reading an input line from a channel that is not a radio is the same kind
// of mistake as keying one.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestGetInputRealInvalidChannel(t *testing.T) {
	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = false
	var p = testPTT(cfg, t.TempDir())

	var hook = capturePTTLog(t)

	assert.Equal(t, -1, p.GetInput(InputTxInhibit, 0))
	pttLogEntry(t, hook, "Internal error, PTT.GetInput did not expect a channel that isn't a radio")
}

// A CM108 audio adapter's HID node is owned by root, and the advice about
// what to do is worth more than the error on its own.  Anything else is not a
// permission problem, and gets no advice.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestCM108PermissionAdvice(t *testing.T) {
	var hook = capturePTTLog(t)

	cm108_print_permission_advice("/dev/hidraw0", fs.ErrPermission)

	var advice = hook.AllEntries()
	require.Len(t, advice, len(cm108.PermissionAdvice("/dev/hidraw0")), "one entry per line of advice")

	for _, entry := range advice {
		assert.Equal(t, "/dev/hidraw0", entry.Data["device"])
	}

	hook.Reset()

	cm108_print_permission_advice("/dev/hidraw0", fs.ErrNotExist)

	assert.Empty(t, hook.AllEntries(), "only a permission problem gets the advice")
}

// A half duplex channel stops listening while it transmits, so it does not
// hear itself, and starts again when it stops; a full duplex one goes on
// listening.  The PTT tells whatever it was handed to mute the input.
func TestPTTMutesHalfDuplexInputWhileTransmitting(t *testing.T) {
	t.Parallel()

	var audioConfig = new([phy.MaxRadioChans]ChannelConfig)
	audioConfig[0].Radio = true
	audioConfig[1].Radio = true
	audioConfig[1].FullDuplex = true

	var muted []string

	var p, err = New(*audioConfig, func(channel int, mute bool) {
		muted = append(muted, fmt.Sprintf("%d=%t", channel, mute))
	}, nil, 0)
	require.NoError(t, err)

	p.Set(OutputPTT, 0, 1)
	p.Set(OutputPTT, 0, 0)
	p.Set(OutputPTT, 1, 1)
	p.Set(OutputDCD, 0, 1)

	assert.Equal(t, []string{"0=true", "0=false"}, muted)
}

// Each change to a channel's PTT or DCD is passed on to whatever was handed
// the PTT for it - the data link state machine, which waits for the channel
// to be clear.
func TestPTTTellsWhoWasHandedOfChannelActivity(t *testing.T) {
	t.Parallel()

	var audioConfig = new([phy.MaxRadioChans]ChannelConfig)
	audioConfig[0].Radio = true
	audioConfig[0].FullDuplex = true

	var busy []string

	var p, err = New(*audioConfig, nil, func(channel int, activity OutputType, isBusy bool) {
		busy = append(busy, fmt.Sprintf("%d %s=%t", channel, octypeName(activity), isBusy))
	}, 0)
	require.NoError(t, err)

	p.Set(OutputPTT, 0, 1)
	p.Set(OutputDCD, 0, 1)
	p.Set(OutputPTT, 0, 0)

	assert.Equal(t, []string{"0 PTT=true", "0 DCD=true", "0 PTT=false"}, busy)
}

// newRecordingPTT is a PTT for radio channel 0 with nothing to drive, which
// records each output it is told to set, as the data link queue would hear
// of it.
func newRecordingPTT(t *testing.T) (*PTT, *[]string) {
	t.Helper()

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[0].FullDuplex = true

	var set []string

	var p, err = newPTT(*cfg, nil, func(channel int, ot OutputType, busy bool) {
		set = append(set, fmt.Sprintf("%s %d=%t", octypeName(ot), channel, busy))
	}, 0, t.TempDir())
	require.NoError(t, err)

	return p, &set
}

func TestPTTSetDCD(t *testing.T) {
	t.Parallel()

	var p, set = newRecordingPTT(t)

	p.SetDCD(0, true)
	p.SetDCD(0, false)

	assert.Equal(t, []string{"DCD 0=true", "DCD 0=false"}, *set)
}

func TestPTTSetPTT(t *testing.T) {
	t.Parallel()

	var p, set = newRecordingPTT(t)

	p.SetPTT(0, true)
	p.SetPTT(0, false)

	assert.Equal(t, []string{"PTT 0=true", "PTT 0=false"}, *set)
}

func TestPTTSetConnected(t *testing.T) {
	t.Parallel()

	var p, set = newRecordingPTT(t)

	p.SetConnected(0, true)
	p.SetConnected(0, false)

	assert.Equal(t, []string{"CON 0=true", "CON 0=false"}, *set)
}

// A channel with no transmit inhibit input configured, or no PTT at all, is
// never inhibited.
func TestPTTTransmitInhibitedWithoutAnInput(t *testing.T) {
	t.Parallel()

	var p, _ = newRecordingPTT(t)

	assert.False(t, p.TransmitInhibited(0))

	var none *PTT
	assert.False(t, none.TransmitInhibited(0))
}

func TestOutputTypesYieldsEachOutputInOrder(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []OutputType{OutputPTT, OutputDCD, OutputCon}, slices.Collect(OutputTypes()))

	for ot := range OutputTypes() {
		assert.Equal(t, OutputPTT, ot, "stopping early should stop at the first")

		break
	}
}

// A radio channel with no PTT is told about VOX, as it may be relying on it -
// but not when it has no audio output, as then it cannot transmit at all.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestPttInitVOXAdviceOnlyWhenTransmitting(t *testing.T) {
	const message = "PTT not configured (OK if using VOX)"

	var cfg = new([phy.MaxRadioChans]ChannelConfig)
	cfg[0].Radio = true
	cfg[1].Radio = true
	cfg[1].ReceiveOnly = true

	var hook = capturePTTLog(t)

	var _, err = newPTT(*cfg, nil, nil, 0, t.TempDir())
	require.NoError(t, err)

	var channels []any

	for _, entry := range hook.AllEntries() {
		if entry.Message == message {
			channels = append(channels, entry.Data["channel"])
		}
	}

	assert.Equal(t, []any{0}, channels, "only the channel that can transmit")
}

// serialPortEnv names a real serial port for TestSetSerialControlLineRealPort
// to drive.  Neither a pseudo terminal nor a plain file has modem control
// lines, so nothing else can show the ioctls working; and a test that keyed
// whatever happened to be on /dev/ttyS0 could key a transmitter, so the port
// has to be asked for.
const serialPortEnv = "SAMOYED_TEST_SERIAL_PORT"

// Opening a serial port raises RTS and DTR, so a PTT on either is keyed until
// something lowers it again.  Both lines go each way, and the port reads back
// what was asked for.
func TestSetSerialControlLineRealPort(t *testing.T) {
	t.Parallel()

	var device = os.Getenv(serialPortEnv)
	if device == "" {
		t.Skipf("set %s to a serial port whose control lines are safe to drive", serialPortEnv)
	}

	var port, err = os.OpenFile(device, os.O_RDONLY|unix.O_NONBLOCK, 0) //nolint:gosec // G304: the port the tester asked for
	require.NoError(t, err)

	t.Cleanup(func() { _ = port.Close() })

	var fd = int(port.Fd())

	for _, line := range []int{unix.TIOCM_RTS, unix.TIOCM_DTR} {
		for _, on := range []bool{false, true, false} {
			require.NoError(t, setSerialControlLine(fd, line, on))

			var lines, getErr = unix.IoctlGetInt(fd, unix.TIOCMGET)
			require.NoError(t, getErr)
			assert.Equal(t, on, lines&line != 0, "line 0x%x after asking for %v", line, on)
		}
	}
}

// A control line that won't move may be a transmitter left keyed, so the
// failure is logged rather than dropped.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestPttSetRealSerialLineFailureLogged(t *testing.T) {
	var p, _ = openTestPTTSerialPort(t, LineRTS, LineNone)

	p.serialControl = func(int, int, bool) error {
		return unix.EIO
	}

	var hook = capturePTTLog(t)

	p.Set(OutputPTT, 0, 1)

	var entry = pttLogEntry(t, hook, "Can't set serial port control line")
	assert.Equal(t, logrus.ErrorLevel, entry.Level)
	var loggedErr, isErr = entry.Data[logrus.ErrorKey].(error)
	require.True(t, isErr, "the failure is logged as an error")
	require.ErrorIs(t, loggedErr, unix.EIO)
	assert.Equal(t, 0, entry.Data["channel"])
	assert.Equal(t, true, entry.Data["on"])
}
