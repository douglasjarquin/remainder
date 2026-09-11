package cache_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
)

func TestStore_EmptyCacheRetryBackoff_coalescesFailedRefreshes(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	clock := now
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return clock }})
	binding := testBinding()
	fetches := 0
	retryAt := now.Add(30 * time.Second)
	fetch := func(context.Context) cache.FetchResult {
		fetches++
		return cache.FetchResult{Failure: cache.FailureTransient, RetryAt: retryAt, Err: errors.New("rate limited")}
	}
	policy := cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Minute}

	// When
	_, firstErr := store.Resolve(t.Context(), binding, "", policy, fetch)
	_, secondErr := store.Resolve(t.Context(), binding, "", policy, fetch)

	// Then
	backoff, ok := errors.AsType[*cache.BackoffError](secondErr)
	if firstErr == nil || !ok || !backoff.RetryAt.Equal(retryAt) || fetches != 1 {
		t.Fatalf("first error = %v, second error = %v, retry at = %s, fetches = %d", firstErr, secondErr, backoff.RetryAt, fetches)
	}
	clock = retryAt
	if _, err := store.Resolve(t.Context(), binding, "", policy, fetch); err == nil || fetches != 2 {
		t.Fatalf("post-backoff error = %v, fetches = %d", err, fetches)
	}
}

func TestStore_RetryBackoff_preservesLastValidObservation(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	observedAt := now.Add(-time.Hour)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
	binding := testBinding()
	if _, err := store.Put(t.Context(), binding, testObservation(observedAt)); err != nil {
		t.Fatal(err)
	}
	fetches := 0
	fetch := func(context.Context) cache.FetchResult {
		fetches++
		return cache.FetchResult{Failure: cache.FailureTransient, RetryAt: now.Add(30 * time.Second), Err: errors.New("rate limited")}
	}
	policy := cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Second, StaleOnError: true}

	// When
	first, firstErr := store.Resolve(t.Context(), binding, "acct-test", policy, fetch)
	second, secondErr := store.Resolve(t.Context(), binding, "acct-test", policy, fetch)

	// Then
	if firstErr != nil || secondErr != nil || fetches != 1 || !first.Observation.ObservedAt.Equal(observedAt) || !second.Observation.ObservedAt.Equal(observedAt) || first.Observation.Account.LastObserved != "acct-test" || second.Observation.Account.LastObserved != "acct-test" {
		t.Fatalf("first = %+v/%v, second = %+v/%v, fetches = %d", first, firstErr, second, secondErr, fetches)
	}
}

func TestStore_RetryBackoff_isBoundedLocally(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }, MaxBackoff: time.Minute})
	binding := testBinding()
	policy := cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Minute}

	// When
	_, _ = store.Resolve(t.Context(), binding, "", policy, func(context.Context) cache.FetchResult {
		return cache.FetchResult{Failure: cache.FailureTransient, RetryAt: now.Add(24 * time.Hour), Err: errors.New("rate limited")}
	})
	_, err := store.Resolve(t.Context(), binding, "", policy, func(context.Context) cache.FetchResult {
		t.Fatal("fetch called during bounded backoff")
		return cache.FetchResult{}
	})

	// Then
	backoff, ok := errors.AsType[*cache.BackoffError](err)
	if !ok || !backoff.RetryAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("error = %v, want retry at %s", err, now.Add(time.Minute))
	}
}

func TestStore_TransientFailureWithoutDeadline_usesLocalBackoff(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }, LocalBackoff: 2 * time.Second})
	binding := testBinding()
	policy := cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Minute}

	// When
	_, _ = store.Resolve(t.Context(), binding, "", policy, func(context.Context) cache.FetchResult {
		return cache.FetchResult{Failure: cache.FailureTransient, Err: errors.New("temporary failure")}
	})
	_, err := store.Resolve(t.Context(), binding, "", policy, func(context.Context) cache.FetchResult {
		t.Fatal("fetch called during local backoff")
		return cache.FetchResult{}
	})

	// Then
	backoff, ok := errors.AsType[*cache.BackoffError](err)
	if !ok || !backoff.RetryAt.Equal(now.Add(2*time.Second)) {
		t.Fatalf("error = %v, want retry at %s", err, now.Add(2*time.Second))
	}
}
