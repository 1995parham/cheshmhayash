// Package cliutil holds the flags and helpers that several subcommands
// share: the settings file, the cluster choice, and the schema files.
package cliutil

import (
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/1995parham/cheshmhayash/internal/config"
)

// ConfigFlag is the global --config flag. The root command declares it;
// every subcommand reads it through LoadSettings.
const ConfigFlag = "config"

// RootFlags returns the flags the root command declares for all
// subcommands.
func RootFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    ConfigFlag,
			Aliases: []string{"c"},
			Usage:   "settings file (TOML)",
			Value:   config.DefaultFile,
			Sources: cli.EnvVars("CHESHMHAYASH_CONFIG"),
		},
	}
}

// LoadSettings loads the settings file that --config names. A file the
// operator named explicitly must exist.
func LoadSettings(cmd *cli.Command, opts ...config.LoadOption) (*config.Settings, error) {
	if cmd.IsSet(ConfigFlag) {
		opts = append(opts, config.MustExist())
	}
	settings, err := config.Load(cmd.String(ConfigFlag), opts...)
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	return settings, nil
}
