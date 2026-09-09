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
