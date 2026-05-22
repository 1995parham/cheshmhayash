// Package owners routes manifest entries to a service team's notify list.
//
// A Router is built once at startup from the bot's owner config, then
// asked which owner (if any) is responsible for each entry. Match
// strategy: first owner whose Streams contains the entry's stream OR
// whose ConsumerPrefix contains a prefix of the entry's consumer name
// claims it. First match wins — the operator's owner order is meaningful.
//
// Entries that match no owner are returned under the empty-string key
// from Group; callers fall those back to their global notify list.
package owners

import (
	"fmt"
	"strings"

	"github.com/1995parham/natsie/internal/infra/config"
	"github.com/1995parham/natsie/internal/manifest"
)

// Owner is the internal form of config.Owner — same shape, lifted into
// this package so callers don't depend on config types directly.
type Owner struct {
	Name           string
	Streams        []string
	ConsumerPrefix []string
}

// Router holds the compiled owner list.
type Router struct {
	owners []Owner
}

// NewRouter validates and returns a Router from a slice of config.Owner.
// An owner with no Streams and no ConsumerPrefix is rejected — it would
// silently claim nothing, which is almost always a misconfiguration.
func NewRouter(cfgs []config.Owner) (*Router, error) {
	r := &Router{}
	for i, c := range cfgs {
		if c.Name == "" {
			return nil, fmt.Errorf("owner[%d]: name is required", i)
		}
		if len(c.Streams) == 0 && len(c.ConsumerPrefix) == 0 {
			return nil, fmt.Errorf("owner %q: at least one of streams or consumer_prefix is required", c.Name)
		}
		r.owners = append(r.owners, Owner{
			Name:           c.Name,
			Streams:        c.Streams,
			ConsumerPrefix: c.ConsumerPrefix,
		})
	}
	return r, nil
}

// Match returns the name of the first owner that claims the entry, or
// "" if no owner matches.
func (r *Router) Match(e manifest.Entry) string {
	for _, o := range r.owners {
		for _, s := range o.Streams {
			if e.Stream == s {
				return o.Name
			}
		}
		for _, p := range o.ConsumerPrefix {
			if p != "" && strings.HasPrefix(e.Consumer, p) {
				return o.Name
			}
		}
	}
	return ""
}

// Group partitions entries by their matched owner name. Unmatched entries
// land under "". An empty Router puts everything under "".
func (r *Router) Group(entries []manifest.Entry) map[string][]manifest.Entry {
	out := map[string][]manifest.Entry{}
	for _, e := range entries {
		owner := r.Match(e)
		out[owner] = append(out[owner], e)
	}
	return out
}

// Names returns the configured owner names in declaration order. Useful
// for deterministic iteration over Group's keys.
func (r *Router) Names() []string {
	names := make([]string, len(r.owners))
	for i, o := range r.owners {
		names[i] = o.Name
	}
	return names
}
