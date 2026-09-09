package ownership

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireCreatesDirectoryAndPreventsSecondAcquisition(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "missing", "data")
	lock, err := Acquire(dataDir)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, ".gowa-manager.lock")); err != nil {
		t.Fatalf("lock file stat error = %v", err)
	}

	second, err := Acquire(dataDir)
	if err == nil {
		second.Release()
		t.Fatal("second Acquire() error = nil")
	}
	if !errors.Is(err, ErrAlreadyLocked) {
		t.Fatalf("second Acquire() error = %v, want ErrAlreadyLocked", err)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	reacquired, err := Acquire(dataDir)
	if err != nil {
		t.Fatalf("reacquire error = %v", err)
	}
	if err := reacquired.Release(); err != nil {
		t.Fatalf("reacquired Release() error = %v", err)
	}
	if err := reacquired.Release(); err != nil {
		t.Fatalf("second Release() error = %v", err)
	}
}

func TestAcquireContentionAcrossProcesses(t *testing.T) {
	if os.Getenv("GOWA_LOCK_HELPER") == "1" {
		lock, err := Acquire(os.Getenv("GOWA_LOCK_DATA_DIR"))
		if err != nil {
			os.Exit(2)
		}
		defer lock.Release()
		time.Sleep(2 * time.Second)
		return
	}

	dataDir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run", "^TestAcquireContentionAcrossProcesses$")
	cmd.Env = append(os.Environ(), "GOWA_LOCK_HELPER=1", "GOWA_LOCK_DATA_DIR="+dataDir)
	if err := cmd.Start(); err != nil {
		t.Fatalf("helper start error = %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	time.Sleep(300 * time.Millisecond)

	lock, err := Acquire(dataDir)
	if err == nil {
		lock.Release()
		t.Fatal("Acquire() while helper holds lock error = nil")
	}
	if !errors.Is(err, ErrAlreadyLocked) {
		t.Fatalf("Acquire() error = %v, want ErrAlreadyLocked", err)
	}
}

func TestAcquireContextRetriesAndAcquiresWhenReleased(t *testing.T) {
	dataDir := t.TempDir()
	firstLock, err := Acquire(dataDir)
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}

	// Release lock after 300ms
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = firstLock.Release()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	secondLock, err := AcquireContext(ctx, dataDir)
	if err != nil {
		t.Fatalf("AcquireContext() error = %v", err)
	}
	defer secondLock.Release()

	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Fatalf("AcquireContext() returned too quickly: %v", elapsed)
	}
}

func TestAcquireContextTimeout(t *testing.T) {
	dataDir := t.TempDir()
	firstLock, err := Acquire(dataDir)
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	defer firstLock.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	_, err = AcquireContext(ctx, dataDir)
	if err == nil {
		t.Fatal("AcquireContext() error = nil, want timeout")
	}
	if !errors.Is(err, ErrAlreadyLocked) {
		t.Fatalf("AcquireContext() error = %v, want ErrAlreadyLocked", err)
	}
}

func TestAcquireContextCanceled(t *testing.T) {
	dataDir := t.TempDir()
	firstLock, err := Acquire(dataDir)
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	defer firstLock.Release()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err = AcquireContext(ctx, dataDir)
	if err == nil {
		t.Fatal("AcquireContext() error = nil, want canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AcquireContext() error = %v, want context.Canceled", err)
	}
}
