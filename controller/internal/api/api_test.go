package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/WuYouOwO/NexusTier/controller/internal/poller"
)

type fakeDatabase struct {
	err error
}

func (database fakeDatabase) Ping(context.Context) error {
	return database.err
}

func testHandler(database Database) http.Handler {
	worker := poller.New(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second, 0, time.Second)
	return New(database, worker)
}

func TestHealthDoesNotRequireDatabase(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	testHandler(fakeDatabase{err: errors.New("down")}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
}

func TestReadinessRequiresDatabase(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()

	testHandler(fakeDatabase{err: errors.New("down")}).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
}
