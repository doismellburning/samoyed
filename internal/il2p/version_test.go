// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package il2p

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIL2PTXFEC(t *testing.T) {
	var testData = []struct {
		version     Version
		max_fec     int
		fec_level   int
		use_max_fec int
	}{
		// v0.4 says what it is doing in the header bit.
		{Version04, 0, 0, 0},
		{Version04, 1, 1, 1},
		// v0.6 always uses 16 parity symbols and reserves the bit.
		{Version06, 0, 0, 1},
		{Version06, 1, 0, 1},
		// Compatibility transmits v0.4.
		{VersionCompat, 0, 0, 0},
		{VersionCompat, 1, 1, 1},
	}

	for _, testDatum := range testData {
		var fec_level, use_max_fec = il2p_tx_fec(testDatum.version, testDatum.max_fec)
		assert.Equal(t, testDatum.fec_level, fec_level, "FEC level for v%s, max_fec %d", testDatum.version, testDatum.max_fec)
		assert.Equal(t, testDatum.use_max_fec, use_max_fec, "max FEC for v%s, max_fec %d", testDatum.version, testDatum.max_fec)
	}
}

func TestIL2PRXMaxFEC(t *testing.T) {
	// Only v0.4 reads the bit; the others know it is reserved.
	assert.Equal(t, 0, il2p_rx_max_fec(Version04, 0))
	assert.Equal(t, 1, il2p_rx_max_fec(Version04, 1))
	assert.Equal(t, 1, il2p_rx_max_fec(Version06, 0))
	assert.Equal(t, 1, il2p_rx_max_fec(Version06, 1))
	assert.Equal(t, 1, il2p_rx_max_fec(VersionCompat, 0))
	assert.Equal(t, 1, il2p_rx_max_fec(VersionCompat, 1))
}

// Send a frame over the fake modem and see whether the receiver, speaking the
// version it was given, makes sense of it.
func TestIL2POnAirVersions(t *testing.T) {
	// Check the information part of whatever arrives against il2pTestText, so
	// build the frame directly rather than from text: the IL2P header cannot
	// represent every combination of the AX.25 address C bits, and a frame
	// that changes shape in flight fails the trailing CRC check.
	var addrs [ax25.MaxAddrs]string
	addrs[0] = "Q1TEST"
	addrs[1] = "Q2TEST"

	var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, 0xF0, []byte(il2pTestText))
	require.NotNil(t, pp)

	var testData = []struct {
		name       string
		tx_version Version
		max_fec    int
		rx_version Version
		received   bool
	}{
		{"v0.4 automatic FEC to v0.4", Version04, 0, Version04, true},
		{"v0.4 max FEC to v0.4", Version04, 1, Version04, true},
		{"v0.6 to v0.6", Version06, 0, Version06, true},
		{"v0.6 to compat", Version06, 0, VersionCompat, true},
		// v0.4 max FEC has the same payload sizing as v0.6 and differs only in
		// the header bit, which a v0.6 receiver ignores.
		{"v0.4 max FEC to v0.6", Version04, 1, Version06, true},
		{"v0.4 max FEC to compat", Version04, 1, VersionCompat, true},
		// A compatibility frame with max FEC is understood by both.
		{"compat to compat", VersionCompat, 1, VersionCompat, true},
		{"compat to v0.4", VersionCompat, 1, Version04, true},
		{"compat to v0.6", VersionCompat, 1, Version06, true},
		// These are the mismatches the version setting exists for.
		{"v0.6 to v0.4", Version06, 0, Version04, false},
		{"v0.4 automatic FEC to v0.6", Version04, 0, Version06, false},
		{"compat automatic FEC to compat", VersionCompat, 0, VersionCompat, false},
	}

	for _, testDatum := range testData {
		t.Run(testDatum.name, func(t *testing.T) {
			var recorder = il2pLoopback(t, testDatum.rx_version)

			require.Positive(t, recorder.sender.SendFrame(pp, testDatum.tx_version, testDatum.max_fec, true, 0))

			recorder.flush() // Extra bit to flush the state machine.

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
