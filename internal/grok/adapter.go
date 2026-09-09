package grok

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

const (
	defaultEndpoint  = "https://grok.com/grok_api_v2.GrokBuildBilling/GetGrokCreditsConfig"
	defaultTimeout   = 15 * time.Second
	maxResponseBytes = 64 << 10
)

var (
	ErrAccountMismatch       = errors.New("Grok quota account mismatch")
	ErrAuthorizationRejected = errors.New("Grok authentication was rejected")
	ErrTransient             = errors.New("transient Grok quota failure")
)

type Options struct {
	AuthFile string
	Client   *http.Client
	Endpoint string
	Timeout  time.Duration
	Now      func() time.Time
}

type Adapter struct {
	authFile string
	client   *http.Client
	endpoint string
	timeout  time.Duration
	now      func() time.Time
}

type RetryError struct {
	RetryAt time.Time
}

func (e *RetryError) Error() string {
	if e.RetryAt.IsZero() {
		return "Grok quota endpoint is rate limited"
	}
	return "Grok quota endpoint is rate limited until " + e.RetryAt.UTC().Format(time.RFC3339)
}

func (e *RetryError) Unwrap() error {
	return ErrTransient
}

func Default() Adapter {
	return New(Options{})
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
	endpoint := options.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	return Adapter{authFile: options.AuthFile, client: &clientCopy, endpoint: endpoint, timeout: timeout, now: now}
}

func (a Adapter) CacheBinding(ctx context.Context, request evidence.Request) (cache.Binding, error) {
	if err := validateRequest(request); err != nil {
		return cache.Binding{}, err
	}
	if err := ctx.Err(); err != nil {
		return cache.Binding{}, err
	}
	path, err := resolveAuthFile(a.authFile)
	if err != nil {
		return cache.Binding{}, collectionError(err)
	}
	file, info, err := openAuthFile(path)
	if err != nil {
		return cache.Binding{}, collectionError(err)
	}
	defer file.Close()
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(authIdentity(path, info))))
	return cache.Binding{
		Provider:              "grok",
		Profile:               "default",
		ResponseBoundary:      "grok_credits_config",
		SourceKind:            "native_file_http",
		SourceName:            "grok_auth_json",
		CredentialFingerprint: fingerprint,
	}, nil
}

func (a Adapter) Observe(ctx context.Context, request evidence.Request) (evidence.Observation, error) {
	if err := validateRequest(request); err != nil {
		return evidence.Observation{}, err
	}
	if err := ctx.Err(); err != nil {
		return evidence.Observation{}, err
	}
	path, err := resolveAuthFile(a.authFile)
	if err != nil {
		return evidence.Observation{}, collectionError(err)
	}
	credential, err := readCredentials(path, a.now())
	if err != nil {
		return evidence.Observation{}, collectionError(err)
	}
	if request.Account != "" {
		return evidence.Observation{}, fmt.Errorf("%w: Grok quota response cannot verify an account selector", evidence.ErrWrongAccount)
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	observation, err := a.fetch(ctx, credential)
	if err != nil {
		return evidence.Observation{}, collectionError(err)
	}
	return observation, nil
}

func validateRequest(request evidence.Request) error {
	if request.Provider != "grok" || request.Profile != "default" || request.All {
		return fmt.Errorf("%w: Grok native source requires --provider grok --profile default", evidence.ErrInvalidSelection)
	}
	return nil
}

func collectionError(err error) error {
	return fmt.Errorf("%w: %w", evidence.ErrProviderUnavailable, err)
}

func (a Adapter) fetch(ctx context.Context, credential credentials) (evidence.Observation, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader([]byte{0, 0, 0, 0, 0}))
	if err != nil {
		return evidence.Observation{}, errors.New("Grok quota endpoint is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credential.token)
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Content-Type", "application/grpc-web+proto")
	request.Header.Set("Origin", "https://grok.com")
	request.Header.Set("Referer", "https://grok.com/?_s=usage")
	request.Header.Set("X-Grpc-Web", "1")
	request.Header.Set("X-User-Agent", "connect-es/2.1.1")
	response, err := a.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return evidence.Observation{}, fmt.Errorf("%w: Grok quota request canceled or timed out: %w", ErrTransient, ctx.Err())
		}
		return evidence.Observation{}, fmt.Errorf("%w: Grok quota request failed", ErrTransient)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return evidence.Observation{}, ErrAuthorizationRejected
	case http.StatusTooManyRequests:
		return evidence.Observation{}, &RetryError{RetryAt: parseRetryAfter(response.Header.Get("Retry-After"), a.now())}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return evidence.Observation{}, fmt.Errorf("%w: Grok quota endpoint returned HTTP %d", ErrTransient, response.StatusCode)
	}
	if err := grpcStatusError(response.Header.Get("Grpc-Status"), response.Header.Get("Grpc-Message")); err != nil {
		return evidence.Observation{}, err
	}
	body, err := readBounded(response)
	if err != nil {
		return evidence.Observation{}, err
	}
	if err := grpcStatusError(response.Trailer.Get("Grpc-Status"), response.Trailer.Get("Grpc-Message")); err != nil {
		return evidence.Observation{}, err
	}
	payload, err := decodeGRPCWeb(body)
	if err != nil {
		if errors.Is(err, ErrAuthorizationRejected) || errors.Is(err, ErrTransient) {
			return evidence.Observation{}, err
		}
		return evidence.Observation{}, fmt.Errorf("%w: Grok quota response has invalid gRPC framing", ErrTransient)
	}
	observation, err := normalize(payload, a.now())
	if err != nil {
		return evidence.Observation{}, fmt.Errorf("%w: Grok quota response has invalid protobuf", ErrTransient)
	}
	return observation, nil
}

func readBounded(response *http.Response) ([]byte, error) {
	if raw := response.Header.Get("Content-Length"); raw != "" {
		length, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || length > maxResponseBytes {
			return nil, errors.New("Grok quota response is too large")
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: Grok quota response could not be read", ErrTransient)
	}
	if len(body) > maxResponseBytes {
		return nil, errors.New("Grok quota response is too large")
	}
	return body, nil
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
