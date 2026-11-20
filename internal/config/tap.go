package config

import (
	"errors"
	"fmt"
	"time"
)

// Tap roles: who may open a tap when auth is on.
const (
	TapRoleAdmin    = "admin"
	TapRoleReadOnly = "readonly"
)

// Tap configures the dashboard's live view of decoded messages
// (GET /api/tap/clusters/{c}/stream). It shows message content, so it is
// off by default and every limit has a conservative default.
type Tap struct {
	Enabled bool `json:"enabled" koanf:"enabled"`
	// Allow lists the subject patterns a tap may subscribe to; a request
	// must be covered by one of them. Empty allows any subject.
	Allow []string `json:"allow,omitempty" koanf:"allow"`
	// MaxRate is the most messages per second sent to one browser; the
	// rest are counted as skipped.
	MaxRate int `json:"max_rate" koanf:"max_rate"`
	// MaxDurationSeconds closes a tap after this long.
	MaxDurationSeconds int `json:"max_duration_seconds" koanf:"max_duration_seconds"`
	// MaxTaps is the most taps one process serves at a time.
	MaxTaps int `json:"max_taps" koanf:"max_taps"`
	// Role is the role that may open a tap when auth is on: "admin"
	// (default) or "readonly" (every signed-in user).
	Role string `json:"role" koanf:"role"`
}

// MaxDuration is MaxDurationSeconds as a duration.
func (t Tap) MaxDuration() time.Duration {
	return time.Duration(t.MaxDurationSeconds) * time.Second
}

func (t Tap) validate() error {
	if !t.Enabled {
		return nil
	}
	var errs []error
	if t.MaxRate <= 0 {
		errs = append(errs, errors.New("max_rate must be positive"))
	}
	if t.MaxDurationSeconds <= 0 {
		errs = append(errs, errors.New("max_duration_seconds must be positive"))
	}
	if t.MaxTaps <= 0 {
		errs = append(errs, errors.New("max_taps must be positive"))
	}
	if t.Role != TapRoleAdmin && t.Role != TapRoleReadOnly {
		errs = append(errs, fmt.Errorf("role must be %q or %q, got %q", TapRoleAdmin, TapRoleReadOnly, t.Role))
	}
	return errors.Join(errs...)
}
