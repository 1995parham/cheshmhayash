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
	Auth   Auth     `json:"auth"             koanf:"auth"`
}

// Auth gates the HTTP API behind OIDC. When Enabled is false the API stays
// open (backward-compatible default). MCP HTTP is gated by MCPKeys
// independently: leave the slice empty to keep /mcp open like before.
type Auth struct {
	Enabled bool        `json:"enabled"            koanf:"enabled"`
	OIDC    AuthOIDC    `json:"oidc"               koanf:"oidc"`
	Access  AuthAccess  `json:"access"             koanf:"access"`
	Session AuthSession `json:"session"            koanf:"session"`
	MCPKeys []MCPKey    `json:"mcp_keys,omitempty" koanf:"mcp_keys"`
}

type AuthOIDC struct {
	Issuer       string   `json:"issuer"       koanf:"issuer"`
	ClientID     string   `json:"client_id"    koanf:"client_id"`
	ClientSecret string   `json:"-"            koanf:"client_secret"`
	RedirectURL  string   `json:"redirect_url" koanf:"redirect_url"`
	Scopes       []string `json:"scopes"       koanf:"scopes"`
}

// AuthAccess is the post-login allowlist. At least one of the three
// sign-in slices must be populated when auth is enabled — otherwise any
// account in the IdP can sign in, which is almost never what you want.
//
// The sign-in slices (AllowedEmails/Domains/Groups) decide who may open
// the dashboard at all; everyone who passes them gets at least read-only
// access. Admin carries a second, write-access allowlist: a signed-in
// user who additionally matches it gets the "admin" role (full read +
// write). When Admin is empty every signed-in user is an admin, which
// preserves the pre-role behaviour where any allowed account had full
// access.
type AuthAccess struct {
	AllowedEmails  []string   `json:"allowed_emails,omitempty"  koanf:"allowed_emails"`
	AllowedDomains []string   `json:"allowed_domains,omitempty" koanf:"allowed_domains"`
	AllowedGroups  []string   `json:"allowed_groups,omitempty"  koanf:"allowed_groups"`
	GroupsClaim    string     `json:"groups_claim,omitempty"    koanf:"groups_claim"`
	Admin          AccessRule `json:"admin,omitzero"            koanf:"admin"`
}

// AccessRule is an email/domain/group allowlist triple. Used for the
// write-access (admin) tier; the same matching logic as the sign-in
// allowlist, scoped to a narrower set of identities.
type AccessRule struct {
	AllowedEmails  []string `json:"allowed_emails,omitempty"  koanf:"allowed_emails"`
	AllowedDomains []string `json:"allowed_domains,omitempty" koanf:"allowed_domains"`
	AllowedGroups  []string `json:"allowed_groups,omitempty"  koanf:"allowed_groups"`
}

// IsEmpty reports whether the rule matches nobody (all three slices empty).
func (r AccessRule) IsEmpty() bool {
	return len(r.AllowedEmails) == 0 && len(r.AllowedDomains) == 0 && len(r.AllowedGroups) == 0
}

type AuthSession struct {
	Secret     string `json:"-"           koanf:"secret"`
	TTLSeconds int    `json:"ttl_seconds" koanf:"ttl_seconds"`
	CookieName string `json:"cookie_name" koanf:"cookie_name"`
	Secure     bool   `json:"secure"      koanf:"secure"`
}

// MCPKey is one bearer token accepted on the /mcp HTTP transport. Name is
// human-readable for log lines / revocation; Value is the secret.
type MCPKey struct {
	Name  string `json:"name" koanf:"name"`
	Value string `json:"-"    koanf:"value"`
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

// TTL is the cookie lifetime as a duration. Falls back to 12 h when unset.
func (a AuthSession) TTL() time.Duration {
	if a.TTLSeconds <= 0 {
		return 12 * time.Hour
	}
	return time.Duration(a.TTLSeconds) * time.Second
}

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
	if s.Auth.Enabled {
		if err := validateAuth(&s.Auth); err != nil {
			return err
		}
	}
	for i, k := range s.Auth.MCPKeys {
		if k.Value == "" {
			return fmt.Errorf("auth.mcp_keys[%d]: value is required", i)
		}
	}
	return nil
}

func validateAuth(a *Auth) error {
	if a.OIDC.Issuer == "" {
		return errors.New("auth.oidc.issuer is required when auth is enabled")
	}
	if a.OIDC.ClientID == "" {
		return errors.New("auth.oidc.client_id is required when auth is enabled")
	}
	if a.OIDC.RedirectURL == "" {
		return errors.New("auth.oidc.redirect_url is required when auth is enabled")
	}
	if a.Session.Secret == "" {
		return errors.New("auth.session.secret is required when auth is enabled")
	}
	if len(a.Session.Secret) < 16 {
		return errors.New("auth.session.secret must be at least 16 characters")
	}
	// Force an explicit allowlist so a typo in the IdP config can't grant
	// access to every account in the directory.
	if len(a.Access.AllowedEmails) == 0 &&
		len(a.Access.AllowedDomains) == 0 &&
		len(a.Access.AllowedGroups) == 0 {
		return errors.New(
			"auth.access requires at least one of allowed_emails, allowed_domains, or allowed_groups",
		)
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
	case "auth":
		return applyAuthEnv(&s.Auth, parts[1:], val)
	}
	return nil
}

