package cli

import (
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/claude"
	"github.com/douglasjarquin/remainder/internal/codex"
	"github.com/douglasjarquin/remainder/internal/cursor"
	"github.com/douglasjarquin/remainder/internal/grok"
)

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
	provider := os.Getenv("REMAINDER_ISSUE5_PROVIDER")
	if provider == "" {
		provider = "codex"
	}
	var adapter Adapter
	switch provider {
	case "codex":
		adapter = codex.New(codex.Options{AuthFile: os.Getenv("REMAINDER_ISSUE5_AUTH"), Endpoints: []string{os.Getenv("REMAINDER_ISSUE5_ENDPOINT")}, Client: client, Timeout: time.Second, Now: fixedNow})
	case "claude":
		adapter = claude.New(claude.Options{AuthFile: os.Getenv("REMAINDER_ISSUE5_AUTH"), ProfileEndpoint: os.Getenv("REMAINDER_ISSUE5_PROFILE_ENDPOINT"), UsageEndpoint: os.Getenv("REMAINDER_ISSUE5_USAGE_ENDPOINT"), Client: client, Timeout: time.Second, Now: fixedNow})
	case "grok":
		adapter = grok.New(grok.Options{AuthFile: os.Getenv("REMAINDER_ISSUE5_AUTH"), Endpoint: os.Getenv("REMAINDER_ISSUE5_GROK_ENDPOINT"), Client: client, Timeout: time.Second, Now: fixedNow})
	case "cursor":
		adapter = cursor.New(cursor.Options{AuthFile: os.Getenv("REMAINDER_ISSUE5_AUTH"), Endpoint: os.Getenv("REMAINDER_ISSUE5_CURSOR_ENDPOINT"), Client: client, Timeout: time.Second, Now: fixedNow})
	default:
		fmt.Fprintln(os.Stderr, "remainder benchmark helper: invalid provider")
		return 1
	}
	window := "five_hour"
	if provider == "grok" {
		window = "credits"
	} else if provider == "cursor" {
		window = "included_usage"
	}
	code := ExecuteWithAdapterAt(context.Background(), []string{"value", "--provider", provider, "--profile", "default", "--window", window, "--field", "remaining"}, os.Stdout, os.Stderr, "test", fixedNow(), adapter)
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
