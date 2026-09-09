package cursor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

var fixtureNow = time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)

func TestAdapterObserve_normalizesIndependentCursorMeters(t *testing.T) {
	// Given
	server := fixtureServer(t, map[string]fixtureResponse{
		"GetCurrentPeriodUsage": {bodyFile: "usage-happy.json"},
		"GetPlanInfo":           {bodyFile: "plan-paid.json"},
		"GetSandUsageStatus":    {bodyFile: "sand-weekly.json"},
	})
	defer server.Close()
	adapter := testAdapter(t, server)

	// When
	observation, err := adapter.Observe(t.Context(), cursorRequest())

	// Then
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observation.Account != (evidence.AccountIdentity{Binding: evidence.IdentityUnknown}) {
		t.Fatalf("account = %+v, want unknown and empty", observation.Account)
	}
	if observation.Outcome != evidence.OutcomeComplete || len(observation.Windows) != 5 {
		t.Fatalf("outcome/windows = %s/%d, want complete/5", observation.Outcome, len(observation.Windows))
	}
	assertValue(t, observation, "included_usage", evidence.Field("used"), "25\n")
	assertValue(t, observation, "auto_usage", evidence.FieldRemaining, "50\n")
	assertValue(t, observation, "api_usage", evidence.Field("used"), "0\n")
	assertValue(t, observation, "spend_limit", evidence.Field("limit"), "2000\n")
	assertValue(t, observation, "spend_limit", evidence.FieldRemaining, "1250\n")
	assertValue(t, observation, "spend_limit", evidence.Field("used"), "750\n")
	assertValue(t, observation, "grok_bot", evidence.FieldRemaining, "60\n")
	if windowByID(t, observation, "spend_limit").Unit != "usd_cents" {
		t.Fatalf("spend unit = %q, want source-preserving usd_cents", windowByID(t, observation, "spend_limit").Unit)
	}
	included := windowByID(t, observation, "included_usage")
	reset := limitByField(t, included, evidence.FieldReset)
	duration := limitByField(t, included, evidence.FieldDuration)
	if reset.ResetAt == nil || reset.ResetAt.Format(time.RFC3339) != "2026-09-15T00:00:00Z" || duration.Duration == nil || *duration.Duration != 31*24*time.Hour {
		t.Fatalf("cycle reset/duration = %v/%v", reset.ResetAt, duration.Duration)
	}
}

func TestAdapterObserve_preservesZeroAndUnknownPlanLimits(t *testing.T) {
	tests := []struct {
		name        string
		fixture     string
		window      evidence.WindowID
		field       evidence.Field
		wantState   evidence.ValueState
		wantWindows int
	}{
		{name: "zero is observed", fixture: "usage-zero.json", window: "included_usage", field: evidence.Field("used"), wantState: evidence.ValueZero, wantWindows: 3},
		{name: "missing auto is unknown", fixture: "usage-missing.json", window: "auto_usage", field: evidence.Field("used"), wantState: evidence.ValueUnknown, wantWindows: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			server := fixtureServer(t, map[string]fixtureResponse{
				"GetCurrentPeriodUsage": {bodyFile: test.fixture},
				"GetPlanInfo":           {body: `{}`},
				"GetSandUsageStatus":    {body: `{}`},
			})
			defer server.Close()

			// When
			observation, err := testAdapter(t, server).Observe(t.Context(), cursorRequest())

			// Then
			if err != nil || len(observation.Windows) != test.wantWindows {
				t.Fatalf("Observe() = %d windows, %v", len(observation.Windows), err)
			}
			limit := limitByField(t, windowByID(t, observation, test.window), test.field)
			if limit.Value.State != test.wantState {
				t.Fatalf("state = %s, want %s", limit.Value.State, test.wantState)
			}
		})
	}
}

func TestAdapterObserve_reportsSupplementalFailureAsPartial(t *testing.T) {
	// Given
	server := fixtureServer(t, map[string]fixtureResponse{
		"GetCurrentPeriodUsage": {bodyFile: "usage-zero.json"},
		"GetPlanInfo":           {status: http.StatusBadGateway, body: `synthetic-secret`},
		"GetSandUsageStatus":    {body: `{}`},
	})
	defer server.Close()

	// When
	observation, err := testAdapter(t, server).Observe(t.Context(), cursorRequest())

	// Then
	if err != nil || observation.Outcome != evidence.OutcomePartial || len(observation.Failures) != 1 {
		t.Fatalf("Observe() = %+v, %v, want explicit partial", observation, err)
	}
	if observation.Failures[0].Scope != "plan" || strings.Contains(observation.Failures[0].Message, "synthetic-secret") {
		t.Fatalf("failure = %+v, want redacted plan failure", observation.Failures[0])
	}
}

