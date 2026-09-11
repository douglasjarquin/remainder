package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/go-toon"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestExecuteWithAdapter_FormatTOONRendersHealthyCodexWeekly(t *testing.T) {
	amount := evidence.JSONNumber("42")
	reset := time.Date(2026, time.March, 8, 8, 0, 0, 0, time.UTC)
	obs := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		Account:       evidence.AccountIdentity{LastObserved: "acct-1", Binding: evidence.IdentityHistorical},
		Source:        evidence.SourceIdentity{Kind: "fixture", Name: "local"},
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows: []evidence.Window{{
			ID: "weekly", Scope: evidence.ScopeModel, Unit: "tokens",
			Limits: []evidence.Limit{{ID: "weekly-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}, ResetAt: &reset}},
		}},
	}
	assertTOONReport(t, obs, []string{"--format", "toon", "--window", "weekly"}, 0, "")
}

func TestExecuteWithAdapter_FormatTOONRendersZeroExhausted(t *testing.T) {
	zero := evidence.JSONNumber("0")
	obs := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		Account:       evidence.AccountIdentity{LastObserved: "acct-1", Binding: evidence.IdentityHistorical},
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows: []evidence.Window{{
			ID: "weekly", Scope: evidence.ScopeModel, Unit: "tokens",
			Limits: []evidence.Limit{{ID: "weekly-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueZero, Amount: &zero}}},
		}},
	}
	assertTOONReport(t, obs, []string{"--format", "toon"}, 0, "")
}

func TestExecuteWithAdapter_FormatTOONRendersUnknown(t *testing.T) {
	obs := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		Account:       evidence.AccountIdentity{LastObserved: "acct-1", Binding: evidence.IdentityHistorical},
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows: []evidence.Window{{
			ID: "weekly", Scope: evidence.ScopeModel, Unit: "tokens",
			Limits: []evidence.Limit{{ID: "weekly-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueUnknown}}},
		}},
	}
	assertTOONReport(t, obs, []string{"--format", "toon"}, 0, "")
}

func TestExecuteWithAdapter_FormatTOONRejectsStaleWhenFreshRequired(t *testing.T) {
	adapter := &fixtureAdapter{observation: evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		Account:       evidence.AccountIdentity{LastObserved: "acct-1", Binding: evidence.IdentityHistorical},
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshStale,
		Outcome:       evidence.OutcomeComplete,
		Windows:       []evidence.Window{{ID: "weekly", Scope: evidence.ScopeModel, Unit: "tokens", Limits: []evidence.Limit{{ID: "weekly-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueUnknown}}}}},
	}}
	var stdout, stderr bytes.Buffer
	code := executeWithAdapter(context.Background(), []string{"--format", "toon", "--freshness", "fresh"}, &stdout, &stderr, "v0.1.0", adapter)
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "stale") {
		t.Fatalf("stale toon code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if adapter.calls != 1 {
		t.Fatalf("adapter calls = %d, want 1", adapter.calls)
	}
}

func TestExecuteWithAdapter_FormatTOONAllRendersPartialDocument(t *testing.T) {
	amount := evidence.JSONNumber("42")
	obs := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		Account:       evidence.AccountIdentity{LastObserved: "acct-1", Binding: evidence.IdentityHistorical},
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomePartial,
		Windows: []evidence.Window{
			{ID: "weekly", Scope: evidence.ScopeModel, Unit: "tokens", Limits: []evidence.Limit{{ID: "weekly-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}}},
			{ID: "daily", Scope: evidence.ScopeAccount, Unit: "tokens", Limits: []evidence.Limit{{ID: "daily-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueUnknown}}}},
		},
		Failures: []evidence.Failure{{Scope: "claude", Message: "source unavailable"}},
	}
	assertTOONReport(t, obs, []string{"--all", "--format", "toon"}, 3, "remainder: partial evidence\n")
}

func TestExecuteWithAdapter_InvalidFormatDoesNotObserve(t *testing.T) {
	adapter := &fixtureAdapter{}
	var stdout, stderr bytes.Buffer
	code := executeWithAdapter(context.Background(), []string{"--format", "yaml"}, &stdout, &stderr, "v0.1.0", adapter)
	if code != 2 || stdout.Len() != 0 || adapter.calls != 0 {
		t.Fatalf("invalid format code=%d stdout=%q calls=%d stderr=%q", code, stdout.String(), adapter.calls, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unsupported format") || bytes.Contains(stderr.Bytes(), []byte("Usage:")) {
		t.Fatalf("invalid format stderr = %q", stderr.String())
	}
}

func TestExecuteWithAdapter_HelpListsTOON(t *testing.T) {
	adapter := &fixtureAdapter{}
	var stdout, stderr bytes.Buffer
	if code := executeWithAdapter(context.Background(), []string{"--help"}, &stdout, &stderr, "v0.1.0", adapter); code != 0 {
		t.Fatalf("help exit code = %d", code)
	}
	if adapter.calls != 0 || stderr.Len() != 0 || !bytes.Contains(stdout.Bytes(), []byte("toon")) {
		t.Fatalf("help stdout = %q, stderr = %q, calls = %d", stdout.String(), stderr.String(), adapter.calls)
	}
}

func TestExecuteWithAdapter_ValueRejectsTOON(t *testing.T) {
	adapter := &fixtureAdapter{}
	var stdout, stderr bytes.Buffer
	code := executeWithAdapter(context.Background(), []string{"value", "--format", "toon", "--provider", "codex", "--profile", "main", "--window", "weekly", "--field", "remaining"}, &stdout, &stderr, "v0.1.0", adapter)
	if code != 2 || stdout.Len() != 0 || adapter.calls != 0 {
		t.Fatalf("value toon code=%d stdout=%q calls=%d stderr=%q", code, stdout.String(), adapter.calls, stderr.String())
	}
	if !strings.Contains(stderr.String(), "value does not support format") || bytes.Contains(stderr.Bytes(), []byte("Usage:")) {
		t.Fatalf("value toon stderr = %q", stderr.String())
	}
}

func assertTOONReport(t *testing.T, obs evidence.Observation, args []string, wantCode int, wantStderr string) {
	t.Helper()
	adapter := &fixtureAdapter{observation: obs}
	var stdout, stderr bytes.Buffer
	code := executeWithAdapter(context.Background(), args, &stdout, &stderr, "v0.1.0", adapter)
	if code != wantCode {
		t.Fatalf("exit code = %d, want %d; stderr = %q", code, wantCode, stderr.String())
	}
	if stderr.String() != wantStderr {
		t.Fatalf("stderr = %q, want %q", stderr.String(), wantStderr)
	}
	if !bytes.HasSuffix(stdout.Bytes(), []byte("\n")) {
		t.Fatalf("TOON stdout missing trailing newline: %q", stdout.String())
	}
	doc := bytes.TrimSuffix(stdout.Bytes(), []byte("\n"))
	jsonOut, err := evidence.RenderJSON(obs)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	if !bytes.Equal(cliDecodeJSONFacts(t, jsonOut), cliDecodeTOONFacts(t, doc)) {
		t.Fatalf("TOON facts = %s, want JSON facts %s\nTOON document:\n%s", cliDecodeTOONFacts(t, doc), cliDecodeJSONFacts(t, jsonOut), doc)
	}
}

func cliDecodeJSONFacts(t *testing.T, data []byte) []byte {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var facts any
	if err := decoder.Decode(&facts); err != nil {
		t.Fatalf("decode JSON facts: %v", err)
	}
	encoded, err := json.Marshal(cliCanonicalizeFacts(facts))
	if err != nil {
		t.Fatalf("marshal JSON facts: %v", err)
	}
	return encoded
}

func cliDecodeTOONFacts(t *testing.T, data []byte) []byte {
	t.Helper()
	facts, err := toon.Decode(data)
	if err != nil {
		t.Fatalf("decode TOON facts: %v", err)
	}
	encoded, err := json.Marshal(cliCanonicalizeFacts(facts))
	if err != nil {
		t.Fatalf("marshal TOON facts: %v", err)
	}
	return encoded
}

func cliCanonicalizeFacts(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = cliCanonicalizeFacts(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cliCanonicalizeFacts(item)
		}
		return out
	case json.Number:
		return cliCanonicalizeNumber(typed.String())
	case float64:
		return cliCanonicalizeNumber(strconv.FormatFloat(typed, 'f', -1, 64))
	case nil:
		return []any{}
	default:
		return typed
	}
}

func cliCanonicalizeNumber(raw string) string {
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return raw
	}
	return strconv.FormatFloat(parsed, 'f', -1, 64)
}
