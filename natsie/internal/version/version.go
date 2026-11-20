// Package version exposes the release tag stamped into the binary at
// build time via -ldflags "-X github.com/1995parham/natsie/internal/version.Version=vX.Y.Z".
//
// Local `go build` leaves Version as "dev"; the CI Docker image build
// passes the pushed tag through --build-arg VERSION=$TAG and bakes it
// into the binary. Falls back to carlmjohnson/versioninfo's VCS info
// when ldflag is unset so `natsie --version` always reports something.
package version

import "github.com/carlmjohnson/versioninfo"

// Version is overridden at link time. Do not change at runtime.
//
//nolint:gochecknoglobals // overridden by ldflags
var Version = "dev"

// Short returns the release tag if set, otherwise the VCS short hash.
func Short() string {
	if Version != "" && Version != "dev" {
		return Version
	}

	return versioninfo.Short()
}
