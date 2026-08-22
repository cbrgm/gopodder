package main

import (
	"log/slog"
	"runtime/debug"
	"testing"
)

func TestStringToLogLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"unknown", slog.LevelInfo},
		{"", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stringToLogLevel(tt.input)
			if got != tt.want {
				t.Errorf("stringToLogLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSetupLogger(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error"}
	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			logger := setupLogger(level)
			if logger == nil {
				t.Error("setupLogger returned nil")
			}
		})
	}
}

func TestResolveBuildInfo(t *testing.T) {
	stamped := &debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.4-0.20260822150227-02a5696e957f"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "02a5696e957f5d3180df93102e4c07f151b1f417"},
			{Key: "vcs.time", Value: "2026-08-22T15:02:27Z"},
			{Key: "vcs.modified", Value: "false"},
		},
	}

	tests := []struct {
		name                                string
		version, revision, buildDate        string
		info                                *debug.BuildInfo
		wantVersion, wantRevision, wantDate string
	}{
		{
			name:    "linker values win",
			version: "1.2.3", revision: "cdd3370", buildDate: "20260821",
			info:        stamped,
			wantVersion: "1.2.3", wantRevision: "cdd3370", wantDate: "20260821",
		},
		{
			name:    "unstamped binary falls back to module version",
			version: devVersion, revision: unknownValue, buildDate: unknownValue,
			info:        stamped,
			wantVersion: "1.2.4-0.20260822150227-02a5696e957f", wantRevision: "02a5696", wantDate: "20260822",
		},
		{
			name:    "untagged module falls back to revision",
			version: devVersion, revision: unknownValue, buildDate: unknownValue,
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "02a5696e957f5d3180df93102e4c07f151b1f417"},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			wantVersion: "02a5696-dirty", wantRevision: "02a5696-dirty", wantDate: unknownValue,
		},
		{
			name:    "no build info keeps defaults",
			version: devVersion, revision: unknownValue, buildDate: unknownValue,
			info:        nil,
			wantVersion: devVersion, wantRevision: unknownValue, wantDate: unknownValue,
		},
		{
			name:    "empty values never leak out",
			version: "", revision: "", buildDate: "",
			info:        &debug.BuildInfo{},
			wantVersion: devVersion, wantRevision: unknownValue, wantDate: unknownValue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, revision, buildDate := resolveBuildInfo(tt.version, tt.revision, tt.buildDate, tt.info)
			if version != tt.wantVersion {
				t.Errorf("version = %q, want %q", version, tt.wantVersion)
			}
			if revision != tt.wantRevision {
				t.Errorf("revision = %q, want %q", revision, tt.wantRevision)
			}
			if buildDate != tt.wantDate {
				t.Errorf("buildDate = %q, want %q", buildDate, tt.wantDate)
			}
		})
	}
}
