package readmodel

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

type TopologyQuery struct {
	Active    *bool
	MachineID *uuid.UUID
	Cursor    *uuid.UUID
	Limit     int
}

type TopologyPage struct {
	GeneratedAt      time.Time          `json:"generated_at"`
	LatestCollection *CollectionSummary `json:"latest_collection"`
	LatestErrors     []CollectionError  `json:"latest_errors"`
	Machines         []Machine          `json:"machines"`
	Page             Page               `json:"page"`
}

type Page struct {
	Limit      int        `json:"limit"`
	NextCursor *uuid.UUID `json:"next_cursor"`
}

type CollectionSummary struct {
	CollectionID uuid.UUID `json:"collection_id"`
	Status       string    `json:"status"`
	MachineCount int       `json:"machine_count"`
	ErrorCount   int       `json:"error_count"`
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  time.Time `json:"completed_at"`
	IngestedAt   time.Time `json:"ingested_at"`
}

type CollectionError struct {
	Index      int        `json:"index"`
	Scope      string     `json:"scope"`
	MachineID  *uuid.UUID `json:"machine_id"`
	InstanceID *uuid.UUID `json:"instance_id"`
	Code       string     `json:"code"`
	Operation  string     `json:"operation"`
	Message    string     `json:"message"`
}

type Machine struct {
	MachineID        uuid.UUID  `json:"machine_id"`
	RemoteURL        string     `json:"remote_url"`
	Hostname         string     `json:"hostname"`
	EasyTierVersion  string     `json:"easytier_version"`
	ReportTime       string     `json:"report_time"`
	ConnectedAt      time.Time  `json:"connected_at"`
	LastHeartbeatAt  time.Time  `json:"last_heartbeat_at"`
	Device           *Device    `json:"device"`
	Active           bool       `json:"active"`
	DisappearedAt    *time.Time `json:"disappeared_at"`
	LastObservedAt   time.Time  `json:"last_observed_at"`
	LastCollectionID uuid.UUID  `json:"last_collection_id"`
	NetworkInstances []Instance `json:"network_instances"`
}

type Device struct {
	OSType       string `json:"os_type"`
	OSVersion    string `json:"os_version"`
	Distribution string `json:"distribution"`
}

type Instance struct {
	InstanceID       uuid.UUID  `json:"instance_id"`
	Active           bool       `json:"active"`
	DisappearedAt    *time.Time `json:"disappeared_at"`
	LastObservedAt   time.Time  `json:"last_observed_at"`
	LastCollectionID uuid.UUID  `json:"last_collection_id"`
	Node             *Node      `json:"node"`
	Peers            []Peer     `json:"peers"`
}

type Node struct {
	PeerID          uint32    `json:"peer_id"`
	IPv4            string    `json:"ipv4"`
	Hostname        string    `json:"hostname"`
	ProxyCIDRs      []string  `json:"proxy_cidrs"`
	Listeners       []string  `json:"listeners"`
	EasyTierVersion string    `json:"easytier_version"`
	LastObservedAt  time.Time `json:"last_observed_at"`
}

