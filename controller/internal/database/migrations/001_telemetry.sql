CREATE TABLE telemetry_collection_runs (
    collection_id uuid PRIMARY KEY,
    schema_version text NOT NULL,
    started_at timestamptz NOT NULL,
    completed_at timestamptz NOT NULL,
    collected_at timestamptz NOT NULL,
    status text NOT NULL CHECK (status IN ('complete', 'partial')),
    machine_count integer NOT NULL CHECK (machine_count >= 0),
    error_count integer NOT NULL CHECK (error_count >= 0),
    raw_payload jsonb NOT NULL,
    ingested_at timestamptz NOT NULL DEFAULT now(),
    CHECK (started_at <= completed_at),
    CHECK (collected_at = completed_at)
);

CREATE TABLE machines (
    machine_id uuid PRIMARY KEY,
    remote_url text NOT NULL,
    hostname text NOT NULL,
    easytier_version text NOT NULL,
    report_time text NOT NULL,
    connected_at timestamptz NOT NULL,
    last_heartbeat_at timestamptz NOT NULL,
    device_os_type text,
    device_os_version text,
    device_distribution text,
    active boolean NOT NULL DEFAULT true,
    disappeared_at timestamptz,
    last_observed_at timestamptz NOT NULL,
    last_collection_completed_at timestamptz NOT NULL,
    last_collection_id uuid NOT NULL REFERENCES telemetry_collection_runs(collection_id),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE network_instances (
    instance_id uuid PRIMARY KEY,
    machine_id uuid NOT NULL REFERENCES machines(machine_id),
    active boolean NOT NULL DEFAULT true,
    disappeared_at timestamptz,
    last_observed_at timestamptz NOT NULL,
    last_collection_completed_at timestamptz NOT NULL,
    last_collection_id uuid NOT NULL REFERENCES telemetry_collection_runs(collection_id),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX network_instances_machine_id_idx ON network_instances(machine_id);

CREATE TABLE nodes (
    instance_id uuid PRIMARY KEY REFERENCES network_instances(instance_id) ON DELETE CASCADE,
    peer_id bigint NOT NULL CHECK (peer_id BETWEEN 0 AND 4294967295),
    ipv4 text NOT NULL,
    hostname text NOT NULL,
    proxy_cidrs text[] NOT NULL,
    listeners text[] NOT NULL,
    easytier_version text NOT NULL,
    last_observed_at timestamptz NOT NULL,
    last_collection_completed_at timestamptz NOT NULL,
    last_collection_id uuid NOT NULL REFERENCES telemetry_collection_runs(collection_id),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE peer_links_current (
    source_instance_id uuid NOT NULL REFERENCES network_instances(instance_id) ON DELETE CASCADE,
    destination_peer_id bigint NOT NULL CHECK (destination_peer_id BETWEEN 0 AND 4294967295),
    ipv4 text,
    hostname text NOT NULL,
    next_hop_peer_id bigint NOT NULL CHECK (next_hop_peer_id BETWEEN 0 AND 4294967295),
    direct boolean NOT NULL,
    path_cost integer NOT NULL,
    latency_ms double precision,
    loss_rate double precision,
    rx_bytes numeric(20, 0),
    tx_bytes numeric(20, 0),
    tunnel_protocols text[] NOT NULL,
    easytier_version text NOT NULL,
    last_observed_at timestamptz NOT NULL,
    last_collection_completed_at timestamptz NOT NULL,
    last_collection_id uuid NOT NULL REFERENCES telemetry_collection_runs(collection_id),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source_instance_id, destination_peer_id),
    CHECK (latency_ms IS NULL OR latency_ms >= 0),
    CHECK (loss_rate IS NULL OR loss_rate >= 0),
    CHECK (rx_bytes IS NULL OR rx_bytes >= 0),
    CHECK (tx_bytes IS NULL OR tx_bytes >= 0)
);

CREATE TABLE metric_samples (
    collection_id uuid NOT NULL REFERENCES telemetry_collection_runs(collection_id) ON DELETE CASCADE,
    instance_id uuid NOT NULL REFERENCES network_instances(instance_id) ON DELETE CASCADE,
    metric_name text NOT NULL,
    labels_hash char(64) NOT NULL,
    labels jsonb NOT NULL,
    value numeric(20, 0) NOT NULL CHECK (value >= 0),
    observed_at timestamptz NOT NULL,
    PRIMARY KEY (collection_id, instance_id, metric_name, labels_hash)
);

CREATE INDEX metric_samples_observed_at_idx ON metric_samples(observed_at);

CREATE TABLE telemetry_collection_errors (
    collection_id uuid NOT NULL REFERENCES telemetry_collection_runs(collection_id) ON DELETE CASCADE,
    error_index integer NOT NULL CHECK (error_index >= 0),
    scope text NOT NULL CHECK (scope IN ('snapshot', 'machine', 'instance')),
    machine_id uuid,
    instance_id uuid,
    code text NOT NULL,
    operation text NOT NULL,
    message text NOT NULL,
    PRIMARY KEY (collection_id, error_index)
);
