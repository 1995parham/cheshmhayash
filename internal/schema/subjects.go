package schema

import (
	"errors"
	"fmt"
	"strings"
)

// Rule maps the subjects that match Pattern to the message Message.
type Rule struct {
	// Pattern is a NATS subject that may hold the wildcards '*' (one
	// token) and '>' (one or more tokens, last token only).
	Pattern string
	// Message is a message reference: "source:full.Name" or "full.Name".
	Message string
}

// Router resolves a subject to a message reference. The first rule that
// matches wins, so put specific rules before broad ones.
type Router struct {
	rules []Rule
}

// NewRouter validates rules and returns a router over them.
func NewRouter(rules []Rule) (*Router, error) {
	for i, r := range rules {
		if err := ValidatePattern(r.Pattern); err != nil {
			return nil, fmt.Errorf("subject rule %d (%q): %w", i, r.Pattern, err)
		}
		if r.Message == "" {
			return nil, fmt.Errorf("subject rule %d (%q): message is required", i, r.Pattern)
		}
	}
	return &Router{rules: rules}, nil
}

// Resolve returns the first rule whose pattern matches subject.
func (r *Router) Resolve(subject string) (Rule, bool) {
	if r == nil {
		return Rule{}, false
	}
	for _, rule := range r.rules {
		if MatchSubject(rule.Pattern, subject) {
			return rule, true
		}
	}
	return Rule{}, false
}

// Rules returns the rules in match order.
func (r *Router) Rules() []Rule {
	if r == nil {
		return nil
	}
	return append([]Rule(nil), r.rules...)
}

// ValidatePattern reports whether pattern is a valid NATS subject filter.
func ValidatePattern(pattern string) error {
	if pattern == "" {
		return errors.New("empty subject")
	}
	tokens := strings.Split(pattern, ".")
	for i, t := range tokens {
		switch {
		case t == "":
			return fmt.Errorf("empty token at position %d", i+1)
		case t == ">" && i != len(tokens)-1:
			return errors.New("'>' must be the last token")
		case len(t) > 1 && strings.ContainsAny(t, "*>"):
			return fmt.Errorf("wildcard in token %q must stand alone", t)
		case strings.ContainsAny(t, " \t\r\n"):
			return fmt.Errorf("whitespace in token %q", t)
		}
	}
	return nil
}

// MatchSubject reports whether the literal subject matches pattern, with
// the NATS wildcard rules.
func MatchSubject(pattern, subject string) bool {
	for {
		pTok, pRest, pMore := strings.Cut(pattern, ".")
		sTok, sRest, sMore := strings.Cut(subject, ".")
		switch {
		case pTok == ">":
			return sTok != ""
		case pTok != "*" && pTok != sTok:
			return false
		case sTok == "":
			return false
		}
		if !pMore || !sMore {
			return pMore == sMore
		}
		pattern, subject = pRest, sRest
	}
}
