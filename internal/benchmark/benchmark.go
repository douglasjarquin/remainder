package benchmark

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

var ErrNoSamples = errors.New("benchmark requires at least one sample")

const (
	FixtureSeed  = "remainder-issue-4-codex-cli-fixture-v1"
	FixtureClock = "2026-03-08T07:30:00Z"
)

var ErrLatencyRegression = errors.New("latency regression")

type Summary struct {
	Count        int   `json:"count"`
	MinNS        int64 `json:"min_ns"`
	MaxNS        int64 `json:"max_ns"`
	P50NS        int64 `json:"p50_ns"`
	P95NS        int64 `json:"p95_ns"`
	MeanNS       int64 `json:"mean_ns"`
	DispersionNS int64 `json:"dispersion_ns"`
}

func Summarize(samples []int64) (Summary, error) {
	if len(samples) == 0 {
		return Summary{}, ErrNoSamples
	}
	sorted := append([]int64(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var total int64
	for _, sample := range sorted {
		total += sample
	}
	p95 := (95*len(sorted)+99)/100 - 1
	return Summary{
		Count:        len(sorted),
		MinNS:        sorted[0],
		MaxNS:        sorted[len(sorted)-1],
		P50NS:        sorted[(len(sorted)-1)/2],
		P95NS:        sorted[p95],
		MeanNS:       total / int64(len(sorted)),
		DispersionNS: sorted[len(sorted)-1] - sorted[0],
	}, nil
}

func FixtureHash() string {
	data, err := json.Marshal(struct {
		Clock    string    `json:"clock"`
		Fixtures []Fixture `json:"fixtures"`
	}{Clock: FixtureClock, Fixtures: Fixtures()})
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func FixtureHashFor(fixture Fixture) string {
	data, err := json.Marshal(struct {
		Clock   string  `json:"clock"`
		Fixture Fixture `json:"fixture"`
	}{Clock: FixtureClock, Fixture: fixture})
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func FixtureTime() time.Time {
	return time.Date(2026, time.March, 8, 7, 30, 0, 0, time.UTC)
}

func DetectLatencyRegression(current, baseline Summary) error {
	if current.P95NS > baseline.P95NS {
		return fmt.Errorf("%w: p95 %d ns exceeds seeded baseline %d ns", ErrLatencyRegression, current.P95NS, baseline.P95NS)
	}
	return nil
}

type Fixture struct {
	Name          string               `json:"name"`
	RequiredFacts []string             `json:"required_facts"`
	Observation   evidence.Observation `json:"observation"`
}

var requiredFacts = []string{
	"schema_version",
	"provider",
	"profile",
	"account.last_observed",
	"account.binding",
	"source.kind",
	"source.name",
	"observed_at",
	"freshness",
	"outcome",
	"window.id",
	"window.scope",
	"window.unit",
	"limit.field",
	"limit.value",
	"failures",
}

func Fixtures() []Fixture {
	sharedPercent := fixtureObservation(evidence.FreshFresh, evidence.OutcomeComplete, evidence.Value{State: evidence.ValueDefined, Amount: number("42")})
	sharedPercent.ObservedAt = FixtureTime()
	sharedPercent.Windows[0].Scope = evidence.ScopeAccount
	sharedPercent.Windows[0].Unit = "percent"
	resetAt := FixtureTime().Add(7 * 24 * time.Hour)
	duration := 7 * 24 * time.Hour
	sharedPercent.Windows[0].Limits = append(sharedPercent.Windows[0].Limits,
		evidence.Limit{ID: "weekly-duration", Field: evidence.FieldDuration, Value: evidence.Value{State: evidence.ValueUnknown}, Duration: &duration},
		evidence.Limit{ID: "weekly-reset", Field: evidence.FieldReset, Value: evidence.Value{State: evidence.ValueUnknown}, ResetAt: &resetAt},
	)
	sharedRequiredFacts := append([]string(nil), requiredFacts...)
	sharedRequiredFacts = append(sharedRequiredFacts, "window.pace.status", "window.pace.calculation", "window.pace.inputs", "window.pace.time_remaining_percent", "window.pace.reserve_percent_points")
	return []Fixture{
		{Name: "healthy", RequiredFacts: requiredFacts, Observation: fixtureObservation(evidence.FreshFresh, evidence.OutcomeComplete, evidence.Value{State: evidence.ValueDefined, Amount: number("42")})},
		{Name: "exhausted", RequiredFacts: requiredFacts, Observation: fixtureObservation(evidence.FreshFresh, evidence.OutcomeComplete, evidence.Value{State: evidence.ValueZero, Amount: number("0")})},
		{Name: "stale", RequiredFacts: requiredFacts, Observation: fixtureObservation(evidence.FreshStale, evidence.OutcomeComplete, evidence.Value{State: evidence.ValueDefined, Amount: number("17")})},
		{Name: "partial-unknown", RequiredFacts: requiredFacts, Observation: fixtureObservation(evidence.FreshFresh, evidence.OutcomePartial, evidence.Value{State: evidence.ValueUnknown}, []evidence.Failure{{Scope: "daily", Message: "source unavailable"}})},
		{Name: "shared-percent", RequiredFacts: sharedRequiredFacts, Observation: sharedPercent},
	}
}

func number(value string) *json.Number {
	number := json.Number(value)
	return &number
}

func fixtureObservation(freshness evidence.Freshness, outcome evidence.Outcome, value evidence.Value, failures ...[]evidence.Failure) evidence.Observation {
	observation := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "codex",
		Profile:       "main",
		Account:       evidence.AccountIdentity{LastObserved: "acct-1", Binding: evidence.IdentityHistorical},
		Source:        evidence.SourceIdentity{Kind: "fixture", Name: "local"},
		ObservedAt:    time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC),
		Freshness:     freshness,
		Outcome:       outcome,
		Windows: []evidence.Window{{
			ID:    "weekly",
			Scope: evidence.ScopeModel,
			Unit:  "tokens",
			Limits: []evidence.Limit{{
				ID:    "weekly-remaining",
				Field: evidence.FieldRemaining,
				Value: value,
			}},
		}},
	}
	if len(failures) > 0 {
		observation.Failures = failures[0]
	}
	return observation
}
