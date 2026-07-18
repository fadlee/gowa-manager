package instances

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Filesystem struct {
	dataDir   string
	mkdirAll  func(string, os.FileMode) error
	removeAll func(string) error
	rename    func(string, string) error
}

type Trash struct {
	InstanceID   int64
	OriginalPath string
	TrashPath    string
}

func NewFilesystem(dataDir string) (*Filesystem, error) {
	cleaned, err := filepath.Abs(filepath.Clean(dataDir))
	if err != nil {
		return nil, err
	}
	return &Filesystem{
		dataDir:   cleaned,
		mkdirAll:  os.MkdirAll,
		removeAll: os.RemoveAll,
		rename:    os.Rename,
	}, nil
}

func (f *Filesystem) InstanceDir(id int64) (string, error) {
	return f.safeJoin("instances", strconv.FormatInt(id, 10))
}

func (f *Filesystem) Ensure(ctx context.Context, id int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dir, err := f.InstanceDir(id)
	if err != nil {
		return "", err
	}
	if err := f.mkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func (f *Filesystem) StageDelete(ctx context.Context, id int64) (Trash, error) {
	if err := ctx.Err(); err != nil {
		return Trash{}, err
	}
	originalPath, err := f.InstanceDir(id)
	if err != nil {
		return Trash{}, err
	}
	info, err := os.Stat(originalPath)
	if errors.Is(err, os.ErrNotExist) {
		return Trash{}, ErrNotFound
	}
	if err != nil {
		return Trash{}, err
	}
	if !info.IsDir() {
		return Trash{}, fmt.Errorf("instance path is not a directory: %s", originalPath)
	}
	trashDir, err := f.safeJoin(".trash")
	if err != nil {
		return Trash{}, err
	}
	if err := f.mkdirAll(trashDir, 0o700); err != nil {
		return Trash{}, err
	}
	trashPath, err := f.newTrashPath(id)
	if err != nil {
		_ = f.removeAll(trashDir)
		return Trash{}, err
	}
	if err := f.rename(originalPath, trashPath); err != nil {
		_ = f.removeAll(trashPath)
		cleanupEmptyDir(trashDir)
		return Trash{}, err
	}
	return Trash{InstanceID: id, OriginalPath: originalPath, TrashPath: trashPath}, nil
}

func (f *Filesystem) Restore(ctx context.Context, trash Trash) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	originalPath, err := f.requireExpectedOriginalPath(trash)
	if err != nil {
		return err
	}
	trashPath, err := f.requireUnderDataDir(trash.TrashPath)
	if err != nil {
		return err
	}
	if err := f.requireTrashPath(trashPath); err != nil {
		return err
	}
	if err := f.mkdirAll(filepath.Dir(originalPath), 0o700); err != nil {
		return err
	}
	if err := f.rename(trashPath, originalPath); err != nil {
		return err
	}
	trashDir, err := f.safeJoin(".trash")
	if err != nil {
		return err
	}
	if err := f.mkdirAll(trashDir, 0o700); err != nil {
		_ = f.rename(originalPath, trashPath)
		return err
	}
	return nil
}

func (f *Filesystem) Purge(ctx context.Context, trash Trash) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	trashPath, err := f.requireUnderDataDir(trash.TrashPath)
	if err != nil {
		return err
	}
	if err := f.requireTrashPath(trashPath); err != nil {
		return err
	}
	if err := f.removeAll(trashPath); err != nil {
		return err
	}
	return nil
}

func (f *Filesystem) Reset(ctx context.Context, id int64) error {
	// Callers must serialize filesystem mutations for the same instance. Reset is
	// deliberately ordered so a failed purge restores the staged state before any
	// fresh visible instance directory is created.
	trash, err := f.StageDelete(ctx, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if trash != (Trash{}) {
		if purgeErr := f.Purge(ctx, trash); purgeErr != nil {
			if restoreErr := f.Restore(context.Background(), trash); restoreErr != nil {
				return fmt.Errorf("purge failed: %w; restore failed: %v", purgeErr, restoreErr)
			}
			return purgeErr
		}
	}
	if _, ensureErr := f.Ensure(ctx, id); ensureErr != nil {
		return ensureErr
	}
	return nil
}

func (f *Filesystem) requireExpectedOriginalPath(trash Trash) (string, error) {
	originalPath, err := f.requireUnderDataDir(trash.OriginalPath)
	if err != nil {
		return "", err
	}
	expectedPath, err := f.InstanceDir(trash.InstanceID)
	if err != nil {
		return "", err
	}
	if originalPath != expectedPath {
		return "", fmt.Errorf("trash original path %q does not match instance %d path %q", originalPath, trash.InstanceID, expectedPath)
	}
	return originalPath, nil
}

func (f *Filesystem) newTrashPath(id int64) (string, error) {
	for i := 0; i < 10; i++ {
		suffix, err := randomSuffix()
		if err != nil {
			return "", err
		}
		path, err := f.safeJoin(".trash", strconv.FormatInt(id, 10)+"-"+suffix)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("could not allocate trash path")
}

func randomSuffix() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (f *Filesystem) safeJoin(parts ...string) (string, error) {
	return f.requireUnderDataDir(filepath.Join(append([]string{f.dataDir}, parts...)...))
}

func (f *Filesystem) requireUnderDataDir(path string) (string, error) {
	cleaned, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(f.dataDir, cleaned)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path escapes data directory: %s", path)
	}
	return cleaned, nil
}

func (f *Filesystem) requireTrashPath(path string) error {
	trashDir, err := f.safeJoin(".trash")
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(trashDir, path)
	if err != nil {
		return err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("trash path escapes trash directory: %s", path)
	}
	return nil
}

func cleanupEmptyDir(path string) {
	entries, err := os.ReadDir(path)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(path)
	}
}
