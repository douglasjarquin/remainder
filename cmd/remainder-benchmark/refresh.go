package main

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/douglasjarquin/remainder/internal/benchmark"
)

const (
	controlledRefreshSource       = "compiled-test-helper-cobra-entrypoint"
	controlledRefreshTimingSource = "controlled-loopback-tls-round-trip-sum"
)

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

func runControlledRefresh(helper, provider string, samples int, tokenizer *tokenizerClient) (controlledRefreshResult, error) {
	return runControlledRefreshWithFault(helper, provider, samples, tokenizer, nil)
}

func runControlledRefreshWithFault(helper, provider string, samples int, tokenizer *tokenizerClient, fault controlledRefreshFault) (result controlledRefreshResult, resultErr error) {
	if provider != "codex" && provider != "claude" {
		return result, fmt.Errorf("controlled refresh provider must be codex or claude")
	}
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

	validator := &controlledRequestValidator{provider: provider}
	server := httptest.NewTLSServer(validator)
	defer server.Close()
	authName := "auth.json"
	authBody := []byte(`{"tokens":{"access_token":"synthetic-secret","account_id":"acct-test"}}`)
	if provider == "claude" {
		authName = ".credentials.json"
		authBody = []byte(`{"claudeAiOauth":{"accessToken":"synthetic-secret"}}`)
	}
	authPath := filepath.Join(root, authName)
	if err := os.WriteFile(authPath, authBody, 0o600); err != nil {
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
		beforeRequests := validator.requests.Load()
		beforeProfile := validator.profileRequests.Load()
		beforeUsage := validator.usageRequests.Load()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		command := exec.CommandContext(ctx, helper, args...)
		command.Env = []string{
			"HOME=" + home,
			"CODEX_HOME=" + codeHome,
			"CLAUDE_CONFIG_DIR=" + codeHome,
			"TMPDIR=" + tmp,
			"REMAINDER_ISSUE5_HELPER=1",
			"REMAINDER_ISSUE5_PROVIDER=" + provider,
			"REMAINDER_ISSUE5_AUTH=" + authPath,
			"REMAINDER_ISSUE5_ENDPOINT=" + server.URL + "/backend-api/wham/usage",
			"REMAINDER_ISSUE5_PROFILE_ENDPOINT=" + server.URL + "/profile",
			"REMAINDER_ISSUE5_USAGE_ENDPOINT=" + server.URL + "/usage",
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
		delta := validator.requests.Load() - beforeRequests
		requestErr := errors.Join(validator.failure(), validator.countFailure(provider, beforeRequests, beforeProfile, beforeUsage))
		if err := validateControlledRefresh(provider, exitCode, stdout.String(), stderr.String(), elapsedNS, delta, metrics, requestErr); err != nil {
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
	result.RequestSummary = requestTimingSummary{Kind: "request-timing-summary", Source: controlledRefreshTimingSource, Workload: "controlled-refresh", Timing: requestSummary}
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

func validateControlledRefresh(provider string, exitCode int, stdout, stderr string, elapsedNS, requestDelta int64, metrics controlledRefreshMetrics, requestErr error) error {
	if requestErr != nil {
		return requestErr
	}
	if exitCode != 0 || stdout != "60\n" || stderr != "" {
		return fmt.Errorf("process output: exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if metrics.SchemaVersion != "v1" {
		return fmt.Errorf("metrics schema %q", metrics.SchemaVersion)
	}
	wantRequests := int64(1)
	if provider == "claude" {
		wantRequests = 2
	}
	if requestDelta != wantRequests || metrics.RequestCount != wantRequests || metrics.RequestCount != requestDelta {
		return fmt.Errorf("request count: parent=%d helper=%d", requestDelta, metrics.RequestCount)
	}
	if metrics.ControlledTLSRoundTripNS <= 0 || elapsedNS <= metrics.ControlledTLSRoundTripNS {
		return fmt.Errorf("timing boundary: elapsed=%d controlled_tls_round_trip=%d", elapsedNS, metrics.ControlledTLSRoundTripNS)
	}
	return nil
}
