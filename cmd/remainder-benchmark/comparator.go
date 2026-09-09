package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const pinchosFilter = `.providers[0].windows[] | select(.label=="week") | .percentRemaining`

func notRunComparators() []comparator {
	return []comparator{
		{Name: "quota-axi-compact", Status: "not-run", ExpectedVersion: quotaAxiVersion, Commands: [][]string{{"quota-axi", "--provider", "codex", "--no-credential-refresh"}}, Reason: "optional developer comparator disabled"},
		{Name: "pinchos-quota-axi-json-jq", Status: "not-run", ExpectedVersion: quotaAxiVersion, Commands: [][]string{{"quota-axi", "--provider", "codex", "--json", "--no-credential-refresh"}, {"jq", "-r", pinchosFilter}}, Reason: "optional controlled consumer probe disabled"},
	}
}

func runComparators(tokenizer *tokenizerClient, preload string) []comparator {
	quotaPath, quotaErr := exec.LookPath("quota-axi")
	jqPath, jqErr := exec.LookPath("jq")
	nodePath, nodeErr := exec.LookPath("node")
	if quotaErr != nil || jqErr != nil || nodeErr != nil {
		return []comparator{
			{Name: "quota-axi-compact", Status: "unavailable", ExpectedVersion: quotaAxiVersion, Commands: [][]string{{"quota-axi", "--provider", "codex", "--no-credential-refresh"}}, Reason: "preinstalled comparator missing: quota-axi=" + errorText(quotaErr) + ", jq=" + errorText(jqErr) + ", node=" + errorText(nodeErr)},
			{Name: "pinchos-quota-axi-json-jq", Status: "unavailable", ExpectedVersion: quotaAxiVersion, Commands: [][]string{{"quota-axi", "--provider", "codex", "--json", "--no-credential-refresh"}, {"jq", "-r", pinchosFilter}}, Reason: "preinstalled comparator missing: quota-axi=" + errorText(quotaErr) + ", jq=" + errorText(jqErr) + ", node=" + errorText(nodeErr)},
		}
	}
	version, err := boundedOutput(quotaPath, "--version")
	if err != nil {
		return []comparator{{Name: "quota-axi-compact", Status: "unavailable", ExpectedVersion: quotaAxiVersion, Commands: [][]string{{"quota-axi", "--version"}}, Reason: "quota-axi version probe failed: " + err.Error()}}
	}
	quotaVersion := strings.TrimSpace(string(version))
	jqVersionBytes, jqVersionErr := boundedOutput(jqPath, "--version")
	if jqVersionErr != nil {
		return []comparator{{Name: "pinchos-quota-axi-json-jq", Status: "unavailable", Version: quotaVersion, ExpectedVersion: quotaAxiVersion, Commands: [][]string{{"quota-axi", "--version"}, {"jq", "--version"}}, Reason: "jq version probe failed: " + jqVersionErr.Error()}}
	}
	if quotaVersion != quotaAxiVersion {
		return []comparator{{Name: "quota-axi-compact", Status: "version-mismatch", Version: quotaVersion, ExpectedVersion: quotaAxiVersion, Commands: [][]string{{"quota-axi", "--version"}}, Reason: "preinstalled quota-axi version does not match the pinned comparator version"}}
	}
	preloadPath, err := filepath.Abs(preload)
	if err != nil {
		return unavailableComparators(quotaVersion, "resolve quota-axi preload: "+err.Error())
	}
	preloadBytes, err := os.ReadFile(preloadPath)
	if err != nil {
		return unavailableComparators(quotaVersion, "read quota-axi preload: "+err.Error())
	}
	sandbox, err := newComparatorSandbox(preloadPath)
	if err != nil {
		return unavailableComparators(quotaVersion, "create quota-axi sandbox: "+err.Error())
	}

	jqVersion := strings.TrimSpace(string(jqVersionBytes))
	harness := runComparatorCommand(nodePath, []string{"--import", preloadPath, "-e", ""}, sandbox.env, nil, tokenizer, "node-preload-startup")
	compact := runComparatorCommand(quotaPath, []string{"--provider", "codex", "--no-credential-refresh"}, sandbox.env, nil, tokenizer, "quota-axi-compact")
	quotaJSON := runComparatorCommand(quotaPath, []string{"--provider", "codex", "--json", "--no-credential-refresh"}, sandbox.env, nil, tokenizer, "quota-axi-json")
	jqOutput := runComparatorCommand(jqPath, []string{"-r", pinchosFilter}, sandbox.env, strings.NewReader(quotaJSON.Stdout), tokenizer, "pinchos-json-jq")
	cacheSnapshot := inspectComparatorCache(sandbox.root)
	status, sharedFacts, extraFacts, uncertainty := compareSharedPercentFacts(quotaJSON)
	compactStatus := compactComparatorStatus(status, compact)
	jqStatus := pinchosComparatorStatus(status, quotaJSON, jqOutput)
	fixtureHash := digest(preloadBytes)
	comparators := []comparator{
		{Name: "quota-axi-compact", Status: compactStatus, Version: quotaVersion, ExpectedVersion: quotaAxiVersion, Commands: [][]string{{"node", "--import", preloadPath, "-e", ""}, {"quota-axi", "--provider", "codex", "--no-credential-refresh"}}, FixtureSHA256: fixtureHash, Reason: "actual pinned compact output under a fixed synthetic OAuth response; elapsed time includes Node and fixture harness startup", SharedFacts: sharedFacts, ExtraFacts: extraFacts, Uncertainty: uncertainty, Harness: &harness, HarnessReason: "separate Node preload startup calibration; not subtracted and not native endpoint performance", CacheSnapshot: cacheSnapshot, Outputs: []comparatorOutput{compact}},
		{Name: "pinchos-quota-axi-json-jq", Status: jqStatus, Version: quotaVersion, JQVersion: jqVersion, ExpectedVersion: quotaAxiVersion, Commands: [][]string{{"quota-axi", "--provider", "codex", "--json", "--no-credential-refresh"}, {"jq", "-r", pinchosFilter}}, FixtureSHA256: fixtureHash, Reason: "actual pinned JSON output feeds the real Pinchos jq projection under the same synthetic response", SharedFacts: sharedFacts, ExtraFacts: extraFacts, Uncertainty: uncertainty, Harness: &harness, HarnessReason: "separate Node preload startup calibration; not subtracted and not native endpoint performance", CacheSnapshot: cacheSnapshot, Outputs: []comparatorOutput{quotaJSON, jqOutput}},
	}
	cleanup := cleanupComparatorSandbox(sandbox.root)
	for index := range comparators {
		comparators[index].SandboxCleanup = cleanup
	}
	return comparators
}

