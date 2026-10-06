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

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsInitImplDisabledStartsNothing(t *testing.T) {
	var port = freeTCPPort(t)

	var audio = new(RadioConfig)
	audio.chan_medium[0] = MEDIUM_RADIO

	// Port 0 is "disabled": no endpoint, and nothing is pushed.
	var mc = new(misc_config_s)
	metrics_init(t.Context(), audio, mc)

	// Nothing should be listening on the port we picked either; the point is
	// just that metrics_init returned without binding anything.
	var dialer = new(net.Dialer)
	dialer.Timeout = time.Second

	var conn, err = dialer.DialContext(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil {
		_ = conn.Close()
	}

	assert.Error(t, err, "a disabled metrics endpoint listens nowhere")
}

func TestMetricsStartImplServesAndMarksChannels(t *testing.T) {
	var audio = new(RadioConfig)
	audio.chan_medium[0] = MEDIUM_RADIO
	audio.chan_medium[1] = MEDIUM_NETTNC

	var listener, _ = testutils.Listen(t)

	var port = listener.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert // A TCP listener has a TCP address.

	var ctx, cancel = context.WithCancel(t.Context())
	t.Cleanup(cancel)

	metricsStart(ctx, audio, listener)

	assert.InDelta(t, 1, metricValue(t, "samoyed_channel_up", map[string]string{"channel": "0"}), 0,
		"a radio channel is up")
	assert.InDelta(t, 0, metricValue(t, "samoyed_channel_up", map[string]string{"channel": "1"}), 0,
		"a network TNC channel is not a radio channel")
	assert.InDelta(t, 0, metricValue(t, "samoyed_channel_up", map[string]string{"channel": "2"}), 0,
		"nor is one never configured")

	// The endpoint was bound before it was handed over, so it can be scraped
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
	// Hold the port ourselves, on all interfaces as metrics_init binds, so
	// that metrics_init's own bind fails.
	var listener, err = new(net.ListenConfig).Listen(t.Context(), "tcp", ":0")
	require.NoError(t, err)

	t.Cleanup(func() { _ = listener.Close() })

	var addr, ok = listener.Addr().(*net.TCPAddr)
	require.True(t, ok)

	var audio = new(RadioConfig)
	audio.chan_medium[0] = MEDIUM_RADIO

	var mc = new(misc_config_s)
	mc.metrics_port = addr.Port

	// A failure to bind is reported and metrics_init returns rather than
	// panicking or claiming to be listening.
	require.NotPanics(t, func() { metrics_init(t.Context(), audio, mc) })

	// The channel state was still pushed before the bind was attempted.
	assert.InDelta(t, 1, metricValue(t, "samoyed_channel_up", map[string]string{"channel": "0"}), 0)
}
