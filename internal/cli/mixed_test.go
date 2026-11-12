package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

type mixedFixtureAdapter struct {
	results       []collectionResult
	requests      []evidence.Request
	calls         int
	cursorFailure error
	devinFailure  error
}

func (a *mixedFixtureAdapter) Observe(context.Context, evidence.Request) (evidence.Observation, error) {
	panic("single observation called for --all")
}

func (a *mixedFixtureAdapter) ObserveAll(_ context.Context, requests []evidence.Request, _ cache.Policy) []collectionResult {
	a.calls++
	a.requests = append(a.requests, requests...)
	results := append([]collectionResult(nil), a.results...)
	for _, request := range requests {
		var failure error
		switch request.Provider {
		case "cursor":
			failure = a.cursorFailure
		case "devin":
			failure = a.devinFailure
		default:
			continue
		}
		result := collectionResult{Request: request, Result: cache.Result{Observation: providerObservation(fixedCLINow(), request.Provider)}}
		if failure != nil {
			result.Err = failure
		}
		results = append(results, result)
	}
	return results
}

func TestExecuteAll_rejectsExplicitSelectorsBeforeCollection(t *testing.T) {
	for _, args := range [][]string{
		{"--all", "--provider="},
		{"--all", "--profile="},
		{"--all", "--account="},
	} {
		adapter := &mixedFixtureAdapter{}
		var stdout, stderr bytes.Buffer

		code := executeWithAdapterAt(t.Context(), args, &stdout, &stderr, "test", fixedCLINow(), adapter)

		if code != 2 || adapter.calls != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--all cannot use") {
			t.Fatalf("args=%q code=%d calls=%d stdout=%q stderr=%q", args, code, adapter.calls, stdout.String(), stderr.String())
		}
	}
}

func TestExecuteAll_JSON_preservesProviderOrderAndSingleObservationWire(t *testing.T) {
	now := fixedCLINow()
	codexObservation := providerObservation(now.Add(-time.Second), "codex")
	duration := time.Hour
	codexObservation.Windows[0].Limits = append(codexObservation.Windows[0].Limits, evidence.Limit{ID: "five_hour_duration", Field: evidence.FieldDuration, Value: evidence.Value{State: evidence.ValueUnknown}, Duration: &duration})
	claudeObservation := providerObservation(now.Add(-2*time.Second), "claude")
	adapter := &mixedFixtureAdapter{results: []collectionResult{
		{Request: evidence.Request{Provider: "grok", Profile: "default"}, Err: errors.New("private path and token must be redacted")},
		{Request: evidence.Request{Provider: "claude", Profile: "default"}, Result: cache.Result{Observation: claudeObservation}},
		{Request: evidence.Request{Provider: "codex", Profile: "default"}, Result: cache.Result{Observation: codexObservation}},
	}}
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--all", "--cache=off", "--format=json"}, &stdout, &stderr, "test", now, adapter)

	output := stdout.String()
	if code != 3 || stderr.String() != "remainder: partial evidence\n" || !strings.HasSuffix(output, "\n") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, output, stderr.String())
	}
	if strings.Index(output, `"provider":"codex"`) >= strings.Index(output, `"provider":"claude"`) || strings.Contains(output, "private path") || !strings.Contains(output, `"amount":80`) || !strings.Contains(output, `"duration":"1h0m0s"`) || strings.Contains(output, `"value":`) {
		t.Fatalf("mixed JSON did not preserve order, redaction, or flattened observation wire: %q", output)
	}
	wantFailure := `"failures":[{"provider":"grok","profile":"default","message":"quota is unavailable"}]`
	if !strings.Contains(output, wantFailure) {
		t.Fatalf("stdout=%q, want %q", output, wantFailure)
	}
	if len(adapter.requests) != len(allRequests) {
		t.Fatalf("requests=%+v", adapter.requests)
	}
	for index, request := range adapter.requests {
		if request.Provider != allRequests[index].Provider || request.Profile != allRequests[index].Profile {
			t.Fatalf("requests=%+v", adapter.requests)
		}
	}
}

func TestExecuteAll_compactQuotesCompleteEntriesOnOneLine(t *testing.T) {
	now := fixedCLINow()
	adapter := &mixedFixtureAdapter{results: []collectionResult{
		{Request: evidence.Request{Provider: "grok", Profile: "default"}, Result: cache.Result{Observation: providerObservation(now, "grok")}},
		{Request: evidence.Request{Provider: "codex", Profile: "default"}, Result: cache.Result{Observation: providerObservation(now, "codex")}},
		{Request: evidence.Request{Provider: "claude", Profile: "default"}, Result: cache.Result{Observation: providerObservation(now, "claude")}},
	}}
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--all", "--cache=off"}, &stdout, &stderr, "test", now, adapter)

	output := stdout.String()
	if code != 0 || stderr.Len() != 0 || strings.Count(output, "\n") != 1 || !strings.HasPrefix(output, `schema=v1 outcome=complete observations="schema=v1 provider=\"codex\"`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, output, stderr.String())
	}
	if strings.Index(output, `provider=\"codex\"`) >= strings.Index(output, `provider=\"claude\"`) || strings.Index(output, `provider=\"claude\"`) >= strings.Index(output, `provider=\"grok\"`) {
		t.Fatalf("provider order=%q", output)
	}
}

