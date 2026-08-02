// Package buildinfo exposes the identity of the running binary so an operator
// can prove which image is deployed.
package buildinfo

import (
	"runtime"
	"runtime/debug"
	"time"
)

// Injected at link time with -ldflags "-X ...". They stay at their fallback
// values for plain `go build` and `go test` runs.
var (
	commit    = ""
	buildTime = ""
	version   = "dev"
)

const unknown = "unknown"

// Info describes the running binary. Every field is safe to expose publicly:
// it carries no configuration, credential, or host detail.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuiltAt   string `json:"built_at"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

// Current resolves build identity, falling back to the Go module stamp that
// `go build` embeds automatically when ldflags were not supplied.
func Current() Info {
	info := Info{
		Version:   version,
		Commit:    commit,
		BuiltAt:   buildTime,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
	if info.Commit == "" || info.BuiltAt == "" {
		fillFromModuleStamp(&info)
	}
	if info.Commit == "" {
		info.Commit = unknown
	}
	if info.BuiltAt == "" {
		info.BuiltAt = unknown
	}
	// An empty -X value overwrites the fallback, so restore it here.
	if info.Version == "" {
		info.Version = "dev"
	}
	return info
}

func fillFromModuleStamp(info *Info) {
	stamp, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	for _, setting := range stamp.Settings {
		switch setting.Key {
		case "vcs.revision":
			if info.Commit == "" {
				info.Commit = setting.Value
			}
		case "vcs.time":
			if info.BuiltAt == "" {
				info.BuiltAt = setting.Value
			}
		}
	}
}

// ShortCommit returns the 7-character prefix used in GHCR `sha-` image tags.
func (info Info) ShortCommit() string {
	if len(info.Commit) < 7 {
		return info.Commit
	}
	return info.Commit[:7]
}

// BuiltAtTime reports the build timestamp when it is a valid RFC 3339 instant.
func (info Info) BuiltAtTime() (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339, info.BuiltAt)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}
