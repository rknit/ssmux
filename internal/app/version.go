package app

import (
	"runtime/debug"
)

// Version and Commit are set at build time with
// -ldflags "-X github.com/rknit/ssmux/internal/app.Version=... -X ...Commit=...".
// When unset, they fall back to the module and VCS info embedded by go build.
var (
	Version = ""
	Commit  = ""
)

// versionString returns "ssmux <version> (<commit>)".
func versionString(info *debug.BuildInfo, ok bool) string {
	version, commit := Version, Commit
	if ok {
		if version == "" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
		if commit == "" {
			var dirty bool
			for _, s := range info.Settings {
				switch s.Key {
				case "vcs.revision":
					commit = s.Value
				case "vcs.modified":
					dirty = s.Value == "true"
				}
			}
			if commit != "" && dirty {
				commit += "-dirty"
			}
		}
	}
	if version == "" {
		version = "dev"
	}
	if commit == "" {
		commit = "unknown"
	}
	return "ssmux " + version + " (" + commit + ")"
}
