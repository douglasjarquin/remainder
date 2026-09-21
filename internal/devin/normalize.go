package devin

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

var ErrAccountMismatch = errors.New("Devin quota account mismatch")

func normalize(raw *statusResponse, selectedAccount string, now time.Time) (evidence.Observation, error) {
	if raw == nil || raw.UserStatus == nil {
		return evidence.Observation{}, errors.New("Devin quota response has no user status")
	}
	user := raw.UserStatus
	if user.PlanStatus == nil {
		return evidence.Observation{}, errors.New("Devin quota response has no plan status")
	}
	status := user.PlanStatus
	if selectedAccount != "" {
		if user.UserID == "" {
			return evidence.Observation{}, fmt.Errorf("%w: response cannot verify an account selector", evidence.ErrWrongAccount)
		}
		if user.UserID != selectedAccount {
			return evidence.Observation{}, fmt.Errorf("%w: response does not match selected profile", ErrAccountMismatch)
		}
	}
	windows := make([]evidence.Window, 0, 6)
	partial := false
	for _, quota := range []struct {
		id       string
		percent  *sourceNumber
		reset    *sourceReset
		duration time.Duration
	}{
		{id: "daily", percent: status.DailyQuotaRemainingPercent, reset: status.DailyQuotaResetAtUnix, duration: 24 * time.Hour},
		{id: "weekly", percent: status.WeeklyQuotaRemainingPercent, reset: status.WeeklyQuotaResetAtUnix, duration: 7 * 24 * time.Hour},
	} {
		window, emitted, incomplete, err := quotaWindow(quota.id, quota.percent, quota.reset, quota.duration)
		if err != nil {
			return evidence.Observation{}, err
		}
		if emitted {
			windows = append(windows, window)
			partial = partial || incomplete
		}
	}
	if window, emitted, incomplete, err := acuWindow(status); err != nil {
		return evidence.Observation{}, err
	} else if emitted {
		windows = append(windows, window)
		partial = partial || incomplete
	}
	monthly := status.PlanInfo
	if monthly == nil {
		monthly = raw.PlanInfo
	}
	for _, family := range []struct {
		id        string
		used      *sourceNumber
		available *sourceNumber
		monthly   *sourceNumber
	}{
		{id: "prompt_credits", used: status.UsedPromptCredits, available: status.AvailablePromptCredits, monthly: monthlyValue(monthly, "prompt")},
		{id: "flow_credits", used: status.UsedFlowCredits, available: status.AvailableFlowCredits, monthly: monthlyValue(monthly, "flow")},
		{id: "flex_credits", used: status.UsedFlexCredits, available: status.AvailableFlexCredits},
	} {
		window, emitted, incomplete, err := creditWindow(family.id, family.used, family.available, family.monthly)
		if err != nil {
			return evidence.Observation{}, err
		}
		if emitted {
			windows = append(windows, window)
			partial = partial || incomplete
		}
	}
	if len(windows) == 0 {
		return evidence.Observation{}, errors.New("Devin plan status has no quota evidence")
	}
	binding := evidence.IdentityUnknown
	if user.UserID != "" {
		binding = evidence.IdentityVerified
	}
	outcome := evidence.OutcomeComplete
	if partial {
		outcome = evidence.OutcomePartial
	}
	observation := evidence.Observation{SchemaVersion: evidence.SchemaV1, Provider: "devin", Profile: "default", Account: evidence.AccountIdentity{LastObserved: user.UserID, Binding: binding}, Source: evidence.SourceIdentity{Kind: "native_file_http", Name: "devin_credentials_toml"}, ObservedAt: now, Freshness: evidence.FreshFresh, Outcome: outcome, Windows: windows}
	if err := observation.Validate(); err != nil {
		return evidence.Observation{}, fmt.Errorf("Devin normalized evidence is invalid: %w", err)
	}
	return observation, nil
}

func monthlyValue(info *planInfo, family string) *sourceNumber {
	if info == nil {
		return nil
	}
	switch family {
	case "prompt":
		return info.MonthlyPromptCredits
	case "flow":
		return info.MonthlyFlowCredits
	default:
		return nil
	}
}

func quotaWindow(id string, percent *sourceNumber, reset *sourceReset, duration time.Duration) (evidence.Window, bool, bool, error) {
	if percent == nil && reset == nil {
		return evidence.Window{}, false, false, nil
	}
	remaining := evidence.Value{State: evidence.ValueUnknown}
	used := evidence.Value{State: evidence.ValueUnknown}
	partial := false
	if percent != nil {
		value := float64(*percent)
		if value < 0 || value > 100 {
			return evidence.Window{}, false, false, errors.New("Devin quota percent is outside 0 through 100")
		}
		var err error
		remaining, err = numericValue(value, true)
		if err != nil {
			return evidence.Window{}, false, false, err
		}
		used, err = numericValue(100-value, true)
		if err != nil {
			return evidence.Window{}, false, false, err
		}
	} else {
		partial = true
	}
	limits := []evidence.Limit{
		{ID: id + "_used", Field: evidence.Field("used"), Value: used},
		{ID: id + "_remaining", Field: evidence.FieldRemaining, Value: remaining},
		{ID: id + "_duration", Field: evidence.FieldDuration, Value: evidence.Value{State: evidence.ValueUnknown}, Duration: &duration},
	}
	if reset != nil {
		resetAt := reset.Time
		limits = append(limits, evidence.Limit{ID: id + "_reset", Field: evidence.FieldReset, Value: evidence.Value{State: evidence.ValueUnknown}, ResetAt: &resetAt})
	} else {
		partial = true
	}
	return evidence.Window{ID: evidence.WindowID(id), Scope: evidence.ScopeAccount, Unit: "percent", Limits: limits}, true, partial, nil
}

