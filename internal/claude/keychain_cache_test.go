package claude

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestAdapterMacKeychain_consentedFetchThenUnconsentedCacheOnlyReadSucceeds(t *testing.T) {
	// Mirrors how the CLI actually derives allowKeychainPrompt: a fresh
	// Adapter per invocation, authorized only when that invocation's own
	// --allow-keychain-prompt flag was passed. A later --cache=only call
	// that omits the flag must still read what an earlier consented call
	// already cached, without needing consent again or touching the helper.
	now := fixedNow()
	var keychainCalls, requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if strings.HasSuffix(r.URL.Path, "/profile") {
			_, _ = w.Write([]byte(`{"account":{"uuid":"account-1"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":0}}`))
	}))
	defer server.Close()
	missing := filepath.Join(t.TempDir(), "missing", ".credentials.json")
	newProvider := func() Adapter {
		return New(Options{
			AuthFile:        missing,
			ProfileEndpoint: server.URL + "/profile",
			UsageEndpoint:   server.URL + "/usage",
			KeychainReader: func(context.Context) ([]byte, error) {
				keychainCalls.Add(1)
				return []byte(`{"claudeAiOauth":{"accessToken":"synthetic-access","expiresAt":1788973200000}}`), nil
			},
			Client: server.Client(),
			Now:    func() time.Time { return now },
			goos:   "darwin",
		})
	}
	store := cache.New(filepath.Join(t.TempDir(), "cache"), cache.Options{Now: func() time.Time { return now }})
	request := evidence.Request{Provider: "claude", Profile: "default"}

	consented := newProvider().WithKeychainPrompt()
	binding, err := consented.CacheBinding(t.Context(), request)
	if err != nil {
		t.Fatalf("first CacheBinding() error = %v", err)
	}
	fresh, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeAuto, MaxAge: 5 * time.Second}, func(fetchCtx context.Context) cache.FetchResult {
		observation, fetchErr := consented.Observe(fetchCtx, request)
		return cache.FetchResult{Observation: observation, Err: fetchErr}
	})
	if err != nil || keychainCalls.Load() != 1 || requests.Load() != 2 || fresh.Observation.Source != keychainSourceIdentity {
		t.Fatalf("fresh Resolve() error=%v keychainCalls=%d requests=%d source=%+v", err, keychainCalls.Load(), requests.Load(), fresh.Observation.Source)
	}

	unconsented := newProvider()
	binding, err = unconsented.CacheBinding(t.Context(), request)
	if err != nil {
		t.Fatalf("second CacheBinding() error = %v (must not require consent to compute an identity)", err)
	}
	cached, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeOnly, MaxAge: time.Minute}, func(context.Context) cache.FetchResult {
		t.Fatal("fetch must not run on a cache hit")
		return cache.FetchResult{}
	})
	if err != nil || keychainCalls.Load() != 1 || requests.Load() != 2 || cached.Observation.Source != keychainSourceIdentity {
		t.Fatalf("cached Resolve() error=%v keychainCalls=%d requests=%d source=%+v", err, keychainCalls.Load(), requests.Load(), cached.Observation.Source)
	}
}

func TestAdapterMacKeychain_deadlineDuringRefreshServesRetainedObservationStale(t *testing.T) {
	now := fixedNow()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/profile") {
			_, _ = w.Write([]byte(`{"account":{"uuid":"account-1"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":0}}`))
	}))
	defer server.Close()
	var blockKeychain atomic.Bool
	provider := New(Options{
		AuthFile:        filepath.Join(t.TempDir(), "missing", ".credentials.json"),
		ProfileEndpoint: server.URL + "/profile",
		UsageEndpoint:   server.URL + "/usage",
		KeychainReader: func(ctx context.Context) ([]byte, error) {
			if blockKeychain.Load() {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return []byte(`{"claudeAiOauth":{"accessToken":"synthetic-access","expiresAt":1788973200000}}`), nil
		},
		Client:  server.Client(),
		Timeout: 20 * time.Millisecond,
		Now:     func() time.Time { return now },
		goos:    "darwin",
	}).WithKeychainPrompt()
	store := cache.New(filepath.Join(t.TempDir(), "cache"), cache.Options{Now: func() time.Time { return now }})
	request := evidence.Request{Provider: "claude", Profile: "default"}
	fetch := func(ctx context.Context) cache.FetchResult {
		observation, err := provider.Observe(ctx, request)
		return cache.FetchResult{Observation: observation, Err: err}
	}
	binding, err := provider.CacheBinding(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Minute}, fetch); err != nil {
		t.Fatalf("seed Resolve() error = %v", err)
	}
	blockKeychain.Store(true)

	result, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeAuto, MaxAge: time.Minute, Refresh: true, StaleOnError: true}, fetch)

	if err != nil || !result.FromCache || result.Observation.Freshness != evidence.FreshStale || !result.Observation.ObservedAt.Equal(now) {
		t.Fatalf("Resolve() = %+v, %v; want the retained observation marked stale", result, err)
	}
}
