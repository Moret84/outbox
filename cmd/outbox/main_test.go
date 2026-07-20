package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPollRunsImmediatelyRetriesAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	var stderr bytes.Buffer

	err := poll(ctx, time.Millisecond, func() error {
		calls++
		if calls == 1 {
			return errors.New("temporary failure")
		}
		cancel()
		return nil
	}, &stderr)

	if err != nil {
		t.Fatalf("poll() error = %v", err)
	}
	if calls != 2 {
		t.Errorf("process calls = %d, want 2", calls)
	}
	if !strings.Contains(stderr.String(), "temporary failure") {
		t.Errorf("stderr = %q, want temporary failure", stderr.String())
	}
}
