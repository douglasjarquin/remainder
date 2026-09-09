package cache_test

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestStore_RevocationRemainsStickyUntilSuccessfulRefresh(t *testing.T) {
	invalidations := []cache.FailureKind{cache.FailureRevoked, cache.FailureAccountMismatch}
	followUps := []cache.FailureKind{cache.FailureTransient, cache.FailurePermanent}
	for _, invalidation := range invalidations {
		for _, followUp := range followUps {
			t.Run(string(invalidation)+"_then_"+string(followUp), func(t *testing.T) {
				// Given
				now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
				clock := now
				originalObservedAt := now.Add(-time.Minute)
				binding := testBinding()
				store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return clock }})
				if _, err := store.Put(t.Context(), binding, testObservation(originalObservedAt)); err != nil {
					t.Fatal(err)
				}
				policy := cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Second, StaleOnError: true}
				firstErr := errors.New("confirmed source invalidation")
				if _, err := store.Resolve(t.Context(), binding, "", policy, func(context.Context) cache.FetchResult {
					return cache.FetchResult{Failure: invalidation, Err: firstErr}
				}); !errors.Is(err, firstErr) {
					t.Fatalf("invalidation error = %v, want %v", err, firstErr)
				}

				// When
				followUpErr := errors.New("later refresh failure")
				_, err := store.Resolve(t.Context(), binding, "", policy, func(context.Context) cache.FetchResult {
					return cache.FetchResult{Failure: followUp, Err: followUpErr}
				})

				// Then
				if !errors.Is(err, followUpErr) {
					t.Fatalf("follow-up error = %v, want %v", err, followUpErr)
				}
				assertPersistedObservationTime(t, store.SnapshotPath(binding), originalObservedAt)
				if _, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeOnly, MaxAge: time.Hour}, func(context.Context) cache.FetchResult {
					t.Fatal("cached-only fetch called")
					return cache.FetchResult{}
				}); !errors.Is(err, cache.ErrUnavailable) {
					t.Fatalf("cached-only error = %v, want unavailable", err)
				}

				if followUp == cache.FailureTransient {
					clock = now.Add(time.Second)
				}
				recoveredAt := clock
				recovered, err := store.Resolve(t.Context(), binding, "", policy, func(context.Context) cache.FetchResult {
					return cache.FetchResult{Observation: testObservation(recoveredAt)}
				})
				if err != nil || !recovered.Observation.ObservedAt.Equal(recoveredAt) {
					t.Fatalf("recovery = %+v, error = %v", recovered, err)
				}
				cached, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeOnly, MaxAge: time.Hour}, func(context.Context) cache.FetchResult {
					t.Fatal("recovered cached-only fetch called")
					return cache.FetchResult{}
				})
				if err != nil || !cached.FromCache || !cached.Observation.ObservedAt.Equal(recoveredAt) {
					t.Fatalf("recovered cache = %+v, error = %v", cached, err)
				}
			})
		}
	}
}

func assertPersistedObservationTime(t *testing.T, path string, want time.Time) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Observation evidence.Observation `json:"observation"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if !snapshot.Observation.ObservedAt.Equal(want) {
		t.Fatalf("persisted observed_at = %s, want %s", snapshot.Observation.ObservedAt, want)
	}
}
