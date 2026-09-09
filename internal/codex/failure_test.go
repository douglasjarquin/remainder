package codex

import (
	"errors"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestAdapter_Failure_preserves_existing_CLI_classification(t *testing.T) {
	now := time.Date(2026, time.September, 9, 16, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		err   error
		kind  cache.FailureKind
		retry time.Time
	}{
		{"none", nil, cache.FailureNone, time.Time{}},
		{"revoked", ErrAuthorizationRejected, cache.FailureRevoked, time.Time{}},
		{"account mismatch", ErrAccountMismatch, cache.FailureAccountMismatch, time.Time{}},
		{"wrong account", evidence.ErrWrongAccount, cache.FailureAccountMismatch, time.Time{}},
		{"transient", ErrTransient, cache.FailureTransient, time.Time{}},
		{"retry", &RetryError{RetryAt: now}, cache.FailureTransient, now},
		{"permanent", errors.New("permanent"), cache.FailurePermanent, time.Time{}},
	}
	adapter := New(Options{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, retry := adapter.Failure(tt.err)
			if kind != tt.kind || !retry.Equal(tt.retry) {
				t.Fatalf("Failure() = %q %s, want %q %s", kind, retry, tt.kind, tt.retry)
			}
		})
	}
}
