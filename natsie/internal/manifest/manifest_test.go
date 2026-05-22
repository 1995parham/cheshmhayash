package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestManifestRoundTrip(t *testing.T) {
	original := &Manifest{
		Version:     Version,
		GeneratedAt: time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC),
		GeneratedBy: "natsie test",
		Scan: ScanInfo{
			Context:     "snapp-js-main-teh1",
			PeerContext: "snapp-js-main-teh2",
			MinPending:  10000,
			MinIdle:     24 * time.Hour,
		},
		Entries: []Entry{
			{
				Cluster:    "snapp-js-main-teh1",
				Stream:     "rides",
				Consumer:   "gabriel-ride-active-group_ride_accepted",
				Status:     "STALE",
				PeerStatus: "ABSENT",
				NumPending: 317252,
				Idle:       216 * time.Hour,
				LastAck:    time.Date(2026, 5, 12, 19, 48, 22, 0, time.UTC),
				Reason:     "stale since may 12 upgrade",
			},
			{
				Cluster:    "snapp-js-main-teh1",
				Stream:     "rides",
				Consumer:   "intercity-teh-1-ride-accepted",
				Status:     "STALE",
				PeerStatus: "ABSENT",
				NumPending: 317259,
				Idle:       216 * time.Hour,
				Skip:       true,
				Reason:     "kept for audit, owner not reached yet",
			},
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	if err := original.Write(path, false); err != nil {
		t.Fatalf("Write: %v", err)
	}

	round, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(original, round) {
		t.Fatalf("round trip mismatch:\noriginal: %+v\nround:    %+v", original, round)
	}
}

func TestWriteRefusesToOverwriteByDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(path, []byte("existing\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	m := &Manifest{Version: Version, GeneratedAt: time.Now()}
	if err := m.Write(path, false); err == nil {
		t.Fatal("expected refusal, got nil")
	}
	if err := m.Write(path, true); err != nil {
		t.Fatalf("force should succeed: %v", err)
	}
}

func TestReadRejectsUnknownVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(path, []byte("version: bogus/v9\ngenerated_at: 2026-05-22T00:00:00Z\nscan:\n  context: x\nentries: []\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("expected version error, got nil")
	}
}
