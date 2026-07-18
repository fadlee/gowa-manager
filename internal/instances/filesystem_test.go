package instances

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesystemEnsureCreatesInstanceDirectoryIdempotently(t *testing.T) {
	ctx := context.Background()
	fs := newTestFilesystem(t)

	dir, err := fs.Ensure(ctx, 42)
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	assertDirExists(t, dir)
	if filepath.Base(dir) != "42" || filepath.Base(filepath.Dir(dir)) != "instances" {
		t.Fatalf("Ensure dir = %q, want dataDir/instances/42", dir)
	}

	marker := filepath.Join(dir, "session.db")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	again, err := fs.Ensure(ctx, 42)
	if err != nil {
		t.Fatalf("Ensure second call returned error: %v", err)
	}
	if again != dir {
		t.Fatalf("Ensure second dir = %q, want %q", again, dir)
	}
	assertFileContent(t, marker, "keep")
}

func TestFilesystemStageDeleteRenamesIntoTrashAndRestoreMovesBack(t *testing.T) {
	ctx := context.Background()
	fs := newTestFilesystem(t)
	dir, err := fs.Ensure(ctx, 7)
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	marker := filepath.Join(dir, "creds.json")
	if err := os.WriteFile(marker, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	trash, err := fs.StageDelete(ctx, 7)
	if err != nil {
		t.Fatalf("StageDelete returned error: %v", err)
	}
	if trash.InstanceID != 7 || trash.OriginalPath != dir {
		t.Fatalf("Trash = %#v, want instance id and original path", trash)
	}
	assertMissing(t, dir)
	assertDirExists(t, trash.TrashPath)
	assertFileContent(t, filepath.Join(trash.TrashPath, "creds.json"), "secret")
	assertUnderDir(t, filepath.Join(fs.dataDir, ".trash"), trash.TrashPath)
	if !strings.HasPrefix(filepath.Base(trash.TrashPath), "7-") {
		t.Fatalf("trash basename = %q, want instance id prefix", filepath.Base(trash.TrashPath))
	}

	if err := fs.Restore(ctx, trash); err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}
	assertDirExists(t, dir)
	assertMissing(t, trash.TrashPath)
	assertFileContent(t, marker, "secret")
}

func TestFilesystemResetStagesExistingDirectoryAndRecreatesEmptyDirectory(t *testing.T) {
	ctx := context.Background()
	fs := newTestFilesystem(t)
	dir, err := fs.Ensure(ctx, 9)
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old.db"), []byte("old"), 0o600); err != nil {
		t.Fatalf("write old file: %v", err)
	}

	if err := fs.Reset(ctx, 9); err != nil {
		t.Fatalf("Reset returned error: %v", err)
	}
	assertDirExists(t, dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir reset dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("reset dir entries = %d, want empty", len(entries))
	}
}

func TestFilesystemResetPurgeFailureRestoresOldDirectoryWithoutCreatingVisibleNewDirectory(t *testing.T) {
	ctx := context.Background()
	fs := newTestFilesystem(t)
	dir, err := fs.Ensure(ctx, 10)
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	marker := filepath.Join(dir, "old.db")
	if err := os.WriteFile(marker, []byte("old"), 0o600); err != nil {
		t.Fatalf("write old file: %v", err)
	}

	injected := errors.New("injected purge failure")
	fs.removeAll = func(path string) error {
		if strings.Contains(path, string(os.PathSeparator)+".trash"+string(os.PathSeparator)) {
			return injected
		}
		return os.RemoveAll(path)
	}

	if err := fs.Reset(ctx, 10); !errors.Is(err, injected) {
		t.Fatalf("Reset error = %v, want injected", err)
	}
	assertDirExists(t, dir)
	assertFileContent(t, marker, "old")
}

func TestFilesystemMissingDirectoryBehavior(t *testing.T) {
	ctx := context.Background()
	fs := newTestFilesystem(t)

	trash, err := fs.StageDelete(ctx, 404)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("StageDelete missing error = %v, want ErrNotFound", err)
	}
	if trash != (Trash{}) {
		t.Fatalf("StageDelete missing trash = %#v, want zero", trash)
	}

	if err := fs.Reset(ctx, 404); err != nil {
		t.Fatalf("Reset missing returned error: %v", err)
	}
	dir, err := fs.InstanceDir(404)
	if err != nil {
		t.Fatalf("InstanceDir returned error: %v", err)
	}
	assertDirExists(t, dir)
}

