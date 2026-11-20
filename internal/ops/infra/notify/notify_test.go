package notify

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestStdoutPost(t *testing.T) {
	var buf bytes.Buffer

	n := &Stdout{W: &buf}
	if err := n.Post(context.Background(), Message{
		Title:      "Test title",
		Body:       "body line",
		ManifestID: "m-1",
		Link:       "http://localhost/m-1",
	}); err != nil {
		t.Fatalf("Post: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"Test title", "body line", "m-1", "http://localhost/m-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestDial(t *testing.T) {
	t.Run("stdout", func(t *testing.T) {
		n, err := Dial("stdout://")
		if err != nil {
			t.Fatalf("Dial: %v", err)
		}

		if n.Name() != "stdout" {
			t.Errorf("Name=%q want stdout", n.Name())
		}
	})
	t.Run("unknown scheme errors", func(t *testing.T) {
		if _, err := Dial("bogus://x"); err == nil {
			t.Fatal("expected error for unknown scheme")
		}
	})
	t.Run("malformed url errors", func(t *testing.T) {
		if _, err := Dial("::::"); err == nil {
			t.Fatal("expected error for malformed url")
		}
	})
}
