package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

type issue3FixtureAdapter struct {
	observation evidence.Observation
	calls       int
}

func TestExecuteWithAdapter_CompactFiltersRequestedScopeAndReportsAge(t *testing.T) {
	amount := evidence.JSONNumber("42")
	adapter := &issue3FixtureAdapter{observation: evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		Account:       evidence.AccountIdentity{LastObserved: "acct-1", Binding: evidence.IdentityHistorical},
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows: []evidence.Window{
			{ID: "daily", Scope: evidence.ScopeAccount, Unit: "tokens", Limits: []evidence.Limit{{ID: "daily-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}}},
			{ID: "weekly", Scope: evidence.ScopeModel, Unit: "tokens", Limits: []evidence.Limit{{ID: "weekly-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}}},
		},
	}}
	var stdout, stderr bytes.Buffer
	if code := executeWithAdapter(context.Background(), []string{"--window", "weekly"}, &stdout, &stderr, "v0.1.0", adapter); code != 0 {
		t.Fatalf("compact exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "daily/account") {
		t.Fatalf("compact stdout = %q, want only requested weekly scope", stdout.String())
	}
	if strings.Contains(stdout.String(), "age_seconds=0") {
		t.Fatalf("compact stdout = %q, want elapsed observation age", stdout.String())
	}
}

func (a *issue3FixtureAdapter) Observe(context.Context, evidence.Request) (evidence.Observation, error) {
	a.calls++
	return a.observation, nil
}

func TestExecuteWithAdapter_RendersJSONAndValueThroughFreshCobraTrees(t *testing.T) {
	amount := evidence.JSONNumber("42")
	adapter := &issue3FixtureAdapter{observation: evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		Account:       evidence.AccountIdentity{LastObserved: "acct-1", Binding: evidence.IdentityHistorical},
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows:       []evidence.Window{{ID: "weekly", Scope: evidence.ScopeModel, Unit: "tokens", Limits: []evidence.Limit{{ID: "weekly-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}}}},
	}}

	var jsonOut, jsonErr bytes.Buffer
	if code := executeWithAdapter(context.Background(), []string{"--format", "json"}, &jsonOut, &jsonErr, "v0.1.0", adapter); code != 0 {
		t.Fatalf("JSON exit code = %d, want 0; stderr = %q", code, jsonErr.String())
	}
	if jsonErr.Len() != 0 || !bytes.Contains(jsonOut.Bytes(), []byte(`"schema_version":"v1"`)) {
		t.Fatalf("JSON output = %q, stderr = %q", jsonOut.String(), jsonErr.String())
	}

	jsonOut.Reset()
	jsonErr.Reset()
	if code := executeWithAdapter(context.Background(), []string{"--format", "json"}, &jsonOut, &jsonErr, "v0.1.0", adapter); code != 0 {
		t.Fatalf("second JSON exit code = %d, want 0; stderr = %q", code, jsonErr.String())
	}
	if adapter.calls != 2 {
		t.Fatalf("adapter calls = %d, want one call per fresh command", adapter.calls)
	}

	var valueOut, valueErr bytes.Buffer
	if code := executeWithAdapter(context.Background(), []string{"value", "--provider", "codex", "--profile", "main", "--window", "weekly", "--field", "remaining", "--account", "acct-1"}, &valueOut, &valueErr, "v0.1.0", adapter); code != 0 {
		t.Fatalf("value exit code = %d, want 0; stderr = %q", code, valueErr.String())
	}
	if valueOut.String() != "42\n" || valueErr.Len() != 0 {
		t.Fatalf("value stdout = %q, stderr = %q, want exact scalar and empty stderr", valueOut.String(), valueErr.String())
	}
}

func TestExecuteWithAdapter_RejectsWrongAccountAcrossOutputFormats(t *testing.T) {
	amount := evidence.JSONNumber("42")
	adapter := &issue3FixtureAdapter{observation: evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		Account:       evidence.AccountIdentity{LastObserved: "other-account", Binding: evidence.IdentityHistorical},
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows:       []evidence.Window{{ID: "weekly", Scope: evidence.ScopeModel, Unit: "tokens", Limits: []evidence.Limit{{ID: "weekly-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}}}},
	}}

	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "compact", args: []string{"--account", "wanted-account"}},
		{name: "json", args: []string{"--format", "json", "--account", "wanted-account"}},
		{name: "scalar", args: []string{"value", "--provider", "codex", "--profile", "main", "--window", "weekly", "--field", "remaining", "--account", "wanted-account"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := executeWithAdapter(context.Background(), test.args, &stdout, &stderr, "v0.1.0", adapter)
			if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "account") {
				t.Fatalf("wrong-account %s result: code=%d stdout=%q stderr=%q", test.name, code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestExecuteWithAdapter_AllHonorsExplicitWindow(t *testing.T) {
	amount := evidence.JSONNumber("42")
	adapter := &issue3FixtureAdapter{observation: evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows: []evidence.Window{
			{ID: "daily", Scope: evidence.ScopeAccount, Unit: "tokens", Limits: []evidence.Limit{{ID: "daily-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}}},
			{ID: "weekly", Scope: evidence.ScopeModel, Unit: "tokens", Limits: []evidence.Limit{{ID: "weekly-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}}},
		},
	}}
	var stdout, stderr bytes.Buffer
	code := executeWithAdapter(context.Background(), []string{"--all", "--window", "weekly"}, &stdout, &stderr, "v0.1.0", adapter)
	if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "weekly/model") || strings.Contains(stdout.String(), "daily/account") {
		t.Fatalf("--all --window weekly result: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestExecuteWithAdapter_HelpAndInvalidFlagsDoNotObserve(t *testing.T) {
	adapter := &issue3FixtureAdapter{}
	var helpOut, helpErr bytes.Buffer
	if code := executeWithAdapter(context.Background(), []string{"--help"}, &helpOut, &helpErr, "v0.1.0", adapter); code != 0 {
		t.Fatalf("help exit code = %d, want 0", code)
	}
	if adapter.calls != 0 || helpErr.Len() != 0 || !bytes.Contains(helpOut.Bytes(), []byte("value")) {
		t.Fatalf("help stdout = %q, stderr = %q, adapter calls = %d", helpOut.String(), helpErr.String(), adapter.calls)
	}

	helpOut.Reset()
	helpErr.Reset()
	if code := executeWithAdapter(context.Background(), []string{"--freshness", "ignored"}, &helpOut, &helpErr, "v0.1.0", adapter); code != 2 {
		t.Fatalf("invalid freshness exit code = %d, want 2", code)
	}
	if adapter.calls != 0 || helpOut.Len() != 0 || bytes.Contains(helpErr.Bytes(), []byte("Usage:")) {
		t.Fatalf("invalid freshness stdout = %q, stderr = %q, adapter calls = %d", helpOut.String(), helpErr.String(), adapter.calls)
	}
}

func TestExecuteWithAdapter_PartialResultHasDataAndDistinctExit(t *testing.T) {
	amount := evidence.JSONNumber("0")
	adapter := &issue3FixtureAdapter{observation: evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		Account:       evidence.AccountIdentity{LastObserved: "acct-1", Binding: evidence.IdentityHistorical},
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomePartial,
		Windows:       []evidence.Window{{ID: "weekly", Scope: evidence.ScopeModel, Unit: "tokens", Limits: []evidence.Limit{{ID: "weekly-remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueZero, Amount: &amount}}}}},
		Failures:      []evidence.Failure{{Scope: "daily", Message: "source unavailable"}},
	}}
	var stdout, stderr bytes.Buffer
	code := executeWithAdapter(context.Background(), []string{"--format", "json"}, &stdout, &stderr, "v0.1.0", adapter)
	if code != 3 {
		t.Fatalf("partial exit code = %d, want 3", code)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"outcome":"partial"`)) || !bytes.Contains(stdout.Bytes(), []byte(`"failures"`)) {
		t.Fatalf("partial stdout = %q, want JSON result and failures", stdout.String())
	}
	if stderr.String() != "remainder: partial evidence\n" {
		t.Fatalf("partial stderr = %q, want exact diagnostic", stderr.String())
	}
}

func TestExecuteWithAdapter_FreshnessPolicyRejectsStaleReport(t *testing.T) {
	adapter := &issue3FixtureAdapter{observation: evidence.Observation{
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
	code := executeWithAdapter(context.Background(), []string{"--freshness", "fresh"}, &stdout, &stderr, "v0.1.0", adapter)
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "stale") {
		t.Fatalf("stale report code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}
