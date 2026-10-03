// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/doismellburning/samoyed/data"
	"github.com/doismellburning/samoyed/internal/aprs"
	"github.com/doismellburning/samoyed/internal/deviceid"
	"github.com/doismellburning/samoyed/internal/symbols"
)

// newDecoder returns a Decoder with the compiled-in data tables, there being
// no data files to read in a browser.
func newDecoder() *aprs.Decoder {
	var deviceIDs, err = deviceid.FromYAML(data.TocallsYAML)
	if err != nil {
		panic(fmt.Sprintf("compiled-in tocalls.yaml does not parse: %v", err))
	}

	return aprs.NewDecoder(deviceIDs, symbols.FromReader(bytes.NewReader(data.SymbolsNew)))
}

// Decode prints a description of each packet in text, one per line, as
// samoyed-decode_aprs does: blank lines and # comments are echoed back.
func Decode(d *aprs.Decoder, text string) {
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\r\n")

		if line == "" || strings.HasPrefix(line, "#") {
			fmt.Println(line)

			continue
		}

		d.DescribeLine(line)
	}
}
