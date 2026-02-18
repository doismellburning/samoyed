package direwolf

import (
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
