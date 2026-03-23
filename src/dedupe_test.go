package direwolf

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_dedupe_check_duplicate(t *testing.T) {
	var ds = NewDedupeService(30 * time.Second)

	var pp = ax25_from_text("W1AW>APRS:test packet", true)
	require.NotNil(t, pp)

	ds.Remember(pp, 0)

	assert.True(t, ds.Check(pp, 0), "packet remembered on channel 0 should be a duplicate on channel 0")
}

func Test_dedupe_check_different_channel_not_duplicate(t *testing.T) {
	var ds = NewDedupeService(30 * time.Second)

	var pp = ax25_from_text("W1AW>APRS:test packet", true)
	require.NotNil(t, pp)

	ds.Remember(pp, 0)

	assert.False(t, ds.Check(pp, 1), "packet remembered on channel 0 should not be a duplicate on channel 1")
}

func Test_dedupe_check_expired_not_duplicate(t *testing.T) {
	var ds = NewDedupeService(30 * time.Second)

	var pp = ax25_from_text("W1AW>APRS:test packet", true)
	require.NotNil(t, pp)

	ds.Remember(pp, 0)

	// Wind the recorded timestamp back past the TTL so the entry is expired.
	var idx = ds.insertNext - 1
	if idx < 0 {
		idx = HISTORY_MAX - 1
	}
	ds.history[idx].time_stamp = time.Now().Add(-1 * time.Hour)

	assert.False(t, ds.Check(pp, 0), "expired entry should not be considered a duplicate")
}

func Test_dedupe_check_empty_history_not_duplicate(t *testing.T) {
	var ds = NewDedupeService(30 * time.Second)

	var pp = ax25_from_text("W1AW>APRS:test packet", true)
	require.NotNil(t, pp)

	assert.False(t, ds.Check(pp, 0), "nothing has been remembered, so no duplicates")
}
