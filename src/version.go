package direwolf

import (
	"fmt"
	"runtime/debug"
	"strconv"
)

// Set at build time via `-ldflags "-X 'direwolf.SAMOYED_VERSION=X'"`
var SAMOYED_VERSION string

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
