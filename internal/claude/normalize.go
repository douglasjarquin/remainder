package claude

import (
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func normalize(raw usageResponse, accountID string, observedAt time.Time) (evidence.Observation, error) {
	var windows []evidence.Window
	if raw.Limits != nil {
		for _, limit := range *raw.Limits {
			window, err := normalizeScopedLimit(limit)
			if err != nil {
				return evidence.Observation{}, err
			}
			windows = append(windows, window)
		}
	} else {
		for _, item := range []struct {
			raw      *legacyWindow
			id       evidence.WindowID
			scope    evidence.Scope
			duration time.Duration
		}{
			{raw.FiveHour, "five_hour", evidence.ScopeAccount, 5 * time.Hour},
			{raw.SevenDay, "weekly", evidence.ScopeAccount, 7 * 24 * time.Hour},
			{raw.SevenDayOpus, "model_opus", evidence.ScopeModel, 7 * 24 * time.Hour},
		} {
			if item.raw == nil && item.scope == evidence.ScopeModel {
				continue
			}
			value := &legacyWindow{}
			if item.raw != nil {
				value = item.raw
			}
			window, err := normalizePercentWindow(item.id, item.scope, value.Utilization, firstNonEmpty(value.ResetsAt, value.ResetAt), item.duration)
			if err != nil {
				return evidence.Observation{}, err
			}
			windows = append(windows, window)
		}
	}
	if raw.ExtraUsage != nil && raw.ExtraUsage.Enabled {
		window, err := normalizeExtraUsage(*raw.ExtraUsage)
		if err != nil {
			return evidence.Observation{}, err
		}
		windows = append(windows, window)
	}
	if len(windows) == 0 {
		return evidence.Observation{}, errors.New("Claude quota response schema has no limits")
	}
	observation := evidence.Observation{SchemaVersion: evidence.SchemaV1, Provider: "claude", Profile: "default", Account: evidence.AccountIdentity{LastObserved: accountID, Binding: evidence.IdentityVerified}, Source: evidence.SourceIdentity{Kind: "native_file_http", Name: "claude_credentials_json"}, ObservedAt: observedAt, Freshness: evidence.FreshFresh, Outcome: evidence.OutcomeComplete, Windows: windows}
	if err := observation.Validate(); err != nil {
		return evidence.Observation{}, errors.New("Claude normalized evidence is invalid")
	}
	return observation, nil
}

func normalizeScopedLimit(raw scopedLimit) (evidence.Window, error) {
	id := evidence.WindowID(raw.Kind)
	scope := evidence.ScopeAccount
	duration := time.Duration(0)
	if raw.Scope.Model.ID != "" || raw.Scope.Model.DisplayName != "" {
		modelID := raw.Scope.Model.ID
		if modelID == "" {
			modelID = safeID(raw.Scope.Model.DisplayName)
		}
		id, scope = evidence.WindowID("model_"+safeID(modelID)), evidence.ScopeModel
		switch raw.Group {
		case "session":
			id, duration = evidence.WindowID(string(id)+"_session"), 5*time.Hour
		case "weekly":
			id, duration = evidence.WindowID(string(id)+"_weekly"), 7*24*time.Hour
		}
	} else {
		switch raw.Group {
		case "session":
			id, duration = "five_hour", 5*time.Hour
		case "weekly":
			id, duration = "weekly", 7*24*time.Hour
		}
	}
	if id == "" {
		id = "limit"
	}
	return normalizePercentWindow(id, scope, raw.Percent, raw.ResetsAt, duration)
}

func normalizePercentWindow(id evidence.WindowID, scope evidence.Scope, percent sourceNumber, reset string, duration time.Duration) (evidence.Window, error) {
	used, remaining := unknownValue(), unknownValue()
	if value, ok := percent.float64(); ok {
		if value < 0 || value > 100 {
			return evidence.Window{}, errors.New("Claude quota percentage is outside 0 through 100")
		}
		used = numericValue(strconv.FormatFloat(value, 'f', -1, 64))
		remaining = numericValue(strconv.FormatFloat(100-value, 'f', -1, 64))
	}
	limits := []evidence.Limit{{ID: string(id) + "_used", Field: evidence.Field("used"), Value: used}, {ID: string(id) + "_remaining", Field: evidence.FieldRemaining, Value: remaining}}
	if reset != "" {
		parsed, err := time.Parse(time.RFC3339Nano, reset)
		if err != nil {
			return evidence.Window{}, errors.New("Claude quota reset time is invalid")
		}
		parsed = parsed.UTC()
		limits = append(limits, evidence.Limit{ID: string(id) + "_reset", Field: evidence.FieldReset, Value: unknownValue(), ResetAt: &parsed})
	}
	if duration > 0 {
		limits = append(limits, evidence.Limit{ID: string(id) + "_duration", Field: evidence.FieldDuration, Value: unknownValue(), Duration: &duration})
	}
	return evidence.Window{ID: id, Scope: scope, Unit: "percent", Limits: limits}, nil
}

func normalizeExtraUsage(raw extraUsage) (evidence.Window, error) {
	used, limit, remaining := unknownValue(), unknownValue(), unknownValue()
	unit := "credits"
	decimalPlaces, hasPlaces := raw.DecimalPlaces.int64()
	if raw.Used != "" || raw.MonthlyLimit != "" {
		if raw.DecimalPlaces != "" && !hasPlaces {
			return evidence.Window{}, errors.New("Claude extra usage decimal places are invalid")
		}
		if hasPlaces && (decimalPlaces < 0 || decimalPlaces > 9) {
			return evidence.Window{}, errors.New("Claude extra usage decimal places are required and invalid")
		}
		if !hasPlaces {
			decimalPlaces = 0
			unit = "credits_native"
		}
		if raw.Used != "" {
			value, usedDecimal, err := minorUnitValue(raw.Used, int(decimalPlaces))
			if err != nil {
				return evidence.Window{}, err
			}
			used = value
			if raw.MonthlyLimit != "" {
				limitValue, limitDecimal, err := minorUnitValue(raw.MonthlyLimit, int(decimalPlaces))
				if err != nil {
					return evidence.Window{}, err
				}
				limit = limitValue
				if remainingDecimal, ok := subtractDecimal(limitDecimal, usedDecimal); ok {
					remaining = numericValue(remainingDecimal.String())
				}
			}
		} else {
			var err error
			limit, _, err = minorUnitValue(raw.MonthlyLimit, int(decimalPlaces))
			if err != nil {
				return evidence.Window{}, err
			}
		}
	}
	return evidence.Window{ID: "extra_usage", Scope: evidence.ScopeAccount, Unit: unit, Limits: []evidence.Limit{{ID: "extra_usage_used", Field: evidence.Field("used"), Value: used}, {ID: "extra_usage_limit", Field: evidence.Field("limit"), Value: limit}, {ID: "extra_usage_remaining", Field: evidence.FieldRemaining, Value: remaining}}}, nil
}

const (
	maxPaidNumberBytes = 1024
	maxPaidExponent    = 4096
)

type exactDecimal struct {
	value *big.Rat
}

func minorUnitValue(raw sourceNumber, places int) (evidence.Value, exactDecimal, error) {
	text := string(raw)
	if text == "" || len(text) > maxPaidNumberBytes || strings.HasPrefix(text, "-") || strings.HasPrefix(text, "+") || !paidExponentBounded(text) {
		return evidence.Value{}, exactDecimal{}, errors.New("Claude extra usage value is invalid")
	}
	value, ok := new(big.Rat).SetString(text)
	if !ok || value.Sign() < 0 {
		return evidence.Value{}, exactDecimal{}, errors.New("Claude extra usage value is invalid")
	}
	if places > 0 {
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(places)), nil)
		value.Quo(value, new(big.Rat).SetInt(divisor))
	}
	decimal := exactDecimal{value: value}
	return numericValue(decimal.String()), decimal, nil
}

