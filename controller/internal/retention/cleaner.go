package retention

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	DeleteMetricSamplesBefore(context.Context, time.Time, int) (int64, error)
	PruneCollectionPayloadsBefore(context.Context, time.Time, int) (int64, error)
}

type PostgreSQLStore struct {
	pool *pgxpool.Pool
}

type Status struct {
	Running             bool      `json:"running"`
	Retention           string    `json:"retention"`
	CleanupInterval     string    `json:"cleanup_interval"`
	BatchSize           int       `json:"batch_size"`
	LastStartedAt       time.Time `json:"last_started_at,omitempty"`
	LastCompletedAt     time.Time `json:"last_completed_at,omitempty"`
	LastDeletedSamples  int64     `json:"last_deleted_samples"`
	TotalDeletedSamples uint64    `json:"total_deleted_samples"`
	LastPrunedPayloads  int64     `json:"last_pruned_payloads"`
	TotalPrunedPayloads uint64    `json:"total_pruned_payloads"`
	LastError           string    `json:"last_error,omitempty"`
}

type Cleaner struct {
	store     Store
	logger    *slog.Logger
	retention time.Duration
	interval  time.Duration
	batchSize int
	now       func() time.Time
	mu        sync.RWMutex
	status    Status
}

func New(store Store, logger *slog.Logger, metricRetention, cleanupInterval time.Duration, batchSize int) *Cleaner {
	return &Cleaner{
		store:     store,
		logger:    logger,
		retention: metricRetention,
		interval:  cleanupInterval,
		batchSize: batchSize,
		now:       time.Now,
		status: Status{
			Retention:       metricRetention.String(),
			CleanupInterval: cleanupInterval.String(),
			BatchSize:       batchSize,
		},
	}
}

func NewPostgreSQLStore(pool *pgxpool.Pool) *PostgreSQLStore {
	return &PostgreSQLStore{pool: pool}
}

func (store *PostgreSQLStore) DeleteMetricSamplesBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	result, err := store.pool.Exec(ctx, `
		DELETE FROM metric_samples
		WHERE ctid IN (
			SELECT ctid
			FROM metric_samples
			WHERE observed_at < $1
			ORDER BY observed_at
			LIMIT $2
		)`, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("delete expired metric samples: %w", err)
	}
	return result.RowsAffected(), nil
}

func (store *PostgreSQLStore) PruneCollectionPayloadsBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	result, err := store.pool.Exec(ctx, `
		UPDATE telemetry_collection_runs
		SET raw_payload = 'null'::jsonb,
		    payload_pruned_at = now()
		WHERE collection_id IN (
			SELECT collection_id
			FROM telemetry_collection_runs
			WHERE completed_at < $1
			  AND payload_pruned_at IS NULL
			  AND raw_payload_sha256 IS NOT NULL
			ORDER BY completed_at, collection_id
			LIMIT $2
		)`, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("prune expired collection payloads: %w", err)
	}
	return result.RowsAffected(), nil
}

func (cleaner *Cleaner) Run(ctx context.Context) {
	cleaner.cleanup(ctx)
	ticker := time.NewTicker(cleaner.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleaner.cleanup(ctx)
		}
	}
}

func (cleaner *Cleaner) Status() Status {
	cleaner.mu.RLock()
	defer cleaner.mu.RUnlock()
	return cleaner.status
}

func (cleaner *Cleaner) cleanup(ctx context.Context) {
	startedAt := cleaner.now().UTC()
	cleaner.mu.Lock()
	cleaner.status.Running = true
	cleaner.status.LastStartedAt = startedAt
	cleaner.mu.Unlock()

	var deleted int64
	var pruned int64
	var cleanupError error
	defer func() {
		completedAt := cleaner.now().UTC()
		cleaner.mu.Lock()
		cleaner.status.Running = false
		cleaner.status.LastCompletedAt = completedAt
		cleaner.status.LastDeletedSamples = deleted
		cleaner.status.LastPrunedPayloads = pruned
		cleaner.status.TotalDeletedSamples += uint64(deleted)
		cleaner.status.TotalPrunedPayloads += uint64(pruned)
		if cleanupError != nil {
			cleaner.status.LastError = cleanupError.Error()
		} else {
			cleaner.status.LastError = ""
		}
		cleaner.mu.Unlock()
	}()

	cutoff := startedAt.Add(-cleaner.retention)
	for {
		count, err := cleaner.store.DeleteMetricSamplesBefore(ctx, cutoff, cleaner.batchSize)
		if err != nil {
			cleanupError = err
			cleaner.logger.Error("metric retention cleanup failed", "error", err)
			return
		}
		deleted += count
		if count < int64(cleaner.batchSize) {
			break
		}
	}
	for {
		count, err := cleaner.store.PruneCollectionPayloadsBefore(ctx, cutoff, cleaner.batchSize)
		if err != nil {
			cleanupError = err
			cleaner.logger.Error("collection payload retention cleanup failed", "error", err)
			return
		}
		pruned += count
		if count < int64(cleaner.batchSize) {
			break
		}
	}
	cleaner.logger.Info("metric retention cleanup completed",
		"cutoff", cutoff,
		"deleted_samples", deleted,
		"pruned_payloads", pruned,
	)
}
