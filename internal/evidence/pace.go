package evidence

import (
	"encoding/json"
	"math"
	"strconv"
	"time"
)

const (
	FieldPace       Field  = "pace"
	PaceCalculation string = "uniform_percent_reserve_v1"
)

type PaceStatus string

const (
	PaceAhead         PaceStatus = "ahead"
	PaceOnPace        PaceStatus = "on_pace"
	PaceBehind        PaceStatus = "behind"
	PaceUnknown       PaceStatus = "unknown"
	PaceNotApplicable PaceStatus = "not_applicable"
)

type PaceInputs struct {
	ObservedAt time.Time      `json:"observed_at"`
	Remaining  Value          `json:"remaining"`
	ResetAt    *time.Time     `json:"reset_at,omitempty"`
	Duration   *time.Duration `json:"duration,omitempty"`
}

type Pace struct {
	Status               PaceStatus   `json:"status"`
	Reason               string       `json:"reason,omitempty"`
	Calculation          string       `json:"calculation"`
	CalculatedAt         time.Time    `json:"calculated_at"`
	Inputs               PaceInputs   `json:"inputs"`
	TimeRemainingPercent *json.Number `json:"time_remaining_percent,omitempty"`
	ReservePercentPoints *json.Number `json:"reserve_percent_points,omitempty"`
}

func (p Pace) validate(observation Observation, window Window) error {
	if p.Calculation == "" || p.CalculatedAt.IsZero() || p.Inputs.ObservedAt.IsZero() {
		return ErrInvalidObservation
	}
	if !p.Inputs.ObservedAt.Equal(observation.ObservedAt) {
		return ErrInvalidObservation
	}
	if err := p.Inputs.Remaining.validate(); err != nil {
		return err
	}
	switch p.Status {
	case PaceAhead, PaceOnPace, PaceBehind:
		if p.Reason != "" || p.TimeRemainingPercent == nil || p.ReservePercentPoints == nil {
			return ErrInvalidObservation
		}
		if err := validateNumber(p.TimeRemainingPercent); err != nil {
			return err
		}
		if err := validateSignedNumber(p.ReservePercentPoints); err != nil {
			return err
		}
		if !sameKnownPace(p, derivePace(observation, window, p.CalculatedAt)) {
			return ErrInvalidObservation
		}
		return nil
	case PaceUnknown, PaceNotApplicable:
		if p.Reason == "" || p.TimeRemainingPercent != nil || p.ReservePercentPoints != nil {
			return ErrInvalidObservation
		}
		return nil
	default:
		return ErrInvalidObservation
	}
}

func sameKnownPace(actual Pace, expected *Pace) bool {
	if expected == nil || expected.TimeRemainingPercent == nil || expected.ReservePercentPoints == nil ||
		actual.Inputs.Remaining.Amount == nil || expected.Inputs.Remaining.Amount == nil ||
		actual.Inputs.ResetAt == nil || expected.Inputs.ResetAt == nil ||
		actual.Inputs.Duration == nil || expected.Inputs.Duration == nil {
		return false
	}
	return actual.Status == expected.Status &&
		actual.Reason == expected.Reason &&
		actual.Calculation == expected.Calculation &&
		actual.Inputs.Remaining.State == expected.Inputs.Remaining.State &&
		samePaceNumber(actual.Inputs.Remaining.Amount, expected.Inputs.Remaining.Amount) &&
		actual.Inputs.ResetAt.Equal(*expected.Inputs.ResetAt) &&
		*actual.Inputs.Duration == *expected.Inputs.Duration &&
		samePaceNumber(actual.TimeRemainingPercent, expected.TimeRemainingPercent) &&
		samePaceNumber(actual.ReservePercentPoints, expected.ReservePercentPoints)
}

func samePaceNumber(actual, expected *json.Number) bool {
	actualValue, actualErr := strconv.ParseFloat(actual.String(), 64)
	expectedValue, expectedErr := strconv.ParseFloat(expected.String(), 64)
	return actualErr == nil && expectedErr == nil && actualValue == expectedValue
}

