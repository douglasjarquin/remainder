package cache

import (
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func (s *Store) retryDeadline(now, providerRetryAt time.Time) time.Time {
	local := now.Add(s.options.LocalBackoff)
	if !providerRetryAt.After(now) {
		return local
	}
	maximum := now.Add(s.options.MaxBackoff)
	if providerRetryAt.After(maximum) {
		return maximum
	}
	return providerRetryAt
}

func (s *Store) backoffResult(loaded loadedRecord, expectedAccount string, policy Policy) (Result, error, bool) {
	if loaded.state != recordSupported || loaded.record.LastFailure != FailureTransient || loaded.record.RetryAt == nil || !s.options.Now().Before(*loaded.record.RetryAt) {
		return Result{}, nil, false
	}
	if policy.StaleOnError && loaded.record.Observation != nil && (expectedAccount == "" || loaded.record.Observation.Account.LastObserved == expectedAccount) {
		observation := *loaded.record.Observation
		observation.Account.Binding = evidence.IdentityHistorical
		observation.Freshness = evidence.FreshStale
		return Result{Observation: observation, Generation: loaded.record.Generation, FromCache: true}, nil, true
	}
	return Result{}, &BackoffError{RetryAt: *loaded.record.RetryAt}, true
}
