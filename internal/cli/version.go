package cli

import "runtime/debug"

// version is set by releases with
// -ldflags "-X github.com/ekalinin/metagit/internal/cli.version=<version>".
var version = "dev"

// versionString returns the version and the commit recorded by go build,
// for example "0.1.0 (3dc143a)" or "dev (3dc143a, modified)".
func versionString() string {
	var revision string
	var modified bool
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
	}
	return formatVersion(version, revision, modified)
}

func formatVersion(version, revision string, modified bool) string {
	if revision == "" {
		return version
	}
	if len(revision) > 7 {
		revision = revision[:7]
	}
	if modified {
		return version + " (" + revision + ", modified)"
	}
	return version + " (" + revision + ")"
}
