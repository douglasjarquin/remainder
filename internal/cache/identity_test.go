package cache_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestStore_UnknownIdentityRemainsUnknownOnReuse(t *testing.T) {
	for _, stale := range []bool{false, true} {
		name := "fresh"
		if stale {
			name = "stale"
		}
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
			store := cache.New(filepath.Join(t.TempDir(), "cache"), cache.Options{Now: func() time.Time { return now }})
			observation := testObservation(now.Add(-time.Second))
			observation.Account = evidence.AccountIdentity{Binding: evidence.IdentityUnknown}
			if stale {
				observation.ObservedAt = now.Add(-time.Hour)
			}
			if _, err := store.Put(t.Context(), testBinding(), observation); err != nil {
				t.Fatal(err)
			}
			fetches := 0
			fetch := func(context.Context) cache.FetchResult {
				fetches++
				return cache.FetchResult{Err: errors.New("temporary failure"), Failure: cache.FailureTransient}
			}
			policy := cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Minute, StaleOnError: stale}
			for range 2 {
				result, err := store.Resolve(t.Context(), testBinding(), "", policy, fetch)
				if err != nil || !result.FromCache || result.Observation.Account.Binding != evidence.IdentityUnknown || result.Observation.Account.LastObserved != "" || !result.Observation.ObservedAt.Equal(observation.ObservedAt) {
					t.Fatalf("cached unknown identity changed: result=%+v error=%v", result, err)
				}
			}
			wantFetches := 0
			if stale {
				wantFetches = 1
			}
			if fetches != wantFetches {
				t.Fatalf("fetches=%d want=%d", fetches, wantFetches)
			}
		})
	}
}
