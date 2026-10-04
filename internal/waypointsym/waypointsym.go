// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package waypointsym

// index finds an APRS symbol's entry in a symbol table: the tables start at
// space and run to '~', one entry per printable character.
func index[T any](table []T, symbol byte, fallback T) T {
	var i = int(symbol) - ' '
	if i < 0 || i >= len(table) {
		return fallback
	}

	return table[i]
}

// Garmin returns the Garmin symbol code, for a $PGRMW sentence, for an APRS
// symbol from the given symbol table: '/' is the primary table and anything
// else the alternate table or an overlay on it.
func Garmin(symtab rune, symbol byte) int {
	if symtab == '/' {
		return index(grm_primary_symtab, symbol, sym_default)
	}

	return index(grm_alternate_symtab, symbol, sym_default)
}

// Magellan returns the Magellan icon, for a $PMGNWPL sentence, for an APRS
// symbol from the given symbol table: '/' is the primary table and anything
// else the alternate table or an overlay on it.
func Magellan(symtab rune, symbol byte) string {
	if symtab == '/' {
		return index(mgn_primary_symtab, symbol, MGN_default)
	}

	return index(mgn_alternate_symtab, symbol, MGN_default)
}
