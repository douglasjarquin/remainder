package main

import (
	"context"

	"github.com/douglasjarquin/remainder/internal/benchmark"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

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
	StdoutTokens    *int     `json:"stdout_tokens,omitempty"`
	StderrTokens    *int     `json:"stderr_tokens,omitempty"`
	StdoutSHA256    string   `json:"stdout_sha256"`
	StderrSHA256    string   `json:"stderr_sha256"`
	SubprocessCount int      `json:"subprocess_count"`
	RequestCount    int      `json:"request_count"`
	OutputStatus    string   `json:"output_status"`
}

type metadata struct {
	Kind                   string            `json:"kind"`
	Source                 string            `json:"source"`
	Binary                 string            `json:"binary"`
	Version                string            `json:"version"`
	Machine                string            `json:"machine"`
	GoVersion              string            `json:"driver_go_version"`
	GoOS                   string            `json:"driver_goos"`
	GoArch                 string            `json:"driver_goarch"`
	CPUCount               int               `json:"cpu_count"`
	GOMAXPROCS             int               `json:"gomaxprocs"`
	Host                   hostMetadata      `json:"host"`
	Build                  buildMetadata     `json:"measured_binary_build"`
	SizeBytes              int64             `json:"size_bytes"`
	SamplesPerWorkload     int               `json:"samples_per_workload"`
	Workloads              []string          `json:"workloads"`
	Tokenizer              tokenizerMetadata `json:"tokenizer"`
	FixtureSeed            string            `json:"fixture_seed"`
	FixtureClock           string            `json:"fixture_clock"`
	FixtureSHA256          string            `json:"fixture_sha256"`
	TokenApplicability     string            `json:"token_applicability"`
	CacheWorkload          string            `json:"cache_workload"`
	RefreshWorkload        string            `json:"refresh_workload"`
	ObservedSubprocesses   int               `json:"observed_subprocesses"`
	ObservedRequests       int               `json:"observed_requests"`
	AllocationsMeasurement string            `json:"allocations_measurement"`
	P95ObjectiveNS         int64             `json:"p95_objective_ns"`
	P95ObjectiveScope      string            `json:"p95_objective_scope"`
	P95Policy              string            `json:"p95_policy"`
	Comparators            []comparator      `json:"comparators"`
}

type latencyRecord struct {
	Kind     string   `json:"kind"`
	Status   string   `json:"status"`
	Scope    string   `json:"scope"`
	Baseline string   `json:"baseline,omitempty"`
	Failures []string `json:"failures,omitempty"`
}

type summary struct {
	Kind      string                       `json:"kind"`
	Source    string                       `json:"source"`
	Workloads map[string]benchmark.Summary `json:"workloads"`
}

type hostMetadata struct {
	OSVersion string `json:"os_version"`
	Kernel    string `json:"kernel"`
	CPUModel  string `json:"cpu_model"`
}

type buildMetadata struct {
	Status            string            `json:"status"`
	Raw               string            `json:"raw,omitempty"`
	GoVersion         string            `json:"go_version,omitempty"`
	Path              string            `json:"path,omitempty"`
	SourceRevision    string            `json:"source_revision,omitempty"`
	SourceModified    string            `json:"source_modified,omitempty"`
	BuildSettings     map[string]string `json:"build_settings,omitempty"`
	Dependencies      []string          `json:"dependencies,omitempty"`
	UnavailableReason string            `json:"unavailable_reason,omitempty"`
}

type tokenizerMetadata struct {
	Status            string `json:"status"`
	Package           string `json:"package,omitempty"`
	Version           string `json:"version,omitempty"`
	Encoding          string `json:"encoding,omitempty"`
	Applicability     string `json:"applicability"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}

type comparator struct {
	Name            string             `json:"name"`
	Status          string             `json:"status"`
	Version         string             `json:"version,omitempty"`
	JQVersion       string             `json:"jq_version,omitempty"`
	ExpectedVersion string             `json:"expected_version"`
	Commands        [][]string         `json:"commands"`
	FixtureSHA256   string             `json:"fixture_sha256,omitempty"`
	Reason          string             `json:"reason,omitempty"`
	SharedFacts     []string           `json:"shared_facts,omitempty"`
	ExtraFacts      []string           `json:"extra_facts,omitempty"`
	Uncertainty     string             `json:"uncertainty,omitempty"`
	Harness         *comparatorOutput  `json:"harness,omitempty"`
	HarnessReason   string             `json:"harness_reason,omitempty"`
	CacheSnapshot   comparatorCache    `json:"cache_snapshot"`
	SandboxCleanup  string             `json:"sandbox_cleanup,omitempty"`
	Outputs         []comparatorOutput `json:"outputs,omitempty"`
}

type comparatorCache struct {
	Status string `json:"status"`
	Path   string `json:"path"`
	Bytes  int    `json:"bytes,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

type comparatorOutput struct {
	Format       string `json:"format"`
	Status       string `json:"status"`
	ExitCode     int    `json:"exit_code"`
	Stdout       string `json:"stdout,omitempty"`
	Stderr       string `json:"stderr,omitempty"`
	Bytes        int    `json:"bytes"`
	Tokens       *int   `json:"tokens,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	ElapsedNS    int64  `json:"elapsed_ns"`
	ProcessCount int    `json:"process_count"`
}

type fixtureRecord struct {
	Kind              string          `json:"kind"`
	Fixture           string          `json:"fixture"`
	FixtureClock      string          `json:"fixture_clock"`
	FixtureSHA256     string          `json:"fixture_sha256"`
	RequiredFacts     []string        `json:"required_facts"`
	ComparableFormats []string        `json:"comparable_formats"`
	ScalarProjection  string          `json:"scalar_projection"`
	ComparisonStatus  string          `json:"comparison_status"`
	ComparisonReason  string          `json:"comparison_reason,omitempty"`
	Formats           []fixtureFormat `json:"formats"`
}

type fixtureFormat struct {
	Format       string   `json:"format"`
	Args         []string `json:"args"`
	ExitCode     int      `json:"exit_code"`
	Stdout       string   `json:"stdout"`
	Stderr       string   `json:"stderr"`
	StdoutBytes  int      `json:"stdout_bytes"`
	StderrBytes  int      `json:"stderr_bytes"`
	StdoutTokens *int     `json:"stdout_tokens,omitempty"`
	StderrTokens *int     `json:"stderr_tokens,omitempty"`
	StdoutSHA256 string   `json:"stdout_sha256"`
	StderrSHA256 string   `json:"stderr_sha256"`
	Information  string   `json:"information"`
}

type fixtureAdapter struct {
	observation evidence.Observation
}

func (a fixtureAdapter) Observe(context.Context, evidence.Request) (evidence.Observation, error) {
	return a.observation, nil
}
