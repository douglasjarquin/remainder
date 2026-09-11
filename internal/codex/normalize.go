package codex

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func normalize(raw usageResponse, selectedAccount string, now time.Time) (evidence.Observation, error) {
	accountID := raw.AccountID
	if accountID == "" {
		accountID = raw.AccountIDCamel
	}
	if accountID != "" && accountID != selectedAccount {
		return evidence.Observation{}, fmt.Errorf("%w: response does not match selected profile", ErrAccountMismatch)
	}
	base := raw.RateLimit
	if base == nil {
		base = raw.RateLimits
	}
	if base == nil {
		base = raw.RateLimitsSnake
	}
	if base == nil {
		return evidence.Observation{}, errors.New("Codex quota response schema has no base rate limit")
	}
	windows, err := appendRateWindows(nil, base, "", evidence.ScopeAccount, now)
	if err != nil {
		return evidence.Observation{}, err
	}
	windows, err = appendRateWindows(windows, raw.CodeReviewRateLimit, "code_review_", evidence.Scope("code_review"), now)
	if err != nil {
		return evidence.Observation{}, err
	}
	for _, additional := range raw.AdditionalRateLimits {
		id := additional.MeteredFeature
		if id == "" {
			id = additional.LimitName
		}
		if id == "" || additional.RateLimit == nil {
			return evidence.Observation{}, errors.New("Codex quota response has malformed additional limit")
		}
		windows, err = appendRateWindows(windows, additional.RateLimit, "model_"+safeID(id)+"_", evidence.ScopeModel, now)
		if err != nil {
			return evidence.Observation{}, err
		}
	}
	if raw.Credits != nil && (raw.Credits.Balance != nil || raw.Credits.Unlimited != nil) {
		value := evidence.Value{State: evidence.ValueUnknown}
		if raw.Credits.Unlimited != nil && *raw.Credits.Unlimited {
			value = evidence.Value{State: evidence.ValueUnlimited}
		} else if raw.Credits.Balance != nil {
			value, err = numericValue(float64(*raw.Credits.Balance), false)
			if err != nil {
				return evidence.Observation{}, errors.New("Codex credits value is invalid")
			}
		}
		windows = append(windows, evidence.Window{ID: "credits", Scope: evidence.ScopeAccount, Unit: "credits", Limits: []evidence.Limit{{ID: "credits_remaining", Field: evidence.FieldRemaining, Value: value}}})
	}
	binding := evidence.IdentityHistorical
	if accountID == selectedAccount {
		binding = evidence.IdentityVerified
	}
	observation := evidence.Observation{SchemaVersion: evidence.SchemaV1, Provider: "codex", Profile: "default", Account: evidence.AccountIdentity{LastObserved: selectedAccount, Binding: binding}, Source: evidence.SourceIdentity{Kind: "native_file_http", Name: "codex_auth_json"}, ObservedAt: now, Freshness: evidence.FreshFresh, Outcome: evidence.OutcomeComplete, Windows: windows}
	if err := observation.Validate(); err != nil {
		return evidence.Observation{}, fmt.Errorf("Codex normalized evidence is invalid: %w", err)
	}
	return observation, nil
}

func appendRateWindows(windows []evidence.Window, limit *rateLimit, prefix string, scope evidence.Scope, now time.Time) ([]evidence.Window, error) {
	if limit == nil {
		return windows, nil
	}
	primary := limit.PrimaryWindow
	if primary == nil {
		primary = limit.Primary
	}
	secondary := limit.SecondaryWindow
	if secondary == nil {
		secondary = limit.Secondary
	}
	rawWindows := [2]*usageWindow{primary, secondary}
	fallbacks := [2]string{"five_hour", "weekly"}
	var parsed [2]*evidence.Window
	for index, window := range rawWindows {
		if window == nil {
			continue
		}
		value, err := normalizeWindow(window, prefix, fallbacks[index], scope, now)
		if err != nil {
			return nil, err
		}
		parsed[index] = &value
	}
	for index, window := range parsed {
		if window != nil {
			windows = append(windows, *window)
			continue
		}
		id := prefix + fallbacks[index]
		for _, present := range parsed {
			if present != nil && string(present.ID) == id {
				id = prefix + fallbacks[1-index]
			}
		}
		windows = append(windows, evidence.Window{ID: evidence.WindowID(id), Scope: scope, Unit: "percent", Limits: []evidence.Limit{{ID: id + "_remaining", Field: evidence.FieldRemaining, Value: evidence.Value{State: evidence.ValueUnknown}}}})
	}
	return windows, nil
}

