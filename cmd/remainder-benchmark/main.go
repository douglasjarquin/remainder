package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/douglasjarquin/remainder/internal/benchmark"
	"github.com/douglasjarquin/remainder/internal/cli"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

const (
	appleSiliconP95ObjectiveNS int64 = 10_000_000
	quotaAxiVersion                  = "0.1.41"
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
	ExpectedVersion string             `json:"expected_version"`
	Commands        [][]string         `json:"commands"`
	Reason          string             `json:"reason,omitempty"`
	Outputs         []comparatorOutput `json:"outputs,omitempty"`
}

type comparatorOutput struct {
	Format string `json:"format"`
	Status string `json:"status"`
	Bytes  int    `json:"bytes"`
	Tokens *int   `json:"tokens,omitempty"`
	SHA256 string `json:"sha256"`
}

type fixtureRecord struct {
	Kind              string          `json:"kind"`
	Fixture           string          `json:"fixture"`
	FixtureSHA256     string          `json:"fixture_sha256"`
	RequiredFacts     []string        `json:"required_facts"`
	ComparableFormats []string        `json:"comparable_formats"`
	ScalarProjection  string          `json:"scalar_projection"`
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

func main() {
	binary := flag.String("binary", "./bin/remainder", "compiled remainder binary to measure")
	samples := flag.Int("samples", 10, "samples per workload")
	tokenizerPython := flag.String("tokenizer-python", "", "optional Python executable with tiktoken installed")
	comparators := flag.Bool("comparators", false, "run cache-only preinstalled quota-axi comparison probes")
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
	tokenizer, tokenizerInfo, err := startTokenizer(*tokenizerPython)
	if err != nil {
		fatalf("start tokenizer: %v", err)
	}
	buildInfo := inspectBuild(*binary)
	host := inspectHost()
	var comparison []comparator
	if *comparators {
		comparison = runComparators(tokenizer)
	} else {
		comparison = []comparator{{Name: "quota-axi", Status: "not-run", ExpectedVersion: quotaAxiVersion, Commands: comparatorCommands(), Reason: "optional probe disabled; no provider or cache access is part of the normal benchmark"}}
	}
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
		Host:                   host,
		Build:                  buildInfo,
		SizeBytes:              info.Size(),
		SamplesPerWorkload:     *samples,
		Workloads:              workloadNames,
		Tokenizer:              tokenizerInfo,
		FixtureSeed:            benchmark.FixtureSeed,
		FixtureSHA256:          benchmark.FixtureHash(),
		TokenApplicability:     "o200k_base is an offline Codex-family comparison encoding; model-specific tokenizer certification remains outside this baseline",
		CacheWorkload:          "unimplemented pending issue #6",
		RefreshWorkload:        "unimplemented pending issue #5",
		ObservedSubprocesses:   len(workloads) * *samples,
		ObservedRequests:       0,
		AllocationsMeasurement: "go test -bench -benchmem output in artifacts/benchmark-in-process.txt",
		P95ObjectiveNS:         appleSiliconP95ObjectiveNS,
		P95ObjectiveScope:      "eligible cache reads on the declared Apple Silicon host; cache is unimplemented pending issue #6",
		P95Policy:              "full-process startup and failure summaries are trend evidence; no gate is applied to them",
		Comparators:            comparison,
	})

	for _, fixture := range benchmark.Fixtures() {
		record, err := measureFixture(fixture, tokenizer)
		if err != nil {
			fatalf("measure fixture %s: %v", fixture.Name, err)
		}
		writeJSON(record)
	}

	byWorkload := make(map[string][]int64, len(workloads))
	var regressions []string
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

	summaries := make(map[string]benchmark.Summary, len(byWorkload))
	for name, samples := range byWorkload {
		value, err := benchmark.Summarize(samples)
		if err != nil {
			fatalf("summarize %s: %v", name, err)
		}
		summaries[name] = value
	}
	if len(regressions) > 0 {
		writeJSON(map[string]interface{}{"kind": "regression", "source": "full-process-cobra-entrypoint", "status": "failed", "failures": regressions})
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

func run(binary string, item workload, tokenizer *tokenizerClient) (sample, error) {
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
	stdoutTokens, err := countTokens(tokenizer, stdout.String())
	if err != nil {
		return sample{}, err
	}
	stderrTokens, err := countTokens(tokenizer, stderr.String())
	if err != nil {
		return sample{}, err
	}
	return sample{
		ElapsedNS:       time.Since(start).Nanoseconds(),
		ExitCode:        exitCode,
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		StdoutBytes:     stdout.Len(),
		StderrBytes:     stderr.Len(),
		StdoutTokens:    stdoutTokens,
		StderrTokens:    stderrTokens,
		StdoutSHA256:    digest(stdout.Bytes()),
		StderrSHA256:    digest(stderr.Bytes()),
		SubprocessCount: 1,
		RequestCount:    0,
	}, nil
}

type tokenizerClient struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	output  *json.Decoder
	nextID  int
}

