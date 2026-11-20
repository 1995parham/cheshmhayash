package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadPathAndSchemas(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "custom.toml", `
[[nats]]
name = "a"
url = "nats://a:4222"

[[nats]]
name = "b"
url = "nats://b:4222"

[schemas]
type_header = "X-Type"

[[schemas.sources]]
name = "backend"
type = "descriptor_set"
path = "schemas/backend.binpb"

[[schemas.subjects]]
match = "ride.>"
message = "backend:ride.Event"
`)
	s, err := Load(path, Quiet())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := s.Schemas.Sources[0].Path, filepath.Join(dir, "schemas/backend.binpb"); got != want {
		t.Errorf("source path = %q, want %q (relative to the settings file)", got, want)
	}
	if s.Schemas.TypeHeader != "X-Type" || len(s.Schemas.Subjects) != 1 {
		t.Errorf("schemas = %+v", s.Schemas)
	}

	if c, err := s.Cluster("b"); err != nil || c.URL != "nats://b:4222" {
		t.Errorf("Cluster(b) = %+v, %v", c, err)
	}
	if _, err := s.Cluster(""); err == nil || !strings.Contains(err.Error(), "--cluster") {
		t.Errorf("Cluster(\"\") with two clusters: err = %v", err)
	}
	if _, err := s.Cluster("zzz"); err == nil {
		t.Error("Cluster(zzz) found a cluster")
	}
}

func TestLoadMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.toml")
	if _, err := Load(missing, MustExist(), Quiet()); err == nil {
		t.Error("MustExist accepted a missing file")
	}
	if _, err := Load(missing, Quiet()); err == nil || !strings.Contains(err.Error(), "no NATS clusters") {
		t.Errorf("missing optional file without clusters: err = %v", err)
	}
	s, err := Load(missing, AllowNoClusters(), Quiet())
	if err != nil {
		t.Fatalf("AllowNoClusters: %v", err)
	}
	if len(s.NATS) != 0 {
		t.Errorf("clusters = %v, want none", s.NATS)
	}
}

func TestLoadRejectsBadSchemas(t *testing.T) {
	path := writeFile(t, t.TempDir(), "s.toml", `
[[nats]]
name = "a"
url = "nats://a:4222"

[[schemas.subjects]]
match = "ride.>"
`)
	if _, err := Load(path, Quiet()); err == nil || !strings.Contains(err.Error(), "match and message") {
		t.Errorf("err = %v, want a schema validation error", err)
	}
}

func TestLoadSchemasAndMerge(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "schemas.toml", `
type_header = "Proto-Type"

[[sources]]
name = "map"
type = "proto_dir"
path = "map/pb"

[[subjects]]
match = "driver.location.raw"
message = "map:location.Point"
`)
	extra, err := LoadSchemas(path)
	if err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	if got, want := extra.Sources[0].Path, filepath.Join(dir, "map/pb"); got != want {
		t.Errorf("path = %q, want %q", got, want)
	}

	base := Schemas{
		Sources:  []SchemaSource{{Name: "backend", Type: "descriptor_set", Path: "/b.binpb"}},
		Subjects: []SubjectRule{{Match: "ride.>", Message: "backend:ride.Event"}},
	}
	merged := base.Merge(extra)
	if len(merged.Sources) != 2 || merged.Sources[0].Name != "backend" || merged.Sources[1].Name != "map" {
		t.Errorf("merged sources = %+v", merged.Sources)
	}
	if len(merged.Subjects) != 2 || merged.Subjects[0].Match != "ride.>" {
		t.Errorf("merged subjects = %+v, want base rules first", merged.Subjects)
	}
	if merged.TypeHeader != "Proto-Type" {
		t.Errorf("type header = %q", merged.TypeHeader)
	}
	if len(base.Sources) != 1 {
		t.Error("Merge changed its receiver")
	}
	if (Schemas{}).IsZero() != true || merged.IsZero() {
		t.Error("IsZero is wrong")
	}
}

func TestLoadSchemasErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadSchemas(filepath.Join(dir, "nope.toml")); err == nil {
		t.Error("missing file accepted")
	}
	bad := writeFile(t, dir, "bad.toml", "[[sources]]\nname = \"x\"\n")
	if _, err := LoadSchemas(bad); err == nil || !strings.Contains(err.Error(), "name, type and path") {
		t.Errorf("err = %v", err)
	}
}

func TestTapDefaultsAndValidation(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "s.toml", "[[nats]]\nname = \"a\"\nurl = \"nats://a:4222\"\n\n[tap]\nenabled = true\nallow = [\"ride.>\"]\n")
	s, err := Load(path, Quiet())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !s.Tap.Enabled || s.Tap.MaxRate != 20 || s.Tap.MaxTaps != 10 || s.Tap.Role != TapRoleAdmin || s.Tap.MaxDuration().Minutes() != 10 {
		t.Errorf("tap = %+v, want the defaults with enabled and allow set", s.Tap)
	}

	bad := writeFile(t, dir, "bad.toml", "[[nats]]\nname = \"a\"\nurl = \"nats://a:4222\"\n\n[tap]\nenabled = true\nrole = \"everyone\"\nmax_rate = 0\n")
	if _, err := Load(bad, Quiet()); err == nil || !strings.Contains(err.Error(), "role must be") || !strings.Contains(err.Error(), "max_rate") {
		t.Errorf("err = %v, want role and max_rate errors", err)
	}
}
