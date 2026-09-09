package cli

import (
	"bytes"
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/benchmark"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func BenchmarkExecuteHelp(b *testing.B) {
	var stdout, stderr bytes.Buffer
	b.ReportAllocs()
	for b.Loop() {
		stdout.Reset()
		stderr.Reset()
		Execute(context.Background(), []string{"--help"}, &stdout, &stderr, "v0.1.0")
	}
}

type benchmarkAdapter struct {
	observation evidence.Observation
}

func (a benchmarkAdapter) Observe(context.Context, evidence.Request) (evidence.Observation, error) {
	return a.observation, nil
}

func benchmarkObservation() evidence.Observation {
	return benchmark.Fixtures()[0].Observation
}

func TestBenchmarkFixtureOutputs(t *testing.T) {
	adapter := benchmarkAdapter{observation: benchmarkObservation()}
	var compactOut, compactErr bytes.Buffer
	if code := executeWithAdapterAt(context.Background(), []string{"--format", "compact"}, &compactOut, &compactErr, "v0.1.0", time.Date(2026, time.March, 8, 7, 30, 0, 0, time.UTC), adapter); code != 0 {
		t.Fatalf("compact exit code = %d, stderr = %q", code, compactErr.String())
	}
	compact := regexp.MustCompile(`age_seconds=\d+`).ReplaceAllString(compactOut.String(), "age_seconds=<age>")
	wantCompact := `schema=v1 provider="codex" profile="main" observed_at=2026-03-08T07:00:00Z age_seconds=<age> freshness=fresh outcome=complete identity=historical account="acct-1" source="fixture/local" windows=weekly/model:remaining=42tokens` + "\n"
	if compact != wantCompact || compactErr.Len() != 0 {
		t.Fatalf("compact output = %q, stderr = %q, want %q", compact, compactErr.String(), wantCompact)
	}

	var jsonOut, jsonErr bytes.Buffer
	if code := executeWithAdapter(context.Background(), []string{"--format", "json"}, &jsonOut, &jsonErr, "v0.1.0", adapter); code != 0 {
		t.Fatalf("JSON exit code = %d, stderr = %q", code, jsonErr.String())
	}
	wantJSON := `{"schema_version":"v1","provider":"codex","profile":"main","account":{"last_observed":"acct-1","binding":"historical"},"source":{"kind":"fixture","name":"local"},"observed_at":"2026-03-08T07:00:00Z","freshness":"fresh","outcome":"complete","windows":[{"id":"weekly","scope":"model","unit":"tokens","limits":[{"id":"weekly-remaining","field":"remaining","state":"defined","amount":42}]}]}` + "\n"
	if jsonOut.String() != wantJSON || jsonErr.Len() != 0 {
		t.Fatalf("JSON output = %q, stderr = %q, want %q", jsonOut.String(), jsonErr.String(), wantJSON)
	}

	var valueOut, valueErr bytes.Buffer
	args := []string{"value", "--provider", "codex", "--profile", "main", "--window", "weekly", "--field", "remaining"}
	if code := executeWithAdapter(context.Background(), args, &valueOut, &valueErr, "v0.1.0", adapter); code != 0 {
		t.Fatalf("scalar exit code = %d, stderr = %q", code, valueErr.String())
	}
	if valueOut.String() != "42\n" || valueErr.Len() != 0 {
		t.Fatalf("scalar output = %q, stderr = %q", valueOut.String(), valueErr.String())
	}
}

func TestBenchmarkFixtureAllocationBudgets(t *testing.T) {
	tests := []struct {
		name string
		args []string
		max  float64
	}{
		{name: "compact", args: []string{"--format", "compact"}, max: 130},
		{name: "json", args: []string{"--format", "json"}, max: 120},
		{name: "scalar", args: []string{"value", "--provider", "codex", "--profile", "main", "--window", "weekly", "--field", "remaining"}, max: 125},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := benchmarkAdapter{observation: benchmarkObservation()}
			var stdout, stderr bytes.Buffer
			allocs := testing.AllocsPerRun(100, func() {
				stdout.Reset()
				stderr.Reset()
				executeWithAdapter(context.Background(), test.args, &stdout, &stderr, "v0.1.0", adapter)
			})
			if allocs > test.max {
				t.Fatalf("allocations = %.0f, want at most %.0f", allocs, test.max)
			}
		})
	}
}

func BenchmarkExecuteFixtureCompact(b *testing.B) {
	benchmarkExecute(b, []string{"--format", "compact"})
}

func BenchmarkExecuteFixtureJSON(b *testing.B) {
	benchmarkExecute(b, []string{"--format", "json"})
}

func BenchmarkExecuteFixtureScalar(b *testing.B) {
	benchmarkExecute(b, []string{"value", "--provider", "codex", "--profile", "main", "--window", "weekly", "--field", "remaining"})
}

func benchmarkExecute(b *testing.B, args []string) {
	adapter := benchmarkAdapter{observation: benchmarkObservation()}
	var stdout, stderr bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		stdout.Reset()
		stderr.Reset()
		executeWithAdapter(context.Background(), args, &stdout, &stderr, "v0.1.0", adapter)
	}
}
