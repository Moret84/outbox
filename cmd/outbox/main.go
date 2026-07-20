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

func runOnce(ctx context.Context, configPath string, stdout, stderr io.Writer) error {
	return withConfig(configPath, func(cfg config.Config) error {
		return runner.RunOnce(ctx, cfg, stdout, stderr)
	})
}

func runContinuously(
	ctx context.Context,
	configPath string,
	stdout, stderr io.Writer,
) error {
	return withConfig(configPath, func(cfg config.Config) error {
		process := func() error {
			return runner.RunOnce(ctx, cfg, stdout, stderr)
		}
		return poll(ctx, cfg.PollInterval(), process, stderr)
	})
}

func withConfig(configPath string, action func(config.Config) error) (runErr error) {
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
	return action(cfg)
}

func poll(ctx context.Context, interval time.Duration, process func() error, stderr io.Writer) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := process(); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintln(stderr, err)
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
