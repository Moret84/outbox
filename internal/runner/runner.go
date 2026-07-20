package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Moret84/outbox/internal/config"
	"golang.org/x/sys/unix"
)

const (
	processingDirectoryName = ".outbox-processing"
	terminationGracePeriod  = time.Second
	processCheckInterval    = 20 * time.Millisecond
)

type queuedFile struct {
	path         string
	originalName string
	claimed      bool
}

func RunOnce(ctx context.Context, cfg config.Config, stdout, stderr io.Writer) error {
	return runOnceAt(ctx, cfg, stdout, stderr, time.Now())
}

func runOnceAt(
	ctx context.Context,
	cfg config.Config,
	stdout, stderr io.Writer,
	now time.Time,
) error {
	var runErrors []error
	for _, rule := range cfg.Rules {
		if err := prepareRule(rule); err != nil {
			runErrors = append(runErrors, err)
			continue
		}

		files, err := matchingFiles(rule, now)
		if err != nil {
			runErrors = append(runErrors, err)
			continue
		}

		for _, file := range files {
			if err := ctx.Err(); err != nil {
				runErrors = append(runErrors, err)
				return errors.Join(runErrors...)
			}
			if !file.claimed && rule.OnSuccess != config.SuccessKeep && rule.OnSuccess != "" {
				file, err = claimFile(rule, file)
				if err != nil {
					runErrors = append(runErrors, err)
					continue
				}
			}

			fmt.Fprintf(stdout, "[%s] processing %s\n", rule.Name, file.originalName)
			if err := runCommand(ctx, rule, file.path, stdout, stderr); err != nil {
				runErrors = append(runErrors, err)
				continue
			}
			if err := applySuccessAction(rule, file, stdout); err != nil {
				runErrors = append(runErrors, err)
			}
		}
	}

	return errors.Join(runErrors...)
}

func prepareRule(rule config.Rule) error {
	if rule.OnSuccess != config.SuccessArchive {
		return nil
	}
	if err := os.MkdirAll(rule.ArchiveDirectory, 0o700); err != nil {
		return fmt.Errorf(
			"rule %q: create archive directory %q: %w",
			rule.Name,
			rule.ArchiveDirectory,
			err,
		)
	}

	sourceInfo, err := os.Stat(rule.Directory)
	if err != nil {
		return fmt.Errorf("rule %q: inspect directory %q: %w", rule.Name, rule.Directory, err)
	}
	archiveInfo, err := os.Stat(rule.ArchiveDirectory)
	if err != nil {
		return fmt.Errorf(
			"rule %q: inspect archive directory %q: %w",
			rule.Name,
			rule.ArchiveDirectory,
			err,
		)
	}
	if os.SameFile(sourceInfo, archiveInfo) {
		return fmt.Errorf("rule %q: archive directory resolves to the watched directory", rule.Name)
	}
	sourceStat, sourceOK := sourceInfo.Sys().(*syscall.Stat_t)
	archiveStat, archiveOK := archiveInfo.Sys().(*syscall.Stat_t)
	if !sourceOK || !archiveOK {
		return fmt.Errorf("rule %q: cannot determine directory filesystems", rule.Name)
	}
	if sourceStat.Dev != archiveStat.Dev {
		return fmt.Errorf("rule %q: archive directory must use the watched directory filesystem", rule.Name)
	}
	return nil
}

func matchingFiles(rule config.Rule, now time.Time) ([]queuedFile, error) {
	files, err := claimedFiles(rule)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(rule.Directory)
	if err != nil {
		return nil, fmt.Errorf("rule %q: read directory %q: %w", rule.Name, rule.Directory, err)
	}

	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		if rule.MinimumAge.Duration > 0 {
			info, err := entry.Info()
			if err != nil {
				return nil, fmt.Errorf(
					"rule %q: inspect file %q: %w",
					rule.Name,
					entry.Name(),
					err,
				)
			}
			if info.ModTime().After(now.Add(-rule.MinimumAge.Duration)) {
				continue
			}
		}
		for _, pattern := range rule.Patterns {
			matched, err := filepath.Match(pattern, entry.Name())
			if err != nil {
				return nil, fmt.Errorf("rule %q: match pattern %q: %w", rule.Name, pattern, err)
			}
			if matched {
				files = append(files, queuedFile{
					path:         filepath.Join(rule.Directory, entry.Name()),
					originalName: entry.Name(),
				})
				break
			}
		}
	}
	sort.Slice(files, func(left, right int) bool {
		if files[left].claimed != files[right].claimed {
			return files[left].claimed
		}
		return files[left].path < files[right].path
	})
	return files, nil
}

func claimedFiles(rule config.Rule) ([]queuedFile, error) {
	root := processingRoot(rule)
	claimDirectories, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("rule %q: read processing directory %q: %w", rule.Name, root, err)
	}

	files := make([]queuedFile, 0, len(claimDirectories))
	for _, claimDirectory := range claimDirectories {
		if !claimDirectory.IsDir() {
			continue
		}
		claimPath := filepath.Join(root, claimDirectory.Name())
		entries, err := os.ReadDir(claimPath)
		if err != nil {
			return nil, fmt.Errorf("rule %q: read claim %q: %w", rule.Name, claimPath, err)
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() {
				files = append(files, queuedFile{
					path:         filepath.Join(claimPath, entry.Name()),
					originalName: entry.Name(),
					claimed:      true,
				})
			}
		}
		if len(entries) == 0 {
			_ = os.Remove(claimPath)
		}
	}
	return files, nil
}

