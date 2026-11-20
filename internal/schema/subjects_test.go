package schema

import "testing"

func TestMatchSubject(t *testing.T) {
	tests := []struct {
		pattern, subject string
		want             bool
	}{
		{"ride.accepted", "ride.accepted", true},
		{"ride.accepted", "ride.accepted.v2", false},
		{"ride.accepted.v2", "ride.accepted", false},
		{"ride.*", "ride.accepted", true},
		{"ride.*", "ride.accepted.v2", false},
		{"ride.*.v2", "ride.accepted.v2", true},
		{"ride.>", "ride.accepted", true},
		{"ride.>", "ride.accepted.v2", true},
		{"ride.>", "ride", false},
		{">", "anything.at.all", true},
		{"*", "one", true},
		{"*", "one.two", false},
		{"driver.location.raw", "driver.location", false},
	}
	for _, tt := range tests {
		if got := MatchSubject(tt.pattern, tt.subject); got != tt.want {
			t.Errorf("MatchSubject(%q, %q) = %v, want %v", tt.pattern, tt.subject, got, tt.want)
		}
	}
}

func TestValidatePattern(t *testing.T) {
	valid := []string{"a", "a.b", "a.*", "a.>", ">", "*.b.>"}
	invalid := []string{"", "a..b", ".a", "a.", "a.>.b", "a.b*", "a.>x", "a b"}
	for _, p := range valid {
		if err := ValidatePattern(p); err != nil {
			t.Errorf("ValidatePattern(%q) = %v, want nil", p, err)
		}
	}
	for _, p := range invalid {
		if err := ValidatePattern(p); err == nil {
			t.Errorf("ValidatePattern(%q) = nil, want an error", p)
		}
	}
}

func TestRouterFirstMatchWins(t *testing.T) {
	r, err := NewRouter([]Rule{
		{Pattern: "ride.accepted", Message: "ride.Accept"},
		{Pattern: "ride.>", Message: "ride.Event"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rule, ok := r.Resolve("ride.accepted"); !ok || rule.Message != "ride.Accept" {
		t.Fatalf("Resolve(ride.accepted) = %v, %v", rule, ok)
	}
	if rule, ok := r.Resolve("ride.finished"); !ok || rule.Message != "ride.Event" {
		t.Fatalf("Resolve(ride.finished) = %v, %v", rule, ok)
	}
	if _, ok := r.Resolve("driver.status"); ok {
		t.Fatal("Resolve(driver.status) matched")
	}
	var nilRouter *Router
	if _, ok := nilRouter.Resolve("x"); ok {
		t.Fatal("nil router matched")
	}
}

func TestNewRouterRejectsBadRules(t *testing.T) {
	if _, err := NewRouter([]Rule{{Pattern: "a..b", Message: "x.Y"}}); err == nil {
		t.Fatal("bad pattern accepted")
	}
	if _, err := NewRouter([]Rule{{Pattern: "a.b"}}); err == nil {
		t.Fatal("rule without message accepted")
	}
}
