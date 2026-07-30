package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/WuYouOwO/NexusTier/controller/internal/poller"
)

type Database interface {
	Ping(context.Context) error
}

type Server struct {
	database Database
	worker   *poller.Worker
	started  time.Time
}

func New(database Database, worker *poller.Worker) http.Handler {
	server := &Server{database: database, worker: worker, started: time.Now().UTC()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /readyz", server.ready)
	mux.HandleFunc("GET /v1/telemetry/status", server.telemetryStatus)
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

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
