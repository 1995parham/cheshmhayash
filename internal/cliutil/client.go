package cliutil

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/urfave/cli/v3"

	"github.com/1995parham/cheshmhayash/internal/codec"
	"github.com/1995parham/cheshmhayash/internal/config"
	"github.com/1995parham/cheshmhayash/internal/schema"
	"github.com/1995parham/cheshmhayash/internal/version"
)

const dialTimeout = 5 * time.Second

// ConnFlags select the NATS server a client command talks to: a cluster
// from the settings file, or a server given on the command line.
func ConnFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "cluster",
			Aliases: []string{"C"},
			Usage:   "cluster name from the settings file (optional when only one is configured)",
			Sources: cli.EnvVars("CHESHMHAYASH_CLUSTER"),
		},
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "NATS URL; overrides --cluster, so no settings file is needed",
			Sources: cli.EnvVars("NATS_URL"),
		},
		&cli.StringFlag{Name: "user", Usage: "user name (overrides the cluster's)", Sources: cli.EnvVars("NATS_USER")},
		&cli.StringFlag{Name: "password", Usage: "password (overrides the cluster's)", Sources: cli.EnvVars("NATS_PASSWORD")},
		&cli.StringFlag{Name: "creds", Usage: "credentials file (overrides the cluster's)", Sources: cli.EnvVars("NATS_CREDS")},
	}
}

// SchemaFlags add schema files and the type header to a command.
func SchemaFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringSliceFlag{
			Name:    "schemas",
			Usage:   "extra schemas file (TOML: [[sources]] and [[subjects]]); repeat for more",
			Sources: cli.EnvVars("CHESHMHAYASH_SCHEMAS"),
		},
	}
}

// ClientSettings loads the settings for a client command: quietly, and
// without requiring a cluster, since --server can replace it.
func ClientSettings(cmd *cli.Command) (*config.Settings, error) {
	return LoadSettings(cmd, config.AllowNoClusters(), config.Quiet())
}

// ResolveCluster returns the connection settings the flags select.
func ResolveCluster(cmd *cli.Command, settings *config.Settings) (config.NATS, error) {
	var c config.NATS
	if url := cmd.String("server"); url != "" {
		c = config.NATS{Name: "cli", URL: url}
	} else {
		var err error
		if c, err = settings.Cluster(cmd.String("cluster")); err != nil {
			return config.NATS{}, err
		}
	}
	if cmd.IsSet("user") {
		c.User = cmd.String("user")
	}
	if cmd.IsSet("password") {
		c.Password = cmd.String("password")
	}
	if cmd.IsSet("creds") {
		c.CredsFile = cmd.String("creds")
	}
	return c, nil
}

// Dial connects to c and fails fast when the server cannot be reached:
// an interactive command must not wait silently for a server that is
// not there. After the first connect it reconnects without limit.
func Dial(c config.NATS) (*nats.Conn, error) {
	opts := []nats.Option{
		nats.Name("cheshmhayash/" + version.String()),
		nats.Timeout(dialTimeout),
		nats.MaxReconnects(-1),
	}
	if c.CredsFile != "" {
		opts = append(opts, nats.UserCredentials(c.CredsFile))
	}
	if c.User != "" {
		opts = append(opts, nats.UserInfo(c.User, c.Password))
	}
	nc, err := nats.Connect(c.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect to %s (%s): %w", c.Name, c.URL, err)
	}
	return nc, nil
}

// Schemas is the loaded schema state of a command.
type Schemas struct {
	Config   config.Schemas
	Registry *schema.Registry
	Router   *schema.Router
}

// LoadSchemas merges the settings' [schemas] with every --schemas file,
// in order, and loads the sources.
func LoadSchemas(ctx context.Context, cmd *cli.Command, settings *config.Settings) (*Schemas, error) {
	cfg := settings.Schemas
	for _, path := range cmd.StringSlice("schemas") {
		extra, err := config.LoadSchemas(path)
		if err != nil {
			return nil, err
		}
		cfg = cfg.Merge(extra)
	}

	sources := make([]schema.SourceConfig, 0, len(cfg.Sources))
	for _, s := range cfg.Sources {
		sources = append(sources, schema.SourceConfig{Name: s.Name, Type: schema.SourceType(s.Type), Path: s.Path})
	}
	reg, err := schema.Load(ctx, sources)
	if err != nil {
		return nil, err
	}

	rules := make([]schema.Rule, 0, len(cfg.Subjects))
	for _, r := range cfg.Subjects {
		rules = append(rules, schema.Rule{Pattern: r.Match, Message: r.Message})
	}
	router, err := schema.NewRouter(rules)
	if err != nil {
		return nil, err
	}
	return &Schemas{Config: cfg, Registry: reg, Router: router}, nil
}

// Proto returns the protobuf decoder, with force as the type for every
// payload when set.
func (s *Schemas) Proto(force string, emitDefaults bool) *codec.ProtoDecoder {
	return &codec.ProtoDecoder{
		Registry:     s.Registry,
		Router:       s.Router,
		TypeHeader:   s.Config.TypeHeader,
		Force:        force,
		EmitDefaults: emitDefaults,
	}
}

// Chain returns the decoder chain: protobuf first, then the schemaless
// fallbacks.
func (s *Schemas) Chain(force string, emitDefaults bool) codec.Chain {
	return append(codec.Chain{s.Proto(force, emitDefaults)}, codec.DefaultFallbacks()...)
}
