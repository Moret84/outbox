package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadResolvesRelativeDirectoryFromConfig(t *testing.T) {
	configDirectory := t.TempDir()
	configPath := filepath.Join(configDirectory, "outbox.yaml")
	writeConfig(t, configPath, `
rules:
  - name: csv
    directory: inbox
    patterns: ["*.csv"]
    command: process "$FILE"
`)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := filepath.Join(configDirectory, "inbox")
	if got := cfg.Rules[0].Directory; got != want {
		t.Errorf("Directory = %q, want %q", got, want)
	}
	if got := cfg.Interval.Duration; got != DefaultInterval {
		t.Errorf("Interval = %s, want %s", got, DefaultInterval)
	}
	if got := cfg.Rules[0].OnSuccess; got != SuccessKeep {
		t.Errorf("OnSuccess = %q, want %q", got, SuccessKeep)
	}
}

func TestLoadParsesContinuousAndArchiveSettings(t *testing.T) {
	configDirectory := t.TempDir()
	configPath := filepath.Join(configDirectory, "outbox.yaml")
	writeConfig(t, configPath, `
interval: 15s
rules:
  - name: csv
    directory: inbox
    patterns: ["*.csv"]
    minimum_age: 1m
    command: process "$FILE"
    on_success: archive
    archive_directory: archive
`)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := cfg.Interval.Duration; got != 15*time.Second {
		t.Errorf("Interval = %s, want 15s", got)
	}
	rule := cfg.Rules[0]
	if got := rule.MinimumAge.Duration; got != time.Minute {
		t.Errorf("MinimumAge = %s, want 1m", got)
	}
	if got := rule.OnSuccess; got != SuccessArchive {
		t.Errorf("OnSuccess = %q, want %q", got, SuccessArchive)
	}
	wantArchive := filepath.Join(configDirectory, "archive")
	if got := rule.ArchiveDirectory; got != wantArchive {
		t.Errorf("ArchiveDirectory = %q, want %q", got, wantArchive)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "outbox.yaml")
	writeConfig(t, configPath, `
rules:
  - name: csv
    directory: inbox
    patterns: ["*.csv"]
    command: process
    typo: true
`)

	_, err := Load(configPath)
	if err == nil || !strings.Contains(err.Error(), "field typo not found") {
		t.Fatalf("Load() error = %v, want unknown field error", err)
	}
}

func TestLoadRejectsDuplicateRuleName(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "outbox.yaml")
	writeConfig(t, configPath, `
rules:
  - name: csv
    directory: first
    patterns: ["*.csv"]
    command: process
  - name: csv
    directory: second
    patterns: ["*.csv"]
    command: process
`)

	_, err := Load(configPath)
	if err == nil || !strings.Contains(err.Error(), "name must be unique") {
		t.Fatalf("Load() error = %v, want duplicate name error", err)
	}
}

func TestLoadRejectsInvalidPattern(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "outbox.yaml")
	writeConfig(t, configPath, `
rules:
  - name: csv
    directory: inbox
    patterns: ["["]
    command: process
`)

	_, err := Load(configPath)
	if err == nil || !strings.Contains(err.Error(), "invalid pattern") {
		t.Fatalf("Load() error = %v, want invalid pattern error", err)
	}
}

func TestLoadRejectsInvalidProcessingSettings(t *testing.T) {
	tests := []struct {
		name           string
		globalSettings string
		ruleSettings   string
		wantError      string
	}{
		{name: "zero interval", globalSettings: "interval: 0s\n", wantError: "interval must be greater than zero"},
		{name: "negative age", ruleSettings: "minimum_age: -1s\n", wantError: "minimum_age cannot be negative"},
		{name: "invalid action", ruleSettings: "on_success: move\n", wantError: "on_success must be keep, delete, or archive"},
		{name: "missing archive directory", ruleSettings: "on_success: archive\n", wantError: "archive_directory is required"},
		{name: "unused archive directory", ruleSettings: "archive_directory: archive\n", wantError: "archive_directory requires"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "outbox.yaml")
			writeConfig(t, configPath, test.globalSettings+"rules:\n  - name: csv\n    directory: inbox\n    patterns: [\"*.csv\"]\n    command: process\n    "+strings.ReplaceAll(test.ruleSettings, "\n", "\n    "))

			_, err := Load(configPath)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Load() error = %v, want error containing %q", err, test.wantError)
			}
		})
	}
}

func writeConfig(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
