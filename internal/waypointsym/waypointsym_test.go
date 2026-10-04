// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package waypointsym

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTablesCoverPrintableCharacters(t *testing.T) {
	assert.Len(t, grm_primary_symtab, SYMTAB_SIZE)
	assert.Len(t, grm_alternate_symtab, SYMTAB_SIZE)
	assert.Len(t, mgn_primary_symtab, SYMTAB_SIZE)
	assert.Len(t, mgn_alternate_symtab, SYMTAB_SIZE)
}

func TestGarmin(t *testing.T) {
	assert.Equal(t, sym_rbcn, Garmin('/', '#'), "primary digi")
	assert.Equal(t, sym_tall_tower, Garmin('/', 'y'), "primary yagi")
	assert.Equal(t, sym_wreck, Garmin('\\', 'x'), "alternate wreck")
	assert.Equal(t, sym_car, Garmin('S', 'u'), "overlaid truck uses the alternate table")
	assert.Equal(t, sym_default, Garmin('/', ' '), "no symbol")
}

func TestMagellan(t *testing.T) {
	assert.Equal(t, MGN_aerial, Magellan('/', '#'), "primary digi")
	assert.Equal(t, MGN_ATM, Magellan('\\', '$'), "alternate ATM")
	assert.Equal(t, MGN_aerial, Magellan('I', '&'), "overlaid IGate uses the alternate table")
	assert.Equal(t, MGN_default, Magellan('/', ' '), "no symbol")
}

func TestOutOfRangeSymbolsFallBack(t *testing.T) {
	for _, symbol := range []byte{0, '\n', 0x1f, 0x7f, 0xff} {
		assert.Equal(t, sym_default, Garmin('/', symbol))
		assert.Equal(t, sym_default, Garmin('\\', symbol))
		assert.Equal(t, MGN_default, Magellan('/', symbol))
		assert.Equal(t, MGN_default, Magellan('\\', symbol))
	}
}
