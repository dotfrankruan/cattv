// Package version holds build-time version information.
package version

import "runtime/debug"

// Version is set at build time with -ldflags. It intentionally defaults to a
// development marker for `go run` builds and for `go install` builds where no
// linker flags are supplied.
var Version = "dev"

// Current returns the linker-injected version when present, otherwise the
// module version embedded by `go install module@version`.
func Current() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	if Version == "" {
		return "dev"
	}
	return Version
}

// UserAgent identifies CatTV to image hosts. It contains no user identifiers.
func UserAgent() string {
	return "cattv/" + Current() + " (+https://github.com/dotfrankruan/cattv)"
}