func normalizeWindow(raw *usageWindow, prefix, fallback string, scope evidence.Scope, now time.Time) (evidence.Window, error) {
	used := raw.UsedPercent
	if used == nil {
		used = raw.UsedPercentCamel
	}
	if used == nil || float64(*used) < 0 || float64(*used) > 100 {
		return evidence.Window{}, errors.New("Codex quota percentage is outside 0 through 100")
	}
	durationSeconds := 0.0
	if raw.WindowSeconds != nil {
		durationSeconds = float64(*raw.WindowSeconds)
	} else if raw.DurationMinutes != nil {
		durationSeconds = float64(*raw.DurationMinutes) * 60
	}
	if math.IsNaN(durationSeconds) || math.IsInf(durationSeconds, 0) || durationSeconds < 0 || durationSeconds > 315_360_000 {
		return evidence.Window{}, errors.New("Codex quota window duration is invalid")
	}
	id := fallback
	if durationSeconds == 18000 {
		id = "five_hour"
	} else if durationSeconds == 604800 {
		id = "weekly"
	} else if durationSeconds > 0 {
		id = "window_" + strconv.FormatFloat(durationSeconds, 'f', -1, 64)
	}
	id = prefix + id
	usedValue, _ := numericValue(float64(*used), true)
	remainingValue, _ := numericValue(100-float64(*used), true)
	limits := []evidence.Limit{{ID: id + "_used", Field: evidence.Field("used"), Value: usedValue}, {ID: id + "_remaining", Field: evidence.FieldRemaining, Value: remainingValue}}
	if durationSeconds > 0 {
		duration := time.Duration(durationSeconds * float64(time.Second))
		limits = append(limits, evidence.Limit{ID: id + "_duration", Field: evidence.FieldDuration, Value: evidence.Value{State: evidence.ValueUnknown}, Duration: &duration})
	}
	reset, err := resetTime(raw, now)
	if err != nil {
		return evidence.Window{}, err
	}
	if !reset.IsZero() {
		limits = append(limits, evidence.Limit{ID: id + "_reset", Field: evidence.FieldReset, Value: evidence.Value{State: evidence.ValueUnknown}, ResetAt: &reset})
	}
	return evidence.Window{ID: evidence.WindowID(id), Scope: scope, Unit: "percent", Limits: limits}, nil
}

func resetTime(raw *usageWindow, now time.Time) (time.Time, error) {
	reset := raw.ResetAt
	if reset == nil {
		reset = raw.ResetAtCamel
	}
	if reset != nil {
		return reset.Time, nil
	}
	if raw.ResetAfterSeconds != nil {
		if float64(*raw.ResetAfterSeconds) < 0 || float64(*raw.ResetAfterSeconds) > 315_360_000 {
			return time.Time{}, errors.New("Codex quota reset delay is invalid")
		}
		return now.Add(time.Duration(float64(*raw.ResetAfterSeconds) * float64(time.Second))), nil
	}
	return time.Time{}, nil
}

func numericValue(value float64, bound bool) (evidence.Value, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || (bound && value > 100) {
		return evidence.Value{}, errors.New("invalid numeric value")
	}
	number := evidence.JSONNumber(strconv.FormatFloat(value, 'f', -1, 64))
	state := evidence.ValueDefined
	if value == 0 {
		state = evidence.ValueZero
	}
	return evidence.Value{State: state, Amount: &number}, nil
}

func safeID(value string) string {
	var result strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			result.WriteRune(r)
		} else {
			result.WriteByte('_')
		}
	}
	return result.String()
}
