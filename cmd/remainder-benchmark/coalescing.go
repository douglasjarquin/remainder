package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/douglasjarquin/remainder/internal/benchmark"
	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/codex"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

const coalescingProcessCount = 12

type coalescingAcceptance struct {
	Kind                 string             `json:"kind"`
	Status               string             `json:"status"`
	Binary               buildMetadata      `json:"binary"`
	Fixture              string             `json:"fixture"`
	SumInstalled         bool               `json:"sum_installed"`
	PinchosInstalled     bool               `json:"pinchos_installed"`
	SuccessfulBurst      coalescingScenario `json:"successful_burst"`
	ForcedOverlap        coalescingScenario `json:"forced_overlap"`
	EmptyCacheRateLimit  coalescingScenario `json:"empty_cache_rate_limit"`
	TotalSubprocessCount int                `json:"total_subprocess_count"`
	TotalRequestCount    int64              `json:"total_request_count"`
	SandboxCleanup       string             `json:"sandbox_cleanup"`
}

type coalescingScenario struct {
	Status          string            `json:"status"`
	Processes       int               `json:"processes"`
	ProofCalls      int               `json:"proof_calls"`
	Requests        int64             `json:"requests"`
	ObservationTime string            `json:"observation_time,omitempty"`
	Account         string            `json:"account,omitempty"`
	Latency         benchmark.Summary `json:"contention_latency_ns"`
	MaxRSSKB        int64             `json:"max_rss_kb"`
	Samples         []coalescedSample `json:"samples"`
}

type coalescedSample struct {
	Args      []string `json:"args"`
	ExitCode  int      `json:"exit_code"`
	Stdout    string   `json:"stdout"`
	Stderr    string   `json:"stderr"`
	ElapsedNS int64    `json:"elapsed_ns"`
	MaxRSSKB  int64    `json:"max_rss_kb"`
}

