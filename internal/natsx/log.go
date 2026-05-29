package natsx

import "log/slog"

// Logger is the package-wide logger when non-nil. Tests set it to capture
// output; production leaves it nil and falls back to slog.Default (which
// main configures).
var Logger *slog.Logger

// defaultLogger returns Logger when set, otherwise slog.Default.
func defaultLogger() *slog.Logger {
	if Logger != nil {
		return Logger
	}
	return slog.Default()
}
