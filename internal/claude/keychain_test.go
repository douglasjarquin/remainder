package claude

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestAdapterMacKeychainFallback_readsCredentialsWhenFileAbsent(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", ".credentials.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/profile") {
			_, _ = w.Write([]byte(`{"account":{"uuid":"account-1"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":0}}`))
	}))
	defer server.Close()
	calls := 0
	adapter := New(Options{
		AuthFile:        missing,
		ProfileEndpoint: server.URL + "/profile",
		UsageEndpoint:   server.URL + "/usage",
		Client:          server.Client(),
		Now:             fixedNow,
		KeychainReader: func(context.Context) ([]byte, error) {
			calls++
			return []byte(`{"claudeAiOauth":{"accessToken":"synthetic-access","expiresAt":1788973200000}}`), nil
		},
		goos: "darwin",
	}).WithKeychainPrompt()

	observation, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})

	if err != nil || calls != 1 {
		t.Fatalf("Observe() error/keychain calls = %v/%d, want success/1", err, calls)
	}
	if observation.Source != keychainSourceIdentity {
		t.Fatalf("source = %+v", observation.Source)
	}
}

func TestAdapterMacKeychainConsent_requiredBeforeHelper(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", ".credentials.json")
	calls := 0
	adapter := New(Options{
		AuthFile: missing,
		Now:      fixedNow,
		KeychainReader: func(context.Context) ([]byte, error) {
			calls++
			return nil, errors.New("must not be called")
		},
		goos: "darwin",
	})

	_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})

	if err == nil || calls != 0 || !strings.Contains(err.Error(), "--allow-keychain-prompt") {
		t.Fatalf("Observe() error/calls = %v/%d", err, calls)
	}
}

func TestAdapterNonDarwinFileAbsent_neverInvokesKeychain(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", ".credentials.json")
	calls := 0
	adapter := New(Options{
		AuthFile: missing,
		Now:      fixedNow,
		KeychainReader: func(context.Context) ([]byte, error) {
			calls++
			return nil, errors.New("must not be called")
		},
		goos: "linux",
	}).WithKeychainPrompt()

	_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})

	if err == nil || calls != 0 || !strings.Contains(err.Error(), "missing") || strings.Contains(err.Error(), "--allow-keychain-prompt") {
		t.Fatalf("Observe() error/calls = %v/%d", err, calls)
	}
}

func TestAdapterDarwinFilePresent_prefersFileOverKeychain(t *testing.T) {
	authFile := writeAuth(t, `{"claudeAiOauth":{"accessToken":"synthetic-access","expiresAt":1788973200000}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/profile") {
			_, _ = w.Write([]byte(`{"account":{"uuid":"account-1"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":0}}`))
	}))
	defer server.Close()
	calls := 0
	adapter := New(Options{
		AuthFile:        authFile,
		ProfileEndpoint: server.URL + "/profile",
		UsageEndpoint:   server.URL + "/usage",
		Client:          server.Client(),
		Now:             fixedNow,
		KeychainReader: func(context.Context) ([]byte, error) {
			calls++
			return nil, errors.New("must not be called")
		},
		goos: "darwin",
	}).WithKeychainPrompt()

	observation, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})

	if err != nil || calls != 0 || observation.Source != fileSourceIdentity {
		t.Fatalf("Observe() error/calls/source = %v/%d/%+v", err, calls, observation.Source)
	}
}

