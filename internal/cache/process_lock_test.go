package cache_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
)

func TestStore_CachedOnly_doesNotWaitForHeldRefreshLock(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }, LockWait: time.Second})
	binding := testBinding()
	if _, err := store.Put(t.Context(), binding, testObservation(now)); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(store.SnapshotPath(binding)), "refresh.lock"), os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	// When
	result, err := store.Resolve(ctx, binding, "", cache.Policy{Mode: cache.ModeOnly, MaxAge: time.Second}, func(context.Context) cache.FetchResult {
		t.Fatal("fetch called")
		return cache.FetchResult{}
	})

	// Then
	if err != nil || !result.FromCache {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func TestStore_KilledWriter_releasesKernelLock(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }, LockWait: time.Second})
	binding := testBinding()
	if _, err := store.Put(t.Context(), binding, testObservation(now.Add(-time.Second))); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(filepath.Dir(store.SnapshotPath(binding)), "refresh.lock")
	command := exec.Command(os.Args[0], "-test.run=^TestCacheLockOwnerHelper$")
	command.Env = []string{"REMAINDER_CACHE_LOCK_OWNER=1", "REMAINDER_CACHE_LOCK_PATH=" + lockPath}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	ready, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || ready != "ready\n" {
		t.Fatalf("helper readiness = %q, error = %v", ready, err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("killed helper exited successfully")
	}

	// When
	result, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Minute, Refresh: true}, func(context.Context) cache.FetchResult {
		return cache.FetchResult{Observation: testObservation(now)}
	})

	// Then
	if err != nil || result.Generation == "" || !result.Observation.ObservedAt.Equal(now) {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("stable lock path missing: %v", err)
	}
}

func TestStore_LockDeadline_boundsRefreshWithoutFetching(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }, LockWait: 20 * time.Millisecond})
	binding := testBinding()
	if _, err := store.Put(t.Context(), binding, testObservation(now.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(store.SnapshotPath(binding)), "refresh.lock"), os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	fetches := 0

	// When
	_, err = store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Second}, func(context.Context) cache.FetchResult {
		fetches++
		return cache.FetchResult{}
	})

	// Then
	if !errors.Is(err, cache.ErrLockTimeout) || fetches != 0 {
		t.Fatalf("error = %v, fetches = %d", err, fetches)
	}
}

func TestStore_CanceledWaiter_releasesOnlyItsResources(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }, LockWait: time.Second})
	binding := testBinding()
	if _, err := store.Put(t.Context(), binding, testObservation(now.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(filepath.Dir(store.SnapshotPath(binding)), "refresh.lock")
	lock, err := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	fetches := 0

	// When
	_, err = store.Resolve(ctx, binding, "", cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Second}, func(context.Context) cache.FetchResult {
		fetches++
		return cache.FetchResult{}
	})
	if unlockErr := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); unlockErr != nil {
		t.Fatal(unlockErr)
	}
	if closeErr := lock.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	result, retryErr := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Second}, func(context.Context) cache.FetchResult {
		fetches++
		return cache.FetchResult{Observation: testObservation(now)}
	})

	// Then
	if !errors.Is(err, context.Canceled) || retryErr != nil || result.Generation == "" || fetches != 1 {
		t.Fatalf("canceled error = %v, retry = %+v/%v, fetches = %d", err, result, retryErr, fetches)
	}
}

func TestCacheLockOwnerHelper(t *testing.T) {
	if os.Getenv("REMAINDER_CACHE_LOCK_OWNER") != "1" {
		return
	}
	file, err := os.OpenFile(os.Getenv("REMAINDER_CACHE_LOCK_PATH"), os.O_RDWR, 0o600)
	if err != nil {
		os.Exit(2)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		os.Exit(3)
	}
	fmt.Println("ready")
	select {}
}
