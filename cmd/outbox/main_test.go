package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunLoopRunsImmediatelyRetriesAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	calls := 0
	var stderr bytes.Buffer
	err := runLoop(ctx, time.Millisecond, func() error {
		calls++
		if calls == 1 {
			return errors.New("temporary failure")
		}
		cancel()
		return nil
	}, &stderr)

	if err != nil {
		t.Fatalf("runLoop() error = %v", err)
	}
	if calls != 2 {
		t.Errorf("process calls = %d, want 2", calls)
	}
	if !strings.Contains(stderr.String(), "temporary failure") {
		t.Errorf("stderr = %q, want temporary failure", stderr.String())
	}
}

func TestRunLoopDoesNotProcessCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	err := runLoop(ctx, time.Hour, func() error {
		called = true
		return nil
	}, &bytes.Buffer{})

	if err != nil {
		t.Fatalf("runLoop() error = %v", err)
	}
	if called {
		t.Error("process was called with a cancelled context")
	}
}
