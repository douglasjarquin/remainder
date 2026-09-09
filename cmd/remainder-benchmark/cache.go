package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/douglasjarquin/remainder/internal/benchmark"
	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/codex"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

const cacheHitWorkload = "cache-hit"

const cacheHitSource = "full-process-cobra-entrypoint"

type cacheHitResult struct {
	Samples         []sample
	Provenance      sample
	Summary         summary
	ObservedAt      time.Time
	Freshness       evidence.Freshness
	IdentityBinding evidence.IdentityBinding
	Account         string
	ObservationAge  time.Duration
	MaxAge          time.Duration
	Generation      string
	SandboxCleanup  string
}

func runCacheHit(ctx context.Context, binary string, samples int, observationAge, maxAge time.Duration, tokenizer *tokenizerClient) (result cacheHitResult, resultErr error) {
	if samples < 1 {
		return result, benchmark.ErrNoSamples
	}
	if observationAge < 0 || maxAge < 0 {
		return result, errors.New("cache-hit ages must not be negative")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		return result, fmt.Errorf("resolve cache-hit binary: %w", err)
	}
	info, err := os.Stat(binary)
	if err != nil {
		return result, fmt.Errorf("stat cache-hit binary: %w", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return result, fmt.Errorf("cache-hit binary is not executable: %s", binary)
	}

	sandbox, err := os.MkdirTemp("", "remainder-cache-hit-")
	if err != nil {
		return result, fmt.Errorf("create cache-hit sandbox: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(sandbox); err != nil {
			result.SandboxCleanup = "failed"
			resultErr = errors.Join(resultErr, fmt.Errorf("remove cache-hit sandbox: %w", err))
			return
		}
		if _, err := os.Stat(sandbox); !errors.Is(err, os.ErrNotExist) {
			result.SandboxCleanup = "failed"
			resultErr = errors.Join(resultErr, errors.New("cache-hit sandbox still exists after cleanup"))
			return
		}
		result.SandboxCleanup = "removed-owned-temporary-sandbox"
	}()

	home := filepath.Join(sandbox, "home")
	codexHome := filepath.Join(sandbox, "codex-home")
	xdgCacheHome := filepath.Join(sandbox, "xdg-cache")
	tmp := filepath.Join(sandbox, "tmp")
	for _, directory := range []string{home, codexHome, xdgCacheHome, tmp} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			return result, fmt.Errorf("create cache-hit directory: %w", err)
		}
	}
	cacheRoot, err := cacheRootForOS(runtime.GOOS, home, xdgCacheHome)
	if err != nil {
		return result, err
	}
	authPath := filepath.Join(codexHome, "auth.json")
	if err := os.WriteFile(authPath, []byte("{"), 0o600); err != nil {
		return result, fmt.Errorf("write cache-hit auth metadata: %w", err)
	}
	request := evidence.Request{Provider: "codex", Profile: "default"}
	binding, err := codex.New(codex.Options{AuthFile: authPath}).CacheBinding(ctx, request)
	if err != nil {
		return result, fmt.Errorf("derive cache-hit binding: %w", err)
	}
	now := time.Now().UTC()
	observedAt := now.Add(-observationAge)
	amount := evidence.JSONNumber("42")
	observation := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "default",
		Account:       evidence.AccountIdentity{LastObserved: "acct-benchmark", Binding: evidence.IdentityVerified},
		Source:        evidence.SourceIdentity{Kind: "native_file_http", Name: "codex_auth_json"},
		ObservedAt:    observedAt,
		Freshness:     evidence.FreshFresh,
		Outcome:       evidence.OutcomeComplete,
		Windows: []evidence.Window{{
			ID: "five_hour", Scope: evidence.ScopeAccount, Unit: "percent",
			Limits: []evidence.Limit{{ID: "five_hour_remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueDefined, Amount: &amount}}},
		}},
	}
	generation, err := cache.New(cacheRoot, cache.Options{}).Put(ctx, binding, observation)
	if err != nil {
		return result, fmt.Errorf("seed cache-hit observation: %w", err)
	}
	result.Generation = generation
	result.ObservationAge = observationAge
	result.MaxAge = maxAge
	result.Samples = make([]sample, 0, samples)
	elapsedSamples := make([]int64, 0, samples)
	args := []string{"value", "--provider", "codex", "--profile", "default", "--window", "five_hour", "--field", "remaining", "--cache", "auto", "--max-age", maxAge.String()}
	for n := 1; n <= samples; n++ {
		value, err := runCacheHitSample(ctx, binary, cacheHitWorkload, args, n, home, codexHome, xdgCacheHome, tmp, tokenizer)
		if err != nil {
			return result, err
		}
		if value.ExitCode != 0 || value.Stdout != "42\n" || value.Stderr != "" || value.RequestCount != 0 {
			value.OutputStatus = "fail"
			result.Samples = append(result.Samples, value)
			return result, fmt.Errorf("cache-hit sample %d failed: exit=%d stdout=%q stderr=%q requests=%d", n, value.ExitCode, value.Stdout, value.Stderr, value.RequestCount)
		}
		value.OutputStatus = "pass"
		result.Samples = append(result.Samples, value)
		elapsedSamples = append(elapsedSamples, value.ElapsedNS)
	}
	provenanceArgs := []string{"--provider", "codex", "--profile", "default", "--format", "json", "--cache", "auto", "--max-age", maxAge.String()}
	provenance, err := runCacheHitSample(ctx, binary, "cache-hit-provenance", provenanceArgs, 1, home, codexHome, xdgCacheHome, tmp, tokenizer)
	if err != nil {
		return result, err
	}
	parsed, err := evidence.ParseJSON([]byte(provenance.Stdout))
	if err != nil || provenance.ExitCode != 0 || provenance.Stderr != "" || provenance.RequestCount != 0 {
		return result, fmt.Errorf("cache-hit provenance failed: exit=%d stderr=%q parse=%v", provenance.ExitCode, provenance.Stderr, err)
	}
	selected, err := evidence.SelectValue(parsed, evidence.ValueRequest{Provider: "codex", Profile: "default", Window: "five_hour", Field: evidence.FieldRemaining}, evidence.FreshOnly)
	if err != nil || selected != "42\n" || !parsed.ObservedAt.Equal(observedAt) || parsed.Freshness != evidence.FreshFresh || parsed.Account.Binding != evidence.IdentityHistorical {
		return result, fmt.Errorf("cache-hit provenance changed: value=%q observed_at=%s freshness=%s identity=%s error=%v", selected, parsed.ObservedAt.Format(time.RFC3339Nano), parsed.Freshness, parsed.Account.Binding, err)
	}
	provenance.OutputStatus = "pass"
	result.Provenance = provenance
	result.ObservedAt = parsed.ObservedAt
	result.Freshness = parsed.Freshness
	result.IdentityBinding = parsed.Account.Binding
	result.Account = parsed.Account.LastObserved
	measured, err := benchmark.Summarize(elapsedSamples)
	if err != nil {
		return result, fmt.Errorf("summarize cache-hit samples: %w", err)
	}
	result.Summary = summary{Kind: "summary", Source: cacheHitSource, Workloads: map[string]benchmark.Summary{cacheHitWorkload: measured}}
	return result, nil
}

