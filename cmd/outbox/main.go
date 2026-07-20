package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Moret84/outbox/internal/config"
	"github.com/Moret84/outbox/internal/runlock"
	"github.com/Moret84/outbox/internal/runner"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: outbox run-once [--config path]")
	}

	switch args[0] {
	case "run-once":
		flags := flag.NewFlagSet("run-once", flag.ContinueOnError)
		flags.SetOutput(stderr)
		configPath := flags.String("config", "outbox.yaml", "path to the YAML configuration")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("run-once does not accept positional arguments")
		}

		return runOnce(ctx, *configPath, stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q; usage: outbox run-once [--config path]", args[0])
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
