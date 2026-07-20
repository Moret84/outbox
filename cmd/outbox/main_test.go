package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
