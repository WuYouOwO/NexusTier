package poller

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/WuYouOwO/NexusTier/controller/internal/gatewayclient"
	"github.com/google/uuid"
)

type Fetcher interface {
	FetchTopology(context.Context) (gatewayclient.Snapshot, error)
}

type Ingester interface {
	Ingest(context.Context, gatewayclient.Snapshot) (bool, error)
}

type Status struct {
	Running             bool      `json:"running"`
	LastAttemptAt       time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt       time.Time `json:"last_success_at,omitempty"`
	LastCollectionID    uuid.UUID `json:"last_collection_id,omitempty"`
	LastCollectionState string    `json:"last_collection_state,omitempty"`
	LastError           string    `json:"last_error,omitempty"`
	ConsecutiveFailures uint64    `json:"consecutive_failures"`
}

type Worker struct {
	fetcher        Fetcher
	ingester       Ingester
	logger         *slog.Logger
	interval       time.Duration
	jitter         time.Duration
	requestTimeout time.Duration
	mu             sync.RWMutex
	status         Status
}

func New(fetcher Fetcher, ingester Ingester, logger *slog.Logger, interval, jitter, requestTimeout time.Duration) *Worker {
	return &Worker{
		fetcher:        fetcher,
		ingester:       ingester,
		logger:         logger,
		interval:       interval,
		jitter:         jitter,
		requestTimeout: requestTimeout,
	}
}

func (worker *Worker) Run(ctx context.Context) {
	worker.collect(ctx)
	for {
		wait := worker.interval
		if worker.jitter > 0 {
			span := 2 * worker.jitter
			wait = worker.interval - worker.jitter + time.Duration(rand.Int64N(int64(span)))
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			worker.collect(ctx)
		}
	}
}

func (worker *Worker) Status() Status {
	worker.mu.RLock()
	defer worker.mu.RUnlock()
	return worker.status
}

func (worker *Worker) collect(parent context.Context) {
	worker.mu.Lock()
	worker.status.Running = true
	worker.status.LastAttemptAt = time.Now().UTC()
	worker.mu.Unlock()
	defer func() {
		worker.mu.Lock()
		worker.status.Running = false
		worker.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(parent, worker.requestTimeout)
	defer cancel()
	snapshot, err := worker.fetcher.FetchTopology(ctx)
	if err != nil {
		worker.recordFailure("fetch topology", err)
		return
	}
	inserted, err := worker.ingester.Ingest(ctx, snapshot)
	if err != nil {
		worker.recordFailure("ingest topology", err)
		return
	}
	state := "ingested"
	if !inserted {
		state = "duplicate"
	}
	worker.mu.Lock()
	worker.status.LastSuccessAt = time.Now().UTC()
	worker.status.LastCollectionID = snapshot.CollectionID
	worker.status.LastCollectionState = state
	worker.status.LastError = ""
	worker.status.ConsecutiveFailures = 0
	worker.mu.Unlock()
	worker.logger.Info("topology collection completed",
		"collection_id", snapshot.CollectionID,
		"state", state,
		"machines", len(snapshot.Machines),
		"snapshot_errors", len(snapshot.Errors),
	)
}

func (worker *Worker) recordFailure(operation string, err error) {
	worker.mu.Lock()
	worker.status.LastError = operation + ": " + err.Error()
	worker.status.ConsecutiveFailures++
	worker.mu.Unlock()
	worker.logger.Error("topology collection failed", "operation", operation, "error", err)
}