func startTokenizer(python string) (*tokenizerClient, tokenizerMetadata, error) {
	if python == "" {
		return nil, tokenizerMetadata{Status: "unmeasured", Applicability: "o200k_base Codex-family comparison; provide --tokenizer-python to measure"}, nil
	}
	script, err := filepath.Abs(filepath.Join("scripts", "benchmark_tokens.py"))
	if err != nil {
		return nil, tokenizerMetadata{}, err
	}
	command := exec.Command(python, script, "--encoding", "o200k_base")
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, tokenizerMetadata{}, err
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, tokenizerMetadata{}, err
	}
	if err := command.Start(); err != nil {
		return nil, tokenizerMetadata{}, err
	}
	var info tokenizerMetadata
	if err := json.NewDecoder(stdout).Decode(&info); err != nil {
		return nil, tokenizerMetadata{}, err
	}
	if info.Status == "" {
		info.Status = "measured"
	}
	info.Applicability = "o200k_base Codex-family comparison; not a model-specific tokenizer certification"
	return &tokenizerClient{command: command, stdin: stdin, output: json.NewDecoder(stdout)}, info, nil
}

func (c *tokenizerClient) Count(text string) (*int, error) {
	c.nextID++
	if err := json.NewEncoder(c.stdin).Encode(map[string]interface{}{"id": c.nextID, "text": text}); err != nil {
		return nil, err
	}
	var response struct {
		ID     int `json:"id"`
		Tokens int `json:"tokens"`
	}
	if err := c.output.Decode(&response); err != nil {
		return nil, err
	}
	if response.ID != c.nextID {
		return nil, fmt.Errorf("tokenizer response id %d, want %d", response.ID, c.nextID)
	}
	return &response.Tokens, nil
}

func (c *tokenizerClient) Close() error {
	if err := c.stdin.Close(); err != nil {
		return err
	}
	return c.command.Wait()
}

func countTokens(tokenizer *tokenizerClient, text string) (*int, error) {
	if tokenizer == nil {
		return nil, nil
	}
	return tokenizer.Count(text)
}

func inspectBuild(binary string) buildMetadata {
	output, err := exec.Command("go", "version", "-m", binary).Output()
	if err != nil {
		return buildMetadata{Status: "unknown", UnavailableReason: err.Error()}
	}
	raw := string(output)
	metadata := buildMetadata{Status: "observed", Raw: raw, GoVersion: "unknown", Path: "unknown", SourceRevision: "unknown", SourceModified: "unknown", BuildSettings: make(map[string]string)}
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) > 0 {
		metadata.GoVersion = strings.TrimSpace(strings.TrimPrefix(lines[0], binary+":"))
	}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "path":
			metadata.Path = fields[1]
		case "dep":
			metadata.Dependencies = append(metadata.Dependencies, strings.Join(fields[1:], " "))
		case "build":
			key, value, found := strings.Cut(strings.Join(fields[1:], " "), "=")
			if found {
				metadata.BuildSettings[key] = value
				if key == "vcs.revision" {
					metadata.SourceRevision = value
				}
				if key == "vcs.modified" {
					metadata.SourceModified = value
				}
			}
		}
	}
	return metadata
}

func inspectHost() hostMetadata {
	return hostMetadata{
		OSVersion: probe("sw_vers", "-productVersion"),
		Kernel:    probe("uname", "-sr"),
		CPUModel:  probe("sysctl", "-n", "hw.model"),
	}
}

func probe(name string, args ...string) string {
	output, err := exec.Command(name, args...).Output()
	if err != nil {
		return "unknown"
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "unknown"
	}
	return value
}

func comparatorCommands() [][]string {
	return [][]string{
		{"quota-axi", "--provider", "codex", "--no-credential-refresh"},
		{"quota-axi", "--provider", "codex", "--json", "--no-credential-refresh"},
		{"jq", "-c", "."},
	}
}