func runCoalescingAcceptance(ctx context.Context, binary string) (result coalescingAcceptance, resultErr error) {
	binary, err := filepath.Abs(binary)
	if err != nil {
		return result, err
	}
	root, err := os.MkdirTemp("", "remainder-coalescing-")
	if err != nil {
		return result, err
	}
	defer func() {
		if err := os.RemoveAll(root); err != nil {
			result.SandboxCleanup = "failed"
			resultErr = errors.Join(resultErr, err)
			return
		}
		result.SandboxCleanup = "removed-owned-temporary-sandbox"
	}()
	home, codexHome := filepath.Join(root, "home"), filepath.Join(root, "codex")
	cacheHome, temporary := filepath.Join(root, "cache"), filepath.Join(root, "tmp")
	for _, directory := range []string{home, codexHome, cacheHome, temporary} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			return result, err
		}
	}
	if err := os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"access_token":"synthetic-fixture-token","account_id":"fixture-account"}}`), 0o600); err != nil {
		return result, err
	}
	fixture, err := newCoalescingFixture(root)
	if err != nil {
		return result, err
	}
	defer fixture.close()
	environment := fixture.environment(home, codexHome, cacheHome, temporary)

	result = coalescingAcceptance{Kind: "coalescing-acceptance", Status: "passed", Binary: inspectBuildPortable(binary), Fixture: fixture.String()}
	_, sumErr := exec.LookPath("sumctl")
	_, pinchosErr := exec.LookPath("pinchos")
	result.SumInstalled = sumErr == nil
	result.PinchosInstalled = pinchosErr == nil
	if result.SumInstalled || result.PinchosInstalled {
		return result, errors.New("coalescing acceptance requires Sum and Pinchos to be absent")
	}

	if err := seedExpiredCoalescingCache(ctx, codexHome, cacheHome); err != nil {
		return result, err
	}
	fixture.delayNS.Store(int64(500 * time.Millisecond))
	before := fixture.requests.Load()
	result.SuccessfulBurst, err = runCoalescedBurst(ctx, binary, environment, false, false)
	if err != nil {
		return result, err
	}
	result.SuccessfulBurst.Requests = fixture.requests.Load() - before
	provenanceArgs := []string{"--provider", "codex", "--profile", "default", "--format", "json", "--cache", "only", "--max-age", "1m"}
	provenance := runCoalescedProcess(ctx, binary, provenanceArgs, environment)
	observation, parseErr := evidence.ParseJSON([]byte(provenance.Stdout))
	if parseErr != nil || provenance.ExitCode != 0 || provenance.Stderr != "" || result.SuccessfulBurst.Requests != 1 || observation.Account.LastObserved != "fixture-account" {
		return result, fmt.Errorf("successful burst provenance failed: requests=%d process=%+v parse=%v", result.SuccessfulBurst.Requests, provenance, parseErr)
	}
	result.SuccessfulBurst.ProofCalls = 1
	result.SuccessfulBurst.ObservationTime = observation.ObservedAt.Format(time.RFC3339Nano)
	result.SuccessfulBurst.Account = observation.Account.LastObserved

	before = fixture.requests.Load()
	result.ForcedOverlap, err = runCoalescedBurst(ctx, binary, environment, true, false)
	if err != nil {
		return result, err
	}
	result.ForcedOverlap.Requests = fixture.requests.Load() - before
	laterArgs := []string{"value", "--provider", "codex", "--profile", "default", "--window", "weekly", "--field", "remaining", "--refresh"}
	later := runCoalescedProcess(ctx, binary, slices.Clone(laterArgs), environment)
	laterRequests := fixture.requests.Load() - before - result.ForcedOverlap.Requests
	if later.ExitCode != 0 || later.Stdout != "80\n" || later.Stderr != "" || !forcedRequestCountsValid(result.ForcedOverlap.Requests, laterRequests) {
		return result, fmt.Errorf("later forced process failed: overlap_requests=%d later_requests=%d process=%+v", result.ForcedOverlap.Requests, laterRequests, later)
	}
	result.ForcedOverlap.ProofCalls = 1

	rateCache := filepath.Join(root, "rate-cache")
	if err := os.Mkdir(rateCache, 0o700); err != nil {
		return result, err
	}
	rateEnvironment := fixture.environment(home, codexHome, rateCache, temporary)
	fixture.mode.Store(fixtureRateLimited)
	fixture.delayNS.Store(0)
	before = fixture.requests.Load()
	result.EmptyCacheRateLimit, err = runCoalescedBurst(ctx, binary, rateEnvironment, false, true)
	if err != nil {
		return result, err
	}
	result.EmptyCacheRateLimit.Requests = fixture.requests.Load() - before
	if result.EmptyCacheRateLimit.Requests != 1 {
		return result, fmt.Errorf("rate-limited burst made %d requests", result.EmptyCacheRateLimit.Requests)
	}

	result.TotalSubprocessCount = result.SuccessfulBurst.Processes + 1 + result.ForcedOverlap.Processes + 1 + result.EmptyCacheRateLimit.Processes
	result.TotalRequestCount = fixture.requests.Load()
	return result, nil
}

func forcedRequestCountsValid(overlap, later int64) bool {
	return overlap == 1 && later == 1
}

func runCoalescedBurst(ctx context.Context, binary string, environment []string, force, rateLimited bool) (coalescingScenario, error) {
	result := coalescingScenario{Status: "passed", Processes: coalescingProcessCount, Samples: make([]coalescedSample, coalescingProcessCount)}
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := range coalescingProcessCount {
		group.Go(func() {
			args, _ := coalescedArgs(index, force)
			<-start
			result.Samples[index] = runCoalescedProcess(ctx, binary, args, environment)
		})
	}
	close(start)
	group.Wait()
	elapsed := make([]int64, 0, coalescingProcessCount)
	for index, value := range result.Samples {
		_, expected := coalescedArgs(index, force)
		if !rateLimited && (value.ExitCode != 0 || value.Stdout != expected || value.Stderr != "") {
			return result, fmt.Errorf("coalesced process failed validation: %+v", value)
		}
		if rateLimited && (value.ExitCode != 1 || value.Stdout != "" || value.Stderr == "") {
			return result, fmt.Errorf("coalesced process failed validation: %+v", value)
		}
		elapsed = append(elapsed, value.ElapsedNS)
		result.MaxRSSKB = max(result.MaxRSSKB, value.MaxRSSKB)
	}
	latency, err := benchmark.Summarize(elapsed)
	if err != nil {
		return result, err
	}
	result.Latency = latency
	return result, nil
}

func coalescedArgs(index int, force bool) ([]string, string) {
	fields := [4][3]string{{"weekly", "remaining", "80\n"}, {"weekly", "used", "20\n"}, {"five_hour", "remaining", "60\n"}, {"five_hour", "used", "40\n"}}
	selected := fields[index%len(fields)]
	args := []string{"value", "--provider", "codex", "--profile", "default", "--window", selected[0], "--field", selected[1], "--max-age", "1s"}
	if force {
		args = append(args, "--refresh")
	}
	return args, selected[2]
}

func seedExpiredCoalescingCache(ctx context.Context, codexHome, cacheHome string) error {
	authPath := filepath.Join(codexHome, "auth.json")
	binding, err := codex.New(codex.Options{AuthFile: authPath}).CacheBinding(ctx, evidence.Request{Provider: "codex", Profile: "default"})
	if err != nil {
		return err
	}
	remaining, used := evidence.JSONNumber("80"), evidence.JSONNumber("20")
	observation := evidence.Observation{
		SchemaVersion: evidence.SchemaV1, Provider: "codex", Profile: "default",
		Account:    evidence.AccountIdentity{LastObserved: "fixture-account", Binding: evidence.IdentityVerified},
		Source:     evidence.SourceIdentity{Kind: "native_file_http", Name: "codex_auth_json"},
		ObservedAt: time.Now().UTC().Add(-time.Minute), Freshness: evidence.FreshFresh, Outcome: evidence.OutcomeComplete,
		Windows: []evidence.Window{{ID: "weekly", Scope: evidence.ScopeAccount, Unit: "percent", Limits: []evidence.Limit{
			{ID: "weekly_used", Field: "used", Value: evidence.Value{State: evidence.ValueDefined, Amount: &used}},
			{ID: "weekly_remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &remaining}},
		}}},
	}
	_, err = cache.New(filepath.Join(cacheHome, "remainder", "v1"), cache.Options{}).Put(ctx, binding, observation)
	return err
}

func runCoalescedProcess(ctx context.Context, binary string, args, environment []string) coalescedSample {
	command := exec.CommandContext(ctx, binary, args...)
	command.Env = environment
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	start := time.Now()
	err := command.Run()
	result := coalescedSample{Args: args, ExitCode: cacheHitExitCode(err), Stdout: stdout.String(), Stderr: stderr.String(), ElapsedNS: time.Since(start).Nanoseconds()}
	if command.ProcessState != nil {
		usage, ok := command.ProcessState.SysUsage().(*syscall.Rusage)
		if ok {
			result.MaxRSSKB = usage.Maxrss
		}
	}
	return result
}
