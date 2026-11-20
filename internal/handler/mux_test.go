package handler

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMuxComponents(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>spa</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)

	tests := []struct {
		name string
		opts Options
		path string
		want int
	}{
		{name: "healthz without components", opts: Options{}, path: "/healthz", want: http.StatusNoContent},
		{name: "api only serves version", opts: Options{API: true}, path: "/api/version", want: http.StatusOK},
		{name: "api only has no spa", opts: Options{API: true}, path: "/", want: http.StatusNotFound},
		{name: "ui only serves spa", opts: Options{StaticDir: dir}, path: "/", want: http.StatusOK},
		{name: "ui only serves spa routes", opts: Options{StaticDir: dir}, path: "/streams", want: http.StatusOK},
		{name: "ui only hides api", opts: Options{StaticDir: dir}, path: "/api/version", want: http.StatusNotFound},
		{name: "both serve version", opts: Options{API: true, StaticDir: dir}, path: "/api/version", want: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := Mux(nil, nil, tt.opts, log, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, nil))
			if rec.Code != tt.want {
				t.Fatalf("GET %s = %d, want %d", tt.path, rec.Code, tt.want)
			}
		})
	}
}