type Peer struct {
	PeerID          uint32    `json:"peer_id"`
	IPv4            *string   `json:"ipv4"`
	Hostname        string    `json:"hostname"`
	NextHopPeerID   uint32    `json:"next_hop_peer_id"`
	Direct          bool      `json:"direct"`
	PathCost        int32     `json:"path_cost"`
	LatencyMS       *float64  `json:"latency_ms"`
	LossRate        *float64  `json:"loss_rate"`
	RXBytes         *uint64   `json:"rx_bytes"`
	TXBytes         *uint64   `json:"tx_bytes"`
	TunnelProtocols []string  `json:"tunnel_protocols"`
	EasyTierVersion string    `json:"easytier_version"`
	LastObservedAt  time.Time `json:"last_observed_at"`
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (store *Store) CurrentTopology(ctx context.Context, query TopologyQuery) (TopologyPage, error) {
	if query.Limit <= 0 {
		query.Limit = 100
	}
	page := TopologyPage{
		GeneratedAt:  time.Now().UTC(),
		LatestErrors: []CollectionError{},
		Machines:     []Machine{},
		Page:         Page{Limit: query.Limit},
	}
	latest, err := store.latestCollection(ctx)
	if err != nil {
		return TopologyPage{}, err
	}
	page.LatestCollection = latest
	if latest != nil {
		page.LatestErrors, err = store.collectionErrors(ctx, latest.CollectionID)
		if err != nil {
			return TopologyPage{}, err
		}
	}

	rows, err := store.pool.Query(ctx, `
		SELECT machine_id, remote_url, hostname, easytier_version, report_time,
		       connected_at, last_heartbeat_at, device_os_type, device_os_version,
		       device_distribution, active, disappeared_at, last_observed_at,
		       last_collection_id
		FROM machines
		WHERE ($1::boolean IS NULL OR active = $1)
		  AND ($2::uuid IS NULL OR machine_id = $2)
		  AND ($3::uuid IS NULL OR machine_id > $3)
		ORDER BY machine_id
		LIMIT $4`, query.Active, query.MachineID, query.Cursor, query.Limit+1)
	if err != nil {
		return TopologyPage{}, fmt.Errorf("query machines: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var machine Machine
		var osType, osVersion, distribution *string
		if err := rows.Scan(
			&machine.MachineID,
			&machine.RemoteURL,
			&machine.Hostname,
			&machine.EasyTierVersion,
			&machine.ReportTime,
			&machine.ConnectedAt,
			&machine.LastHeartbeatAt,
			&osType,
			&osVersion,
			&distribution,
			&machine.Active,
			&machine.DisappearedAt,
			&machine.LastObservedAt,
			&machine.LastCollectionID,
		); err != nil {
			return TopologyPage{}, fmt.Errorf("scan machine: %w", err)
		}
		if osType != nil || osVersion != nil || distribution != nil {
			machine.Device = &Device{}
			if osType != nil {
				machine.Device.OSType = *osType
			}
			if osVersion != nil {
				machine.Device.OSVersion = *osVersion
			}
			if distribution != nil {
				machine.Device.Distribution = *distribution
			}
		}
		machine.NetworkInstances = []Instance{}
		page.Machines = append(page.Machines, machine)
	}
	if err := rows.Err(); err != nil {
		return TopologyPage{}, fmt.Errorf("iterate machines: %w", err)
	}
	if len(page.Machines) > query.Limit {
		next := page.Machines[query.Limit-1].MachineID
		page.Page.NextCursor = &next
		page.Machines = page.Machines[:query.Limit]
	}
	if err := store.loadInstances(ctx, page.Machines, query.Active); err != nil {
		return TopologyPage{}, err
	}
	return page, nil
}

func (store *Store) collectionErrors(ctx context.Context, collectionID uuid.UUID) ([]CollectionError, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT error_index, scope, machine_id, instance_id, code, operation, message
		FROM telemetry_collection_errors
		WHERE collection_id = $1
		ORDER BY error_index`, collectionID)
	if err != nil {
		return nil, fmt.Errorf("query collection errors: %w", err)
	}
	defer rows.Close()
	errors := []CollectionError{}
	for rows.Next() {
		var collectionError CollectionError
		if err := rows.Scan(
			&collectionError.Index,
			&collectionError.Scope,
			&collectionError.MachineID,
			&collectionError.InstanceID,
			&collectionError.Code,
			&collectionError.Operation,
			&collectionError.Message,
		); err != nil {
			return nil, fmt.Errorf("scan collection error: %w", err)
		}
		errors = append(errors, collectionError)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate collection errors: %w", err)
	}
	return errors, nil
}

func (store *Store) latestCollection(ctx context.Context) (*CollectionSummary, error) {
	var collection CollectionSummary
	err := store.pool.QueryRow(ctx, `
		SELECT collection_id, status, machine_count, error_count,
		       started_at, completed_at, ingested_at
		FROM telemetry_collection_runs
		ORDER BY completed_at DESC, collection_id DESC
		LIMIT 1`).Scan(
		&collection.CollectionID,
		&collection.Status,
		&collection.MachineCount,
		&collection.ErrorCount,
		&collection.StartedAt,
		&collection.CompletedAt,
		&collection.IngestedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query latest collection: %w", err)
	}
	return &collection, nil
}

func (store *Store) loadInstances(ctx context.Context, machines []Machine, active *bool) error {
	if len(machines) == 0 {
		return nil
	}
	machineIDs := make([]uuid.UUID, len(machines))
	machineIndex := make(map[uuid.UUID]int, len(machines))
	for index := range machines {
		machineIDs[index] = machines[index].MachineID
		machineIndex[machines[index].MachineID] = index
	}

	rows, err := store.pool.Query(ctx, `
		SELECT i.machine_id, i.instance_id, i.active, i.disappeared_at,
		       i.last_observed_at, i.last_collection_id,
		       n.peer_id, n.ipv4, n.hostname, n.proxy_cidrs, n.listeners,
		       n.easytier_version, n.last_observed_at
		FROM network_instances AS i
		LEFT JOIN nodes AS n ON n.instance_id = i.instance_id
		WHERE i.machine_id = ANY($1::uuid[])
		  AND ($2::boolean IS NULL OR i.active = $2)
		ORDER BY i.machine_id, i.instance_id`, machineIDs, active)
	if err != nil {
		return fmt.Errorf("query network instances: %w", err)
	}
	defer rows.Close()

	instanceLocations := make(map[uuid.UUID][2]int)
	instanceIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var machineID uuid.UUID
		var instance Instance
		var peerID *int64
		var ipv4, hostname, version *string
		var proxyCIDRs, listeners []string
		var nodeObservedAt *time.Time
		if err := rows.Scan(
			&machineID,
			&instance.InstanceID,
			&instance.Active,
			&instance.DisappearedAt,
			&instance.LastObservedAt,
			&instance.LastCollectionID,
			&peerID,
			&ipv4,
			&hostname,
			&proxyCIDRs,
			&listeners,
			&version,
			&nodeObservedAt,
		); err != nil {
			return fmt.Errorf("scan network instance: %w", err)
		}
		if peerID != nil {
			instance.Node = &Node{
				PeerID:          uint32(*peerID),
				IPv4:            valueOrEmpty(ipv4),
				Hostname:        valueOrEmpty(hostname),
				ProxyCIDRs:      nonNilStrings(proxyCIDRs),
				Listeners:       nonNilStrings(listeners),
				EasyTierVersion: valueOrEmpty(version),
				LastObservedAt:  *nodeObservedAt,
			}
		}
		instance.Peers = []Peer{}
		machinePosition := machineIndex[machineID]
		instancePosition := len(machines[machinePosition].NetworkInstances)
		machines[machinePosition].NetworkInstances = append(machines[machinePosition].NetworkInstances, instance)
		instanceLocations[instance.InstanceID] = [2]int{machinePosition, instancePosition}
		instanceIDs = append(instanceIDs, instance.InstanceID)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate network instances: %w", err)
	}
	return store.loadPeers(ctx, machines, instanceIDs, instanceLocations)
}

func (store *Store) loadPeers(ctx context.Context, machines []Machine, instanceIDs []uuid.UUID, locations map[uuid.UUID][2]int) error {
	if len(instanceIDs) == 0 {
		return nil
	}
	rows, err := store.pool.Query(ctx, `
		SELECT source_instance_id, destination_peer_id, ipv4, hostname,
		       next_hop_peer_id, direct, path_cost, latency_ms, loss_rate,
		       rx_bytes::text, tx_bytes::text, tunnel_protocols,
		       easytier_version, last_observed_at
		FROM peer_links_current
		WHERE source_instance_id = ANY($1::uuid[])
		ORDER BY source_instance_id, destination_peer_id`, instanceIDs)
	if err != nil {
		return fmt.Errorf("query peer links: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var instanceID uuid.UUID
		var peerID, nextHopPeerID int64
		var peer Peer
		var rxBytes, txBytes *string
		if err := rows.Scan(
			&instanceID,
			&peerID,
			&peer.IPv4,
			&peer.Hostname,
			&nextHopPeerID,
			&peer.Direct,
			&peer.PathCost,
			&peer.LatencyMS,
			&peer.LossRate,
			&rxBytes,
			&txBytes,
			&peer.TunnelProtocols,
			&peer.EasyTierVersion,
			&peer.LastObservedAt,
		); err != nil {
			return fmt.Errorf("scan peer link: %w", err)
		}
		peer.PeerID = uint32(peerID)
		peer.NextHopPeerID = uint32(nextHopPeerID)
		peer.TunnelProtocols = nonNilStrings(peer.TunnelProtocols)
		if peer.RXBytes, err = parseOptionalUint64(rxBytes); err != nil {
			return fmt.Errorf("parse peer rx_bytes: %w", err)
		}
		if peer.TXBytes, err = parseOptionalUint64(txBytes); err != nil {
			return fmt.Errorf("parse peer tx_bytes: %w", err)
		}
		location := locations[instanceID]
		instance := &machines[location[0]].NetworkInstances[location[1]]
		instance.Peers = append(instance.Peers, peer)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate peer links: %w", err)
	}
	return nil
}

func parseOptionalUint64(value *string) (*uint64, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := strconv.ParseUint(*value, 10, 64)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
