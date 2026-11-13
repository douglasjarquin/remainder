package cache_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestClassify_returnsKindAndRetryTime(t *testing.T) {
	retryAt := time.Date(2026, time.November, 12, 16, 0, 0, 0, time.UTC)
	cause := errors.New("provider said no")
	tests := []struct {
		name  string
		err   error
		kind  cache.FailureKind
		retry time.Time
	}{
		{"nil", nil, cache.FailureNone, time.Time{}},
		{"transient", &cache.FetchError{Kind: cache.FailureTransient, RetryAt: retryAt, Err: cause}, cache.FailureTransient, retryAt},
		{"permanent", &cache.FetchError{Kind: cache.FailurePermanent, Err: cause}, cache.FailurePermanent, time.Time{}},
		{"revoked", &cache.FetchError{Kind: cache.FailureRevoked, Err: cause}, cache.FailureRevoked, time.Time{}},
		{"account mismatch", &cache.FetchError{Kind: cache.FailureAccountMismatch, Err: cause}, cache.FailureAccountMismatch, time.Time{}},
		{"wrapped fetch error", fmt.Errorf("outer: %w", &cache.FetchError{Kind: cache.FailureRevoked, Err: cause}), cache.FailureRevoked, time.Time{}},
		{"fetch error with no kind", &cache.FetchError{Err: cause}, cache.FailurePermanent, time.Time{}},
		{"wrong account", evidence.ErrWrongAccount, cache.FailureAccountMismatch, time.Time{}},
		{"wrapped wrong account", fmt.Errorf("%w: selector cannot be verified", evidence.ErrWrongAccount), cache.FailureAccountMismatch, time.Time{}},
		{"deadline", context.DeadlineExceeded, cache.FailureTransient, time.Time{}},
		{"canceled", context.Canceled, cache.FailureTransient, time.Time{}},
		{"wrapped deadline", fmt.Errorf("keychain: %w", context.DeadlineExceeded), cache.FailureTransient, time.Time{}},
		{"unrecognized", cause, cache.FailurePermanent, time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, retry := cache.Classify(tt.err)
			if kind != tt.kind || !retry.Equal(tt.retry) {
				t.Fatalf("Classify() = %q %s, want %q %s", kind, retry, tt.kind, tt.retry)
			}
		})
	}
}

func TestFetchError_matchesProviderUnavailableAndCause(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &cache.FetchError{Kind: cache.FailureTransient, Err: fmt.Errorf("request timed out: %w", context.DeadlineExceeded)})

	if !errors.Is(err, evidence.ErrProviderUnavailable) {
		t.Fatalf("errors.Is(%v, ErrProviderUnavailable) = false", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(%v, DeadlineExceeded) = false", err)
	}
	if got, want := (&cache.FetchError{Err: errors.New("Codex quota response is too large")}).Error(), "provider evidence is unavailable: Codex quota response is too large"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	rateLimited := &cache.FetchError{Kind: cache.FailureTransient, RetryAt: time.Date(2026, time.November, 12, 16, 0, 30, 0, time.UTC), Err: errors.New("Codex quota endpoint is rate limited")}
	if got, want := rateLimited.Error(), "provider evidence is unavailable: Codex quota endpoint is rate limited until 2026-11-12T16:00:30Z"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestFetchFailure_keepsDeclaredKindAndClassifiesTheRest(t *testing.T) {
	declared := &cache.FetchError{Kind: cache.FailureRevoked, Err: errors.New("rejected")}
	tests := []struct {
		name string
		err  error
		kind cache.FailureKind
	}{
		{"declared", declared, cache.FailureRevoked},
		{"keychain deadline", fmt.Errorf("keychain: %w", context.DeadlineExceeded), cache.FailureTransient},
		{"malformed credentials", errors.New("credentials are malformed"), cache.FailurePermanent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cache.FetchFailure(tt.err)
			fetchErr, ok := errors.AsType[*cache.FetchError](err)
			if !ok || fetchErr.Kind != tt.kind || !errors.Is(err, tt.err) || !errors.Is(err, evidence.ErrProviderUnavailable) {
				t.Fatalf("FetchFailure(%v) = %#v, want kind %q wrapping the cause", tt.err, err, tt.kind)
			}
		})
	}
	if got := cache.FetchFailure(declared); got != error(declared) {
		t.Fatalf("FetchFailure() rewrapped a declared fetch error: %v", got)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		value string
		want  time.Time
	}{
		{"seconds", "30", now.Add(30 * time.Second)},
		{"zero seconds", "0", now},
		{"http date", "Wed, 09 Sep 2026 12:05:00 GMT", time.Date(2026, time.September, 9, 12, 5, 0, 0, time.UTC)},
		{"empty", "", time.Time{}},
		{"negative", "-1", time.Time{}},
		{"malformed", "soon", time.Time{}},
		{"overflowing seconds", "99999999999", time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cache.ParseRetryAfter(tt.value, now); !got.Equal(tt.want) {
				t.Fatalf("ParseRetryAfter(%q) = %s, want %s", tt.value, got, tt.want)
			}
		})
	}
}

func TestFetchFailure_passesAccountSelectorErrorsThrough(t *testing.T) {
	err := fmt.Errorf("%w: response cannot verify an account selector", evidence.ErrWrongAccount)

	got := cache.FetchFailure(err)

	if got != err || errors.Is(got, evidence.ErrProviderUnavailable) {
		t.Fatalf("FetchFailure(%v) = %v, want the selector error unchanged so it exits as a usage error", err, got)
	}
}

func TestRequestFailure_isTransientAndNamesCancellation(t *testing.T) {
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	tests := []struct {
		name    string
		ctx     context.Context
		message string
		cause   error
	}{
		{"canceled", canceled, "provider evidence is unavailable: Grok quota request canceled or timed out: context canceled", context.Canceled},
		{"network", t.Context(), "provider evidence is unavailable: Grok quota request failed", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cache.RequestFailure(tt.ctx, "Grok")

			kind, retryAt := cache.Classify(err)
			if kind != cache.FailureTransient || !retryAt.IsZero() || err.Error() != tt.message || (tt.cause != nil && !errors.Is(err, tt.cause)) {
				t.Fatalf("RequestFailure() = %v (%q %s), want transient %q", err, kind, retryAt, tt.message)
			}
		})
	}
}
