package codex

import (
	"context"
	"crypto/sha256"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

const (
	maxResponseBytes = 1 << 20
	defaultTimeout   = 15 * time.Second
)

var defaultEndpoints = []string{
	"https://chatgpt.com/backend-api/wham/usage",
}

var (
	ErrAccountMismatch       = errors.New("Codex quota account mismatch")
	ErrAuthorizationRejected = errors.New("Codex authentication was rejected")
	ErrTransient             = errors.New("transient Codex quota failure")
)

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

type RetryError struct {
	RetryAt time.Time
}

func (e *RetryError) Error() string {
	if e.RetryAt.IsZero() {
		return "Codex quota endpoint is rate limited"
	}
	return "Codex quota endpoint is rate limited until " + e.RetryAt.UTC().Format(time.RFC3339)
}

func (e *RetryError) Unwrap() error {
	return ErrTransient
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

func (a Adapter) CacheBinding(ctx context.Context, request evidence.Request) (cache.Binding, error) {
	if request.Provider != "codex" || request.Profile != "default" || request.All {
		return cache.Binding{}, fmt.Errorf("%w: Codex native source requires --provider codex --profile default", evidence.ErrInvalidSelection)
	}
	if err := ctx.Err(); err != nil {
		return cache.Binding{}, err
	}
	info, err := os.Lstat(a.authFile)
	if err != nil {
		return cache.Binding{}, collectionError(errors.New("Codex authentication file cannot be inspected"))
	}
	if !info.Mode().IsRegular() || info.Size() > maxAuthBytes {
		return cache.Binding{}, collectionError(errors.New("Codex authentication file is not a bounded regular file"))
	}
	identity := fmt.Sprintf("%s\x00%d\x00%d\x00%d", filepath.Clean(a.authFile), info.Size(), info.ModTime().UnixNano(), info.Mode())
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		identity += fmt.Sprintf("\x00%d\x00%d", stat.Dev, stat.Ino)
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
	return cache.Binding{Provider: "codex", Profile: "default", ResponseBoundary: "usage", SourceKind: "native_file_http", SourceName: "codex_auth_json", CredentialFingerprint: fingerprint}, nil
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

func (a Adapter) Failure(err error) (cache.FailureKind, time.Time) {
	switch {
	case err == nil:
		return cache.FailureNone, time.Time{}
	case errors.Is(err, ErrAuthorizationRejected):
		return cache.FailureRevoked, time.Time{}
	case errors.Is(err, ErrAccountMismatch), errors.Is(err, evidence.ErrWrongAccount):
		return cache.FailureAccountMismatch, time.Time{}
	case errors.Is(err, ErrTransient):
		if retry, ok := errors.AsType[*RetryError](err); ok {
			return cache.FailureTransient, retry.RetryAt
		}
		return cache.FailureTransient, time.Time{}
	default:
		return cache.FailurePermanent, time.Time{}
	}
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
			return evidence.Observation{}, false, fmt.Errorf("%w: Codex quota request canceled or timed out: %w", ErrTransient, ctx.Err())
		}
		return evidence.Observation{}, true, fmt.Errorf("%w: Codex quota request failed", ErrTransient)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return evidence.Observation{}, true, ErrAuthorizationRejected
	case http.StatusTooManyRequests:
		return evidence.Observation{}, false, &RetryError{RetryAt: parseRetryAfter(response.Header.Get("Retry-After"), a.now())}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return evidence.Observation{}, true, fmt.Errorf("%w: Codex quota endpoint returned HTTP %d", ErrTransient, response.StatusCode)
	}
	limited := io.LimitReader(response.Body, maxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return evidence.Observation{}, true, fmt.Errorf("%w: Codex quota response could not be read", ErrTransient)
	}
	if len(body) > maxResponseBytes {
		return evidence.Observation{}, false, errors.New("Codex quota response is too large")
	}
	var raw usageResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return evidence.Observation{}, true, fmt.Errorf("%w: Codex quota response is malformed JSON", ErrTransient)
	}
	observation, err := normalize(raw, credentials.accountID, a.now())
	if err != nil {
		return evidence.Observation{}, false, err
	}
	return observation, false, nil
}

func parseRetryAfter(value string, now time.Time) time.Time {
	if seconds, err := strconv.ParseUint(value, 10, 32); err == nil {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if date, err := http.ParseTime(value); err == nil {
		return date.UTC()
	}
	return time.Time{}
}
