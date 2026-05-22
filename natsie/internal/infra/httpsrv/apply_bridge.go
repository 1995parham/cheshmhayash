package httpsrv

import (
	"context"

	"github.com/1995parham/natsie/internal/cleanup"
	"github.com/1995parham/natsie/internal/manifest"
)

// applyManifest is a tiny indirection so tests can stub out cleanup.Apply
// without standing up a real NATS server. Kept as a var (not a const fn)
// because tests rebind it; production never mutates it.
//
//nolint:gochecknoglobals // documented testing seam, not configuration
var applyManifest = func(ctx context.Context, m *manifest.Manifest, c cleanup.Connector) (*cleanup.Result, error) {
	return cleanup.Apply(ctx, m, false, c)
}
