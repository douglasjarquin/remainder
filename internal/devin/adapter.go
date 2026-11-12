package devin

import (
	"bytes"
	"context"
	"crypto/sha256"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

var sourceIdentity = evidence.SourceIdentity{Kind: "native_file_http", Name: "devin_credentials_toml"}

const (
	defaultEndpoint  = "https://server.codeium.com"
	defaultTimeout   = 15 * time.Second
	maxResponseBytes = 1 << 20
	statusPath       = "/exa.seat_management_pb.SeatManagementService/GetUserStatus"
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

type statusRequest struct {
	Metadata requestMetadata `json:"metadata"`
}

type requestMetadata struct {
	APIKey           string `json:"apiKey"`
	IDEName          string `json:"ideName"`
	IDEVersion       string `json:"ideVersion"`
	ExtensionName    string `json:"extensionName"`
	ExtensionVersion string `json:"extensionVersion"`
	Locale           string `json:"locale"`
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
	return Adapter{authFile: options.AuthFile, client: &clientCopy, endpoint: options.Endpoint, timeout: timeout, now: now}
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
		Provider:              "devin",
		Profile:               "default",
		ResponseBoundary:      "user_status",
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
	credential, err := readCredentials(path)
	if err != nil {
		return evidence.Observation{}, cache.FetchFailure(err)
	}
	endpoint := a.endpoint
	if endpoint == "" {
		endpoint = credential.apiHost
	}
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	observation, err := a.fetch(ctx, endpoint, credential.apiKey, request.Account)
	if err != nil {
		return evidence.Observation{}, cache.FetchFailure(err)
	}
	return observation, nil
}

func validateRequest(request evidence.Request) error {
	if request.Provider != "devin" || request.Profile != "default" || request.All {
		return fmt.Errorf("%w: Devin native source requires --provider devin --profile default", evidence.ErrInvalidSelection)
	}
	return nil
}

func (a Adapter) fetch(ctx context.Context, endpoint, apiKey, account string) (evidence.Observation, error) {
	body, err := json.Marshal(statusRequest{Metadata: requestMetadata{
		APIKey:           apiKey,
		IDEName:          "remainder",
		IDEVersion:       "0.0.0",
		ExtensionName:    "remainder",
		ExtensionVersion: "0.0.0",
		Locale:           "en",
	}})
	if err != nil {
		return evidence.Observation{}, errors.New("Devin quota request could not be encoded")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(endpoint, "/")+statusPath, bytes.NewReader(body))
	if err != nil {
		return evidence.Observation{}, errors.New("Devin quota endpoint is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	request.Header.Set("Content-Type", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		return evidence.Observation{}, cache.RequestFailure(ctx, "Devin")
	}
	defer response.Body.Close()
	raw, err := readBounded(response)
	if err != nil {
		return evidence.Observation{}, err
	}
	if response.StatusCode != http.StatusOK {
		return evidence.Observation{}, a.statusFailure(response, raw)
	}
	var payload statusResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return evidence.Observation{}, &cache.FetchError{Kind: cache.FailureTransient, Err: errors.New("Devin quota response is malformed")}
	}
	return normalize(&payload, account, a.now())
}

func (a Adapter) statusFailure(response *http.Response, body []byte) error {
	var payload connectError
	code := ""
	if err := json.Unmarshal(body, &payload); err == nil {
		code = payload.Code
	}
	switch code {
	case "unauthenticated", "permission_denied", "invalid_argument":
		return &cache.FetchError{Kind: cache.FailureRevoked, Err: errors.New("Devin authentication was rejected")}
	case "resource_exhausted":
		return &cache.FetchError{Kind: cache.FailureTransient, RetryAt: cache.ParseRetryAfter(response.Header.Get("Retry-After"), a.now()), Err: errors.New("Devin quota endpoint is rate limited")}
	case "unavailable", "deadline_exceeded", "internal", "aborted", "data_loss", "unknown":
		return &cache.FetchError{Kind: cache.FailureTransient, Err: fmt.Errorf("Devin quota endpoint returned %s", code)}
	}
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &cache.FetchError{Kind: cache.FailureRevoked, Err: errors.New("Devin authentication was rejected")}
	case http.StatusTooManyRequests:
		return &cache.FetchError{Kind: cache.FailureTransient, RetryAt: cache.ParseRetryAfter(response.Header.Get("Retry-After"), a.now()), Err: errors.New("Devin quota endpoint is rate limited")}
	default:
		return &cache.FetchError{Kind: cache.FailureTransient, Err: fmt.Errorf("Devin quota endpoint returned HTTP %d", response.StatusCode)}
	}
}

func readBounded(response *http.Response) ([]byte, error) {
	if raw := response.Header.Get("Content-Length"); raw != "" {
		length, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || length > maxResponseBytes {
			return nil, errors.New("Devin quota response is too large")
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, &cache.FetchError{Kind: cache.FailureTransient, Err: errors.New("Devin quota response could not be read")}
	}
	if len(body) > maxResponseBytes {
		return nil, errors.New("Devin quota response is too large")
	}
	return body, nil
}
