package natsctx

import (
	"context"
	"strings"
	"testing"
)

// TestConnectRejectsPathTraversal guards against a chat-supplied context
// name escaping ~/.config/nats/context via separators or "..". Connect and
// Probe must fail before touching the filesystem.
func TestConnectRejectsPathTraversal(t *testing.T) {
	bad := []string{
		"",
		"..",
		"../../etc/passwd",
		"../secret",
		"sub/dir",
		`back\slash`,
		"/etc/passwd",
	}

	for _, name := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := Connect(name); err == nil {
				t.Errorf("Connect(%q) = nil error, want rejection", name)
			} else if !strings.Contains(err.Error(), "invalid nats context name") {
				t.Errorf("Connect(%q) error = %v, want invalid-name rejection", name, err)
			}

			if err := Probe(context.Background(), name); err == nil {
				t.Errorf("Probe(%q) = nil error, want rejection", name)
			} else if !strings.Contains(err.Error(), "invalid nats context name") {
				t.Errorf("Probe(%q) error = %v, want invalid-name rejection", name, err)
			}
		})
	}
}

// TestContextPathAcceptsRealNames keeps the validation from being so strict
// it rejects the names operators actually use (see snapp-js-* contexts).
func TestContextPathAcceptsRealNames(t *testing.T) {
	for _, name := range []string{"snapp-js-main-teh1", "snapp-js-hodhod", "local", "dev_1.2"} {
		if _, err := contextPath(name); err != nil {
			t.Errorf("contextPath(%q) = %v, want accepted", name, err)
		}
	}
}
