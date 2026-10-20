package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Moret84/outbox/internal/config"
	"github.com/Moret84/outbox/internal/runlock"
	"github.com/Moret84/outbox/internal/runner"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: outbox <run|run-once> [--config path]")
	}

	switch args[0] {
	case "run", "run-once":
		command := args[0]
		flags := flag.NewFlagSet(command, flag.ContinueOnError)
		flags.SetOutput(stderr)
		configPath := flags.String("config", "outbox.yaml", "path to the YAML configuration")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("%s does not accept positional arguments", command)
		}

		if command == "run" {
			return runContinuously(ctx, *configPath, stdout, stderr)
		}
		return runOnce(ctx, *configPath, stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q; usage: outbox <run|run-once> [--config path]", args[0])
	}
}

func runOnce(ctx context.Context, configPath string, stdout, stderr io.Writer) (runErr error) {
	lock, err := runlock.Acquire(configPath)
	if err != nil {
		return err
	}
	defer func() {
		runErr = errors.Join(runErr, lock.Close())
	}()

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	return runner.RunOnce(ctx, cfg, stdout, stderr)
}

func runContinuously(
	ctx context.Context,
	configPath string,
	stdout, stderr io.Writer,
) (runErr error) {
	lock, err := runlock.Acquire(configPath)
	if err != nil {
		return err
	}
	defer func() {
		runErr = errors.Join(runErr, lock.Close())
	}()

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := runner.RunOnce(ctx, cfg, stdout, stderr); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintln(stderr, err)
		}

		var stopped bool
		cfg, stopped = waitForValidConfig(ctx, configPath, cfg.PollInterval(), stderr)
		if stopped {
			return nil
		}
	}
}

// waitForValidConfig reloads the configuration after each interval until it
// is valid. While it is invalid no pass runs: falling back to the previous
// configuration could keep sending files to a destination being corrected.
func waitForValidConfig(
	ctx context.Context,
	configPath string,
	interval time.Duration,
	stderr io.Writer,
) (cfg config.Config, stopped bool) {
	for {
		if !wait(ctx, interval) {
			return config.Config{}, true
		}
		cfg, err := config.Load(configPath)
		if err == nil {
			return cfg, false
		}
		fmt.Fprintf(stderr, "%v; skipping pass until the configuration is fixed\n", err)
	}
}

func wait(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