func TestAdapterMacKeychainFailures_areBoundedAndRedacted(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", ".credentials.json")
	tests := []struct {
		name       string
		reader     func(context.Context) ([]byte, error)
		wantPhrase string
	}{
		{name: "malformed", reader: func(context.Context) ([]byte, error) { return []byte("{"), nil }, wantPhrase: "malformed"},
		{name: "no token", reader: func(context.Context) ([]byte, error) { return []byte(`{"claudeAiOauth":{}}`), nil }, wantPhrase: "lacks an OAuth access token"},
		{name: "expired", reader: func(context.Context) ([]byte, error) {
			return []byte(`{"claudeAiOauth":{"accessToken":"synthetic-secret","expiresAt":1}}`), nil
		}, wantPhrase: "expired"},
		{name: "oversized", reader: func(context.Context) ([]byte, error) { return []byte(strings.Repeat("x", maxAuthBytes+1)), nil }, wantPhrase: "too large"},
		{name: "helper error", reader: func(context.Context) ([]byte, error) { return nil, errors.New("synthetic-secret") }, wantPhrase: "could not be read"},
		{name: "helper reports oversized", reader: func(context.Context) ([]byte, error) { return nil, errKeychainOutputTooLarge }, wantPhrase: "too large"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := New(Options{AuthFile: missing, Now: fixedNow, KeychainReader: test.reader, goos: "darwin"}).WithKeychainPrompt()

			_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})
			kind, _ := adapter.Failure(err)

			if err == nil || kind != cache.FailurePermanent || !strings.Contains(err.Error(), test.wantPhrase) || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatalf("Observe() error/kind = %v/%s", err, kind)
			}
		})
	}
}

func TestAdapterMacKeychainCancellation_returnsContextError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", ".credentials.json")
	adapter := New(Options{AuthFile: missing, Now: fixedNow, KeychainReader: func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}, Timeout: 10 * time.Millisecond, goos: "darwin"}).WithKeychainPrompt()

	_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Observe() error = %v", err)
	}
}

func TestAdapterMacCacheBinding_usesStaticIdentityWithoutInvokingHelper(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", ".credentials.json")
	calls := 0
	adapter := New(Options{AuthFile: missing, Now: fixedNow, KeychainReader: func(context.Context) ([]byte, error) {
		calls++
		return nil, errors.New("must not be called")
	}, goos: "darwin"}).WithKeychainPrompt()

	binding, err := adapter.CacheBinding(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})

	if err != nil || calls != 0 {
		t.Fatalf("CacheBinding() error/calls = %v/%d", err, calls)
	}
	if binding.SourceKind != keychainSourceIdentity.Kind || binding.SourceName != keychainSourceIdentity.Name || binding.CredentialFingerprint == "" {
		t.Fatalf("binding = %+v", binding)
	}
}

func TestAdapterMacKeychain_rejectsWrongAccountBeforeUsage(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", ".credentials.json")
	usageCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/usage") {
			usageCalls++
		}
		_, _ = w.Write([]byte(`{"account":{"uuid":"account-1"}}`))
	}))
	defer server.Close()
	adapter := New(Options{
		AuthFile:        missing,
		ProfileEndpoint: server.URL + "/profile",
		UsageEndpoint:   server.URL + "/usage",
		Client:          server.Client(),
		Now:             fixedNow,
		KeychainReader: func(context.Context) ([]byte, error) {
			return []byte(`{"claudeAiOauth":{"accessToken":"synthetic-access","expiresAt":1788973200000}}`), nil
		},
		goos: "darwin",
	}).WithKeychainPrompt()

	_, err := adapter.Observe(t.Context(), evidence.Request{Provider: "claude", Profile: "default", Account: "account-2"})

	if !errors.Is(err, evidence.ErrWrongAccount) || usageCalls != 0 {
		t.Fatalf("error/usage calls = %v/%d", err, usageCalls)
	}
}

func TestAdapterMacCacheBinding_requiresConsentBeforeHelper(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", ".credentials.json")
	calls := 0
	adapter := New(Options{AuthFile: missing, Now: fixedNow, KeychainReader: func(context.Context) ([]byte, error) {
		calls++
		return nil, errors.New("must not be called")
	}, goos: "darwin"})

	_, err := adapter.CacheBinding(t.Context(), evidence.Request{Provider: "claude", Profile: "default"})

	if err == nil || calls != 0 || !strings.Contains(err.Error(), "--allow-keychain-prompt") {
		t.Fatalf("CacheBinding() error/calls = %v/%d", err, calls)
	}
}
