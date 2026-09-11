package benchmark

import (
	"errors"
	"reflect"
	"testing"
)

func TestSummarizeSeededSamples(t *testing.T) {
	got, err := Summarize([]int64{10, 20, 30, 40, 50})
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	want := Summary{
		Count:        5,
		MinNS:        10,
		MaxNS:        50,
		P50NS:        30,
		P95NS:        50,
		MeanNS:       30,
		DispersionNS: 40,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Summarize() = %#v, want %#v", got, want)
	}
}

func TestSummarizeRejectsEmptySamples(t *testing.T) {
	if _, err := Summarize(nil); err != ErrNoSamples {
		t.Fatalf("Summarize(nil) error = %v, want %v", err, ErrNoSamples)
	}
}

func TestFixtureHashIsStable(t *testing.T) {
	const want = "eadac2d82be444394253e39881baae3637a60fd9b3322fbd498e815dcea47b33"
	if got := FixtureHash(); got != want {
		t.Fatalf("FixtureHash() = %q, want %q", got, want)
	}
}

func TestFixturesCoverRequiredObservationStates(t *testing.T) {
	fixtures := Fixtures()
	if len(fixtures) != 5 {
		t.Fatalf("fixture count = %d, want 5", len(fixtures))
	}
	want := []string{"healthy", "exhausted", "stale", "partial-unknown", "shared-percent"}
	for index, fixture := range fixtures {
		if fixture.Name != want[index] {
			t.Fatalf("fixture %d = %q, want %q", index, fixture.Name, want[index])
		}
		if err := fixture.Observation.Validate(); err != nil {
			t.Fatalf("fixture %q validation error = %v", fixture.Name, err)
		}
	}
}

func TestFixturesIncludeSharedPercentComparatorScope(t *testing.T) {
	for _, fixture := range Fixtures() {
		if fixture.Name != "shared-percent" {
			continue
		}
		window := fixture.Observation.Windows[0]
		if window.ID != "weekly" || window.Scope != "account" || window.Unit != "percent" {
			t.Fatalf("shared-percent id/scope/unit = %q/%q/%q, want weekly/account/percent", window.ID, window.Scope, window.Unit)
		}
		if !fixture.Observation.ObservedAt.Equal(FixtureTime()) {
			t.Fatalf("shared-percent observed_at = %s, want %s", fixture.Observation.ObservedAt, FixtureTime())
		}
		if got := window.Limits[0].Value.Amount.String(); got != "42" {
			t.Fatalf("shared-percent remaining = %s, want 42", got)
		}
		return
	}
	t.Fatal("shared-percent fixture missing")
}

func TestDetectLatencyRegressionUsesSeededP95(t *testing.T) {
	baseline, err := Summarize([]int64{10, 20, 30, 40, 50})
	if err != nil {
		t.Fatal(err)
	}
	regressed, err := Summarize([]int64{10, 20, 30, 40, 60})
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(DetectLatencyRegression(regressed, baseline), ErrLatencyRegression) {
		t.Fatalf("DetectLatencyRegression() did not reject seeded p95 regression")
	}
	if err := DetectLatencyRegression(baseline, baseline); err != nil {
		t.Fatalf("DetectLatencyRegression() rejected equal baseline: %v", err)
	}
}
