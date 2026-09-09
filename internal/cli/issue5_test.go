package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	json "encoding/json/v2"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/codex"
)

func TestExecuteWithCodexAdapter_rendersEveryOutputMode(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":100,"limit_window_seconds":18000},"secondary_window":{"used_percent":25,"limit_window_seconds":604800}}}`)
	}))
	defer server.Close()
	adapter := codex.New(codex.Options{AuthFile: writeCLIAuth(t), Endpoints: []string{server.URL}, Client: server.Client(), Timeout: time.Second})
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "compact", args: []string{"--provider", "codex", "--profile", "default"}, want: `provider="codex"`},
		{name: "json", args: []string{"--provider", "codex", "--profile", "default", "--format", "json"}, want: `"source":{"kind":"native_file_http","name":"codex_auth_json"}`},
		{name: "scalar", args: []string{"value", "--provider", "codex", "--profile", "default", "--window", "five_hour", "--field", "remaining"}, want: "0\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := ExecuteWithAdapter(t.Context(), test.args, &stdout, &stderr, "v0.1.0", adapter)
			if code != 0 || !strings.Contains(stdout.String(), test.want) || stderr.Len() != 0 {
				t.Fatalf("result: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestCodexProcessEntryPoint_readsControlledTLSSource(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/backend-api/wham/usage" || r.Header.Get("Authorization") != "Bearer synthetic-secret" || r.Header.Get("ChatGPT-Account-Id") != "acct-test" || r.Header.Get("Accept") != "application/json" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":40,"limit_window_seconds":18000}}}`)
	}))
	defer server.Close()
	metricsPath := filepath.Join(t.TempDir(), "metrics.json")
	command := issue5HelperCommand(t, server, metricsPath, writeCLIAuth(t), writeTestCA(t, server))
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil || stdout.String() != "60\n" || stderr.Len() != 0 {
		t.Fatalf("process result: err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	metrics := readIssue5Metrics(t, metricsPath)
	if requests.Load() != 1 || metrics.RequestCount != 1 || metrics.ControlledTLSRoundTripNS <= 0 {
		t.Fatalf("requests = %d, metrics = %+v", requests.Load(), metrics)
	}
}

func TestCodexProcessEntryPoint_rejectsInvalidCA(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	fixtureDir := t.TempDir()
	metricsPath := filepath.Join(fixtureDir, "metrics.json")
	caPath := filepath.Join(fixtureDir, "ca.pem")
	if err := os.WriteFile(caPath, []byte("invalid CA"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := issue5HelperCommand(t, server, metricsPath, writeCLIAuth(t), caPath)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	metrics, _ := os.ReadFile(metricsPath)
	allOutput := stdout.String() + stderr.String() + string(metrics)
	if err == nil || stdout.Len() != 0 || requests.Load() != 0 || strings.Contains(allOutput, "synthetic-secret") {
		t.Fatalf("process result: err=%v stdout=%q stderr=%q requests=%d metrics=%q", err, stdout.String(), stderr.String(), requests.Load(), metrics)
	}
}

func TestExecuteWithCodexAdapter_rendersWeeklyPrimaryWithUnknownShortWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":20,"limit_window_seconds":604800},"secondary_window":null},"additional_rate_limits":[{"metered_feature":"test-model","rate_limit":{"primary_window":{"used_percent":25,"limit_window_seconds":18000},"secondary_window":{"used_percent":50,"limit_window_seconds":604800}}}],"credits":{"balance":"7"}}`)
	}))
	defer server.Close()
	adapter := codex.New(codex.Options{AuthFile: writeCLIAuth(t), Endpoints: []string{server.URL}, Client: server.Client(), Timeout: time.Second})
	var stdout, stderr bytes.Buffer
	code := ExecuteWithAdapter(t.Context(), []string{"value", "--provider", "codex", "--profile", "default", "--window", "weekly", "--field", "remaining"}, &stdout, &stderr, "test", adapter)
	if code != 0 || stdout.String() != "80\n" || stderr.Len() != 0 {
		t.Fatalf("weekly scalar: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	code = ExecuteWithAdapter(t.Context(), []string{"--provider", "codex", "--profile", "default", "--format", "json"}, &stdout, &stderr, "test", adapter)
	if code != 0 || !strings.Contains(stdout.String(), `"id":"five_hour","scope":"account","unit":"percent","limits":[{"id":"five_hour_remaining","field":"remaining","state":"unknown"}]`) || stderr.Len() != 0 {
		t.Fatalf("JSON report: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestExecuteWithCodexAdapter_preservesUnknownAndRuntimeExitCodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":40,"limit_window_seconds":18000}}}`)
	}))
	defer server.Close()
	adapter := codex.New(codex.Options{AuthFile: writeCLIAuth(t), Endpoints: []string{server.URL}, Client: server.Client(), Timeout: time.Second})
	var stdout, stderr bytes.Buffer
	code := ExecuteWithAdapter(t.Context(), []string{"value", "--provider", "codex", "--profile", "default", "--window", "weekly", "--field", "remaining"}, &stdout, &stderr, "test", adapter)
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "undefined") {
		t.Fatalf("unknown scalar: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	missing := codex.New(codex.Options{AuthFile: filepath.Join(t.TempDir(), "missing"), Endpoints: []string{server.URL}, Client: server.Client(), Timeout: time.Second})
	stdout.Reset()
	stderr.Reset()
	code = ExecuteWithAdapter(t.Context(), []string{"--provider", "codex", "--profile", "default"}, &stdout, &stderr, "test", missing)
	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "authentication file is missing") {
		t.Fatalf("missing auth: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	stdout.Reset()
	stderr.Reset()
	code = ExecuteWithAdapter(ctx, []string{"--provider", "codex", "--profile", "default"}, &stdout, &stderr, "test", adapter)
	if code != 130 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "interrupted") {
		t.Fatalf("canceled request: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestIssue5HelperProcess(t *testing.T) {
	if os.Getenv("REMAINDER_ISSUE5_HELPER") != "1" {
		return
	}
	os.Exit(runIssue5Helper())
}

type issue5Metrics struct {
	SchemaVersion            string `json:"schema_version"`
	RequestCount             int64  `json:"request_count"`
	ControlledTLSRoundTripNS int64  `json:"controlled_tls_round_trip_ns"`
}

type issue5TimingTransport struct {
	base     http.RoundTripper
	requests atomic.Int64
	elapsed  atomic.Int64
}

func (t *issue5TimingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	start := time.Now()
	response, err := t.base.RoundTrip(request)
	t.elapsed.Add(time.Since(start).Nanoseconds())
	t.requests.Add(1)
	return response, err
}

func runIssue5Helper() int {
	caData, err := os.ReadFile(os.Getenv("REMAINDER_ISSUE5_CA"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "remainder benchmark helper: read CA")
		return 1
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caData) {
		fmt.Fprintln(os.Stderr, "remainder benchmark helper: invalid CA")
		return 1
	}
	transport := &issue5TimingTransport{base: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	client := &http.Client{Transport: transport}
	fixedNow := func() time.Time { return time.Date(2026, time.March, 8, 7, 30, 0, 0, time.UTC) }
	adapter := codex.New(codex.Options{AuthFile: os.Getenv("REMAINDER_ISSUE5_AUTH"), Endpoints: []string{os.Getenv("REMAINDER_ISSUE5_ENDPOINT")}, Client: client, Timeout: time.Second, Now: fixedNow})
	code := ExecuteWithAdapterAt(context.Background(), []string{"value", "--provider", "codex", "--profile", "default", "--window", "five_hour", "--field", "remaining"}, os.Stdout, os.Stderr, "test", fixedNow(), adapter)
	if code != 0 {
		return code
	}
	metrics := issue5Metrics{SchemaVersion: "v1", RequestCount: transport.requests.Load(), ControlledTLSRoundTripNS: transport.elapsed.Load()}
	data, err := json.Marshal(metrics)
	if err != nil {
		fmt.Fprintln(os.Stderr, "remainder benchmark helper: encode metrics")
		return 1
	}
	if err := os.WriteFile(os.Getenv("REMAINDER_ISSUE5_METRICS"), append(data, '\n'), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "remainder benchmark helper: write metrics")
		return 1
	}
	return 0
}

func issue5HelperCommand(t *testing.T, server *httptest.Server, metricsPath, authPath, caPath string) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestIssue5HelperProcess$")
	command.Env = []string{
		"REMAINDER_ISSUE5_HELPER=1",
		"REMAINDER_ISSUE5_AUTH=" + authPath,
		"REMAINDER_ISSUE5_ENDPOINT=" + server.URL + "/backend-api/wham/usage",
		"REMAINDER_ISSUE5_CA=" + caPath,
		"REMAINDER_ISSUE5_METRICS=" + metricsPath,
	}
	return command
}

func writeTestCA(t *testing.T, server *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(path, certificate, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readIssue5Metrics(t *testing.T, path string) issue5Metrics {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var metrics issue5Metrics
	if err := json.Unmarshal(data, &metrics); err != nil {
		t.Fatal(err)
	}
	return metrics
}

func writeCLIAuth(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(`{"tokens":{"access_token":"synthetic-secret","account_id":"acct-test"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
