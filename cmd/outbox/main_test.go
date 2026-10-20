package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunContinuouslyRetriesAndStops(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "input.csv"), []byte("test"), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "outbox.yaml")
	configContents := fmt.Sprintf(`
interval: 1ms
rules:
  - name: csv
    directory: %q
    patterns: ["*.csv"]
    command: |
      if [ ! -e "$MARKER_PATH" ]; then
        touch "$MARKER_PATH"
        exit 1
      fi
      printf x > "$RESULT_PATH"
`, directory)
	if err := os.WriteFile(configPath, []byte(configContents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	markerPath := filepath.Join(t.TempDir(), "marker")
	resultPath := filepath.Join(t.TempDir(), "result")
	t.Setenv("MARKER_PATH", markerPath)
	t.Setenv("RESULT_PATH", resultPath)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var stderr bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- runContinuously(ctx, configPath, &bytes.Buffer{}, &stderr)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(resultPath); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatalf("inspect result: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(resultPath); err != nil {
		t.Fatalf("continuous run did not produce a result: %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runContinuously() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runContinuously() did not stop")
	}
	if !strings.Contains(stderr.String(), "command failed") {
		t.Errorf("stderr = %q, want first-attempt failure", stderr.String())
	}
}

func TestRunContinuouslyReloadsConfig(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "outbox.yaml")
	writeConfig := func(command string) {
		t.Helper()
		contents := fmt.Sprintf(`
interval: 1ms
rules:
  - name: csv
    directory: %q
    patterns: ["*.csv"]
    command: %q
`, directory, command)
		writeFileAtomically(t, configPath, contents)
	}
	writeInput := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(directory, name), []byte("test"), 0o600); err != nil {
			t.Fatalf("write input: %v", err)
		}
	}
	firstPath := filepath.Join(t.TempDir(), "first")
	secondPath := filepath.Join(t.TempDir(), "second")
	t.Setenv("FIRST_PATH", firstPath)
	t.Setenv("SECOND_PATH", secondPath)
	writeConfig(`mv -- "$FILE" "$FIRST_PATH"`)
	writeInput("first.csv")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var stderr synchronizedBuffer
	done := make(chan error, 1)
	go func() {
		done <- runContinuously(ctx, configPath, &bytes.Buffer{}, &stderr)
	}()
	waitForPath(t, firstPath)

	writeFileAtomically(t, configPath, "interval: [")
	waitFor(t, "invalid configuration report", func() bool {
		return strings.Contains(stderr.String(), "skipping pass")
	})
	writeInput("second.csv")
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(directory, "second.csv")); err != nil {
		t.Fatalf("file processed while the configuration was invalid: %v", err)
	}

	writeConfig(`mv -- "$FILE" "$SECOND_PATH"`)
	waitForPath(t, secondPath)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runContinuously() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runContinuously() did not stop")
	}
}

func TestRunContinuouslyRejectsInvalidConfigAtStartup(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "outbox.yaml")
	writeFileAtomically(t, configPath, "interval: [")

	err := runContinuously(context.Background(), configPath, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "decode config") {
		t.Fatalf("runContinuously() error = %v, want decode error", err)
	}
}

// writeFileAtomically keeps the polling loop from reading a half-written
// configuration, which would be reported as invalid.
func writeFileAtomically(t *testing.T, path, contents string) {
	t.Helper()
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", temporary, err)
	}
	if err := os.Rename(temporary, path); err != nil {
		t.Fatalf("rename %s: %v", temporary, err)
	}
}

func waitForPath(t *testing.T, path string) {
	t.Helper()
	waitFor(t, path, func() bool {
		_, err := os.Stat(path)
		return err == nil
	})
}

func waitFor(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

type synchronizedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (b *synchronizedBuffer) Write(data []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.Write(data)
}

func (b *synchronizedBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.String()
}
