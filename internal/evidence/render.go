package evidence

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/douglasjarquin/go-toon"
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
		return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || strings.ContainsRune(" \t\r\n;:=/,\\\"", r)
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
	SchemaVersion string        `json:"schema_version" toon:"schema_version"`
	Provider      string        `json:"provider" toon:"provider"`
	Profile       string        `json:"profile" toon:"profile"`
	Account       jsonAccount   `json:"account" toon:"account"`
	Source        jsonSource    `json:"source" toon:"source"`
	ObservedAt    string        `json:"observed_at" toon:"observed_at"`
	SourceAt      *string       `json:"source_at,omitempty" toon:"source_at,omitempty"`
	Freshness     string        `json:"freshness" toon:"freshness"`
	Outcome       string        `json:"outcome" toon:"outcome"`
	Windows       []jsonWindow  `json:"windows" toon:"windows"`
	Failures      []jsonFailure `json:"failures,omitempty" toon:"failures,omitempty"`
}

type jsonAccount struct {
	LastObserved string `json:"last_observed" toon:"last_observed"`
	Binding      string `json:"binding" toon:"binding"`
}

type jsonSource struct {
	Kind string `json:"kind" toon:"kind"`
	Name string `json:"name" toon:"name"`
}

type jsonFailure struct {
	Scope   string `json:"scope" toon:"scope"`
	Message string `json:"message" toon:"message"`
}

type jsonWindow struct {
	ID     string      `json:"id" toon:"id"`
	Scope  string      `json:"scope" toon:"scope"`
	Unit   string      `json:"unit" toon:"unit"`
	Limits []jsonLimit `json:"limits" toon:"limits"`
}

type jsonLimit struct {
	ID       string  `json:"id" toon:"id"`
	Field    string  `json:"field" toon:"field"`
	State    string  `json:"state" toon:"state"`
	Amount   any     `json:"amount,omitempty" toon:"amount,omitempty"`
	ResetAt  *string `json:"reset_at,omitempty" toon:"reset_at,omitempty"`
	Duration *string `json:"duration,omitempty" toon:"duration,omitempty"`
}

func reportDocument(observation Observation) jsonObservation {
	result := jsonObservation{
		SchemaVersion: observation.SchemaVersion,
		Provider:      string(observation.Provider),
		Profile:       string(observation.Profile),
		Account: jsonAccount{
			LastObserved: observation.Account.LastObserved,
			Binding:      string(observation.Account.Binding),
		},
		Source: jsonSource{
			Kind: observation.Source.Kind,
			Name: observation.Source.Name,
		},
		ObservedAt: observation.ObservedAt.Format(time.RFC3339Nano),
		Freshness:  string(observation.Freshness),
		Outcome:    string(observation.Outcome),
	}
	if observation.SourceAt != nil {
		value := observation.SourceAt.Format(time.RFC3339Nano)
		result.SourceAt = &value
	}
	if len(observation.Failures) > 0 {
		result.Failures = make([]jsonFailure, 0, len(observation.Failures))
		for _, failure := range observation.Failures {
			result.Failures = append(result.Failures, jsonFailure{Scope: failure.Scope, Message: failure.Message})
		}
	}
	windows := append([]Window(nil), observation.Windows...)
	sort.Slice(windows, func(i, j int) bool { return windows[i].ID < windows[j].ID })
	for _, window := range windows {
		limits := append([]Limit(nil), window.Limits...)
		sort.Slice(limits, func(i, j int) bool { return limits[i].ID < limits[j].ID })
		parsed := jsonWindow{ID: string(window.ID), Scope: string(window.Scope), Unit: window.Unit}
		for _, limit := range limits {
			item := jsonLimit{ID: limit.ID, Field: string(limit.Field), State: string(limit.Value.State)}
			if limit.Value.Amount != nil {
				item.Amount = *limit.Value.Amount
			}
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
	return result
}

func RenderJSON(observation Observation) ([]byte, error) {
	if err := observation.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(reportDocument(observation))
}

func RenderTOON(observation Observation) ([]byte, error) {
	if err := observation.Validate(); err != nil {
		return nil, err
	}
	return toon.Marshal(reportDocument(observation))
}
