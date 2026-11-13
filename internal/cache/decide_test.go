package cache

import (
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestDecide(t *testing.T) {
	now := time.Date(2026, time.November, 12, 16, 0, 0, 0, time.UTC)
	pending := now.Add(30 * time.Second)
	due := now
	auto := Policy{Mode: ModeAuto, MaxAge: 5 * time.Second}
	stale := Policy{Mode: ModeAuto, MaxAge: 5 * time.Second, StaleOnError: true}
	only := Policy{Mode: ModeOnly, MaxAge: 5 * time.Second}
	tests := []struct {
		name       string
		loaded     loadedRecord
		account    string
		policy     Policy
		hitAllowed bool
		want       decision
	}{
		{"missing record refetches", loadedRecord{state: recordMissing}, "", auto, true, decisionRefetch},
		{"corrupt record refetches", loadedRecord{state: recordCorrupt}, "", stale, true, decisionRefetch},
		{"unknown schema refetches", loadedRecord{state: recordUnknown}, "", auto, true, decisionRefetch},
		{"unreadable record refetches", loadedRecord{state: recordUnavailable}, "", auto, true, decisionRefetch},
		{"missing record misses cache only", loadedRecord{state: recordMissing}, "", only, true, decisionCacheOnlyMiss},
		{"corrupt record misses cache only", loadedRecord{state: recordCorrupt}, "", only, true, decisionCacheOnlyMiss},
		{"unknown schema misses cache only", loadedRecord{state: recordUnknown}, "", only, true, decisionCacheOnlyMiss},

		{"hit within max-age is served", supported(observed(now, -time.Second)), "", auto, true, decisionServeCached},
		{"hit exactly at max-age is served", supported(observed(now, -5*time.Second)), "", auto, true, decisionServeCached},
		{"hit one nanosecond past max-age refetches", supported(observed(now, -5*time.Second-time.Nanosecond)), "", auto, true, decisionRefetch},
		{"expired hit misses cache only", supported(observed(now, -time.Minute)), "", only, true, decisionCacheOnlyMiss},
		{"hit is served under cache only", supported(observed(now, -time.Second)), "", only, true, decisionServeCached},
		{"hit is not served when hits are not allowed", supported(observed(now, -time.Second)), "", auto, false, decisionRefetch},
		{"matching account is served", supported(observed(now, -time.Second)), "acct-test", auto, true, decisionServeCached},
		{"other account refetches", supported(observed(now, -time.Second)), "acct-other", auto, true, decisionRefetch},
		{"record without observation refetches", supported(record{}), "", auto, true, decisionRefetch},

		{"hit in back-off is served", supported(failed(observed(now, -time.Second), FailureTransient, &pending)), "", auto, true, decisionServeCached},
		{"hit in back-off is served after the lock", supported(failed(observed(now, -time.Second), FailureTransient, &pending)), "", stale, true, decisionServeCached},
		{"expired record in back-off is backed off", supported(failed(observed(now, -time.Minute), FailureTransient, &pending)), "", auto, true, decisionBackedOff},
		{"expired record in back-off is served stale", supported(failed(observed(now, -time.Minute), FailureTransient, &pending)), "", stale, true, decisionServeStale},
		{"back-off for another account is backed off", supported(failed(observed(now, -time.Minute), FailureTransient, &pending)), "acct-other", stale, true, decisionBackedOff},
		{"back-off without observation is backed off", supported(failed(record{}, FailureTransient, &pending)), "", stale, true, decisionBackedOff},
		{"due retry refetches", supported(failed(observed(now, -time.Minute), FailureTransient, &due)), "", stale, true, decisionRefetch},
		{"permanent failure refetches", supported(failed(observed(now, -time.Minute), FailurePermanent, nil)), "", stale, true, decisionRefetch},
		{"failed fetch with stale allowed is served stale", supported(failed(observed(now, -time.Second), FailureTransient, &pending)), "", stale, false, decisionServeStale},
		{"failed fetch without stale is backed off", supported(failed(observed(now, -time.Second), FailureTransient, &pending)), "", auto, false, decisionBackedOff},

		{"revoked hit is not served", supported(revoked(failed(observed(now, -time.Second), FailureRevoked, nil))), "", auto, true, decisionRefetch},
		{"revoked hit misses cache only", supported(revoked(failed(observed(now, -time.Second), FailureRevoked, nil))), "", only, true, decisionCacheOnlyMiss},
		{"revoked record in back-off is never served stale", supported(revoked(failed(observed(now, -time.Second), FailureTransient, &pending))), "", stale, true, decisionBackedOff},

		{"future observation refetches", supported(observed(now, time.Second)), "", stale, true, decisionRefetch},
		{"future observation misses cache only", supported(observed(now, time.Second)), "", only, true, decisionCacheOnlyMiss},
		{"future observation in back-off is backed off", supported(failed(observed(now, time.Second), FailureTransient, &pending)), "", auto, true, decisionBackedOff},
		{"future observation in back-off is not served stale", supported(failed(observed(now, time.Second), FailureTransient, &pending)), "", stale, true, decisionBackedOff},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decide(tt.loaded, tt.account, tt.policy, now, tt.hitAllowed); got != tt.want {
				t.Fatalf("decide() = %d, want %d", got, tt.want)
			}
		})
	}
}

func supported(value record) loadedRecord {
	return loadedRecord{state: recordSupported, record: value}
}

func observed(now time.Time, offset time.Duration) record {
	return record{Generation: "00000000000000000000000000000000", Observation: &evidence.Observation{ObservedAt: now.Add(offset), Account: evidence.AccountIdentity{LastObserved: "acct-test"}}}
}

func failed(value record, kind FailureKind, retryAt *time.Time) record {
	value.LastFailure = kind
	value.RetryAt = retryAt
	return value
}

func revoked(value record) record {
	value.Revoked = true
	return value
}
