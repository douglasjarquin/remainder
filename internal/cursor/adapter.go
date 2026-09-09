package cursor

import (
	"bytes"
	"context"
	"crypto/sha256"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

const (
	defaultEndpoint  = "https://api2.cursor.sh"
	defaultTimeout   = 15 * time.Second
	maxResponseBytes = 1 << 20
)

var (
	ErrUnsupported     = errors.New("Cursor CLI file source is unsupported on this operating system")
	ErrInvalidAuth     = errors.New("Cursor CLI authentication is invalid")
	ErrRevoked         = errors.New("Cursor CLI authentication was rejected")
	ErrForbidden       = errors.New("Cursor quota request was forbidden")
	ErrTransient       = errors.New("transient Cursor quota failure")
	ErrInvalidResponse = errors.New("Cursor quota response is invalid")
)

type Options struct {
	AuthFile string

	endpoint string
	client   *http.Client
	timeout  time.Duration
	now      func() time.Time
	goos     string
}

type Adapter struct {
	authFile string
	pathErr  error
	endpoint string
	client   *http.Client
	timeout  time.Duration
	now      func() time.Time
	goos     string
}

type RetryError struct{ RetryAt time.Time }

func (e *RetryError) Error() string {
	if e.RetryAt.IsZero() {
		return "Cursor quota endpoint is rate limited"
	}
	return "Cursor quota endpoint is rate limited until " + e.RetryAt.UTC().Format(time.RFC3339)
}

func (e *RetryError) Unwrap() error { return ErrTransient }

func Default() Adapter {
	return defaultWithRuntime(runtime.GOOS, os.LookupEnv)
}

func defaultWithRuntime(goos string, getenv func(string) (string, bool)) Adapter {
	path, err := defaultAuthPath(getenv)
	adapter := New(Options{AuthFile: path, goos: goos})
	adapter.pathErr = err
	return adapter
}

func New(options Options) Adapter {
	client := options.client
	if client == nil {
		client = &http.Client{}
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	timeout := options.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	now := options.now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	goos := options.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	endpoint := options.endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	return Adapter{authFile: options.AuthFile, endpoint: endpoint, client: &clientCopy, timeout: timeout, now: now, goos: goos}
}

func (a Adapter) CacheBinding(ctx context.Context, request evidence.Request) (cache.Binding, error) {
	if err := a.validateRequest(ctx, request); err != nil {
		return cache.Binding{}, err
	}
	info, err := inspectAuthFile(a.authFile)
	if err != nil {
		return cache.Binding{}, collectionError(err)
	}
	identity := fmt.Sprintf("%s\x00%d\x00%d\x00%d", filepath.Clean(a.authFile), info.Size(), info.ModTime().UnixNano(), info.Mode())
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		identity += fmt.Sprintf("\x00%d\x00%d", stat.Dev, stat.Ino)
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
	return cache.Binding{Provider: "cursor", Profile: "default", ResponseBoundary: "usage", SourceKind: "native_file_http", SourceName: "cursor_cli_auth_json", CredentialFingerprint: fingerprint}, nil
}

func (a Adapter) Observe(ctx context.Context, request evidence.Request) (evidence.Observation, error) {
	if err := a.validateRequest(ctx, request); err != nil {
		return evidence.Observation{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	token, err := readAccessToken(a.authFile, a.now())
	if err != nil {
		return evidence.Observation{}, collectionError(err)
	}
	usageBody, err := a.postRPC(ctx, token, "GetCurrentPeriodUsage")
	if err != nil {
		return evidence.Observation{}, collectionError(err)
	}
	usage, err := decodeUsage(usageBody)
	if err != nil {
		return evidence.Observation{}, collectionError(err)
	}

	var plan planResponse
	var sand sandResponse
	failures := make([]evidence.Failure, 0, 2)
	if body, rpcErr := a.postRPC(ctx, token, "GetPlanInfo"); rpcErr != nil {
		failures = append(failures, evidence.Failure{Scope: "plan", Message: safeFailure(rpcErr)})
	} else if decodeErr := json.Unmarshal(body, &plan); decodeErr != nil {
		failures = append(failures, evidence.Failure{Scope: "plan", Message: "Cursor plan response is malformed JSON"})
	}
	if body, rpcErr := a.postRPC(ctx, token, "GetSandUsageStatus"); rpcErr != nil {
		failures = append(failures, evidence.Failure{Scope: "grok_bot", Message: safeFailure(rpcErr)})
	} else if decodeErr := json.Unmarshal(body, &sand); decodeErr != nil {
		failures = append(failures, evidence.Failure{Scope: "grok_bot", Message: "Cursor Grok Bot response is malformed JSON"})
	}
	return normalize(usage, plan, sand, failures, a.now())
}

func (a Adapter) validateRequest(ctx context.Context, request evidence.Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.goos != "linux" {
		return collectionError(ErrUnsupported)
	}
	if request.Provider != "cursor" || request.Profile != "default" || request.All {
		return fmt.Errorf("%w: Cursor CLI file source requires --provider cursor --profile default", evidence.ErrInvalidSelection)
	}
	if request.Account != "" {
		return evidence.ErrWrongAccount
	}
	if a.pathErr != nil {
		return collectionError(a.pathErr)
	}
	if a.authFile == "" {
		return collectionError(fmt.Errorf("%w: authentication file path is empty", ErrInvalidAuth))
	}
	return nil
}

func (a Adapter) postRPC(ctx context.Context, token, method string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint+rpcPath(method), bytes.NewBufferString("{}"))
	if err != nil {
		return nil, fmt.Errorf("%w: Cursor quota endpoint is invalid", ErrInvalidResponse)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	response, err := a.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: Cursor quota request canceled or timed out: %w", ErrTransient, ctx.Err())
		}
		return nil, fmt.Errorf("%w: Cursor quota request failed", ErrTransient)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return nil, ErrRevoked
	case http.StatusForbidden:
		return nil, ErrForbidden
	case http.StatusTooManyRequests:
		return nil, &RetryError{RetryAt: parseRetryAfter(response.Header.Get("Retry-After"), a.now())}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: Cursor quota endpoint returned HTTP %d", ErrTransient, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: Cursor quota response could not be read", ErrTransient)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("%w: Cursor quota response is too large", ErrInvalidResponse)
	}
	return body, nil
}

func Failure(err error) (cache.FailureKind, time.Time) {
	if err == nil {
		return cache.FailureNone, time.Time{}
	}
	if retry, ok := errors.AsType[*RetryError](err); ok {
		return cache.FailureTransient, retry.RetryAt
	}
	switch {
	case errors.Is(err, evidence.ErrWrongAccount):
		return cache.FailureAccountMismatch, time.Time{}
	case errors.Is(err, ErrRevoked):
		return cache.FailureRevoked, time.Time{}
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, ErrTransient):
		return cache.FailureTransient, time.Time{}
	default:
		return cache.FailurePermanent, time.Time{}
	}
}

func rpcPath(method string) string { return "/aiserver.v1.DashboardService/" + method }

func collectionError(err error) error {
	return fmt.Errorf("%w: %w", evidence.ErrProviderUnavailable, err)
}

func safeFailure(err error) string {
	switch {
	case errors.Is(err, ErrRevoked):
		return "Cursor CLI authentication was rejected"
	case errors.Is(err, ErrForbidden):
		return "Cursor quota request was forbidden; the cause is unknown"
	case errors.Is(err, ErrTransient):
		return "Cursor quota request failed transiently"
	default:
		return "Cursor supplemental quota response is invalid"
	}
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
