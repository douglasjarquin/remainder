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

var sourceIdentity = evidence.SourceIdentity{Kind: "native_file_http", Name: "grok_auth_json"}

const (
	defaultEndpoint  = "https://grok.com/grok_api_v2.GrokBuildBilling/GetGrokCreditsConfig"
	defaultTimeout   = 15 * time.Second
	maxResponseBytes = 64 << 10
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
		return cache.Binding{}, cache.FetchFailure(err)
	}
	file, info, err := openAuthFile(path)
	if err != nil {
		return cache.Binding{}, cache.FetchFailure(err)
	}
	defer file.Close()
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(authIdentity(path, info))))
	return cache.Binding{
		Provider:              "grok",
		Profile:               "default",
		ResponseBoundary:      "grok_credits_config",
		SourceKind:            sourceIdentity.Kind,
		SourceName:            sourceIdentity.Name,
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
		return evidence.Observation{}, cache.FetchFailure(err)
	}
	credential, err := readCredentials(path, a.now())
	if err != nil {
		return evidence.Observation{}, cache.FetchFailure(err)
	}
	if request.Account != "" {
		return evidence.Observation{}, fmt.Errorf("%w: Grok quota response cannot verify an account selector", evidence.ErrWrongAccount)
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	observation, err := a.fetch(ctx, credential)
	if err != nil {
		return evidence.Observation{}, cache.FetchFailure(err)
	}
	return observation, nil
}

func validateRequest(request evidence.Request) error {
	if request.Provider != "grok" || request.Profile != "default" || request.All {
		return fmt.Errorf("%w: Grok native source requires --provider grok --profile default", evidence.ErrInvalidSelection)
	}
	return nil
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
		return evidence.Observation{}, cache.RequestFailure(ctx, "Grok")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return evidence.Observation{}, errAuthorizationRejected()
	case http.StatusTooManyRequests:
		return evidence.Observation{}, &cache.FetchError{Kind: cache.FailureTransient, RetryAt: cache.ParseRetryAfter(response.Header.Get("Retry-After"), a.now()), Err: errors.New("Grok quota endpoint is rate limited")}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return evidence.Observation{}, &cache.FetchError{Kind: cache.FailureTransient, Err: fmt.Errorf("Grok quota endpoint returned HTTP %d", response.StatusCode)}
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
		if _, ok := errors.AsType[*cache.FetchError](err); ok {
			return evidence.Observation{}, err
		}
		return evidence.Observation{}, &cache.FetchError{Kind: cache.FailureTransient, Err: errors.New("Grok quota response has invalid gRPC framing")}
	}
	observation, err := normalize(payload, a.now())
	if err != nil {
		return evidence.Observation{}, &cache.FetchError{Kind: cache.FailureTransient, Err: errors.New("Grok quota response has invalid protobuf")}
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
		return nil, &cache.FetchError{Kind: cache.FailureTransient, Err: errors.New("Grok quota response could not be read")}
	}
	if len(body) > maxResponseBytes {
		return nil, errors.New("Grok quota response is too large")
	}
	return body, nil
}
