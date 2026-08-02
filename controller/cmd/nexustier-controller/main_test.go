package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/WuYouOwO/NexusTier/controller/internal/auth"
	"github.com/WuYouOwO/NexusTier/controller/internal/config"
)

func TestPrintPasswordHashAcceptsASingleInteractiveLine(t *testing.T) {
	// Trailing content proves the reader stops at the first newline instead of
	// waiting for EOF, which is what an interactive operator relies on.
	in := strings.NewReader("correct horse\nignored second line\n")
	var out bytes.Buffer

	if err := printPasswordHash(in, &out); err != nil {
		t.Fatalf("printPasswordHash returned %v", err)
	}

	hash := strings.TrimSpace(out.String())
	credential, err := auth.NewCredential("admin", hash)
	if err != nil {
		t.Fatalf("NewCredential rejected the printed hash: %v", err)
	}
	if !credential.Verify("admin", "correct horse") {
		t.Fatal("printed hash does not verify the password that produced it")
	}
	if credential.Verify("admin", "ignored second line") {
		t.Fatal("hash was derived from more than the first line")
	}
}

func TestPrintPasswordHashAcceptsInputWithoutATrailingNewline(t *testing.T) {
	var out bytes.Buffer

	if err := printPasswordHash(strings.NewReader("piped"), &out); err != nil {
		t.Fatalf("printPasswordHash returned %v", err)
	}

	credential, err := auth.NewCredential("admin", strings.TrimSpace(out.String()))
	if err != nil {
		t.Fatalf("NewCredential rejected the printed hash: %v", err)
	}
	if !credential.Verify("admin", "piped") {
		t.Fatal("hash does not verify piped input that lacked a newline")
	}
}

func TestPrintPasswordHashRejectsAnEmptyPassword(t *testing.T) {
	for name, input := range map[string]string{
		"only newline": "\n",
		"empty":        "",
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := printPasswordHash(strings.NewReader(input), &out); err == nil {
				t.Fatal("expected an error for an empty password")
			}
			if out.Len() != 0 {
				t.Fatalf("nothing should be printed, got %q", out.String())
			}
		})
	}
}

func TestGuardedHandlerProtectsTheAPIWhenCredentialsArePresent(t *testing.T) {
	hash, err := auth.HashPassword("operator secret")
	if err != nil {
		t.Fatalf("HashPassword returned %v", err)
	}
	settings := config.Config{
		ListenAddress:    "0.0.0.0:8080",
		AuthMode:         config.AuthModeRequired,
		AuthUsername:     "admin",
		AuthPasswordHash: hash,
		SessionKey:       strings.Repeat("k", 32),
		SessionTTL:       time.Hour,
	}
	reached := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })

	handler, err := guardedHandler(settings, next, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("guardedHandler returned %v", err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/topology", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a session, got %d", recorder.Code)
	}
	if reached {
		t.Fatal("the guard let an unauthenticated request through")
	}
}

func TestGuardedHandlerPassesThroughWhenAuthenticationIsDisabled(t *testing.T) {
	settings := config.Config{
		ListenAddress: "0.0.0.0:8080",
		AuthMode:      config.AuthModeDisabled,
	}
	reached := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })

	handler, err := guardedHandler(settings, next, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("guardedHandler returned %v", err)
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/topology", nil))

	if !reached {
		t.Fatal("expected the request to reach the API when auth is disabled")
	}
}
