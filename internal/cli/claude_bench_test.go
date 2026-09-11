package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/claude"
	"github.com/douglasjarquin/remainder/internal/codex"
)

func TestClaudeProcessEntryPoint_readsControlledTLSSources(t *testing.T) {
	// Given
	var profileRequests, usageRequests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/profile":
			profileRequests.Add(1)
		case "/usage":
			usageRequests.Add(1)
		default:
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer synthetic-secret" || r.Header.Get("anthropic-beta") != "oauth-2025-04-20" || r.Header.Get("Accept") != "application/json" || r.Header.Get("ChatGPT-Account-Id") != "" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/profile" {
			fmt.Fprint(w, `{"account":{"uuid":"acct-test"}}`)
			return
		}
		fmt.Fprint(w, `{"limits":[{"group":"session","percent":40}]}`)
	}))
	defer server.Close()
	fixtureDir := t.TempDir()
	metricsPath := filepath.Join(fixtureDir, "metrics.json")
	authPath := filepath.Join(fixtureDir, ".credentials.json")
	if err := os.WriteFile(authPath, []byte(`{"claudeAiOauth":{"accessToken":"synthetic-secret"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	command := issue5HelperCommand(t, server, metricsPath, authPath, writeTestCA(t, server))
	command.Env = append(command.Env,
		"REMAINDER_ISSUE5_PROVIDER=claude",
		"REMAINDER_ISSUE5_PROFILE_ENDPOINT="+server.URL+"/profile",
		"REMAINDER_ISSUE5_USAGE_ENDPOINT="+server.URL+"/usage",
	)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	// When
	err := command.Run()

	// Then
	if err != nil || stdout.String() != "60\n" || stderr.Len() != 0 {
		t.Fatalf("process result: err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	metrics := readIssue5Metrics(t, metricsPath)
	if profileRequests.Load() != 1 || usageRequests.Load() != 1 || metrics.RequestCount != 2 || metrics.ControlledTLSRoundTripNS <= 0 {
		t.Fatalf("profile=%d usage=%d metrics=%+v", profileRequests.Load(), usageRequests.Load(), metrics)
	}
}

func BenchmarkExecuteClaudeControlledRefresh(b *testing.B) {
	// Given
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/profile":
			fmt.Fprint(w, `{"account":{"uuid":"acct-test"}}`)
		case "/usage":
			fmt.Fprint(w, `{"limits":[{"group":"session","percent":40}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	b.Cleanup(server.Close)
	adapter := controlledClaudeRuntimeAdapter(b, server)
	args := []string{"value", "--provider", "claude", "--profile", "default", "--window", "five_hour", "--field", "remaining", "--cache", "off"}
	var stdout, stderr bytes.Buffer
	code := 0
	b.ReportAllocs()
	b.ResetTimer()

	// When
	for b.Loop() {
		stdout.Reset()
		stderr.Reset()
		code = ExecuteWithAdapterAt(b.Context(), args, &stdout, &stderr, "test", fixedCLINow(), adapter)
	}
	b.StopTimer()

	// Then
	b.ReportMetric(float64(requests.Load())/float64(b.N), "requests/op")
	if code != 0 || stdout.String() != "60\n" || stderr.Len() != 0 || requests.Load() != 2*int64(b.N) {
		b.Fatalf("result: code=%d stdout=%q stderr=%q requests=%d iterations=%d", code, stdout.String(), stderr.String(), requests.Load(), b.N)
	}
}

func controlledClaudeRuntimeAdapter(t testing.TB, server *httptest.Server) runtimeAdapter {
	t.Helper()
	fixtureDir := t.TempDir()
	authFile := filepath.Join(fixtureDir, ".credentials.json")
	if err := os.WriteFile(authFile, []byte(`{"claudeAiOauth":{"accessToken":"synthetic-secret"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := claude.New(claude.Options{
		AuthFile:        authFile,
		ProfileEndpoint: server.URL + "/profile",
		UsageEndpoint:   server.URL + "/usage",
		Client:          server.Client(),
		Timeout:         time.Second,
		Now:             fixedCLINow,
	})
	return runtimeAdapter{
		codex:  codex.Default(),
		claude: provider,
		newStore: func() (*cache.Store, error) {
			return cache.New(filepath.Join(fixtureDir, "cache"), cache.Options{Now: fixedCLINow}), nil
		},
	}
}
