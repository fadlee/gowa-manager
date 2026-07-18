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

var (
	filesystemMkdirAll  = os.MkdirAll
	filesystemRemoveAll = os.RemoveAll
	filesystemRename    = os.Rename
)

type Filesystem struct {
	dataDir string
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
	return &Filesystem{dataDir: cleaned}, nil
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
	if err := filesystemMkdirAll(dir, 0o700); err != nil {
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
	if err := filesystemMkdirAll(trashDir, 0o700); err != nil {
		return Trash{}, err
	}
	trashPath, err := f.newTrashPath(id)
	if err != nil {
		_ = filesystemRemoveAll(trashDir)
		return Trash{}, err
	}
	if err := filesystemRename(originalPath, trashPath); err != nil {
		_ = filesystemRemoveAll(trashPath)
		cleanupEmptyDir(trashDir)
		return Trash{}, err
	}
	return Trash{InstanceID: id, OriginalPath: originalPath, TrashPath: trashPath}, nil
}

func (f *Filesystem) Restore(ctx context.Context, trash Trash) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	originalPath, err := f.requireUnderDataDir(trash.OriginalPath)
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
	if err := filesystemMkdirAll(filepath.Dir(originalPath), 0o700); err != nil {
		return err
	}
	if err := filesystemRename(trashPath, originalPath); err != nil {
		return err
	}
	trashDir, err := f.safeJoin(".trash")
	if err != nil {
		return err
	}
	if err := filesystemMkdirAll(trashDir, 0o700); err != nil {
		_ = filesystemRename(originalPath, trashPath)
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
	if err := filesystemRemoveAll(trashPath); err != nil {
		return err
	}
	return nil
}

func (f *Filesystem) Reset(ctx context.Context, id int64) error {
	trash, err := f.StageDelete(ctx, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	dir, ensureErr := f.Ensure(ctx, id)
	if ensureErr != nil {
		if trash != (Trash{}) {
			_ = f.Restore(context.Background(), trash)
		}
		return ensureErr
	}
	if trash != (Trash{}) {
		if purgeErr := f.Purge(ctx, trash); purgeErr != nil {
			_ = filesystemRemoveAll(dir)
			_ = f.Restore(context.Background(), trash)
			return purgeErr
		}
	}
	return nil
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
