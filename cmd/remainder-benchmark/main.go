package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/douglasjarquin/remainder/internal/benchmark"
)

const (
	appleSiliconP95ObjectiveNS int64 = 10_000_000
	quotaAxiVersion                  = "0.1.41"
)

func main() {
	binary := flag.String("binary", "./bin/remainder", "compiled remainder binary to measure")
	helper := flag.String("controlled-refresh-helper", "", "compiled internal/cli test helper for controlled refresh measurements")
	samples := flag.Int("samples", 10, "samples per workload")
	tokenizerPython := flag.String("tokenizer-python", "", "optional Python executable with tiktoken installed")
	comparators := flag.Bool("comparators", false, "run controlled preinstalled quota-axi and Pinchos consumer probes")
	coalescing := flag.Bool("coalescing-acceptance", false, "run the release-binary cross-process cache acceptance workload")
	quotaAxiPreload := flag.String("quota-axi-preload", "scripts/quota_axi_fixture.mjs", "developer-only Node preload for synthetic quota-axi input")
	latencyBaseline := flag.String("latency-baseline", "", "optional JSON baseline for seeded p95 regression detection")
	flag.Parse()
	if *samples < 1 {
		fatalf("samples must be at least 1")
	}
	info, err := os.Stat(*binary)
	if err != nil {
		fatalf("stat binary: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		fatalf("benchmark binary is not executable: %s", *binary)
	}
	if *coalescing {
		result, err := runCoalescingAcceptance(context.Background(), *binary)
		if err != nil {
			fatalf("coalescing acceptance: %v", err)
		}
		writeJSON(result)
		return
	}
	if *helper == "" {
		fatalf("controlled refresh helper is required")
	}
	workloads := []workload{
		{Name: "startup-help", Args: []string{"--help"}, ExitCode: 0},
		{Name: "startup-version", Args: []string{"--version"}, ExitCode: 0},
		{Name: "failure-unavailable", ExitCode: 1},
		{Name: "failure-invalid-freshness", Args: []string{"--freshness", "ignored"}, ExitCode: 2},
	}
	workloadNames := make([]string, 0, len(workloads))
	for _, item := range workloads {
		workloadNames = append(workloadNames, item.Name)
	}
	workloadNames = append(workloadNames, "controlled-refresh", cacheHitWorkload, "cache-hit-provenance")
	version := commandOutput(*binary, "--version")
	tokenizer, tokenizerInfo, err := startTokenizer(*tokenizerPython)
	if err != nil {
		fatalf("start tokenizer: %v", err)
	}
	controlled, err := runControlledRefresh(*helper, *samples, tokenizer)
	if err != nil {
		fatalf("controlled refresh: %v", err)
	}
	cached, err := runCacheHit(context.Background(), *binary, *samples, time.Second, time.Hour, tokenizer)
	if err != nil {
		fatalf("cache hit: %v", err)
	}
	buildInfo := inspectBuild(*binary)
	host := inspectHost()
	var comparison []comparator
	var regressions []string
	if err := cacheHitObjectiveFailure(cached.Summary.Workloads[cacheHitWorkload], runtime.GOOS, runtime.GOARCH, appleSiliconP95ObjectiveNS); err != nil {
		regressions = append(regressions, err.Error())
	}
	if *comparators {
		comparison = runComparators(tokenizer, *quotaAxiPreload)
		regressions = append(regressions, comparatorFailures(comparison)...)
	} else {
		comparison = notRunComparators()
	}
	writeJSON(metadata{
		Kind:               "metadata",
		Source:             "full-process-cobra-entrypoint",
		Binary:             *binary,
		Version:            version,
		Machine:            runtime.GOOS + "/" + runtime.GOARCH,
		GoVersion:          runtime.Version(),
		GoOS:               runtime.GOOS,
		GoArch:             runtime.GOARCH,
		CPUCount:           runtime.NumCPU(),
		GOMAXPROCS:         runtime.GOMAXPROCS(0),
		Host:               host,
		Build:              buildInfo,
		HelperBuild:        controlled.HelperBuild,
		SizeBytes:          info.Size(),
		HelperSizeBytes:    controlled.HelperSize,
		SamplesPerWorkload: *samples,
		Workloads:          workloadNames,
		Tokenizer:          tokenizerInfo,
		FixtureSeed:        benchmark.FixtureSeed,
		FixtureClock:       benchmark.FixtureClock,
		FixtureSHA256:      benchmark.FixtureHash(),
		TokenApplicability: "o200k_base is an offline Codex-family comparison encoding; model-specific tokenizer certification remains outside this baseline",
		CacheWorkload:      "eligible release-binary cache hit with synthetic metadata binding",
		CacheMeasurement: cacheMeasurement{
			Source:                 cacheHitSource,
			Policy:                 "auto",
			ObservationAgeNS:       cached.ObservationAge.Nanoseconds(),
			MaxAgeNS:               cached.MaxAge.Nanoseconds(),
			ObservedAt:             cached.ObservedAt.Format(time.RFC3339Nano),
			Freshness:              string(cached.Freshness),
			Account:                cached.Account,
			IdentityBinding:        string(cached.IdentityBinding),
			TimedSamples:           len(cached.Samples),
			ProvenanceSubprocesses: cached.Provenance.SubprocessCount,
			SandboxCleanup:         cached.SandboxCleanup,
		},
		RefreshWorkload:        "controlled compiled test helper through Cobra and native Codex adapter",
		ObservedSubprocesses:   (len(workloads)+2)*(*samples) + cached.Provenance.SubprocessCount,
		ObservedRequests:       controlled.RequestCount,
		AllocationsMeasurement: "go test -bench -benchmem output in artifacts/benchmark-in-process.txt",
		P95ObjectiveNS:         appleSiliconP95ObjectiveNS,
		P95ObjectiveScope:      "eligible release-binary cache hits on the declared Apple Silicon reference host",
		P95Policy:              "cache-hit p95 gates darwin/arm64 reference-host runs; startup, failure, refresh, and other-host summaries remain trend evidence",
		Comparators:            comparison,
	})

	for _, fixture := range benchmark.Fixtures() {
		record, err := measureFixture(fixture, tokenizer)
		if err != nil {
			fatalf("measure fixture %s: %v", fixture.Name, err)
		}
		writeJSON(record)
		if record.ComparisonStatus != "equivalent-required-facts" {
			regressions = append(regressions, fmt.Sprintf("fixture %s comparison %s", fixture.Name, record.ComparisonStatus))
		}
	}

	byWorkload := make(map[string][]int64, len(workloads))
	for _, item := range workloads {
		for n := 1; n <= *samples; n++ {
			result, err := run(*binary, item, tokenizer)
			if err != nil {
				fatalf("%s sample %d: %v", item.Name, n, err)
			}
			if result.ExitCode != item.ExitCode {
				regressions = append(regressions, fmt.Sprintf("%s sample %d exited %d, want %d", item.Name, n, result.ExitCode, item.ExitCode))
			}
			result.OutputStatus = validateOutput(item, result)
			if result.OutputStatus != "pass" {
				regressions = append(regressions, fmt.Sprintf("%s sample %d output %s", item.Name, n, result.OutputStatus))
			}
			result.Kind = "sample"
			result.Workload = item.Name
			result.Args = item.Args
			result.Sample = n
			if n == 1 {
				result.FilesystemState = "first-process"
			} else {
				result.FilesystemState = "warm-filesystem"
			}
			writeJSON(result)
			byWorkload[item.Name] = append(byWorkload[item.Name], result.ElapsedNS)
		}
	}
	for _, result := range controlled.Samples {
		writeJSON(result)
	}
	writeJSON(controlled.ProcessSummary)
	writeJSON(controlled.RequestSummary)
	for _, result := range cached.Samples {
		writeJSON(result)
	}
	writeJSON(cached.Provenance)
	writeJSON(cached.Summary)

	summaries := make(map[string]benchmark.Summary, len(byWorkload))
	for name, samples := range byWorkload {
		value, err := benchmark.Summarize(samples)
		if err != nil {
			fatalf("summarize %s: %v", name, err)
		}
		summaries[name] = value
	}
	latency := checkLatencyRegression(summaries, *latencyBaseline)
	if latency.Status == "failed" {
		regressions = append(regressions, latency.Failures...)
	}
	writeJSON(latency)
	if len(regressions) > 0 {
		writeJSON(map[string]any{"kind": "regression", "source": "full-process-cobra-entrypoint", "status": "failed", "failures": regressions})
	}
	writeJSON(summary{Kind: "summary", Source: "full-process-cobra-entrypoint", Workloads: summaries})
	if tokenizer != nil {
		if err := tokenizer.Close(); err != nil {
			fatalf("close tokenizer: %v", err)
		}
	}
	if len(regressions) > 0 {
		os.Exit(1)
	}
}

func commandOutput(binary string, arg string) string {
	output, err := exec.Command(binary, arg).Output()
	if err != nil {
		fatalf("read version: %v", err)
	}
	return string(output)
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeJSON(value any) {
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		fatalf("write JSON: %v", err)
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
