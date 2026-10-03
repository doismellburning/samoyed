// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package data compiles in the data files for a program with no filesystem
// to read them from at run time, such as the in-browser APRS tool.  Everything
// else finds them on disk with dwutil.OpenDataFile, so they can be updated
// without a rebuild.
package data

import _ "embed"

// TocallsYAML is tocalls.yaml, for deviceid.FromYAML.
//
//go:embed tocalls.yaml
var TocallsYAML []byte

// SymbolsNew is symbols-new.txt, for symbols.FromReader.
//
//go:embed symbols-new.txt
var SymbolsNew []byte
