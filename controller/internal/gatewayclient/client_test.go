package gatewayclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestClientDecodesSharedTopologyFixture(t *testing.T) {
	fixture := readFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/topology" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(fixture)
	}))
	defer server.Close()
	client, err := New(server.URL, time.Second)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	snapshot, err := client.FetchTopology(context.Background())
	if err != nil {
		t.Fatalf("fetch topology: %v", err)
	}
	if snapshot.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %q, want %q", snapshot.SchemaVersion, SchemaVersion)
	}
	if len(snapshot.Machines) != 1 || len(snapshot.Machines[0].Instances) != 1 {
		t.Fatalf("unexpected hierarchy: %+v", snapshot.Machines)
	}
	if snapshot.Machines[0].Instances[0].Peers[0].PeerID != 1002 {
		t.Fatalf("peer ID was not decoded")
	}
}

func TestClientRejectsUnknownContractFields(t *testing.T) {
	fixture := strings.Replace(string(readFixture(t)), "{", "{\"unexpected\":true,", 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(fixture))
	}))
	defer server.Close()
	client, err := New(server.URL, time.Second)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	_, err = client.FetchTopology(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestSnapshotRejectsTimestampsOutsideSupportedRange(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion: SchemaVersion,
		CollectionID:  uuid.New(),
		StartedAtMS:   maxTimestampMS + 1,
		CompletedAtMS: maxTimestampMS + 1,
		CollectedAtMS: maxTimestampMS + 1,
	}

	err := snapshot.Validate()
	if err == nil || !strings.Contains(err.Error(), "supported timestamp range") {
		t.Fatalf("expected timestamp range error, got %v", err)
	}
}

func readFixture(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "contracts", "fixtures", "topology-v1.json")
	fixture, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read shared fixture: %v", err)
	}
	return fixture
}
