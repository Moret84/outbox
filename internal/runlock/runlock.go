package runlock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var ErrAlreadyRunning = errors.New("another outbox instance is already running")

type Lock struct {
	file *os.File
}

func Acquire(configPath string) (*Lock, error) {
	cacheDirectory, err := os.UserCacheDir()
	if err != nil {
		cacheDirectory = os.TempDir()
	}
	return acquire(configPath, filepath.Join(cacheDirectory, "outbox", "locks"))
}

func acquire(configPath, lockDirectory string) (*Lock, error) {
	canonicalPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("resolve config path %q: %w", configPath, err)
	}
	if resolvedPath, err := filepath.EvalSymlinks(canonicalPath); err == nil {
		canonicalPath = resolvedPath
	}

	if err := os.MkdirAll(lockDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory %q: %w", lockDirectory, err)
	}

	hash := sha256.Sum256([]byte(canonicalPath))
	lockPath := filepath.Join(lockDirectory, hex.EncodeToString(hash[:])+".lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %q: %w", lockPath, err)
	}
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		closeErr := lockFile.Close()
		if closeErr != nil {
			closeErr = fmt.Errorf("close lock file %q: %w", lockPath, closeErr)
		}
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, errors.Join(
				fmt.Errorf("%w for config %q", ErrAlreadyRunning, canonicalPath),
				closeErr,
			)
		}
		return nil, errors.Join(
			fmt.Errorf("lock config %q: %w", canonicalPath, err),
			closeErr,
		)
	}

	return &Lock{file: lockFile}, nil
}

func (lock *Lock) Close() error {
	unlockErr := unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
	closeErr := lock.file.Close()
	if unlockErr != nil {
		unlockErr = fmt.Errorf("unlock outbox lock: %w", unlockErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close outbox lock: %w", closeErr)
	}
	return errors.Join(unlockErr, closeErr)
}
