// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAX25LinkFirstTryConnectIsNotARetry is a regression test: the retry
// counter used to be driven from SET_RC, incrementing whenever the retry count
// rose.  But establish_data_link sets RC to 1 for the *first* SABM/SABME (see
// the erratum note there), so every successful first-try connect reported a
// retry, and t3_expiry's keepalive poll did the same.  A link that worked
// perfectly looked like a link in trouble.
func TestAX25LinkFirstTryConnectIsNotARetry(t *testing.T) {
	const (
		MY_CALL    = "Q1TEST"
		THEIR_CALL = "Q2TEST"
		CHANNEL    = 1
	)

	var labels = map[string]string{"channel": "1"}

	setupTestEnv(t)

	var before = metricValue(t, "samoyed_ax25_link_retries_total", labels)

	// Connect request.
	var E = new(dlq_item_t)
	E._type = DLQ_CONNECT_REQUEST
	E._chan = CHANNEL
	E.addrs[OWNCALL] = MY_CALL
	E.addrs[PEERCALL] = THEIR_CALL
	E.num_addr = 2

	dl_connect_request(E)

	// Peer acknowledges first time, so nothing was ever retransmitted.
	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = THEIR_CALL
	addrs[PEERCALL] = MY_CALL

	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	require.NotNil(t, pp)

	E = new(dlq_item_t)
	E._chan = CHANNEL
	E.pp = pp

	lm_data_indication(E)

	require.NotNil(t, ax25Link.listHead)
	require.Equal(t, state_3_connected, ax25Link.listHead.state, "%+v", ax25Link.listHead)

	assert.InDelta(t, before, metricValue(t, "samoyed_ax25_link_retries_total", labels), 0,
		"a first-try connect is not a retry")
}

// TestAX25LinkT1ExpiryCountsARetry is the other half: a T1 expiry that actually
// retransmits must be counted, or the metric would be uselessly always-zero.
func TestAX25LinkT1ExpiryCountsARetry(t *testing.T) {
	const (
		MY_CALL    = "Q1TEST"
		THEIR_CALL = "Q2TEST"
		CHANNEL    = 1
	)

	var labels = map[string]string{"channel": "1"}

	setupTestEnv(t)

	var E = new(dlq_item_t)
	E._type = DLQ_CONNECT_REQUEST
	E._chan = CHANNEL
	E.addrs[OWNCALL] = MY_CALL
	E.addrs[PEERCALL] = THEIR_CALL
	E.num_addr = 2

	dl_connect_request(E)

	require.NotNil(t, ax25Link.listHead)
	require.Equal(t, state_1_awaiting_connection, ax25Link.listHead.state)

	var before = metricValue(t, "samoyed_ax25_link_retries_total", labels)

	// No UA came back, so T1 expires and the SABM is sent again.
	t1_expiry(ax25Link.listHead)

	assert.InDelta(t, before+1, metricValue(t, "samoyed_ax25_link_retries_total", labels), 0,
		"a retransmitted SABM is a retry")
}

// TestAX25LinkT3ExpiryIsNotARetry guards the other SET_RC(S, 1) site: T3 is the
// idle keepalive poll, not a retransmission of anything.
func TestAX25LinkT3ExpiryIsNotARetry(t *testing.T) {
	const (
		MY_CALL    = "Q1TEST"
		THEIR_CALL = "Q2TEST"
		CHANNEL    = 1
	)

	var labels = map[string]string{"channel": "1"}

	setupTestEnv(t)

	var E = new(dlq_item_t)
	E._type = DLQ_CONNECT_REQUEST
	E._chan = CHANNEL
	E.addrs[OWNCALL] = MY_CALL
	E.addrs[PEERCALL] = THEIR_CALL
	E.num_addr = 2

	dl_connect_request(E)

	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = THEIR_CALL
	addrs[PEERCALL] = MY_CALL

	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	require.NotNil(t, pp)

	E = new(dlq_item_t)
	E._chan = CHANNEL
	E.pp = pp

	lm_data_indication(E)

	require.NotNil(t, ax25Link.listHead)
	require.Equal(t, state_3_connected, ax25Link.listHead.state)

	var before = metricValue(t, "samoyed_ax25_link_retries_total", labels)

	t3_expiry(ax25Link.listHead)

	assert.InDelta(t, before, metricValue(t, "samoyed_ax25_link_retries_total", labels), 0,
		"a T3 keepalive poll is not a retry")
}
