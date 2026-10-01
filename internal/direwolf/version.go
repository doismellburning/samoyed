package direwolf

import (
	"fmt"
	"runtime/debug"
	"strconv"
)

var SAMOYED_VERSION string //nolint:gochecknoglobals // Set at build time via `-ldflags "-X 'github.com/doismellburning/samoyed/internal/direwolf.SAMOYED_VERSION=X'"`

// MAJOR_VERSION and MINOR_VERSION exist because a bunch of things, both Dire Wolf and APRS,
// seem to expect two-part single-digit versions.
// This obviously doesn't interact well with my choice of CalVer...
// TODO Figure out what to do with MAJOR_VERSION etc.
const MAJOR_VERSION = 0
const MINOR_VERSION = 0

// APP_TOCALL is put in APRS destination field to identify the equipment used.
// Dire Wolf used APDW - "Assigned by WB4APR in tocalls.txt".
// KG 2026-01-19: Nobody has assigned SMYD, but I figured it was better to differentiate sooner rather than later.
const APP_TOCALL = "SMYD"

func getBuildSettingOrDefault(bi *debug.BuildInfo, key string, defaultValue string) string {
	for _, bs := range bi.Settings {
		if bs.Key == key {
			return bs.Value
		}
	}

	return defaultValue
}

func printVersion(verbose bool) {
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

	var version = SAMOYED_VERSION
	if version == "" {
		version = "!UNKNOWN!"
	}

	fmt.Printf("Samoyed - Version %s (revision %s, built at %s)\n", version, buildCommit, buildTimeStr)

	if verbose {
		fmt.Printf("\nBuildInfo: %+v\n", buildInfo)
	}
}
