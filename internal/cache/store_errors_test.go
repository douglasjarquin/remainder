package cache_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestStore_Corrupt(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
	binding := testBinding()
	if _, err := store.Put(t.Context(), binding, testObservation(now.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.SnapshotPath(binding), []byte(`{"schema_version":`), 0o600); err != nil {
		t.Fatal(err)
	}

	// When
	result, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeAuto, MaxAge: 5 * time.Second}, func(context.Context) cache.FetchResult {
		return cache.FetchResult{Observation: testObservation(now)}
	})

	// Then
	if err != nil || result.FromCache || !result.Observation.ObservedAt.Equal(now) {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func TestStore_Revoked(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
	binding := testBinding()
	if _, err := store.Put(t.Context(), binding, testObservation(now.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("authorization rejected")

	// When
	_, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeAuto, MaxAge: 5 * time.Second, StaleOnError: true}, func(context.Context) cache.FetchResult {
		return cache.FetchResult{Failure: cache.FailureRevoked, Err: wantErr}
	})

	// Then
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want revocation", err)
	}
	_, err = store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeOnly, MaxAge: time.Hour}, func(context.Context) cache.FetchResult {
		t.Fatal("fetch called")
		return cache.FetchResult{}
	})
	if !errors.Is(err, cache.ErrUnavailable) {
		t.Fatalf("cached-only error = %v, want unavailable after revocation", err)
	}
}

func TestStore_WriteUnavailable(t *testing.T) {
	// Given
	root := filepath.Join(t.TempDir(), "cache")
	if err := os.WriteFile(root, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := cache.New(root, cache.Options{})
	fetchCalls := 0

	// When
	result, err := store.Resolve(t.Context(), testBinding(), "", cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Second}, func(context.Context) cache.FetchResult {
		fetchCalls++
		return cache.FetchResult{Observation: testObservation(time.Now().UTC())}
	})

	// Then
	if err != nil || fetchCalls != 1 || result.Warning != cache.WarningStorageUnavailable {
		t.Fatalf("result = %+v, error = %v, fetch calls = %d", result, err, fetchCalls)
	}
}

func TestStore_UnknownSchema(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
	binding := testBinding()
	if _, err := store.Put(t.Context(), binding, testObservation(now.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	unknown := []byte(`{"schema_version":"v2","future":"preserve"}`)
	if err := os.WriteFile(store.SnapshotPath(binding), unknown, 0o600); err != nil {
		t.Fatal(err)
	}

	// When
	result, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Second}, func(context.Context) cache.FetchResult {
		return cache.FetchResult{Observation: testObservation(now)}
	})

	// Then
	if err != nil || result.Warning != cache.WarningStorageUnavailable {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	after, readErr := os.ReadFile(store.SnapshotPath(binding))
	if readErr != nil || string(after) != string(unknown) {
		t.Fatalf("unknown record changed: %q, error = %v", after, readErr)
	}
}

func TestStore_TransientFailure_preservesOriginalObservationAge(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	observedAt := now.Add(-time.Minute)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
	if _, err := store.Put(t.Context(), testBinding(), testObservation(observedAt)); err != nil {
		t.Fatal(err)
	}

	// When
	result, err := store.Resolve(t.Context(), testBinding(), "", cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Second, StaleOnError: true}, func(context.Context) cache.FetchResult {
		return cache.FetchResult{Failure: cache.FailureTransient, Err: errors.New("temporary failure")}
	})

	// Then
	if err != nil || !result.Observation.ObservedAt.Equal(observedAt) || result.Observation.Freshness != evidence.FreshStale {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func TestStore_FutureAndExpiredSnapshots_neverBecomeFresh(t *testing.T) {
	for _, offset := range []time.Duration{-time.Minute, time.Minute} {
		t.Run(offset.String(), func(t *testing.T) {
			// Given
			now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
			store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
			if _, err := store.Put(t.Context(), testBinding(), testObservation(now.Add(offset))); err != nil {
				t.Fatal(err)
			}
			fetchCalls := 0

			// When
			_, err := store.Resolve(t.Context(), testBinding(), "", cache.Policy{Mode: cache.ModeOnly, MaxAge: 5 * time.Second}, func(context.Context) cache.FetchResult {
				fetchCalls++
				return cache.FetchResult{}
			})

			// Then
			if !errors.Is(err, cache.ErrUnavailable) || fetchCalls != 0 {
				t.Fatalf("error = %v, fetch calls = %d", err, fetchCalls)
			}
		})
	}
}

func TestStore_RecordOmitsSecretsAndRawAuthPath(t *testing.T) {
	// Given
	root := filepath.Join(t.TempDir(), "remainder", "v1")
	store := cache.New(root, cache.Options{})
	binding := testBinding()
	binding.CredentialFingerprint = "hashed-only"

	// When
	if _, err := store.Put(t.Context(), binding, testObservation(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.SnapshotPath(binding))
	// Then
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"synthetic-secret", "/Users/", "auth.json", "hashed-only"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("cache record contains forbidden value %q", forbidden)
		}
	}
	for path, wantMode := range map[string]os.FileMode{
		root: 0o700,
		filepath.Dir(store.SnapshotPath(binding)):                                0o700,
		store.SnapshotPath(binding):                                              0o600,
		filepath.Join(filepath.Dir(store.SnapshotPath(binding)), "refresh.lock"): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Mode().Perm() != wantMode {
			t.Fatalf("%s mode = %v, want %v", path, info.Mode().Perm(), wantMode)
		}
	}
}
