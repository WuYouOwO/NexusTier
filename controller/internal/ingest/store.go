package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/WuYouOwO/NexusTier/controller/internal/gatewayclient"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (store *Store) Ingest(ctx context.Context, snapshot gatewayclient.Snapshot) (bool, error) {
	if err := snapshot.Validate(); err != nil {
		return false, fmt.Errorf("validate topology snapshot: %w", err)
	}
	rawPayload, err := json.Marshal(snapshot)
	if err != nil {
		return false, fmt.Errorf("encode raw topology snapshot: %w", err)
	}
	rawPayloadSHA256 := fmt.Sprintf("%x", sha256.Sum256(rawPayload))
	errorCount := countErrors(snapshot)
	status := "complete"
	if errorCount > 0 {
		status = "partial"
	}

	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin topology ingestion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `
		INSERT INTO telemetry_collection_runs (
			collection_id, schema_version, started_at, completed_at, collected_at,
			status, machine_count, error_count, raw_payload, raw_payload_sha256
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (collection_id) DO NOTHING`,
		snapshot.CollectionID,
		snapshot.SchemaVersion,
		millis(snapshot.StartedAtMS),
		millis(snapshot.CompletedAtMS),
		millis(snapshot.CollectedAtMS),
		status,
		len(snapshot.Machines),
		errorCount,
		rawPayload,
		rawPayloadSHA256,
	)
	if err != nil {
		return false, fmt.Errorf("insert collection run: %w", err)
	}
	if result.RowsAffected() == 0 {
		var matches bool
		if err := tx.QueryRow(ctx, `
			SELECT CASE
				WHEN raw_payload_sha256 IS NOT NULL THEN raw_payload_sha256 = $2
				ELSE raw_payload = $3::jsonb
			END
			FROM telemetry_collection_runs
			WHERE collection_id = $1`,
			snapshot.CollectionID,
			rawPayloadSHA256,
			rawPayload,
		).Scan(&matches); err != nil {
			return false, fmt.Errorf("verify duplicate collection: %w", err)
		}
		if !matches {
			return false, fmt.Errorf("collection_id %s was reused with different payload", snapshot.CollectionID)
		}
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit duplicate collection check: %w", err)
		}
		return false, nil
	}

	errorIndex := 0
	if err := insertErrors(ctx, tx, snapshot.CollectionID, "snapshot", uuid.Nil, uuid.Nil, snapshot.Errors, &errorIndex); err != nil {
		return false, err
	}
	for _, machine := range snapshot.Machines {
		if err := upsertMachine(ctx, tx, snapshot.CollectionID, millis(snapshot.CompletedAtMS), machine); err != nil {
			return false, err
		}
		if err := insertErrors(ctx, tx, snapshot.CollectionID, "machine", machine.Session.MachineID, uuid.Nil, machine.Errors, &errorIndex); err != nil {
			return false, err
		}
		for _, instance := range machine.Instances {
			if err := ingestInstance(ctx, tx, snapshot.CollectionID, millis(snapshot.CompletedAtMS), machine.Session.MachineID, instance); err != nil {
				return false, err
			}
			if err := insertErrors(ctx, tx, snapshot.CollectionID, "instance", machine.Session.MachineID, instance.InstanceID, instance.Errors, &errorIndex); err != nil {
				return false, err
			}
		}
		if !hasOperationError(machine.Errors, "list_network_instances") {
			if err := markMissingInstancesInactive(ctx, tx, snapshot.CollectionID, millis(snapshot.CompletedAtMS), machine); err != nil {
				return false, err
			}
		}
	}
	if len(snapshot.Errors) == 0 {
		if err := markMissingMachinesInactive(ctx, tx, snapshot.CollectionID, millis(snapshot.CompletedAtMS), snapshot.Machines); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit topology ingestion: %w", err)
	}
	return true, nil
}

