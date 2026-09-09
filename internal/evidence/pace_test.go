package evidence_test

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestWithPace_classifiesPercentReserveAtThresholds(t *testing.T) {
	observedAt := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)
	resetAt := observedAt.Add(12 * time.Hour)
	duration := 24 * time.Hour
	tests := []struct {
		name      string
		remaining string
		want      evidence.PaceStatus
		reserve   string
	}{
		{name: "below negative threshold is ahead", remaining: "48.9999", want: evidence.PaceAhead, reserve: "-1.0001"},
		{name: "negative threshold tie is on pace", remaining: "49", want: evidence.PaceOnPace, reserve: "-1"},
		{name: "positive threshold tie is on pace", remaining: "51", want: evidence.PaceOnPace, reserve: "1"},
		{name: "above positive threshold is behind", remaining: "51.0001", want: evidence.PaceBehind, reserve: "1.0001"},
		{name: "zero remaining is ahead", remaining: "0", want: evidence.PaceAhead, reserve: "-50"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			observation := paceObservation(observedAt, evidence.FreshFresh, "weekly", evidence.ScopeAccount, percentValue(test.remaining), &resetAt, &duration)

			// When
			got := evidence.WithPace(observation, observedAt)

			// Then
			pace := got.Windows[0].Pace
			if pace == nil || pace.Status != test.want || pace.ReservePercentPoints == nil || pace.ReservePercentPoints.String() != test.reserve {
				t.Fatalf("pace = %#v, want status=%q reserve=%s", pace, test.want, test.reserve)
			}
		})
	}
}

func TestWithPace_reportsUnknownWithoutSufficientCurrentEvidence(t *testing.T) {
	observedAt := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)
	futureReset := observedAt.Add(12 * time.Hour)
	expiredReset := observedAt.Add(time.Hour)
	duration := 24 * time.Hour
	shortDuration := time.Hour
	tests := []struct {
		name        string
		freshness   evidence.Freshness
		remaining   evidence.Value
		reset       *time.Time
		duration    *time.Duration
		evaluatedAt time.Time
		wantReason  string
	}{
		{name: "stale", freshness: evidence.FreshStale, remaining: percentValue("42"), reset: &futureReset, duration: &duration, evaluatedAt: observedAt, wantReason: "stale"},
		{name: "unknown remaining", freshness: evidence.FreshFresh, remaining: evidence.Value{State: evidence.ValueUnknown}, reset: &futureReset, duration: &duration, evaluatedAt: observedAt, wantReason: "missing_usage"},
		{name: "unlimited remaining", freshness: evidence.FreshFresh, remaining: evidence.Value{State: evidence.ValueUnlimited}, reset: &futureReset, duration: &duration, evaluatedAt: observedAt, wantReason: "missing_usage"},
		{name: "missing reset", freshness: evidence.FreshFresh, remaining: percentValue("42"), duration: &duration, evaluatedAt: observedAt, wantReason: "missing_cycle"},
		{name: "missing duration", freshness: evidence.FreshFresh, remaining: percentValue("42"), reset: &futureReset, evaluatedAt: observedAt, wantReason: "missing_cycle"},
		{name: "reset passed since observation", freshness: evidence.FreshFresh, remaining: percentValue("42"), reset: &expiredReset, duration: &duration, evaluatedAt: expiredReset, wantReason: "expired_reset"},
		{name: "future implied cycle", freshness: evidence.FreshFresh, remaining: percentValue("42"), reset: &futureReset, duration: &shortDuration, evaluatedAt: observedAt, wantReason: "future_cycle_start"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			observation := paceObservation(observedAt, test.freshness, "weekly", evidence.ScopeAccount, test.remaining, test.reset, test.duration)

			// When
			got := evidence.WithPace(observation, test.evaluatedAt)

			// Then
			pace := got.Windows[0].Pace
			if pace == nil || pace.Status != evidence.PaceUnknown || pace.Reason != test.wantReason || pace.TimeRemainingPercent != nil || pace.ReservePercentPoints != nil {
				t.Fatalf("pace = %#v, want unknown reason %q", pace, test.wantReason)
			}
		})
	}
}

func TestWithPace_preservesScopedConstraintsAndScalarAmbiguity(t *testing.T) {
	// Given
	observedAt := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)
	resetAt := observedAt.Add(12 * time.Hour)
	duration := 24 * time.Hour
	observation := paceObservation(observedAt, evidence.FreshFresh, "weekly", evidence.ScopeAccount, percentValue("40"), &resetAt, &duration)
	model := paceObservation(observedAt, evidence.FreshFresh, "model_gpt_weekly", evidence.ScopeModel, percentValue("80"), &resetAt, &duration).Windows[0]
	observation.Windows = append(observation.Windows, model)

	// When
	derived := evidence.WithPace(observation, observedAt)
	_, ambiguous := evidence.SelectValue(derived, evidence.ValueRequest{Provider: "codex", Profile: "default", Field: evidence.FieldPace}, evidence.FreshAny)
	selected, selectedErr := evidence.SelectValue(derived, evidence.ValueRequest{Provider: "codex", Profile: "default", Window: "model_gpt_weekly", Scope: evidence.ScopeModel, Field: evidence.FieldPace}, evidence.FreshAny)

	// Then
	if !errors.Is(ambiguous, evidence.ErrAmbiguous) || selectedErr != nil || selected != "behind\n" || len(derived.Windows) != 2 {
		t.Fatalf("ambiguous=%v selected=%q selectedErr=%v windows=%d", ambiguous, selected, selectedErr, len(derived.Windows))
	}
}

