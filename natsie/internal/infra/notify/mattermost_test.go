package notify

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"testing"
)

// tlsTestClient returns an http.Client that trusts httptest.NewTLSServer's
// self-signed certificate. Used by webhook notifier tests so we can hit a
// real https endpoint locally.
func tlsTestClient() *http.Client {
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec // self-signed in-process test cert
}

func TestMattermostPost(t *testing.T) {
	var received mattermostPayload

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%s want POST", r.Method)
		}

		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type=%q want application/json", ct)
		}

		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &received); err != nil {
			t.Fatalf("server json: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	parsed, err := neturl.Parse(strings.Replace(srv.URL, "https://", "mattermost://", 1) + "/hooks/test-token?channel=nats-cleanup")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	mm, err := NewMattermost(parsed)
	if err != nil {
		t.Fatalf("NewMattermost: %v", err)
	}

	mm.Client = tlsTestClient()

	if err := mm.Post(context.Background(), Message{
		Title:      "Daily cleanup",
		Body:       "4 stale consumers found",
		ManifestID: "m-2026-05-22",
		Link:       "https://natsie.example.com/manifest/m-2026-05-22",
	}); err != nil {
		t.Fatalf("Post: %v", err)
	}

	if received.Channel != "nats-cleanup" {
		t.Errorf("channel=%q want nats-cleanup", received.Channel)
	}

	if received.Username != defaultMattermostUser {
		t.Errorf("username=%q want %s", received.Username, defaultMattermostUser)
	}

	for _, want := range []string{"Daily cleanup", "4 stale consumers", "natsie.example.com"} {
		if !strings.Contains(received.Text, want) {
			t.Errorf("text missing %q\nfull text: %s", want, received.Text)
		}
	}
}

func TestMattermostPostNon2xx(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad webhook token", http.StatusUnauthorized)
	}))
	defer srv.Close()

	parsed, _ := neturl.Parse(strings.Replace(srv.URL, "https://", "mattermost://", 1) + "/hooks/x")
	mm, _ := NewMattermost(parsed)
	mm.Client = tlsTestClient()

	err := mm.Post(context.Background(), Message{Body: "x"})
	if err == nil {
		t.Fatal("expected error on 401")
	}

	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error didn't mention status: %v", err)
	}
}

func TestMattermostMissingHost(t *testing.T) {
	u, _ := neturl.Parse("mattermost:///hooks/x")
	if _, err := NewMattermost(u); err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestMattermostDialThroughDispatch(t *testing.T) {
	n, err := Dial("mattermost://chat.example.com/hooks/abc?channel=test")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	if n.Name() != "mattermost#test" {
		t.Errorf("Name=%q want mattermost#test", n.Name())
	}
}
