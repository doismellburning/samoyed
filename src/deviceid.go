package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:	Determine the device identifier from the destination field,
 *		or from prefix/suffix for MIC-E format.
 *
 * Description: Originally this used the tocalls.txt file and was part of decode_aprs.c.
 *		For release 1.8, we use tocalls.yaml and this is split into a separate file.
 *
 *------------------------------------------------------------------*/

// #include "direwolf.h"
// #include <stdlib.h>
// #include <stdio.h>
// #include <string.h>
// #include <assert.h>
// #include "textcolor.h"
import "C"

import (
	"cmp"
	"io"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
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

var pmice []*mice
var ptocalls []*tocalls

/*------------------------------------------------------------------
 *
 * Function:	deviceid_init
 *
 * Purpose:	Called once at startup to read the tocalls.yaml file which was obtained from
 *		https://github.com/aprsorg/aprs-deviceid .
 *
 * Inputs:	tocalls.yaml with OS specific directory search list.
 *
 * Outputs:	static variables listed above.
 *
 * Description:	For maximum flexibility, we will read the
 *		data file at run time rather than compiling it in.
 *
 *------------------------------------------------------------------*/

// If search order is changed, do the same in symbols.c for consistency.
// fopen is perfectly happy with / in file path when running on Windows.

var search_locations = []string{
	"tocalls.yaml",         // Current working directory
	"data/tocalls.yaml",    // Windows with CMake
	"../data/tocalls.yaml", // Source tree
	"/usr/local/share/direwolf/tocalls.yaml",
	"/usr/share/direwolf/tocalls.yaml",
	// https://groups.yahoo.com/neo/groups/direwolf_packet/conversations/messages/2458
	// Adding the /opt/local tree since macports typically installs there.  Users might want their
	// INSTALLDIR (see Makefile.macosx) to mirror that.  If so, then we need to search the /opt/local
	// path as well.
	"/opt/local/share/direwolf/tocalls.yaml",
}

func deviceid_init() {

	var fp *os.File
	for _, location := range search_locations {
		var err error
		fp, err = os.Open(location)

		if err == nil {
			defer fp.Close()
			break
		}
	}

	if fp == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Could not open any of these file locations:\n")
		for _, location := range search_locations {
			dw_printf("    %s\n", location)
		}
		dw_printf("It won't be possible to extract device identifiers from packets.\n")
		return
	}

	var data, readErr = io.ReadAll(fp)
	if readErr != nil {
		dw_printf("Error reading deviceid file %s: %s\n", fp.Name(), readErr)
		return
	}

	// Some shenanigans to map this all to the right data types...
	// Could probably do something with fancy struct tagging etc. but this is at least better than parsing with strcmp

	var deviceidConfig map[string]interface{}

	var unmarshallErr = yaml.Unmarshal(data, &deviceidConfig)
	if unmarshallErr != nil {
		dw_printf("Error parsing deviceid file %s: %s\n", fp.Name(), unmarshallErr)
		return
	}

	var miceSection, _ = deviceidConfig["mice"].([]interface{})
	for _, _entry := range miceSection {
		var entry, _ = _entry.(map[string]interface{})
		var m = new(mice)

		m.suffix, _ = entry["suffix"].(string)
		m.vendor, _ = entry["vendor"].(string)
		m.model, _ = entry["model"].(string)

		pmice = append(pmice, m)
	}

	var micelegacySection, _ = deviceidConfig["micelegacy"].([]interface{})
	for _, _entry := range micelegacySection {
		var entry, _ = _entry.(map[string]interface{})
		var m = new(mice)

		m.prefix, _ = entry["prefix"].(string)
		m.suffix, _ = entry["suffix"].(string)
		m.vendor, _ = entry["vendor"].(string)
		m.model, _ = entry["model"].(string)

		pmice = append(pmice, m)
	}

	var tocallsSection, _ = deviceidConfig["tocalls"].([]interface{})
	for _, _entry := range tocallsSection {
		var entry, _ = _entry.(map[string]interface{})
		var t = new(tocalls)

		t.tocall, _ = entry["tocall"].(string)
		t.vendor, _ = entry["vendor"].(string)
		t.model, _ = entry["model"].(string)

		// Remove trailing wildcard characters
		t.tocall = strings.TrimRight(t.tocall, "?*n")

		ptocalls = append(ptocalls, t)
	}

	// MIC-E Legacy needs to be sorted so those with suffix come first.

	slices.SortFunc(pmice, func(a, b *mice) int {
		// Used to sort the suffixes by length.
		// Longer at the top.
		// Example check for  >xxx^ before >xxx .
		return cmp.Compare(len(b.suffix), len(a.suffix))
	})

	// Sort tocalls by decreasing length so the search will go from most specific to least specific.
	// Example:  APY350 or APY008 would match those specific models before getting to the more generic APY.

	slices.SortFunc(ptocalls, func(a, b *tocalls) int {
		// Used to sort the tocalls by length.
		// When length is equal, alphabetically.
		var c = cmp.Compare(len(b.tocall), len(a.tocall))
		if c != 0 {
			return c
		}

		return strings.Compare(a.tocall, b.tocall)
	})
} // end deviceid_init