func claimFile(rule config.Rule, file queuedFile) (queuedFile, error) {
	root := processingRoot(rule)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return queuedFile{}, fmt.Errorf("rule %q: create processing directory: %w", rule.Name, err)
	}
	claimDirectory, err := os.MkdirTemp(root, "claim-")
	if err != nil {
		return queuedFile{}, fmt.Errorf("rule %q: create file claim: %w", rule.Name, err)
	}
	destination := filepath.Join(claimDirectory, file.originalName)
	if err := os.Rename(file.path, destination); err != nil {
		_ = os.Remove(claimDirectory)
		return queuedFile{}, fmt.Errorf("rule %q: claim %q: %w", rule.Name, file.path, err)
	}
	return queuedFile{path: destination, originalName: file.originalName, claimed: true}, nil
}

func processingRoot(rule config.Rule) string {
	hash := sha256.Sum256([]byte(rule.Name))
	ruleDirectory := hex.EncodeToString(hash[:8])
	return filepath.Join(rule.Directory, processingDirectoryName, ruleDirectory)
}

func applySuccessAction(rule config.Rule, file queuedFile, stdout io.Writer) error {
	switch rule.OnSuccess {
	case "", config.SuccessKeep:
		if file.claimed {
			if err := restoreClaimedFile(rule, file); err != nil {
				return err
			}
			removeClaimDirectory(file)
		}
		return nil
	case config.SuccessDelete:
		if err := os.Remove(file.path); err != nil {
			return fmt.Errorf("rule %q: delete %q: %w", rule.Name, file.path, err)
		}
		removeClaimDirectory(file)
		fmt.Fprintf(stdout, "[%s] deleted %s\n", rule.Name, file.originalName)
		return nil
	case config.SuccessArchive:
		destination, err := archiveFile(rule.ArchiveDirectory, file)
		if err != nil {
			return fmt.Errorf("rule %q: archive %q: %w", rule.Name, file.path, err)
		}
		removeClaimDirectory(file)
		fmt.Fprintf(stdout, "[%s] archived %s to %s\n", rule.Name, file.originalName, destination)
		return nil
	default:
		return fmt.Errorf("rule %q: unsupported success action %q", rule.Name, rule.OnSuccess)
	}
}

func restoreClaimedFile(rule config.Rule, file queuedFile) error {
	destination := filepath.Join(rule.Directory, file.originalName)
	if err := os.Link(file.path, destination); err != nil {
		return fmt.Errorf("rule %q: restore %q: %w", rule.Name, file.path, err)
	}
	if err := removeClaimedSource(file, destination); err != nil {
		return fmt.Errorf("rule %q: restore %q: %w", rule.Name, file.path, err)
	}
	return nil
}

func archiveFile(directory string, file queuedFile) (string, error) {
	extension := filepath.Ext(file.originalName)
	base := strings.TrimSuffix(file.originalName, extension)
	for suffix := 0; ; suffix++ {
		candidateName := file.originalName
		if suffix > 0 {
			candidateName = fmt.Sprintf("%s.%d%s", base, suffix, extension)
		}
		candidate := filepath.Join(directory, candidateName)
		if err := os.Link(file.path, candidate); err != nil {
			if errors.Is(err, os.ErrExist) {
				sourceInfo, sourceErr := os.Stat(file.path)
				candidateInfo, candidateErr := os.Stat(candidate)
				if sourceErr == nil && candidateErr == nil && os.SameFile(sourceInfo, candidateInfo) {
					if err := os.Remove(file.path); err != nil {
						return "", err
					}
					return candidate, nil
				}
				continue
			}
			return "", err
		}
		if err := removeClaimedSource(file, candidate); err != nil {
			return "", err
		}
		return candidate, nil
	}
}

func removeClaimedSource(file queuedFile, linkedPath string) error {
	if err := os.Remove(file.path); err != nil {
		rollbackErr := os.Remove(linkedPath)
		return errors.Join(err, rollbackErr)
	}
	return nil
}

func removeClaimDirectory(file queuedFile) {
	if file.claimed {
		_ = os.Remove(filepath.Dir(file.path))
	}
}

func runCommand(
	ctx context.Context,
	rule config.Rule,
	file string,
	stdout, stderr io.Writer,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("rule %q: command cancelled for %q: %w", rule.Name, file, err)
	}
	command := exec.Command("/bin/sh", "-c", rule.Command)
	command.Env = append(
		os.Environ(),
		"FILE="+file,
		"DIRECTORY="+rule.Directory,
		"RULE="+rule.Name,
	)
	command.Stdout = stdout
	command.Stderr = stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return fmt.Errorf("rule %q: start command for %q: %w", rule.Name, file, err)
	}

	wait := make(chan error, 1)
	go func() {
		wait <- command.Wait()
	}()

	select {
	case err := <-wait:
		if err == nil {
			return nil
		}
		return fmt.Errorf("rule %q: command failed for %q: %w", rule.Name, file, err)
	case <-ctx.Done():
		_ = unix.Kill(-command.Process.Pid, unix.SIGTERM)
		stopProcessGroup(command.Process.Pid, wait)
		return fmt.Errorf("rule %q: command cancelled for %q: %w", rule.Name, file, ctx.Err())
	}
}

func stopProcessGroup(pid int, wait <-chan error) {
	timer := time.NewTimer(terminationGracePeriod)
	defer timer.Stop()
	ticker := time.NewTicker(processCheckInterval)
	defer ticker.Stop()

	shellExited := false
	for {
		if err := unix.Kill(-pid, 0); errors.Is(err, unix.ESRCH) {
			if !shellExited {
				<-wait
			}
			return
		}

		select {
		case <-wait:
			shellExited = true
			wait = nil
		case <-ticker.C:
		case <-timer.C:
			_ = unix.Kill(-pid, unix.SIGKILL)
			if !shellExited {
				<-wait
			}
			return
		}
	}
}
