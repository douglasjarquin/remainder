package evidence_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/go-toon"
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
	wantJSON := `{"schema_version":"v1","provider":"codex\nprod","profile":"main","account":{"last_observed":"acct-1","binding":"historical"},"source":{"kind":"fixture","name":"local"},"observed_at":"2026-03-08T07:00:00Z","freshness":"fresh","outcome":"complete","windows":[{"id":"daily","scope":"account","unit":"tokens","limits":[{"id":"daily-remaining","field":"remaining","state":"zero","amount":0}]},{"id":"weekly","scope":"model","unit":"tokens","limits":[{"id":"weekly-remaining","field":"remaining","state":"defined","amount":42,"reset_at":"2026-03-08T08:00:00Z"}]}]}`
	if string(jsonOne) != wantJSON {
		t.Fatalf("RenderJSON() = %s, want %s", jsonOne, wantJSON)
	}

	toonOne, err := evidence.RenderTOON(obs)
	if err != nil {
		t.Fatalf("RenderTOON() error = %v", err)
	}
	toonTwo, err := evidence.RenderTOON(obs)
	if err != nil {
		t.Fatalf("second RenderTOON() error = %v", err)
	}
	if string(toonOne) != string(toonTwo) {
		t.Fatalf("RenderTOON() is not deterministic: %q != %q", toonOne, toonTwo)
	}
	wantTOON := "schema_version: v1\nprovider: \"codex\\nprod\"\nprofile: main\naccount:\n  last_observed: acct-1\n  binding: historical\nsource:\n  kind: fixture\n  name: local\nobserved_at: \"2026-03-08T07:00:00Z\"\nfreshness: fresh\noutcome: complete\nwindows[2]:\n  - id: daily\n    scope: account\n    unit: tokens\n    limits[1]{id,field,state,amount}:\n      daily-remaining,remaining,zero,0\n  - id: weekly\n    scope: model\n    unit: tokens\n    limits[1]{id,field,state,amount,reset_at}:\n      weekly-remaining,remaining,defined,42,\"2026-03-08T08:00:00Z\""
	if string(toonOne) != wantTOON {
		t.Fatalf("RenderTOON() = %q, want %q", toonOne, wantTOON)
	}
	if !bytes.Equal(decodeJSONFacts(t, jsonOne), decodeTOONFacts(t, toonOne)) {
		t.Fatalf("TOON facts = %#v, want JSON facts %#v\nTOON document:\n%s", decodeTOONFacts(t, toonOne), decodeJSONFacts(t, jsonOne), toonOne)
	}
}

func TestRenderersRecordIndependentOutputSizes(t *testing.T) {
	obs := mixedObservation()
	now := time.Date(2026, time.March, 8, 7, 30, 0, 0, time.UTC)
	compact, err := evidence.RenderCompact(obs, now)
	if err != nil {
		t.Fatalf("RenderCompact() error = %v", err)
	}
	jsonOut, err := evidence.RenderJSON(obs)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	toonOut, err := evidence.RenderTOON(obs)
	if err != nil {
		t.Fatalf("RenderTOON() error = %v", err)
	}
	wantCompact := `schema=v1 provider="codex\nprod" profile="main" observed_at=2026-03-08T07:00:00Z age_seconds=1800 freshness=fresh outcome=complete identity=historical account="acct-1" source="fixture/local" windows=daily/account:remaining=0tokens;weekly/model:remaining=42tokens reset=2026-03-08T08:00:00Z`
	if compact != wantCompact {
		t.Fatalf("compact output changed: %q", compact)
	}
	if len(jsonOut) != 558 {
		t.Fatalf("json bytes = %d, want existing ceiling 558", len(jsonOut))
	}
	if len(toonOut) != 522 {
		t.Fatalf("toon bytes = %d, want recorded 522", len(toonOut))
	}
	if bytes.Equal(toonOut, jsonOut) {
		t.Fatal("TOON output matched JSON bytes")
	}
	t.Logf("toon_bytes=%d", len(toonOut))
}

func BenchmarkRenderCompact(b *testing.B) {
	obs := mixedObservation()
	now := obs.ObservedAt
	b.ReportAllocs()
	for b.Loop() {
		if _, err := evidence.RenderCompact(obs, now); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderJSON(b *testing.B) {
	obs := mixedObservation()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := evidence.RenderJSON(obs); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderTOON(b *testing.B) {
	obs := mixedObservation()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := evidence.RenderTOON(obs); err != nil {
			b.Fatal(err)
		}
	}
}

func mixedObservation() evidence.Observation {
	reset := time.Date(2026, time.March, 8, 8, 0, 0, 0, time.UTC)
	amount := json.Number("42")
	zero := json.Number("0")
	return evidence.Observation{
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
}

func decodeJSONFacts(t *testing.T, data []byte) []byte {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var facts any
	if err := decoder.Decode(&facts); err != nil {
		t.Fatalf("decode JSON facts: %v", err)
	}
	encoded, err := json.Marshal(canonicalizeFacts(facts))
	if err != nil {
		t.Fatalf("marshal JSON facts: %v", err)
	}
	return encoded
}

func decodeTOONFacts(t *testing.T, data []byte) []byte {
	t.Helper()
	facts, err := toon.Decode(data)
	if err != nil {
		t.Fatalf("decode TOON facts: %v", err)
	}
	encoded, err := json.Marshal(canonicalizeFacts(facts))
	if err != nil {
		t.Fatalf("marshal TOON facts: %v", err)
	}
	return encoded
}

func canonicalizeFacts(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = canonicalizeFacts(item)
		}
		return out
	case []any:
		if typed == nil {
			return []any{}
		}
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = canonicalizeFacts(item)
		}
		return out
	case json.Number:
		return canonicalizeNumber(typed.String())
	case float64:
		return canonicalizeNumber(strconv.FormatFloat(typed, 'f', -1, 64))
	case nil:
		return []any{}
	default:
		return typed
	}
}