func runCacheHitSample(ctx context.Context, binary, workload string, args []string, number int, home, codexHome, xdgCacheHome, tmp string, tokenizer *tokenizerClient) (sample, error) {
	command := exec.CommandContext(ctx, binary, args...)
	command.Env = []string{
		"HOME=" + home,
		"CODEX_HOME=" + codexHome,
		"XDG_CACHE_HOME=" + xdgCacheHome,
		"TMPDIR=" + tmp,
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	start := time.Now()
	runErr := command.Run()
	elapsedNS := time.Since(start).Nanoseconds()
	stdoutTokens, err := countTokens(tokenizer, stdout.String())
	if err != nil {
		return sample{}, err
	}
	stderrTokens, err := countTokens(tokenizer, stderr.String())
	if err != nil {
		return sample{}, err
	}
	filesystemState := "eligible-cache-warm-filesystem"
	if number == 1 {
		filesystemState = "eligible-cache-first-process"
	}
	if workload == "cache-hit-provenance" {
		filesystemState = "seeded-cache-provenance"
	}
	return sample{
		Kind: "sample", Source: cacheHitSource, Workload: workload, Args: args, Sample: number, FilesystemState: filesystemState,
		ElapsedNS: elapsedNS, ExitCode: cacheHitExitCode(runErr), Stdout: stdout.String(), Stderr: stderr.String(),
		StdoutBytes: stdout.Len(), StderrBytes: stderr.Len(), StdoutTokens: stdoutTokens, StderrTokens: stderrTokens,
		StdoutSHA256: digest(stdout.Bytes()), StderrSHA256: digest(stderr.Bytes()), SubprocessCount: 1, RequestCount: 0,
	}, nil
}

func cacheHitObjectiveFailure(measured benchmark.Summary, goos, goarch string, objectiveNS int64) error {
	if goos != "darwin" || goarch != "arm64" {
		return nil
	}
	if measured.P95NS > objectiveNS {
		return fmt.Errorf("cache-hit p95 %d ns exceeds Apple Silicon objective %d ns", measured.P95NS, objectiveNS)
	}
	return nil
}

func cacheHitExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitErr.ExitCode()
	}
	return 125
}

func cacheRootForOS(goos, home, xdgCacheHome string) (string, error) {
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Caches", "remainder", "v1"), nil
	case "linux":
		return filepath.Join(xdgCacheHome, "remainder", "v1"), nil
	default:
		return "", fmt.Errorf("cache-hit benchmark does not support %s", goos)
	}
}
