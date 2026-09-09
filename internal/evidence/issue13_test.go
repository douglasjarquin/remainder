package evidence_test

import (
	"errors"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestObservation_SelectValueRejectsMissingLimit(t *testing.T) {
	// Given
	observation := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "default",
		ObservedAt:    time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows:       []evidence.Window{{ID: "weekly", Scope: evidence.ScopeAccount, Unit: "percent"}},
	}

	// When
	_, err := evidence.SelectValue(observation, evidence.ValueRequest{Provider: "codex", Profile: "default", Window: "weekly", Field: evidence.FieldRemaining}, evidence.FreshOnly)

	// Then
	if !errors.Is(err, evidence.ErrNoMatch) {
		t.Fatalf("SelectValue() error = %v, want ErrNoMatch", err)
	}
}
