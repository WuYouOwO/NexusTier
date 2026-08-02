package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/WuYouOwO/NexusTier/controller/internal/api"
	"github.com/WuYouOwO/NexusTier/controller/internal/auth"
	"github.com/WuYouOwO/NexusTier/controller/internal/config"
	"github.com/WuYouOwO/NexusTier/controller/internal/database"
	"github.com/WuYouOwO/NexusTier/controller/internal/gatewayclient"
	"github.com/WuYouOwO/NexusTier/controller/internal/ingest"
	"github.com/WuYouOwO/NexusTier/controller/internal/poller"
	"github.com/WuYouOwO/NexusTier/controller/internal/readmodel"
	"github.com/WuYouOwO/NexusTier/controller/internal/retention"
)

func main() {
	hashPassword := flag.Bool("hash-password", false,
		"read a password from stdin and print a hash for NEXUSTIER_CONTROLLER_AUTH_PASSWORD_HASH")
	flag.Parse()
	if *hashPassword {
		if err := printPasswordHash(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "hash password:", err)
			os.Exit(1)
		}
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("controller stopped", "error", err)
		os.Exit(1)
	}
}

// printPasswordHash turns a password on stdin into a storable hash. Reading
// from stdin keeps the plaintext out of the process list and shell history.
func printPasswordHash(in io.Reader, out io.Writer) error {
	// One line, so an interactive operator only needs Enter rather than EOF.
	line, err := bufio.NewReader(io.LimitReader(in, 1024)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	password := strings.TrimRight(line, "\r\n")
	if password == "" {
		return errors.New("password must not be empty")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, hash)
	return err
}

// guardedHandler wraps the API in a session guard, or returns it untouched when
// the operator has deliberately opted out.
func guardedHandler(settings config.Config, next http.Handler, logger *slog.Logger) (http.Handler, error) {
	if !settings.AuthRequired() {
		logger.Warn("console authentication is disabled",
			"listen_addr", settings.ListenAddress,
			"auth_mode", string(settings.AuthMode),
			"hint", "anyone who can reach this address can read the full topology")
		return next, nil
	}
	credential, err := auth.NewCredential(settings.AuthUsername, settings.AuthPasswordHash)
	if err != nil {
		return nil, fmt.Errorf("operator credential: %w", err)
	}
	signer, err := auth.NewSessionSigner([]byte(settings.SessionKey), settings.SessionTTL)
	if err != nil {
		return nil, fmt.Errorf("session signer: %w", err)
	}
	guard, err := auth.NewGuard(credential, signer, logger, settings.SecureCookie)
	if err != nil {
		return nil, fmt.Errorf("session guard: %w", err)
	}
	logger.Info("console authentication is enabled",
		"subject", credential.Username,
		"session_ttl", settings.SessionTTL.String(),
		"secure_cookie", settings.SecureCookie)
	return guard.Protect(next), nil
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

	handler, err := guardedHandler(settings, api.New(pool, readmodel.NewStore(pool), worker, retentionCleaner), logger)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              settings.ListenAddress,
		Handler:           handler,
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
