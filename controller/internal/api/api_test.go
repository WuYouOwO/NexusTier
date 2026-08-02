package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/WuYouOwO/NexusTier/controller/internal/poller"
	"github.com/WuYouOwO/NexusTier/controller/internal/readmodel"
	"github.com/WuYouOwO/NexusTier/controller/internal/retention"
	"github.com/google/uuid"
)

type fakeDatabase struct {
	err error
}

func (database fakeDatabase) Ping(context.Context) error {
	return database.err
}

type fakeTopologyReader struct {
	page  readmodel.TopologyPage
	err   error
	query readmodel.TopologyQuery
}

func (reader *fakeTopologyReader) CurrentTopology(_ context.Context, query readmodel.TopologyQuery) (readmodel.TopologyPage, error) {
	reader.query = query
	return reader.page, reader.err
}

func testHandler(database Database) http.Handler {
	worker := poller.New(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second, 0, time.Second)
	cleaner := retention.New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour, time.Hour, 100)
	return New(database, &fakeTopologyReader{}, worker, cleaner)
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

func TestCurrentTopologyParsesFilters(t *testing.T) {
	machineID := uuid.New()
	cursor := uuid.New()
	reader := &fakeTopologyReader{page: readmodel.TopologyPage{Machines: []readmodel.Machine{}}}
	worker := poller.New(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second, 0, time.Second)
	cleaner := retention.New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour, time.Hour, 100)
	handler := New(fakeDatabase{}, reader, worker, cleaner)
	request := httptest.NewRequest(http.MethodGet,
		"/v1/topology?active=true&machine_id="+machineID.String()+"&cursor="+cursor.String()+"&limit=25", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if reader.query.Active == nil || !*reader.query.Active || reader.query.Limit != 25 {
		t.Fatalf("unexpected query: %+v", reader.query)
	}
	if reader.query.MachineID == nil || *reader.query.MachineID != machineID {
		t.Fatalf("machine_id = %v, want %s", reader.query.MachineID, machineID)
	}
	if reader.query.Cursor == nil || *reader.query.Cursor != cursor {
		t.Fatalf("cursor = %v, want %s", reader.query.Cursor, cursor)
	}
}

func TestCurrentTopologyRejectsInvalidLimit(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/topology?limit=501", nil)
	response := httptest.NewRecorder()

	testHandler(fakeDatabase{}).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestCurrentTopologyHidesDatabaseErrors(t *testing.T) {
	reader := &fakeTopologyReader{err: errors.New("postgres details")}
	worker := poller.New(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second, 0, time.Second)
	cleaner := retention.New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour, time.Hour, 100)
	handler := New(fakeDatabase{}, reader, worker, cleaner)
	request := httptest.NewRequest(http.MethodGet, "/v1/topology", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if strings.Contains(response.Body.String(), "postgres details") {
		t.Fatal("response leaked internal database error")
	}
}

func TestBuildIdentityReportsRunningBinary(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/build", nil)
	response := httptest.NewRecorder()

	testHandler(fakeDatabase{}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	var payload struct {
		Version   string `json:"version"`
		Commit    string `json:"commit"`
		BuiltAt   string `json:"built_at"`
		GoVersion string `json:"go_version"`
		Platform  string `json:"platform"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode build identity: %v", err)
	}
	if payload.Version == "" || payload.Commit == "" || payload.BuiltAt == "" {
		t.Fatalf("build identity has empty fields: %+v", payload)
	}
	if payload.GoVersion == "" || payload.Platform == "" {
		t.Fatalf("build identity omits runtime details: %+v", payload)
	}
}

func TestBuildIdentityDoesNotRequireDatabase(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/build", nil)
	response := httptest.NewRecorder()

	testHandler(fakeDatabase{err: errors.New("down")}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
}