func TestAdapterObserve_rejectsInvalidSelectionsBeforeAccess(t *testing.T) {
	tests := []struct {
		name    string
		request evidence.Request
	}{
		{name: "wrong profile", request: evidence.Request{Provider: "cursor", Profile: "other"}},
		{name: "explicit account unavailable", request: evidence.Request{Provider: "cursor", Profile: "default", Account: "someone@example.test"}},
		{name: "all sources unsupported", request: evidence.Request{Provider: "cursor", Profile: "default", All: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			transport := &countingTransport{}
			adapter := New(Options{AuthFile: filepath.Join(t.TempDir(), "missing"), client: &http.Client{Transport: transport}, goos: "linux"})

			// When
			_, err := adapter.Observe(t.Context(), test.request)

			// Then
			if err == nil || transport.calls != 0 {
				t.Fatalf("Observe() error/calls = %v/%d", err, transport.calls)
			}
			if test.request.Account != "" && !errors.Is(err, evidence.ErrWrongAccount) {
				t.Fatalf("error = %v, want wrong-account refusal", err)
			}
		})
	}
}

func TestAdapterObserve_boundsAndClassifiesFailures(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantKind   cache.FailureKind
		wantRetry  time.Time
		wantPhrase string
	}{
		{name: "revoked", status: http.StatusUnauthorized, wantKind: cache.FailureRevoked, wantPhrase: "rejected"},
		{name: "forbidden unknown cause", status: http.StatusForbidden, wantKind: cache.FailurePermanent, wantPhrase: "forbidden"},
		{name: "rate limited", status: http.StatusTooManyRequests, wantKind: cache.FailureTransient, wantRetry: fixtureNow.Add(30 * time.Second), wantPhrase: "rate limited"},
		{name: "oversized", status: http.StatusOK, body: strings.Repeat("x", maxResponseBytes+1), wantKind: cache.FailurePermanent, wantPhrase: "too large"},
		{name: "schema drift", status: http.StatusOK, body: `{"newUsage":{}}`, wantKind: cache.FailurePermanent, wantPhrase: "schema"},
		{name: "secret bearing malformed body", status: http.StatusOK, body: `synthetic-secret`, wantKind: cache.FailurePermanent, wantPhrase: "malformed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != rpcPath("GetCurrentPeriodUsage") {
					t.Fatalf("unexpected supplemental call after required failure: %s", r.URL.Path)
				}
				if test.status == http.StatusTooManyRequests {
					w.Header().Set("Retry-After", "30")
				}
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()

			// When
			_, err := testAdapter(t, server).Observe(t.Context(), cursorRequest())
			kind, retryAt := Failure(err)

			// Then
			if err == nil || kind != test.wantKind || !retryAt.Equal(test.wantRetry) || !strings.Contains(err.Error(), test.wantPhrase) || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatalf("failure = %v/%s/%s", err, kind, retryAt)
			}
		})
	}
}

func TestAdapterObserve_refusesRedirectAndHonorsCancellation(t *testing.T) {
	t.Run("redirect", func(t *testing.T) {
		// Given
		leaked := false
		destination := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { leaked = r.Header.Get("Authorization") != "" }))
		defer destination.Close()
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
		defer source.Close()

		// When
		_, err := testAdapter(t, source).Observe(t.Context(), cursorRequest())

		// Then
		if err == nil || leaked {
			t.Fatalf("Observe() error/leaked = %v/%v", err, leaked)
		}
	})
	t.Run("canceled", func(t *testing.T) {
		// Given
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		// When
		_, err := testAdapter(t, nil).Observe(ctx, cursorRequest())

		// Then
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want canceled", err)
		}
	})
	t.Run("total timeout", func(t *testing.T) {
		// Given
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
		defer server.Close()
		adapter := New(Options{AuthFile: writeAuth(t, "synthetic-secret"), endpoint: server.URL, client: server.Client(), timeout: 20 * time.Millisecond, now: func() time.Time { return fixtureNow }, goos: "linux"})

		// When
		_, err := adapter.Observe(t.Context(), cursorRequest())
		close(release)

		// Then
		kind, _ := Failure(err)
		if !errors.Is(err, context.DeadlineExceeded) || kind != cache.FailureTransient {
			t.Fatalf("error/kind = %v/%s, want bounded transient deadline", err, kind)
		}
	})
}

func TestAdapterObserve_sendsBoundedDashboardRPCContract(t *testing.T) {
	// Given
	seen := make(map[string]bool)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if r.Method != http.MethodPost || string(body) != `{}` || r.Header.Get("Authorization") != "Bearer synthetic-secret" || r.Header.Get("Accept") != "application/json" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Connect-Protocol-Version") != "1" {
			t.Fatalf("request contract = %s %q %#v", r.Method, body, r.Header)
		}
		seen[r.URL.Path] = true
		if r.URL.Path == rpcPath("GetCurrentPeriodUsage") {
			fmt.Fprint(w, fixture(t, "usage-zero.json"))
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()

	// When
	_, err := testAdapter(t, server).Observe(t.Context(), cursorRequest())

	// Then
	if err != nil || len(seen) != 3 {
		t.Fatalf("Observe() error/paths = %v/%v", err, seen)
	}
}
