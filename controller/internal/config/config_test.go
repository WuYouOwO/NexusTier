package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadUsesTypedDefaults(t *testing.T) {
	t.Setenv("NEXUSTIER_CONTROLLER_DATABASE_URL", "postgres://localhost/nexustier")

	config, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.GatewayURL != "http://127.0.0.1:11211" || config.PollInterval != 15*time.Second {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	if config.MetricRetention != 720*time.Hour || config.CleanupInterval != 6*time.Hour || config.CleanupBatchSize != 10_000 {
		t.Fatalf("unexpected retention defaults: %+v", config)
	}
}

func TestLoadRejectsInvalidCleanupBatchSize(t *testing.T) {
	t.Setenv("NEXUSTIER_CONTROLLER_DATABASE_URL", "postgres://localhost/nexustier")
	t.Setenv("NEXUSTIER_CONTROLLER_CLEANUP_BATCH_SIZE", "0")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "CLEANUP_BATCH_SIZE") {
		t.Fatalf("expected cleanup batch error, got %v", err)
	}
}

func TestLoadRejectsJitterAtOrAboveInterval(t *testing.T) {
	t.Setenv("NEXUSTIER_CONTROLLER_DATABASE_URL", "postgres://localhost/nexustier")
	t.Setenv("NEXUSTIER_CONTROLLER_POLL_INTERVAL", "5s")
	t.Setenv("NEXUSTIER_CONTROLLER_POLL_JITTER", "5s")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "poll jitter") {
		t.Fatalf("expected jitter error, got %v", err)
	}
}
