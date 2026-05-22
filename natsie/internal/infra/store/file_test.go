package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/1995parham/natsie/internal/manifest"
)

func sampleManifest() *manifest.Manifest {
	return &manifest.Manifest{
		Version:     manifest.Version,
		GeneratedAt: time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC),
		Scan:        manifest.ScanInfo{Context: "snapp-js-main-teh1"},
		Entries: []manifest.Entry{{
			Cluster:    "snapp-js-main-teh1",
			Stream:     "rides",
			Consumer:   "stale-consumer",
			Status:     "STALE",
			NumPending: 1000,
		}},
	}
}

func TestFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFile(dir)
	if err != nil {
		t.Fatalf("NewFile: %v", err)
	}
	ctx := context.Background()

	original := sampleManifest()
	if err := s.Put(ctx, "m-1", original); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, "m-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(original, got) {
		t.Fatalf("round trip mismatch:\noriginal: %+v\ngot:      %+v", original, got)
	}
}

func TestFileList(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFile(dir)
	ctx := context.Background()
	for _, id := range []string{"alpha", "bravo", "charlie"} {
		if err := s.Put(ctx, id, sampleManifest()); err != nil {
			t.Fatalf("Put %s: %v", id, err)
		}
	}
	ids, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"alpha", "bravo", "charlie"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("List=%v want %v", ids, want)
	}
}

func TestFileDelete(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFile(dir)
	ctx := context.Background()
	if err := s.Put(ctx, "m-1", sampleManifest()); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Delete(ctx, "m-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, "m-1"); err == nil {
		t.Fatal("Get should fail after Delete")
	}
	// Delete of missing id is not an error.
	if err := s.Delete(ctx, "missing"); err != nil {
		t.Fatalf("Delete missing should be a no-op, got: %v", err)
	}
}

func TestFileRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFile(dir)
	ctx := context.Background()
	bad := []string{
		"../etc/passwd",
		"/absolute",
		".hidden",
		"with/slash",
		"",
	}
	for _, id := range bad {
		if err := s.Put(ctx, id, sampleManifest()); err == nil {
			t.Errorf("Put(%q) should have failed", id)
		}
	}
}

func TestDialFile(t *testing.T) {
	dir := t.TempDir()
	s, err := Dial("file://" + dir)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if !reflect.DeepEqual(s.Name(), "file://"+dir) {
		t.Errorf("Name=%q want file://%s", s.Name(), dir)
	}
}

func TestDialUnknownScheme(t *testing.T) {
	if _, err := Dial("bogus://x"); err == nil {
		t.Fatal("expected error for unknown scheme")
	}
}
