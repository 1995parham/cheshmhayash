// Package natsctx reads ~/.config/nats/context/*.json files (the same store
// the official `nats` CLI uses) and dials a connection from them.
package natsctx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

type contextFile struct {
	URL                string `json:"url"`
	Username           string `json:"user"`
	Password           string `json:"password"`
	Token              string `json:"token"`
	Creds              string `json:"creds"`
	Nkey               string `json:"nkey"`
	TLSCert            string `json:"tls_cert"`
	TLSKey             string `json:"tls_key"`
	TLSCA              string `json:"ca"`
	InboxPrefix        string `json:"inbox_prefix"`
	UserJWT            string `json:"user_jwt"`
	SocksProxy         string `json:"socks_proxy"`
	JetstreamAPIPrefix string `json:"jetstream_api_prefix"`
}

// Conn wraps a *nats.Conn with the context name it came from, so callers can
// pass it to other packages without re-threading the context name everywhere.
type Conn struct {
	*nats.Conn

	Name string
}

// contextPath resolves the on-disk path for a NATS context name. It refuses
// names that aren't a single, local path component so a name reaching us
// from chat (e.g. `/natsie cluster <ctx>`) can't escape the context
// directory with separators or ".." (e.g. "../../etc/passwd").
func contextPath(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, `/\`) || !filepath.IsLocal(name) {
		return "", fmt.Errorf("invalid nats context name %q", name)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".config", "nats", "context", name+".json"), nil
}

// Connect dials the cluster described by ~/.config/nats/context/<name>.json.
func Connect(name string) (*Conn, error) {
	path, err := contextPath(name)
	if err != nil {
		return nil, err
	}

	b, err := os.ReadFile(path) //nolint:gosec // contextPath validated name is a single local component
	if err != nil {
		return nil, fmt.Errorf("read context %s: %w", path, err)
	}

	var cf contextFile
	if err := json.Unmarshal(b, &cf); err != nil {
		return nil, fmt.Errorf("parse context %s: %w", path, err)
	}

	if cf.URL == "" {
		return nil, fmt.Errorf("context %s has no url", name)
	}

	var opts []nats.Option

	switch {
	case cf.Creds != "":
		opts = append(opts, nats.UserCredentials(cf.Creds))
	case cf.UserJWT != "" && cf.Nkey != "":
		opts = append(opts, nats.UserJWTAndSeed(cf.UserJWT, cf.Nkey))
	case cf.Token != "":
		opts = append(opts, nats.Token(cf.Token))
	case cf.Username != "":
		opts = append(opts, nats.UserInfo(cf.Username, cf.Password))
	}

	if cf.InboxPrefix != "" {
		opts = append(opts, nats.CustomInboxPrefix(cf.InboxPrefix))
	}

	opts = append(opts, nats.Name("natsie"))

	nc, err := nats.Connect(cf.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", cf.URL, err)
	}

	return &Conn{Conn: nc, Name: name}, nil
}

// Info is a metadata snapshot of one context file: enough to render a
// clusters table without dialing. The user/password fields are *not*
// included on purpose — this is the chat-readable view.
type Info struct {
	Name string
	URL  string
	User string
}

// List enumerates every ~/.config/nats/context/*.json the bot can see,
// sorted by name. Returns an empty slice when the directory is absent
// (e.g. a CLI invocation with no contexts installed) rather than an error.
func List() ([]Info, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(home, ".config", "nats", "context")

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	out := make([]Info, 0, len(entries))

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		name := strings.TrimSuffix(e.Name(), ".json")

		b, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // path composed from os.ReadDir result inside our config dir
		if err != nil {
			continue
		}

		var cf contextFile
		if err := json.Unmarshal(b, &cf); err != nil {
			continue
		}

		out = append(out, Info{Name: name, URL: cf.URL, User: cf.Username})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out, nil
}

// probeTimeout caps how long Probe will wait. Short on purpose — chat
// commands feel slow past ~5 s and the bot may probe several contexts
// in parallel.
const probeTimeout = 3 * time.Second

// Probe opens a short-lived connection to the named context and
// immediately closes it. Used by the `clusters` chat command to render a
// reachable Y/N column without keeping a long-lived connection open.
func Probe(_ context.Context, name string) error {
	path, err := contextPath(name)
	if err != nil {
		return err
	}

	b, err := os.ReadFile(path) //nolint:gosec // contextPath validated name is a single local component
	if err != nil {
		return err
	}

	var cf contextFile
	if err := json.Unmarshal(b, &cf); err != nil {
		return err
	}

	if cf.URL == "" {
		return fmt.Errorf("context %s has no url", name)
	}

	var opts []nats.Option

	switch {
	case cf.Creds != "":
		opts = append(opts, nats.UserCredentials(cf.Creds))
	case cf.UserJWT != "" && cf.Nkey != "":
		opts = append(opts, nats.UserJWTAndSeed(cf.UserJWT, cf.Nkey))
	case cf.Token != "":
		opts = append(opts, nats.Token(cf.Token))
	case cf.Username != "":
		opts = append(opts, nats.UserInfo(cf.Username, cf.Password))
	}

	opts = append(opts,
		nats.Name("natsie-probe"),
		nats.Timeout(probeTimeout),
		nats.NoReconnect(),
	)

	nc, err := nats.Connect(cf.URL, opts...)
	if err != nil {
		return err
	}

	nc.Close()

	return nil
}