/*------------------------------------------------------------------
 *
 * Function:	deviceid_decode_dest
 *
 * Purpose:	Find vendor/model for destination address of form APxxxx.
 *
 * Inputs:	dest	- Destination address.  No SSID.
 *
 *		device_size - Amount of space available for result to avoid buffer overflow.
 *
 * Outputs:	device	- Vendor and model.
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

func deviceid_decode_dest(dest string) string {
	var device = "UNKNOWN vendor/model"

	if len(ptocalls) == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("deviceid_decode_dest called without any deviceid data.\n")
		return device
	}

	for _, t := range ptocalls {
		if strings.HasPrefix(dest, t.tocall) {

			if t.vendor != "" {
				device = t.vendor
			}

			if t.vendor != "" && t.model != "" {
				device += " "
			}

			if t.vendor == "" && t.model != "" {
				device = ""
			}

			if t.model != "" {
				device += t.model
			}

			return device
		}
	}

	// Not found in table.
	return "UNKNOWN vendor/model"

} // end deviceid_decode_dest

/*------------------------------------------------------------------
 *
 * Function:	deviceid_decode_mice
 *
 * Purpose:	Find vendor/model for MIC-E comment.
 *
 * Inputs:	comment - MIC-E comment that might have vendor/model encoded as
 *			a prefix and/or suffix.
 *			Any trailing CR has already been removed.
 *
 *		trimmed_size - Amount of space available for result to avoid buffer overflow.
 *
 *		device_size - Amount of space available for result to avoid buffer overflow.
 *
 * Outputs:	trimmed - Final comment with device vendor/model removed.
 *				This would include any altitude.
 *
 *		device	- Vendor and model.
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

func deviceid_decode_mice(comment string) (string, string) {
	var device = "UNKNOWN vendor/model"
	var trimmed = comment

	if len(comment) < 1 {
		return trimmed, device
	}

	if len(ptocalls) == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("deviceid_decode_mice called without any deviceid data.\n")
		return trimmed, device
	}

	// The Legacy format has an explicit prefix in the table.
	// For others, it must be ` or ' to indicate whether messaging capable.

	for _, m := range pmice {
		if (len(m.prefix) != 0 && // Legacy
			strings.HasPrefix(comment, m.prefix) && // prefix from table
			strings.HasSuffix(comment, m.suffix)) || // possible suffix

			(len(m.prefix) == 0 && // Later
				(comment[0] == '`' || comment[0] == '\'') && // prefix ` or '
				strings.HasSuffix(comment, m.suffix)) { // suffix

			if m.vendor != "" {
				device = m.vendor
			}

			if m.vendor != "" && m.model != "" {
				device += " "
			}

			if m.model != "" {
				device += m.model
			}

			// Remove any prefix/suffix and return what remains.

			trimmed = comment[1:]
			trimmed = trimmed[:len(trimmed)-len(m.suffix)]

			return trimmed, device
		}
	}

	// Not found.

	return comment, "UNKNOWN vendor/model"
}
