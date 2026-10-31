// Package version holds build-time version information.
package version

// Version is set at build time with -ldflags. It intentionally defaults to a
// development marker for `go run` builds.
var Version = "dev"

// UserAgent identifies CatTV to image hosts. It contains no user identifiers.
func UserAgent() string {
	return "cattv/" + Version
}
