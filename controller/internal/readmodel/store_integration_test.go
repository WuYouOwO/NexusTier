package readmodel_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/WuYouOwO/NexusTier/controller/internal/database"
	"github.com/WuYouOwO/NexusTier/controller/internal/readmodel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCurrentTopologyReturnsStableNestedState(t *testing.T) {
	ctx, pool := testDatabase(t)
	firstMachineID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	secondMachineID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	firstInstanceID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	collectionID := seedTopology(t, ctx, pool, firstMachineID, secondMachineID, firstInstanceID)
	store := readmodel.NewStore(pool)
	active := true

	page, err := store.CurrentTopology(ctx, readmodel.TopologyQuery{Active: &active, Limit: 1})
	if err != nil {
		t.Fatalf("query current topology: %v", err)
	}
	if page.LatestCollection == nil || page.LatestCollection.CollectionID != collectionID {
		t.Fatalf("latest collection = %+v, want %s", page.LatestCollection, collectionID)
	}
	if len(page.LatestErrors) != 1 || page.LatestErrors[0].Code != "rpc_timeout" {
		t.Fatalf("latest errors = %+v, want rpc_timeout", page.LatestErrors)
	}
	if len(page.Machines) != 1 || page.Machines[0].MachineID != firstMachineID {
		t.Fatalf("machines = %+v, want first machine", page.Machines)
	}
	if page.Page.NextCursor == nil || *page.Page.NextCursor != firstMachineID {
		t.Fatalf("next cursor = %v, want %s", page.Page.NextCursor, firstMachineID)
	}
	instances := page.Machines[0].NetworkInstances
	if len(instances) != 1 || instances[0].Node == nil || instances[0].Node.PeerID != 1001 {
		t.Fatalf("instances = %+v, want nested node", instances)
	}
	if len(instances[0].Peers) != 1 || instances[0].Peers[0].PeerID != 1002 {
		t.Fatalf("peers = %+v, want peer 1002", instances[0].Peers)
	}
	if instances[0].Peers[0].RXBytes == nil || *instances[0].Peers[0].RXBytes != 1024 {
		t.Fatalf("rx_bytes = %v, want 1024", instances[0].Peers[0].RXBytes)
	}

	nextPage, err := store.CurrentTopology(ctx, readmodel.TopologyQuery{Active: &active, Cursor: page.Page.NextCursor, Limit: 1})
	if err != nil {
		t.Fatalf("query next topology page: %v", err)
	}
	if len(nextPage.Machines) != 1 || nextPage.Machines[0].MachineID != secondMachineID {
		t.Fatalf("next machines = %+v, want second machine", nextPage.Machines)
	}
}

func TestCurrentTopologyFiltersByMachineID(t *testing.T) {
	ctx, pool := testDatabase(t)
	firstMachineID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	secondMachineID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	seedTopology(t, ctx, pool, firstMachineID, secondMachineID, uuid.MustParse("33333333-3333-4333-8333-333333333333"))

	page, err := readmodel.NewStore(pool).CurrentTopology(ctx, readmodel.TopologyQuery{MachineID: &secondMachineID, Limit: 100})
	if err != nil {
		t.Fatalf("query machine topology: %v", err)
	}
	if len(page.Machines) != 1 || page.Machines[0].MachineID != secondMachineID {
		t.Fatalf("machines = %+v, want selected machine", page.Machines)
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

func seedTopology(t *testing.T, ctx context.Context, pool *pgxpool.Pool, firstMachineID, secondMachineID, instanceID uuid.UUID) uuid.UUID {
	t.Helper()
	collectionID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	observedAt := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO telemetry_collection_runs (
			collection_id, schema_version, started_at, completed_at, collected_at,
			status, machine_count, error_count, raw_payload, raw_payload_sha256
		) VALUES ($1, 'nexustier.topology.v1', $2, $2, $2, 'complete', 2, 0,
		          '{}'::jsonb, repeat('a', 64))`,
		collectionID, observedAt,
	); err != nil {
		t.Fatalf("insert collection: %v", err)
	}
	for _, machine := range []struct {
		id       uuid.UUID
		hostname string
	}{{firstMachineID, "edge-a"}, {secondMachineID, "edge-b"}} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO machines (
				machine_id, remote_url, hostname, easytier_version, report_time,
				connected_at, last_heartbeat_at, device_os_type, device_os_version,
				device_distribution, active, last_observed_at,
				last_collection_completed_at, last_collection_id
			) VALUES ($1, 'udp://203.0.113.10:42000', $2, '2.6.4', '', $3, $3,
			          'linux', '6.12', 'Debian', true, $3, $3, $4)`,
			machine.id, machine.hostname, observedAt, collectionID,
		); err != nil {
			t.Fatalf("insert machine: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO network_instances (
			instance_id, machine_id, active, last_observed_at,
			last_collection_completed_at, last_collection_id
		) VALUES ($1, $2, true, $3, $3, $4)`,
		instanceID, firstMachineID, observedAt, collectionID,
	); err != nil {
		t.Fatalf("insert instance: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO nodes (
			instance_id, peer_id, ipv4, hostname, proxy_cidrs, listeners,
			easytier_version, last_observed_at, last_collection_completed_at,
			last_collection_id
		) VALUES ($1, 1001, '10.10.0.1/24', 'edge-a', '{}', '{}', '2.6.4', $2, $2, $3)`,
		instanceID, observedAt, collectionID,
	); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO peer_links_current (
			source_instance_id, destination_peer_id, ipv4, hostname,
			next_hop_peer_id, direct, path_cost, latency_ms, loss_rate,
			rx_bytes, tx_bytes, tunnel_protocols, easytier_version,
			last_observed_at, last_collection_completed_at, last_collection_id
		) VALUES ($1, 1002, '10.10.0.2/24', 'edge-b', 1002, true, 1, 18.42,
		          0.001, 1024, 2048, '{udp}', '2.6.4', $2, $2, $3)`,
		instanceID, observedAt, collectionID,
	); err != nil {
		t.Fatalf("insert peer: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO telemetry_collection_errors (
			collection_id, error_index, scope, machine_id, instance_id,
			code, operation, message
		) VALUES ($1, 0, 'instance', $2, $3, 'rpc_timeout', 'get_stats', 'deadline elapsed')`,
		collectionID, firstMachineID, instanceID,
	); err != nil {
		t.Fatalf("insert collection error: %v", err)
	}
	return collectionID
}
