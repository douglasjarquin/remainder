package cache_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestStore_Resolve_preservesOriginalObservationAgeWhenPresentationCacheReusesResult(t *testing.T) {
	// Given
	cacheReadAt := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	originalObservedAt := cacheReadAt.Add(-9 * time.Minute)
	now := cacheReadAt
	store := cache.New(t.TempDir(), cache.Options{Now: func() time.Time { return now }})
	binding := testBinding()
	if _, err := store.Put(t.Context(), binding, testObservation(originalObservedAt)); err != nil {
		t.Fatal(err)
	}
	maxAge := 10 * time.Minute

	// When
	resolved, err := store.Resolve(t.Context(), binding, "acct-test", cache.Policy{Mode: cache.ModeAuto, MaxAge: maxAge}, func(context.Context) cache.FetchResult {
		t.Fatal("fetch called for eligible source cache record")
		return cache.FetchResult{}
	})
	if err != nil {
		t.Fatal(err)
	}
	presentationCache := resolved.Observation
	now = cacheReadAt.Add(2 * time.Minute)
	presentation, err := evidence.RenderCompact(presentationCache, now)
	if err != nil {
		t.Fatal(err)
	}

	// Then
	if !resolved.FromCache {
		t.Fatal("source cache result was not marked as cached")
	}
	if !presentationCache.ObservedAt.Equal(originalObservedAt) {
		t.Fatalf("presentation observed_at = %s, want original source observation %s", presentationCache.ObservedAt, originalObservedAt)
	}
	if age := now.Sub(presentationCache.ObservedAt); age <= maxAge {
		t.Fatalf("presentation age = %s, want older than source max age %s without a second TTL", age, maxAge)
	}
	if !strings.Contains(presentation, "age_seconds=660") {
		t.Fatalf("presentation = %q, want age from the original observation", presentation)
	}
}
