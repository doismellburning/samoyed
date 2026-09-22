package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// il2pTestChannelVersion points the IL2P receiver at a configuration speaking
// the given version, restoring whatever was there before when the test ends.
func il2pTestChannelVersion(t *testing.T, version il2p_version_t) {
	t.Helper()

	var saved = save_audio_config_p
	t.Cleanup(func() { save_audio_config_p = saved })

	var config = new(audio_s)
	for i := range config.achan {
		config.achan[i].il2p_version = version
		config.achan[i].il2p_crc = true // Same as the default with no configuration.
	}

	save_audio_config_p = config
}

func TestIL2PTXFEC(t *testing.T) {
	var testData = []struct {
		version     il2p_version_t
		max_fec     int
		fec_level   int
		use_max_fec int
	}{
		// v0.4 says what it is doing in the header bit.
		{IL2P_VERSION_0_4, 0, 0, 0},
		{IL2P_VERSION_0_4, 1, 1, 1},
		// v0.6 always uses 16 parity symbols and reserves the bit.
		{IL2P_VERSION_0_6, 0, 0, 1},
		{IL2P_VERSION_0_6, 1, 0, 1},
		// Compatibility transmits v0.4.
		{IL2P_VERSION_COMPAT, 0, 0, 0},
		{IL2P_VERSION_COMPAT, 1, 1, 1},
	}

	for _, testDatum := range testData {
		var fec_level, use_max_fec = il2p_tx_fec(testDatum.version, testDatum.max_fec)
		assert.Equal(t, testDatum.fec_level, fec_level, "FEC level for v%s, max_fec %d", testDatum.version, testDatum.max_fec)
		assert.Equal(t, testDatum.use_max_fec, use_max_fec, "max FEC for v%s, max_fec %d", testDatum.version, testDatum.max_fec)
	}
}

func TestIL2PRXMaxFEC(t *testing.T) {
	// Only v0.4 reads the bit; the others know it is reserved.
	assert.Equal(t, 0, il2p_rx_max_fec(IL2P_VERSION_0_4, 0))
	assert.Equal(t, 1, il2p_rx_max_fec(IL2P_VERSION_0_4, 1))
	assert.Equal(t, 1, il2p_rx_max_fec(IL2P_VERSION_0_6, 0))
	assert.Equal(t, 1, il2p_rx_max_fec(IL2P_VERSION_0_6, 1))
	assert.Equal(t, 1, il2p_rx_max_fec(IL2P_VERSION_COMPAT, 0))
	assert.Equal(t, 1, il2p_rx_max_fec(IL2P_VERSION_COMPAT, 1))
}

func TestIL2PChannelVersionDefault(t *testing.T) {
	var saved = save_audio_config_p
	t.Cleanup(func() { save_audio_config_p = saved })

	save_audio_config_p = nil
	assert.Equal(t, IL2P_VERSION_0_6, il2p_channel_version(0))

	il2pTestChannelVersion(t, IL2P_VERSION_0_4)
	assert.Equal(t, IL2P_VERSION_0_4, il2p_channel_version(0))
}

// Send a frame over the fake modem and see whether the receiver, speaking the
// version configured for the channel, makes sense of it.
func TestIL2POnAirVersions(t *testing.T) {
	il2p_init(0)

	// Check the information part of whatever arrives against il2pTestText, so
	// build the frame directly rather than from text: the IL2P header cannot
	// represent every combination of the AX.25 address C bits, and a frame
	// that changes shape in flight fails the trailing CRC check.
	var addrs [AX25_MAX_ADDRS]string
	addrs[0] = "Q1TEST"
	addrs[1] = "Q2TEST"

	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_UI, 0, 0xF0, []byte(il2pTestText))
	require.NotNil(t, pp)

	var testData = []struct {
		name       string
		tx_version il2p_version_t
		max_fec    int
		rx_version il2p_version_t
		received   bool
	}{
		{"v0.4 automatic FEC to v0.4", IL2P_VERSION_0_4, 0, IL2P_VERSION_0_4, true},
		{"v0.4 max FEC to v0.4", IL2P_VERSION_0_4, 1, IL2P_VERSION_0_4, true},
		{"v0.6 to v0.6", IL2P_VERSION_0_6, 0, IL2P_VERSION_0_6, true},
		{"v0.6 to compat", IL2P_VERSION_0_6, 0, IL2P_VERSION_COMPAT, true},
		// v0.4 max FEC has the same payload sizing as v0.6 and differs only in
		// the header bit, which a v0.6 receiver ignores.
		{"v0.4 max FEC to v0.6", IL2P_VERSION_0_4, 1, IL2P_VERSION_0_6, true},
		{"v0.4 max FEC to compat", IL2P_VERSION_0_4, 1, IL2P_VERSION_COMPAT, true},
		// A compatibility frame with max FEC is understood by both.
		{"compat to compat", IL2P_VERSION_COMPAT, 1, IL2P_VERSION_COMPAT, true},
		{"compat to v0.4", IL2P_VERSION_COMPAT, 1, IL2P_VERSION_0_4, true},
		{"compat to v0.6", IL2P_VERSION_COMPAT, 1, IL2P_VERSION_0_6, true},
		// These are the mismatches the version setting exists for.
		{"v0.6 to v0.4", IL2P_VERSION_0_6, 0, IL2P_VERSION_0_4, false},
		{"v0.4 automatic FEC to v0.6", IL2P_VERSION_0_4, 0, IL2P_VERSION_0_6, false},
		{"compat automatic FEC to compat", IL2P_VERSION_COMPAT, 0, IL2P_VERSION_COMPAT, false},
	}

	for _, testDatum := range testData {
		t.Run(testDatum.name, func(t *testing.T) {
			il2pTestChannelVersion(t, testDatum.rx_version)

			var recorder = il2pLoopback(t)

			require.Positive(t, il2p_send_frame(0, pp, testDatum.tx_version, testDatum.max_fec, 0))

			il2p_rec_bit(0, 0, 0, 0) // Extra bit to flush the state machine.

			var received = recorder.take()

			if testDatum.received {
				require.Len(t, received, 1)
				assert.Equal(t, il2pTestText, string(received[0].info))
			} else {
				assert.Empty(t, received)
			}
		})
	}
}
