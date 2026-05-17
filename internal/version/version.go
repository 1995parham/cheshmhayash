// Package version exposes the cheshmhayash binary version to other
// subsystems (HTTP /api/version, MCP initialize handshake, etc.).
//
// The value is resolved at startup with the following precedence:
//
//  1. Version, when set at link time via -ldflags
//     `-X github.com/1995parham/cheshmhayash/internal/version.Version=v1.2.3`
//     (release builds inject the pushed git tag this way).
//  2. The Go module version from runtime/debug.ReadBuildInfo, which is
//     populated for binaries installed via `go install module@version`.
//  3. "dev" — local `go run .` and plain `go build .` without ldflags
//     fall through to this.
package version

import "runtime/debug"

// Version is the linker-overridable build version. Default empty so the
// runtime fallbacks decide; release builds set this to the pushed tag.
var Version = ""

// String returns the resolved binary version. Safe to call repeatedly —
// the lookup is cheap and idempotent.
func String() string {
	if Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
