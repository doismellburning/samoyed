// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package symbols

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
)

// ToTones gives the APRStt "AB" form of a symbol: AB1nn for the primary
// table, AB2nn for the alternate, and AB0nn followed by the overlay in
// two-key form when there is one.  nn is the symbol's offset from space.
func TestToTones(t *testing.T) {
	var sd = New()

	for _, tc := range []struct {
		symtab byte
		symbol byte
		want   string
	}{
		{'/', 'K', "AB143"},
		{'/', '!', "AB101"},
		{'\\', 'w', "AB287"},
		{'A', '#', "AB0032A"}, // A is the first letter on the 2 key
		{'S', '#', "AB0037D"}, // S is the fourth letter on the 7 key
		{'9', '#', "AB0039"},  // A digit is its own key
	} {
		assert.Equal(t, tc.want, sd.ToTones(tc.symtab, tc.symbol), "%c%c", tc.symtab, tc.symbol)
	}
}

// The forms of FromDestOrSrc that the ported Dire Wolf test leaves out: the
// SSID of the source, which is only consulted for raw NMEA, the bounds of the
// GPSCnn and GPSEnn forms, and Mic-E, whose destination is a latitude rather
// than a symbol.
func TestFromDestOrSrc(t *testing.T) {
	var sd = New()

	for _, tc := range []struct {
		name       string
		dti        byte
		src        string
		dest       string
		wantSymtab byte
		wantSymbol byte
		wantOK     bool
	}{
		{"source SSID for raw NMEA", '$', "Q1TEST-14", "APDW17", '/', 'k', true},
		{"lowest source SSID", '$', "Q1TEST-1", "APDW17", '/', 'a', true},
		{"source SSID 0 means no symbol", '$', "Q1TEST-0", "APDW17", 0, 0, false},
		{"no source SSID", '$', "Q1TEST", "APDW17", 0, 0, false},
		{"source SSID only for raw NMEA", '!', "Q1TEST-14", "APDW17", 0, 0, false},
		{"destination before source SSID", '$', "Q1TEST-14", "GPSC43", '/', 'K', true},
		{"lowest GPSCnn", '!', "Q1TEST", "GPSC01", '/', '!', true},
		{"highest GPSCnn", '!', "Q1TEST", "GPSC94", '/', '~', true},
		{"GPSEnn", '!', "Q1TEST", "GPSE01", '\\', '!', true},
		{"GPSxy in the primary table", '!', "Q1TEST", "GPSBL", '/', '+', true},
		{"GPSxyz with a lower case overlay", '!', "Q1TEST", "GPSODa", '\\', '#', true},
		{"unknown xy", '!', "Q1TEST", "GPS???", 0, 0, false},
		{"too short for xy", '!', "Q1TEST", "GPS", 0, 0, false},
		{"Mic-E", '`', "Q1TEST", "GPSC43", 0, 0, false},
		{"old Mic-E", '\'', "Q1TEST-14", "GPSC43", 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var symtab, symbol, ok = sd.FromDestOrSrc(tc.dti, tc.src, tc.dest)

			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, string(tc.wantSymtab), string(symtab))
			assert.Equal(t, string(tc.wantSymbol), string(symbol))
		})
	}
}

func TestList(t *testing.T) {
	var sd = New()

	var output = testutils.CaptureOutput(t, sd.List)

	assert.Contains(t, output, "PRIMARY SYMBOL TABLE")
	assert.Contains(t, output, "ALTERNATE SYMBOL TABLE")
	assert.Contains(t, output, "NEW SYMBOLS from symbols-new.txt")

	// One row from each table, and one of each kind of new symbol: primary,
	// overlaid, and alternate.
	assert.Contains(t, output, " /K     PK      43  AB143   School\n")
	assert.Contains(t, output, " \\w     SW      87  AB287   Flooding\n")
	assert.Contains(t, output, " /O     PO      C47  AB147    Original Balloon (think Ham balloon)\n")
	assert.Contains(t, output, " Js     SSJ          AB0835A  Jet Ski\n")
	assert.Contains(t, output, " \\O     AO      E47  AB247    ROCKET (amateur)(2007)\n")
}

// symbols-new.txt is only read for lines whose symbol is printable, so a
// symbol off the end of the table can't come from there, but List would rather
// skip one than index past the end of a table.
func TestListSkipsSymbolsOffTheTable(t *testing.T) {
	var sd = new(Data)
	sd.newSymbols = []*newSymbol{
		{overlay: '/', symbol: 0x7f, description: "Off the primary table"},
		{overlay: 'A', symbol: 0x7f, description: "Off the overlaid table"},
		{overlay: '\\', symbol: 0x7f, description: "Off the alternate table"},
	}

	var output = testutils.CaptureOutput(t, sd.List)

	assert.NotContains(t, output, "Off the")
}
