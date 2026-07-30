package retention

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeStore struct {
	metricCounts  []int64
	payloadCounts []int64
	err           error
	cutoffs       []time.Time
	limits        []int
}

func (store *fakeStore) DeleteMetricSamplesBefore(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	store.cutoffs = append(store.cutoffs, cutoff)
	store.limits = append(store.limits, limit)
	if store.err != nil {
		return 0, store.err
	}
	count := store.metricCounts[0]
	store.metricCounts = store.metricCounts[1:]
	return count, nil
}

func (store *fakeStore) PruneCollectionPayloadsBefore(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	store.cutoffs = append(store.cutoffs, cutoff)
	store.limits = append(store.limits, limit)
	if store.err != nil {
		return 0, store.err
	}
	count := store.payloadCounts[0]
	store.payloadCounts = store.payloadCounts[1:]
	return count, nil
}

func TestCleanupDeletesAllExpiredBatches(t *testing.T) {
	store := &fakeStore{metricCounts: []int64{2, 2, 1}, payloadCounts: []int64{2, 1}}
	cleaner := New(store, testLogger(), 24*time.Hour, time.Hour, 2)
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	cleaner.now = func() time.Time { return now }

	cleaner.cleanup(context.Background())

	status := cleaner.Status()
	if status.Running || status.LastDeletedSamples != 5 || status.TotalDeletedSamples != 5 ||
		status.LastPrunedPayloads != 3 || status.TotalPrunedPayloads != 3 || status.LastError != "" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if len(store.cutoffs) != 5 || !store.cutoffs[0].Equal(now.Add(-24*time.Hour)) {
		t.Fatalf("cutoffs = %v", store.cutoffs)
	}
	for _, limit := range store.limits {
		if limit != 2 {
			t.Fatalf("cleanup limit = %d, want 2", limit)
		}
	}
}

func TestCleanupRecordsFailure(t *testing.T) {
	store := &fakeStore{err: errors.New("database unavailable")}
	cleaner := New(store, testLogger(), 24*time.Hour, time.Hour, 100)

	cleaner.cleanup(context.Background())

	status := cleaner.Status()
	if status.Running || status.LastError == "" || status.TotalDeletedSamples != 0 {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
