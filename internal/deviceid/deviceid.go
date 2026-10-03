// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package deviceid identifies the vendor and model of the device that sent
// an APRS packet, from its destination address or MIC-E comment, using the
// tocalls.yaml tables from https://github.com/aprsorg/aprs-deviceid .
package deviceid

/*------------------------------------------------------------------
 *
 * Purpose:	Determine the device identifier from the destination field,
 *		or from prefix/suffix for MIC-E format.
 *
 * Description: Originally this used the tocalls.txt file and was part of decode_aprs.c.
 *		For release 1.8, we use tocalls.yaml and this is split into a separate file.
 *
 *------------------------------------------------------------------*/

import (
	"cmp"
	"io"
	"slices"
	"strings"

	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
	"go.yaml.in/yaml/v3"
)

// Structures to hold mapping from encoded form to vendor and model.
// The .yaml file has two separate sections for MIC-E but they can
// both be handled as a single more general case.

type mice struct {
	prefix string // The legacy form has 1 prefix character.
	// The newer form has none.  (more accurately ` or ')
	suffix string // The legacy form has 0 or 1.
	// The newer form has 2.
	vendor string
	model  string
}

type tocalls struct {
	tocall string // Up to 6 characters.  Some may have wildcards at the end.
	// Most often they are trailing "??" or "?" or "???" in one case.
	// Sometimes there is trailing "nnn".  Does that imply digits only?
	// Sometimes we see a trailing "*".  Is "*" different than "?"?
	// There are a couple bizarre cases like APnnnD which can
	// create an ambiguous situation. APMPAD, APRFGD, APY0[125]D.
	// Screw them if they can't follow the rules.  I'm not putting in a special case.
	vendor string
	model  string
}

// Data holds the loaded device identification tables.
type Data struct {
	pmice    []*mice
	ptocalls []*tocalls
}

/*------------------------------------------------------------------
 *
 * Function:	New
 *
 * Purpose:	Called once at startup to read the tocalls.yaml file which was obtained from
 *		https://github.com/aprsorg/aprs-deviceid .
 *
 * Inputs:	tocalls.yaml with OS specific directory search list.
 *
 * Returns:	Populated Data, or empty struct if file not found.
 *
 * Description:	For maximum flexibility, we will read the
 *		data file at run time rather than compiling it in.
 *
 *------------------------------------------------------------------*/

func New() *Data {
	var d = new(Data)

	var fp, openErr = dwutil.OpenDataFile("tocalls.yaml")
	if openErr != nil {
		logrus.WithError(openErr).
			Error("It won't be possible to extract device identifiers from packets.")

		return d
	}

	defer fp.Close()

	var data, readErr = io.ReadAll(fp)
	if readErr != nil {
		logrus.WithField("file", fp.Name()).WithError(readErr).Error("Error reading deviceid file")

		return d
	}

	// Some shenanigans to map this all to the right data types...
	// Could probably do something with fancy struct tagging etc. but this is at least better than parsing with strcmp

	var parsed, parseErr = FromYAML(data)
	if parseErr != nil {
		logrus.WithField("file", fp.Name()).WithError(parseErr).Error("Error parsing deviceid file")

		return d
	}

	return parsed
}

// FromYAML returns the tables held in data, the contents of a tocalls.yaml
// file, for a program that has them from somewhere other than a data file.
func FromYAML(data []byte) (*Data, error) {
	var d = new(Data)

	var deviceidConfig map[string]any

	var unmarshallErr = yaml.Unmarshal(data, &deviceidConfig)
	if unmarshallErr != nil {
		return d, unmarshallErr
	}

	var miceSection, _ = deviceidConfig["mice"].([]any)
	for _, _entry := range miceSection {
		var entry, _ = _entry.(map[string]any)
		var m = new(mice)

		m.suffix, _ = entry["suffix"].(string)
		m.vendor, _ = entry["vendor"].(string)
		m.model, _ = entry["model"].(string)

		d.pmice = append(d.pmice, m)
	}

	var micelegacySection, _ = deviceidConfig["micelegacy"].([]any)
	for _, _entry := range micelegacySection {
		var entry, _ = _entry.(map[string]any)
		var m = new(mice)

		m.prefix, _ = entry["prefix"].(string)
		m.suffix, _ = entry["suffix"].(string)
		m.vendor, _ = entry["vendor"].(string)
		m.model, _ = entry["model"].(string)

		d.pmice = append(d.pmice, m)
	}

	var tocallsSection, _ = deviceidConfig["tocalls"].([]any)
	for _, _entry := range tocallsSection {
		var entry, _ = _entry.(map[string]any)
		var t = new(tocalls)

		t.tocall, _ = entry["tocall"].(string)
		t.vendor, _ = entry["vendor"].(string)
		t.model, _ = entry["model"].(string)

		// Remove trailing wildcard characters
		t.tocall = strings.TrimRight(t.tocall, "?*n")

		d.ptocalls = append(d.ptocalls, t)
	}

	// MIC-E Legacy needs to be sorted so those with suffix come first.

	slices.SortFunc(d.pmice, func(a, b *mice) int {
		// Used to sort the suffixes by length.
		// Longer at the top.
		// Example check for  >xxx^ before >xxx .
		return cmp.Compare(len(b.suffix), len(a.suffix))
	})

	// Sort tocalls by decreasing length so the search will go from most specific to least specific.
	// Example:  APY350 or APY008 would match those specific models before getting to the more generic APY.

	slices.SortFunc(d.ptocalls, func(a, b *tocalls) int {
		// Used to sort the tocalls by length.
		// When length is equal, alphabetically.
		var c = cmp.Compare(len(b.tocall), len(a.tocall))
		if c != 0 {
			return c
		}

		return strings.Compare(a.tocall, b.tocall)
	})

	return d, nil
}

