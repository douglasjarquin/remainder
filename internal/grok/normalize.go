package grok

import (
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

var productNames = map[uint64]string{
	0: "unspecified",
	1: "api",
	2: "grok_build",
	3: "grok_plugins",
	4: "chat",
	5: "imagine",
	6: "voice",
}

type period struct {
	start time.Time
	reset time.Time
}

func normalize(payload []byte, now time.Time) (evidence.Observation, error) {
	response, err := scanMessage(payload, 0)
	if err != nil {
		return evidence.Observation{}, err
	}
	configBytes, present, err := bytesAt(response, 1)
	if err != nil || !present {
		return evidence.Observation{}, errors.New("Grok quota response has no credits config")
	}
	config, err := scanMessage(configBytes, 1)
	if err != nil {
		return evidence.Observation{}, err
	}
	cycle, hasCycle, err := periodAt(config)
	if err != nil {
		return evidence.Observation{}, err
	}
	windows := make([]evidence.Window, 0)
	partial := false
	shared, hasShared, err := floatAt(config, 1)
	if err != nil {
		return evidence.Observation{}, err
	}
	if hasShared || hasCycle {
		window, unknown, err := percentWindow("credits", evidence.ScopeAccount, shared, hasShared, cycle, hasCycle)
		if err != nil {
			return evidence.Observation{}, err
		}
		windows = append(windows, window)
		partial = partial || unknown
	}
	productPayloads, err := allBytesAt(config, 7)
	if err != nil {
		return evidence.Observation{}, err
	}
	for _, raw := range productPayloads {
		product, err := scanMessage(raw, 2)
		if err != nil {
			return evidence.Observation{}, err
		}
		kind, present, err := varintAt(product, 1)
		if err != nil || !present {
			return evidence.Observation{}, errors.New("Grok product limit has no product kind")
		}
		name, ok := productNames[kind]
		if !ok {
			name = "unknown_" + strconv.FormatUint(kind, 10)
		}
		used, hasUsed, err := floatAt(product, 2)
		if err != nil {
			return evidence.Observation{}, err
		}
		id := "product:" + name
		window, unknown, err := percentWindow(id, evidence.Scope(id), used, hasUsed, cycle, hasCycle)
		if err != nil {
			return evidence.Observation{}, err
		}
		windows = append(windows, window)
		partial = partial || unknown
	}
	if prepaidBytes, present, err := bytesAt(config, 12); err != nil {
		return evidence.Observation{}, err
	} else if present {
		prepaid, err := scanMessage(prepaidBytes, 2)
		if err != nil {
			return evidence.Observation{}, err
		}
		balance, hasBalance, err := varintAt(prepaid, 1)
		if err != nil {
			return evidence.Observation{}, err
		}
		value := evidence.Value{State: evidence.ValueUnknown}
		if hasBalance {
			value = integerValue(balance)
		} else {
			partial = true
		}
		windows = append(windows, evidence.Window{ID: "prepaid", Scope: evidence.ScopeAccount, Unit: "credits", Limits: []evidence.Limit{{ID: "prepaid_remaining", Field: evidence.FieldRemaining, Value: value}}})
	}
	if len(windows) == 0 {
		return evidence.Observation{}, errors.New("Grok quota response has no entitlements")
	}
	outcome := evidence.OutcomeComplete
	if partial {
		outcome = evidence.OutcomePartial
	}
	observation := evidence.Observation{
		SchemaVersion: evidence.SchemaV1,
		Provider:      "grok",
		Profile:       "default",
		Account:       evidence.AccountIdentity{Binding: evidence.IdentityUnknown},
		Source:        evidence.SourceIdentity{Kind: "native_file_http", Name: "grok_auth_json"},
		ObservedAt:    now,
		Freshness:     evidence.FreshFresh,
		Outcome:       outcome,
		Windows:       windows,
	}
	if err := observation.Validate(); err != nil {
		return evidence.Observation{}, err
	}
	return observation, nil
}

func percentWindow(id string, scope evidence.Scope, used float64, present bool, cycle period, hasCycle bool) (evidence.Window, bool, error) {
	usedValue := evidence.Value{State: evidence.ValueUnknown}
	remainingValue := evidence.Value{State: evidence.ValueUnknown}
	unknown := !present
	if present {
		if used < 0 || used > 100 || math.IsNaN(used) || math.IsInf(used, 0) {
			return evidence.Window{}, false, errors.New("Grok quota percentage is outside 0 through 100")
		}
		usedValue = numericValue(used)
		remainingValue = numericValue(100 - used)
	}
	limits := []evidence.Limit{
		{ID: id + "_used", Field: evidence.Field("used"), Value: usedValue},
		{ID: id + "_remaining", Field: evidence.FieldRemaining, Value: remainingValue},
	}
	if hasCycle {
		duration := cycle.reset.Sub(cycle.start)
		limits = append(limits,
			evidence.Limit{ID: id + "_duration", Field: evidence.FieldDuration, Value: evidence.Value{State: evidence.ValueUnknown}, Duration: &duration},
			evidence.Limit{ID: id + "_reset", Field: evidence.FieldReset, Value: evidence.Value{State: evidence.ValueUnknown}, ResetAt: &cycle.reset},
		)
	}
	return evidence.Window{ID: evidence.WindowID(id), Scope: scope, Unit: "percent", Limits: limits}, unknown, nil
}

func numericValue(value float64) evidence.Value {
	number := evidence.JSONNumber(strconv.FormatFloat(value, 'f', -1, 64))
	state := evidence.ValueDefined
	if value == 0 {
		state = evidence.ValueZero
	}
	return evidence.Value{State: state, Amount: &number}
}

func integerValue(value uint64) evidence.Value {
	number := evidence.JSONNumber(strconv.FormatUint(value, 10))
	state := evidence.ValueDefined
	if value == 0 {
		state = evidence.ValueZero
	}
	return evidence.Value{State: state, Amount: &number}
}
