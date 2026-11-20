// Package store persists manifests so the HTTP listener and slash-command
// handler can refer back to them by ID. Each implementation is selected by
// URL scheme:
//
//	file:///var/lib/natsie/manifests  — local filesystem (default)
//	s3://bucket/prefix                — planned
//
// The Store contract is intentionally small. Manifests are versioned by
// the manifest package itself; the store does not interpret contents.
package store

import (
	"context"
	"fmt"
	neturl "net/url"

	"github.com/1995parham/natsie/internal/manifest"
)

// Store persists manifests by an opaque ID assigned by the caller.
// Implementations must be safe for concurrent use.
type Store interface {
	Put(ctx context.Context, id string, m *manifest.Manifest) error
	Get(ctx context.Context, id string) (*manifest.Manifest, error)
	List(ctx context.Context) ([]string, error)
	Delete(ctx context.Context, id string) error
	Name() string
}

// Dial parses url and returns the Store implementation that matches the
// scheme.
func Dial(url string) (Store, error) {
	u, err := neturl.Parse(url)
	if err != nil {
		return nil, fmt.Errorf("parse store url %q: %w", url, err)
	}

	switch u.Scheme {
	case "file":
		return NewFile(u.Path)
	default:
		return nil, fmt.Errorf("store scheme %q is not registered (try file:///path)", u.Scheme)
	}
}
