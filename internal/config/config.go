package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Rules []Rule `yaml:"rules"`
}

type Rule struct {
	Name      string   `yaml:"name"`
	Directory string   `yaml:"directory"`
	Patterns  []string `yaml:"patterns"`
	Command   string   `yaml:"command"`
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
