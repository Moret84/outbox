package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/Moret84/outbox/internal/config"
)

func RunOnce(ctx context.Context, cfg config.Config, stdout, stderr io.Writer) error {
	var runErrors []error
	for _, rule := range cfg.Rules {
		files, err := matchingFiles(rule)
		if err != nil {
			runErrors = append(runErrors, err)
			continue
		}

		for _, file := range files {
			fmt.Fprintf(stdout, "[%s] processing %s\n", rule.Name, file)
			if err := runCommand(ctx, rule, file, stdout, stderr); err != nil {
				runErrors = append(runErrors, err)
			}
		}
	}

	return errors.Join(runErrors...)
}

func matchingFiles(rule config.Rule) ([]string, error) {
	entries, err := os.ReadDir(rule.Directory)
	if err != nil {
		return nil, fmt.Errorf("rule %q: read directory %q: %w", rule.Name, rule.Directory, err)
	}

	files := make([]string, 0)
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		for _, pattern := range rule.Patterns {
			matched, err := filepath.Match(pattern, entry.Name())
			if err != nil {
				return nil, fmt.Errorf("rule %q: match pattern %q: %w", rule.Name, pattern, err)
			}
			if matched {
				files = append(files, filepath.Join(rule.Directory, entry.Name()))
				break
			}
		}
	}
	sort.Strings(files)
	return files, nil
}

func runCommand(
	ctx context.Context,
	rule config.Rule,
	file string,
	stdout, stderr io.Writer,
) error {
	command := exec.CommandContext(ctx, "/bin/sh", "-c", rule.Command)
	command.Env = append(
		os.Environ(),
		"FILE="+file,
		"DIRECTORY="+rule.Directory,
		"RULE="+rule.Name,
	)
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("rule %q: command failed for %q: %w", rule.Name, file, err)
	}
	return nil
}
