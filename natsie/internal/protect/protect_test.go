package protect

import "testing"

func TestDefaultMetadataKeyProtectsWithoutConfig(t *testing.T) {
	p, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	meta := map[string]string{DefaultMetadataKey: "true"}
	if !p.Protected("rides", "worker", meta) {
		t.Fatal("expected the default metadata key to protect with an empty config")
	}

	if p.Protected("rides", "worker", nil) {
		t.Fatal("expected an unannotated consumer to be deletable")
	}
}

func TestExplicitFalseOptsOut(t *testing.T) {
	p, _ := New(Config{})

	for _, v := range []string{"false", "FALSE", " no ", "0", "off"} {
		if p.Protected("rides", "worker", map[string]string{DefaultMetadataKey: v}) {
			t.Fatalf("value %q should opt out of protection", v)
		}
	}

	// Anything else counts as "managed" — an operator who sets the key at all
	// is signalling ownership.
	if !p.Protected("rides", "worker", map[string]string{DefaultMetadataKey: "nack"}) {
		t.Fatal("non-falsey value should protect")
	}
}

func TestCustomMetadataKey(t *testing.T) {
	p, err := New(Config{MetadataKeys: []string{"app.kubernetes.io/managed-by"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	meta := map[string]string{"app.kubernetes.io/managed-by": "nack"}
	if !p.Protected("rides", "worker", meta) {
		t.Fatal("expected the configured key to protect")
	}

	// The default key keeps working alongside a custom one.
	if !p.Protected("rides", "worker", map[string]string{DefaultMetadataKey: "true"}) {
		t.Fatal("default key should still apply when custom keys are configured")
	}
}

func TestPatterns(t *testing.T) {
	p, err := New(Config{Patterns: []Pattern{
		{Stream: "rides", Consumer: "svc-*"},
		{Consumer: "*-gitops"},
		{Stream: "billing*"},
	}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cases := []struct {
		stream, consumer string
		want             bool
	}{
		{"rides", "svc-trip", true},         // both fields match
		{"rides", "adhoc", false},           // consumer glob misses
		{"orders", "svc-trip", false},       // stream glob misses
		{"anything", "deploy-gitops", true}, // empty stream field is a wildcard
		{"billing-eu", "whatever", true},    // empty consumer field is a wildcard
		{"orders", "worker", false},
	}

	for _, c := range cases {
		if got := p.Protected(c.stream, c.consumer, nil); got != c.want {
			t.Errorf("Protected(%q,%q) = %v, want %v", c.stream, c.consumer, got, c.want)
		}
	}
}

func TestReasonExplainsWhy(t *testing.T) {
	p, _ := New(Config{Patterns: []Pattern{{Stream: "rides", Consumer: "svc-*"}}})

	if got := p.Reason("rides", "svc-trip", nil); got != "protected by pattern rides/svc-*" {
		t.Errorf("pattern reason = %q", got)
	}

	got := p.Reason("rides", "x", map[string]string{DefaultMetadataKey: "nack"})
	if got != "declaratively managed (metadata natsie.io/managed=nack)" {
		t.Errorf("metadata reason = %q", got)
	}

	if got := p.Reason("orders", "x", nil); got != "" {
		t.Errorf("unprotected consumer should have no reason, got %q", got)
	}
}

func TestEmptyPatternRejected(t *testing.T) {
	if _, err := New(Config{Patterns: []Pattern{{}}}); err == nil {
		t.Fatal("expected a pattern with neither stream nor consumer to be rejected")
	}
}

func TestBadGlobRejected(t *testing.T) {
	if _, err := New(Config{Patterns: []Pattern{{Consumer: "[unclosed"}}}); err == nil {
		t.Fatal("expected a malformed glob to be rejected at construction")
	}
}

func TestNilProtectorProtectsNothing(t *testing.T) {
	var p *Protector
	if p.Protected("rides", "worker", map[string]string{DefaultMetadataKey: "true"}) {
		t.Fatal("a nil Protector must protect nothing")
	}
}
