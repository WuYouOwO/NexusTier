package config

import (
	"fmt"
	"net/url"
	"os"
	"time"
)

type Config struct {
	GatewayURL      string
	DatabaseURL     string
	ListenAddress   string
	PollInterval    time.Duration
	PollJitter      time.Duration
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	config := Config{
		GatewayURL:      env("NEXUSTIER_CONTROLLER_GATEWAY_URL", "http://127.0.0.1:11211"),
		DatabaseURL:     os.Getenv("NEXUSTIER_CONTROLLER_DATABASE_URL"),
		ListenAddress:   env("NEXUSTIER_CONTROLLER_LISTEN_ADDR", "127.0.0.1:8080"),
		PollInterval:    15 * time.Second,
		PollJitter:      3 * time.Second,
		RequestTimeout:  20 * time.Second,
		ShutdownTimeout: 10 * time.Second,
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
	if config.PollInterval <= 0 || config.RequestTimeout <= 0 || config.ShutdownTimeout <= 0 {
		return Config{}, fmt.Errorf("poll interval, request timeout, and shutdown timeout must be positive")
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
	return config, nil
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
