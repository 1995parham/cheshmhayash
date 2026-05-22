// Package manifest defines the hand-editable YAML document that mediates
// between `natsie consumer scan` and `natsie consumer apply`.
//
// A manifest is a snapshot of which consumers a scan judged stale, with
// enough metadata for apply to re-verify before acting. Operators can edit
// it (delete rows, set skip: true, add a reason) before running apply.
package manifest

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Version is the manifest schema tag. Bump when fields change incompatibly.
const Version = "natsie/v1"

// Manifest is the top-level document.
type Manifest struct {
	Version     string    `yaml:"version"`
	GeneratedAt time.Time `yaml:"generated_at"`
	GeneratedBy string    `yaml:"generated_by,omitempty"`
	Scan        ScanInfo  `yaml:"scan"`
	Entries     []Entry   `yaml:"entries"`
}

// ScanInfo records the inputs that produced this manifest, so apply can
// reason about what "stale" meant when the snapshot was taken.
type ScanInfo struct {
	Context     string        `yaml:"context"`
	PeerContext string        `yaml:"peer_context,omitempty"`
	Stream      string        `yaml:"stream,omitempty"`
	MinPending  int64         `yaml:"min_pending,omitempty"`
	MinIdle     time.Duration `yaml:"min_idle,omitempty"`
}

// Entry is a single consumer marked for deletion. Skip is the operator
// hand-edit hook — set it to keep the row in the manifest but exclude it
// from apply.
type Entry struct {
	Cluster    string        `yaml:"cluster"`
	Stream     string        `yaml:"stream"`
	Consumer   string        `yaml:"consumer"`
	Status     string        `yaml:"status"`
	PeerStatus string        `yaml:"peer_status,omitempty"`
	NumPending int64         `yaml:"num_pending"`
	Idle       time.Duration `yaml:"idle,omitempty"`
	LastAck    time.Time     `yaml:"last_ack,omitempty"`
	Reason     string        `yaml:"reason,omitempty"`
	Skip       bool          `yaml:"skip,omitempty"`
}

// Read parses a YAML manifest from path.
func Read(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	if m.Version != Version {
		return nil, fmt.Errorf("unsupported manifest version %q (want %q)", m.Version, Version)
	}
	return &m, nil
}

// Write serializes the manifest to path. Refuses to overwrite an existing
// file unless force is true — apply produces no backup of the previous
// manifest, so a careless re-run shouldn't be able to clobber edits.
func (m *Manifest) Write(path string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	b, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write manifest %s: %w", path, err)
	}
	return nil
}
