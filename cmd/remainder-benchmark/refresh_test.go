package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestControlledRefreshRun_recordsProviderSamplesAndSummaries(t *testing.T) {
	helper := buildControlledRefreshHelper(t)
	for _, test := range []struct {
		provider string
		requests int
	}{
		{provider: "codex", requests: 1},
		{provider: "claude", requests: 2},
	} {
		t.Run(test.provider, func(t *testing.T) {
			// Given
			const samples = 2

			// When
			result, err := runControlledRefresh(helper, test.provider, samples, nil)
			// Then
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Samples) != samples || result.RequestCount != samples*test.requests || result.ProcessSummary.Workloads["controlled-refresh"].Count != samples || result.RequestSummary.Timing.Count != samples || result.RequestSummary.Source != controlledRefreshTimingSource {
				t.Fatalf("controlled result = %+v", result)
			}
			for _, value := range result.Samples {
				if value.Source != controlledRefreshSource || value.ExitCode != 0 || value.Stdout != "60\n" || value.Stderr != "" || value.SubprocessCount != 1 || value.RequestCount != test.requests || value.ElapsedNS <= value.ControlledTLSRoundTripNS || value.ControlledTLSRoundTripNS <= 0 {
					t.Fatalf("controlled sample = %+v", value)
				}
			}
		})
	}
}

func TestControlledRefreshRun_rejectsCountOrMetricsMismatch(t *testing.T) {
	helper := buildControlledRefreshHelper(t)
	tests := []struct {
		name  string
		fault controlledRefreshFault
	}{
		{name: "parent request count", fault: func(_ string, requests *atomic.Int64) error { requests.Add(1); return nil }},
		{name: "missing metrics", fault: func(path string, _ *atomic.Int64) error { return os.Remove(path) }},
		{name: "malformed metrics output", fault: func(path string, _ *atomic.Int64) error { return os.WriteFile(path, []byte("not json"), 0o600) }},
		{name: "metrics schema", fault: func(path string, _ *atomic.Int64) error {
			return os.WriteFile(path, []byte(`{"schema_version":"v2","request_count":1,"controlled_tls_round_trip_ns":1}`), 0o600)
		}},
		{name: "metrics timing", fault: func(path string, _ *atomic.Int64) error {
			return os.WriteFile(path, []byte(`{"schema_version":"v1","request_count":1,"controlled_tls_round_trip_ns":0}`), 0o600)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := runControlledRefreshWithFault(helper, "codex", 1, nil, test.fault)
			if err == nil || len(result.Samples) != 0 || result.ProcessSummary.Kind != "" || result.RequestSummary.Kind != "" {
				t.Fatalf("result = %+v, error = %v", result, err)
			}
		})
	}
}

func TestControlledRequestValidator_rejectsWrongHeaders(t *testing.T) {
	validator := &controlledRequestValidator{provider: "codex"}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.test/wrong", nil)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()

	validator.ServeHTTP(response, request)
	if validator.requests.Load() != 1 || validator.failure() == nil || response.Code != http.StatusBadRequest {
		t.Fatalf("requests=%d failure=%v status=%d", validator.requests.Load(), validator.failure(), response.Code)
	}
}

func buildControlledRefreshHelper(t *testing.T) string {
	t.Helper()
	output, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(strings.TrimSpace(string(output)))
	helper := filepath.Join(t.TempDir(), "remainder-refresh-helper")
	command := exec.Command("go", "test", "-c", "-o", helper, "./internal/cli")
	command.Dir = root
	command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, output)
	}
	return helper
}
