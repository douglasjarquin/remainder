package grok

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
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		closed      bool
		canceled    bool
		missingAuth bool
		account     string
		kind        cache.FailureKind
		retry       time.Time
		unavailable bool
	}{
		{name: "http 401", handler: grokStatus(http.StatusUnauthorized), kind: cache.FailureRevoked, unavailable: true},
		{name: "http 403", handler: grokStatus(http.StatusForbidden), kind: cache.FailureRevoked, unavailable: true},
		{name: "http 429 with retry-after", handler: grokRateLimited("30"), kind: cache.FailureTransient, retry: now.Add(30 * time.Second), unavailable: true},
		{name: "http 429 without retry-after", handler: grokRateLimited(""), kind: cache.FailureTransient, unavailable: true},
		{name: "http 500", handler: grokStatus(http.StatusInternalServerError), kind: cache.FailureTransient, unavailable: true},
		{name: "header grpc unauthenticated", handler: grpcHeader("16", ""), kind: cache.FailureRevoked, unavailable: true},
		{name: "header grpc permission denied for auth", handler: grpcHeader("7", "token%20expired"), kind: cache.FailureRevoked, unavailable: true},
		{name: "header grpc permission denied otherwise", handler: grpcHeader("7", "quota%20permission"), kind: cache.FailureTransient, unavailable: true},
		{name: "header grpc resource exhausted", handler: grpcHeader("8", ""), kind: cache.FailureTransient, unavailable: true},
		{name: "header grpc unavailable", handler: grpcHeader("14", ""), kind: cache.FailureTransient, unavailable: true},
		{name: "header grpc status invalid", handler: grpcHeader("99", ""), kind: cache.FailurePermanent, unavailable: true},
		{name: "trailer grpc unauthenticated", handler: grpcTrailer("16"), kind: cache.FailureRevoked, unavailable: true},
		{name: "trailer grpc unavailable", handler: grpcTrailer("14"), kind: cache.FailureTransient, unavailable: true},
		{name: "body trailer grpc unauthenticated", handler: grpcBody(append(frame(0, []byte{}), frame(0x80, []byte("grpc-status: 16\r\n"))...)), kind: cache.FailureRevoked, unavailable: true},
		{name: "body trailer grpc resource exhausted", handler: grpcBody(append(frame(0, []byte{}), frame(0x80, []byte("grpc-status: 8\r\n"))...)), kind: cache.FailureTransient, unavailable: true},
		{name: "body trailer grpc status invalid", handler: grpcBody(append(frame(0, []byte{}), frame(0x80, []byte("grpc-status: 99\r\n"))...)), kind: cache.FailureTransient, unavailable: true},
		{name: "invalid framing", handler: grpcBody([]byte{0, 0, 0, 0, 9}), kind: cache.FailureTransient, unavailable: true},
		{name: "invalid protobuf", handler: grpcBody(grpcResponse([]byte{0xff})), kind: cache.FailureTransient, unavailable: true},
		{name: "oversized content length", handler: oversized, kind: cache.FailurePermanent, unavailable: true},
		{name: "network error", closed: true, kind: cache.FailureTransient, unavailable: true},
		{name: "context canceled before request", canceled: true, kind: cache.FailureTransient},
		{name: "missing auth file", missingAuth: true, kind: cache.FailurePermanent, unavailable: true},
		{name: "account selector", account: "user@example.test", kind: cache.FailureAccountMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := tt.handler
			if handler == nil {
				handler = grokStatus(http.StatusOK)
			}
			server := httptest.NewServer(handler)
			if tt.closed {
				server.Close()
			} else {
				t.Cleanup(server.Close)
			}
			path := authPath(t, "grok.com", "synthetic-token", "", "", "")
			if tt.missingAuth {
				path = filepath.Join(t.TempDir(), "missing.json")
			}
			adapter := New(Options{AuthFile: path, Client: server.Client(), Endpoint: server.URL, Timeout: time.Second, Now: func() time.Time { return now }})
			ctx := t.Context()
			if tt.canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			_, err := adapter.Observe(ctx, evidence.Request{Provider: "grok", Profile: "default", Account: tt.account})

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

func grokStatus(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

func grokRateLimited(retryAfter string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}
}

func grpcHeader(status, message string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Grpc-Status", status)
		if message != "" {
			w.Header().Set("Grpc-Message", message)
		}
		w.WriteHeader(http.StatusOK)
	}
}

func grpcTrailer(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Trailer", "Grpc-Status")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(frame(0, []byte{}))
		w.Header().Set("Grpc-Status", status)
	}
}

func grpcBody(data []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) }
}

func oversized(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Length", strconv.Itoa(maxResponseBytes+1))
	w.WriteHeader(http.StatusOK)
}
