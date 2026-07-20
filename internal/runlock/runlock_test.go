package runlock

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAcquireRejectsConcurrentLockForSameConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "outbox.yaml")
	lockDirectory := t.TempDir()

	first, err := acquire(configPath, lockDirectory)
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}
	t.Cleanup(func() {
		_ = first.Close()
	})

	_, err = acquire(configPath, lockDirectory)
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second acquire() error = %v, want ErrAlreadyRunning", err)
	}
}

func TestAcquireAllowsLockAfterRelease(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "outbox.yaml")
	lockDirectory := t.TempDir()

	first, err := acquire(configPath, lockDirectory)
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}

	second, err := acquire(configPath, lockDirectory)
	if err != nil {
		t.Fatalf("second acquire() after release error = %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}
