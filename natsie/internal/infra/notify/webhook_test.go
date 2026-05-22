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

func TestWebhookPost(t *testing.T) {
	var received webhookPayload
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type=%q", r.Header.Get("Content-Type"))
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &received); err != nil {
			t.Fatalf("server decode: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	parsed, _ := neturl.Parse(strings.Replace(srv.URL, "https://", "webhook://", 1) + "/hook")
	wh, err := NewWebhook(parsed)
	if err != nil {
		t.Fatalf("NewWebhook: %v", err)
	}
	wh.Client = tlsTestClient()

	in := Message{
		Title:      "Daily cleanup",
		Body:       "4 stale consumers",
		ManifestID: "m-1",
		Link:       "https://natsie.example.com/manifest/m-1",
	}
	if err := wh.Post(context.Background(), in); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if received.Title != in.Title || received.Body != in.Body ||
		received.ManifestID != in.ManifestID || received.Link != in.Link {
		t.Errorf("payload mismatch:\nwant %+v\ngot  %+v", in, received)
	}
}

func TestWebhookPostNon2xx(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	parsed, _ := neturl.Parse(strings.Replace(srv.URL, "https://", "webhook://", 1) + "/x")
	wh, _ := NewWebhook(parsed)
	wh.Client = tlsTestClient()
	if err := wh.Post(context.Background(), Message{Body: "x"}); err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestWebhookMissingHost(t *testing.T) {
	u, _ := neturl.Parse("webhook:///hook")
	if _, err := NewWebhook(u); err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestWebhookDialThroughDispatch(t *testing.T) {
	n, err := Dial("webhook://hooks.example.com/n8n/abc")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if n.Name() != "webhook" {
		t.Errorf("Name=%q", n.Name())
	}
}
