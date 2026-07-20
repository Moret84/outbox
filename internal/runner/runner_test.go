package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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
	cfg := config.Config{Rules: []config.Rule{
		{
			Name:      "bank",
			Directory: directory,
			Patterns:  []string{"*.csv"},
			Command:   `case "$FILE" in */first.csv) exit 7;; esac; printf '%s\n' "$FILE" >> "$RESULT_PATH"`,
			OnSuccess: config.SuccessDelete,
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
	if got := string(result); !strings.HasSuffix(got, "/second.csv\n") {
		t.Errorf("result = %q, want claimed second.csv path", got)
	}
	if _, statErr := os.Stat(failingFile); !os.IsNotExist(statErr) {
		t.Errorf("failing file should be claimed, stat error = %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(directory, "second.csv")); !os.IsNotExist(statErr) {
		t.Errorf("successful file should be deleted, stat error = %v", statErr)
	}

	claimed := claimedPaths(t, cfg.Rules[0])
	if len(claimed) != 1 {
		t.Fatalf("claimed file count = %d, want 1", len(claimed))
	}
	retryConfig := cfg
	retryConfig.Rules[0].Command = "true"
	if err := RunOnce(context.Background(), retryConfig, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("retry RunOnce() error = %v", err)
	}
	if got := len(claimedPaths(t, cfg.Rules[0])); got != 0 {
		t.Errorf("claimed file count after retry = %d, want 0", got)
	}
}

func TestRunOnceSkipsFilesYoungerThanMinimumAge(t *testing.T) {
	directory := t.TempDir()
	oldFile := filepath.Join(directory, "old.csv")
	youngFile := filepath.Join(directory, "young.csv")
	writeFile(t, oldFile)
	writeFile(t, youngFile)

	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldFile, now.Add(-2*time.Minute), now.Add(-2*time.Minute)); err != nil {
		t.Fatalf("set old file time: %v", err)
	}
	if err := os.Chtimes(youngFile, now.Add(-30*time.Second), now.Add(-30*time.Second)); err != nil {
		t.Fatalf("set young file time: %v", err)
	}

	resultPath := filepath.Join(t.TempDir(), "result")
	t.Setenv("RESULT_PATH", resultPath)
	cfg := config.Config{Rules: []config.Rule{
		{
			Name:       "bank",
			Directory:  directory,
			Patterns:   []string{"*.csv"},
			MinimumAge: config.Duration{Duration: time.Minute},
			Command:    `printf '%s\n' "$FILE" >> "$RESULT_PATH"`,
		},
	}}

	if err := runOnceAt(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, now); err != nil {
		t.Fatalf("runOnceAt() error = %v", err)
	}
	result, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if got, want := string(result), oldFile+"\n"; got != want {
		t.Errorf("result = %q, want %q", got, want)
	}
}

func TestRunOnceArchivesWithoutOverwritingExistingFile(t *testing.T) {
	directory := t.TempDir()
	archiveDirectory := t.TempDir()
	file := filepath.Join(directory, "export.csv")
	writeFile(t, file)
	existingArchive := filepath.Join(archiveDirectory, "export.csv")
	if err := os.WriteFile(existingArchive, []byte("existing"), 0o600); err != nil {
		t.Fatalf("write existing archive: %v", err)
	}

	cfg := config.Config{Rules: []config.Rule{
		{
			Name:             "bank",
			Directory:        directory,
			Patterns:         []string{"*.csv"},
			Command:          "true",
			OnSuccess:        config.SuccessArchive,
			ArchiveDirectory: archiveDirectory,
		},
	}}

	if err := RunOnce(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("source file should be archived, stat error = %v", err)
	}
	archived, err := os.ReadFile(filepath.Join(archiveDirectory, "export.1.csv"))
	if err != nil {
		t.Fatalf("read archived file: %v", err)
	}
	if got, want := string(archived), "test"; got != want {
		t.Errorf("archived contents = %q, want %q", got, want)
	}
	existing, err := os.ReadFile(existingArchive)
	if err != nil {
		t.Fatalf("read existing archive: %v", err)
	}
	if got, want := string(existing), "existing"; got != want {
		t.Errorf("existing archive contents = %q, want %q", got, want)
	}
}

func TestRunOnceCompletesPartiallyArchivedClaim(t *testing.T) {
	directory := t.TempDir()
	archiveDirectory := t.TempDir()
	rule := config.Rule{
		Name:             "bank",
		Directory:        directory,
		Patterns:         []string{"*.csv"},
		Command:          "true",
		OnSuccess:        config.SuccessArchive,
		ArchiveDirectory: archiveDirectory,
	}
	if err := os.MkdirAll(processingRoot(rule), 0o700); err != nil {
		t.Fatalf("create processing root: %v", err)
	}
	claimDirectory, err := os.MkdirTemp(processingRoot(rule), "claim-")
	if err != nil {
		t.Fatalf("create claim directory: %v", err)
	}
	claimedPath := filepath.Join(claimDirectory, "export.csv")
	writeFile(t, claimedPath)
	archivePath := filepath.Join(archiveDirectory, "export.csv")
	if err := os.Link(claimedPath, archivePath); err != nil {
		t.Fatalf("create partial archive link: %v", err)
	}

	if err := RunOnce(
		context.Background(),
		config.Config{Rules: []config.Rule{rule}},
		&bytes.Buffer{},
		&bytes.Buffer{},
	); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if _, err := os.Stat(claimedPath); !os.IsNotExist(err) {
		t.Errorf("claimed source should be removed, stat error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(archiveDirectory, "export.1.csv")); !os.IsNotExist(err) {
		t.Errorf("duplicate archive should not exist, stat error = %v", err)
	}
	archived, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read completed archive: %v", err)
	}
	if got, want := string(archived), "test"; got != want {
		t.Errorf("archive contents = %q, want %q", got, want)
	}
}

func TestRunOnceDoesNotDeleteReplacementFile(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "export.csv")
	writeFile(t, file)
	readyPath := filepath.Join(t.TempDir(), "ready")
	continuePath := filepath.Join(t.TempDir(), "continue")
	t.Setenv("READY_PATH", readyPath)
	t.Setenv("CONTINUE_PATH", continuePath)

	cfg := config.Config{Rules: []config.Rule{
		{
			Name:      "bank",
			Directory: directory,
			Patterns:  []string{"*.csv"},
			Command:   `touch "$READY_PATH"; while [ ! -e "$CONTINUE_PATH" ]; do sleep 0.01; done`,
			OnSuccess: config.SuccessDelete,
		},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- RunOnce(ctx, cfg, &bytes.Buffer{}, &bytes.Buffer{})
	}()

	waitForPath(t, readyPath)
	if err := os.WriteFile(file, []byte("replacement"), 0o600); err != nil {
		t.Fatalf("write replacement file: %v", err)
	}
	if err := os.WriteFile(continuePath, nil, 0o600); err != nil {
		t.Fatalf("allow command to continue: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunOnce() did not complete")
	}

	replacement, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read replacement file: %v", err)
	}
	if got, want := string(replacement), "replacement"; got != want {
		t.Errorf("replacement contents = %q, want %q", got, want)
	}
}

func TestRunOnceRejectsArchiveAliasOfWatchedDirectory(t *testing.T) {
	directory := t.TempDir()
	archiveAlias := filepath.Join(t.TempDir(), "archive")
	if err := os.Symlink(directory, archiveAlias); err != nil {
		t.Fatalf("create archive symlink: %v", err)
	}
	writeFile(t, filepath.Join(directory, "export.csv"))

	cfg := config.Config{Rules: []config.Rule{
		{
			Name:             "bank",
			Directory:        directory,
			Patterns:         []string{"*.csv"},
			Command:          "true",
			OnSuccess:        config.SuccessArchive,
			ArchiveDirectory: archiveAlias,
		},
	}}

	err := RunOnce(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "resolves to the watched directory") {
		t.Fatalf("RunOnce() error = %v, want archive alias error", err)
	}
}

func TestRunOnceTerminatesCommandProcessGroupOnCancellation(t *testing.T) {
	directory := t.TempDir()
	writeFile(t, filepath.Join(directory, "export.csv"))
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	heartbeatPath := filepath.Join(t.TempDir(), "heartbeat")
	t.Setenv("PID_PATH", pidPath)
	t.Setenv("HEARTBEAT_PATH", heartbeatPath)

	cfg := config.Config{Rules: []config.Rule{
		{
			Name:      "bank",
			Directory: directory,
			Patterns:  []string{"*.csv"},
			Command:   `sh -c 'trap "" TERM; while :; do printf x >> "$HEARTBEAT_PATH"; sleep 0.05; done' & child=$!; printf '%s' "$child" > "$PID_PATH"; wait "$child"`,
		},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- RunOnce(ctx, cfg, &bytes.Buffer{}, &bytes.Buffer{})
	}()

	var pid int
	waitForPath(t, pidPath)
	contents, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	pid, err = strconv.Atoi(string(contents))
	if err != nil {
		t.Fatalf("parse child pid: %v", err)
	}
	if pid == 0 {
		t.Fatal("command wrote an invalid child pid")
	}
	waitForPath(t, heartbeatPath)

	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "command cancelled") {
			t.Fatalf("RunOnce() error = %v, want cancellation error", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunOnce() did not stop after cancellation")
	}
	heartbeat, err := os.Stat(heartbeatPath)
	if err != nil {
		t.Fatalf("inspect heartbeat: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	heartbeatAfterWait, err := os.Stat(heartbeatPath)
	if err != nil {
		t.Fatalf("inspect heartbeat after wait: %v", err)
	}
	if heartbeatAfterWait.Size() != heartbeat.Size() {
		t.Errorf(
			"child process %d is still writing: heartbeat grew from %d to %d bytes",
			pid,
			heartbeat.Size(),
			heartbeatAfterWait.Size(),
		)
	}
}

func waitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, err := os.Stat(path)
		if err == nil {
			return
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("inspect %q: %v", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%q was not created", path)
}

func claimedPaths(t *testing.T, rule config.Rule) []string {
	t.Helper()
	claims, err := claimedFiles(rule)
	if err != nil {
		t.Fatalf("claimedFiles() error = %v", err)
	}
	paths := make([]string, len(claims))
	for index, claim := range claims {
		paths[index] = claim.path
	}
	return paths
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
