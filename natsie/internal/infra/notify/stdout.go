package notify

import (
	"context"
	"fmt"
	"io"
	"os"
)

// Stdout is a notifier that writes to an io.Writer (default os.Stdout).
// Useful for local debugging and as the fallback when no other URL is
// configured.
type Stdout struct {
	W io.Writer
}

func NewStdout() *Stdout { return &Stdout{W: os.Stdout} }

func (s *Stdout) Name() string { return "stdout" }

func (s *Stdout) Post(_ context.Context, msg Message) error {
	w := s.W
	if w == nil {
		w = os.Stdout
	}
	if msg.Title != "" {
		fmt.Fprintf(w, "== %s ==\n", msg.Title)
	}
	if msg.Body != "" {
		fmt.Fprintln(w, msg.Body)
	}
	if msg.Link != "" {
		fmt.Fprintf(w, "manifest: %s (%s)\n", msg.ManifestID, msg.Link)
	} else if msg.ManifestID != "" {
		fmt.Fprintf(w, "manifest: %s\n", msg.ManifestID)
	}
	return nil
}
