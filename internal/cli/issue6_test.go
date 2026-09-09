package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/codex"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestCacheFlags_rejectIncompatiblePoliciesBeforeSourceWork(t *testing.T) {
	// Given
	adapter := runtimeAdapter{codex: codex.New(codex.Options{AuthFile: filepath.Join(t.TempDir(), "missing")}), newStore: func() (*cache.Store, error) {
		t.Fatal("cache store initialized")
		return nil, nil
	}}
	tests := [][]string{
		{"--cache=only", "--refresh"},
		{"--cache=only", "--stale-on-error"},
		{"--cache=off", "--max-age=5s"},
		{"--cache=off", "--refresh"},
		{"--max-age=-1s"},
		{"--cache=invalid"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			// When
			var stdout, stderr bytes.Buffer
			code := executeWithAdapterAt(t.Context(), args, &stdout, &stderr, "test", time.Now().UTC(), adapter)

			// Then
			if code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestCacheFlags_rejectExplicitPolicyForAdapterWithoutCacheCapability(t *testing.T) {
	// Given
	adapter := &fixtureAdapter{observation: cacheObservation(time.Now().UTC())}

	// When
	var stdout, stderr bytes.Buffer
	code := executeWithAdapterAt(t.Context(), []string{"--cache=only"}, &stdout, &stderr, "test", time.Now().UTC(), adapter)

	// Then
	if code != 2 || adapter.calls != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "cache policy is unavailable") {
		t.Fatalf("code = %d, calls = %d, stdout = %q, stderr = %q", code, adapter.calls, stdout.String(), stderr.String())
	}
}

func TestStore_CachedOnly(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	authPath := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(authPath, []byte("malformed OAuth fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := codex.New(codex.Options{AuthFile: authPath, Endpoints: []string{"http://127.0.0.1:1"}, Timeout: 50 * time.Millisecond, Now: func() time.Time { return now }})
	binding, err := provider.CacheBinding(t.Context(), evidence.Request{Provider: "codex", Profile: "default"})
	if err != nil {
		t.Fatal(err)
	}
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
	observation := cacheObservation(now.Add(-time.Second))
	if _, err := store.Put(t.Context(), binding, observation); err != nil {
		t.Fatal(err)
	}
	adapter := runtimeAdapter{codex: provider, newStore: func() (*cache.Store, error) { return store, nil }}

	// When
	var stdout, stderr bytes.Buffer
	code := executeWithAdapterAt(t.Context(), []string{"value", "--cache=only", "--provider=codex", "--profile=default", "--window=weekly", "--field=remaining"}, &stdout, &stderr, "test", now, adapter)
	weekly := stdout.String()
	stdout.Reset()
	secondCode := executeWithAdapterAt(t.Context(), []string{"value", "--cache=only", "--provider=codex", "--profile=default", "--window=five_hour", "--field=remaining"}, &stdout, &stderr, "test", now, adapter)

	// Then
	if code != 0 || secondCode != 0 || weekly != "80\n" || stdout.String() != "60\n" || stderr.Len() != 0 {
		t.Fatalf("codes = %d/%d, weekly = %q, five-hour = %q, stderr = %q", code, secondCode, weekly, stdout.String(), stderr.String())
	}
}

func TestStore_CachedOnly_whenSnapshotMissingUsesExactDiagnostic(t *testing.T) {
	// Given
	authPath := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(authPath, []byte("metadata only"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{})
	adapter := runtimeAdapter{codex: codex.New(codex.Options{AuthFile: authPath}), newStore: func() (*cache.Store, error) { return store, nil }}

	// When
	var stdout, stderr bytes.Buffer
	code := executeWithAdapterAt(t.Context(), []string{"--cache=only", "--provider=codex", "--profile=default"}, &stdout, &stderr, "test", time.Now().UTC(), adapter)

	// Then
	if code != 1 || stdout.Len() != 0 || stderr.String() != "remainder: cached provider evidence is unavailable\n" {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestStore_CachedOnly_preservesSelectionErrors(t *testing.T) {
	// Given
	authPath := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(authPath, []byte("metadata only"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{})
	adapter := runtimeAdapter{codex: codex.New(codex.Options{AuthFile: authPath}), newStore: func() (*cache.Store, error) { return store, nil }}
	tests := [][]string{
		{"--cache=only", "--provider=other", "--profile=default"},
		{"--cache=only", "--provider=codex", "--profile=other"},
		{"--cache=only", "--provider=codex", "--profile=default", "--all"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			// When
			var stdout, stderr bytes.Buffer
			code := executeWithAdapterAt(t.Context(), args, &stdout, &stderr, "test", time.Now().UTC(), adapter)

			// Then
			if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), evidence.ErrInvalidSelection.Error()) {
				t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestStore_StaleFallback_remainsStaleUnderFreshnessPolicy(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	authPath := writeCLIAuth(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary", http.StatusInternalServerError)
	}))
	defer server.Close()
	provider := codex.New(codex.Options{AuthFile: authPath, Endpoints: []string{server.URL}, Client: server.Client(), Now: func() time.Time { return now }})
	binding, err := provider.CacheBinding(t.Context(), evidence.Request{Provider: "codex", Profile: "default"})
	if err != nil {
		t.Fatal(err)
	}
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
	if _, err := store.Put(t.Context(), binding, cacheObservation(now.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	adapter := runtimeAdapter{codex: provider, newStore: func() (*cache.Store, error) { return store, nil }}

	// When
	var stdout, stderr bytes.Buffer
	code := executeWithAdapterAt(t.Context(), []string{"--provider=codex", "--profile=default", "--max-age=1s", "--stale-on-error", "--freshness=fresh"}, &stdout, &stderr, "test", now, adapter)

	// Then
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), evidence.ErrStale.Error()) {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestStore_WriteUnavailable_emitsOneSanitizedWarningWithLiveResult(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":40,"limit_window_seconds":18000}}}`)
	}))
	defer server.Close()
	provider := codex.New(codex.Options{AuthFile: writeCLIAuth(t), Endpoints: []string{server.URL}, Client: server.Client(), Now: func() time.Time { return now }})
	adapter := runtimeAdapter{codex: provider, newStore: func() (*cache.Store, error) {
		return nil, errors.New("/Users/example/private-cache")
	}}

	// When
	var stdout, stderr bytes.Buffer
	code := executeWithAdapterAt(t.Context(), []string{"--provider=codex", "--profile=default"}, &stdout, &stderr, "test", now, adapter)

	// Then
	if code != 0 || stdout.Len() == 0 || strings.Count(stderr.String(), "remainder: warning:") != 1 || strings.Contains(stderr.String(), "/Users/") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestExecute_CacheWaitTimeout_returnsUnavailableExit(t *testing.T) {
	tests := []struct {
		name        string
		options     cache.Options
		cancelAfter time.Duration
		wantCode    int
	}{
		{name: "lock deadline", options: cache.Options{LockWait: 20 * time.Millisecond, OperationTimeout: time.Second}, wantCode: 1},
		{name: "operation deadline", options: cache.Options{LockWait: time.Second, OperationTimeout: 20 * time.Millisecond}, wantCode: 1},
		{name: "caller cancellation", options: cache.Options{LockWait: time.Second, OperationTimeout: time.Second}, cancelAfter: 20 * time.Millisecond, wantCode: 130},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
			authPath := filepath.Join(t.TempDir(), "auth.json")
			if err := os.WriteFile(authPath, []byte("malformed OAuth fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			provider := codex.New(codex.Options{AuthFile: authPath, Now: func() time.Time { return now }})
			binding, err := provider.CacheBinding(t.Context(), evidence.Request{Provider: "codex", Profile: "default"})
			if err != nil {
				t.Fatal(err)
			}
			test.options.Now = func() time.Time { return now }
			store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), test.options)
			if _, err := store.Put(t.Context(), binding, cacheObservation(now.Add(-time.Minute))); err != nil {
				t.Fatal(err)
			}
			lock, err := os.OpenFile(filepath.Join(filepath.Dir(store.SnapshotPath(binding)), "refresh.lock"), os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
				t.Fatal(err)
			}
			defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
			adapter := runtimeAdapter{codex: provider, newStore: func() (*cache.Store, error) { return store, nil }}
			ctx := t.Context()
			if test.cancelAfter > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				timer := time.AfterFunc(test.cancelAfter, cancel)
				defer timer.Stop()
			}

			// When
			var stdout, stderr bytes.Buffer
			code := executeWithAdapterAt(ctx, []string{"--provider=codex", "--profile=default"}, &stdout, &stderr, "test", now, adapter)

			// Then
			if code != test.wantCode || stdout.Len() != 0 {
				t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestCodexCacheProcessEntryPoint_reusesSnapshotAcrossCobraRuns(t *testing.T) {
	// Given
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":40,"limit_window_seconds":18000}}}`)
	}))
	defer server.Close()
	command := exec.Command(os.Args[0], "-test.run=^TestIssue6HelperProcess$")
	command.Env = []string{
		"REMAINDER_ISSUE6_HELPER=1",
		"REMAINDER_ISSUE6_AUTH=" + writeCLIAuth(t),
		"REMAINDER_ISSUE6_ENDPOINT=" + server.URL,
		"REMAINDER_ISSUE6_CACHE=" + filepath.Join(t.TempDir(), "remainder", "v1"),
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	// When
	err := command.Run()

	// Then
	if err != nil || stdout.String() != "60\n60\n" || stderr.Len() != 0 || requests.Load() != 1 {
		t.Fatalf("error = %v, stdout = %q, stderr = %q, requests = %d", err, stdout.String(), stderr.String(), requests.Load())
	}
}

func TestRuntimeCache_writesNormalizedProviderObservation(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":40,"limit_window_seconds":18000}}}`)
	}))
	defer server.Close()
	provider := codex.New(codex.Options{AuthFile: writeCLIAuth(t), Endpoints: []string{server.URL}, Client: server.Client(), Now: func() time.Time { return now }})
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
	adapter := runtimeAdapter{codex: provider, newStore: func() (*cache.Store, error) { return store, nil }}

	// When
	result, err := adapter.ObserveWithCache(t.Context(), evidence.Request{Provider: "codex", Profile: "default"}, cache.Policy{Mode: cache.ModeAuto, MaxAge: 5 * time.Second})

	// Then
	if err != nil || result.CacheError != nil {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func TestIssue6HelperProcess(t *testing.T) {
	if os.Getenv("REMAINDER_ISSUE6_HELPER") != "1" {
		return
	}
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	provider := codex.New(codex.Options{AuthFile: os.Getenv("REMAINDER_ISSUE6_AUTH"), Endpoints: []string{os.Getenv("REMAINDER_ISSUE6_ENDPOINT")}, Timeout: time.Second, Now: func() time.Time { return now }})
	store := cache.New(os.Getenv("REMAINDER_ISSUE6_CACHE"), cache.Options{Now: func() time.Time { return now }})
	adapter := runtimeAdapter{codex: provider, newStore: func() (*cache.Store, error) { return store, nil }}
	args := []string{"value", "--provider=codex", "--profile=default", "--window=five_hour", "--field=remaining"}
	if code := executeWithAdapterAt(context.Background(), args, os.Stdout, os.Stderr, "test", now, adapter); code != 0 {
		os.Exit(code)
	}
	args = append(args, "--cache=only")
	os.Exit(executeWithAdapterAt(context.Background(), args, os.Stdout, os.Stderr, "test", now, adapter))
}

func cacheObservation(observedAt time.Time) evidence.Observation {
	amount := evidence.JSONNumber("80")
	shortAmount := evidence.JSONNumber("60")
	return evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "default",
		Account:       evidence.AccountIdentity{LastObserved: "acct-test", Binding: evidence.IdentityVerified},
		Source:        evidence.SourceIdentity{Kind: "native_file_http", Name: "codex_auth_json"},
		ObservedAt:    observedAt,
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows: []evidence.Window{
			{ID: "five_hour", Scope: evidence.ScopeAccount, Unit: "percent", Limits: []evidence.Limit{{ID: "five_hour_remaining", Field: "remaining", Value: evidence.Value{State: evidence.ValueDefined, Amount: &shortAmount}}}},
			{ID: "weekly", Scope: evidence.ScopeAccount, Unit: "percent", Limits: []evidence.Limit{{ID: "weekly_remaining", Field: "remaining", Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}}},
		},
	}
}

var _ = errors.Is
