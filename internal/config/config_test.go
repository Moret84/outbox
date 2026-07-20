package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func writeConfig(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
