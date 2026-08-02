package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoopbackListenerDoesNotRequireCredentials(t *testing.T) {
	t.Setenv("NEXUSTIER_CONTROLLER_DATABASE_URL", "postgres://localhost/nexustier")
	t.Setenv("NEXUSTIER_CONTROLLER_LISTEN_ADDR", "127.0.0.1:8080")

	config, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.AuthRequired() {
		t.Fatal("a loopback listener must not demand credentials")
	}
}

func TestPublicListenerRefusesToStartWithoutAPasswordHash(t *testing.T) {
	t.Setenv("NEXUSTIER_CONTROLLER_DATABASE_URL", "postgres://localhost/nexustier")
	t.Setenv("NEXUSTIER_CONTROLLER_LISTEN_ADDR", "0.0.0.0:8080")

	_, err := Load()
	if err == nil {
		t.Fatal("a listener reachable off-host must not start unauthenticated")
	}
	if !strings.Contains(err.Error(), "AUTH_PASSWORD_HASH") {
		t.Fatalf("error should name the missing variable, got %v", err)
	}
}

func TestPublicListenerAcceptsAPasswordHash(t *testing.T) {
	t.Setenv("NEXUSTIER_CONTROLLER_DATABASE_URL", "postgres://localhost/nexustier")
	t.Setenv("NEXUSTIER_CONTROLLER_LISTEN_ADDR", "0.0.0.0:8080")
	t.Setenv("NEXUSTIER_CONTROLLER_AUTH_PASSWORD_HASH", "pbkdf2-sha256$600000$c2FsdHNhbHRzYWx0c2E$a2V5")

	config, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !config.AuthRequired() {
		t.Fatal("a public listener must demand credentials")
	}
}

func TestDisabledModeAllowsAPublicListenerWithoutCredentials(t *testing.T) {
	t.Setenv("NEXUSTIER_CONTROLLER_DATABASE_URL", "postgres://localhost/nexustier")
	t.Setenv("NEXUSTIER_CONTROLLER_LISTEN_ADDR", "0.0.0.0:8080")
	t.Setenv("NEXUSTIER_CONTROLLER_AUTH_MODE", "disabled")

	config, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.AuthRequired() {
		t.Fatal("disabled mode must not demand credentials")
	}
}

func TestRequiredModeDemandsCredentialsEvenOnLoopback(t *testing.T) {
	t.Setenv("NEXUSTIER_CONTROLLER_DATABASE_URL", "postgres://localhost/nexustier")
	t.Setenv("NEXUSTIER_CONTROLLER_LISTEN_ADDR", "127.0.0.1:8080")
	t.Setenv("NEXUSTIER_CONTROLLER_AUTH_MODE", "required")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "AUTH_PASSWORD_HASH") {
		t.Fatalf("required mode must demand a hash on loopback too, got %v", err)
	}
}

func TestUnknownAuthModeIsRejected(t *testing.T) {
	t.Setenv("NEXUSTIER_CONTROLLER_DATABASE_URL", "postgres://localhost/nexustier")
	t.Setenv("NEXUSTIER_CONTROLLER_AUTH_MODE", "maybe")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "AUTH_MODE") {
		t.Fatalf("expected an auth mode error, got %v", err)
	}
}

func TestSessionTTLMustBePositive(t *testing.T) {
	t.Setenv("NEXUSTIER_CONTROLLER_DATABASE_URL", "postgres://localhost/nexustier")
	t.Setenv("NEXUSTIER_CONTROLLER_SESSION_TTL", "0s")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "SESSION_TTL") {
		t.Fatalf("expected a session ttl error, got %v", err)
	}
}

func TestSessionTTLDefaultsToAWorkingDay(t *testing.T) {
	t.Setenv("NEXUSTIER_CONTROLLER_DATABASE_URL", "postgres://localhost/nexustier")

	config, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.SessionTTL != 12*time.Hour {
		t.Fatalf("SessionTTL = %s, want 12h", config.SessionTTL)
	}
}

func TestIPv6LoopbackCountsAsLoopback(t *testing.T) {
	if !isLoopbackAddress("[::1]:8080") {
		t.Fatal("[::1]:8080 must count as loopback")
	}
	if isLoopbackAddress("0.0.0.0:8080") {
		t.Fatal("0.0.0.0:8080 must not count as loopback")
	}
	if isLoopbackAddress(":8080") {
		t.Fatal("a bare port binds every interface and must not count as loopback")
	}
}
