package evidence_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestObservation_RenderersPreserveTypedFacts(t *testing.T) {
	reset := time.Date(2026, time.March, 8, 8, 0, 0, 0, time.UTC)
	amount := json.Number("42")
	zero := json.Number("0")
	obs := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex\nprod",
		Profile:       "main",
		Account: evidence.AccountIdentity{
			LastObserved: "acct-1",
			Binding:      evidence.IdentityHistorical,
		},
		Source:     evidence.SourceIdentity{Kind: "fixture", Name: "local"},
		ObservedAt: time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:  evidence.FreshFresh,
		Outcome:    evidence.OutcomeComplete,
		Windows: []evidence.Window{
			{ID: "weekly", Scope: evidence.ScopeModel, Unit: "tokens", Limits: []evidence.Limit{{ID: "weekly-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}, ResetAt: &reset}}},
			{ID: "daily", Scope: evidence.ScopeAccount, Unit: "tokens", Limits: []evidence.Limit{{ID: "daily-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueZero, Amount: &zero}}}},
		},
	}

	compact, err := evidence.RenderCompact(obs, time.Date(2026, time.March, 8, 7, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("RenderCompact() error = %v", err)
	}
	wantCompact := `schema=v1 provider="codex\nprod" profile="main" observed_at=2026-03-08T07:00:00Z age_seconds=1800 freshness=fresh outcome=complete identity=historical account="acct-1" source="fixture/local" windows=daily/account:remaining=0tokens;weekly/model:remaining=42tokens reset=2026-03-08T08:00:00Z`
	if compact != wantCompact {
		t.Fatalf("RenderCompact() = %q, want %q", compact, wantCompact)
	}

	jsonOne, err := evidence.RenderJSON(obs)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	jsonTwo, err := evidence.RenderJSON(obs)
	if err != nil {
		t.Fatalf("second RenderJSON() error = %v", err)
	}
	if string(jsonOne) != string(jsonTwo) {
		t.Fatalf("RenderJSON() is not deterministic: %q != %q", jsonOne, jsonTwo)
	}
	var decoded map[string]any
	if err := json.Unmarshal(jsonOne, &decoded); err != nil {
		t.Fatalf("RenderJSON() produced invalid JSON: %v", err)
	}
	if decoded["schema_version"] != "v1" || decoded["provider"] != "codex\nprod" {
		t.Fatalf("RenderJSON() lost typed identity: %#v", decoded)
	}
	if !bytes.Contains(jsonOne, []byte(`"codex\nprod"`)) {
		t.Fatalf("RenderJSON() did not escape control characters: %q", jsonOne)
	}
}

func TestObservation_RenderCompactEscapesControlCharactersInStructuralFields(t *testing.T) {
	amount := json.Number("1")
	obs := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows:       []evidence.Window{{ID: "week\nly", Scope: evidence.Scope("model\t scope"), Unit: "tok\tens", Limits: []evidence.Limit{{ID: "remaining", Field: evidence.Field("remain\ning"), Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}}}},
	}
	got, err := evidence.RenderCompact(obs, obs.ObservedAt)
	if err != nil {
		t.Fatalf("RenderCompact() error = %v", err)
	}
	if strings.ContainsAny(got, "\r\n\t") {
		t.Fatalf("RenderCompact() contains raw control characters: %q", got)
	}
}

func TestObservation_SelectValueRequiresExactEligibleSelection(t *testing.T) {
	amount := json.Number("42")
	obs := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		Account:       evidence.AccountIdentity{LastObserved: "acct-1", Binding: evidence.IdentityHistorical},
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows:       []evidence.Window{{ID: "weekly", Scope: evidence.ScopeModel, Unit: "tokens", Limits: []evidence.Limit{{ID: "weekly-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}}}},
	}

	got, err := evidence.SelectValue(obs, evidence.ValueRequest{Provider: "codex", Profile: "main", Window: "weekly", Field: evidence.FieldRemaining, Account: "acct-1"}, evidence.FreshOnly)
	if err != nil {
		t.Fatalf("SelectValue() error = %v", err)
	}
	if got != "42\n" {
		t.Fatalf("SelectValue() = %q, want %q", got, "42\n")
	}

	obs.Freshness = evidence.FreshStale
	if _, err := evidence.SelectValue(obs, evidence.ValueRequest{Provider: "codex", Profile: "main", Window: "weekly", Field: evidence.FieldRemaining, Account: "acct-1"}, evidence.FreshOnly); !errors.Is(err, evidence.ErrStale) {
		t.Fatalf("stale SelectValue() error = %v, want ErrStale", err)
	}
	if _, err := evidence.SelectValue(obs, evidence.ValueRequest{Provider: "codex", Profile: "main", Window: "weekly", Field: evidence.FieldRemaining, Account: "other"}, evidence.FreshAny); !errors.Is(err, evidence.ErrWrongAccount) {
		t.Fatalf("wrong-account SelectValue() error = %v, want ErrWrongAccount", err)
	}
	obs.Freshness = evidence.FreshFresh
	obs.Windows = append(obs.Windows, evidence.Window{ID: "daily", Scope: evidence.ScopeAccount, Limits: []evidence.Limit{{ID: "daily-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}}})
	if _, err := evidence.SelectValue(obs, evidence.ValueRequest{Provider: "codex", Profile: "main", Field: evidence.FieldRemaining, Account: "acct-1"}, evidence.FreshAny); !errors.Is(err, evidence.ErrAmbiguous) {
		t.Fatalf("ambiguous SelectValue() error = %v, want ErrAmbiguous", err)
	}
}

func TestObservation_ValidateRejectsMalformedAndDuplicateEvidence(t *testing.T) {
	duplicate := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows:       []evidence.Window{{ID: "weekly", Limits: []evidence.Limit{{ID: "same", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueUnknown}}, {ID: "same", Field: evidence.FieldReset, Value: evidence.Value{State: evidence.ValueUnknown}}}}},
	}
	if err := duplicate.Validate(); !errors.Is(err, evidence.ErrDuplicateID) {
		t.Fatalf("duplicate Validate() error = %v, want ErrDuplicateID", err)
	}
	if _, err := evidence.ParseJSON([]byte(`{"schema_version":"v1","observed_at":"not-a-time"}`)); !errors.Is(err, evidence.ErrMalformedTime) {
		t.Fatalf("malformed ParseJSON() error = %v, want ErrMalformedTime", err)
	}
}

func TestObservation_SelectValuePreservesZeroUnlimitedAndUnknown(t *testing.T) {
	zero := json.Number("0")
	for _, test := range []struct {
		name  string
		state evidence.ValueState
		value *json.Number
		want  string
		err   error
	}{
		{name: "zero", state: evidence.ValueZero, value: &zero, want: "0\n"},
		{name: "unlimited", state: evidence.ValueUnlimited, want: "unlimited\n"},
		{name: "unknown", state: evidence.ValueUnknown, err: evidence.ErrUndefined},
	} {
		t.Run(test.name, func(t *testing.T) {
			obs := evidence.Observation{
				SchemaVersion: evidence.SchemaV1,
				Provider:      "codex",
				Profile:       "main",
				ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
				Freshness:     evidence.FreshFresh,
				Outcome:       evidence.OutcomeComplete,
				Windows:       []evidence.Window{{ID: "weekly", Limits: []evidence.Limit{{ID: "remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: test.state, Amount: test.value}}}}},
			}
			got, err := evidence.SelectValue(obs, evidence.ValueRequest{Provider: "codex", Profile: "main", Window: "weekly", Field: evidence.FieldRemaining}, evidence.FreshAny)
			if test.err != nil {
				if !errors.Is(err, test.err) {
					t.Fatalf("SelectValue() error = %v, want %v", err, test.err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("SelectValue() = %q, %v, want %q", got, err, test.want)
			}
		})
	}
}

func TestObservation_SelectValueReturnsResetAndDurationFields(t *testing.T) {
	reset := time.Date(2026, time.March, 8, 8, 0, 0, 0, time.UTC)
	duration := 7 * 24 * time.Hour
	obs := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows: []evidence.Window{{ID: "weekly", Limits: []evidence.Limit{
			{ID: "reset", Field: evidence.FieldReset, Value: evidence.Value{State: evidence.ValueUnknown}, ResetAt: &reset},
			{ID: "duration", Field: evidence.FieldDuration, Value: evidence.Value{State: evidence.ValueUnknown}, Duration: &duration},
		}}},
	}
	for _, test := range []struct {
		field evidence.Field
		want  string
	}{
		{field: evidence.FieldReset, want: "2026-03-08T08:00:00Z\n"},
		{field: evidence.FieldDuration, want: "168h0m0s\n"},
	} {
		got, err := evidence.SelectValue(obs, evidence.ValueRequest{Provider: "codex", Profile: "main", Window: "weekly", Field: test.field}, evidence.FreshAny)
		if err != nil || got != test.want {
			t.Fatalf("SelectValue(%q) = %q, %v, want %q", test.field, got, err, test.want)
		}
	}
}
