// Package config loads runtime settings via koanf in the layered pattern:
// struct defaults → settings.toml → CHESHMHAYASH__* environment variables.
//
// Env keys use `__` as the path separator (e.g. CHESHMHAYASH__SERVER__PORT).
// `[]NATS` entries are addressed by their array index — koanf can't merge a
// numeric-keyed env map into a TOML-loaded slice, so slice paths are
// overlaid manually after the struct is unmarshalled.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
	"github.com/tidwall/pretty"
)

const (
	defaultRequestTimeout   = 2 * time.Second
	defaultDiscoveryTimeout = 500 * time.Millisecond
	envPrefix               = "CHESHMHAYASH__"
	configFile              = "settings.toml"
)

type Settings struct {
	Server Server   `json:"server"           koanf:"server"`
	NATS   []NATS   `json:"nats"             koanf:"nats"`
	Notify []Notify `json:"notify,omitempty" koanf:"notify"`
}

// Notify describes one outbound chat webhook. The provider field selects
// the JSON shape and target API.
type Notify struct {
	Provider string `json:"provider"           koanf:"provider"` // slack | mattermost | matrix
	URL      string `json:"-"                  koanf:"url"`      // redacted in startup log
	Channel  string `json:"channel,omitempty"  koanf:"channel"`
	Username string `json:"username,omitempty" koanf:"username"`
}

type Server struct {
	Host string `json:"host" koanf:"host"`
	Port int    `json:"port" koanf:"port"`
}

type NATS struct {
	Name      string `json:"name"                 koanf:"name"`
	URL       string `json:"url"                  koanf:"url"`
	CredsFile string `json:"creds_file,omitempty" koanf:"creds_file"`
	User      string `json:"user,omitempty"       koanf:"user"`
	// Password is redacted on the startup log; the JSON tag matters only
	// for that single pretty-print.
	Password           string `json:"-"                                  koanf:"password"`
	RequestTimeoutMS   int    `json:"request_timeout_ms,omitempty"       koanf:"request_timeout_ms"`
	DiscoveryTimeoutMS int    `json:"discovery_timeout_ms,omitempty"     koanf:"discovery_timeout_ms"`
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

// Load layers defaults → TOML → env. The returned *Settings is fully
// validated; callers can use it without further checks.
func Load() (*Settings, error) {
	k := koanf.New(".")

	if err := k.Load(structs.Provider(Default(), "koanf"), nil); err != nil {
		return nil, fmt.Errorf("load defaults: %w", err)
	}

	if _, err := os.Stat(configFile); err == nil {
		if lerr := k.Load(file.Provider(configFile), toml.Parser()); lerr != nil {
			return nil, fmt.Errorf("load %s: %w", configFile, lerr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat %s: %w", configFile, err)
	}

	// Unmarshal *before* applying env vars. koanf's env provider stores
	// `nats.<i>.<key>` as a numeric-keyed map and the internal merge
	// clobbers the TOML-loaded slice. We do env via the koanf env loader
	// for scalar paths (server.port, etc.) and via a manual overlay for
	// slice paths (nats[i].<field>).
	var s Settings
	if err := k.Unmarshal("", &s); err != nil {
		return nil, fmt.Errorf("unmarshal settings: %w", err)
	}

	if err := overlayEnv(&s); err != nil {
		return nil, fmt.Errorf("env overlay: %w", err)
	}

	if err := validate(&s); err != nil {
		return nil, err
	}

	logLoaded(&s)
	return &s, nil
}

func validate(s *Settings) error {
	if len(s.NATS) == 0 {
		return errors.New("no NATS clusters configured")
	}
	for i, n := range s.NATS {
		if n.Name == "" {
			return fmt.Errorf("nats[%d]: name is required", i)
		}
		if n.URL == "" {
			return fmt.Errorf("nats[%d] (%s): url is required", i, n.Name)
		}
	}
	return nil
}

// overlayEnv walks CHESHMHAYASH__* env vars and applies them onto Settings.
// Scalar paths (server.host, server.port) and slice paths (nats[i].<field>)
// are both handled here so behavior is uniform across the schema.
func overlayEnv(s *Settings) error {
	for _, kv := range os.Environ() {
		key, val, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(key, envPrefix) {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(key, envPrefix), "__")
		if err := applyEnvPath(s, parts, val); err != nil {
			return fmt.Errorf("env %s: %w", key, err)
		}
	}
	return nil
}

func applyEnvPath(s *Settings, parts []string, val string) error {
	if len(parts) == 0 {
		return nil
	}
	switch strings.ToLower(parts[0]) {
	case "server":
		if len(parts) != 2 {
			return fmt.Errorf("expected server.<key>")
		}
		switch strings.ToLower(parts[1]) {
		case "host":
			s.Server.Host = val
		case "port":
			n, err := strconv.Atoi(val)
			if err != nil {
				return err
			}
			s.Server.Port = n
		default:
			return fmt.Errorf("unknown server key %q", parts[1])
		}
	case "nats":
		if len(parts) < 3 {
			return fmt.Errorf("expected nats.<index>.<key>")
		}
		idx, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("nats index must be numeric")
		}
		for len(s.NATS) <= idx {
			s.NATS = append(s.NATS, NATS{})
		}
		return setNATSField(&s.NATS[idx], strings.ToLower(parts[2]), val)
	case "notify":
		if len(parts) < 3 {
			return fmt.Errorf("expected notify.<index>.<key>")
		}
		idx, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("notify index must be numeric")
		}
		for len(s.Notify) <= idx {
			s.Notify = append(s.Notify, Notify{})
		}
		return setNotifyField(&s.Notify[idx], strings.ToLower(parts[2]), val)
	}
	return nil
}

func setNotifyField(n *Notify, key, val string) error {
	switch key {
	case "provider":
		n.Provider = val
	case "url":
		n.URL = val
	case "channel":
		n.Channel = val
	case "username":
		n.Username = val
	default:
		return fmt.Errorf("unknown notify field %q", key)
	}
	return nil
}

func setNATSField(n *NATS, key, val string) error {
	rv := reflect.ValueOf(n).Elem()
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("koanf")
		if tag != key {
			continue
		}
		fv := rv.Field(i)
		switch fv.Kind() {
		case reflect.String:
			fv.SetString(val)
		case reflect.Int, reflect.Int64:
			n, err := strconv.Atoi(val)
			if err != nil {
				return fmt.Errorf("field %q: %w", key, err)
			}
			fv.SetInt(int64(n))
		default:
			return fmt.Errorf("field %q: unsupported kind %s", key, fv.Kind())
		}
		return nil
	}
	return fmt.Errorf("unknown nats field %q", key)
}

func logLoaded(s *Settings) {
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		slog.Default().Warn("could not marshal settings for log", "err", err)
		return
	}
	// tidwall/pretty adds ANSI colors for human-friendly tail-the-log
	// inspection. Logs shipped to a JSON aggregator strip the codes.
	colored := pretty.Color(out, nil)
	fmt.Fprintf(os.Stderr, "\n================ loaded configuration ================\n%s\n=======================================================\n\n", colored)
}
