package main

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/douglasjarquin/remainder/internal/benchmark"
)

const controlledRefreshSource = "compiled-test-helper-cobra-entrypoint"

type controlledRefreshMetrics struct {
	SchemaVersion            string `json:"schema_version"`
	RequestCount             int64  `json:"request_count"`
	ControlledTLSRoundTripNS int64  `json:"controlled_tls_round_trip_ns"`
}

type controlledRefreshResult struct {
	Samples        []sample
	ProcessSummary summary
	RequestSummary requestTimingSummary
	RequestCount   int
	HelperBuild    buildMetadata
	HelperSize     int64
}

type controlledRefreshFault func(string, *atomic.Int64) error

type controlledRequestValidator struct {
	requests atomic.Int64
	mu       sync.Mutex
	failures []string
}

func (v *controlledRequestValidator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	v.requests.Add(1)
	checks := []struct {
		valid   bool
		failure string
	}{
		{r.Method == http.MethodGet, "method mismatch"},
		{r.URL.Path == "/backend-api/wham/usage", "path mismatch"},
		{r.Header.Get("Authorization") == "Bearer synthetic-secret", "authorization header mismatch"},
		{r.Header.Get("ChatGPT-Account-Id") == "acct-test", "account header mismatch"},
		{r.Header.Get("Accept") == "application/json", "accept header mismatch"},
	}
	for _, check := range checks {
		if !check.valid {
			v.mu.Lock()
			v.failures = append(v.failures, check.failure)
			v.mu.Unlock()
		}
	}
	v.mu.Lock()
	failed := len(v.failures) > 0
	v.mu.Unlock()
	if failed {
		http.Error(w, "invalid controlled request", http.StatusBadRequest)
		return
	}
	fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":40,"limit_window_seconds":18000}}}`)
}

func (v *controlledRequestValidator) failure() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.failures) == 0 {
		return nil
	}
	return fmt.Errorf("controlled request: %s", strings.Join(v.failures, ", "))
}

func runControlledRefresh(helper string, samples int, tokenizer *tokenizerClient) (controlledRefreshResult, error) {
	return runControlledRefreshWithFault(helper, samples, tokenizer, nil)
}

