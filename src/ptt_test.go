package direwolf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// setupGPIODChannel wires save_audio_config_p and gpiod_line for channel 0 OCTYPE_PTT,
// returning the mock so the caller can inspect it.  The test's Cleanup restores
// both globals to a safe state.
func setupGPIODChannel(t *testing.T, invert bool) *mockGPIODLine {
	t.Helper()

	var mock = new(mockGPIODLine)
	gpiod_line[0][OCTYPE_PTT] = mock

	var cfg audio_s
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_GPIOD
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 0
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_invert = invert
	save_audio_config_p = &cfg

	t.Cleanup(func() {
		gpiod_line[0][OCTYPE_PTT] = nil
		save_audio_config_p = nil
	})

	return mock
}

// TestPttSetRealGPIOD_Activate verifies that PTT-active drives the line high.
func TestPttSetRealGPIOD_Activate(t *testing.T) {
	var mock = setupGPIODChannel(t, false)

	ptt_set_real(OCTYPE_PTT, 0, 1)

	assert.Equal(t, 1, mock.value, "line should be high when PTT is active")
}

// TestPttSetRealGPIOD_Deactivate verifies that PTT-inactive drives the line low.
func TestPttSetRealGPIOD_Deactivate(t *testing.T) {
	var mock = setupGPIODChannel(t, false)

	ptt_set_real(OCTYPE_PTT, 0, 0)

	assert.Equal(t, 0, mock.value, "line should be low when PTT is inactive")
}

// TestPttSetRealGPIOD_Invert_Activate verifies that ptt_invert flips the level
// when PTT is active (signal=1 → line low).
func TestPttSetRealGPIOD_Invert_Activate(t *testing.T) {
	var mock = setupGPIODChannel(t, true)

	ptt_set_real(OCTYPE_PTT, 0, 1)

	assert.Equal(t, 0, mock.value, "inverted line should be low when PTT is active")
}

// TestPttSetRealGPIOD_Invert_Deactivate verifies that ptt_invert flips the level
// when PTT is inactive (signal=0 → line high).
func TestPttSetRealGPIOD_Invert_Deactivate(t *testing.T) {
	var mock = setupGPIODChannel(t, true)

	ptt_set_real(OCTYPE_PTT, 0, 0)

	assert.Equal(t, 1, mock.value, "inverted line should be high when PTT is inactive")
}

// TestPttSetRealGPIOD_NilLine verifies that ptt_set_real does not panic when
// the GPIOD line handle has not been initialised.
func TestPttSetRealGPIOD_NilLine(t *testing.T) {
	var cfg audio_s
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_GPIOD
	save_audio_config_p = &cfg
	gpiod_line[0][OCTYPE_PTT] = nil

	t.Cleanup(func() { save_audio_config_p = nil })

	require.NotPanics(t, func() {
		ptt_set_real(OCTYPE_PTT, 0, 1)
	})
}

// TestPttTermGPIOD verifies that ptt_term closes every open line handle and
// sets the slot to nil.
func TestPttTermGPIOD(t *testing.T) {
	var mock = setupGPIODChannel(t, false)
	// setupGPIODChannel registers a Cleanup that nils the slot;
	// ptt_term should do the nil-ing itself.

	ptt_term()

	assert.True(t, mock.closed, "ptt_term should close the line handle")
	assert.Nil(t, gpiod_line[0][OCTYPE_PTT], "ptt_term should nil the line handle")
}