// applyAuthEnv handles CHESHMHAYASH__AUTH__* env keys. Subsections:
//
//	enabled
//	oidc.{issuer,client_id,client_secret,redirect_url,scopes}     scopes is comma-separated
//	access.{allowed_emails,allowed_domains,allowed_groups,groups_claim}   slices comma-separated
//	access.admin.{allowed_emails,allowed_domains,allowed_groups}          write-access allowlist
//	session.{secret,ttl_seconds,cookie_name,secure}
//	mcp_keys.<idx>.{name,value}
func applyAuthEnv(a *Auth, parts []string, val string) error {
	if len(parts) == 0 {
		return fmt.Errorf("expected auth.<key>")
	}
	switch strings.ToLower(parts[0]) {
	case "enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("auth.enabled: %w", err)
		}
		a.Enabled = b
	case "oidc":
		if len(parts) != 2 {
			return fmt.Errorf("expected auth.oidc.<key>")
		}
		switch strings.ToLower(parts[1]) {
		case "issuer":
			a.OIDC.Issuer = val
		case "client_id":
			a.OIDC.ClientID = val
		case "client_secret":
			a.OIDC.ClientSecret = val
		case "redirect_url":
			a.OIDC.RedirectURL = val
		case "scopes":
			a.OIDC.Scopes = splitCSV(val)
		default:
			return fmt.Errorf("unknown auth.oidc field %q", parts[1])
		}
	case "access":
		return applyAccessEnv(&a.Access, parts[1:], val)
	case "session":
		if len(parts) != 2 {
			return fmt.Errorf("expected auth.session.<key>")
		}
		switch strings.ToLower(parts[1]) {
		case "secret":
			a.Session.Secret = val
		case "ttl_seconds":
			n, err := strconv.Atoi(val)
			if err != nil {
				return fmt.Errorf("auth.session.ttl_seconds: %w", err)
			}
			a.Session.TTLSeconds = n
		case "cookie_name":
			a.Session.CookieName = val
		case "secure":
			b, err := strconv.ParseBool(val)
			if err != nil {
				return fmt.Errorf("auth.session.secure: %w", err)
			}
			a.Session.Secure = b
		default:
			return fmt.Errorf("unknown auth.session field %q", parts[1])
		}
	case "mcp_keys":
		if len(parts) < 3 {
			return fmt.Errorf("expected auth.mcp_keys.<index>.<key>")
		}
		idx, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("mcp_keys index must be numeric")
		}
		for len(a.MCPKeys) <= idx {
			a.MCPKeys = append(a.MCPKeys, MCPKey{})
		}
		switch strings.ToLower(parts[2]) {
		case "name":
			a.MCPKeys[idx].Name = val
		case "value":
			a.MCPKeys[idx].Value = val
		default:
			return fmt.Errorf("unknown auth.mcp_keys field %q", parts[2])
		}
	default:
		return fmt.Errorf("unknown auth field %q", parts[0])
	}
	return nil
}

// applyAccessEnv handles CHESHMHAYASH__AUTH__ACCESS__* env keys, including
// the nested admin allowlist (auth.access.admin.allowed_{emails,domains,
// groups}). Slice values are comma-separated.
func applyAccessEnv(acc *AuthAccess, parts []string, val string) error {
	if len(parts) == 0 {
		return fmt.Errorf("expected auth.access.<key>")
	}
	switch strings.ToLower(parts[0]) {
	case "allowed_emails":
		acc.AllowedEmails = splitCSV(val)
	case "allowed_domains":
		acc.AllowedDomains = splitCSV(val)
	case "allowed_groups":
		acc.AllowedGroups = splitCSV(val)
	case "groups_claim":
		acc.GroupsClaim = val
	case "admin":
		if len(parts) != 2 {
			return fmt.Errorf("expected auth.access.admin.<key>")
		}
		switch strings.ToLower(parts[1]) {
		case "allowed_emails":
			acc.Admin.AllowedEmails = splitCSV(val)
		case "allowed_domains":
			acc.Admin.AllowedDomains = splitCSV(val)
		case "allowed_groups":
			acc.Admin.AllowedGroups = splitCSV(val)
		default:
			return fmt.Errorf("unknown auth.access.admin field %q", parts[1])
		}
	default:
		return fmt.Errorf("unknown auth.access field %q", parts[0])
	}
	return nil
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
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
