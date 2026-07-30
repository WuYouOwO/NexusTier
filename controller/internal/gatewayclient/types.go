package gatewayclient

import (
	"fmt"

	"github.com/google/uuid"
)

const SchemaVersion = "nexustier.topology.v1"
const maxTimestampMS uint64 = 253_402_300_799_999

type Snapshot struct {
	SchemaVersion string           `json:"schema_version"`
	CollectionID  uuid.UUID        `json:"collection_id"`
	StartedAtMS   uint64           `json:"started_at_ms"`
	CompletedAtMS uint64           `json:"completed_at_ms"`
	CollectedAtMS uint64           `json:"collected_at_ms"`
	Machines      []Machine        `json:"machines"`
	Errors        []TelemetryError `json:"errors"`
}

type Machine struct {
	Session      Session          `json:"session"`
	ObservedAtMS uint64           `json:"observed_at_ms"`
	Instances    []Instance       `json:"instances"`
	Errors       []TelemetryError `json:"errors"`
}

type Session struct {
	MachineID          uuid.UUID   `json:"machine_id"`
	RemoteURL          string      `json:"remote_url"`
	Hostname           string      `json:"hostname"`
	EasyTierVersion    string      `json:"easytier_version"`
	ReportTime         string      `json:"report_time"`
	ConnectedAtMS      uint64      `json:"connected_at_ms"`
	LastHeartbeatAtMS  uint64      `json:"last_heartbeat_at_ms"`
	RunningInstanceIDs []uuid.UUID `json:"running_instance_ids"`
	Device             *Device     `json:"device"`
}

type Device struct {
	OSType       string `json:"os_type"`
	OSVersion    string `json:"os_version"`
	Distribution string `json:"distribution"`
}

type Instance struct {
	InstanceID   uuid.UUID        `json:"instance_id"`
	ObservedAtMS uint64           `json:"observed_at_ms"`
	Node         *Node            `json:"node"`
	Peers        []Peer           `json:"peers"`
	Metrics      []Metric         `json:"metrics"`
	Errors       []TelemetryError `json:"errors"`
}

type Node struct {
	PeerID     uint32   `json:"peer_id"`
	IPv4       string   `json:"ipv4"`
	Hostname   string   `json:"hostname"`
	ProxyCIDRs []string `json:"proxy_cidrs"`
	Listeners  []string `json:"listeners"`
	Version    string   `json:"version"`
}

type Peer struct {
	PeerID          uint32   `json:"peer_id"`
	IPv4            *string  `json:"ipv4"`
	Hostname        string   `json:"hostname"`
	NextHopPeerID   uint32   `json:"next_hop_peer_id"`
	Direct          bool     `json:"direct"`
	PathCost        int32    `json:"path_cost"`
	LatencyMS       *float64 `json:"latency_ms"`
	LossRate        *float64 `json:"loss_rate"`
	RXBytes         *uint64  `json:"rx_bytes"`
	TXBytes         *uint64  `json:"tx_bytes"`
	TunnelProtocols []string `json:"tunnel_protocols"`
	Version         string   `json:"version"`
}

type Metric struct {
	Name   string            `json:"name"`
	Value  uint64            `json:"value"`
	Labels map[string]string `json:"labels"`
}

type TelemetryError struct {
	Code      string `json:"code"`
	Operation string `json:"operation"`
	Message   string `json:"message"`
}

func (snapshot Snapshot) Validate() error {
	if snapshot.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported topology schema %q", snapshot.SchemaVersion)
	}
	if snapshot.CollectionID == uuid.Nil {
		return fmt.Errorf("collection_id must not be nil")
	}
	if snapshot.StartedAtMS > snapshot.CompletedAtMS {
		return fmt.Errorf("started_at_ms must not exceed completed_at_ms")
	}
	if snapshot.CollectedAtMS != snapshot.CompletedAtMS {
		return fmt.Errorf("collected_at_ms must equal completed_at_ms")
	}
	if err := validateTimestamp("started_at_ms", snapshot.StartedAtMS); err != nil {
		return err
	}
	if err := validateTimestamp("completed_at_ms", snapshot.CompletedAtMS); err != nil {
		return err
	}
	for machineIndex, machine := range snapshot.Machines {
		if machine.Session.MachineID == uuid.Nil {
			return fmt.Errorf("machines[%d].session.machine_id must not be nil", machineIndex)
		}
		if machine.ObservedAtMS > snapshot.CompletedAtMS {
			return fmt.Errorf("machines[%d].observed_at_ms exceeds collection completion", machineIndex)
		}
		if err := validateTimestamp(fmt.Sprintf("machines[%d].session.connected_at_ms", machineIndex), machine.Session.ConnectedAtMS); err != nil {
			return err
		}
		if err := validateTimestamp(fmt.Sprintf("machines[%d].session.last_heartbeat_at_ms", machineIndex), machine.Session.LastHeartbeatAtMS); err != nil {
			return err
		}
		for instanceIndex, instance := range machine.Instances {
			if instance.InstanceID == uuid.Nil {
				return fmt.Errorf("machines[%d].instances[%d].instance_id must not be nil", machineIndex, instanceIndex)
			}
			if instance.ObservedAtMS > snapshot.CompletedAtMS {
				return fmt.Errorf("machines[%d].instances[%d].observed_at_ms exceeds collection completion", machineIndex, instanceIndex)
			}
		}
	}
	return nil
}

func validateTimestamp(field string, value uint64) error {
	if value > maxTimestampMS {
		return fmt.Errorf("%s exceeds supported timestamp range", field)
	}
	return nil
}