func runComparators(tokenizer *tokenizerClient) []comparator {
	commands := comparatorCommands()
	quotaPath, quotaErr := exec.LookPath("quota-axi")
	jqPath, jqErr := exec.LookPath("jq")
	result := comparator{Name: "quota-axi", ExpectedVersion: quotaAxiVersion, Commands: commands}
	if quotaErr != nil || jqErr != nil {
		result.Status = "unavailable"
		result.Reason = "preinstalled comparator missing: quota-axi=" + errorText(quotaErr) + ", jq=" + errorText(jqErr)
		return []comparator{result}
	}
	version, err := exec.Command(quotaPath, "--version").Output()
	if err != nil {
		result.Status = "unavailable"
		result.Reason = "quota-axi version probe failed: " + err.Error()
		return []comparator{result}
	}
	result.Version = strings.TrimSpace(string(version))
	if result.Version != quotaAxiVersion {
		result.Status = "version-mismatch"
		result.Reason = "preinstalled quota-axi version does not match the pinned comparator version"
		return []comparator{result}
	}
	compact, compactErr := exec.Command(quotaPath, "--provider", "codex", "--no-credential-refresh").Output()
	jsonOutput, jsonErr := exec.Command(quotaPath, "--provider", "codex", "--json", "--no-credential-refresh").Output()
	var jqOutput []byte
	var jqRunErr error
	if jsonErr != nil {
		jqRunErr = jsonErr
	} else {
		jq := exec.Command(jqPath, "-c", ".")
		jq.Stdin = bytes.NewReader(jsonOutput)
		jqOutput, jqRunErr = jq.Output()
	}
	result.Outputs = append(result.Outputs, comparatorResult("compact", compact, compactErr, tokenizer))
	result.Outputs = append(result.Outputs, comparatorResult("json-jq", jqOutput, jqRunErr, tokenizer))
	result.Status = "cache-only-not-equivalent"
	result.Reason = "read-only quota-axi output is cache/provider data, not a controlled fixture with equal freshness, scopes, and required facts"
	return []comparator{result}
}

func errorText(err error) string {
	if err == nil {
		return "present"
	}
	return err.Error()
}

func comparatorResult(format string, output []byte, err error, tokenizer *tokenizerClient) comparatorOutput {
	status := "observed"
	if err != nil {
		status = "unavailable: " + err.Error()
	}
	tokens, tokenErr := countTokens(tokenizer, string(output))
	if tokenErr != nil {
		status = "tokenization failed: " + tokenErr.Error()
	}
	return comparatorOutput{Format: format, Status: status, Bytes: len(output), Tokens: tokens, SHA256: digest(output)}
}

func measureFixture(fixture benchmark.Fixture, tokenizer *tokenizerClient) (fixtureRecord, error) {
	fixtureBytes, err := json.Marshal(fixture)
	if err != nil {
		return fixtureRecord{}, err
	}
	formats := []struct {
		name string
		args []string
		info string
	}{
		{name: "compact", args: []string{"--format", "compact", "--provider", "codex", "--profile", "main", "--window", "weekly"}, info: "full observation summary"},
		{name: "json", args: []string{"--format", "json", "--provider", "codex", "--profile", "main", "--window", "weekly"}, info: "full observation"},
		{name: "scalar", args: []string{"value", "--provider", "codex", "--profile", "main", "--window", "weekly", "--field", "remaining"}, info: "selected remaining value projection"},
	}
	record := fixtureRecord{
		Kind:              "fixture-comparison",
		Fixture:           fixture.Name,
		FixtureSHA256:     digest(fixtureBytes),
		RequiredFacts:     fixture.RequiredFacts,
		ComparableFormats: []string{"compact", "json"},
		ScalarProjection:  "selected remaining value; not equivalent to the full required-facts observation",
	}
	for _, format := range formats {
		var stdout, stderr bytes.Buffer
		code := cli.ExecuteWithAdapter(context.Background(), format.args, &stdout, &stderr, "v0.1.0", fixtureAdapter{observation: fixture.Observation})
		stdoutTokens, err := countTokens(tokenizer, stdout.String())
		if err != nil {
			return fixtureRecord{}, err
		}
		stderrTokens, err := countTokens(tokenizer, stderr.String())
		if err != nil {
			return fixtureRecord{}, err
		}
		record.Formats = append(record.Formats, fixtureFormat{
			Format: format.name, Args: format.args, ExitCode: code,
			Stdout: stdout.String(), Stderr: stderr.String(),
			StdoutBytes: stdout.Len(), StderrBytes: stderr.Len(),
			StdoutTokens: stdoutTokens, StderrTokens: stderrTokens,
			StdoutSHA256: digest(stdout.Bytes()), StderrSHA256: digest(stderr.Bytes()),
			Information: format.info,
		})
	}
	return record, nil
}

func validateOutput(item workload, result sample) string {
	if len(result.Stderr) > 0 && item.ExitCode == 0 {
		return "unexpected stderr"
	}
	switch item.Name {
	case "startup-help":
		if result.Stderr != "" || !strings.Contains(result.Stdout, "Usage:\n  remainder") || !strings.Contains(result.Stdout, "value") {
			return "help output mismatch"
		}
	case "startup-version":
		if result.Stdout != "remainder v0.1.0 (github.com/douglasjarquin/remainder)\n" || result.Stderr != "" {
			return "version output mismatch"
		}
	case "failure-unavailable":
		if result.Stdout != "" || result.Stderr != "remainder: no provider is implemented; quota is unavailable\n" {
			return "unavailable error output mismatch"
		}
	case "failure-invalid-freshness":
		if result.Stdout != "" || result.Stderr != "remainder: invalid command usage: unsupported freshness \"ignored\"\n" {
			return "invalid-freshness error output mismatch"
		}
	}
	return "pass"
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
