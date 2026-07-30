package ingest_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WuYouOwO/NexusTier/controller/internal/database"
	"github.com/WuYouOwO/NexusTier/controller/internal/gatewayclient"
	"github.com/WuYouOwO/NexusTier/controller/internal/ingest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStoreIngestsFixtureIdempotently(t *testing.T) {
	ctx, pool := testDatabase(t)
	snapshot := fixtureSnapshot(t)
	store := ingest.NewStore(pool)

	inserted, err := store.Ingest(ctx, snapshot)
	if err != nil || !inserted {
		t.Fatalf("first ingestion: inserted=%v err=%v", inserted, err)
	}
	inserted, err = store.Ingest(ctx, snapshot)
	if err != nil || inserted {
		t.Fatalf("duplicate ingestion: inserted=%v err=%v", inserted, err)
	}

	assertCount(t, ctx, pool, "telemetry_collection_runs", 1)
	assertCount(t, ctx, pool, "machines", 1)
	assertCount(t, ctx, pool, "network_instances", 1)
	assertCount(t, ctx, pool, "nodes", 1)
	assertCount(t, ctx, pool, "peer_links_current", 1)
	assertCount(t, ctx, pool, "metric_samples", 1)
	assertCount(t, ctx, pool, "telemetry_collection_errors", 1)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeat migrations: %v", err)
	}
}

func TestStoreRejectsCollectionIDReuseWithDifferentPayload(t *testing.T) {
	ctx, pool := testDatabase(t)
	store := ingest.NewStore(pool)
	snapshot := fixtureSnapshot(t)
	if inserted, err := store.Ingest(ctx, snapshot); err != nil || !inserted {
		t.Fatalf("first ingestion: inserted=%v err=%v", inserted, err)
	}
	snapshot.Machines[0].Session.Hostname = "different-host"

	_, err := store.Ingest(ctx, snapshot)
	if err == nil || !strings.Contains(err.Error(), "reused with different payload") {
		t.Fatalf("expected collection reuse error, got %v", err)
	}
}

func TestOlderSnapshotCannotDeleteNewerPeer(t *testing.T) {
	ctx, pool := testDatabase(t)
	store := ingest.NewStore(pool)
	newer := fixtureSnapshot(t)
	newer.CollectionID = uuid.New()
	newer.CompletedAtMS += 2_000
	newer.CollectedAtMS = newer.CompletedAtMS
	newer.Machines[0].ObservedAtMS += 2_000
	newer.Machines[0].Instances[0].ObservedAtMS += 2_000
	secondPeer := newer.Machines[0].Instances[0].Peers[0]
	secondPeer.PeerID = 1003
	secondPeer.Hostname = "newer-peer"
	newer.Machines[0].Instances[0].Peers = append(newer.Machines[0].Instances[0].Peers, secondPeer)
	if _, err := store.Ingest(ctx, newer); err != nil {
		t.Fatalf("ingest newer snapshot: %v", err)
	}
	older := fixtureSnapshot(t)
	older.CollectionID = uuid.New()
	if _, err := store.Ingest(ctx, older); err != nil {
		t.Fatalf("ingest older snapshot: %v", err)
	}

	assertCount(t, ctx, pool, "peer_links_current", 2)
}

func TestPartialPeerFailurePreservesDirectLatency(t *testing.T) {
	ctx, pool := testDatabase(t)
	store := ingest.NewStore(pool)
	initial := fixtureSnapshot(t)
	initial.CollectionID = uuid.New()
	initial.Machines[0].Instances[0].Errors = nil
	if _, err := store.Ingest(ctx, initial); err != nil {
		t.Fatalf("ingest initial snapshot: %v", err)
	}
	partial := fixtureSnapshot(t)
	partial.CollectionID = uuid.New()
	partial.CompletedAtMS += 1_000
	partial.CollectedAtMS = partial.CompletedAtMS
	partial.Machines[0].ObservedAtMS += 1_000
	partial.Machines[0].Instances[0].ObservedAtMS += 1_000
	partial.Machines[0].Instances[0].Peers[0].LatencyMS = nil
	partial.Machines[0].Instances[0].Errors = []gatewayclient.TelemetryError{{
		Code: "rpc_timeout", Operation: "list_peers", Message: "deadline elapsed",
	}}
	if _, err := store.Ingest(ctx, partial); err != nil {
		t.Fatalf("ingest partial snapshot: %v", err)
	}

	var latency float64
	if err := pool.QueryRow(ctx, `SELECT latency_ms FROM peer_links_current`).Scan(&latency); err != nil {
		t.Fatalf("read preserved latency: %v", err)
	}
	if latency != 18.42 {
		t.Fatalf("latency = %v, want 18.42", latency)
	}
}

func TestCompleteSnapshotMarksMissingEntitiesInactive(t *testing.T) {
	ctx, pool := testDatabase(t)
	store := ingest.NewStore(pool)
	initial := fixtureSnapshot(t)
	initial.CollectionID = uuid.New()
	if _, err := store.Ingest(ctx, initial); err != nil {
		t.Fatalf("ingest initial snapshot: %v", err)
	}
	empty := gatewayclient.Snapshot{
		SchemaVersion: gatewayclient.SchemaVersion,
		CollectionID:  uuid.New(),
		StartedAtMS:   initial.CompletedAtMS + 1_000,
		CompletedAtMS: initial.CompletedAtMS + 2_000,
		CollectedAtMS: initial.CompletedAtMS + 2_000,
		Machines:      []gatewayclient.Machine{},
		Errors:        []gatewayclient.TelemetryError{},
	}
	if _, err := store.Ingest(ctx, empty); err != nil {
		t.Fatalf("ingest empty complete snapshot: %v", err)
	}

	assertBoolean(t, ctx, pool, `SELECT active FROM machines`, false)
	assertBoolean(t, ctx, pool, `SELECT active FROM network_instances`, false)
}

func assertCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}

func assertBoolean(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, want bool) {
	t.Helper()
	var got bool
	if err := pool.QueryRow(ctx, query).Scan(&got); err != nil {
		t.Fatalf("query boolean: %v", err)
	}
	if got != want {
		t.Fatalf("boolean = %v, want %v", got, want)
	}
}

func testDatabase(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	databaseURL := os.Getenv("NEXUSTIER_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("NEXUSTIER_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE telemetry_collection_runs CASCADE`); err != nil {
		t.Fatalf("reset telemetry tables: %v", err)
	}
	return ctx, pool
}

func fixtureSnapshot(t *testing.T) gatewayclient.Snapshot {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "contracts", "fixtures", "topology-v1.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var snapshot gatewayclient.Snapshot
	if err := json.Unmarshal(fixture, &snapshot); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return snapshot
}
