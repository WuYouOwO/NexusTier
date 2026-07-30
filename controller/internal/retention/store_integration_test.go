package retention_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/WuYouOwO/NexusTier/controller/internal/database"
	"github.com/WuYouOwO/NexusTier/controller/internal/retention"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgreSQLStoreDeletesMetricsAndPrunesPayloadsBeforeCutoff(t *testing.T) {
	ctx, pool := testDatabase(t)
	oldCollection := uuid.New()
	newCollection := uuid.New()
	instanceID := uuid.New()
	machineID := uuid.New()
	cutoff := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	seedRetentionData(t, ctx, pool, oldCollection, newCollection, machineID, instanceID, cutoff)
	store := retention.NewPostgreSQLStore(pool)

	deleted, err := store.DeleteMetricSamplesBefore(ctx, cutoff, 100)
	if err != nil {
		t.Fatalf("delete old metrics: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted metrics = %d, want 1", deleted)
	}
	pruned, err := store.PruneCollectionPayloadsBefore(ctx, cutoff, 100)
	if err != nil {
		t.Fatalf("prune old payloads: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("pruned payloads = %d, want 1", pruned)
	}

	var metricCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metric_samples`).Scan(&metricCount); err != nil {
		t.Fatalf("count metrics: %v", err)
	}
	if metricCount != 1 {
		t.Fatalf("metric count = %d, want 1", metricCount)
	}
	var oldPayload string
	var oldPrunedAt *time.Time
	var fingerprint string
	if err := pool.QueryRow(ctx, `
		SELECT raw_payload::text, payload_pruned_at, raw_payload_sha256
		FROM telemetry_collection_runs
		WHERE collection_id = $1`, oldCollection).Scan(&oldPayload, &oldPrunedAt, &fingerprint); err != nil {
		t.Fatalf("read old collection: %v", err)
	}
	if oldPayload != "null" || oldPrunedAt == nil || len(fingerprint) != 64 {
		t.Fatalf("old collection payload=%q pruned_at=%v fingerprint=%q", oldPayload, oldPrunedAt, fingerprint)
	}
	var newPayload string
	if err := pool.QueryRow(ctx, `SELECT raw_payload::text FROM telemetry_collection_runs WHERE collection_id = $1`, newCollection).Scan(&newPayload); err != nil {
		t.Fatalf("read new collection: %v", err)
	}
	if newPayload == "null" {
		t.Fatal("new collection payload was pruned")
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

func seedRetentionData(t *testing.T, ctx context.Context, pool *pgxpool.Pool, oldCollection, newCollection, machineID, instanceID uuid.UUID, cutoff time.Time) {
	t.Helper()
	for _, collection := range []struct {
		id          uuid.UUID
		completedAt time.Time
	}{{oldCollection, cutoff.Add(-time.Hour)}, {newCollection, cutoff.Add(time.Hour)}} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO telemetry_collection_runs (
				collection_id, schema_version, started_at, completed_at, collected_at,
				status, machine_count, error_count, raw_payload, raw_payload_sha256
			) VALUES ($1, 'nexustier.topology.v1', $2, $2, $2, 'complete', 1, 0,
			          '{"metrics": [1]}'::jsonb, repeat('a', 64))`,
			collection.id, collection.completedAt); err != nil {
			t.Fatalf("insert collection: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO machines (
			machine_id, remote_url, hostname, easytier_version, report_time,
			connected_at, last_heartbeat_at, active, last_observed_at,
			last_collection_completed_at, last_collection_id
		) VALUES ($1, 'udp://127.0.0.1:1', 'test', '2.6.4', '', $2, $2,
		          true, $2, $2, $3)`, machineID, cutoff, newCollection); err != nil {
		t.Fatalf("insert machine: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO network_instances (
			instance_id, machine_id, active, last_observed_at,
			last_collection_completed_at, last_collection_id
		) VALUES ($1, $2, true, $3, $3, $4)`, instanceID, machineID, cutoff, newCollection); err != nil {
		t.Fatalf("insert instance: %v", err)
	}
	for _, sample := range []struct {
		collectionID uuid.UUID
		observedAt   time.Time
	}{{oldCollection, cutoff.Add(-time.Hour)}, {newCollection, cutoff.Add(time.Hour)}} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO metric_samples (
				collection_id, instance_id, metric_name, labels_hash, labels, value, observed_at
			) VALUES ($1, $2, 'bytes_rx', $3, '{}', 1, $4)`,
			sample.collectionID, instanceID, sample.collectionID.String()+"0000000000000000000000000000", sample.observedAt); err != nil {
			t.Fatalf("insert metric: %v", err)
		}
	}
}
