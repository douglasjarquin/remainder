package cursor

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func normalize(usage usageResponse, plan planResponse, sand sandResponse, failures []evidence.Failure, now time.Time) (evidence.Observation, error) {
	cycleStart := usage.BillingCycleStart
	cycleEnd := usage.BillingCycleEnd
	if plan.PlanInfo != nil {
		if cycleStart == nil {
			cycleStart = plan.PlanInfo.BillingCycleStart
		}
		if cycleEnd == nil {
			cycleEnd = plan.PlanInfo.BillingCycleEnd
		}
	}
	windows := make([]evidence.Window, 0, 5)
	if usage.PlanUsage != nil {
		windows = append(windows,
			percentWindow("included_usage", usage.PlanUsage.TotalPercentUsed, cycleStart, cycleEnd),
			percentWindow("auto_usage", usage.PlanUsage.AutoPercentUsed, cycleStart, cycleEnd),
			percentWindow("api_usage", usage.PlanUsage.APIPercentUsed, cycleStart, cycleEnd),
		)
	}
	if usage.SpendLimitUsage != nil {
		spend, err := spendWindow(usage.SpendLimitUsage, cycleStart, cycleEnd)
		if err != nil {
			return evidence.Observation{}, err
		}
		windows = append(windows, spend)
	}
	if grok, ok, err := grokWindow(sand); err != nil {
		return evidence.Observation{}, err
	} else if ok {
		windows = append(windows, grok)
	}
	outcome := evidence.OutcomeComplete
	if len(failures) > 0 {
		outcome = evidence.OutcomePartial
	}
	observation := evidence.Observation{SchemaVersion: evidence.SchemaV1, Provider: "cursor", Profile: "default", Account: evidence.AccountIdentity{Binding: evidence.IdentityUnknown}, Source: evidence.SourceIdentity{Kind: "native_file_http", Name: "cursor_cli_auth_json"}, ObservedAt: now, Freshness: evidence.FreshFresh, Outcome: outcome, Windows: windows, Failures: failures}
	if err := observation.Validate(); err != nil {
		return evidence.Observation{}, fmt.Errorf("Cursor normalized evidence is invalid: %w", err)
	}
	return observation, nil
}

func percentWindow(id evidence.WindowID, used *sourceNumber, start, end *sourceTime) evidence.Window {
	usedValue := unknownValue()
	remainingValue := unknownValue()
	if used != nil {
		value := min(max(used.value, 0), 100)
		usedValue = numericValue(value)
		remainingValue = numericValue(100 - value)
	}
	limits := []evidence.Limit{
		{ID: string(id) + "_used", Field: evidence.Field("used"), Value: usedValue},
		{ID: string(id) + "_remaining", Field: evidence.FieldRemaining, Value: remainingValue},
	}
	return evidence.Window{ID: id, Scope: evidence.ScopeAccount, Unit: "percent", Limits: appendCycle(limits, string(id), start, end)}
}

func spendWindow(spend *spendLimitUsage, start, end *sourceTime) (evidence.Window, error) {
	fields := []struct {
		name  string
		field evidence.Field
		value *sourceNumber
	}{
		{name: "limit", field: evidence.Field("limit"), value: spend.IndividualLimit},
		{name: "remaining", field: evidence.FieldRemaining, value: spend.IndividualRemaining},
		{name: "used", field: evidence.Field("used"), value: spend.IndividualUsed},
	}
	limits := make([]evidence.Limit, 0, 5)
	for _, source := range fields {
		value := unknownValue()
		if source.value != nil {
			if source.value.value < 0 || math.IsInf(source.value.value, 0) || math.IsNaN(source.value.value) {
				return evidence.Window{}, fmtInvalidResponse("Cursor spend limit contains an invalid amount")
			}
			value = numericTextValue(source.value.raw, source.value.value)
		}
		limits = append(limits, evidence.Limit{ID: "spend_limit_" + source.name, Field: source.field, Value: value})
	}
	return evidence.Window{ID: "spend_limit", Scope: evidence.ScopeAccount, Unit: "usd_cents", Limits: appendCycle(limits, "spend_limit", start, end)}, nil
}

func grokWindow(sand sandResponse) (evidence.Window, bool, error) {
	pooled := sand.UsesPooledEnterpriseAllowance
	if pooled == nil {
		pooled = sand.UsesPooledEnterpriseAllowanceSnake
	}
	if pooled != nil && *pooled {
		return evidence.Window{}, false, nil
	}
	used := sand.UsagePercent
	if used == nil {
		used = sand.UsagePercentSnake
	}
	if used == nil {
		return evidence.Window{}, false, nil
	}
	start := sand.CurrentPeriodStart
	if start == nil {
		start = sand.CurrentPeriodStartSnake
	}
	end := sand.NextResetTimestampUTC
	if end == nil {
		end = sand.NextResetTimestampUTCSnake
	}
	return percentWindow("grok_bot", used, start, end), true, nil
}

func appendCycle(limits []evidence.Limit, id string, start, end *sourceTime) []evidence.Limit {
	if end != nil {
		reset := end.Time
		limits = append(limits, evidence.Limit{ID: id + "_reset", Field: evidence.FieldReset, Value: unknownValue(), ResetAt: &reset})
	}
	if start != nil && end != nil && end.After(start.Time) {
		duration := end.Sub(start.Time)
		if start.Add(duration).Equal(end.Time) {
			limits = append(limits, evidence.Limit{ID: id + "_duration", Field: evidence.FieldDuration, Value: unknownValue(), Duration: &duration})
		}
	}
	return limits
}

func numericValue(value float64) evidence.Value {
	return numericTextValue(strconv.FormatFloat(value, 'f', -1, 64), value)
}

func numericTextValue(raw string, value float64) evidence.Value {
	number := evidence.JSONNumber(raw)
	state := evidence.ValueDefined
	if value == 0 {
		state = evidence.ValueZero
	}
	return evidence.Value{State: state, Amount: &number}
}

func unknownValue() evidence.Value { return evidence.Value{State: evidence.ValueUnknown} }