func runControlledRefreshWithFault(helper string, samples int, tokenizer *tokenizerClient, fault controlledRefreshFault) (result controlledRefreshResult, resultErr error) {
	info, err := os.Stat(helper)
	if err != nil {
		return result, fmt.Errorf("stat controlled helper: %w", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return result, fmt.Errorf("controlled helper is not executable: %s", helper)
	}
	root, err := os.MkdirTemp("", "remainder-controlled-refresh-")
	if err != nil {
		return result, fmt.Errorf("create controlled fixture: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(root); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove controlled fixture: %w", err))
			return
		}
		if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, errors.New("controlled fixture still exists after cleanup"))
		}
	}()

	validator := &controlledRequestValidator{}
	server := httptest.NewTLSServer(validator)
	defer server.Close()
	authPath := filepath.Join(root, "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"tokens":{"access_token":"synthetic-secret","account_id":"acct-test"}}`), 0o600); err != nil {
		return result, fmt.Errorf("write controlled auth: %w", err)
	}
	caPath := filepath.Join(root, "ca.pem")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caPath, certificate, 0o600); err != nil {
		return result, fmt.Errorf("write controlled CA: %w", err)
	}
	home := filepath.Join(root, "home")
	codeHome := filepath.Join(root, "codex-home")
	tmp := filepath.Join(root, "tmp")
	for _, directory := range []string{home, codeHome, tmp} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			return result, fmt.Errorf("create controlled directory: %w", err)
		}
	}

	result.HelperBuild = inspectBuild(helper)
	result.HelperSize = info.Size()
	processTimes := make([]int64, 0, samples)
	requestTimes := make([]int64, 0, samples)
	result.Samples = make([]sample, 0, samples)
	args := []string{"-test.run=^TestIssue5HelperProcess$"}
	for n := 1; n <= samples; n++ {
		metricsPath := filepath.Join(root, fmt.Sprintf("metrics-%d.json", n))
		before := validator.requests.Load()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		command := exec.CommandContext(ctx, helper, args...)
		command.Env = []string{
			"HOME=" + home,
			"CODEX_HOME=" + codeHome,
			"TMPDIR=" + tmp,
			"REMAINDER_ISSUE5_HELPER=1",
			"REMAINDER_ISSUE5_AUTH=" + authPath,
			"REMAINDER_ISSUE5_ENDPOINT=" + server.URL + "/backend-api/wham/usage",
			"REMAINDER_ISSUE5_CA=" + caPath,
			"REMAINDER_ISSUE5_METRICS=" + metricsPath,
		}
		var stdout, stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		start := time.Now()
		runErr := command.Run()
		elapsedNS := time.Since(start).Nanoseconds()
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
		cancel()
		if timedOut {
			return controlledRefreshResult{}, fmt.Errorf("controlled sample %d timed out", n)
		}
		exitCode := processExitCode(runErr)
		if fault != nil {
			if err := fault(metricsPath, &validator.requests); err != nil {
				return controlledRefreshResult{}, fmt.Errorf("controlled sample %d fault: %w", n, err)
			}
		}
		metrics, err := readControlledRefreshMetrics(metricsPath)
		if err != nil {
			return controlledRefreshResult{}, fmt.Errorf("controlled sample %d metrics: %w", n, err)
		}
		delta := validator.requests.Load() - before
		if err := validateControlledRefresh(exitCode, stdout.String(), stderr.String(), elapsedNS, delta, metrics, validator.failure()); err != nil {
			return controlledRefreshResult{}, fmt.Errorf("controlled sample %d: %w", n, err)
		}
		stdoutTokens, err := countTokens(tokenizer, stdout.String())
		if err != nil {
			return controlledRefreshResult{}, err
		}
		stderrTokens, err := countTokens(tokenizer, stderr.String())
		if err != nil {
			return controlledRefreshResult{}, err
		}
		result.Samples = append(result.Samples, sample{
			Kind: "sample", Source: controlledRefreshSource, Workload: "controlled-refresh", Args: args, Sample: n,
			FilesystemState: "isolated-controlled-fixture", ElapsedNS: elapsedNS, ExitCode: exitCode,
			Stdout: stdout.String(), Stderr: stderr.String(), StdoutBytes: stdout.Len(), StderrBytes: stderr.Len(),
			StdoutTokens: stdoutTokens, StderrTokens: stderrTokens, StdoutSHA256: digest(stdout.Bytes()), StderrSHA256: digest(stderr.Bytes()),
			SubprocessCount: 1, RequestCount: int(delta), ControlledTLSRoundTripNS: metrics.ControlledTLSRoundTripNS,
			HelperBuild: &result.HelperBuild, OutputStatus: "pass",
		})
		processTimes = append(processTimes, elapsedNS)
		requestTimes = append(requestTimes, metrics.ControlledTLSRoundTripNS)
	}
	processSummary, err := benchmark.Summarize(processTimes)
	if err != nil {
		return controlledRefreshResult{}, fmt.Errorf("summarize controlled helper process: %w", err)
	}
	requestSummary, err := benchmark.Summarize(requestTimes)
	if err != nil {
		return controlledRefreshResult{}, fmt.Errorf("summarize controlled TLS round trip: %w", err)
	}
	result.ProcessSummary = summary{Kind: "summary", Source: controlledRefreshSource, Workloads: map[string]benchmark.Summary{"controlled-refresh": processSummary}}
	result.RequestSummary = requestTimingSummary{Kind: "request-timing-summary", Source: "controlled-loopback-tls-round-trip", Workload: "controlled-refresh", Timing: requestSummary}
	result.RequestCount = int(validator.requests.Load())
	return result, nil
}

func processExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitErr.ExitCode()
	}
	return 125
}

func readControlledRefreshMetrics(path string) (controlledRefreshMetrics, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return controlledRefreshMetrics{}, err
	}
	var metrics controlledRefreshMetrics
	if err := json.Unmarshal(data, &metrics); err != nil {
		return controlledRefreshMetrics{}, err
	}
	return metrics, nil
}

func validateControlledRefresh(exitCode int, stdout, stderr string, elapsedNS, requestDelta int64, metrics controlledRefreshMetrics, requestErr error) error {
	if requestErr != nil {
		return requestErr
	}
	if exitCode != 0 || stdout != "60\n" || stderr != "" {
		return fmt.Errorf("process output: exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if metrics.SchemaVersion != "v1" {
		return fmt.Errorf("metrics schema %q", metrics.SchemaVersion)
	}
	if requestDelta != 1 || metrics.RequestCount != 1 || metrics.RequestCount != requestDelta {
		return fmt.Errorf("request count: parent=%d helper=%d", requestDelta, metrics.RequestCount)
	}
	if metrics.ControlledTLSRoundTripNS <= 0 || elapsedNS <= metrics.ControlledTLSRoundTripNS {
		return fmt.Errorf("timing boundary: elapsed=%d controlled_tls_round_trip=%d", elapsedNS, metrics.ControlledTLSRoundTripNS)
	}
	return nil
}
