package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Moret84/outbox/internal/config"
)

func TestRunOnceProcessesMatchingFilesInOrderWithEnvironment(t *testing.T) {
	directory := t.TempDir()
	writeFile(t, filepath.Join(directory, "second file.csv"))
	writeFile(t, filepath.Join(directory, "first.csv"))
	writeFile(t, filepath.Join(directory, "ignored.txt"))
	if err := os.Mkdir(filepath.Join(directory, "nested.csv"), 0o700); err != nil {
		t.Fatalf("create nested directory: %v", err)
	}

	resultPath := filepath.Join(t.TempDir(), "result")
	t.Setenv("RESULT_PATH", resultPath)
	cfg := config.Config{Rules: []config.Rule{
		{
			Name:      "bank",
			Directory: directory,
			Patterns:  []string{"*.csv"},
			Command:   `printf '%s|%s|%s\n' "$RULE" "$DIRECTORY" "$FILE" >> "$RESULT_PATH"`,
		},
	}}

	var stdout bytes.Buffer
	if err := RunOnce(context.Background(), cfg, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	result, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	want := strings.Join([]string{
		"bank|" + directory + "|" + filepath.Join(directory, "first.csv"),
		"bank|" + directory + "|" + filepath.Join(directory, "second file.csv"),
		"",
	}, "\n")
	if got := string(result); got != want {
		t.Errorf("result = %q, want %q", got, want)
	}
	if got := strings.Count(stdout.String(), "[bank] processing"); got != 2 {
		t.Errorf("processing log count = %d, want 2", got)
	}
}

func TestRunOnceContinuesAfterCommandFailure(t *testing.T) {
	directory := t.TempDir()
	failingFile := filepath.Join(directory, "first.csv")
	writeFile(t, failingFile)
	writeFile(t, filepath.Join(directory, "second.csv"))

	resultPath := filepath.Join(t.TempDir(), "result")
	t.Setenv("RESULT_PATH", resultPath)
	t.Setenv("FAILING_FILE", failingFile)
	cfg := config.Config{Rules: []config.Rule{
		{
			Name:      "bank",
			Directory: directory,
			Patterns:  []string{"*.csv"},
			Command:   `if [ "$FILE" = "$FAILING_FILE" ]; then exit 7; fi; printf '%s\n' "$FILE" >> "$RESULT_PATH"`,
		},
	}}

	err := RunOnce(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("RunOnce() error = %v, want exit status 7", err)
	}

	result, readErr := os.ReadFile(resultPath)
	if readErr != nil {
		t.Fatalf("read result: %v", readErr)
	}
	if got, want := string(result), filepath.Join(directory, "second.csv")+"\n"; got != want {
		t.Errorf("result = %q, want %q", got, want)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
