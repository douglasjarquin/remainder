package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/codex"
	"github.com/douglasjarquin/remainder/internal/devin"
)

func TestExecute_native_Devin_supportsCompactJSONAndScalar(t *testing.T) {
	now := fixedCLINow()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeDevinCLIStatus(w)
	}))
	defer server.Close()
	adapter := devinRuntimeAdapter(t, server, now)
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "compact", args: []string{"--provider=devin", "--profile=default", "--cache=off"}, want: `provider="devin" profile="default"`},
		{name: "JSON flattened values", args: []string{"--provider=devin", "--profile=default", "--cache=off", "--format=json"}, want: `"id":"weekly_remaining","field":"remaining","state":"defined","amount":60`},
		{name: "scalar", args: []string{"value", "--provider=devin", "--profile=default", "--window=weekly", "--field=remaining", "--cache=off"}, want: "60\n"},
		{name: "scalar reset", args: []string{"value", "--provider=devin", "--profile=default", "--window=daily", "--field=reset", "--cache=off"}, want: "2026-09-22T08:00:00Z\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := executeWithAdapterAt(t.Context(), test.args, &stdout, &stderr, "test", now, adapter)

			if code != 0 || !strings.Contains(stdout.String(), test.want) || stderr.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestExecute_native_Devin_preservesZeroAndUnknownValues(t *testing.T) {
	now := fixedCLINow()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"userStatus":{"userId":"user-test","planStatus":{"dailyQuotaRemainingPercent":0,"dailyQuotaResetAtUnix":"1790064000","weeklyQuotaResetAtUnix":"1790496000"}}}`)
	}))
	defer server.Close()
	adapter := devinRuntimeAdapter(t, server, now)
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--provider=devin", "--profile=default", "--cache=off", "--format=json"}, &stdout, &stderr, "test", now, adapter)

	if code != 3 || !strings.Contains(stdout.String(), `"id":"daily_remaining","field":"remaining","state":"zero","amount":0`) || !strings.Contains(stdout.String(), `"id":"weekly_remaining","field":"remaining","state":"unknown"`) || stderr.String() != "remainder: partial evidence\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRuntimeAdapter_Devin_cacheHitPreservesTimestampAndVerifiedIdentity(t *testing.T) {
	now := fixedCLINow()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeDevinCLIStatus(w)
	}))
	defer server.Close()
	adapter := devinRuntimeAdapter(t, server, now)
	store := cache.New(filepath.Join(t.TempDir(), "cache"), cache.Options{Now: func() time.Time { return now }})
	adapter.newStore = func() (*cache.Store, error) { return store, nil }
	var firstOut, secondOut, stderr bytes.Buffer
	firstCode := executeWithAdapterAt(t.Context(), []string{"--provider=devin", "--profile=default", "--format=json"}, &firstOut, &stderr, "test", now, adapter)
	secondCode := executeWithAdapterAt(t.Context(), []string{"--provider=devin", "--profile=default", "--cache=only", "--max-age=1m", "--format=json"}, &secondOut, &stderr, "test", now, adapter)

	wantTime := `"observed_at":"2026-09-09T16:00:00Z"`
	wantIdentity := `"account":{"last_observed":"user-test","binding":"historical"}`
	if firstCode != 0 || secondCode != 0 || !strings.Contains(firstOut.String(), wantTime) || !strings.Contains(secondOut.String(), wantTime) || !strings.Contains(secondOut.String(), wantIdentity) || requests.Load() != 1 || stderr.Len() != 0 {
		t.Fatalf("codes=%d/%d first=%q second=%q requests=%d stderr=%q", firstCode, secondCode, firstOut.String(), secondOut.String(), requests.Load(), stderr.String())
	}
}

func TestRuntimeAdapter_Devin_accountMismatchFails(t *testing.T) {
	now := fixedCLINow()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeDevinCLIStatus(w)
	}))
	defer server.Close()
	adapter := devinRuntimeAdapter(t, server, now)
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--provider=devin", "--profile=default", "--account=user-other", "--cache=off"}, &stdout, &stderr, "test", now, adapter)

	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), devin.ErrAccountMismatch.Error()) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRuntimeAdapter_Devin_401RevokesAnd429BacksOff(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "401 revokes", status: http.StatusUnauthorized},
		{name: "connect unauthenticated revokes", status: http.StatusBadRequest, body: `{"code":"unauthenticated","message":"bad key"}`},
		{name: "429 backs off", status: http.StatusTooManyRequests},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := fixedCLINow()
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				if test.status == http.StatusTooManyRequests {
					w.Header().Set("Retry-After", "30")
				}
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			adapter := devinRuntimeAdapter(t, server, now)
			store := cache.New(filepath.Join(t.TempDir(), "cache"), cache.Options{Now: func() time.Time { return now }})
			adapter.newStore = func() (*cache.Store, error) { return store, nil }
			var stdout, stderr bytes.Buffer
			firstCode := executeWithAdapterAt(t.Context(), []string{"--provider=devin", "--profile=default"}, &stdout, &stderr, "test", now, adapter)
			stdout.Reset()
			stderr.Reset()
			secondArgs := []string{"--provider=devin", "--profile=default", "--cache=only", "--max-age=1m"}
			if test.status == http.StatusTooManyRequests {
				secondArgs = []string{"--provider=devin", "--profile=default"}
			}
			secondCode := executeWithAdapterAt(t.Context(), secondArgs, &stdout, &stderr, "test", now, adapter)

			if firstCode != 1 || secondCode != 1 || stdout.Len() != 0 || stderr.Len() == 0 || requests.Load() != 1 {
				t.Fatalf("codes=%d/%d stdout=%q stderr=%q requests=%d", firstCode, secondCode, stdout.String(), stderr.String(), requests.Load())
			}
		})
	}
}

func TestExecute_native_Devin_missingFileIsOperationalFailure(t *testing.T) {
	adapter := runtimeAdapter{
		codex: codex.Default(),
		devin: devin.New(devin.Options{AuthFile: filepath.Join(t.TempDir(), "missing")}),
		newStore: func() (*cache.Store, error) {
			t.Fatal("cache initialized")
			return nil, nil
		},
	}
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--provider=devin", "--profile=default", "--cache=off"}, &stdout, &stderr, "test", fixedCLINow(), adapter)

	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "credential file is missing") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestExecute_native_Devin_providerSelection(t *testing.T) {
	now := fixedCLINow()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeDevinCLIStatus(w)
	}))
	defer server.Close()
	adapter := devinRuntimeAdapter(t, server, now)
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--provider=devin", "--profile=default", "--format=json", "--cache=off"}, &stdout, &stderr, "test", now, adapter)

	if code != 0 || requests.Load() != 1 || !strings.Contains(stdout.String(), `"provider":"devin"`) || !strings.Contains(stdout.String(), `"source":{"kind":"native_file_http","name":"devin_credentials_toml"}`) {
		t.Fatalf("code=%d requests=%d stdout=%q stderr=%q", code, requests.Load(), stdout.String(), stderr.String())
	}
}

func devinRuntimeAdapter(t *testing.T, server *httptest.Server, now time.Time) runtimeAdapter {
	t.Helper()
	authFile := filepath.Join(t.TempDir(), "credentials.toml")
	if err := os.WriteFile(authFile, []byte(`windsurf_api_key = "synthetic-devin-key"`+"\n"+`api_server_url = "https://server.codeium.com"`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := devin.New(devin.Options{AuthFile: authFile, Endpoint: server.URL, Client: server.Client(), Timeout: time.Second, Now: func() time.Time { return now }})
	return runtimeAdapter{codex: codex.Default(), devin: provider, newStore: func() (*cache.Store, error) {
		return cache.New(filepath.Join(t.TempDir(), "cache"), cache.Options{Now: func() time.Time { return now }}), nil
	}}
}

func writeDevinCLIStatus(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"userStatus":{"userId":"user-test","email":"user@example.com","teamId":"devin-team$account-test","teamsTier":"TEAMS_TIER_DEVIN_PRO","planStatus":{"planStart":"2026-09-17T13:57:49Z","planEnd":"2026-10-17T13:57:49Z","planInfo":{"planName":"Pro","billingStrategy":"BILLING_STRATEGY_QUOTA","monthlyPromptCredits":-1},"dailyQuotaRemainingPercent":80,"dailyQuotaResetAtUnix":"1790064000","weeklyQuotaRemainingPercent":60,"weeklyQuotaResetAtUnix":"1790496000","availablePromptCredits":-1}},"planInfo":{"planName":"Pro"}}`)
}
