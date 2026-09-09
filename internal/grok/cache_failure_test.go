package grok

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestCacheBindingIsolation(t *testing.T) {
	first := filepath.Join(t.TempDir(), "auth.json")
	second := filepath.Join(t.TempDir(), "auth.json")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("not parsed by CacheBinding"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	one, firstErr := New(Options{AuthFile: first}).CacheBinding(t.Context(), request())
	two, secondErr := New(Options{AuthFile: second}).CacheBinding(t.Context(), request())
	if firstErr != nil || secondErr != nil {
		t.Fatalf("binding errors = %v, %v", firstErr, secondErr)
	}
	if one.CredentialFingerprint == two.CredentialFingerprint || one.Provider != "grok" || one.Profile != "default" || one.ResponseBoundary != "grok_credits_config" || one.SourceKind != "native_file_http" || one.SourceName != "grok_auth_json" {
		t.Fatalf("bindings = %+v, %+v", one, two)
	}
	if err := one.Validate(); err != nil {
		t.Fatalf("invalid binding: %v", err)
	}
	if _, err := New(Options{AuthFile: first}).CacheBinding(t.Context(), evidence.Request{Provider: "codex", Profile: "default"}); !errors.Is(err, evidence.ErrInvalidSelection) {
		t.Fatalf("selection error = %v", err)
	}
}

func TestFailureClassification(t *testing.T) {
	adapter := Default()
	retryAt := time.Date(2026, 9, 9, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		err       error
		want      cache.FailureKind
		wantRetry time.Time
	}{{"none", nil, cache.FailureNone, time.Time{}}, {"revoked", ErrAuthorizationRejected, cache.FailureRevoked, time.Time{}}, {"account mismatch", ErrAccountMismatch, cache.FailureAccountMismatch, time.Time{}}, {"evidence account mismatch", evidence.ErrWrongAccount, cache.FailureAccountMismatch, time.Time{}}, {"transient", ErrTransient, cache.FailureTransient, time.Time{}}, {"retry", &RetryError{RetryAt: retryAt}, cache.FailureTransient, retryAt}, {"permanent", errors.New("bad local source"), cache.FailurePermanent, time.Time{}}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, retry := adapter.Failure(tt.err)
			if kind != tt.want || !retry.Equal(tt.wantRetry) {
				t.Fatalf("Failure(%v) = %q, %v", tt.err, kind, retry)
			}
		})
	}
}
