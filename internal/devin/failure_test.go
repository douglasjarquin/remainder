package devin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestObserve_failureKindPerResponse(t *testing.T) {
	type row struct {
		name        string
		handler     http.HandlerFunc
		closed      bool
		canceled    bool
		missingAuth bool
		account     string
		kind        cache.FailureKind
		retry       time.Time
		unavailable bool
	}
	tests := []row{
		{name: "http 401", handler: devinStatus(http.StatusUnauthorized, ""), kind: cache.FailureRevoked, unavailable: true},
		{name: "http 403", handler: devinStatus(http.StatusForbidden, ""), kind: cache.FailureRevoked, unavailable: true},
		{name: "http 429 with retry-after", handler: devinRateLimited("30", ""), kind: cache.FailureTransient, retry: testNow.Add(30 * time.Second), unavailable: true},
		{name: "http 429 without retry-after", handler: devinRateLimited("", ""), kind: cache.FailureTransient, unavailable: true},
		{name: "http 500", handler: devinStatus(http.StatusInternalServerError, ""), kind: cache.FailureTransient, unavailable: true},
		{name: "http 404", handler: devinStatus(http.StatusNotFound, ""), kind: cache.FailureTransient, unavailable: true},
		{name: "connect resource exhausted", handler: devinRateLimited("30", `{"code":"resource_exhausted"}`), kind: cache.FailureTransient, retry: testNow.Add(30 * time.Second), unavailable: true},
		{name: "connect unknown code falls back to http status", handler: devinStatus(http.StatusForbidden, `{"code":"not_found"}`), kind: cache.FailureRevoked, unavailable: true},
		{name: "malformed body", handler: devinBody(`{`), kind: cache.FailureTransient, unavailable: true},
		{name: "oversized content length", handler: devinOversized, kind: cache.FailurePermanent, unavailable: true},
		{name: "response without user status", handler: devinBody(`{}`), kind: cache.FailurePermanent, unavailable: true},
		{name: "network error", closed: true, kind: cache.FailureTransient, unavailable: true},
		{name: "context canceled before request", canceled: true, kind: cache.FailureTransient},
		{name: "missing auth file", missingAuth: true, kind: cache.FailurePermanent, unavailable: true},
		{name: "account selector mismatch", handler: devinBody(`{"userStatus":{"userId":"user-test","planStatus":{}}}`), account: "user-other", kind: cache.FailureAccountMismatch},
		{name: "account selector unverifiable", handler: devinBody(`{"userStatus":{"planStatus":{}}}`), account: "user-test", kind: cache.FailureAccountMismatch},
	}
	for _, code := range []string{"unauthenticated", "permission_denied", "invalid_argument"} {
		tests = append(tests, row{name: "connect " + code, handler: devinStatus(http.StatusBadRequest, `{"code":"`+code+`"}`), kind: cache.FailureRevoked, unavailable: true})
	}
	for _, code := range []string{"unavailable", "deadline_exceeded", "internal", "aborted", "data_loss", "unknown"} {
		tests = append(tests, row{name: "connect " + code, handler: devinStatus(http.StatusUnauthorized, `{"code":"`+code+`"}`), kind: cache.FailureTransient, unavailable: true})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := tt.handler
			if handler == nil {
				handler = devinStatus(http.StatusOK, "")
			}
			server := httptest.NewServer(handler)
			if tt.closed {
				server.Close()
			} else {
				t.Cleanup(server.Close)
			}
			authFile := writeCredentials(t, `windsurf_api_key = "synthetic-devin-key"`+"\n")
			if tt.missingAuth {
				authFile = filepath.Join(t.TempDir(), "missing.toml")
			}
			ctx := t.Context()
			if tt.canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			request := testRequest()
			request.Account = tt.account

			_, err := testAdapter(t, authFile, server).Observe(ctx, request)

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

func devinStatus(code int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}
}

func devinRateLimited(retryAfter, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(body))
	}
}

func devinBody(value string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(value)) }
}

func devinOversized(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Length", strconv.Itoa(maxResponseBytes+1))
	w.WriteHeader(http.StatusOK)
}
