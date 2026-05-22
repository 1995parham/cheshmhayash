// Package config loads natsie's configuration from a YAML file and
// environment variables, layered in that order.
//
// File: ~/.config/natsie/config.yaml (override with --config or NATSIE_CONFIG).
// Env:  any variable prefixed NATSIE_, with double-underscore as the section
// separator so single underscores can appear inside field names. For example
//
//	NATSIE_DEFAULTS__MIN_PENDING=10000
//	sets defaults.min_pending to 10000.
//
// Command-line flags are applied on top of this in the calling command (urfave
// CLI v3 manages flag state on the *cli.Command itself, so we layer it there
// rather than via a koanf flag provider).
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const envPrefix = "NATSIE_"

type Config struct {
	Defaults Defaults                  `koanf:"defaults"`
	Contexts map[string]ContextOptions `koanf:"contexts"`
	Bot      Bot                       `koanf:"bot"`
}

type Defaults struct {
	MinPending int64         `koanf:"min_pending"`
	MinIdle    time.Duration `koanf:"min_idle"`
	Format     string        `koanf:"format"`
}

// ContextOptions lets the user pre-declare cluster topology so the bot does
// not need a --peer-context flag on every invocation.
type ContextOptions struct {
	Peer string `koanf:"peer"`
}

// Bot holds everything `natsie bot serve` needs to run unattended. Each
// schedule produces manifests, the store persists them, the notify list
// announces them, and the HTTP listener exposes them.
type Bot struct {
	Schedules  []Schedule `koanf:"schedules"`
	Notify     []string   `koanf:"notify"`
	Owners     []Owner    `koanf:"owners"`
	Store      string     `koanf:"store"`
	AuditLog   string     `koanf:"audit_log"`
	HTTP       HTTP       `koanf:"http"`
	SigningKey string     `koanf:"signing_key"`
}

// Owner routes a subset of manifest entries to a team-specific notify list.
// An entry matches an owner if its stream appears in Streams (exact match)
// or its consumer name starts with one of ConsumerPrefix. Matching is
// first-hit, evaluated in the order the owners appear in config.
type Owner struct {
	Name           string   `koanf:"name"`
	Streams        []string `koanf:"streams"`
	ConsumerPrefix []string `koanf:"consumer_prefix"`
	Notify         []string `koanf:"notify"`
}

// Schedule is one recurring scan. Cron uses standard 5-field syntax
// (or @daily/@hourly/etc.).
type Schedule struct {
	Name        string        `koanf:"name"`
	Cron        string        `koanf:"cron"`
	Context     string        `koanf:"context"`
	PeerContext string        `koanf:"peer_context"`
	Stream      string        `koanf:"stream"`
	MinPending  int64         `koanf:"min_pending"`
	MinIdle     time.Duration `koanf:"min_idle"`
}

// HTTP configures the bot's listener used for the manifest viewer,
// slash-command webhook, and approval URLs.
type HTTP struct {
	Listen  string `koanf:"listen"`
	BaseURL string `koanf:"base_url"`
}

// Load reads the config file (if it exists) and overlays environment variables.
// An absent file is not an error — defaults stand in.
func Load(path string) (*Config, error) {
	if path == "" {
		path = defaultPath()
	}

	k := koanf.New(".")

	if path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
	}

	envProvider := env.Provider(".", env.Opt{
		Prefix: envPrefix,
		TransformFunc: func(key, val string) (string, any) {
			base := strings.ToLower(strings.TrimPrefix(key, envPrefix))

			return strings.ReplaceAll(base, "__", "."), val
		},
	})
	if err := k.Load(envProvider, nil); err != nil {
		return nil, fmt.Errorf("load env: %w", err)
	}

	cfg := &Config{
		Defaults: Defaults{
			MinIdle: 24 * time.Hour,
			Format:  "tsv",
		},
	}
	if err := k.Unmarshal("", cfg); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	return cfg, nil
}

func defaultPath() string {
	if p := os.Getenv("NATSIE_CONFIG"); p != "" {
		return p
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, ".config", "natsie", "config.yaml")
}
