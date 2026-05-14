// Package config loads runtime settings via koanf. Sources stack in order
// of increasing precedence:
//
//  1. config/default.toml       (shipped — required)
//  2. settings.toml             (operator override — optional)
//  3. CHESHMHAYASH__* env vars  (CI / Kubernetes overrides)
//
// Env keys use `__` as the path separator (e.g. CHESHMHAYASH__SERVER__PORT).
// List entries are indexed (e.g. CHESHMHAYASH__NATS__0__USER=admin).
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/toml/v2"
	envprovider "github.com/knadh/koanf/providers/env/v2"
	fileprovider "github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const (
	defaultRequestTimeout   = 2 * time.Second
	defaultDiscoveryTimeout = 500 * time.Millisecond
	envPrefix               = "CHESHMHAYASH__"
	defaultPath             = "config/default.toml"
	overridePath            = "settings.toml"
)

type Settings struct {
	Server Server `koanf:"server"`
	NATS   []NATS `koanf:"nats"`
}

type Server struct {
	Host string `koanf:"host"`
	Port int    `koanf:"port"`
}

type NATS struct {
	Name               string `koanf:"name"`
	URL                string `koanf:"url"`
	CredsFile          string `koanf:"creds_file"`
	User               string `koanf:"user"`
	Password           string `koanf:"password"`
	RequestTimeoutMS   int    `koanf:"request_timeout_ms"`
	DiscoveryTimeoutMS int    `koanf:"discovery_timeout_ms"`
}

func (n NATS) RequestTimeout() time.Duration {
	if n.RequestTimeoutMS <= 0 {
		return defaultRequestTimeout
	}
	return time.Duration(n.RequestTimeoutMS) * time.Millisecond
}

func (n NATS) DiscoveryTimeout() time.Duration {
	if n.DiscoveryTimeoutMS <= 0 {
		return defaultDiscoveryTimeout
	}
	return time.Duration(n.DiscoveryTimeoutMS) * time.Millisecond
}

func (s Server) Addr() string { return fmt.Sprintf("%s:%d", s.Host, s.Port) }

// Load reads default + override TOML files and overlays env vars.
func Load() (*Settings, error) {
	// `.` matches the TOML structure (nested tables → nested maps); koanf
	// rewrites env vars from `CHESHMHAYASH__SERVER__PORT` to `server.port`
	// via the transform below.
	k := koanf.New(".")

	if err := k.Load(fileprovider.Provider(defaultPath), toml.Parser()); err != nil {
		return nil, fmt.Errorf("load %s: %w", defaultPath, err)
	}

	if _, err := os.Stat(overridePath); err == nil {
		if lerr := k.Load(fileprovider.Provider(overridePath), toml.Parser()); lerr != nil {
			return nil, fmt.Errorf("load %s: %w", overridePath, lerr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat %s: %w", overridePath, err)
	}

	envProv := envprovider.Provider(".", envprovider.Opt{
		Prefix: envPrefix,
		TransformFunc: func(key, value string) (string, any) {
			key = strings.TrimPrefix(key, envPrefix)
			key = strings.ToLower(strings.ReplaceAll(key, "__", "."))
			return key, value
		},
	})
	if err := k.Load(envProv, nil); err != nil {
		return nil, fmt.Errorf("load env: %w", err)
	}

	var s Settings
	if err := k.Unmarshal("", &s); err != nil {
		return nil, fmt.Errorf("unmarshal settings: %w", err)
	}

	if len(s.NATS) == 0 {
		return nil, errors.New("no NATS clusters configured")
	}
	for i, n := range s.NATS {
		if n.Name == "" {
			return nil, fmt.Errorf("nats[%d]: name is required", i)
		}
		if n.URL == "" {
			return nil, fmt.Errorf("nats[%d] (%s): url is required", i, n.Name)
		}
	}
	return &s, nil
}
