package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_pfilter_validate(t *testing.T) {
	var p_igate_config igate_config_s
	p_igate_config.max_digi_hops = 2
	pfilter_init(&p_igate_config, 0)

	t.Run("valid APRS filter returns no error", func(t *testing.T) {
		assert.NoError(t, pfilter_validate(0, 0, "t/p & b/Q1TEST", true))
	})

	t.Run("valid connected-mode filter returns no error", func(t *testing.T) {
		assert.NoError(t, pfilter_validate(0, 0, "b/Q1TEST", false))
	})

	t.Run("bad wildcard placement returns an error", func(t *testing.T) {
		assert.Error(t, pfilter_validate(0, 0, "b/Q1TEST*Q2TEST", true))
	})

	t.Run("unrecognized filter type returns an error", func(t *testing.T) {
		assert.Error(t, pfilter_validate(0, 0, "x/", true))
	})

	t.Run("filter type not allowed in connected mode returns an error", func(t *testing.T) {
		assert.Error(t, pfilter_validate(0, 0, "t/p", false))
	})

	t.Run("unbalanced parentheses returns an error", func(t *testing.T) {
		assert.Error(t, pfilter_validate(0, 0, "t/w & ( t/w | t/w ", true))
	})

	t.Run("error message reflects the real from/to channels", func(t *testing.T) {
		var err = pfilter_validate(1, 2, "x/", true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "filter[1,2]")
	})
}