func TestExecuteAll_appliesWindowAndScopePerProvider(t *testing.T) {
	now := fixedCLINow()
	claudeObservation := providerObservation(now, "claude")
	claudeObservation.Windows = claudeObservation.Windows[:1]
	adapter := &mixedFixtureAdapter{results: []collectionResult{
		{Request: evidence.Request{Provider: "codex", Profile: "default"}, Result: cache.Result{Observation: providerObservation(now, "codex")}},
		{Request: evidence.Request{Provider: "claude", Profile: "default"}, Result: cache.Result{Observation: claudeObservation}},
		{Request: evidence.Request{Provider: "grok", Profile: "default"}, Result: cache.Result{Observation: providerObservation(now, "grok")}},
	}}
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--all", "--cache=off", "--window=weekly", "--scope=account", "--format=json"}, &stdout, &stderr, "test", now, adapter)

	if code != 3 || strings.Count(stdout.String(), `"id":"weekly"`) != len(allRequests)-1 || !strings.Contains(stdout.String(), `"provider":"claude","profile":"default","message":"requested quota evidence is unavailable"`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, request := range adapter.requests {
		if request.Window != "weekly" || request.Scope != evidence.ScopeAccount {
			t.Fatalf("request=%+v", request)
		}
	}
}

func TestExecuteAll_partialObservationReturnsThreeWithoutInventingScopedFailure(t *testing.T) {
	now := fixedCLINow()
	grokObservation := providerObservation(now, "grok")
	grokObservation.Outcome = evidence.OutcomePartial
	grokObservation.Failures = []evidence.Failure{{Scope: "product:voice", Message: "source field unavailable"}}
	adapter := &mixedFixtureAdapter{results: []collectionResult{
		{Request: evidence.Request{Provider: "codex", Profile: "default"}, Result: cache.Result{Observation: providerObservation(now, "codex")}},
		{Request: evidence.Request{Provider: "claude", Profile: "default"}, Result: cache.Result{Observation: providerObservation(now, "claude")}},
		{Request: evidence.Request{Provider: "grok", Profile: "default"}, Result: cache.Result{Observation: grokObservation}},
	}}
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--all", "--cache=off", "--format=json"}, &stdout, &stderr, "test", now, adapter)

	if code != 3 || !strings.Contains(stdout.String(), `"outcome":"partial","observations"`) || strings.Contains(stdout.String(), `],"failures":[{"provider"`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestExecuteAll_warningsAreWrittenByParentInProviderOrder(t *testing.T) {
	now := fixedCLINow()
	adapter := &mixedFixtureAdapter{results: []collectionResult{
		{Request: evidence.Request{Provider: "grok", Profile: "default"}, Result: cache.Result{Observation: providerObservation(now, "grok"), Warning: cache.Warning("grok warning")}},
		{Request: evidence.Request{Provider: "claude", Profile: "default"}, Result: cache.Result{Observation: providerObservation(now, "claude"), Warning: cache.Warning("claude warning")}},
		{Request: evidence.Request{Provider: "codex", Profile: "default"}, Result: cache.Result{Observation: providerObservation(now, "codex"), Warning: cache.Warning("codex warning")}},
	}}
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--all", "--cache=off"}, &stdout, &stderr, "test", now, adapter)

	want := "remainder: warning: codex/default: codex warning\n" +
		"remainder: warning: claude/default: claude warning\n" +
		"remainder: warning: grok/default: grok warning\n"
	if code != 0 || stderr.String() != want {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestExecuteAll_preservesInjectedSingleObservationAdapter(t *testing.T) {
	adapter := &fixtureAdapter{observation: providerObservation(fixedCLINow(), "codex")}
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--all"}, &stdout, &stderr, "test", fixedCLINow(), adapter)

	if code != 0 || adapter.calls != 1 || !strings.Contains(stdout.String(), `provider="codex"`) || stderr.Len() != 0 || len(adapter.requests) != 1 || !adapter.requests[0].All {
		t.Fatalf("code=%d calls=%d stdout=%q stderr=%q", code, adapter.calls, stdout.String(), stderr.String())
	}
}

func TestExecuteAll_noUsableObservationsUsesDeterministicScopedStderr(t *testing.T) {
	adapter := &mixedFixtureAdapter{results: []collectionResult{
		{Request: evidence.Request{Provider: "grok", Profile: "default"}, Err: errors.New("grok secret")},
		{Request: evidence.Request{Provider: "codex", Profile: "default"}, Err: context.DeadlineExceeded},
		{Request: evidence.Request{Provider: "claude", Profile: "default"}, Err: cache.ErrUnavailable},
	}, cursorFailure: errors.New("cursor unavailable"), devinFailure: errors.New("devin unavailable")}
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--all", "--cache=only"}, &stdout, &stderr, "test", fixedCLINow(), adapter)

	want := "remainder: codex/default: quota collection timed out\n" +
		"remainder: claude/default: cached quota evidence is unavailable\n" +
		"remainder: grok/default: quota is unavailable\n" +
		"remainder: devin/default: quota is unavailable\n"
	if len(allRequests) == 5 {
		want += "remainder: cursor/default: quota is unavailable\n"
	}
	if code != 1 || stdout.Len() != 0 || stderr.String() != want {
		t.Fatalf("code=%d stdout=%q stderr=%q want=%q", code, stdout.String(), stderr.String(), want)
	}
}

func TestExecuteAll_userCancellationReturns130WithoutStdout(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	adapter := &mixedFixtureAdapter{}
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(ctx, []string{"--all"}, &stdout, &stderr, "test", fixedCLINow(), adapter)

	if code != 130 || stdout.Len() != 0 || adapter.calls != 0 || stderr.String() != "remainder: interrupted\n" {
		t.Fatalf("code=%d calls=%d stdout=%q stderr=%q", code, adapter.calls, stdout.String(), stderr.String())
	}
}

func TestExecuteAll_reportsCacheHitThatAgesPastMaxAgeDuringCollection(t *testing.T) {
	start := fixedCLINow()
	assembly := start.Add(6 * time.Second)
	hitObservedAt := start.Add(-3 * time.Second)
	for _, test := range []struct {
		name        string
		grokFailure error
		wantCode    int
	}{
		{name: "every provider succeeds", wantCode: 0},
		{name: "one provider fails", grokFailure: cache.ErrUnavailable, wantCode: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			grok := collectionResult{Request: evidence.Request{Provider: "grok", Profile: "default"}, Result: cache.Result{Observation: providerObservation(start, "grok")}}
			if test.grokFailure != nil {
				grok = collectionResult{Request: grok.Request, Err: test.grokFailure}
			}
			adapter := &mixedFixtureAdapter{results: []collectionResult{
				{Request: evidence.Request{Provider: "codex", Profile: "default"}, Result: cache.Result{Observation: providerObservation(hitObservedAt, "codex"), FromCache: true}},
				{Request: evidence.Request{Provider: "claude", Profile: "default"}, Result: cache.Result{Observation: providerObservation(start, "claude")}},
				grok,
			}}
			var stdout, stderr bytes.Buffer

			code := executeWithAdapterClock(t.Context(), []string{"--all", "--max-age=5s", "--format=json"}, &stdout, &stderr, "test", func() time.Time { return assembly }, adapter)

			if code != test.wantCode || !strings.Contains(stdout.String(), `"observations":[{"schema_version":"v1","provider":"codex"`) || !strings.Contains(stdout.String(), `"observed_at":"`+hitObservedAt.Format(time.RFC3339)+`"`) || strings.Contains(stdout.String(), "max-age") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestExecuteAll_futureDatedCacheRecordIsNeverReported(t *testing.T) {
	now := fixedCLINow()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	adapter := failureRuntimeAdapter(t, server, now)
	request := evidence.Request{Provider: "codex", Profile: "default"}
	binding, err := adapter.codex.CacheBinding(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	store, err := adapter.newStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(t.Context(), binding, cacheObservation(now.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--all", "--max-age=1m", "--stale-on-error", "--format=json"}, &stdout, &stderr, "test", now, adapter)

	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "remainder: codex/default: quota is unavailable\n") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestExecuteAll_staleOnErrorResultRemainsSubjectToFreshnessSelection(t *testing.T) {
	now := fixedCLINow()
	stale := providerObservation(now.Add(-time.Hour), "codex")
	stale.Freshness = evidence.FreshStale
	for _, test := range []struct {
		name      string
		freshness string
		wantCodex bool
	}{
		{name: "any retains stale fallback", freshness: "any", wantCodex: true},
		{name: "fresh excludes stale fallback", freshness: "fresh", wantCodex: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := &mixedFixtureAdapter{results: []collectionResult{
				{Request: evidence.Request{Provider: "codex", Profile: "default"}, Result: cache.Result{Observation: stale, FromCache: true}},
				{Request: evidence.Request{Provider: "claude", Profile: "default"}, Result: cache.Result{Observation: providerObservation(now, "claude")}},
				{Request: evidence.Request{Provider: "grok", Profile: "default"}, Result: cache.Result{Observation: providerObservation(now, "grok")}},
			}}
			var stdout, stderr bytes.Buffer

			code := executeWithAdapterAt(t.Context(), []string{"--all", "--max-age=1s", "--stale-on-error", "--freshness=" + test.freshness, "--format=json"}, &stdout, &stderr, "test", now, adapter)

			hasCodex := strings.Contains(stdout.String(), `"observations":[{"schema_version":"v1","provider":"codex"`)
			if hasCodex != test.wantCodex || code != map[bool]int{true: 0, false: 3}[test.wantCodex] {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func providerObservation(observedAt time.Time, provider evidence.Provider) evidence.Observation {
	observation := cacheObservation(observedAt)
	observation.Provider = provider
	observation.Account.LastObserved = "acct-" + string(provider)
	observation.Source.Name = string(provider) + "_auth_json"
	return observation
}
