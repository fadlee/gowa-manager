package ownership

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/gofrs/flock"
)

var ErrAlreadyLocked = errors.New("data directory is already locked by another manager")

type Lock struct {
	mu       sync.Mutex
	fileLock *flock.Flock
	released bool
}

func Acquire(dataDir string) (*Lock, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory %q: %w", dataDir, err)
	}
	path := filepath.Join(dataDir, ".gowa-manager.lock")
	fileLock := flock.New(path)
	locked, err := fileLock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("acquire manager lock %q: %w", path, err)
	}
	if !locked {
		return nil, fmt.Errorf("acquire manager lock %q: %w", path, ErrAlreadyLocked)
	}
	return &Lock{fileLock: fileLock}, nil
}

func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released {
		return nil
	}
	l.released = true
	if err := l.fileLock.Unlock(); err != nil {
		return fmt.Errorf("release manager lock %q: %w", l.fileLock.Path(), err)
	}
	return nil
}
