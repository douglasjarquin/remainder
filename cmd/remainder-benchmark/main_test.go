package main

import (
	"strings"
	"testing"

	"github.com/douglasjarquin/remainder/internal/benchmark"
)

func TestValidateOutputRequiresExactHelpGolden(t *testing.T) {
	item := workload{Name: "startup-help", ExitCode: 0}
	result := sample{Stdout: expectedHelp, OutputStatus: "pass"}
	if got := validateOutput(item, result); got != "pass" {
		t.Fatalf("validateOutput() = %q, want pass", got)
	}
	result.Stdout += "GARBAGE OUTPUT THAT MUST NOT PASS\n"
	if got := validateOutput(item, result); got == "pass" {
		t.Fatalf("validateOutput() accepted appended help output")
	}
}

func TestMeasureFixturePreservesEquivalentRequiredFacts(t *testing.T) {
	for _, fixture := range benchmark.Fixtures() {
		record, err := measureFixture(fixture, nil)
		if err != nil {
			t.Fatalf("measureFixture(%q) error = %v", fixture.Name, err)
		}
		if record.ComparisonStatus != "equivalent-required-facts" || len(record.ComparableFormats) != 2 {
			t.Fatalf("measureFixture(%q) status = %q formats = %v", fixture.Name, record.ComparisonStatus, record.ComparableFormats)
		}
		if record.FixtureClock != benchmark.FixtureClock || record.FixtureSHA256 != benchmark.FixtureHashFor(fixture) {
			t.Fatalf("measureFixture(%q) provenance = clock %q hash %q", fixture.Name, record.FixtureClock, record.FixtureSHA256)
		}
		if fixture.Name == "partial-unknown" && !strings.Contains(record.Formats[0].Stdout, "unit=tokens") {
			t.Fatalf("partial-unknown compact output = %q, want unit", record.Formats[0].Stdout)
		}
	}
}

func TestCompareSharedPercentFactsReportsPartialScope(t *testing.T) {
	// Given: actual quota-axi JSON's relevant provider, freshness, and weekly percentage shape.
	output := comparatorOutput{ExitCode: 0, Stdout: `{"generatedAt":"2026-03-08T07:30:00.000Z","providers":[{"provider":"codex","state":{"status":"fresh"},"windows":[{"id":"weekly","kind":"weekly","percentRemaining":42}],"quotaSemantics":{"effectiveAvailability":[{"scope":"all_models"}]}}]}`}

	// When: the comparator checks the declared shared subset.
	status, shared, extra, uncertainty := compareSharedPercentFacts(output)

	// Then: it accepts the subset while preserving non-equivalent facts and scope uncertainty.
	if status != "observed-shared-percent-subset" || len(shared) != 8 || len(extra) != 2 || !strings.Contains(uncertainty, "five_hour") {
		t.Fatalf("comparison = status %q shared %v extra %v uncertainty %q", status, shared, extra, uncertainty)
	}
}

func TestCompareSharedPercentFactsRejectsSeededMismatch(t *testing.T) {
	// Given: quota-axi JSON with a deliberately different percentage.
	output := comparatorOutput{ExitCode: 0, Stdout: `{"generatedAt":"2026-03-08T07:30:00.000Z","providers":[{"provider":"codex","state":{"status":"fresh"},"windows":[{"id":"weekly","kind":"weekly","percentRemaining":41}],"quotaSemantics":{"effectiveAvailability":[{"scope":"all_models"}]}}]}`}

	// When: the comparator checks the declared shared subset.
	status, _, _, _ := compareSharedPercentFacts(output)

	// Then: the mismatch is observable.
	if status != "shared-facts-mismatch" {
		t.Fatalf("comparison status = %q, want shared-facts-mismatch", status)
	}
}

func TestComparatorOutputValidatorsRejectMissingOutputs(t *testing.T) {
	// Given: the shared JSON facts match while compact and jq output are absent.
	missing := comparatorOutput{Status: "observed"}

	// When: each real output path is validated independently.
	compactStatus := compactComparatorStatus("observed-shared-percent-subset", missing)
	jqStatus := pinchosComparatorStatus("observed-shared-percent-subset", comparatorOutput{Status: "observed"}, missing)

	// Then: neither path can inherit a passing JSON fact status.
	if compactStatus != "compact-output-mismatch" || jqStatus != "json-jq-output-mismatch" {
		t.Fatalf("output statuses = compact %q jq %q", compactStatus, jqStatus)
	}
}

func TestComparatorFailuresRejectsSeededMismatch(t *testing.T) {
	// Given: one comparator reports a seeded shared-fact mismatch.
	comparators := []comparator{{Name: "quota-axi-compact", Status: "shared-facts-mismatch", CacheSnapshot: comparatorCache{Status: "fresh-snapshot-written"}, SandboxCleanup: "removed-owned-temporary-sandbox"}}

	// When: the benchmark evaluates comparator results.
	failures := comparatorFailures(comparators)

	// Then: the run has a concrete regression failure.
	if len(failures) != 1 || !strings.Contains(failures[0], "shared-facts-mismatch") {
		t.Fatalf("comparator failures = %v", failures)
	}
}

func TestComparatorFailuresRequiresSandboxCleanup(t *testing.T) {
	for _, cleanup := range []string{"failed: permission denied", "", "removed-owned-temporary-sandbox"} {
		t.Run(cleanup, func(t *testing.T) {
			comparators := []comparator{{Name: "quota-axi-compact", Status: "observed-shared-percent-subset", CacheSnapshot: comparatorCache{Status: "fresh-snapshot-written"}, SandboxCleanup: cleanup}}
			failures := comparatorFailures(comparators)
			wantFailure := cleanup != "removed-owned-temporary-sandbox"
			if (len(failures) != 0) != wantFailure {
				t.Fatalf("cleanup %q produced failures %v, want failure %t", cleanup, failures, wantFailure)
			}
		})
	}
}
