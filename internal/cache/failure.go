package cache

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

// FetchError is a provider collection failure tagged with how the cache
// treats it. Providers build it where they detect the failure, so the kind is
// declared once at the source instead of being inferred from sentinels later.
type FetchError struct {
	Kind    FailureKind
	RetryAt time.Time
	Err     error
}

func (e *FetchError) Error() string {
	return evidence.ErrProviderUnavailable.Error() + ": " + e.Err.Error()
}

func (e *FetchError) Unwrap() []error {
	return []error{evidence.ErrProviderUnavailable, e.Err}
}

// FetchFailure reports err as a provider collection failure. An error that
// already carries a FetchError keeps its declared kind; any other error is
// classified by Classify.
func FetchFailure(err error) error {
	if _, ok := errors.AsType[*FetchError](err); ok {
		return err
	}
	kind, _ := Classify(err)
	return &FetchError{Kind: kind, Err: err}
}

// Classify returns the cache failure kind and provider retry time for a fetch
// error. Every non-nil error yields a failure kind, so a failed fetch can never
// be recorded as a success.
func Classify(err error) (FailureKind, time.Time) {
	if err == nil {
		return FailureNone, time.Time{}
	}
	if fetchErr, ok := errors.AsType[*FetchError](err); ok && fetchErr.Kind != FailureNone {
		return fetchErr.Kind, fetchErr.RetryAt
	}
	switch {
	case errors.Is(err, evidence.ErrWrongAccount):
		return FailureAccountMismatch, time.Time{}
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return FailureTransient, time.Time{}
	default:
		return FailurePermanent, time.Time{}
	}
}

// ParseRetryAfter returns the time an HTTP Retry-After value names, given in
// delay seconds or as an HTTP date, or the zero time when it names none.
func ParseRetryAfter(value string, now time.Time) time.Time {
	if seconds, err := strconv.ParseUint(value, 10, 32); err == nil {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if date, err := http.ParseTime(value); err == nil {
		return date.UTC()
	}
	return time.Time{}
}
