// Package natsctx reads ~/.config/nats/context/*.json files (the same store
// the official `nats` CLI uses) and dials a connection from them.
package natsctx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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

// Connect dials the cluster described by ~/.config/nats/context/<name>.json.
func Connect(name string) (*Conn, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".config", "nats", "context", name+".json")
	b, err := os.ReadFile(path)
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
