// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sdr

import (
	"io"
	"net"
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_RTLTCP_readsHeaderAndSamples(t *testing.T) {
	t.Parallel()

	var server = testutils.NewRTLTCPServer(t, []byte{1, 2, 3, 4})

	var r, err = DialRTLTCP(t.Context(), server.Address)
	require.NoError(t, err)

	defer r.Close()

	assert.Equal(t, uint32(5), r.TunerType)
	assert.Equal(t, uint32(29), r.GainCount)

	var iq = make([]byte, 4)
	_, err = io.ReadFull(r, iq)
	require.NoError(t, err)
	assert.Equal(t, []byte{1, 2, 3, 4}, iq)
}

func Test_RTLTCP_sendsCommands(t *testing.T) {
	t.Parallel()

	var server = testutils.NewRTLTCPServer(t, nil)

	var r, err = DialRTLTCP(t.Context(), server.Address)
	require.NoError(t, err)

	defer r.Close()

	require.NoError(t, r.SetFrequency(144_650_000))
	require.NoError(t, r.SetSampleRate(1_200_000))
	require.NoError(t, r.SetFrequencyCorrection(-12))
	require.NoError(t, r.SetGain(maybe.Just(40.2)))
	require.NoError(t, r.SetGain(maybe.Nothing[float64]()))

	assert.Equal(t, []testutils.RTLTCPCommand{
		{Command: 0x01, Param: 144_650_000},
		{Command: 0x02, Param: 1_200_000},
		{Command: 0x05, Param: -12},
		{Command: 0x03, Param: 1},
		{Command: 0x04, Param: 402},
		{Command: 0x03, Param: 0},
	}, server.RequireCommands(t, 6))
}

func Test_RTLTCP_refusesOutOfRange(t *testing.T) {
	t.Parallel()

	var server = testutils.NewRTLTCPServer(t, nil)

	var r, err = DialRTLTCP(t.Context(), server.Address)
	require.NoError(t, err)

	defer r.Close()

	require.Error(t, r.SetFrequency(1<<40))
	require.Error(t, r.SetFrequencyCorrection(5000))
	require.Error(t, r.SetGain(maybe.Just(-3.0)))
}

func Test_RTLTCP_refusesSomethingElse(t *testing.T) {
	t.Parallel()

	var ln, port = testutils.Listen(t)

	go func() {
		var conn, err = ln.Accept()
		if err != nil {
			return
		}

		defer conn.Close()

		_, _ = conn.Write([]byte("HTTP/1.1 400"))
	}()

	var _, err = DialRTLTCP(t.Context(), net.JoinHostPort("127.0.0.1", port))
	require.ErrorIs(t, err, ErrNotRTLTCP)
}

func Test_RTLTCP_reportsNothingListening(t *testing.T) {
	t.Parallel()

	var _, err = DialRTLTCP(t.Context(), net.JoinHostPort("127.0.0.1", testutils.UnusedPort(t)))
	require.Error(t, err)
}
