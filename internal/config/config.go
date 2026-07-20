package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Interval Duration `yaml:"interval"`
	Rules    []Rule   `yaml:"rules"`
}

type Rule struct {
	Name             string        `yaml:"name"`
	Directory        string        `yaml:"directory"`
	Patterns         []string      `yaml:"patterns"`
	MinimumAge       Duration      `yaml:"minimum_age"`
	Command          string        `yaml:"command"`
	OnSuccess        SuccessAction `yaml:"on_success"`
	ArchiveDirectory string        `yaml:"archive_directory"`
}

type SuccessAction string

const (
	SuccessKeep    SuccessAction = "keep"
	SuccessDelete  SuccessAction = "delete"
	SuccessArchive SuccessAction = "archive"

	DefaultInterval = 30 * time.Second
)

type Duration struct {
	time.Duration
	set bool
}

func (duration *Duration) UnmarshalYAML(node *yaml.Node) error {
	parsed, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", node.Value, err)
	}
	duration.Duration = parsed
	duration.set = true
	return nil
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config %q: %w", path, err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, fmt.Errorf("decode config %q: multiple YAML documents are not supported", path)
		}
		return Config{}, fmt.Errorf("decode config %q: %w", path, err)
	}

	if err := cfg.validate(filepath.Dir(path)); err != nil {
		return Config{}, fmt.Errorf("validate config %q: %w", path, err)
	}

	return cfg, nil
}

func (cfg *Config) validate(configDirectory string) error {
	if !cfg.Interval.set {
		cfg.Interval.Duration = DefaultInterval
	}
	if cfg.Interval.Duration <= 0 {
		return errors.New("interval must be greater than zero")
	}

	if len(cfg.Rules) == 0 {
		return errors.New("at least one rule is required")
	}

	names := make(map[string]struct{}, len(cfg.Rules))
	for index := range cfg.Rules {
		rule := &cfg.Rules[index]
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf("rule %d: name is required", index+1)
		}
		if _, exists := names[rule.Name]; exists {
			return fmt.Errorf("rule %q: name must be unique", rule.Name)
		}
		names[rule.Name] = struct{}{}

		if strings.TrimSpace(rule.Directory) == "" {
			return fmt.Errorf("rule %q: directory is required", rule.Name)
		}
		directory, err := resolveDirectory(rule.Directory, configDirectory)
		if err != nil {
			return fmt.Errorf("rule %q: %w", rule.Name, err)
		}
		rule.Directory = directory

		if len(rule.Patterns) == 0 {
			return fmt.Errorf("rule %q: at least one pattern is required", rule.Name)
		}
		for _, pattern := range rule.Patterns {
			if pattern == "" {
				return fmt.Errorf("rule %q: patterns cannot be empty", rule.Name)
			}
			if _, err := filepath.Match(pattern, ""); err != nil {
				return fmt.Errorf("rule %q: invalid pattern %q: %w", rule.Name, pattern, err)
			}
		}

		if strings.TrimSpace(rule.Command) == "" {
			return fmt.Errorf("rule %q: command is required", rule.Name)
		}
		if rule.MinimumAge.Duration < 0 {
			return fmt.Errorf("rule %q: minimum_age cannot be negative", rule.Name)
		}

		if rule.OnSuccess == "" {
			rule.OnSuccess = SuccessKeep
		}
		switch rule.OnSuccess {
		case SuccessKeep, SuccessDelete:
			if rule.ArchiveDirectory != "" {
				return fmt.Errorf(
					"rule %q: archive_directory requires on_success: archive",
					rule.Name,
				)
			}
		case SuccessArchive:
			if strings.TrimSpace(rule.ArchiveDirectory) == "" {
				return fmt.Errorf(
					"rule %q: archive_directory is required for on_success: archive",
					rule.Name,
				)
			}
			archiveDirectory, err := resolveDirectory(rule.ArchiveDirectory, configDirectory)
			if err != nil {
				return fmt.Errorf("rule %q: resolve archive_directory: %w", rule.Name, err)
			}
			if archiveDirectory == rule.Directory {
				return fmt.Errorf("rule %q: archive_directory must differ from directory", rule.Name)
			}
			rule.ArchiveDirectory = archiveDirectory
		default:
			return fmt.Errorf(
				"rule %q: on_success must be keep, delete, or archive",
				rule.Name,
			)
		}
	}

	return nil
}

func resolveDirectory(directory, configDirectory string) (string, error) {
	if directory == "~" || strings.HasPrefix(directory, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		directory = filepath.Join(home, strings.TrimPrefix(directory, "~/"))
	}
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(configDirectory, directory)
	}

	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("resolve directory %q: %w", directory, err)
	}
	return filepath.Clean(absolute), nil
}
