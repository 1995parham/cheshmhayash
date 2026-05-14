package config

// Default returns the baseline configuration that ships with the binary.
// Anything left unset in `settings.toml` or `CHESHMHAYASH__*` env vars
// falls back to these values.
func Default() Settings {
	return Settings{
		Server: Server{
			Host: "0.0.0.0",
			Port: 1378,
		},
		// No NATS clusters by default — the operator must declare at least
		// one in settings.toml or via env vars. Validation in Load() will
		// error out if the final list is empty.
		NATS: nil,
	}
}
