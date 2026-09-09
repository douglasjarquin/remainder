package codex

import (
	json "encoding/json/v2"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

type usageResponse struct {
	AccountID            string            `json:"account_id"`
	AccountIDCamel       string            `json:"accountId"`
	RateLimit            *rateLimit        `json:"rate_limit"`
	RateLimits           *rateLimit        `json:"rateLimits"`
	RateLimitsSnake      *rateLimit        `json:"rate_limits"`
	CodeReviewRateLimit  *rateLimit        `json:"code_review_rate_limit"`
	AdditionalRateLimits []additionalLimit `json:"additional_rate_limits"`
	Credits              *credits          `json:"credits"`
}

type rateLimit struct {
	PrimaryWindow   *usageWindow `json:"primary_window"`
	SecondaryWindow *usageWindow `json:"secondary_window"`
	Primary         *usageWindow `json:"primary"`
	Secondary       *usageWindow `json:"secondary"`
}

type usageWindow struct {
	UsedPercent       *sourceNumber `json:"used_percent"`
	UsedPercentCamel  *sourceNumber `json:"usedPercent"`
	WindowSeconds     *sourceNumber `json:"limit_window_seconds"`
	DurationMinutes   *sourceNumber `json:"windowDurationMins"`
	ResetAt           *sourceReset  `json:"reset_at"`
	ResetAtCamel      *sourceReset  `json:"resetsAt"`
	ResetAfterSeconds *sourceNumber `json:"reset_after_seconds"`
}

type additionalLimit struct {
	MeteredFeature string     `json:"metered_feature"`
	LimitName      string     `json:"limit_name"`
	RateLimit      *rateLimit `json:"rate_limit"`
}

type credits struct {
	Balance   *sourceNumber `json:"balance"`
	Unlimited *bool         `json:"unlimited"`
}

type sourceNumber float64

type sourceReset struct {
	time.Time
}

func (n *sourceNumber) UnmarshalJSON(data []byte) error {
	var value float64
	if len(data) > 0 && data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return err
		}
		value = parsed
	} else if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return errors.New("number must be finite")
	}
	*n = sourceNumber(value)
	return nil
}

func (r *sourceReset) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			r.Time = parsed.UTC()
			return nil
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return err
		}
		return r.setUnix(value)
	}
	var value float64
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	return r.setUnix(value)
}

func (r *sourceReset) setUnix(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > math.MaxInt64 {
		return errors.New("reset time must be a finite positive epoch")
	}
	r.Time = time.Unix(int64(value), 0).UTC()
	return nil
}
