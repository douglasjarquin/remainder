package codex

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

const (
	maxResponseBytes = 1 << 20
	defaultTimeout   = 15 * time.Second
)

var defaultEndpoints = []string{
	"https://chatgpt.com/backend-api/wham/usage",
	"https://chatgpt.com/backend-api/codex/usage",
}

var errAccountMismatch = errors.New("Codex quota account mismatch")

type Options struct {
	AuthFile  string
	Endpoints []string
	Client    *http.Client
	Timeout   time.Duration
	Now       func() time.Time
}

type Adapter struct {
	authFile  string
	endpoints []string
	client    *http.Client
	timeout   time.Duration
	now       func() time.Time
}

func Default() Adapter {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		if userHome, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(userHome, ".codex")
		}
	}
	if home == "" {
		return New(Options{})
	}
	return New(Options{AuthFile: filepath.Join(home, "auth.json")})
}

func New(options Options) Adapter {
	client := options.Client
	if client == nil {
		client = &http.Client{}
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	endpoints := options.Endpoints
	if len(endpoints) == 0 {
		endpoints = defaultEndpoints
	}
	return Adapter{authFile: options.AuthFile, endpoints: append([]string(nil), endpoints...), client: &clientCopy, timeout: timeout, now: now}
}

func (a Adapter) Observe(ctx context.Context, request evidence.Request) (evidence.Observation, error) {
	if request.Provider != "codex" || request.Profile != "default" || request.All {
		return evidence.Observation{}, fmt.Errorf("%w: Codex native source requires --provider codex --profile default", evidence.ErrInvalidSelection)
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	credentials, err := readCredentials(a.authFile, a.now())
	if err != nil {
		return evidence.Observation{}, collectionError(err)
	}
	if request.Account != "" && request.Account != credentials.accountID {
		return evidence.Observation{}, evidence.ErrWrongAccount
	}
	var lastError error
	for _, endpoint := range a.endpoints {
		observation, retry, err := a.fetch(ctx, endpoint, credentials)
		if err == nil {
			return observation, nil
		}
		lastError = err
		if !retry {
			return evidence.Observation{}, collectionError(err)
		}
	}
	if lastError != nil {
		return evidence.Observation{}, collectionError(lastError)
	}
	return evidence.Observation{}, errors.New("Codex quota source is unavailable")
}

func collectionError(err error) error {
	return fmt.Errorf("%w: %w", evidence.ErrProviderUnavailable, err)
}

func (a Adapter) fetch(ctx context.Context, endpoint string, credentials credentials) (evidence.Observation, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return evidence.Observation{}, false, errors.New("Codex quota endpoint is invalid")
	}
	req.Header.Set("Authorization", "Bearer "+credentials.accessToken)
	req.Header.Set("ChatGPT-Account-Id", credentials.accountID)
	req.Header.Set("Accept", "application/json")
	response, err := a.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return evidence.Observation{}, false, fmt.Errorf("Codex quota request canceled or timed out: %w", ctx.Err())
		}
		return evidence.Observation{}, true, errors.New("Codex quota request failed")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return evidence.Observation{}, true, errors.New("Codex authentication was rejected")
	case http.StatusTooManyRequests:
		retryAfter := response.Header.Get("Retry-After")
		if retryAfter == "" {
			return evidence.Observation{}, false, errors.New("Codex quota endpoint is rate limited")
		}
		return evidence.Observation{}, false, fmt.Errorf("Codex quota endpoint is rate limited; retry after %s", safeRetryAfter(retryAfter))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return evidence.Observation{}, true, fmt.Errorf("Codex quota endpoint returned HTTP %d", response.StatusCode)
	}
	limited := io.LimitReader(response.Body, maxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return evidence.Observation{}, true, errors.New("Codex quota response could not be read")
	}
	if len(body) > maxResponseBytes {
		return evidence.Observation{}, false, errors.New("Codex quota response is too large")
	}
	var raw usageResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return evidence.Observation{}, true, errors.New("Codex quota response is malformed JSON")
	}
	observation, err := normalize(raw, credentials.accountID, a.now())
	if err != nil {
		return evidence.Observation{}, !errors.Is(err, errAccountMismatch), err
	}
	return observation, false, nil
}

func safeRetryAfter(value string) string {
	if seconds, err := strconv.ParseUint(value, 10, 32); err == nil {
		return strconv.FormatUint(seconds, 10) + " seconds"
	}
	if date, err := http.ParseTime(value); err == nil {
		return date.UTC().Format(http.TimeFormat)
	}
	return "the provider-specified interval"
}