/*------------------------------------------------------------------
 *
 * Function:	FromDest
 *
 * Purpose:	Find vendor/model for destination address of form APxxxx.
 *
 * Inputs:	dest	- Destination address.  No SSID.
 *
 * Returns:	Vendor and model, or Nothing if they can't be identified.
 *
 * Description:	With the exception of MIC-E format, we expect to find the vendor/model in the
 *		AX.25 destination field.   The form should be APxxxx.
 *
 *		Search the list looking for the maximum length match.
 *		For example,
 *			APXR	= Xrouter
 *			APX	= Xastir
 *
 *------------------------------------------------------------------*/

func (d *Data) FromDest(dest string) maybe.Maybe[string] {
	if d == nil || len(d.ptocalls) == 0 {
		logrus.Trace("FromDest called without any deviceid data.")

		return maybe.Nothing[string]()
	}

	for _, t := range d.ptocalls {
		if strings.HasPrefix(dest, t.tocall) {
			return describe(t.vendor, t.model)
		}
	}

	// Not found in table.
	return maybe.Nothing[string]()
}

/*------------------------------------------------------------------
 *
 * Function:	FromMicE
 *
 * Purpose:	Find vendor/model for MIC-E comment.
 *
 * Inputs:	comment - MIC-E comment that might have vendor/model encoded as
 *			a prefix and/or suffix.
 *			Any trailing CR has already been removed.
 *
 * Returns:	trimmed - Final comment with device vendor/model removed.
 *				This would include any altitude.
 *
 *		device	- Vendor and model, or Nothing if they can't be identified.
 *
 * Description:	MIC-E device identification has a tortured history.
 *
 *		The Kenwood TH-D7A  put ">" at the beginning of the comment.
 *		The Kenwood TM-D700 put "]" at the beginning of the comment.
 *		Later Kenwood models also added a single suffix character
 *		using a character very unlikely to appear at the end of a comment.
 *
 *		The later convention, used by everyone else, is to have a prefix of ` or '
 *		and a suffix of two characters.  The suffix characters need to be
 *		something very unlikely to be found at the end of a comment.
 *
 *		A receiving device is expected to remove those extra characters
 *		before displaying the comment.
 *
 * References:	http://www.aprs.org/aprs12/mic-e-types.txt
 *		http://www.aprs.org/aprs12/mic-e-examples.txt
 *		https://github.com/wb2osz/aprsspec containing:
 *			APRS Protocol Specification 1.2
 *			Understanding APRS Packets
 *------------------------------------------------------------------*/

func (d *Data) FromMicE(comment string) (string, maybe.Maybe[string]) {
	if len(comment) < 1 {
		return comment, maybe.Nothing[string]()
	}

	if d == nil || len(d.pmice) == 0 {
		logrus.Trace("FromMicE called without any deviceid data.")

		return comment, maybe.Nothing[string]()
	}

	// The Legacy format has an explicit prefix in the table.
	// For others, it must be ` or ' to indicate whether messaging capable.

	for _, m := range d.pmice {
		if (len(m.prefix) != 0 && // Legacy
			strings.HasPrefix(comment, m.prefix) && // prefix from table
			strings.HasSuffix(comment, m.suffix)) || // possible suffix

			(len(m.prefix) == 0 && // Later
				(comment[0] == '`' || comment[0] == '\'') && // prefix ` or '
				strings.HasSuffix(comment, m.suffix)) { // suffix
			// Remove any prefix/suffix and return what remains.

			var trimmed = comment[1:]
			trimmed = trimmed[:len(trimmed)-len(m.suffix)]

			return trimmed, describe(m.vendor, m.model)
		}
	}

	// Not found.

	return comment, maybe.Nothing[string]()
}

// describe joins a table entry's vendor and model, either of which may be
// missing. An entry with neither doesn't identify anything.
func describe(vendor, model string) maybe.Maybe[string] {
	switch {
	case vendor != "" && model != "":
		return maybe.Just(vendor + " " + model)
	case vendor != "" || model != "":
		return maybe.Just(vendor + model)
	default:
		return maybe.Nothing[string]()
	}
}
