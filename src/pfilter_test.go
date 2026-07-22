package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_pfilter_validate(t *testing.T) {
	var p_igate_config igate_config_s
	p_igate_config.max_digi_hops = 2
	pfilter_init(&p_igate_config, 0)

	t.Run("valid APRS filter returns no error", func(t *testing.T) {
		assert.NoError(t, pfilter_validate("t/p & b/W2UB", true))
	})

	t.Run("valid connected-mode filter returns no error", func(t *testing.T) {
		assert.NoError(t, pfilter_validate("b/W2UB", false))
	})

	t.Run("bad wildcard placement returns an error", func(t *testing.T) {
		assert.Error(t, pfilter_validate("b/W2UB*OSZ", true))
	})

	t.Run("unrecognized filter type returns an error", func(t *testing.T) {
		assert.Error(t, pfilter_validate("x/", true))
	})

	t.Run("filter type not allowed in connected mode returns an error", func(t *testing.T) {
		assert.Error(t, pfilter_validate("t/p", false))
	})

	t.Run("unbalanced parentheses returns an error", func(t *testing.T) {
		assert.Error(t, pfilter_validate("t/w & ( t/w | t/w ", true))
	})
}
