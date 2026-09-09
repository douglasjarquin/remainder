package main

import (
	"bytes"
	"context"
	"strings"

	"github.com/douglasjarquin/remainder/internal/benchmark"
	"github.com/douglasjarquin/remainder/internal/cli"
)

func measureFixture(fixture benchmark.Fixture, tokenizer *tokenizerClient) (fixtureRecord, error) {
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
		Kind:             "fixture-comparison",
		Fixture:          fixture.Name,
		FixtureClock:     benchmark.FixtureClock,
		FixtureSHA256:    benchmark.FixtureHashFor(fixture),
		RequiredFacts:    fixture.RequiredFacts,
		ScalarProjection: "selected remaining value; not equivalent to the full required-facts observation",
		ComparisonStatus: "unresolved",
	}
	var comparisonIssues []string
	for _, format := range formats {
		var stdout, stderr bytes.Buffer
		code := cli.ExecuteWithAdapterAt(context.Background(), format.args, &stdout, &stderr, "v0.1.0", benchmark.FixtureTime(), fixtureAdapter{observation: fixture.Observation})
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
		wantCode, wantStdout, wantStderr := expectedFixtureOutput(fixture.Name, format.name)
		if code != wantCode || stdout.String() != wantStdout || stderr.String() != wantStderr {
			comparisonIssues = append(comparisonIssues, format.name+" output does not preserve the approved fixture facts")
		}
	}
	if len(comparisonIssues) == 0 {
		record.ComparableFormats = []string{"compact", "json"}
		record.ComparisonStatus = "equivalent-required-facts"
	} else {
		record.ComparisonReason = strings.Join(comparisonIssues, "; ")
	}
	return record, nil
}

