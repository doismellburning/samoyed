// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package dwutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// OpenDataFile opens one of the data files Dire Wolf ships in data/, such as
// tocalls.yaml or symbols-new.txt, from the first of the usual places it is
// found. The error names every location tried.
func OpenDataFile(name string) (*os.File, error) {
	var dirs = []string{
		".",          // Current working directory
		"data",       // Windows with CMake
		"../../data", // Source tree, e.g. running tests from internal/<pkg>/ or cmd/<name>/
		"/usr/local/share/direwolf",
		"/usr/share/direwolf",
		// https://groups.yahoo.com/neo/groups/direwolf_packet/conversations/messages/2458
		// Adding the /opt/local tree since macports typically installs there.  Users might want their
		// INSTALLDIR (see Makefile.macosx) to mirror that.  If so, then we need to search the /opt/local
		// path as well.
		"/opt/local/share/direwolf",
	}

	var searched = make([]string, 0, len(dirs))

	for _, dir := range dirs {
		var location = filepath.Join(dir, name)

		var fp, err = os.Open(location) //nolint:gosec // G304: the directories are ours; name comes from our callers, not user input
		if err == nil {
			return fp, nil
		}

		searched = append(searched, location)
	}

	return nil, fmt.Errorf("could not open %s from any of %v", name, searched)
}
