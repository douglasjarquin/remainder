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

const failureTestProfile = `{"account":{"uuid":"account-test"},"organization":{"uuid":"org-test"}}`

func TestObserve_failureKindPerResponse(t *testing.T) {
	now := fixedNow()
	tests := []struct {
		name        string
		profile     http.HandlerFunc
		usage       http.HandlerFunc
		closed      bool
		canceled    bool
		keychain    func(context.Context) ([]byte, error)
		noPrompt    bool
		account     string
		kind        cache.FailureKind
		retry       time.Time
		unavailable bool
	}{
		{name: "http 401", usage: status(http.StatusUnauthorized), kind: cache.FailureRevoked, unavailable: true},
		{name: "http 403", usage: status(http.StatusForbidden), kind: cache.FailureTransient, unavailable: true},
		{name: "http 429 with retry-after", usage: rateLimited("30"), kind: cache.FailureTransient, retry: now.Add(30 * time.Second), unavailable: true},
		{name: "http 429 without retry-after", usage: rateLimited(""), kind: cache.FailureTransient, unavailable: true},
		{name: "http 500", usage: status(http.StatusInternalServerError), kind: cache.FailureTransient, unavailable: true},
		{name: "profile http 401", profile: status(http.StatusUnauthorized), kind: cache.FailureRevoked, unavailable: true},
		{name: "network error", closed: true, kind: cache.FailureTransient, unavailable: true},
		{name: "malformed body", usage: body(`{`), kind: cache.FailureTransient, unavailable: true},
		{name: "oversized body", usage: body(strings.Repeat(" ", maxResponseBytes+1)), kind: cache.FailurePermanent, unavailable: true},
		{name: "profile without account", profile: body(`{}`), kind: cache.FailurePermanent, unavailable: true},
		{name: "context canceled before request", canceled: true, kind: cache.FailureTransient},
		{name: "keychain read deadline", keychain: blockUntilDone, kind: cache.FailureTransient, unavailable: true},
		{name: "keychain read failure", keychain: func(context.Context) ([]byte, error) { return nil, errors.New("security failed") }, kind: cache.FailurePermanent, unavailable: true},
		{name: "keychain prompt not allowed", keychain: blockUntilDone, noPrompt: true, kind: cache.FailurePermanent, unavailable: true},
		{name: "account selector mismatch", account: "account-other@org-test", kind: cache.FailureAccountMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile, usage := tt.profile, tt.usage
			if profile == nil {
				profile = body(failureTestProfile)
			}
			if usage == nil {
				usage = body(`{}`)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/profile") {
					profile(w, r)
					return
				}
				usage(w, r)
			}))
			if tt.closed {
				server.Close()
			} else {
				t.Cleanup(server.Close)
			}
			options := Options{AuthFile: writeAuth(t, `{"claudeAiOauth":{"accessToken":"synthetic-access","expiresAt":1788973200000}}`), ProfileEndpoint: server.URL + "/profile", UsageEndpoint: server.URL + "/usage", Client: server.Client(), Timeout: time.Second, Now: fixedNow}
			if tt.keychain != nil {
				options.AuthFile = filepath.Join(t.TempDir(), ".credentials.json")
				options.KeychainReader = tt.keychain
				options.Timeout = 10 * time.Millisecond
				options.goos = "darwin"
			}
			adapter := New(options)
			if !tt.noPrompt {
				adapter = adapter.WithKeychainPrompt()
			}
			ctx := t.Context()
			if tt.canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			_, err := adapter.Observe(ctx, evidence.Request{Provider: "claude", Profile: "default", Account: tt.account})

			kind, retry := cache.Classify(err)
			if kind != tt.kind || !retry.Equal(tt.retry) {
				t.Fatalf("failure for %v = %q %s, want %q %s", err, kind, retry, tt.kind, tt.retry)
			}
			if got := errors.Is(err, evidence.ErrProviderUnavailable); got != tt.unavailable {
				t.Fatalf("errors.Is(%v, ErrProviderUnavailable) = %t, want %t", err, got, tt.unavailable)
			}
		})
	}
}

func blockUntilDone(ctx context.Context) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

func rateLimited(retryAfter string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}
}

func body(value string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(value)) }
}