func WithPace(observation Observation, evaluatedAt time.Time) Observation {
	derived := observation
	derived.Windows = append([]Window(nil), observation.Windows...)
	for index := range derived.Windows {
		derived.Windows[index].Pace = derivePace(observation, derived.Windows[index], evaluatedAt)
	}
	return derived
}

func derivePace(observation Observation, window Window, evaluatedAt time.Time) *Pace {
	pace := &Pace{
		Calculation:  PaceCalculation,
		CalculatedAt: evaluatedAt,
		Inputs:       PaceInputs{ObservedAt: observation.ObservedAt, Remaining: Value{State: ValueUnknown}},
	}
	if window.Unit != "percent" {
		return nil
	}
	if observation.Freshness != FreshFresh {
		pace.Status, pace.Reason = PaceUnknown, "stale"
		return pace
	}
	if evaluatedAt.Before(observation.ObservedAt) {
		pace.Status, pace.Reason = PaceUnknown, "future_observation"
		return pace
	}

	remaining, resetAt, duration, ok := paceInputs(window)
	pace.Inputs.Remaining, pace.Inputs.ResetAt, pace.Inputs.Duration = remaining, resetAt, duration
	if !ok {
		pace.Status, pace.Reason = PaceUnknown, "ambiguous_inputs"
		return pace
	}
	if remaining.State != ValueDefined && remaining.State != ValueZero || remaining.Amount == nil {
		pace.Status, pace.Reason = PaceUnknown, "missing_usage"
		return pace
	}
	percentRemaining, err := strconv.ParseFloat(remaining.Amount.String(), 64)
	if err != nil || math.IsNaN(percentRemaining) || math.IsInf(percentRemaining, 0) || percentRemaining < 0 || percentRemaining > 100 {
		pace.Status, pace.Reason = PaceUnknown, "invalid_usage"
		return pace
	}
	if resetAt == nil || duration == nil {
		pace.Status, pace.Reason = PaceUnknown, "missing_cycle"
		return pace
	}
	if *duration <= 0 {
		pace.Status, pace.Reason = PaceUnknown, "invalid_cycle"
		return pace
	}
	if !observation.ObservedAt.Before(*resetAt) || !evaluatedAt.Before(*resetAt) {
		pace.Status, pace.Reason = PaceUnknown, "expired_reset"
		return pace
	}
	startsAt := resetAt.Add(-*duration)
	if startsAt.After(observation.ObservedAt) {
		pace.Status, pace.Reason = PaceUnknown, "future_cycle_start"
		return pace
	}
	timeRemaining := 100 * resetAt.Sub(observation.ObservedAt).Seconds() / duration.Seconds()
	reserve := percentRemaining - timeRemaining
	pace.TimeRemainingPercent = paceNumber(timeRemaining)
	pace.ReservePercentPoints = paceNumber(reserve)
	switch {
	case reserve < -1:
		pace.Status = PaceAhead
	case reserve > 1:
		pace.Status = PaceBehind
	default:
		pace.Status = PaceOnPace
	}
	return pace
}

func paceInputs(window Window) (Value, *time.Time, *time.Duration, bool) {
	remaining := Value{State: ValueUnknown}
	var resetAt *time.Time
	var duration *time.Duration
	remainingCount, resetCount, durationCount := 0, 0, 0
	for _, limit := range window.Limits {
		if limit.Field == FieldRemaining {
			remaining, remainingCount = limit.Value, remainingCount+1
		}
		if limit.ResetAt != nil {
			resetAt, resetCount = limit.ResetAt, resetCount+1
		}
		if limit.Duration != nil {
			duration, durationCount = limit.Duration, durationCount+1
		}
	}
	return remaining, resetAt, duration, remainingCount == 1 && resetCount <= 1 && durationCount <= 1
}

func paceNumber(value float64) *json.Number {
	rounded := strconv.FormatFloat(value, 'f', 4, 64)
	parsed, _ := strconv.ParseFloat(rounded, 64)
	number := json.Number(strconv.FormatFloat(parsed, 'f', -1, 64))
	return &number
}
