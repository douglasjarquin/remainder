package cache_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
)

func TestRootRevokedSourceRespectsBackoff(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(t.TempDir(), cache.Options{Now: func() time.Time { return now }})
	binding := testBinding()
	if _, err := store.Put(t.Context(), binding, testObservation(now.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	policy := cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Second, StaleOnError: true}
	failure := errors.New("synthetic rejected")
	store.Resolve(t.Context(), binding, "", policy, func(context.Context) cache.FetchResult {
		return cache.FetchResult{Failure: cache.FailureRevoked, Err: failure}
	})
	store.Resolve(t.Context(), binding, "", policy, func(context.Context) cache.FetchResult {
		return cache.FetchResult{Failure: cache.FailureTransient, RetryAt: now.Add(30 * time.Second), Err: failure}
	})
	calls := 0
	for range 12 {
		result, err := store.Resolve(t.Context(), binding, "", policy, func(context.Context) cache.FetchResult {
			calls++
			return cache.FetchResult{Failure: cache.FailureTransient, RetryAt: now.Add(30 * time.Second), Err: failure}
		})
		if err == nil || result.FromCache {
			t.Fatal("revoked evidence reused")
		}
	}
	if calls != 0 {
		t.Fatalf("recorded backoff allowed %d additional refreshes; want zero", calls)
	}
}

func TestStore_RevokedBackoff_blocksRefreshUntilDeadline(t *testing.T) {
	invalidations := []cache.FailureKind{cache.FailureRevoked, cache.FailureAccountMismatch}
	for _, invalidation := range invalidations {
		for _, staleOnError := range []bool{false, true} {
			t.Run(string(invalidation)+"_stale_"+strconv.FormatBool(staleOnError), func(t *testing.T) {
				// Given
				now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
				clock := now
				retryAt := now.Add(30 * time.Second)
				store := cache.New(t.TempDir(), cache.Options{Now: func() time.Time { return clock }})
				binding := testBinding()
				if _, err := store.Put(t.Context(), binding, testObservation(now.Add(-time.Minute))); err != nil {
					t.Fatal(err)
				}
				policy := cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Second, StaleOnError: staleOnError}
				invalidationErr := errors.New("confirmed source invalidation")
				if _, err := store.Resolve(t.Context(), binding, "", policy, func(context.Context) cache.FetchResult {
					return cache.FetchResult{Failure: invalidation, Err: invalidationErr}
				}); !errors.Is(err, invalidationErr) {
					t.Fatalf("invalidation error = %v, want %v", err, invalidationErr)
				}
				transientErr := errors.New("transient failure")
				result, err := store.Resolve(t.Context(), binding, "", policy, func(context.Context) cache.FetchResult {
					return cache.FetchResult{Failure: cache.FailureTransient, RetryAt: retryAt, Err: transientErr}
				})
				if !errors.Is(err, transientErr) || result.FromCache {
					t.Fatalf("recorded result = %+v, error = %v", result, err)
				}

				// When
				fetches := 0
				result, err = store.Resolve(t.Context(), binding, "", policy, func(context.Context) cache.FetchResult {
					fetches++
					return cache.FetchResult{}
				})

				// Then
				backoff, ok := errors.AsType[*cache.BackoffError](err)
				if !ok || !backoff.RetryAt.Equal(retryAt) || result.FromCache || fetches != 0 {
					t.Fatalf("result = %+v, error = %v, retry at = %v, fetches = %d", result, err, backoff, fetches)
				}
				if _, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeOnly, MaxAge: time.Hour}, func(context.Context) cache.FetchResult {
					t.Fatal("cached-only fetch called")
					return cache.FetchResult{}
				}); !errors.Is(err, cache.ErrUnavailable) {
					t.Fatalf("cached-only error = %v, want unavailable", err)
				}

				clock = retryAt
				recovered := testObservation(clock)
				result, err = store.Resolve(t.Context(), binding, "", policy, func(context.Context) cache.FetchResult {
					fetches++
					return cache.FetchResult{Observation: recovered}
				})
				if err != nil || result.FromCache || fetches != 1 || !result.Observation.ObservedAt.Equal(clock) {
					t.Fatalf("recovery = %+v, error = %v, fetches = %d", result, err, fetches)
				}
				cached, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeOnly, MaxAge: time.Hour}, func(context.Context) cache.FetchResult {
					t.Fatal("recovered cached-only fetch called")
					return cache.FetchResult{}
				})
				if err != nil || !cached.FromCache || !cached.Observation.ObservedAt.Equal(clock) {
					t.Fatalf("recovered cache = %+v, error = %v", cached, err)
				}
			})
		}
	}
}
