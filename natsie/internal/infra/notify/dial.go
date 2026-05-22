package notify

import (
	"fmt"
	neturl "net/url"
)

// Dial parses url and returns the Notifier implementation that matches the
// scheme. Unknown schemes return an error so misconfiguration fails loudly
// at startup rather than silently dropping messages later.
func Dial(url string) (Notifier, error) {
	u, err := neturl.Parse(url)
	if err != nil {
		return nil, fmt.Errorf("parse notify url %q: %w", url, err)
	}
	switch u.Scheme {
	case "stdout":
		return NewStdout(), nil
	case "mattermost":
		return NewMattermost(u)
	default:
		return nil, fmt.Errorf("notify scheme %q is not registered (try stdout://, mattermost://..., slack://...)", u.Scheme)
	}
}