func boundedOutput(path string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, path, args...).Output()
}

func unavailableComparators(version, reason string) []comparator {
	return []comparator{
		{Name: "quota-axi-compact", Status: "unavailable", Version: version, ExpectedVersion: quotaAxiVersion, Commands: [][]string{{"quota-axi", "--provider", "codex", "--no-credential-refresh"}}, Reason: reason},
		{Name: "pinchos-quota-axi-json-jq", Status: "unavailable", Version: version, ExpectedVersion: quotaAxiVersion, Commands: [][]string{{"quota-axi", "--provider", "codex", "--json", "--no-credential-refresh"}, {"jq", "-r", pinchosFilter}}, Reason: reason},
	}
}

func comparatorFailures(comparators []comparator) []string {
	var failures []string
	for _, comparator := range comparators {
		if comparator.Status != "observed-shared-percent-subset" {
			failures = append(failures, comparator.Name+" comparator status "+comparator.Status)
		}
		for _, output := range comparator.Outputs {
			if output.Status != "observed" {
				failures = append(failures, comparator.Name+" "+output.Format+" output status "+output.Status)
			}
		}
		if comparator.CacheSnapshot.Status != "fresh-snapshot-written" {
			failures = append(failures, comparator.Name+" cache status "+comparator.CacheSnapshot.Status)
		}
		if comparator.SandboxCleanup != "removed-owned-temporary-sandbox" {
			failures = append(failures, comparator.Name+" sandbox cleanup "+comparator.SandboxCleanup)
		}
	}
	return failures
}

