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

// Settings is the fully-resolved runtime configuration (defaults → TOML →
// env), returned by Load and consumed by main.
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
	Enabled bool `json:"enabled" koanf:"enabled"`
	// Mode selects how a request is authenticated when Enabled:
	//   "oidc" (default) — cheshmhayash runs the OIDC login flow itself and
	//                       issues an HMAC-signed session cookie.
	//   "jwt"            — no login flow. Every request must carry an
	//                       `Authorization: Bearer <jwt>` access token minted
	//                       by auth.oidc.issuer (a "builtin oauth" gateway in
	//                       front of cheshmhayash). The token's signature /
	//                       issuer / expiry are verified and its claims drive
	//                       the same allowlist + admin/readonly roles.
	Mode     string       `json:"mode,omitempty"     koanf:"mode"`
	OIDC     AuthOIDC     `json:"oidc"               koanf:"oidc"`
	Access   AuthAccess   `json:"access"             koanf:"access"`
	Session  AuthSession  `json:"session"            koanf:"session"`
	JWT      AuthJWT      `json:"jwt,omitzero"       koanf:"jwt"`
	MCPKeys  []MCPKey     `json:"mcp_keys,omitempty" koanf:"mcp_keys"`
	MCPOAuth AuthMCPOAuth `json:"mcp_oauth,omitzero" koanf:"mcp_oauth"`
}

// Auth mode identifiers (auth.mode).
const (
	AuthModeOIDC = "oidc"
	AuthModeJWT  = "jwt"
)

// ModeOrDefault returns the configured auth mode lower-cased, defaulting to
// "oidc" when unset so existing configs keep their cookie-login behaviour.
func (a Auth) ModeOrDefault() string {
	if a.Mode == "" {
		return AuthModeOIDC
	}
	return strings.ToLower(a.Mode)
}

// AuthJWT tunes the "jwt" auth mode (auth.mode = "jwt"), where cheshmhayash
// validates an access-token JWT presented on every request instead of running
// its own login flow. The token is verified against auth.oidc.issuer.
type AuthJWT struct {
	// Audiences, when non-empty, restricts accepted tokens to those whose
	// `aud` carries one of these values (RFC 8707). Empty accepts any token
	// the issuer signed — fine when the gateway is the sole ingress and
	// strips a client-supplied Authorization header.
	Audiences []string `json:"audiences,omitempty" koanf:"audiences"`
}

// AuthMCPOAuth turns the /mcp HTTP transport into an OAuth 2.0 resource
// server per the MCP authorization spec (2025-06-18): the server advertises
// Protected Resource Metadata (RFC 9728) pointing at the same OIDC issuer as
// the dashboard, and validates Keycloak-issued access-token JWTs presented as
// `Authorization: Bearer`. Requires auth.enabled (it reuses the OIDC provider
// and the auth.access allowlists). Static auth.mcp_keys keep working as a
// fallback alongside it. Off by default — backward-compatible.
type AuthMCPOAuth struct {
	Enabled bool `json:"enabled" koanf:"enabled"`
	// Resource is the canonical URI of this MCP server (RFC 8707), e.g.
	// "https://cheshmhayash.example.com/mcp". Advertised in the metadata
	// document and used as the default accepted token audience.
	Resource string `json:"resource" koanf:"resource"`
	// AuthorizationServers overrides the metadata's authorization_servers
	// list. Defaults to [auth.oidc.issuer] when empty.
	AuthorizationServers []string `json:"authorization_servers,omitempty" koanf:"authorization_servers"`
	// Audiences is the set of `aud` values accepted on inbound access
	// tokens. Defaults to [Resource] when empty. Keycloak must be configured
	// (audience mapper / client scope) to mint tokens carrying one of these.
	Audiences []string `json:"audiences,omitempty" koanf:"audiences"`
	// SkipAudienceCheck accepts any token the issuer signed, regardless of
	// `aud` (signature, issuer and expiry are still enforced, and the
	// subject still has to pass the access allowlist). This drops the
	// RFC 8707 confused-deputy protection — only set it when the IdP can't
	// mint resource audiences (e.g. a Keycloak client without an audience
	// mapper) and the issuer is trusted for this realm.
	SkipAudienceCheck bool `json:"skip_audience_check,omitempty" koanf:"skip_audience_check"`
}

