package cleanup

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/nats-io/jsm.go"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/1995parham/natsie/internal/manifest"
	"github.com/1995parham/natsie/internal/protect"
)

// startJetStream boots an in-process JetStream server and returns a raw
// connection plus a Connector that hands it to Apply.
func startJetStream(t *testing.T) (*nats.Conn, Connector) {
	t.Helper()

	srv, err := natsserver.NewServer(&natsserver.Options{
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	go srv.Start()

	t.Cleanup(srv.Shutdown)

	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("server not ready")
	}

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	t.Cleanup(nc.Close)

	return nc, func(string) (*nats.Conn, func(), error) { return nc, func() {}, nil }
}

// mkConsumer creates a durable pull consumer, optionally carrying JetStream
// metadata — which is how a declaring controller marks it as its own.
func mkConsumer(t *testing.T, mgr *jsm.Manager, stream, name string, meta map[string]string) {
	t.Helper()

	opts := []jsm.ConsumerOption{jsm.DurableName(name), jsm.AcknowledgeExplicit()}
	if meta != nil {
		opts = append(opts, jsm.ConsumerMetadata(meta))
	}

	if _, err := mgr.NewConsumer(stream, opts...); err != nil {
		t.Fatalf("create consumer %s: %v", name, err)
	}
}

func exists(t *testing.T, mgr *jsm.Manager, stream, name string) bool {
	t.Helper()

	names, err := mgr.ConsumerNames(stream)
	if err != nil {
		t.Fatalf("consumer names: %v", err)
	}

	return slices.Contains(names, name)
}

// The guarantee that matters: a consumer declared by an external controller
// survives an apply that explicitly names it for deletion, while its
// unmanaged neighbour in the same manifest is deleted. This is what stops
// natsie fighting a NACK reconciler.
func TestApplyRefusesToDeleteManagedConsumer(t *testing.T) {
	nc, connect := startJetStream(t)

	mgr, err := jsm.New(nc)
	if err != nil {
		t.Fatalf("jsm.New: %v", err)
	}

	if _, err := mgr.NewStream("rides", jsm.Subjects("rides.>"), jsm.MemoryStorage()); err != nil {
		t.Fatalf("create stream: %v", err)
	}

	mkConsumer(t, mgr, "rides", "declared", map[string]string{protect.DefaultMetadataKey: "true"})
	mkConsumer(t, mgr, "rides", "orphan", nil)

	guard, err := protect.New(protect.Config{})
	if err != nil {
		t.Fatalf("protect.New: %v", err)
	}

	// Both consumers are named for deletion — the manifest is hostile on
	// purpose, standing in for a hand-edited file or a stale proposal.
	m := &manifest.Manifest{
		Version:     manifest.Version,
		GeneratedAt: time.Now().UTC(),
		Entries: []manifest.Entry{
			{Cluster: "test", Stream: "rides", Consumer: "declared"},
			{Cluster: "test", Stream: "rides", Consumer: "orphan"},
		},
	}

	result, err := Apply(context.Background(), m, Options{Connect: connect, Protect: guard})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if result.Protected != 1 {
		t.Errorf("Protected = %d, want 1 (summary: %s)", result.Protected, result.Summary())
	}

	if result.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1 (summary: %s)", result.Deleted, result.Summary())
	}

	if !exists(t, mgr, "rides", "declared") {
		t.Error("managed consumer was deleted — the protection veto did not hold")
	}

	if exists(t, mgr, "rides", "orphan") {
		t.Error("unmanaged consumer survived; protection is over-matching")
	}
}

// Pattern rules have to work on consumers that carry no metadata at all,
// since that is the case they exist to cover.
func TestApplyRefusesPatternProtectedConsumer(t *testing.T) {
	nc, connect := startJetStream(t)

	mgr, err := jsm.New(nc)
	if err != nil {
		t.Fatalf("jsm.New: %v", err)
	}

	if _, err := mgr.NewStream("billing", jsm.Subjects("billing.>"), jsm.MemoryStorage()); err != nil {
		t.Fatalf("create stream: %v", err)
	}

	mkConsumer(t, mgr, "billing", "tf-ledger", nil)

	guard, err := protect.New(protect.Config{Patterns: []protect.Pattern{{Consumer: "tf-*"}}})
	if err != nil {
		t.Fatalf("protect.New: %v", err)
	}

	m := &manifest.Manifest{
		Version:     manifest.Version,
		GeneratedAt: time.Now().UTC(),
		Entries:     []manifest.Entry{{Cluster: "test", Stream: "billing", Consumer: "tf-ledger"}},
	}

	result, err := Apply(context.Background(), m, Options{Connect: connect, Protect: guard})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if result.Protected != 1 || result.Deleted != 0 {
		t.Errorf("want 1 protected / 0 deleted, got %s", result.Summary())
	}

	if !exists(t, mgr, "billing", "tf-ledger") {
		t.Error("pattern-protected consumer was deleted")
	}
}

// Without a Protector nothing is protected — the veto must be something the
// caller opts into, not an accident of construction.
func TestApplyWithoutProtectorDeletes(t *testing.T) {
	nc, connect := startJetStream(t)

	mgr, err := jsm.New(nc)
	if err != nil {
		t.Fatalf("jsm.New: %v", err)
	}

	if _, err := mgr.NewStream("rides", jsm.Subjects("rides.>"), jsm.MemoryStorage()); err != nil {
		t.Fatalf("create stream: %v", err)
	}

	mkConsumer(t, mgr, "rides", "declared", map[string]string{protect.DefaultMetadataKey: "true"})

	m := &manifest.Manifest{
		Version:     manifest.Version,
		GeneratedAt: time.Now().UTC(),
		Entries:     []manifest.Entry{{Cluster: "test", Stream: "rides", Consumer: "declared"}},
	}

	result, err := Apply(context.Background(), m, Options{Connect: connect})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if result.Deleted != 1 {
		t.Errorf("want the consumer deleted with no Protector, got %s", result.Summary())
	}
}

// Dry run must not touch anything, protected or not.
func TestApplyDryRunDeletesNothing(t *testing.T) {
	nc, connect := startJetStream(t)

	mgr, err := jsm.New(nc)
	if err != nil {
		t.Fatalf("jsm.New: %v", err)
	}

	if _, err := mgr.NewStream("rides", jsm.Subjects("rides.>"), jsm.MemoryStorage()); err != nil {
		t.Fatalf("create stream: %v", err)
	}

	mkConsumer(t, mgr, "rides", "orphan", nil)

	m := &manifest.Manifest{
		Version:     manifest.Version,
		GeneratedAt: time.Now().UTC(),
		Entries:     []manifest.Entry{{Cluster: "test", Stream: "rides", Consumer: "orphan"}},
	}

	if _, err := Apply(context.Background(), m, Options{Connect: connect, DryRun: true}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if !exists(t, mgr, "rides", "orphan") {
		t.Error("dry run deleted a consumer")
	}
}