func expectedFixtureOutput(fixture, format string) (int, string, string) {
	type golden struct {
		compact      string
		json         string
		scalar       string
		code         int
		stderr       string
		scalarCode   int
		scalarStderr string
	}
	goldens := map[string]golden{
		"healthy": {
			compact: `schema=v1 provider="codex" profile="main" observed_at=2026-03-08T07:00:00Z age_seconds=1800 freshness=fresh outcome=complete identity=historical account="acct-1" source="fixture/local" windows=weekly/model:remaining=42tokens` + "\n",
			json:    `{"schema_version":"v1","provider":"codex","profile":"main","account":{"last_observed":"acct-1","binding":"historical"},"source":{"kind":"fixture","name":"local"},"observed_at":"2026-03-08T07:00:00Z","freshness":"fresh","outcome":"complete","windows":[{"id":"weekly","scope":"model","unit":"tokens","limits":[{"id":"weekly-remaining","field":"remaining","state":"defined","amount":42}]}]}` + "\n",
			scalar:  "42\n",
		},
		"exhausted": {
			compact: `schema=v1 provider="codex" profile="main" observed_at=2026-03-08T07:00:00Z age_seconds=1800 freshness=fresh outcome=complete identity=historical account="acct-1" source="fixture/local" windows=weekly/model:remaining=0tokens` + "\n",
			json:    `{"schema_version":"v1","provider":"codex","profile":"main","account":{"last_observed":"acct-1","binding":"historical"},"source":{"kind":"fixture","name":"local"},"observed_at":"2026-03-08T07:00:00Z","freshness":"fresh","outcome":"complete","windows":[{"id":"weekly","scope":"model","unit":"tokens","limits":[{"id":"weekly-remaining","field":"remaining","state":"zero","amount":0}]}]}` + "\n",
			scalar:  "0\n",
		},
		"stale": {
			compact: `schema=v1 provider="codex" profile="main" observed_at=2026-03-08T07:00:00Z age_seconds=1800 freshness=stale outcome=complete identity=historical account="acct-1" source="fixture/local" windows=weekly/model:remaining=17tokens` + "\n",
			json:    `{"schema_version":"v1","provider":"codex","profile":"main","account":{"last_observed":"acct-1","binding":"historical"},"source":{"kind":"fixture","name":"local"},"observed_at":"2026-03-08T07:00:00Z","freshness":"stale","outcome":"complete","windows":[{"id":"weekly","scope":"model","unit":"tokens","limits":[{"id":"weekly-remaining","field":"remaining","state":"defined","amount":17}]}]}` + "\n",
			scalar:  "17\n",
		},
		"partial-unknown": {
			compact:      `schema=v1 provider="codex" profile="main" observed_at=2026-03-08T07:00:00Z age_seconds=1800 freshness=fresh outcome=partial identity=historical account="acct-1" source="fixture/local" windows=weekly/model:remaining=unknown unit=tokens failures="daily:source unavailable"` + "\n",
			json:         `{"schema_version":"v1","provider":"codex","profile":"main","account":{"last_observed":"acct-1","binding":"historical"},"source":{"kind":"fixture","name":"local"},"observed_at":"2026-03-08T07:00:00Z","freshness":"fresh","outcome":"partial","windows":[{"id":"weekly","scope":"model","unit":"tokens","limits":[{"id":"weekly-remaining","field":"remaining","state":"unknown"}]}],"failures":[{"scope":"daily","message":"source unavailable"}]}` + "\n",
			code:         3,
			stderr:       "remainder: partial evidence\n",
			scalarCode:   2,
			scalarStderr: "remainder: evidence value is undefined\n",
		},
		"shared-percent": {
			compact: `schema=v1 provider="codex" profile="main" observed_at=2026-03-08T07:30:00Z age_seconds=0 freshness=fresh outcome=complete identity=historical account="acct-1" source="fixture/local" windows=weekly/account:duration=unknown unit=percent duration=168h0m0s,remaining=42percent,reset=unknown unit=percent reset=2026-03-15T07:30:00Z pace=ahead pace_calculation=uniform_percent_reserve_v1 pace_calculated_at=2026-03-08T07:30:00Z pace_observed_at=2026-03-08T07:30:00Z pace_remaining=42percent pace_reset=2026-03-15T07:30:00Z pace_duration=168h0m0s pace_time_remaining=100percent pace_reserve=-58percentage_points` + "\n",
			json:    `{"schema_version":"v1","provider":"codex","profile":"main","account":{"last_observed":"acct-1","binding":"historical"},"source":{"kind":"fixture","name":"local"},"observed_at":"2026-03-08T07:30:00Z","freshness":"fresh","outcome":"complete","windows":[{"id":"weekly","scope":"account","unit":"percent","limits":[{"id":"weekly-duration","field":"duration","state":"unknown","duration":"168h0m0s"},{"id":"weekly-remaining","field":"remaining","state":"defined","amount":42},{"id":"weekly-reset","field":"reset","state":"unknown","reset_at":"2026-03-15T07:30:00Z"}],"pace":{"status":"ahead","calculation":"uniform_percent_reserve_v1","calculated_at":"2026-03-08T07:30:00Z","inputs":{"observed_at":"2026-03-08T07:30:00Z","remaining":{"state":"defined","amount":42},"reset_at":"2026-03-15T07:30:00Z","duration":"168h0m0s"},"time_remaining_percent":100,"reserve_percent_points":-58}}]}` + "\n",
			scalar:  "42\n",
		},
	}
	want, ok := goldens[fixture]
	if !ok {
		return 2, "", "remainder: unknown fixture golden\n"
	}
	switch format {
	case "compact":
		return want.code, want.compact, want.stderr
	case "json":
		return want.code, want.json, want.stderr
	case "scalar":
		code, stderr := want.code, want.stderr
		if want.scalarCode != 0 {
			code, stderr = want.scalarCode, want.scalarStderr
		}
		return code, want.scalar, stderr
	default:
		return 2, "", "remainder: unknown fixture format\n"
	}
}

func validateOutput(item workload, result sample) string {
	if len(result.Stderr) > 0 && item.ExitCode == 0 {
		return "unexpected stderr"
	}
	switch item.Name {
	case "startup-help":
		if result.Stdout != expectedHelp || result.Stderr != "" {
			return "help output mismatch"
		}
	case "startup-version":
		if result.Stdout != "remainder v0.1.0 (github.com/douglasjarquin/remainder)\n" || result.Stderr != "" {
			return "version output mismatch"
		}
	case "failure-unavailable":
		if result.Stdout != "" || result.Stderr != "remainder: quota is unavailable; select a supported provider\n" {
			return "unavailable error output mismatch"
		}
	case "failure-invalid-freshness":
		if result.Stdout != "" || result.Stderr != "remainder: invalid command usage: unsupported freshness \"ignored\"\n" {
			return "invalid-freshness error output mismatch"
		}
	}
	return "pass"
}
