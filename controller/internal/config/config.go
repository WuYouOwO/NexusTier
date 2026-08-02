package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// AuthMode decides whether the console demands a session.
type AuthMode string

const (
	// AuthModeAuto requires credentials unless the listener is loopback-only.
	AuthModeAuto AuthMode = "auto"
	// AuthModeRequired always requires credentials.
	AuthModeRequired AuthMode = "required"
	// AuthModeDisabled serves the console unauthenticated. Only for a trusted
	// network path such as an SSH tunnel.
	AuthModeDisabled AuthMode = "disabled"
)

type Config struct {
	GatewayURL       string
	DatabaseURL      string
	ListenAddress    string
	PollInterval     time.Duration
	PollJitter       time.Duration
	RequestTimeout   time.Duration
	ShutdownTimeout  time.Duration
	MetricRetention  time.Duration
	CleanupInterval  time.Duration
	CleanupBatchSize int
	AuthMode         AuthMode
	AuthUsername     string
	AuthPasswordHash string
	SessionKey       string
	SessionTTL       time.Duration
	SecureCookie     bool
}

// AuthRequired reports whether the controller must stand up a session guard.
func (config Config) AuthRequired() bool {
	switch config.AuthMode {
	case AuthModeDisabled:
		return false
	case AuthModeRequired:
		return true
	default:
		return !isLoopbackAddress(config.ListenAddress)
	}
}

func Load() (Config, error) {
	config := Config{
		GatewayURL:       env("NEXUSTIER_CONTROLLER_GATEWAY_URL", "http://127.0.0.1:11211"),
		DatabaseURL:      os.Getenv("NEXUSTIER_CONTROLLER_DATABASE_URL"),
		ListenAddress:    env("NEXUSTIER_CONTROLLER_LISTEN_ADDR", "127.0.0.1:8080"),
		PollInterval:     15 * time.Second,
		PollJitter:       3 * time.Second,
		RequestTimeout:   20 * time.Second,
		ShutdownTimeout:  10 * time.Second,
		MetricRetention:  720 * time.Hour,
		CleanupInterval:  6 * time.Hour,
		CleanupBatchSize: 10_000,
		AuthMode:         AuthMode(env("NEXUSTIER_CONTROLLER_AUTH_MODE", string(AuthModeAuto))),
		AuthUsername:     env("NEXUSTIER_CONTROLLER_AUTH_USERNAME", "admin"),
		AuthPasswordHash: os.Getenv("NEXUSTIER_CONTROLLER_AUTH_PASSWORD_HASH"),
		SessionKey:       os.Getenv("NEXUSTIER_CONTROLLER_SESSION_KEY"),
		SessionTTL:       12 * time.Hour,
		SecureCookie:     true,
	}
	if config.DatabaseURL == "" {
		return Config{}, fmt.Errorf("NEXUSTIER_CONTROLLER_DATABASE_URL is required")
	}
	var err error
	if config.PollInterval, err = durationEnv("NEXUSTIER_CONTROLLER_POLL_INTERVAL", config.PollInterval); err != nil {
		return Config{}, err
	}
	if config.PollJitter, err = durationEnv("NEXUSTIER_CONTROLLER_POLL_JITTER", config.PollJitter); err != nil {
		return Config{}, err
	}
	if config.RequestTimeout, err = durationEnv("NEXUSTIER_CONTROLLER_REQUEST_TIMEOUT", config.RequestTimeout); err != nil {
		return Config{}, err
	}
	if config.ShutdownTimeout, err = durationEnv("NEXUSTIER_CONTROLLER_SHUTDOWN_TIMEOUT", config.ShutdownTimeout); err != nil {
		return Config{}, err
	}
	if config.MetricRetention, err = durationEnv("NEXUSTIER_CONTROLLER_METRIC_RETENTION", config.MetricRetention); err != nil {
		return Config{}, err
	}
	if config.CleanupInterval, err = durationEnv("NEXUSTIER_CONTROLLER_CLEANUP_INTERVAL", config.CleanupInterval); err != nil {
		return Config{}, err
	}
	if config.CleanupBatchSize, err = positiveIntEnv("NEXUSTIER_CONTROLLER_CLEANUP_BATCH_SIZE", config.CleanupBatchSize); err != nil {
		return Config{}, err
	}
	if config.PollInterval <= 0 || config.RequestTimeout <= 0 || config.ShutdownTimeout <= 0 || config.MetricRetention <= 0 || config.CleanupInterval <= 0 {
		return Config{}, fmt.Errorf("poll interval, request timeout, shutdown timeout, metric retention, and cleanup interval must be positive")
	}
	if config.PollJitter < 0 || config.PollJitter >= config.PollInterval {
		return Config{}, fmt.Errorf("poll jitter must be non-negative and less than poll interval")
	}
	if _, err := parseURL(config.GatewayURL, "gateway"); err != nil {
		return Config{}, err
	}
	if _, err := parseURL(config.DatabaseURL, "database"); err != nil {
		return Config{}, err
	}
	if config.SessionTTL, err = durationEnv("NEXUSTIER_CONTROLLER_SESSION_TTL", config.SessionTTL); err != nil {
		return Config{}, err
	}
	if config.SecureCookie, err = boolEnv("NEXUSTIER_CONTROLLER_SECURE_COOKIE", config.SecureCookie); err != nil {
		return Config{}, err
	}
	if err := validateAuth(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func validateAuth(config Config) error {
	switch config.AuthMode {
	case AuthModeAuto, AuthModeRequired, AuthModeDisabled:
	default:
		return fmt.Errorf("NEXUSTIER_CONTROLLER_AUTH_MODE must be auto, required, or disabled")
	}
	if config.SessionTTL <= 0 {
		return fmt.Errorf("NEXUSTIER_CONTROLLER_SESSION_TTL must be positive")
	}
	if !config.AuthRequired() {
		return nil
	}
	// Fail closed: a listener reachable off-host must not serve the console and
	// the topology API without a credential.
	if config.AuthPasswordHash == "" {
		return fmt.Errorf(
			"NEXUSTIER_CONTROLLER_AUTH_PASSWORD_HASH is required when listening on %s; "+
				"generate one with `nexustier-controller -hash-password`, or set "+
				"NEXUSTIER_CONTROLLER_AUTH_MODE=disabled if the listener is already behind a trusted proxy",
			config.ListenAddress,
		)
	}
	if strings.TrimSpace(config.AuthUsername) == "" {
		return fmt.Errorf("NEXUSTIER_CONTROLLER_AUTH_USERNAME must not be empty")
	}
	return nil
}

// isLoopbackAddress reports whether a listen address only accepts connections
// from the same host. An empty or wildcard host counts as remotely reachable.
func isLoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}

func boolEnv(name string, fallback bool) (bool, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("parse %s: must be true or false", name)
	}
	return parsed, nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return duration, nil
}

func positiveIntEnv(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("parse %s: must be a positive integer", name)
	}
	return parsed, nil
}

func parseURL(value, kind string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("parse %s URL: %w", kind, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("%s URL must be absolute", kind)
	}
	return parsed, nil
}