func canonicalizeNumber(raw string) string {
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return raw
	}
	return strconv.FormatFloat(parsed, 'f', -1, 64)
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

func TestObservation_RejectsNonzeroAmountForZeroState(t *testing.T) {
	amount := json.Number("42")
	obs := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows: []evidence.Window{{ID: "weekly", Limits: []evidence.Limit{{
			ID: "remaining", Field: evidence.FieldRemaining,
			Value: evidence.Value{State: evidence.ValueZero, Amount: &amount},
		}}}},
	}
	if err := obs.Validate(); !errors.Is(err, evidence.ErrInvalidObservation) {
		t.Fatalf("Validate() error = %v, want ErrInvalidObservation for nonzero zero-state amount", err)
	}
}

func TestObservation_ForRequestHonorsExplicitWindowWithAll(t *testing.T) {
	amount := json.Number("42")
	obs := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows: []evidence.Window{
			{ID: "daily", Limits: []evidence.Limit{{ID: "daily-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}}},
			{ID: "weekly", Limits: []evidence.Limit{{ID: "weekly-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}}},
		},
	}
	filtered, err := obs.ForRequest(evidence.Request{Window: "weekly", All: true})
	if err != nil {
		t.Fatalf("ForRequest() error = %v", err)
	}
	if len(filtered.Windows) != 1 || filtered.Windows[0].ID != "weekly" {
		t.Fatalf("ForRequest() windows = %#v, want only weekly", filtered.Windows)
	}
}

func TestObservation_RenderCompactQuotesStructuralTokens(t *testing.T) {
	amount := json.Number("42")
	obs := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		Account:       evidence.AccountIdentity{LastObserved: "acct-1", Binding: evidence.IdentityBinding("historical\ninjected")},
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows: []evidence.Window{{
			ID: "weekly;injected=1\n", Scope: evidence.Scope("model,scope"), Unit: "tokens,unit",
			Limits: []evidence.Limit{{ID: "remaining", Field: evidence.Field("remaining:field"), Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}},
		}},
	}
	compact, err := evidence.RenderCompact(obs, obs.ObservedAt)
	if err != nil {
		t.Fatalf("RenderCompact() error = %v", err)
	}
	if strings.ContainsAny(compact, "\r\n\t") {
		t.Fatalf("RenderCompact() contains raw control characters: %q", compact)
	}
	for _, token := range []string{`identity="historical\ninjected"`, `"weekly;injected=1\n"`, `"model,scope"`, `"remaining:field"`, `"tokens,unit"`} {
		if !strings.Contains(compact, token) {
			t.Fatalf("RenderCompact() = %q, want quoted structural token %q", compact, token)
		}
	}
}

func TestObservation_RenderCompactQuotesUnicodeLineSeparators(t *testing.T) {
	amount := json.Number("42")
	obs := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		Account:       evidence.AccountIdentity{Binding: evidence.IdentityBinding("historical\u2028injected\u2029")},
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows: []evidence.Window{{
			ID: "weekly\u2028injected\u2029", Scope: evidence.ScopeModel, Unit: "tokens",
			Limits: []evidence.Limit{{ID: "remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}},
		}},
	}
	compact, err := evidence.RenderCompact(obs, obs.ObservedAt)
	if err != nil {
		t.Fatalf("RenderCompact() error = %v", err)
	}
	if strings.ContainsRune(compact, '\u2028') || strings.ContainsRune(compact, '\u2029') {
		t.Fatalf("RenderCompact() contains raw Unicode line separator: %q", compact)
	}
	for _, token := range []string{`identity="historical\u2028injected\u2029"`, `"weekly\u2028injected\u2029"`} {
		if !strings.Contains(compact, token) {
			t.Fatalf("RenderCompact() = %q, want quoted token %q", compact, token)
		}
	}
}

func TestObservation_RejectsInvalidNumericPayload(t *testing.T) {
	amount := json.Number("42;injected")
	obs := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows: []evidence.Window{{ID: "weekly", Limits: []evidence.Limit{{
			ID: "remaining", Field: evidence.FieldRemaining,
			Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount},
		}}}},
	}
	if err := obs.Validate(); !errors.Is(err, evidence.ErrInvalidObservation) {
		t.Fatalf("Validate() error = %v, want ErrInvalidObservation for injected numeric payload", err)
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
