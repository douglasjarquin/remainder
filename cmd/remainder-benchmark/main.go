package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/douglasjarquin/remainder/internal/benchmark"
)

const buildFlags = "CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X main.version=v0.1.0'"

type workload struct {
	Name     string
	Args     []string
	ExitCode int
}

type sample struct {
	Kind            string   `json:"kind"`
	Workload        string   `json:"workload"`
	Args            []string `json:"args"`
	Sample          int      `json:"sample"`
	FilesystemState string   `json:"filesystem_state"`
	ElapsedNS       int64    `json:"elapsed_ns"`
	ExitCode        int      `json:"exit_code"`
	Stdout          string   `json:"stdout"`
	Stderr          string   `json:"stderr"`
	StdoutBytes     int      `json:"stdout_bytes"`
	StderrBytes     int      `json:"stderr_bytes"`
	StdoutTokens    int      `json:"stdout_tokens"`
	StderrTokens    int      `json:"stderr_tokens"`
	StdoutSHA256    string   `json:"stdout_sha256"`
	StderrSHA256    string   `json:"stderr_sha256"`
	SubprocessCount int      `json:"subprocess_count"`
	RequestCount    int      `json:"request_count"`
}

type metadata struct {
	Kind                   string   `json:"kind"`
	Source                 string   `json:"source"`
	Binary                 string   `json:"binary"`
	Version                string   `json:"version"`
	Machine                string   `json:"machine"`
	GoVersion              string   `json:"go_version"`
	GoOS                   string   `json:"goos"`
	GoArch                 string   `json:"goarch"`
	CPUCount               int      `json:"cpu_count"`
	GOMAXPROCS             int      `json:"gomaxprocs"`
	BuildFlags             string   `json:"build_flags"`
	SizeBytes              int64    `json:"size_bytes"`
	SamplesPerWorkload     int      `json:"samples_per_workload"`
	Workloads              []string `json:"workloads"`
	Tokenizer              string   `json:"tokenizer"`
	FixtureSeed            string   `json:"fixture_seed"`
	FixtureSHA256          string   `json:"fixture_sha256"`
	TokenApplicability     string   `json:"token_applicability"`
	CacheWorkload          string   `json:"cache_workload"`
	RefreshWorkload        string   `json:"refresh_workload"`
	ObservedSubprocesses   int      `json:"observed_subprocesses"`
	ObservedRequests       int      `json:"observed_requests"`
	AllocationsMeasurement string   `json:"allocations_measurement"`
}

type summary struct {
	Kind      string                       `json:"kind"`
	Source    string                       `json:"source"`
	Workloads map[string]benchmark.Summary `json:"workloads"`
}

func main() {
	binary := flag.String("binary", "./bin/remainder", "compiled remainder binary to measure")
	samples := flag.Int("samples", 10, "samples per workload")
	flag.Parse()
	if *samples < 1 {
		fatalf("samples must be at least 1")
	}
	info, err := os.Stat(*binary)
	if err != nil {
		fatalf("stat binary: %v", err)
	}
	if info.Mode().Perm()&0111 == 0 {
		fatalf("benchmark binary is not executable: %s", *binary)
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
	version := commandOutput(*binary, "--version")
	writeJSON(metadata{
		Kind:                   "metadata",
		Source:                 "full-process-cobra-entrypoint",
		Binary:                 *binary,
		Version:                version,
		Machine:                runtime.GOOS + "/" + runtime.GOARCH,
		GoVersion:              runtime.Version(),
		GoOS:                   runtime.GOOS,
		GoArch:                 runtime.GOARCH,
		CPUCount:               runtime.NumCPU(),
		GOMAXPROCS:             runtime.GOMAXPROCS(0),
		BuildFlags:             buildFlags,
		SizeBytes:              info.Size(),
		SamplesPerWorkload:     *samples,
		Workloads:              workloadNames,
		Tokenizer:              "offline-word-v1",
		FixtureSeed:            benchmark.FixtureSeed,
		FixtureSHA256:          benchmark.FixtureHash(),
		TokenApplicability:     "structural output comparison only; model-token advantage unmeasured",
		CacheWorkload:          "unimplemented pending issue #6",
		RefreshWorkload:        "unimplemented pending issue #5",
		ObservedSubprocesses:   len(workloads) * *samples,
		ObservedRequests:       0,
		AllocationsMeasurement: "go test -bench -benchmem output in artifacts/benchmark-in-process.txt",
	})

	byWorkload := make(map[string][]int64, len(workloads))
	for _, item := range workloads {
		for n := 1; n <= *samples; n++ {
			result := run(*binary, item)
			if result.ExitCode != item.ExitCode {
				fatalf("%s sample %d exited %d, want %d", item.Name, n, result.ExitCode, item.ExitCode)
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

	summaries := make(map[string]benchmark.Summary, len(byWorkload))
	for name, samples := range byWorkload {
		value, err := benchmark.Summarize(samples)
		if err != nil {
			fatalf("summarize %s: %v", name, err)
		}
		summaries[name] = value
	}
	writeJSON(summary{Kind: "summary", Source: "full-process-cobra-entrypoint", Workloads: summaries})
}

func run(binary string, item workload) sample {
	start := time.Now()
	command := exec.Command(binary, item.Args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	exitCode := 0
	if err != nil {
		exitCode = 125
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	return sample{
		ElapsedNS:       time.Since(start).Nanoseconds(),
		ExitCode:        exitCode,
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		StdoutBytes:     stdout.Len(),
		StderrBytes:     stderr.Len(),
		StdoutTokens:    benchmark.TokenCount(stdout.String()),
		StderrTokens:    benchmark.TokenCount(stderr.String()),
		StdoutSHA256:    digest(stdout.Bytes()),
		StderrSHA256:    digest(stderr.Bytes()),
		SubprocessCount: 1,
		RequestCount:    0,
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

func writeJSON(value interface{}) {
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		fatalf("write JSON: %v", err)
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
