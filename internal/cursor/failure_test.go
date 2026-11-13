package cursor

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
	usage := cursorRPC{body: `{"planUsage":{"totalPercentUsed":25}}`}
	tests := []struct {
		name        string
		usage       cursorRPC
		plan        cursorRPC
		closed      bool
		canceled    bool
		missingAuth bool
		account     string
		kind        cache.FailureKind
		retry       time.Time
		unavailable bool
	}{
		{name: "http 401", usage: cursorRPC{status: http.StatusUnauthorized}, kind: cache.FailureRevoked, unavailable: true},
		{name: "http 403", usage: cursorRPC{status: http.StatusForbidden}, kind: cache.FailurePermanent, unavailable: true},
		{name: "http 429 with retry-after", usage: cursorRPC{status: http.StatusTooManyRequests, retryAfter: "30"}, kind: cache.FailureTransient, retry: fixtureNow.Add(30 * time.Second), unavailable: true},
		{name: "http 429 without retry-after", usage: cursorRPC{status: http.StatusTooManyRequests}, kind: cache.FailureTransient, unavailable: true},
		{name: "http 500", usage: cursorRPC{status: http.StatusInternalServerError}, kind: cache.FailureTransient, unavailable: true},
		{name: "malformed body", usage: cursorRPC{body: `{`}, kind: cache.FailurePermanent, unavailable: true},
		{name: "oversized body", usage: cursorRPC{body: strings.Repeat(" ", maxResponseBytes+1)}, kind: cache.FailurePermanent, unavailable: true},
		{name: "supplemental http 401", usage: usage, plan: cursorRPC{status: http.StatusUnauthorized}, kind: cache.FailureRevoked, unavailable: true},
		{name: "network error", closed: true, kind: cache.FailureTransient, unavailable: true},
		{name: "context canceled before request", canceled: true, kind: cache.FailureTransient},
		{name: "missing auth file", missingAuth: true, kind: cache.FailurePermanent, unavailable: true},
		{name: "account selector", account: "user@example.test", kind: cache.FailureAccountMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := tt.plan
			if plan == (cursorRPC{}) {
				plan = cursorRPC{body: `{}`}
			}
			responses := map[string]cursorRPC{"GetCurrentPeriodUsage": tt.usage, "GetPlanInfo": plan, "GetSandUsageStatus": {body: `{}`}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				response := responses[filepath.Base(r.URL.Path)]
				if response.retryAfter != "" {
					w.Header().Set("Retry-After", response.retryAfter)
				}
				if response.status != 0 {
					w.WriteHeader(response.status)
				}
				_, _ = w.Write([]byte(response.body))
			}))
			if tt.closed {
				server.Close()
			} else {
				t.Cleanup(server.Close)
			}
			authFile := writeAuth(t, "synthetic-secret")
			if tt.missingAuth {
				authFile = filepath.Join(t.TempDir(), "missing.json")
			}
			adapter := New(Options{AuthFile: authFile, Endpoint: server.URL, Client: server.Client(), Now: func() time.Time { return fixtureNow }, Timeout: time.Second, goos: "linux"})
			ctx := t.Context()
			if tt.canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			request := cursorRequest()
			request.Account = tt.account

			_, err := adapter.Observe(ctx, request)

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

type cursorRPC struct {
	status     int
	retryAfter string
	body       string
}
