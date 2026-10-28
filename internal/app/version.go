package app

import (
	"regexp"
	"runtime/debug"
)

// Version is set at build time with
// -ldflags "-X github.com/rknit/ssmux/internal/app.Version=...".
// When unset, it falls back to the module version embedded by go build.
var Version = ""

var (
	releaseVersion = regexp.MustCompile(`^v?(\d+\.\d+\.\d+)$`)
	pseudoVersion  = regexp.MustCompile(`-([0-9a-f]{12})$`)
)

// versionString returns "ssmux vX.Y.Z" for releases, otherwise
// "ssmux dev-<hash>[-dirty]" (or "ssmux dev" when no commit is known).
func versionString(info *debug.BuildInfo, ok bool) string {
	version := Version
	if ok && version == "" {
		version = info.Main.Version
	}
	if m := releaseVersion.FindStringSubmatch(version); m != nil {
		return "ssmux v" + m[1]
	}
	var hash string
	var dirty bool
	if ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				hash = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if m := pseudoVersion.FindStringSubmatch(info.Main.Version); hash == "" && m != nil {
			hash = m[1]
		}
	}
	if len(hash) > 7 {
		hash = hash[:7]
	}
	if hash == "" {
		return "ssmux dev"
	}
	if dirty {
		hash += "-dirty"
	}
	return "ssmux dev-" + hash
}
