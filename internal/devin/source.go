package devin

import (
	json "encoding/json/v2"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

type statusResponse struct {
	UserStatus *userStatus `json:"userStatus"`
	PlanInfo   *planInfo   `json:"planInfo"`
}

type userStatus struct {
	UserID     string      `json:"userId"`
	PlanStatus *planStatus `json:"planStatus"`
}

type planStatus struct {
	PlanStart                   *sourceReset  `json:"planStart"`
	PlanEnd                     *sourceReset  `json:"planEnd"`
	PlanInfo                    *planInfo     `json:"planInfo"`
	DailyQuotaRemainingPercent  *sourceNumber `json:"dailyQuotaRemainingPercent"`
	DailyQuotaResetAtUnix       *sourceReset  `json:"dailyQuotaResetAtUnix"`
	WeeklyQuotaRemainingPercent *sourceNumber `json:"weeklyQuotaRemainingPercent"`
	WeeklyQuotaResetAtUnix      *sourceReset  `json:"weeklyQuotaResetAtUnix"`
	AvailablePromptCredits      *sourceNumber `json:"availablePromptCredits"`
	UsedPromptCredits           *sourceNumber `json:"usedPromptCredits"`
	AvailableFlowCredits        *sourceNumber `json:"availableFlowCredits"`
	UsedFlowCredits             *sourceNumber `json:"usedFlowCredits"`
	AvailableFlexCredits        *sourceNumber `json:"availableFlexCredits"`
	UsedFlexCredits             *sourceNumber `json:"usedFlexCredits"`
	AcuConsumed                 *sourceNumber `json:"acuConsumed"`
	AcuLimit                    *sourceNumber `json:"acuLimit"`
}

type planInfo struct {
	MonthlyPromptCredits *sourceNumber `json:"monthlyPromptCredits"`
	MonthlyFlowCredits   *sourceNumber `json:"monthlyFlowCredits"`
}

type connectError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type sourceNumber float64

type sourceReset struct {
	time.Time
}

const maxRFC3339Unix = 253402300799

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
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > maxRFC3339Unix {
		return errors.New("reset time must be a positive RFC3339-representable epoch")
	}
	r.Time = time.Unix(int64(value), 0).UTC()
	return nil
}
