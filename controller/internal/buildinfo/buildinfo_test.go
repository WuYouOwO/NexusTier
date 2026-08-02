package buildinfo

import (
	"strings"
	"testing"
	"time"
)

func TestCurrentNeverReportsEmptyFields(t *testing.T) {
	info := Current()

	if info.Version == "" {
		t.Fatal("version must not be empty")
	}
	if info.Commit == "" || info.BuiltAt == "" {
		t.Fatalf("commit and built_at must fall back to a placeholder, got %+v", info)
	}
	if !strings.HasPrefix(info.GoVersion, "go") {
		t.Fatalf("go_version = %q, want a go... prefix", info.GoVersion)
	}
	if !strings.Contains(info.Platform, "/") {
		t.Fatalf("platform = %q, want os/arch", info.Platform)
	}
}

func TestShortCommitMatchesImageTagPrefix(t *testing.T) {
	info := Info{Commit: "7126599d59efdbba3a17a131cd6208d8ac6e3d8d"}

	if got := info.ShortCommit(); got != "7126599" {
		t.Fatalf("ShortCommit() = %q, want 7126599", got)
	}
}

func TestShortCommitKeepsShortValuesIntact(t *testing.T) {
	info := Info{Commit: unknown}

	if got := info.ShortCommit(); got != unknown {
		t.Fatalf("ShortCommit() = %q, want %q", got, unknown)
	}
}

func TestBuiltAtTimeRejectsNonTimestamps(t *testing.T) {
	if _, ok := (Info{BuiltAt: unknown}).BuiltAtTime(); ok {
		t.Fatal("BuiltAtTime() accepted a placeholder")
	}
	parsed, ok := (Info{BuiltAt: "2026-08-02T10:30:00Z"}).BuiltAtTime()
	if !ok {
		t.Fatal("BuiltAtTime() rejected a valid RFC 3339 instant")
	}
	if !parsed.Equal(time.Date(2026, 8, 2, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("BuiltAtTime() = %s, want 2026-08-02T10:30:00Z", parsed)
	}
}