func acuWindow(status *planStatus) (evidence.Window, bool, bool, error) {
	if status.AcuConsumed == nil && status.AcuLimit == nil {
		return evidence.Window{}, false, false, nil
	}
	used := evidence.Value{State: evidence.ValueUnknown}
	remaining := evidence.Value{State: evidence.ValueUnknown}
	partial := false
	if status.AcuConsumed != nil {
		if *status.AcuConsumed < 0 {
			partial = true
		} else {
			var err error
			used, err = numericValue(float64(*status.AcuConsumed), false)
			if err != nil {
				return evidence.Window{}, false, false, errors.New("Devin ACU usage is invalid")
			}
		}
	} else {
		partial = true
	}
	switch {
	case status.AcuLimit == nil:
		partial = true
	case *status.AcuLimit < 0:
		remaining = evidence.Value{State: evidence.ValueUnlimited}
	case status.AcuConsumed == nil || *status.AcuConsumed < 0 || *status.AcuConsumed > *status.AcuLimit:
		partial = true
	default:
		value, err := numericValue(float64(*status.AcuLimit)-float64(*status.AcuConsumed), false)
		if err != nil {
			return evidence.Window{}, false, false, errors.New("Devin ACU remaining is invalid")
		}
		remaining = value
	}
	limits := []evidence.Limit{
		{ID: "acu_used", Field: evidence.Field("used"), Value: used},
		{ID: "acu_remaining", Field: evidence.FieldRemaining, Value: remaining},
	}
	if status.PlanEnd != nil {
		resetAt := status.PlanEnd.Time
		limits = append(limits, evidence.Limit{ID: "acu_reset", Field: evidence.FieldReset, Value: evidence.Value{State: evidence.ValueUnknown}, ResetAt: &resetAt})
		if status.PlanStart != nil && status.PlanEnd.After(status.PlanStart.Time) {
			duration := status.PlanEnd.Sub(status.PlanStart.Time)
			limits = append(limits, evidence.Limit{ID: "acu_duration", Field: evidence.FieldDuration, Value: evidence.Value{State: evidence.ValueUnknown}, Duration: &duration})
		}
	} else {
		partial = true
	}
	return evidence.Window{ID: "acu", Scope: evidence.ScopeAccount, Unit: "acu", Limits: limits}, true, partial, nil
}

func creditWindow(id string, used, available, monthly *sourceNumber) (evidence.Window, bool, bool, error) {
	if used == nil && available == nil && monthly == nil {
		return evidence.Window{}, false, false, nil
	}
	limits := make([]evidence.Limit, 0, 3)
	partial := false
	usedValue := evidence.Value{State: evidence.ValueUnknown}
	switch {
	case used == nil:
		if available != nil && *available < 0 {
			usedValue = evidence.Value{State: evidence.ValueNotApplicable}
		} else {
			partial = true
		}
	case *used < 0:
		partial = true
	default:
		value, err := numericValue(float64(*used), false)
		if err != nil {
			return evidence.Window{}, false, false, fmt.Errorf("Devin %s used is invalid", id)
		}
		usedValue = value
	}
	limits = append(limits, evidence.Limit{ID: id + "_used", Field: evidence.Field("used"), Value: usedValue})
	remaining := evidence.Value{State: evidence.ValueUnknown}
	switch {
	case available == nil:
		partial = true
	case *available < 0:
		remaining = evidence.Value{State: evidence.ValueUnlimited}
	default:
		value, err := numericValue(float64(*available), false)
		if err != nil {
			return evidence.Window{}, false, false, fmt.Errorf("Devin %s remaining is invalid", id)
		}
		remaining = value
	}
	limits = append(limits, evidence.Limit{ID: id + "_remaining", Field: evidence.FieldRemaining, Value: remaining})
	if monthly != nil {
		limit := evidence.Value{State: evidence.ValueUnknown}
		if *monthly < 0 {
			limit = evidence.Value{State: evidence.ValueUnlimited}
		} else {
			value, err := numericValue(float64(*monthly), false)
			if err != nil {
				return evidence.Window{}, false, false, fmt.Errorf("Devin %s limit is invalid", id)
			}
			limit = value
		}
		limits = append(limits, evidence.Limit{ID: id + "_limit", Field: evidence.Field("limit"), Value: limit})
	}
	return evidence.Window{ID: evidence.WindowID(id), Scope: evidence.ScopeAccount, Unit: "credits", Limits: limits}, true, partial, nil
}

func numericValue(value float64, bound bool) (evidence.Value, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || bound && value > 100 {
		return evidence.Value{}, errors.New("invalid numeric value")
	}
	number := evidence.JSONNumber(strconv.FormatFloat(value, 'f', -1, 64))
	state := evidence.ValueDefined
	if value == 0 {
		state = evidence.ValueZero
	}
	return evidence.Value{State: state, Amount: &number}, nil
}
