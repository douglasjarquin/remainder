package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/douglasjarquin/remainder/internal/benchmark"
)

func checkLatencyRegression(current map[string]benchmark.Summary, path string) latencyRecord {
	record := latencyRecord{Kind: "latency-regression", Status: "not-run", Scope: "optional seeded p95 baseline; startup and failure trends are not checked against the eligible-cache 10 ms objective"}
	if path == "" {
		return record
	}
	data, err := os.ReadFile(path)
	if err != nil {
		record.Status = "failed"
		record.Baseline = path
		record.Failures = []string{"latency baseline: " + err.Error()}
		return record
	}
	var baseline struct {
		Workloads map[string]benchmark.Summary `json:"workloads"`
	}
	if err := json.Unmarshal(data, &baseline); err != nil {
		record.Status = "failed"
		record.Baseline = path
		record.Failures = []string{"latency baseline: " + err.Error()}
		return record
	}
	record.Status = "passed"
	record.Baseline = path
	for name, expected := range baseline.Workloads {
		actual, ok := current[name]
		if !ok {
			record.Status = "failed"
			record.Failures = append(record.Failures, "latency baseline workload missing: "+name)
			continue
		}
		if err := benchmark.DetectLatencyRegression(actual, expected); err != nil {
			record.Status = "failed"
			record.Failures = append(record.Failures, name+": "+err.Error())
		}
	}
	return record
}

func run(binary string, item workload, tokenizer *tokenizerClient) (sample, error) {
	start := time.Now()
	command := exec.Command(binary, item.Args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	elapsedNS := time.Since(start).Nanoseconds()
	exitCode := 0
	if err != nil {
		exitCode = 125
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
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
		Source:          "full-process-cobra-entrypoint",
		ElapsedNS:       elapsedNS,
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
	if err := json.NewEncoder(c.stdin).Encode(map[string]any{"id": c.nextID, "text": text}); err != nil {
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
