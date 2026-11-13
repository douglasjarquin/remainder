package cache

import "time"

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
