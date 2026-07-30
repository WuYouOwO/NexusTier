package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/WuYouOwO/NexusTier/controller/internal/api"
	"github.com/WuYouOwO/NexusTier/controller/internal/config"
	"github.com/WuYouOwO/NexusTier/controller/internal/database"
	"github.com/WuYouOwO/NexusTier/controller/internal/gatewayclient"
	"github.com/WuYouOwO/NexusTier/controller/internal/ingest"
	"github.com/WuYouOwO/NexusTier/controller/internal/poller"
	"github.com/WuYouOwO/NexusTier/controller/internal/readmodel"
	"github.com/WuYouOwO/NexusTier/controller/internal/retention"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("controller stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := database.Open(ctx, settings.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := database.Migrate(ctx, pool); err != nil {
		return err
	}
	client, err := gatewayclient.New(settings.GatewayURL, settings.RequestTimeout)
	if err != nil {
		return err
	}
	worker := poller.New(
		client,
		ingest.NewStore(pool),
		logger,
		settings.PollInterval,
		settings.PollJitter,
		settings.RequestTimeout,
	)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker.Run(ctx)
	}()
	retentionCleaner := retention.New(
		retention.NewPostgreSQLStore(pool),
		logger,
		settings.MetricRetention,
		settings.CleanupInterval,
		settings.CleanupBatchSize,
	)
	retentionDone := make(chan struct{})
	go func() {
		defer close(retentionDone)
		retentionCleaner.Run(ctx)
	}()

	server := &http.Server{
		Addr:              settings.ListenAddress,
		Handler:           api.New(pool, readmodel.NewStore(pool), worker, retentionCleaner),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("controller API is listening", "listen_addr", settings.ListenAddress)
		serverErrors <- server.ListenAndServe()
	}()

	var serveError error
	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			serveError = err
		}
		stop()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), settings.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil && serveError == nil {
		serveError = err
	}
	select {
	case <-workerDone:
	case <-shutdownCtx.Done():
		if serveError == nil {
			serveError = shutdownCtx.Err()
		}
	}
	select {
	case <-retentionDone:
	case <-shutdownCtx.Done():
		if serveError == nil {
			serveError = shutdownCtx.Err()
		}
	}
	return serveError
}
