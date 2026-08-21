// Package protect decides which consumers natsie must never delete because
// something else owns their lifecycle.
//
// The motivating case is a Kubernetes operator such as NACK, whose CRDs
// declare consumers and whose reconciler enforces that declaration. Deleting
// such a consumer out of band produces one of two failures, both of which
// only bite while nobody is watching:
//
//   - with reconciliation enabled, the operator recreates the consumer, the
//     next scan finds it idle again, and natsie deletes it again — an endless
//     delete/recreate loop that resets the ack floor every round;
//   - with reconciliation disabled, the delete sticks and the cluster silently
//     diverges from the declared state until some unrelated future reconcile
//     brings the consumer back.
//
// The same reasoning covers Terraform, Pulumi, and any GitOps pipeline, so
// the rules here are deliberately not NACK-specific: a consumer is protected
// when it carries one of MetadataKeys, or when it matches one of Patterns.
//
// NACK does not stamp a marker of its own — its controller forwards only the
// metadata set on the CR — so the metadata rule is opt-in: add the key to the
// Consumer CR's `spec.metadata` and natsie will leave it alone. Patterns cover
// consumers you cannot or do not want to annotate.
package protect

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// DefaultMetadataKey is the metadata key natsie honours without configuration.
// Setting it on a NACK Consumer CR's spec.metadata is the cheapest way to make
// a declaratively-managed consumer permanently undeletable by natsie.
const DefaultMetadataKey = "natsie.io/managed"

// Pattern matches a (stream, consumer) pair. Both fields are shell-style
// globs as understood by path.Match; an empty field matches anything.
type Pattern struct {
	Stream   string `koanf:"stream"   yaml:"stream,omitempty"`
	Consumer string `koanf:"consumer" yaml:"consumer,omitempty"`
}

// Config is the operator-supplied rule set.
type Config struct {
	// MetadataKeys are consumer-metadata keys that mark a consumer as owned
	// elsewhere. DefaultMetadataKey is always honoured in addition to these.
	MetadataKeys []string `koanf:"metadata_keys" yaml:"metadata_keys,omitempty"`
	// Patterns protect consumers by name, for consumers whose metadata you
	// cannot set.
	Patterns []Pattern `koanf:"patterns" yaml:"patterns,omitempty"`
}

// Protector answers "may natsie delete this?". The zero value is usable and
// still honours DefaultMetadataKey, so callers that skip configuration are
// protected against annotated consumers by default rather than by accident.
type Protector struct {
	keys     []string
	patterns []Pattern
}

// New validates the rule set and returns a Protector. Invalid globs are
// rejected here rather than silently failing to match at delete time — a
// protection rule that never fires is worse than no rule at all.
func New(cfg Config) (*Protector, error) {
	keys := []string{DefaultMetadataKey}

	for _, k := range cfg.MetadataKeys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}

		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}

	for i, p := range cfg.Patterns {
		if p.Stream == "" && p.Consumer == "" {
			return nil, fmt.Errorf("protect.patterns[%d]: needs at least one of stream/consumer", i)
		}

		for _, g := range []string{p.Stream, p.Consumer} {
			if g == "" {
				continue
			}

			if _, err := path.Match(g, ""); err != nil {
				return nil, fmt.Errorf("protect.patterns[%d]: bad glob %q: %w", i, g, err)
			}
		}
	}

	return &Protector{keys: keys, patterns: slices.Clone(cfg.Patterns)}, nil
}

// falsey lists the metadata values that explicitly opt *out* of protection,
// so `natsie.io/managed: "false"` in a CR reads the way an operator expects
// rather than protecting on mere presence of the key.
var falsey = []string{"false", "no", "0", "off"} //nolint:gochecknoglobals // fixed lookup table

// Reason returns a human-readable explanation when the consumer is protected,
// or "" when natsie may delete it. meta is the consumer's live JetStream
// metadata and may be nil.
//
// A nil *Protector protects nothing, so callers holding an optional Protector
// need no nil check.
func (p *Protector) Reason(stream, consumer string, meta map[string]string) string {
	if p == nil {
		return ""
	}

	keys := p.keys
	if len(keys) == 0 {
		keys = []string{DefaultMetadataKey}
	}

	for _, k := range keys {
		v, ok := meta[k]
		if !ok {
			continue
		}

		if slices.Contains(falsey, strings.ToLower(strings.TrimSpace(v))) {
			continue
		}

		return fmt.Sprintf("declaratively managed (metadata %s=%s)", k, v)
	}

	for _, pat := range p.patterns {
		if matches(pat.Stream, stream) && matches(pat.Consumer, consumer) {
			return fmt.Sprintf("protected by pattern %s", pat)
		}
	}

	return ""
}

// Protected is Reason reduced to a bool, for call sites that only branch.
func (p *Protector) Protected(stream, consumer string, meta map[string]string) bool {
	return p.Reason(stream, consumer, meta) != ""
}

func (p Pattern) String() string {
	stream, consumer := p.Stream, p.Consumer
	if stream == "" {
		stream = "*"
	}

	if consumer == "" {
		consumer = "*"
	}

	return stream + "/" + consumer
}

// matches reports whether glob matches name. An empty glob is a wildcard.
// A malformed glob cannot reach here — New rejects those — so a match error
// is treated as "no match".
func matches(glob, name string) bool {
	if glob == "" {
		return true
	}

	ok, err := path.Match(glob, name)

	return err == nil && ok
}
