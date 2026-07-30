package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/WuYouOwO/NexusTier/controller/internal/poller"
	"github.com/WuYouOwO/NexusTier/controller/internal/readmodel"
	"github.com/WuYouOwO/NexusTier/controller/internal/retention"
	"github.com/WuYouOwO/NexusTier/controller/internal/webui"
	"github.com/google/uuid"
)

type Database interface {
	Ping(context.Context) error
}

type TopologyReader interface {
	CurrentTopology(context.Context, readmodel.TopologyQuery) (readmodel.TopologyPage, error)
}

type Server struct {
	database       Database
	topologyReader TopologyReader
	worker         *poller.Worker
	retention      *retention.Cleaner
	started        time.Time
}

func New(database Database, topologyReader TopologyReader, worker *poller.Worker, retentionCleaner *retention.Cleaner) http.Handler {
	server := &Server{
		database:       database,
		topologyReader: topologyReader,
		worker:         worker,
		retention:      retentionCleaner,
		started:        time.Now().UTC(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /readyz", server.ready)
	mux.HandleFunc("GET /v1/telemetry/status", server.telemetryStatus)
	mux.HandleFunc("GET /v1/topology", server.currentTopology)
	mux.HandleFunc("GET /v1/retention/status", server.retentionStatus)
	mux.Handle("GET /", webui.Handler())
	return mux
}

func (server *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{
		"status":     "ok",
		"started_at": server.started,
	})
}

func (server *Server) ready(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), time.Second)
	defer cancel()
	if err := server.database.Ping(ctx); err != nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{
				"code":    "database_unavailable",
				"message": "controller database is unavailable",
			},
		})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
}

func (server *Server) telemetryStatus(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, server.worker.Status())
}

func (server *Server) retentionStatus(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, server.retention.Status())
}

func (server *Server) currentTopology(writer http.ResponseWriter, request *http.Request) {
	query, err := topologyQuery(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	topology, err := server.topologyReader.CurrentTopology(request.Context(), query)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "topology_unavailable", "current topology is unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, topology)
}

func topologyQuery(request *http.Request) (readmodel.TopologyQuery, error) {
	query := readmodel.TopologyQuery{Limit: 100}
	values := request.URL.Query()
	if value := values.Get("active"); value != "" {
		active, err := strconv.ParseBool(value)
		if err != nil {
			return readmodel.TopologyQuery{}, errors.New("active must be true or false")
		}
		query.Active = &active
	}
	if value := values.Get("machine_id"); value != "" {
		machineID, err := uuid.Parse(value)
		if err != nil {
			return readmodel.TopologyQuery{}, errors.New("machine_id must be a UUID")
		}
		query.MachineID = &machineID
	}
	if value := values.Get("cursor"); value != "" {
		cursor, err := uuid.Parse(value)
		if err != nil {
			return readmodel.TopologyQuery{}, errors.New("cursor must be a UUID")
		}
		query.Cursor = &cursor
	}
	if value := values.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 500 {
			return readmodel.TopologyQuery{}, errors.New("limit must be between 1 and 500")
		}
		query.Limit = limit
	}
	return query, nil
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