// AuthOIDC holds the OpenID Connect provider coordinates for the login flow.
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

// AuthSession configures the HMAC-signed cookie that carries the session.
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

// Server is the HTTP listener's bind address.
type Server struct {
	Host string `json:"host" koanf:"host"`
	Port int    `json:"port" koanf:"port"`
}

// NATS describes one configured cluster connection.
type NATS struct {
	Name      string `json:"name"                 koanf:"name"`
	URL       string `json:"url"                  koanf:"url"`
	CredsFile string `json:"creds_file,omitempty" koanf:"creds_file"`
	User      string `json:"user,omitempty"       koanf:"user"`
	// Password is redacted on the startup log; the JSON tag matters only
	// for that single pretty-print.
	Password           string `json:"-"                              koanf:"password"`
	RequestTimeoutMS   int    `json:"request_timeout_ms,omitempty"   koanf:"request_timeout_ms"`
	DiscoveryTimeoutMS int    `json:"discovery_timeout_ms,omitempty" koanf:"discovery_timeout_ms"`
}

// RequestTimeout is the per-request NATS deadline, defaulted when unset.
func (n NATS) RequestTimeout() time.Duration {
	if n.RequestTimeoutMS <= 0 {
		return defaultRequestTimeout
	}
	return time.Duration(n.RequestTimeoutMS) * time.Millisecond
}

// DiscoveryTimeout is the deadline for multi-responder discovery requests
// (e.g. SERVER.PING), defaulted when unset.
func (n NATS) DiscoveryTimeout() time.Duration {
	if n.DiscoveryTimeoutMS <= 0 {
		return defaultDiscoveryTimeout
	}
	return time.Duration(n.DiscoveryTimeoutMS) * time.Millisecond
}

// Addr is the host:port the HTTP server binds to.
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
	} else if s.Auth.MCPOAuth.Enabled {
		// MCP OAuth reuses the OIDC provider + allowlists, so it can't run
		// without the dashboard auth being on.
		return errors.New("auth.mcp_oauth requires auth.enabled = true")
	}
	for i, k := range s.Auth.MCPKeys {
		if k.Value == "" {
			return fmt.Errorf("auth.mcp_keys[%d]: value is required", i)
		}
	}
	return nil
}

func validateAuth(a *Auth) error {
	// The issuer is needed in every mode: oidc mode redirects to it, jwt mode
	// fetches its JWKS to verify inbound tokens.
	if a.OIDC.Issuer == "" {
		return errors.New("auth.oidc.issuer is required when auth is enabled")
	}
	// Force an explicit allowlist so a typo in the IdP config can't grant
	// access to every account in the directory. Shared by both modes.
	if len(a.Access.AllowedEmails) == 0 &&
		len(a.Access.AllowedDomains) == 0 &&
		len(a.Access.AllowedGroups) == 0 {
		return errors.New(
			"auth.access requires at least one of allowed_emails, allowed_domains, or allowed_groups",
		)
	}
	switch a.ModeOrDefault() {
	case AuthModeOIDC:
		if err := validateAuthOIDC(a); err != nil {
			return err
		}
	case AuthModeJWT:
		// jwt mode needs only the issuer (for JWKS) + the allowlist above.
		// There is no login flow, so no client credentials, redirect URL, or
		// session secret are required.
	default:
		return fmt.Errorf(
			"auth.mode %q is not recognised (want %q or %q)", a.Mode, AuthModeOIDC, AuthModeJWT,
		)
	}
	if a.MCPOAuth.Enabled && a.MCPOAuth.Resource == "" {
		return errors.New("auth.mcp_oauth.resource is required when auth.mcp_oauth is enabled")
	}
	return nil
}