func TestFilesystemRejectsPathsOutsideDataDir(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	fs, err := NewFilesystem(dataDir)
	if err != nil {
		t.Fatalf("NewFilesystem returned error: %v", err)
	}

	outside := filepath.Join(filepath.Dir(dataDir), "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	absoluteOutside := filepath.Join(outside, "trash")
	invalidOriginal := filepath.Join(dataDir, "instances", "..", "..", filepath.Base(outside), "original")
	invalidTrash := filepath.Join(dataDir, ".trash", "..", "..", filepath.Base(outside), "trash")

	if err := fs.Restore(ctx, Trash{InstanceID: 1, OriginalPath: invalidOriginal, TrashPath: absoluteOutside}); err == nil {
		t.Fatalf("Restore accepted invalid absolute trash path outside data dir")
	}
	if err := fs.Restore(ctx, Trash{InstanceID: 1, OriginalPath: invalidOriginal, TrashPath: invalidTrash}); err == nil {
		t.Fatalf("Restore accepted traversal paths outside data dir")
	}
	if err := fs.Purge(ctx, Trash{InstanceID: 1, TrashPath: absoluteOutside}); err == nil {
		t.Fatalf("Purge accepted invalid absolute trash path outside data dir")
	}
}

func TestFilesystemRestoreRejectsOriginalPathForDifferentInstance(t *testing.T) {
	ctx := context.Background()
	fs := newTestFilesystem(t)
	dir, err := fs.Ensure(ctx, 21)
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state"), []byte("ok"), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	trash, err := fs.StageDelete(ctx, 21)
	if err != nil {
		t.Fatalf("StageDelete returned error: %v", err)
	}
	wrongOriginal, err := fs.InstanceDir(22)
	if err != nil {
		t.Fatalf("InstanceDir returned error: %v", err)
	}
	trash.OriginalPath = wrongOriginal

	if err := fs.Restore(ctx, trash); err == nil {
		t.Fatalf("Restore accepted OriginalPath for a different instance")
	}
	assertMissing(t, wrongOriginal)
	assertDirExists(t, trash.TrashPath)
}

func TestFilesystemCleansTrashDirectoryAfterInjectedStageDeleteFailure(t *testing.T) {
	ctx := context.Background()
	fs := newTestFilesystem(t)
	dir, err := fs.Ensure(ctx, 12)
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	injected := errors.New("injected rename failure")
	fs.rename = func(oldpath string, newpath string) error {
		return injected
	}

	trash, err := fs.StageDelete(ctx, 12)
	if !errors.Is(err, injected) {
		t.Fatalf("StageDelete error = %v, want injected", err)
	}
	if trash != (Trash{}) {
		t.Fatalf("StageDelete trash = %#v, want zero", trash)
	}
	assertDirExists(t, dir)
	assertTrashEmpty(t, fs.dataDir)
}

func TestFilesystemRestoreRollsBackWhenRecreateTrashParentFails(t *testing.T) {
	ctx := context.Background()
	fs := newTestFilesystem(t)
	dir, err := fs.Ensure(ctx, 13)
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state"), []byte("ok"), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	trash, err := fs.StageDelete(ctx, 13)
	if err != nil {
		t.Fatalf("StageDelete returned error: %v", err)
	}
	injected := errors.New("injected mkdir failure")
	fs.mkdirAll = func(path string, perm os.FileMode) error {
		if path == filepath.Join(fs.dataDir, ".trash") {
			return injected
		}
		return os.MkdirAll(path, perm)
	}

	if err := fs.Restore(ctx, trash); !errors.Is(err, injected) {
		t.Fatalf("Restore error = %v, want injected", err)
	}
	assertMissing(t, dir)
	assertDirExists(t, trash.TrashPath)
	assertFileContent(t, filepath.Join(trash.TrashPath, "state"), "ok")
}

func newTestFilesystem(t *testing.T) *Filesystem {
	t.Helper()
	fs, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatalf("NewFilesystem returned error: %v", err)
	}
	return fs
}

func assertDirExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%q is not a directory", path)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat %q error = %v, want not exists", path, err)
	}
}

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("read %q = %q, want %q", path, got, want)
	}
}

func assertUnderDir(t *testing.T, base string, path string) {
	t.Helper()
	rel, err := filepath.Rel(base, path)
	if err != nil {
		t.Fatalf("Rel(%q, %q): %v", base, path, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		t.Fatalf("path %q is not under %q", path, base)
	}
}

func assertTrashEmpty(t *testing.T, dataDir string) {
	t.Helper()
	trashDir := filepath.Join(dataDir, ".trash")
	entries, err := os.ReadDir(trashDir)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatalf("ReadDir trash: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("trash entries = %d, want empty", len(entries))
	}
}
