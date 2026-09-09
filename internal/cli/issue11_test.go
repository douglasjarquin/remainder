package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/codex"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestExecuteWithCodexAdapter_calculatesPaceAfterControlledCollection(t *testing.T) {
	// Given
	requestStarted := make(chan struct{})
	releaseResponse := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseResponse
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"secondary_window":{"used_percent":58,"limit_window_seconds":604800,"reset_after_seconds":604800}}}`)
	}))
	defer server.Close()
	adapter := codex.New(codex.Options{AuthFile: writeCLIAuth(t), Endpoints: []string{server.URL}, Client: server.Client(), Timeout: time.Second})
	var stdout, stderr bytes.Buffer
	result := make(chan int, 1)
	var workers sync.WaitGroup
	workers.Go(func() {
		result <- ExecuteWithAdapter(t.Context(), []string{"--provider", "codex", "--profile", "default", "--format", "json"}, &stdout, &stderr, "test", adapter)
	})
	<-requestStarted

	// When
	close(releaseResponse)
	code := <-result
	workers.Wait()
	observation, err := evidence.ParseJSON(stdout.Bytes())

	// Then
	if code != 0 || err != nil || stderr.Len() != 0 || len(observation.Windows) != 2 {
		t.Fatalf("result: code=%d parseErr=%v stderr=%q windows=%d", code, err, stderr.String(), len(observation.Windows))
	}
	pace := observation.Windows[1].Pace
	if pace == nil || pace.CalculatedAt.Before(pace.Inputs.ObservedAt) {
		t.Fatalf("result: code=%d parseErr=%v stderr=%q pace=%#v", code, err, stderr.String(), pace)
	}
}

func TestExecuteWithAdapterAt_marksFutureObservationPaceUnknown(t *testing.T) {
	// Given
	evaluatedAt := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)
	observedAt := evaluatedAt.Add(time.Minute)
	resetAt := observedAt.Add(7 * 24 * time.Hour)
	duration := 7 * 24 * time.Hour
	remaining := evidence.JSONNumber("42")
	adapter := &fixtureAdapter{observation: evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "default",
		Account:       evidence.AccountIdentity{LastObserved: "acct-test", Binding: evidence.IdentityVerified},
		Source:        evidence.SourceIdentity{Kind: "fixture", Name: "future-observation"},
		ObservedAt:    observedAt,
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows: []evidence.Window{{
			ID: "weekly", Scope: evidence.ScopeAccount, Unit: "percent",
			Limits: []evidence.Limit{
				{ID: "weekly_remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &remaining}},
				{ID: "weekly_reset", Field: evidence.FieldReset, Value: evidence.Value{State: evidence.ValueUnknown}, ResetAt: &resetAt},
				{ID: "weekly_duration", Field: evidence.FieldDuration, Value: evidence.Value{State: evidence.ValueUnknown}, Duration: &duration},
			},
		}},
	}}
	var stdout, stderr bytes.Buffer

	// When
	code := ExecuteWithAdapterAt(t.Context(), []string{"--provider", "codex", "--profile", "default", "--format", "json"}, &stdout, &stderr, "test", evaluatedAt, adapter)
	observation, err := evidence.ParseJSON(stdout.Bytes())

	// Then
	pace := observation.Windows[0].Pace
	if code != 0 || err != nil || stderr.Len() != 0 || pace == nil || pace.Status != evidence.PaceUnknown || pace.Reason != "future_observation" {
		t.Fatalf("result: code=%d parseErr=%v stderr=%q pace=%#v", code, err, stderr.String(), pace)
	}
}

func TestStore_CachedOnly_supportsRemainingAndPaceFromOneObservation(t *testing.T) {
	// Given
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	authPath := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(authPath, []byte("malformed OAuth fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := codex.New(codex.Options{AuthFile: authPath, Endpoints: []string{"http://127.0.0.1:1"}, Timeout: 50 * time.Millisecond, Now: func() time.Time { return now }})
	binding, err := provider.CacheBinding(t.Context(), evidence.Request{Provider: "codex", Profile: "default"})
	if err != nil {
		t.Fatal(err)
	}
	store := cache.New(filepath.Join(t.TempDir(), "remainder", "v1"), cache.Options{Now: func() time.Time { return now }})
	observation := cacheObservation(now.Add(-time.Second))
	resetAt := observation.ObservedAt.Add(7 * 24 * time.Hour)
	duration := 7 * 24 * time.Hour
	observation.Windows[1].Limits = append(observation.Windows[1].Limits,
		evidence.Limit{ID: "weekly_reset", Field: evidence.FieldReset, Value: evidence.Value{State: evidence.ValueUnknown}, ResetAt: &resetAt},
		evidence.Limit{ID: "weekly_duration", Field: evidence.FieldDuration, Value: evidence.Value{State: evidence.ValueUnknown}, Duration: &duration},
	)
	if _, err := store.Put(t.Context(), binding, observation); err != nil {
		t.Fatal(err)
	}
	adapter := runtimeAdapter{codex: provider, newStore: func() (*cache.Store, error) { return store, nil }}

	// When
	var remainingOut, paceOut, stderr bytes.Buffer
	remainingCode := executeWithAdapterAt(t.Context(), []string{"value", "--cache=only", "--provider=codex", "--profile=default", "--window=weekly", "--field=remaining"}, &remainingOut, &stderr, "test", now, adapter)
	paceCode := executeWithAdapterAt(t.Context(), []string{"value", "--cache=only", "--provider=codex", "--profile=default", "--window=weekly", "--field=pace"}, &paceOut, &stderr, "test", now, adapter)

	// Then
	if remainingCode != 0 || paceCode != 0 || remainingOut.String() != "80\n" || paceOut.String() != "ahead\n" || stderr.Len() != 0 {
		t.Fatalf("codes = %d/%d, remaining = %q, pace = %q, stderr = %q", remainingCode, paceCode, remainingOut.String(), paceOut.String(), stderr.String())
	}
}

func TestCodexPaceProcessEntryPoint_emitsWeeklyPaceFromControlledSource(t *testing.T) {
	// Given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"secondary_window":{"used_percent":58,"limit_window_seconds":604800,"reset_after_seconds":604800}}}`)
	}))
	defer server.Close()
	command := exec.Command(os.Args[0], "-test.run=^TestIssue11HelperProcess$")
	command.Env = []string{"REMAINDER_ISSUE11_HELPER=1", "REMAINDER_ISSUE11_AUTH=" + writeCLIAuth(t), "REMAINDER_ISSUE11_ENDPOINT=" + server.URL}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	// When
	err := command.Run()

	// Then
	if err != nil || stdout.String() != "ahead\n" || stderr.Len() != 0 {
		t.Fatalf("process result: err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
}

func TestIssue11HelperProcess(t *testing.T) {
	if os.Getenv("REMAINDER_ISSUE11_HELPER") != "1" {
		return
	}
	adapter := codex.New(codex.Options{AuthFile: os.Getenv("REMAINDER_ISSUE11_AUTH"), Endpoints: []string{os.Getenv("REMAINDER_ISSUE11_ENDPOINT")}, Timeout: time.Second})
	code := ExecuteWithAdapter(context.Background(), []string{"value", "--provider", "codex", "--profile", "default", "--window", "weekly", "--field", "pace"}, os.Stdout, os.Stderr, "test", adapter)
	os.Exit(code)
}
