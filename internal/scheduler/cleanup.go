package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fadlee/gowa-manager/internal/instances"
)

// InstanceLister lists instances to clean. instances.Repository satisfies it.
type InstanceLister interface {
	List(ctx context.Context) ([]instances.Instance, error)
}

// DirResolver resolves an instance id to its on-disk directory.
// instances.Filesystem satisfies it and validates the path stays under the
// data directory.
type DirResolver interface {
	InstanceDir(id int64) (string, error)
}

// CleanupResult holds the aggregate outcome of a cleanup run.
type CleanupResult struct {
	Deleted  int
	Errors   int
	Duration time.Duration
}

// CleanupOptions configures a Cleanup job. Now defaults to time.Now and
// Logger to slog.Default when nil.
type CleanupOptions struct {
	Lister   InstanceLister
	Resolver DirResolver
	Logger   *slog.Logger
	Now      func() time.Time
}

// Cleanup removes JPEG files from each instance's storages/ directory and
// all entries from each instance's statics/media/ directory. It mirrors the
// legacy Bun CleanupScheduler: missing directories are no-ops, per-file
// errors are logged and skipped, and a failure for one instance does not
// stop the others.
type Cleanup struct {
	lister   InstanceLister
	resolver DirResolver
	logger   *slog.Logger
	now      func() time.Time
}

// NewCleanup builds a Cleanup job from opts.
func NewCleanup(opts CleanupOptions) *Cleanup {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Cleanup{
		lister:   opts.Lister,
		resolver: opts.Resolver,
		logger:   logger,
		now:      now,
	}
}

// Run iterates every instance and cleans its storages/ and statics/media/
// directories. The context is honored between instances (and within file
// loops) so a cancelled run stops promptly. Per-instance failures are
// counted but never abort the whole run.
func (c *Cleanup) Run(ctx context.Context) (CleanupResult, error) {
	start := c.now()
	items, err := c.lister.List(ctx)
	if err != nil {
		return CleanupResult{}, err
	}

	var total, errs int
	for _, inst := range items {
		if err := ctx.Err(); err != nil {
			break
		}
		n, err := c.cleanupInstance(ctx, inst)
		if err != nil {
			errs++
			c.logger.Warn("cleanup: instance failed", "id", inst.ID, "name", inst.Name, "error", err)
			continue
		}
		if n > 0 {
			c.logger.Info("cleanup: instance cleaned", "id", inst.ID, "name", inst.Name, "deleted", n)
		}
		total += n
	}

	res := CleanupResult{Deleted: total, Errors: errs, Duration: c.now().Sub(start)}
	c.logger.Info("cleanup: completed", "deleted", res.Deleted, "errors", res.Errors, "duration", res.Duration)
	return res, nil
}

func (c *Cleanup) cleanupInstance(ctx context.Context, inst instances.Instance) (int, error) {
	dir, err := c.resolver.InstanceDir(inst.ID)
	if err != nil {
		return 0, fmt.Errorf("resolve instance dir: %w", err)
	}
	storageDir, err := safeSubdir(dir, "storages")
	if err != nil {
		return 0, err
	}
	mediaDir, err := safeSubdir(dir, "statics", "media")
	if err != nil {
		return 0, err
	}
	jpegs := c.cleanStorageJpegs(ctx, storageDir)
	media := c.cleanMediaFiles(ctx, mediaDir)
	return jpegs + media, nil
}

// cleanStorageJpegs deletes *.jpg and *.jpeg files (case-insensitive) from
// dir. Subdirectories are never deleted even if their name ends in .jpg.
// Missing dir => 0. Per-file errors are logged and skipped.
func (c *Cleanup) cleanStorageJpegs(ctx context.Context, dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			c.logger.Warn("cleanup: read storage dir failed", "error", err)
		}
		return 0
	}
	var n int
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return n
		}
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if !strings.HasSuffix(name, ".jpg") && !strings.HasSuffix(name, ".jpeg") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if err := os.Remove(path); err != nil {
			if !os.IsNotExist(err) {
				c.logger.Warn("cleanup: failed to delete jpeg", "error", err)
			}
			continue
		}
		n++
	}
	return n
}

// cleanMediaFiles deletes every entry (files and subdirectories) from dir.
// Subdirectories are removed recursively and counted as a single deletion
// unit. Missing dir => 0. Per-entry errors are logged and skipped.
func (c *Cleanup) cleanMediaFiles(ctx context.Context, dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			c.logger.Warn("cleanup: read media dir failed", "error", err)
		}
		return 0
	}
	var n int
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return n
		}
		path := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if err := os.RemoveAll(path); err != nil {
				c.logger.Warn("cleanup: failed to remove media subdir", "error", err)
				continue
			}
			n++
			continue
		}
		if err := os.Remove(path); err != nil {
			if !os.IsNotExist(err) {
				c.logger.Warn("cleanup: failed to delete media file", "error", err)
			}
			continue
		}
		n++
	}
	return n
}

// safeSubdir joins parts under base and verifies the result stays within
// base, mirroring instances.Filesystem.requireUnderDataDir so a malformed
// instance dir cannot cause cleanup to escape the instance tree.
func safeSubdir(base string, parts ...string) (string, error) {
	baseClean, err := filepath.Abs(filepath.Clean(base))
	if err != nil {
		return "", err
	}
	target := filepath.Join(append([]string{base}, parts...)...)
	targetClean, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(baseClean, targetClean)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path escapes instance dir: %s", target)
	}
	return targetClean, nil
}
