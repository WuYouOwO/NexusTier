package poller

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WuYouOwO/NexusTier/controller/internal/gatewayclient"
	"github.com/google/uuid"
)

type fakeFetcher struct {
	active    atomic.Int32
	maxActive atomic.Int32
	calls     atomic.Int32
	delay     time.Duration
	fail      bool
}

func (fetcher *fakeFetcher) FetchTopology(ctx context.Context) (gatewayclient.Snapshot, error) {
	active := fetcher.active.Add(1)
	defer fetcher.active.Add(-1)
	for {
		maximum := fetcher.maxActive.Load()
		if active <= maximum || fetcher.maxActive.CompareAndSwap(maximum, active) {
			break
		}
	}
	fetcher.calls.Add(1)
	select {
	case <-ctx.Done():
		return gatewayclient.Snapshot{}, ctx.Err()
	case <-time.After(fetcher.delay):
	}
	if fetcher.fail {
		return gatewayclient.Snapshot{}, errors.New("unavailable")
	}
	now := uint64(time.Now().UnixMilli())
	return gatewayclient.Snapshot{
		SchemaVersion: gatewayclient.SchemaVersion,
		CollectionID:  uuid.New(),
		StartedAtMS:   now,
		CompletedAtMS: now,
		CollectedAtMS: now,
	}, nil
}

type fakeIngester struct{}

func (fakeIngester) Ingest(context.Context, gatewayclient.Snapshot) (bool, error) {
	return true, nil
}

func TestWorkerNeverOverlapsCollections(t *testing.T) {
	fetcher := &fakeFetcher{delay: 20 * time.Millisecond}
	worker := New(fetcher, fakeIngester{}, slog.New(slog.NewTextHandler(io.Discard, nil)), 5*time.Millisecond, 0, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Millisecond)
	defer cancel()

	worker.Run(ctx)

	if fetcher.calls.Load() < 2 {
		t.Fatalf("expected repeated collections, got %d", fetcher.calls.Load())
	}
	if fetcher.maxActive.Load() != 1 {
		t.Fatalf("maximum active collections = %d, want 1", fetcher.maxActive.Load())
	}
}

func TestWorkerRecordsFailures(t *testing.T) {
	fetcher := &fakeFetcher{fail: true}
	worker := New(fetcher, fakeIngester{}, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour, 0, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	worker.Run(ctx)
	status := worker.Status()
	if status.ConsecutiveFailures != 1 || status.LastError == "" {
		t.Fatalf("unexpected failure status: %+v", status)
	}
}
