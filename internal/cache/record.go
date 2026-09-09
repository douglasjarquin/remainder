package cache

import (
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

type record struct {
	SchemaVersion string                `json:"schema_version"`
	BindingHash   string                `json:"binding"`
	Generation    string                `json:"generation,omitempty"`
	Observation   *evidence.Observation `json:"observation,omitempty"`
	LastAttemptAt *time.Time            `json:"last_attempt_at,omitempty"`
	RetryAt       *time.Time            `json:"retry_at,omitempty"`
	LastFailure   FailureKind           `json:"last_failure,omitempty"`
	Revoked       bool                  `json:"revoked,omitzero"`
}

type recordState uint8

const (
	recordMissing recordState = iota
	recordSupported
	recordCorrupt
	recordUnknown
	recordUnavailable
)

type loadedRecord struct {
	record record
	state  recordState
	err    error
}