func TestWithPace_JSONGoldenPreservesCalculationProvenance(t *testing.T) {
	// Given
	observedAt := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)
	resetAt := observedAt.Add(12 * time.Hour)
	duration := 24 * time.Hour
	derived := evidence.WithPace(paceObservation(observedAt, evidence.FreshFresh, "weekly", evidence.ScopeAccount, percentValue("42"), &resetAt, &duration), observedAt)

	// When
	encoded, err := evidence.RenderJSON(derived)
	parsed, parseErr := evidence.ParseJSON(encoded)

	// Then
	wantPace := `"pace":{"status":"ahead","calculation":"uniform_percent_reserve_v1","calculated_at":"2026-03-08T12:00:00Z","inputs":{"observed_at":"2026-03-08T12:00:00Z","remaining":{"state":"defined","amount":42},"reset_at":"2026-03-09T00:00:00Z","duration":"24h0m0s"},"time_remaining_percent":50,"reserve_percent_points":-8}`
	if err != nil || parseErr != nil || !strings.Contains(string(encoded), wantPace) || parsed.Windows[0].Pace == nil {
		t.Fatalf("RenderJSON=%s err=%v parseErr=%v parsedPace=%#v", encoded, err, parseErr, parsed.Windows[0].Pace)
	}
}

func FuzzWithPace_classificationMatchesReserve(f *testing.F) {
	f.Add(float64(42), int64(12*60*60), int64(24*60*60))
	f.Add(float64(0), int64(1), int64(604800))
	f.Fuzz(func(t *testing.T, remaining float64, remainingSeconds, durationSeconds int64) {
		if math.IsNaN(remaining) || math.IsInf(remaining, 0) || remaining < 0 || remaining > 100 || remainingSeconds <= 0 || durationSeconds <= 0 || remainingSeconds > durationSeconds || durationSeconds > 315360000 {
			t.Skip()
		}
		// Given
		observedAt := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)
		resetAt := observedAt.Add(time.Duration(remainingSeconds) * time.Second)
		duration := time.Duration(durationSeconds) * time.Second
		value := percentValue(strconv.FormatFloat(remaining, 'f', -1, 64))

		// When
		pace := evidence.WithPace(paceObservation(observedAt, evidence.FreshFresh, "weekly", evidence.ScopeAccount, value, &resetAt, &duration), observedAt).Windows[0].Pace

		// Then
		reserve := remaining - 100*float64(remainingSeconds)/float64(durationSeconds)
		want := evidence.PaceOnPace
		if reserve < -1 {
			want = evidence.PaceAhead
		} else if reserve > 1 {
			want = evidence.PaceBehind
		}
		if pace == nil || pace.Status != want {
			t.Fatalf("pace = %#v, want %q for reserve %g", pace, want, reserve)
		}
	})
}

func paceObservation(observedAt time.Time, freshness evidence.Freshness, id evidence.WindowID, scope evidence.Scope, remaining evidence.Value, resetAt *time.Time, duration *time.Duration) evidence.Observation {
	limits := []evidence.Limit{{ID: string(id) + "_remaining", Field: evidence.FieldRemaining, Value: remaining}}
	if resetAt != nil {
		limits = append(limits, evidence.Limit{ID: string(id) + "_reset", Field: evidence.FieldReset, Value: evidence.Value{State: evidence.ValueUnknown}, ResetAt: resetAt})
	}
	if duration != nil {
		limits = append(limits, evidence.Limit{ID: string(id) + "_duration", Field: evidence.FieldDuration, Value: evidence.Value{State: evidence.ValueUnknown}, Duration: duration})
	}
	return evidence.Observation{SchemaVersion: evidence.SchemaV1, Provider: "codex", Profile: "default", Account: evidence.AccountIdentity{LastObserved: "acct-test", Binding: evidence.IdentityVerified}, Source: evidence.SourceIdentity{Kind: "fixture", Name: "pace"}, ObservedAt: observedAt, Freshness: freshness, Outcome: evidence.OutcomeComplete, Windows: []evidence.Window{{ID: id, Scope: scope, Unit: "percent", Limits: limits}}}
}

func percentValue(raw string) evidence.Value {
	number := json.Number(raw)
	state := evidence.ValueDefined
	if raw == "0" {
		state = evidence.ValueZero
	}
	return evidence.Value{State: state, Amount: &number}
}
