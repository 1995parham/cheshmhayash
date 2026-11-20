package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"testing"
)

func TestSlackPost(t *testing.T) {
	var received slackPayload

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &received); err != nil {
			t.Fatalf("server json: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	parsed, _ := neturl.Parse(strings.Replace(srv.URL, "https://", "slack://", 1) + "/services/T1/B1/secret")

	sl, err := NewSlack(parsed)
	if err != nil {
		t.Fatalf("NewSlack: %v", err)
	}

	sl.Client = tlsTestClient()

	if err := sl.Post(context.Background(), Message{Title: "T", Body: "B"}); err != nil {
		t.Fatalf("Post: %v", err)
	}

	if !strings.Contains(received.Text, "T") || !strings.Contains(received.Text, "B") {
		t.Errorf("text missing title/body: %q", received.Text)
	}
}

func TestSlackPostNon2xx(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid_token", http.StatusForbidden)
	}))
	defer srv.Close()

	parsed, _ := neturl.Parse(strings.Replace(srv.URL, "https://", "slack://", 1) + "/services/x")
	sl, _ := NewSlack(parsed)

	sl.Client = tlsTestClient()
	if err := sl.Post(context.Background(), Message{Body: "x"}); err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestSlackMissingHost(t *testing.T) {
	u, _ := neturl.Parse("slack:///services/x")
	if _, err := NewSlack(u); err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestSlackDialThroughDispatch(t *testing.T) {
	n, err := Dial("slack://hooks.slack.com/services/T/B/secret")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	if n.Name() != "slack" {
		t.Errorf("Name=%q want slack", n.Name())
	}
}