// validateAuthOIDC enforces the extra inputs the cookie login flow needs:
// client credentials, a redirect URL, and a session-signing secret.
func validateAuthOIDC(a *Auth) error {
	if a.OIDC.ClientID == "" {
		return errors.New("auth.oidc.client_id is required when auth.mode = oidc")
	}
	if a.OIDC.RedirectURL == "" {
		return errors.New("auth.oidc.redirect_url is required when auth.mode = oidc")
	}
	if a.Session.Secret == "" {
		return errors.New("auth.session.secret is required when auth.mode = oidc")
	}
	if len(a.Session.Secret) < 16 {
		return errors.New("auth.session.secret must be at least 16 characters")
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
			return errors.New("expected server.<key>")
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
			return errors.New("expected nats.<index>.<key>")
		}
		idx, err := strconv.Atoi(parts[1])
		if err != nil {
			return errors.New("nats index must be numeric")
		}
		for len(s.NATS) <= idx {
			s.NATS = append(s.NATS, NATS{})
		}
		return setNATSField(&s.NATS[idx], strings.ToLower(parts[2]), val)
	case "notify":
		if len(parts) < 3 {
			return errors.New("expected notify.<index>.<key>")
		}
		idx, err := strconv.Atoi(parts[1])
		if err != nil {
			return errors.New("notify index must be numeric")
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
//	mode                                                          "oidc" | "jwt"
//	oidc.{issuer,client_id,client_secret,redirect_url,scopes}     scopes is comma-separated
//	access.{allowed_emails,allowed_domains,allowed_groups,groups_claim}   slices comma-separated
//	access.admin.{allowed_emails,allowed_domains,allowed_groups}          write-access allowlist
//	session.{secret,ttl_seconds,cookie_name,secure}
//	jwt.audiences                                                 comma-separated
//	mcp_keys.<idx>.{name,value}
//	mcp_oauth.{enabled,resource,authorization_servers,audiences}   slices comma-separated
func applyAuthEnv(a *Auth, parts []string, val string) error {
	if len(parts) == 0 {
		return errors.New("expected auth.<key>")
	}
	switch strings.ToLower(parts[0]) {
	case "enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("auth.enabled: %w", err)
		}
		a.Enabled = b
	case "mode":
		a.Mode = val
	case "jwt":
		return applyJWTEnv(&a.JWT, parts[1:], val)
	case "oidc":
		if len(parts) != 2 {
			return errors.New("expected auth.oidc.<key>")
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
			return errors.New("expected auth.session.<key>")
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
			return errors.New("expected auth.mcp_keys.<index>.<key>")
		}
		idx, err := strconv.Atoi(parts[1])
		if err != nil {
			return errors.New("mcp_keys index must be numeric")
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
	case "mcp_oauth":
		return applyMCPOAuthEnv(&a.MCPOAuth, parts[1:], val)
	default:
		return fmt.Errorf("unknown auth field %q", parts[0])
	}
	return nil
}

// applyJWTEnv handles CHESHMHAYASH__AUTH__JWT__* env keys:
//
//	audiences   comma-separated
func applyJWTEnv(j *AuthJWT, parts []string, val string) error {
	if len(parts) != 1 {
		return errors.New("expected auth.jwt.<key>")
	}
	switch strings.ToLower(parts[0]) {
	case "audiences":
		j.Audiences = splitCSV(val)
	default:
		return fmt.Errorf("unknown auth.jwt field %q", parts[0])
	}
	return nil
}

// applyMCPOAuthEnv handles CHESHMHAYASH__AUTH__MCP_OAUTH__* env keys:
//
//	enabled
//	resource
//	authorization_servers   comma-separated
//	audiences               comma-separated
//	skip_audience_check
func applyMCPOAuthEnv(m *AuthMCPOAuth, parts []string, val string) error {
	if len(parts) != 1 {
		return errors.New("expected auth.mcp_oauth.<key>")
	}
	switch strings.ToLower(parts[0]) {
	case "enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("auth.mcp_oauth.enabled: %w", err)
		}
		m.Enabled = b
	case "resource":
		m.Resource = val
	case "authorization_servers":
		m.AuthorizationServers = splitCSV(val)
	case "audiences":
		m.Audiences = splitCSV(val)
	case "skip_audience_check":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("auth.mcp_oauth.skip_audience_check: %w", err)
		}
		m.SkipAudienceCheck = b
	default:
		return fmt.Errorf("unknown auth.mcp_oauth field %q", parts[0])
	}
	return nil
}

// applyAccessEnv handles CHESHMHAYASH__AUTH__ACCESS__* env keys, including
// the nested admin allowlist (auth.access.admin.allowed_{emails,domains,
// groups}). Slice values are comma-separated.
func applyAccessEnv(acc *AuthAccess, parts []string, val string) error {
	if len(parts) == 0 {
		return errors.New("expected auth.access.<key>")
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
			return errors.New("expected auth.access.admin.<key>")
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
	for i := range rt.NumField() {
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
