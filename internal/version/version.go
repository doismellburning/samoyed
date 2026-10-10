// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package version

import (
	"fmt"
	"runtime/debug"
	"strconv"
)

// Version is Samoyed's version, a CalVer string.
var Version string //nolint:gochecknoglobals // Set at build time via `-ldflags "-X 'github.com/doismellburning/samoyed/internal/version.Version=X'"`

// Major and Minor exist because a bunch of things, both Dire Wolf and APRS,
// seem to expect two-part single-digit versions.
// This obviously doesn't interact well with my choice of CalVer...
// TODO Figure out what to do with Major etc.
const Major = 0
const Minor = 0

// Tocall is put in APRS destination field to identify the equipment used.
// Dire Wolf used APDW - "Assigned by WB4APR in tocalls.txt".
// KG 2026-01-19: Nobody has assigned SMYD, but I figured it was better to differentiate sooner rather than later.
const Tocall = "SMYD"

func getBuildSettingOrDefault(bi *debug.BuildInfo, key string, defaultValue string) string {
	for _, bs := range bi.Settings {
		if bs.Key == key {
			return bs.Value
		}
	}

	return defaultValue
}

// Print prints Samoyed's version, VCS revision and build time to stdout,
// and with verbose the whole of the Go BuildInfo too.
func Print(verbose bool) {
	var buildInfo, _ = debug.ReadBuildInfo()

	// TODO KG Allow overriding by env var for reproducible builds? Or does Go support this already?
	var buildTimeStr = getBuildSettingOrDefault(buildInfo, "vcs.time", "UNKNOWN")

	var (
		buildCommit               = getBuildSettingOrDefault(buildInfo, "vcs.revision", "UNKNOWN")
		buildDirtyStr             = getBuildSettingOrDefault(buildInfo, "vcs.modified", "INVALID")
		buildDirty, buildDirtyErr = strconv.ParseBool(buildDirtyStr)
	)

	if buildDirty {
		buildCommit += "-DIRTY"
	} else if buildDirtyErr != nil {
		fmt.Printf("Error parsing vcs.modified, got %s, %s\n", buildDirtyStr, buildDirtyErr)

		buildCommit += "-UNKNOWNDIRTY"
	}

	var version = Version
	if version == "" {
		version = "!UNKNOWN!"
	}

	fmt.Printf("Samoyed - Version %s (revision %s, built at %s)\n", version, buildCommit, buildTimeStr)

	if verbose {
		fmt.Printf("\nBuildInfo: %+v\n", buildInfo)
	}
}