func upsertMachine(ctx context.Context, tx pgx.Tx, collectionID uuid.UUID, collectionCompletedAt time.Time, machine gatewayclient.Machine) error {
	var osType, osVersion, distribution *string
	if machine.Session.Device != nil {
		osType = &machine.Session.Device.OSType
		osVersion = &machine.Session.Device.OSVersion
		distribution = &machine.Session.Device.Distribution
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO machines (
			machine_id, remote_url, hostname, easytier_version, report_time,
			connected_at, last_heartbeat_at, device_os_type, device_os_version,
			device_distribution, active, disappeared_at, last_observed_at,
			last_collection_completed_at, last_collection_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, true, NULL, $11, $12, $13)
		ON CONFLICT (machine_id) DO UPDATE SET
			remote_url = EXCLUDED.remote_url,
			hostname = EXCLUDED.hostname,
			easytier_version = EXCLUDED.easytier_version,
			report_time = EXCLUDED.report_time,
			connected_at = EXCLUDED.connected_at,
			last_heartbeat_at = EXCLUDED.last_heartbeat_at,
			device_os_type = EXCLUDED.device_os_type,
			device_os_version = EXCLUDED.device_os_version,
			device_distribution = EXCLUDED.device_distribution,
			active = true,
			disappeared_at = NULL,
			last_observed_at = EXCLUDED.last_observed_at,
			last_collection_completed_at = EXCLUDED.last_collection_completed_at,
			last_collection_id = EXCLUDED.last_collection_id,
			updated_at = now()
		WHERE machines.last_observed_at < EXCLUDED.last_observed_at
		   OR (machines.last_observed_at = EXCLUDED.last_observed_at
		       AND machines.last_collection_completed_at < EXCLUDED.last_collection_completed_at)`,
		machine.Session.MachineID,
		machine.Session.RemoteURL,
		machine.Session.Hostname,
		machine.Session.EasyTierVersion,
		machine.Session.ReportTime,
		millis(machine.Session.ConnectedAtMS),
		millis(machine.Session.LastHeartbeatAtMS),
		osType,
		osVersion,
		distribution,
		millis(machine.ObservedAtMS),
		collectionCompletedAt,
		collectionID,
	)
	if err != nil {
		return fmt.Errorf("upsert machine %s: %w", machine.Session.MachineID, err)
	}
	return nil
}

func ingestInstance(ctx context.Context, tx pgx.Tx, collectionID uuid.UUID, collectionCompletedAt time.Time, machineID uuid.UUID, instance gatewayclient.Instance) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO network_instances (
			instance_id, machine_id, active, disappeared_at, last_observed_at,
			last_collection_completed_at, last_collection_id
		) VALUES ($1, $2, true, NULL, $3, $4, $5)
		ON CONFLICT (instance_id) DO UPDATE SET
			machine_id = EXCLUDED.machine_id,
			active = true,
			disappeared_at = NULL,
			last_observed_at = EXCLUDED.last_observed_at,
			last_collection_completed_at = EXCLUDED.last_collection_completed_at,
			last_collection_id = EXCLUDED.last_collection_id,
			updated_at = now()
		WHERE network_instances.last_observed_at < EXCLUDED.last_observed_at
		   OR (network_instances.last_observed_at = EXCLUDED.last_observed_at
		       AND network_instances.last_collection_completed_at < EXCLUDED.last_collection_completed_at)`,
		instance.InstanceID,
		machineID,
		millis(instance.ObservedAtMS),
		collectionCompletedAt,
		collectionID,
	)
	if err != nil {
		return fmt.Errorf("upsert instance %s: %w", instance.InstanceID, err)
	}
	if instance.Node != nil {
		if err := upsertNode(ctx, tx, collectionID, collectionCompletedAt, instance); err != nil {
			return err
		}
	}

	peerDetailsComplete := !hasOperationError(instance.Errors, "list_peers")
	peerIDs := make([]int64, 0, len(instance.Peers))
	for _, peer := range instance.Peers {
		peerIDs = append(peerIDs, int64(peer.PeerID))
		if err := upsertPeer(ctx, tx, collectionID, collectionCompletedAt, instance, peer, peerDetailsComplete); err != nil {
			return err
		}
	}
	if !hasOperationError(instance.Errors, "list_routes") {
		if _, err := tx.Exec(ctx, `
			DELETE FROM peer_links_current
			WHERE source_instance_id = $1
			  AND (last_observed_at < $3
			       OR (last_observed_at = $3 AND last_collection_completed_at < $4))
			  AND NOT (destination_peer_id = ANY($2::bigint[]))`,
			instance.InstanceID,
			peerIDs,
			millis(instance.ObservedAtMS),
			collectionCompletedAt,
		); err != nil {
			return fmt.Errorf("remove stale peers for instance %s: %w", instance.InstanceID, err)
		}
	}
	for _, metric := range instance.Metrics {
		labels, err := json.Marshal(metric.Labels)
		if err != nil {
			return fmt.Errorf("encode metric labels for %s: %w", metric.Name, err)
		}
		labelsHash := fmt.Sprintf("%x", sha256.Sum256(labels))
		if _, err := tx.Exec(ctx, `
			INSERT INTO metric_samples (
				collection_id, instance_id, metric_name, labels_hash, labels, value, observed_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			collectionID,
			instance.InstanceID,
			metric.Name,
			labelsHash,
			labels,
			fmt.Sprint(metric.Value),
			millis(instance.ObservedAtMS),
		); err != nil {
			return fmt.Errorf("insert metric %s for instance %s: %w", metric.Name, instance.InstanceID, err)
		}
	}
	return nil
}

func upsertNode(ctx context.Context, tx pgx.Tx, collectionID uuid.UUID, collectionCompletedAt time.Time, instance gatewayclient.Instance) error {
	node := instance.Node
	_, err := tx.Exec(ctx, `
		INSERT INTO nodes (
			instance_id, peer_id, ipv4, hostname, proxy_cidrs, listeners,
			easytier_version, last_observed_at, last_collection_completed_at, last_collection_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (instance_id) DO UPDATE SET
			peer_id = EXCLUDED.peer_id,
			ipv4 = EXCLUDED.ipv4,
			hostname = EXCLUDED.hostname,
			proxy_cidrs = EXCLUDED.proxy_cidrs,
			listeners = EXCLUDED.listeners,
			easytier_version = EXCLUDED.easytier_version,
			last_observed_at = EXCLUDED.last_observed_at,
			last_collection_completed_at = EXCLUDED.last_collection_completed_at,
			last_collection_id = EXCLUDED.last_collection_id,
			updated_at = now()
		WHERE nodes.last_observed_at < EXCLUDED.last_observed_at
		   OR (nodes.last_observed_at = EXCLUDED.last_observed_at
		       AND nodes.last_collection_completed_at < EXCLUDED.last_collection_completed_at)`,
		instance.InstanceID,
		int64(node.PeerID),
		node.IPv4,
		node.Hostname,
		node.ProxyCIDRs,
		node.Listeners,
		node.Version,
		millis(instance.ObservedAtMS),
		collectionCompletedAt,
		collectionID,
	)
	if err != nil {
		return fmt.Errorf("upsert node for instance %s: %w", instance.InstanceID, err)
	}
	return nil
}

func upsertPeer(ctx context.Context, tx pgx.Tx, collectionID uuid.UUID, collectionCompletedAt time.Time, instance gatewayclient.Instance, peer gatewayclient.Peer, detailsComplete bool) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO peer_links_current (
			source_instance_id, destination_peer_id, ipv4, hostname, next_hop_peer_id,
			direct, path_cost, latency_ms, loss_rate, rx_bytes, tx_bytes,
			tunnel_protocols, easytier_version, last_observed_at,
			last_collection_completed_at, last_collection_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (source_instance_id, destination_peer_id) DO UPDATE SET
			ipv4 = EXCLUDED.ipv4,
			hostname = EXCLUDED.hostname,
			next_hop_peer_id = EXCLUDED.next_hop_peer_id,
			direct = EXCLUDED.direct,
			path_cost = EXCLUDED.path_cost,
			latency_ms = CASE WHEN $17 OR NOT EXCLUDED.direct THEN EXCLUDED.latency_ms ELSE peer_links_current.latency_ms END,
			loss_rate = CASE WHEN $17 THEN EXCLUDED.loss_rate ELSE peer_links_current.loss_rate END,
			rx_bytes = CASE WHEN $17 THEN EXCLUDED.rx_bytes ELSE peer_links_current.rx_bytes END,
			tx_bytes = CASE WHEN $17 THEN EXCLUDED.tx_bytes ELSE peer_links_current.tx_bytes END,
			tunnel_protocols = CASE WHEN $17 THEN EXCLUDED.tunnel_protocols ELSE peer_links_current.tunnel_protocols END,
			easytier_version = EXCLUDED.easytier_version,
			last_observed_at = EXCLUDED.last_observed_at,
			last_collection_completed_at = EXCLUDED.last_collection_completed_at,
			last_collection_id = EXCLUDED.last_collection_id,
			updated_at = now()
		WHERE peer_links_current.last_observed_at < EXCLUDED.last_observed_at
		   OR (peer_links_current.last_observed_at = EXCLUDED.last_observed_at
		       AND peer_links_current.last_collection_completed_at < EXCLUDED.last_collection_completed_at)`,
		instance.InstanceID,
		int64(peer.PeerID),
		peer.IPv4,
		peer.Hostname,
		int64(peer.NextHopPeerID),
		peer.Direct,
		peer.PathCost,
		peer.LatencyMS,
		peer.LossRate,
		decimalString(peer.RXBytes),
		decimalString(peer.TXBytes),
		peer.TunnelProtocols,
		peer.Version,
		millis(instance.ObservedAtMS),
		collectionCompletedAt,
		collectionID,
		detailsComplete,
	)
	if err != nil {
		return fmt.Errorf("upsert peer %d for instance %s: %w", peer.PeerID, instance.InstanceID, err)
	}
	return nil
}

func markMissingInstancesInactive(
	ctx context.Context,
	tx pgx.Tx,
	collectionID uuid.UUID,
	collectionCompletedAt time.Time,
	machine gatewayclient.Machine,
) error {
	instanceIDs := make([]uuid.UUID, 0, len(machine.Instances))
	for _, instance := range machine.Instances {
		instanceIDs = append(instanceIDs, instance.InstanceID)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE network_instances
		SET active = false,
			disappeared_at = $3,
			last_collection_completed_at = $3,
			last_collection_id = $4,
			updated_at = now()
		WHERE machine_id = $1
		  AND active
		  AND NOT (instance_id = ANY($2::uuid[]))
		  AND last_collection_completed_at < $3`,
		machine.Session.MachineID,
		instanceIDs,
		collectionCompletedAt,
		collectionID,
	); err != nil {
		return fmt.Errorf("mark missing instances inactive for machine %s: %w", machine.Session.MachineID, err)
	}
	return nil
}

func markMissingMachinesInactive(
	ctx context.Context,
	tx pgx.Tx,
	collectionID uuid.UUID,
	collectionCompletedAt time.Time,
	machines []gatewayclient.Machine,
) error {
	machineIDs := make([]uuid.UUID, 0, len(machines))
	for _, machine := range machines {
		machineIDs = append(machineIDs, machine.Session.MachineID)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE machines
		SET active = false,
			disappeared_at = $2,
			last_collection_completed_at = $2,
			last_collection_id = $3,
			updated_at = now()
		WHERE active
		  AND NOT (machine_id = ANY($1::uuid[]))
		  AND last_collection_completed_at < $2`,
		machineIDs,
		collectionCompletedAt,
		collectionID,
	); err != nil {
		return fmt.Errorf("mark missing machines inactive: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE network_instances
		SET active = false,
			disappeared_at = $2,
			last_collection_completed_at = $2,
			last_collection_id = $3,
			updated_at = now()
		WHERE active
		  AND NOT (machine_id = ANY($1::uuid[]))
		  AND last_collection_completed_at < $2`,
		machineIDs,
		collectionCompletedAt,
		collectionID,
	); err != nil {
		return fmt.Errorf("mark instances of missing machines inactive: %w", err)
	}
	return nil
}

func insertErrors(
	ctx context.Context,
	tx pgx.Tx,
	collectionID uuid.UUID,
	scope string,
	machineID, instanceID uuid.UUID,
	errors []gatewayclient.TelemetryError,
	errorIndex *int,
) error {
	for _, telemetryError := range errors {
		var machineValue, instanceValue *uuid.UUID
		if machineID != uuid.Nil {
			machineValue = &machineID
		}
		if instanceID != uuid.Nil {
			instanceValue = &instanceID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO telemetry_collection_errors (
				collection_id, error_index, scope, machine_id, instance_id, code, operation, message
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			collectionID,
			*errorIndex,
			scope,
			machineValue,
			instanceValue,
			telemetryError.Code,
			telemetryError.Operation,
			telemetryError.Message,
		); err != nil {
			return fmt.Errorf("insert %s collection error: %w", scope, err)
		}
		*errorIndex++
	}
	return nil
}

func countErrors(snapshot gatewayclient.Snapshot) int {
	count := len(snapshot.Errors)
	for _, machine := range snapshot.Machines {
		count += len(machine.Errors)
		for _, instance := range machine.Instances {
			count += len(instance.Errors)
		}
	}
	return count
}

func hasOperationError(errors []gatewayclient.TelemetryError, operation string) bool {
	for _, telemetryError := range errors {
		if telemetryError.Operation == operation {
			return true
		}
	}
	return false
}

func millis(value uint64) time.Time {
	return time.UnixMilli(int64(value)).UTC()
}

func decimalString(value *uint64) any {
	if value == nil {
		return nil
	}
	return fmt.Sprint(*value)
}