func paidExponentBounded(raw string) bool {
	index := strings.IndexAny(raw, "eE")
	if index < 0 {
		return true
	}
	exponent, err := strconv.ParseInt(raw[index+1:], 10, 16)
	return err == nil && exponent >= -maxPaidExponent && exponent <= maxPaidExponent
}

func subtractDecimal(left, right exactDecimal) (exactDecimal, bool) {
	difference := new(big.Rat).Sub(left.value, right.value)
	return exactDecimal{value: difference}, difference.Sign() >= 0
}

func (d exactDecimal) String() string {
	if d.value.IsInt() {
		return d.value.Num().String()
	}
	result := strings.TrimRight(d.value.FloatString(maxPaidNumberBytes+maxPaidExponent+9), "0")
	return strings.TrimSuffix(result, ".")
}

func numericValue(raw string) evidence.Value {
	number := evidence.JSONNumber(raw)
	state := evidence.ValueDefined
	if raw == "0" {
		state = evidence.ValueZero
	}
	return evidence.Value{State: state, Amount: &number}
}

func unknownValue() evidence.Value { return evidence.Value{State: evidence.ValueUnknown} }
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func safeID(value string) string {
	var result strings.Builder
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			result.WriteRune(r)
		} else {
			result.WriteByte('_')
		}
	}
	return strings.Trim(result.String(), "_")
}
