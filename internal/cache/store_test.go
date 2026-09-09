package cache_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestStore_EligibleHit(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
	binding := testBinding()
	want := testObservation(now.Add(-time.Second))
	if _, err := store.Put(t.Context(), binding, want); err != nil {
		t.Fatal(err)
	}
	fetchCalls := 0

	// When
	result, err := store.Resolve(t.Context(), binding, "acct-test", cache.Policy{Mode: cache.ModeAuto, MaxAge: 5 * time.Second}, func(context.Context) cache.FetchResult {
		fetchCalls++
		return cache.FetchResult{Err: errors.New("fetch must not run")}
	})
	// Then
	if err != nil {
		t.Fatal(err)
	}
	if !result.FromCache || fetchCalls != 0 {
		t.Fatalf("result = %+v, fetch calls = %d", result, fetchCalls)
	}
	if !result.Observation.ObservedAt.Equal(want.ObservedAt) {
		t.Fatalf("observed_at = %s, want %s", result.Observation.ObservedAt, want.ObservedAt)
	}
	if result.Observation.Account.Binding != evidence.IdentityHistorical {
		t.Fatalf("identity = %q, want historical", result.Observation.Account.Binding)
	}
}

func TestStore_CachedOnly(t *testing.T) {
	// Given
	root := filepath.Join(t.TempDir(), "remainder", "v1")
	store := cache.New(root, cache.Options{})
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
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cached-only miss created storage: %v", err)
	}
}

func TestStore_Put_roundTripsCompleteObservation(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
	binding := testBinding()
	observation := testObservation(now)
	amount := evidence.JSONNumber("80")
	observation.Windows = []evidence.Window{{ID: "weekly", Scope: evidence.ScopeAccount, Unit: "percent", Limits: []evidence.Limit{{ID: "weekly_remaining", Field: "remaining", Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}}}}

	// When
	_, err := store.Put(t.Context(), binding, observation)
	// Then
	if err != nil {
		t.Fatal(err)
	}
}

func TestStore_CachedOnly_refusesChangedAccountAndSourceBinding(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
	binding := testBinding()
	if _, err := store.Put(t.Context(), binding, testObservation(now)); err != nil {
		t.Fatal(err)
	}
	changedBinding := binding
	changedBinding.CredentialFingerprint = "changed"

	for name, request := range map[string]struct {
		binding cache.Binding
		account string
	}{
		"account": {binding: binding, account: "acct-other"},
		"source":  {binding: changedBinding},
	} {
		t.Run(name, func(t *testing.T) {
			// When
			_, err := store.Resolve(t.Context(), request.binding, request.account, cache.Policy{Mode: cache.ModeOnly, MaxAge: time.Minute}, func(context.Context) cache.FetchResult {
				t.Fatal("fetch called")
				return cache.FetchResult{}
			})

			// Then
			if !errors.Is(err, cache.ErrUnavailable) {
				t.Fatalf("error = %v, want unavailable", err)
			}
		})
	}
}

func testBinding() cache.Binding {
	return cache.Binding{Provider: "codex", Profile: "default", ResponseBoundary: "usage", SourceKind: "native_file_http", SourceName: "codex_auth_json", CredentialFingerprint: "fingerprint"}
}

func testObservation(observedAt time.Time) evidence.Observation {
	return evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "default",
		Account:       evidence.AccountIdentity{LastObserved: "acct-test", Binding: evidence.IdentityVerified},
		Source:        evidence.SourceIdentity{Kind: "native_file_http", Name: "codex_auth_json"},
		ObservedAt:    observedAt,
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
	}
}
