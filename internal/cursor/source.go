package cursor

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type usageResponse struct {
	BillingCycleStart *sourceTime      `json:"billingCycleStart"`
	BillingCycleEnd   *sourceTime      `json:"billingCycleEnd"`
	PlanUsage         *planUsage       `json:"planUsage"`
	SpendLimitUsage   *spendLimitUsage `json:"spendLimitUsage"`
}

type planUsage struct {
	TotalPercentUsed *sourceNumber `json:"totalPercentUsed"`
	AutoPercentUsed  *sourceNumber `json:"autoPercentUsed"`
	APIPercentUsed   *sourceNumber `json:"apiPercentUsed"`
}

type spendLimitUsage struct {
	IndividualLimit     *sourceNumber `json:"individualLimit"`
	IndividualRemaining *sourceNumber `json:"individualRemaining"`
	IndividualUsed      *sourceNumber `json:"individualUsed"`
}

type planResponse struct {
	PlanInfo *planInfo `json:"planInfo"`
}

type planInfo struct {
	BillingCycleStart *sourceTime `json:"billingCycleStart"`
	BillingCycleEnd   *sourceTime `json:"billingCycleEnd"`
}

type sandResponse struct {
	UsesPooledEnterpriseAllowance      *bool         `json:"usesPooledEnterpriseAllowance"`
	UsesPooledEnterpriseAllowanceSnake *bool         `json:"uses_pooled_enterprise_allowance"`
	UsagePercent                       *sourceNumber `json:"usagePercent"`
	UsagePercentSnake                  *sourceNumber `json:"usage_percent"`
	CurrentPeriodStart                 *sourceTime   `json:"currentPeriodStart"`
	CurrentPeriodStartSnake            *sourceTime   `json:"current_period_start"`
	NextResetTimestampUTC              *sourceTime   `json:"nextResetTimestampUtc"`
	NextResetTimestampUTCSnake         *sourceTime   `json:"next_reset_timestamp_utc"`
}

type sourceNumber struct {
	raw   string
	value float64
}

func (n *sourceNumber) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*n = sourceNumber{}
		return nil
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		raw = strings.TrimSpace(text)
	}
	value, err := parseJSONFloat(raw)
	if err != nil {
		return err
	}
	*n = sourceNumber{raw: raw, value: value}
	return nil
}

func parseJSONFloat(raw string) (float64, error) {
	encoded := jsontext.Value(raw)
	if !encoded.IsValid() || encoded.Kind() != '0' {
		return 0, errors.New("number must use JSON decimal syntax")
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, errors.New("number must be finite")
	}
	return value, nil
}

type sourceTime struct{ time.Time }

func (t *sourceTime) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			t.Time = parsed.UTC()
			return nil
		}
		value, err := parseJSONFloat(strings.TrimSpace(raw))
		if err != nil {
			return err
		}
		return t.setEpoch(value)
	}
	value, err := parseJSONFloat(strings.TrimSpace(string(data)))
	if err != nil {
		return err
	}
	return t.setEpoch(value)
}

func (t *sourceTime) setEpoch(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return errors.New("time must be a positive epoch")
	}
	seconds := value
	if value > 10_000_000_000 {
		seconds = value / 1000
	}
	if seconds > 253_402_300_799 {
		return errors.New("time is outside RFC3339 range")
	}
	t.Time = time.Unix(int64(seconds), 0).UTC()
	return nil
}

func decodeUsage(body []byte) (usageResponse, error) {
	var usage usageResponse
	if err := json.Unmarshal(body, &usage); err != nil {
		return usageResponse{}, fmtInvalidResponse("Cursor usage response is malformed JSON")
	}
	if usage.PlanUsage == nil && usage.SpendLimitUsage == nil {
		return usageResponse{}, fmtInvalidResponse("Cursor usage response schema has no supported meters")
	}
	return usage, nil
}

func fmtInvalidResponse(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidResponse, message)
}
