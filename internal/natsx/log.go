package natsx

import "log/slog"

// defaultLogger returns the package's logger. Tests can override by setting
// Logger; production code reuses slog.Default which the caller configured
// in main.
var Logger *slog.Logger

func defaultLogger() *slog.Logger {
	if Logger != nil {
		return Logger
	}
	return slog.Default()
}
