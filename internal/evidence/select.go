package evidence

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrNoMatch          = errors.New("no matching evidence")
	ErrInvalidSelection = errors.New("invalid evidence selection")
)

func (o Observation) ForRequest(request Request) (Observation, error) {
	if err := o.Validate(); err != nil {
		return Observation{}, err
	}
	if request.Provider != "" && o.Provider != request.Provider {
		return Observation{}, ErrNoMatch
	}
	if request.Profile != "" && o.Profile != request.Profile {
		return Observation{}, ErrNoMatch
	}
	if request.Account != "" && (o.Account.LastObserved != request.Account || o.Account.Binding == IdentityMismatch) {
		return Observation{}, ErrWrongAccount
	}
	if request.Window == "" && request.Scope == "" {
		return o, nil
	}
	filtered := o
	filtered.Windows = nil
	for _, window := range o.Windows {
		if request.Window != "" && window.ID != request.Window {
			continue
		}
		if request.Scope != "" && window.Scope != request.Scope {
			continue
		}
		filtered.Windows = append(filtered.Windows, window)
	}
	if len(filtered.Windows) == 0 {
		return Observation{}, ErrNoMatch
	}
	return filtered, nil
}

func SelectValue(observation Observation, request ValueRequest, policy FreshnessPolicy) (string, error) {
	if err := observation.Validate(); err != nil {
		return "", err
	}
	if request.Provider == "" || request.Profile == "" || request.Field == "" {
		return "", ErrInvalidSelection
	}
	if observation.Provider != request.Provider || observation.Profile != request.Profile {
		return "", ErrNoMatch
	}
	if policy == FreshOnly && observation.Freshness != FreshFresh {
		return "", ErrStale
	}
	if policy != FreshAny && policy != FreshOnly {
		return "", ErrInvalidSelection
	}
	if request.Account != "" && (observation.Account.LastObserved != request.Account || observation.Account.Binding == IdentityMismatch) {
		return "", ErrWrongAccount
	}
	var matches []Limit
	for _, window := range observation.Windows {
		if (request.Window != "" && window.ID != request.Window) || (request.Scope != "" && window.Scope != request.Scope) {
			continue
		}
		for _, limit := range window.Limits {
			if limit.Field == request.Field {
				matches = append(matches, limit)
			}
		}
	}
	if len(matches) == 0 {
		return "", ErrNoMatch
	}
	if len(matches) > 1 {
		return "", ErrAmbiguous
	}
	value := matches[0]
	if value.Field == FieldReset {
		if value.ResetAt == nil {
			return "", ErrUndefined
		}
		return value.ResetAt.Format(time.RFC3339Nano) + "\n", nil
	}
	if value.Field == FieldDuration {
		if value.Duration == nil {
			return "", ErrUndefined
		}
		return value.Duration.String() + "\n", nil
	}
	switch value.Value.State {
	case ValueDefined, ValueZero:
		if value.Value.Amount == nil {
			return "", ErrUndefined
		}
		return value.Value.Amount.String() + "\n", nil
	case ValueUnknown, ValueNotApplicable:
		return "", ErrUndefined
	case ValueUnlimited:
		return "unlimited\n", nil
	default:
		return "", fmt.Errorf("%w: unknown state %q", ErrUndefined, value.Value.State)
	}
}