// useFakeGPIOSysfs points the sysfs GPIO interface at a temporary directory
// for the duration of the test, so the GPIO paths can be exercised on a
// machine whose kernel does not offer the real one.
func useFakeGPIOSysfs(t *testing.T) string {
	t.Helper()

	var dir = t.TempDir()
	var saved = gpio_sysfs_dir

	gpio_sysfs_dir = dir

	t.Cleanup(func() { gpio_sysfs_dir = saved })

	return dir
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

// useAudioConfig installs cfg as the saved configuration for the duration of
// the test.
func useAudioConfig(t *testing.T, cfg *audio_s) {
	t.Helper()

	save_audio_config_p = cfg

	t.Cleanup(func() { save_audio_config_p = nil })
}

// TestGetAccessToGPIOMissing verifies that an absent GPIO node is reported to
// the caller rather than ending the process.
func TestGetAccessToGPIOMissing(t *testing.T) {
	var err = get_access_to_gpio(filepath.Join(t.TempDir(), "export"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "GPIO user interface")
}

// TestGetAccessToGPIOPresent verifies that a node we can stat is accepted.
func TestGetAccessToGPIOPresent(t *testing.T) {
	var dir = useFakeGPIOSysfs(t)
	writeFakeGPIOExport(t, dir)

	require.NoError(t, get_access_to_gpio(filepath.Join(dir, "export")))
}

// TestExportGPIOOutput verifies that exporting an output line writes the line
// number to "export" and leaves the line low, i.e. off.
func TestExportGPIOOutput(t *testing.T) {
	var dir = useFakeGPIOSysfs(t)
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio25")

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25
	useAudioConfig(t, cfg)

	require.NoError(t, export_gpio(0, OCTYPE_PTT, false, 1))

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
	var dir = useFakeGPIOSysfs(t)
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio25")

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25
	useAudioConfig(t, cfg)

	require.NoError(t, export_gpio(0, OCTYPE_PTT, true, 1))

	var direction, dirErr = os.ReadFile(filepath.Join(dir, "gpio25", "direction")) //nolint:gosec
	require.NoError(t, dirErr)
	assert.Equal(t, "high", string(direction), "an inverted output line should start off, i.e. high")
}

// TestExportGPIOInput verifies that an input line is set to "in" and takes its
// number from the input rather than the output configuration.
func TestExportGPIOInput(t *testing.T) {
	var dir = useFakeGPIOSysfs(t)
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio7")

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].ictrl[ICTYPE_TXINH].in_gpio_num = 7
	useAudioConfig(t, cfg)

	require.NoError(t, export_gpio(0, ICTYPE_TXINH, false, 0))

	var direction, dirErr = os.ReadFile(filepath.Join(dir, "gpio7", "direction")) //nolint:gosec
	require.NoError(t, dirErr)
	assert.Equal(t, "in", string(direction))
}

// TestExportGPIOSuffixedNode verifies the CubieBoard-style node names, where
// GPIO 25 appears as something like gpio25_ph11.
func TestExportGPIOSuffixedNode(t *testing.T) {
	var dir = useFakeGPIOSysfs(t)
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio25_ph11")

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25
	useAudioConfig(t, cfg)

	require.NoError(t, export_gpio(0, OCTYPE_PTT, false, 1))

	var direction, dirErr = os.ReadFile(filepath.Join(dir, "gpio25_ph11", "direction")) //nolint:gosec
	require.NoError(t, dirErr)
	assert.Equal(t, "low", string(direction))
}

// TestExportGPIONoSuchNode verifies that a line that never appears under
// /sys/class/gpio is reported rather than ending the process.
func TestExportGPIONoSuchNode(t *testing.T) {
	var dir = useFakeGPIOSysfs(t)
	writeFakeGPIOExport(t, dir)

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25
	useAudioConfig(t, cfg)

	var err = export_gpio(0, OCTYPE_PTT, false, 1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gpio number 25")
}

// TestExportGPIONoSysfs verifies that a system without the sysfs GPIO
// interface is reported rather than ending the process.
func TestExportGPIONoSysfs(t *testing.T) {
	useFakeGPIOSysfs(t)

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25
	useAudioConfig(t, cfg)

	var err = export_gpio(0, OCTYPE_PTT, false, 1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "GPIO user interface")
}

// TestPttInitGPIONoSysfs verifies that ptt_init reports a GPIO channel it
// cannot set up, rather than ending the process, which is what made the
// function untestable.
func TestPttInitGPIONoSysfs(t *testing.T) {
	useFakeGPIOSysfs(t)

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_GPIO
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25
	useAudioConfig(t, cfg)

	var err = ptt_init(cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "GPIO user interface")
}

// TestPttInitGPIO verifies that a GPIO channel that can be set up leaves the
// line off and reports no error.
func TestPttInitGPIO(t *testing.T) {
	var dir = useFakeGPIOSysfs(t)
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio25")

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_GPIO
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25
	useAudioConfig(t, cfg)

	require.NoError(t, ptt_init(cfg))

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
	useAudioConfig(t, cfg)

	var err = ptt_init(cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "/dev/samoyed-no-such-gpiochip")
	assert.Nil(t, gpiod_line[0][OCTYPE_PTT], "a line that could not be requested should not be recorded")
}

// TestPttInitRollsBackOnFailure verifies that a failure part way through
// setting up gives back what has been set up already: setting up is
// incremental, and the descriptors and line handles live in globals the
// caller cannot reach.
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
	useAudioConfig(t, cfg)

	require.Error(t, ptt_init(cfg))
	assert.Nil(t, ptt_fd[0][OCTYPE_PTT], "a serial port opened before the failure should be closed again")
}

// TestPttInitSerialOpenFailure verifies that a serial port we cannot open is
// not fatal: the channel falls back to no PTT method, as it always has.
func TestPttInitSerialOpenFailure(t *testing.T) {
	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_SERIAL
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_device = filepath.Join(t.TempDir(), "no-such-tty")
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_line = PTT_LINE_RTS
	useAudioConfig(t, cfg)

	require.NoError(t, ptt_init(cfg))
	assert.Equal(t, PTT_METHOD_NONE, cfg.achan[0].octrl[OCTYPE_PTT].ptt_method)
}

// TestGetInputRealGPIO verifies that an input line's value is read from sysfs,
// and that invert flips it.
func TestGetInputRealGPIO(t *testing.T) {
	var dir = useFakeGPIOSysfs(t)
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio7")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gpio7", "value"), []byte("1"), 0o600))

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].ictrl[ICTYPE_TXINH].method = PTT_METHOD_GPIO
	cfg.achan[0].ictrl[ICTYPE_TXINH].in_gpio_name = "gpio7"
	useAudioConfig(t, cfg)

	assert.Equal(t, 1, get_input_real(ICTYPE_TXINH, 0))

	cfg.achan[0].ictrl[ICTYPE_TXINH].invert = true

	assert.Equal(t, 0, get_input_real(ICTYPE_TXINH, 0), "invert should flip the value read")
}

