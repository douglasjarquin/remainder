package evidence

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func RenderCompact(observation Observation, now time.Time) (string, error) {
	if err := observation.Validate(); err != nil {
		return "", err
	}
	age := now.Sub(observation.ObservedAt)
	age = max(age, 0)
	parts := []string{
		"schema=" + observation.SchemaVersion,
		"provider=" + strconv.Quote(string(observation.Provider)),
		"profile=" + strconv.Quote(string(observation.Profile)),
		"observed_at=" + observation.ObservedAt.Format(time.RFC3339Nano),
		"age_seconds=" + strconv.FormatInt(int64(age/time.Second), 10),
		"freshness=" + string(observation.Freshness),
		"outcome=" + string(observation.Outcome),
		"identity=" + safeToken(string(observation.Account.Binding)),
		"account=" + strconv.Quote(observation.Account.LastObserved),
		"source=" + strconv.Quote(observation.Source.Kind+"/"+observation.Source.Name),
	}
	windows := append([]Window(nil), observation.Windows...)
	sort.Slice(windows, func(i, j int) bool { return windows[i].ID < windows[j].ID })
	windowParts := make([]string, 0, len(windows))
	for _, window := range windows {
		limits := append([]Limit(nil), window.Limits...)
		sort.Slice(limits, func(i, j int) bool { return limits[i].ID < limits[j].ID })
		limitParts := make([]string, 0, len(limits))
		for _, limit := range limits {
			part := safeToken(string(limit.Field)) + "=" + formatValue(limit, safeToken(window.Unit))
			if limit.ResetAt != nil {
				part += " reset=" + limit.ResetAt.Format(time.RFC3339Nano)
			}
			if limit.Duration != nil {
				part += " duration=" + limit.Duration.String()
			}
			limitParts = append(limitParts, part)
		}
		windowParts = append(windowParts, safeToken(string(window.ID))+"/"+safeToken(string(window.Scope))+":"+strings.Join(limitParts, ","))
	}
	parts = append(parts, "windows="+strings.Join(windowParts, ";"))
	if len(observation.Failures) > 0 {
		failures := make([]string, 0, len(observation.Failures))
		for _, failure := range observation.Failures {
			failures = append(failures, strconv.Quote(failure.Scope+":"+failure.Message))
		}
		parts = append(parts, "failures="+strings.Join(failures, ","))
	}
	return strings.Join(parts, " "), nil
}

func safeToken(value string) string {
	if value == "" || strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || strings.ContainsRune(" \t\r\n;:=/,\\\"", r)
	}) >= 0 {
		return strconv.Quote(value)
	}
	return value
}

func formatValue(limit Limit, unit string) string {
	switch limit.Value.State {
	case ValueDefined, ValueZero:
		if limit.Value.Amount == nil {
			return "unknown"
		}
		return limit.Value.Amount.String() + unit
	case ValueUnknown:
		return string(ValueUnknown)
	case ValueUnlimited:
		return string(ValueUnlimited)
	case ValueNotApplicable:
		return string(ValueNotApplicable)
	default:
		return "unknown"
	}
}

type jsonObservation struct {
	SchemaVersion string          `json:"schema_version"`
	Provider      Provider        `json:"provider"`
	Profile       Profile         `json:"profile"`
	Account       AccountIdentity `json:"account"`
	Source        SourceIdentity  `json:"source"`
	ObservedAt    string          `json:"observed_at"`
	SourceAt      *string         `json:"source_at,omitempty"`
	Freshness     Freshness       `json:"freshness"`
	Outcome       Outcome         `json:"outcome"`
	Windows       []jsonWindow    `json:"windows"`
	Failures      []Failure       `json:"failures,omitempty"`
}

type jsonWindow struct {
	ID     WindowID    `json:"id"`
	Scope  Scope       `json:"scope"`
	Unit   string      `json:"unit"`
	Limits []jsonLimit `json:"limits"`
}

type jsonLimit struct {
	ID       string       `json:"id"`
	Field    Field        `json:"field"`
	State    ValueState   `json:"state"`
	Amount   *json.Number `json:"amount,omitempty"`
	ResetAt  *string      `json:"reset_at,omitempty"`
	Duration *string      `json:"duration,omitempty"`
}

func RenderJSON(observation Observation) ([]byte, error) {
	if err := observation.Validate(); err != nil {
		return nil, err
	}
	result := jsonObservation{
		SchemaVersion: observation.SchemaVersion,
		Provider:      observation.Provider,
		Profile:       observation.Profile,
		Account:       observation.Account,
		Source:        observation.Source,
		ObservedAt:    observation.ObservedAt.Format(time.RFC3339Nano),
		Freshness:     observation.Freshness,
		Outcome:       observation.Outcome,
		Failures:      observation.Failures,
	}
	if observation.SourceAt != nil {
		value := observation.SourceAt.Format(time.RFC3339Nano)
		result.SourceAt = &value
	}
	windows := append([]Window(nil), observation.Windows...)
	sort.Slice(windows, func(i, j int) bool { return windows[i].ID < windows[j].ID })
	for _, window := range windows {
		limits := append([]Limit(nil), window.Limits...)
		sort.Slice(limits, func(i, j int) bool { return limits[i].ID < limits[j].ID })
		parsed := jsonWindow{ID: window.ID, Scope: window.Scope, Unit: window.Unit}
		for _, limit := range limits {
			item := jsonLimit{ID: limit.ID, Field: limit.Field, State: limit.Value.State, Amount: limit.Value.Amount}
			if limit.ResetAt != nil {
				value := limit.ResetAt.Format(time.RFC3339Nano)
				item.ResetAt = &value
			}
			if limit.Duration != nil {
				value := limit.Duration.String()
				item.Duration = &value
			}
			parsed.Limits = append(parsed.Limits, item)
		}
		result.Windows = append(result.Windows, parsed)
	}
	return json.Marshal(result)
}
