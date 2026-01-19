package direwolf

import (
	"fmt"
	"runtime/debug"
	"strconv"
)

// Set at build time via `-ldflags "-X 'direwolf.SAMOYED_VERSION=X'"`
var SAMOYED_VERSION string

// Put in APRS destination field to identify the equipment used.
// Dire Wolf used APDW - "Assigned by WB4APR in tocalls.txt".
// KG 2026-01-19: Nobody has assigned SMYD, but I figured it was better to differentiate sooner rather than later.
const APP_TOCALL = "SMYD"

// For user-defined data format.
// APRS protocol spec Chapter 18 and http://www.aprs.org/aprs11/expfmts.txt

// KG 2026-01-19: Dire Wolf has D reserved per
// https://www.aprs.org/aprs11/expfmts.txt and there seems a lot less space for
// me to comfortably just DIY like with APP_TOCALL (and S is already assigned).
// So I'll stick with D for now as it seems the least-worst option.
const USER_DEF_USER_ID = 'D'

const USER_DEF_TYPE_AIS = 'A' // data type A for AIS NMEA sentence
const USER_DEF_TYPE_EAS = 'E' // data type E for EAS broadcasts

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
