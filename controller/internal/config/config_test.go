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
