// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsInitImplServesAndMarksChannels(t *testing.T) {
	var audio = new(RadioConfig)
	audio.chan_medium[0] = MEDIUM_RADIO
	audio.chan_medium[1] = MEDIUM_NETTNC

	var ctx, cancel = context.WithCancel(t.Context())
	t.Cleanup(cancel)

	var listening = metrics_init(ctx, audio, "127.0.0.1:0")
	require.NotNil(t, listening, "the endpoint is listening")

	var port = addrPort(listening)

	assert.InDelta(t, 1, metricValue(t, "samoyed_channel_up", map[string]string{"channel": "0"}), 0,
		"a radio channel is up")
	assert.InDelta(t, 0, metricValue(t, "samoyed_channel_up", map[string]string{"channel": "1"}), 0,
		"a network TNC channel is not a radio channel")
	assert.InDelta(t, 0, metricValue(t, "samoyed_channel_up", map[string]string{"channel": "2"}), 0,
		"nor is one never configured")

	// The endpoint is bound before metrics_init returns, so it can be scraped
	// straight away, and the seeded series for the radio channel are there.
	var url = "http://127.0.0.1:" + strconv.Itoa(port) + "/metrics"

	var req, reqErr = http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, reqErr)

	var resp, getErr = http.DefaultClient.Do(req)
	require.NoError(t, getErr)

	var body, readErr = io.ReadAll(resp.Body)
	require.NoError(t, resp.Body.Close())
	require.NoError(t, readErr)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), `samoyed_channel_up{channel="0"} 1`)
	assert.Contains(t, string(body), `samoyed_tx_queue_depth{channel="0",priority="0"} 0`)

	// Cancelling the context shuts the endpoint down; the watcher goroutine
	// sees http.ErrServerClosed, which it does not report as a failure.
	cancel()

	require.Eventually(t, func() bool {
		var dialer = new(net.Dialer)
		dialer.Timeout = 100 * time.Millisecond

		var conn, err = dialer.DialContext(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return true
		}

		_ = conn.Close()

		return false
	}, 2*time.Second, 10*time.Millisecond, "the endpoint stops listening once its context is cancelled")
}

func TestMetricsInitImplPortInUse(t *testing.T) {
	// Hold the port ourselves, on all interfaces as a configured port is bound, so
	// that metrics_init's own bind fails.
	var listener, err = new(net.ListenConfig).Listen(t.Context(), "tcp", ":0")
	require.NoError(t, err)

	t.Cleanup(func() { _ = listener.Close() })

	var addr, ok = listener.Addr().(*net.TCPAddr)
	require.True(t, ok)

	var audio = new(RadioConfig)
	audio.chan_medium[0] = MEDIUM_RADIO

	// A failure to bind is reported and metrics_init returns rather than
	// panicking or claiming to be listening.
	var listening net.Addr

	require.NotPanics(t, func() { listening = metrics_init(t.Context(), audio, fmt.Sprintf(":%d", addr.Port)) })
	assert.Nil(t, listening, "an endpoint that failed to bind is not listening")

	// The channel state was still pushed before the bind was attempted.
	assert.InDelta(t, 1, metricValue(t, "samoyed_channel_up", map[string]string{"channel": "0"}), 0)
}