// TestGetInputRealGPIONoNode verifies that an input line whose sysfs node is
// missing reports an error rather than ending the process.
func TestGetInputRealGPIONoNode(t *testing.T) {
	useFakeGPIOSysfs(t)

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].ictrl[ICTYPE_TXINH].method = PTT_METHOD_GPIO
	cfg.achan[0].ictrl[ICTYPE_TXINH].in_gpio_name = "gpio7"
	useAudioConfig(t, cfg)

	assert.Equal(t, -1, get_input_real(ICTYPE_TXINH, 0))
}

// TestPttInitGPIOThenSet is a regression test for a GPIO line that never
// moved: export_gpio worked out which node under /sys/class/gpio belongs to
// the configured line number, but kept that to itself, so ptt_set built its
// path from an empty name and could not open anything.
func TestPttInitGPIOThenSet(t *testing.T) {
	var dir = useFakeGPIOSysfs(t)
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio25_ph11")

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].octrl[OCTYPE_PTT].ptt_method = PTT_METHOD_GPIO
	cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_num = 25
	useAudioConfig(t, cfg)

	require.NoError(t, ptt_init(cfg))
	assert.Equal(t, "gpio25_ph11", cfg.achan[0].octrl[OCTYPE_PTT].out_gpio_name,
		"the node name found while exporting should be remembered")

	ptt_set_real(OCTYPE_PTT, 0, 1)

	var on, onErr = os.ReadFile(filepath.Join(dir, "gpio25_ph11", "value")) //nolint:gosec
	require.NoError(t, onErr)
	assert.Equal(t, "1", string(on), "PTT on should drive the line")

	ptt_set_real(OCTYPE_PTT, 0, 0)

	var off, offErr = os.ReadFile(filepath.Join(dir, "gpio25_ph11", "value")) //nolint:gosec
	require.NoError(t, offErr)
	assert.Equal(t, "0", string(off), "PTT off should release the line")
}

// TestPttInitGPIOInputThenGet is the same regression on the input side, where
// get_input reads the node that export_gpio found.
func TestPttInitGPIOInputThenGet(t *testing.T) {
	var dir = useFakeGPIOSysfs(t)
	writeFakeGPIOExport(t, dir)
	writeFakeGPIONode(t, dir, "gpio7_pi13")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gpio7_pi13", "value"), []byte("1"), 0o600))

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.achan[0].ictrl[ICTYPE_TXINH].method = PTT_METHOD_GPIO
	cfg.achan[0].ictrl[ICTYPE_TXINH].in_gpio_num = 7
	useAudioConfig(t, cfg)

	require.NoError(t, ptt_init(cfg))
	assert.Equal(t, "gpio7_pi13", cfg.achan[0].ictrl[ICTYPE_TXINH].in_gpio_name,
		"the node name found while exporting should be remembered")
	assert.Equal(t, 1, get_input_real(ICTYPE_TXINH, 0))
}

// TestPttTermBeforeAudioConfig covers a stop signal that arrives while we are
// still starting up: the shutdown path runs cleanup, and ptt_term used to
// dereference the audio configuration that audio_open had not installed yet,
// so a stop during startup panicked instead of shutting down.
func TestPttTermBeforeAudioConfig(t *testing.T) {
	var saved = save_audio_config_p

	save_audio_config_p = nil

	t.Cleanup(func() { save_audio_config_p = saved })

	require.NotPanics(t, ptt_term)
}