func errorText(err error) string {
	if err == nil {
		return "present"
	}
	return err.Error()
}

func runComparatorCommand(path string, args, env []string, input io.Reader, tokenizer *tokenizerClient, format string) comparatorOutput {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path, args...)
	command.Env = env
	command.Stdin = input
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	start := time.Now()
	err := command.Run()
	elapsed := time.Since(start).Nanoseconds()
	status := "observed"
	exitCode := 0
	if err != nil {
		status = "unavailable: " + err.Error()
		exitCode = 125
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			exitCode = exitErr.ExitCode()
		}
	}
	tokens, tokenErr := countTokens(tokenizer, stdout.String())
	if tokenErr != nil {
		status = "tokenization failed: " + tokenErr.Error()
	}
	return comparatorOutput{Format: format, Status: status, ExitCode: exitCode, Stdout: stdout.String(), Stderr: stderr.String(), Bytes: stdout.Len(), Tokens: tokens, SHA256: digest(stdout.Bytes()), ElapsedNS: elapsed, ProcessCount: 1}
}

func compareSharedPercentFacts(output comparatorOutput) (string, []string, []string, string) {
	var report struct {
		GeneratedAt string `json:"generatedAt"`
		Providers   []struct {
			Provider string `json:"provider"`
			State    struct {
				Status string `json:"status"`
			} `json:"state"`
			Windows []struct {
				ID               string  `json:"id"`
				Kind             string  `json:"kind"`
				PercentRemaining float64 `json:"percentRemaining"`
			} `json:"windows"`
			QuotaSemantics struct {
				EffectiveAvailability []struct {
					Scope string `json:"scope"`
				} `json:"effectiveAvailability"`
			} `json:"quotaSemantics"`
		} `json:"providers"`
	}
	if output.ExitCode != 0 || json.Unmarshal([]byte(output.Stdout), &report) != nil || len(report.Providers) != 1 {
		return "unavailable", nil, nil, "quota-axi JSON was not an observed single-provider report"
	}
	provider := report.Providers[0]
	allModels := len(provider.QuotaSemantics.EffectiveAvailability) == 1 && provider.QuotaSemantics.EffectiveAvailability[0].Scope == "all_models"
	for _, window := range provider.Windows {
		if report.GeneratedAt == "2026-03-08T07:30:00.000Z" && provider.Provider == "codex" && provider.State.Status == "fresh" && allModels && window.ID == "weekly" && window.Kind == "weekly" && window.PercentRemaining == 42 {
			return "observed-shared-percent-subset",
				[]string{"provider=codex", "observed_at=2026-03-08T07:30:00Z", "age_seconds=0", "freshness=fresh", "scope=account/all_models", "window.id=weekly", "window.unit=percent", "remaining=42"},
				[]string{"remainder: profile, historical account binding, fixture source, outcome, limit field/state", "quota-axi: five_hour session constraint, reset times, plan, pace"},
				"The account/all_models weekly percentage subset is comparable. Provenance and full payloads remain partially different, and quota-axi also constrains the result with its five_hour window. Equal values do not imply token and percentage units are interchangeable."
		}
	}
	return "shared-facts-mismatch", nil, nil, "expected fresh codex weekly percentage remaining=42 was absent"
}

func compactComparatorStatus(sharedStatus string, output comparatorOutput) string {
	if sharedStatus != "observed-shared-percent-subset" {
		return sharedStatus
	}
	if output.Status != "observed" || !strings.Contains(output.Stdout, "codex,all_models,42") || !strings.Contains(output.Stdout, "five_hour + weekly") {
		return "compact-output-mismatch"
	}
	return sharedStatus
}

func pinchosComparatorStatus(sharedStatus string, jsonOutput, jqOutput comparatorOutput) string {
	if sharedStatus != "observed-shared-percent-subset" {
		return sharedStatus
	}
	if jsonOutput.Status != "observed" || jqOutput.Status != "observed" || jqOutput.Stdout != "42\n" {
		return "json-jq-output-mismatch"
	}
	return sharedStatus
}
