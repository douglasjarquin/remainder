package codex

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

func TestObserve_failureKindPerResponse(t *testing.T) {
	now := time.Unix(1_773_000_000, 0).UTC()
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		closed      bool
		canceled    bool
		authFile    string
		account     string
		kind        cache.FailureKind
		retry       time.Time
		unavailable bool
	}{
		{name: "http 401", handler: status(http.StatusUnauthorized), kind: cache.FailureRevoked, unavailable: true},
		{name: "http 403", handler: status(http.StatusForbidden), kind: cache.FailureRevoked, unavailable: true},
		{name: "http 429 with retry-after", handler: rateLimited("30"), kind: cache.FailureTransient, retry: now.Add(30 * time.Second), unavailable: true},
		{name: "http 429 without retry-after", handler: rateLimited(""), kind: cache.FailureTransient, unavailable: true},
		{name: "http 500", handler: status(http.StatusInternalServerError), kind: cache.FailureTransient, unavailable: true},
		{name: "network error", closed: true, kind: cache.FailureTransient, unavailable: true},
		{name: "context canceled before request", handler: status(http.StatusOK), canceled: true, kind: cache.FailureTransient, unavailable: true},
		{name: "malformed body", handler: body(`{`), kind: cache.FailureTransient, unavailable: true},
		{name: "oversized body", handler: body(strings.Repeat(" ", maxResponseBytes+1)), kind: cache.FailurePermanent, unavailable: true},
		{name: "unsupported schema", handler: body(`{}`), kind: cache.FailurePermanent, unavailable: true},
		{name: "response account differs from credential", handler: body(`{"account_id":"acct-other"}`), kind: cache.FailureAccountMismatch, unavailable: true},
		{name: "missing auth file", handler: status(http.StatusOK), authFile: "missing", kind: cache.FailurePermanent, unavailable: true},
		{name: "account selector mismatch", handler: status(http.StatusOK), account: "acct-other", kind: cache.FailureAccountMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			if tt.closed {
				server.Close()
			} else {
				t.Cleanup(server.Close)
			}
			authFile := writeTestAuth(t)
			if tt.authFile != "" {
				authFile = filepath.Join(t.TempDir(), tt.authFile)
			}
			adapter := New(Options{AuthFile: authFile, Endpoints: []string{server.URL}, Client: server.Client(), Timeout: time.Second, Now: func() time.Time { return now }})
			ctx := t.Context()
			if tt.canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			_, err := adapter.Observe(ctx, evidence.Request{Provider: "codex", Profile: "default", Account: tt.account})

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
