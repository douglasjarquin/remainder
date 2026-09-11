package claude

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
)

type sourceNumber string

func (n *sourceNumber) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*n = ""
		return nil
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		raw = strings.TrimSpace(value)
	}
	if !jsontext.Value(raw).IsValid() {
		return errors.New("number must use JSON decimal syntax")
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return errors.New("number must be finite")
	}
	*n = sourceNumber(raw)
	return nil
}

func (n sourceNumber) float64() (float64, bool) {
	if n == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(string(n), 64)
	return value, err == nil && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (n sourceNumber) int64() (int64, bool) {
	if n == "" {
		return 0, false
	}
	value, err := strconv.ParseInt(string(n), 10, 64)
	return value, err == nil
}

type profileResponse struct {
	Account struct {
		UUID string `json:"uuid"`
	} `json:"account"`
	Organization struct {
		UUID string `json:"uuid"`
	} `json:"organization"`
}

type profileIdentity struct {
	accountID string
}

type usageResponse struct {
	Limits       *[]scopedLimit `json:"limits"`
	FiveHour     *legacyWindow  `json:"five_hour"`
	SevenDay     *legacyWindow  `json:"seven_day"`
	SevenDayOpus *legacyWindow  `json:"seven_day_opus"`
	ExtraUsage   *extraUsage    `json:"extra_usage"`
}

type legacyWindow struct {
	Utilization sourceNumber `json:"utilization"`
	ResetsAt    string       `json:"resets_at"`
	ResetAt     string       `json:"reset_at"`
}

type scopedLimit struct {
	Kind     string       `json:"kind"`
	Group    string       `json:"group"`
	Percent  sourceNumber `json:"percent"`
	ResetsAt string       `json:"resets_at"`
	Scope    struct {
		Model struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"model"`
	} `json:"scope"`
}

type extraUsage struct {
	Enabled       bool         `json:"is_enabled"`
	Utilization   sourceNumber `json:"utilization"`
	Used          sourceNumber `json:"used_credits"`
	MonthlyLimit  sourceNumber `json:"monthly_limit"`
	DecimalPlaces sourceNumber `json:"decimal_places"`
}

func (a Adapter) fetchProfile(ctx context.Context, token string) (profileIdentity, error) {
	var raw profileResponse
	if err := a.fetchJSON(ctx, a.profileEndpoint, token, &raw); err != nil {
		return profileIdentity{}, err
	}
	if raw.Account.UUID == "" {
		return profileIdentity{}, errors.New("Claude profile response has no account UUID")
	}
	accountID := raw.Account.UUID
	if raw.Organization.UUID != "" {
		accountID += "@" + raw.Organization.UUID
	}
	return profileIdentity{accountID: accountID}, nil
}

func (a Adapter) fetchUsage(ctx context.Context, token string) (usageResponse, error) {
	var raw usageResponse
	if err := a.fetchJSON(ctx, a.usageEndpoint, token, &raw); err != nil {
		return usageResponse{}, err
	}
	return raw, nil
}

func (a Adapter) fetchJSON(ctx context.Context, endpoint, token string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errors.New("Claude quota endpoint is invalid")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", oauthBeta)
	req.Header.Set("Accept", "application/json")
	response, err := a.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: Claude quota request canceled or timed out: %w", ErrTransient, ctx.Err())
		}
		return fmt.Errorf("%w: Claude quota request failed", ErrTransient)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return ErrAuthorizationRejected
	case http.StatusTooManyRequests:
		return &RetryError{RetryAt: parseRetryAfter(response.Header.Get("Retry-After"), a.now())}
	case http.StatusForbidden:
		return fmt.Errorf("%w: Claude quota endpoint returned HTTP 403", ErrTransient)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%w: Claude quota endpoint returned HTTP %d", ErrTransient, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("%w: Claude quota response could not be read", ErrTransient)
	}
	if len(body) > maxResponseBytes {
		return errors.New("Claude quota response is too large")
	}
	if err := json.UnmarshalRead(bytes.NewReader(body), target); err != nil {
		return fmt.Errorf("%w: Claude quota response is malformed JSON", ErrTransient)
	}
	return nil
}
