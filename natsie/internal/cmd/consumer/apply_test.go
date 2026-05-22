package consumer

import "testing"

// The isActive / consumerInfo unit tests moved to internal/cleanup with
// the underlying logic. This file keeps a minimal smoke test for the CLI
// wiring so the consumer package still has direct coverage.

func TestConsumerCommandRegistersBothSubcommands(t *testing.T) {
	cmd := Command()
	if cmd.Name != "consumer" {
		t.Errorf("Name=%q want consumer", cmd.Name)
	}
	names := map[string]bool{}
	for _, c := range cmd.Commands {
		names[c.Name] = true
	}
	for _, want := range []string{"scan", "apply"} {
		if !names[want] {
			t.Errorf("missing subcommand %q (got %v)", want, names)
		}
	}
}
