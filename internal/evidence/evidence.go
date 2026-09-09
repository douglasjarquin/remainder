package evidence

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

const SchemaV1 = "v1"

type (
	Provider string
	Profile  string
	WindowID string
	Scope    string
	Field    string
)

const (
	ScopeAccount Scope = "account"
	ScopeModel   Scope = "model"
	ScopeGlobal  Scope = "global"

	FieldRemaining Field = "remaining"
	FieldReset     Field = "reset"
	FieldDuration  Field = "duration"
)

type ValueState string

const (
	ValueDefined       ValueState = "defined"
	ValueZero          ValueState = "zero"
	ValueUnknown       ValueState = "unknown"
	ValueUnlimited     ValueState = "unlimited"
	ValueNotApplicable ValueState = "not_applicable"
)

type Freshness string

const (
	FreshFresh   Freshness = "fresh"
	FreshStale   Freshness = "stale"
	FreshUnknown Freshness = "unknown"
)

type FreshnessPolicy string

const (
	FreshAny  FreshnessPolicy = "any"
	FreshOnly FreshnessPolicy = "fresh"
)

type Outcome string

const (
	OutcomeComplete    Outcome = "complete"
	OutcomePartial     Outcome = "partial"
	OutcomeUnavailable Outcome = "unavailable"
)

type IdentityBinding string

const (
	IdentityVerified   IdentityBinding = "verified"
	IdentityHistorical IdentityBinding = "historical"
	IdentityUnknown    IdentityBinding = "unknown"
	IdentityMismatch   IdentityBinding = "mismatch"
)

type AccountIdentity struct {
	LastObserved string          `json:"last_observed"`
	Binding      IdentityBinding `json:"binding"`
}

type SourceIdentity struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type Value struct {
	State  ValueState   `json:"state"`
	Amount *json.Number `json:"amount,omitempty"`
}

type Limit struct {
	ID       string         `json:"id"`
	Field    Field          `json:"field"`
	Value    Value          `json:"value"`
	ResetAt  *time.Time     `json:"reset_at,omitempty"`
	Duration *time.Duration `json:"duration,omitempty"`
}

type Window struct {
	ID     WindowID `json:"id"`
	Scope  Scope    `json:"scope"`
	Unit   string   `json:"unit"`
	Limits []Limit  `json:"limits"`
	Pace   *Pace    `json:"pace,omitempty"`
}

type Failure struct {
	Scope   string `json:"scope"`
	Message string `json:"message"`
}

type Observation struct {
	SchemaVersion string          `json:"schema_version"`
	Provider      Provider        `json:"provider"`
	Profile       Profile         `json:"profile"`
	Account       AccountIdentity `json:"account"`
	Source        SourceIdentity  `json:"source"`
	ObservedAt    time.Time       `json:"observed_at"`
	SourceAt      *time.Time      `json:"source_at,omitempty"`
	Freshness     Freshness       `json:"freshness"`
	Outcome       Outcome         `json:"outcome"`
	Windows       []Window        `json:"windows"`
	Failures      []Failure       `json:"failures,omitempty"`
}

type Request struct {
	Provider  Provider
	Profile   Profile
	Window    WindowID
	Scope     Scope
	Field     Field
	Account   string
	Freshness FreshnessPolicy
	All       bool
}

type ValueRequest struct {
	Provider Provider
	Profile  Profile
	Window   WindowID
	Scope    Scope
	Field    Field
	Account  string
}

var (
	ErrInvalidObservation  = errors.New("invalid observation")
	ErrDuplicateID         = errors.New("duplicate evidence id")
	ErrMalformedTime       = errors.New("malformed evidence time")
	ErrAmbiguous           = errors.New("ambiguous evidence selection")
	ErrStale               = errors.New("evidence is stale")
	ErrWrongAccount        = errors.New("evidence account does not match")
	ErrUndefined           = errors.New("evidence value is undefined")
	ErrProviderUnavailable = errors.New("provider evidence is unavailable")
)

func (o Observation) Validate() error {
	if o.SchemaVersion != SchemaV1 || o.Provider == "" || o.Profile == "" || o.ObservedAt.IsZero() {
		return ErrInvalidObservation
	}
	if o.Freshness != FreshFresh && o.Freshness != FreshStale && o.Freshness != FreshUnknown {
		return ErrInvalidObservation
	}
	if o.Outcome != OutcomeComplete && o.Outcome != OutcomePartial && o.Outcome != OutcomeUnavailable {
		return ErrInvalidObservation
	}
	windowIDs := make(map[WindowID]struct{}, len(o.Windows))
	limitIDs := make(map[string]struct{})
	for _, window := range o.Windows {
		if window.ID == "" {
			return ErrInvalidObservation
		}
		if _, exists := windowIDs[window.ID]; exists {
			return ErrDuplicateID
		}
		windowIDs[window.ID] = struct{}{}
		for _, limit := range window.Limits {
			if limit.ID == "" {
				return ErrInvalidObservation
			}
			if _, exists := limitIDs[limit.ID]; exists {
				return ErrDuplicateID
			}
			limitIDs[limit.ID] = struct{}{}
			if err := limit.Value.validate(); err != nil {
				return err
			}
			if window.Unit == "percent" && (limit.Field == FieldRemaining || limit.Field == Field("used")) && limit.Value.Amount != nil {
				amount, err := strconv.ParseFloat(limit.Value.Amount.String(), 64)
				if err != nil || amount > 100 {
					return ErrInvalidObservation
				}
			}
		}
		if window.Pace != nil {
			if err := window.Pace.validate(o, window); err != nil {
				return err
			}
		}
	}
	return nil
}

func (v Value) validate() error {
	switch v.State {
	case ValueDefined, ValueZero:
		if v.Amount == nil {
			return ErrInvalidObservation
		}
		if err := validateNumber(v.Amount); err != nil {
			return err
		}
		if v.State == ValueZero && !isZeroNumber(*v.Amount) {
			return ErrInvalidObservation
		}
	case ValueUnknown, ValueUnlimited, ValueNotApplicable:
		if v.Amount != nil {
			return ErrInvalidObservation
		}
	default:
		return ErrInvalidObservation
	}
	return nil
}

func validateNumber(number *json.Number) error {
	if err := validateSignedNumber(number); err != nil {
		return err
	}
	value, _ := strconv.ParseFloat(number.String(), 64)
	if value < 0 {
		return ErrInvalidObservation
	}
	return nil
}

func validateSignedNumber(number *json.Number) error {
	raw := number.String()
	var parsed json.Number
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || parsed.String() != raw {
		return ErrInvalidObservation
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return ErrInvalidObservation
	}
	return nil
}

func isZeroNumber(number json.Number) bool {
	raw := strings.TrimPrefix(number.String(), "-")
	if exponent := strings.IndexAny(raw, "eE"); exponent >= 0 {
		raw = raw[:exponent]
	}
	raw = strings.ReplaceAll(raw, ".", "")
	return strings.Trim(raw, "0") == ""
}

type JSONNumber = json.Number
