package cache_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
)

func TestStore_ConcurrentWrites_keepNewestCompleteObservation(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
	binding := testBinding()
	observations := []time.Time{now.Add(-time.Second), now.Add(-3 * time.Second), now.Add(-2 * time.Second), now}
	var wait sync.WaitGroup
	errorsByWriter := make(chan error, len(observations))

	// When
	for _, observedAt := range observations {
		wait.Go(func() {
			_, err := store.Put(t.Context(), binding, testObservation(observedAt))
			if err != nil && !errors.Is(err, cache.ErrOlderObservation) {
				errorsByWriter <- err
			}
		})
	}
	wait.Wait()
	close(errorsByWriter)

	// Then
	for err := range errorsByWriter {
		t.Fatal(err)
	}
	result, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeOnly, MaxAge: time.Minute}, func(context.Context) cache.FetchResult {
		t.Fatal("fetch called")
		return cache.FetchResult{}
	})
	if err != nil || !result.Observation.ObservedAt.Equal(now) {
		t.Fatalf("observed_at = %s, error = %v", result.Observation.ObservedAt, err)
	}
}

func TestStore_ForcedOverlap_reusesOneNewGeneration(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	allInitialReads := make(chan struct{})
	var nowCalls atomic.Int64
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time {
		if nowCalls.Add(1) == 3 {
			close(allInitialReads)
		}
		return now
	}})
	binding := testBinding()
	if _, err := store.Put(t.Context(), binding, testObservation(now.Add(-time.Second))); err != nil {
		t.Fatal(err)
	}
	var fetches int
	var mutex sync.Mutex
	results := make(chan cache.Result, 2)
	errorsFound := make(chan error, 2)
	policy := cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Minute, Refresh: true}

	// When
	for range 2 {
		go func() {
			result, err := store.Resolve(t.Context(), binding, "", policy, func(context.Context) cache.FetchResult {
				mutex.Lock()
				fetches++
				mutex.Unlock()
				<-allInitialReads
				return cache.FetchResult{Observation: testObservation(now)}
			})
			results <- result
			errorsFound <- err
		}()
	}
	first, second := <-results, <-results

	// Then
	if err := <-errorsFound; err != nil {
		t.Fatal(err)
	}
	if err := <-errorsFound; err != nil {
		t.Fatal(err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if fetches != 1 || first.Generation == "" || first.Generation != second.Generation {
		t.Fatalf("fetches = %d, generations = %q/%q", fetches, first.Generation, second.Generation)
	}
}

func TestStore_LaterForcedRequest_requiresAnotherGeneration(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
	binding := testBinding()
	if _, err := store.Put(t.Context(), binding, testObservation(now.Add(-time.Second))); err != nil {
		t.Fatal(err)
	}
	fetches := 0
	policy := cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Minute, Refresh: true}
	fetch := func(context.Context) cache.FetchResult {
		fetches++
		return cache.FetchResult{Observation: testObservation(now.Add(time.Duration(fetches) * time.Second))}
	}

	// When
	first, firstErr := store.Resolve(t.Context(), binding, "", policy, fetch)
	second, secondErr := store.Resolve(t.Context(), binding, "", policy, fetch)

	// Then
	if firstErr != nil || secondErr != nil || fetches != 2 || first.Generation == second.Generation || !second.Observation.ObservedAt.After(first.Observation.ObservedAt) {
		t.Fatalf("first = %+v/%v, second = %+v/%v, fetches = %d", first, firstErr, second, secondErr, fetches)
	}
}

func TestStore_SeparateBindings_doNotShareRefreshOwnership(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
	firstBinding := testBinding()
	secondBinding := testBinding()
	secondBinding.CredentialFingerprint = "other-fingerprint"
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)

	// When
	go func() {
		_, err := store.Resolve(t.Context(), firstBinding, "", cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Minute}, func(context.Context) cache.FetchResult {
			close(firstStarted)
			<-releaseFirst
			return cache.FetchResult{Observation: testObservation(now)}
		})
		firstDone <- err
	}()
	<-firstStarted
	second, secondErr := store.Resolve(t.Context(), secondBinding, "acct-other", cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Minute}, func(context.Context) cache.FetchResult {
		observation := testObservation(now)
		observation.Account.LastObserved = "acct-other"
		return cache.FetchResult{Observation: observation}
	})
	close(releaseFirst)

	// Then
	if secondErr != nil || second.Generation == "" || second.Observation.Account.LastObserved != "acct-other" {
		t.Fatalf("second = %+v, error = %v", second, secondErr)
	}
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func TestStore_KilledWriterArtifact_doesNotExposePartialJSON(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
	binding := testBinding()
	if _, err := store.Put(t.Context(), binding, testObservation(now)); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(filepath.Dir(store.SnapshotPath(binding)), ".snapshot-killed-writer")
	if err := os.WriteFile(partial, []byte(`{"schema_version":`), 0o600); err != nil {
		t.Fatal(err)
	}

	// When
	result, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeOnly, MaxAge: time.Second}, func(context.Context) cache.FetchResult {
		t.Fatal("fetch called")
		return cache.FetchResult{}
	})

	// Then
	if err != nil || !result.Observation.ObservedAt.Equal(now) {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(store.SnapshotPath(binding)), "refresh.lock")); err != nil {
		t.Fatalf("stable lock missing: %v", err)
	}
}
