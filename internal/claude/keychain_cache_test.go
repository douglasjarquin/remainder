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

func TestAdapterMacKeychain_throughCacheStoreResolve_fetchesOnceThenReusesCache(t *testing.T) {
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
	provider := New(Options{
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
	}).WithKeychainPrompt()
	store := cache.New(filepath.Join(t.TempDir(), "cache"), cache.Options{Now: func() time.Time { return now }})
	request := evidence.Request{Provider: "claude", Profile: "default"}
	fetch := func(fetchCtx context.Context) cache.FetchResult {
		observation, fetchErr := provider.Observe(fetchCtx, request)
		failure, retryAt := provider.Failure(fetchErr)
		return cache.FetchResult{Observation: observation, Failure: failure, RetryAt: retryAt, Err: fetchErr}
	}

	binding, err := provider.CacheBinding(t.Context(), request)
	if err != nil {
		t.Fatalf("CacheBinding() error = %v", err)
	}
	fresh, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeAuto, MaxAge: 5 * time.Second}, fetch)
	if err != nil || keychainCalls.Load() != 1 || requests.Load() != 2 || fresh.Observation.Source != keychainSourceIdentity {
		t.Fatalf("fresh Resolve() error=%v keychainCalls=%d requests=%d source=%+v", err, keychainCalls.Load(), requests.Load(), fresh.Observation.Source)
	}

	binding, err = provider.CacheBinding(t.Context(), request)
	if err != nil {
		t.Fatalf("second CacheBinding() error = %v", err)
	}
	cached, err := store.Resolve(t.Context(), binding, "", cache.Policy{Mode: cache.ModeOnly, MaxAge: time.Minute}, func(context.Context) cache.FetchResult {
		t.Fatal("fetch must not run on a cache hit")
		return cache.FetchResult{}
	})
	if err != nil || keychainCalls.Load() != 1 || requests.Load() != 2 || cached.Observation.Source != keychainSourceIdentity {
		t.Fatalf("cached Resolve() error=%v keychainCalls=%d requests=%d source=%+v", err, keychainCalls.Load(), requests.Load(), cached.Observation.Source)
	}
}
