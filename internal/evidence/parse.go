package evidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

type rawObservation struct {
	SchemaVersion string          `json:"schema_version"`
	Provider      Provider        `json:"provider"`
	Profile       Profile         `json:"profile"`
	Account       AccountIdentity `json:"account"`
	Source        SourceIdentity  `json:"source"`
	ObservedAt    string          `json:"observed_at"`
	SourceAt      *string         `json:"source_at,omitempty"`
	Freshness     Freshness       `json:"freshness"`
	Outcome       Outcome         `json:"outcome"`
	Windows       []rawWindow     `json:"windows"`
	Failures      []Failure       `json:"failures,omitempty"`
}

type rawWindow struct {
	ID     WindowID   `json:"id"`
	Scope  Scope      `json:"scope"`
	Unit   string     `json:"unit"`
	Limits []rawLimit `json:"limits"`
}

type rawLimit struct {
	ID       string       `json:"id"`
	Field    Field        `json:"field"`
	State    ValueState   `json:"state"`
	Amount   *json.Number `json:"amount,omitempty"`
	ResetAt  *string      `json:"reset_at,omitempty"`
	Duration *string      `json:"duration,omitempty"`
}

func ParseJSON(data []byte) (Observation, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var raw rawObservation
	if err := decoder.Decode(&raw); err != nil {
		return Observation{}, fmt.Errorf("decode observation: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return Observation{}, err
	}
	observedAt, err := parseTime(raw.ObservedAt)
	if err != nil {
		return Observation{}, err
	}
	o := Observation{SchemaVersion: raw.SchemaVersion, Provider: raw.Provider, Profile: raw.Profile, Account: raw.Account, Source: raw.Source, ObservedAt: observedAt, Freshness: raw.Freshness, Outcome: raw.Outcome, Failures: raw.Failures}
	if raw.SourceAt != nil {
		sourceAt, err := parseTime(*raw.SourceAt)
		if err != nil {
			return Observation{}, err
		}
		o.SourceAt = &sourceAt
	}
	for _, window := range raw.Windows {
		parsed := Window{ID: window.ID, Scope: window.Scope, Unit: window.Unit}
		for _, limit := range window.Limits {
			parsedLimit := Limit{ID: limit.ID, Field: limit.Field, Value: Value{State: limit.State, Amount: limit.Amount}}
			if limit.ResetAt != nil {
				resetAt, err := parseTime(*limit.ResetAt)
				if err != nil {
					return Observation{}, err
				}
				parsedLimit.ResetAt = &resetAt
			}
			if limit.Duration != nil {
				duration, err := time.ParseDuration(*limit.Duration)
				if err != nil {
					return Observation{}, fmt.Errorf("parse duration: %w", err)
				}
				parsedLimit.Duration = &duration
			}
			parsed.Limits = append(parsed.Limits, parsedLimit)
		}
		o.Windows = append(o.Windows, parsed)
	}
	if err := o.Validate(); err != nil {
		return Observation{}, err
	}
	return o, nil
}

func parseTime(raw string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %q: %v", ErrMalformedTime, raw, err)
	}
	return parsed, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return ErrInvalidObservation
		}
		return fmt.Errorf("decode trailing observation: %w", err)
	}
	return nil
}
