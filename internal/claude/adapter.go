package claude

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
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
	maxAuthBytes     = 1 << 20
	maxResponseBytes = 1 << 20
	oauthBeta        = "oauth-2025-04-20"
	defaultTimeout   = 15 * time.Second
	profileURL       = "https://api.anthropic.com/api/oauth/profile"
	usageURL         = "https://api.anthropic.com/api/oauth/usage"
)

var (
	ErrAccountMismatch       = errors.New("Claude quota account mismatch")
	ErrAuthorizationRejected = errors.New("Claude authentication was rejected")
	ErrTransient             = errors.New("transient Claude quota failure")
)

type Options struct {
	AuthFile        string
	ProfileEndpoint string
	UsageEndpoint   string
	Client          *http.Client
	Timeout         time.Duration
	Now             func() time.Time
}

type Adapter struct {
	authFile        string
	profileEndpoint string
	usageEndpoint   string
	client          *http.Client
	timeout         time.Duration
	now             func() time.Time
}

type RetryError struct{ RetryAt time.Time }

func (e *RetryError) Error() string {
	if e.RetryAt.IsZero() {
		return "Claude quota endpoint is rate limited"
	}
	return "Claude quota endpoint is rate limited until " + e.RetryAt.UTC().Format(time.RFC3339)
}

func (e *RetryError) Unwrap() error { return ErrTransient }

func Default() Adapter {
	root, set := os.LookupEnv("CLAUDE_CONFIG_DIR")
	if !set {
		home, err := os.UserHomeDir()
		if err == nil {
			root = filepath.Join(home, ".claude")
		}
	}
	if root == "" {
		return New(Options{})
	}
	return New(Options{AuthFile: filepath.Join(root, ".credentials.json")})
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
	profileEndpoint := options.ProfileEndpoint
	if profileEndpoint == "" {
		profileEndpoint = profileURL
	}
	usageEndpoint := options.UsageEndpoint
	if usageEndpoint == "" {
		usageEndpoint = usageURL
	}
	return Adapter{authFile: options.AuthFile, profileEndpoint: profileEndpoint, usageEndpoint: usageEndpoint, client: &clientCopy, timeout: timeout, now: now}
}

func (a Adapter) CacheBinding(ctx context.Context, request evidence.Request) (cache.Binding, error) {
	if err := validateRequest(request); err != nil {
		return cache.Binding{}, err
	}
	if err := ctx.Err(); err != nil {
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
	return cache.Binding{Provider: "claude", Profile: "default", ResponseBoundary: "profile_usage", SourceKind: "native_file_http", SourceName: "claude_credentials_json", CredentialFingerprint: fingerprint}, nil
}

func (a Adapter) Observe(ctx context.Context, request evidence.Request) (evidence.Observation, error) {
	if err := validateRequest(request); err != nil {
		return evidence.Observation{}, err
	}
	if err := ctx.Err(); err != nil {
		return evidence.Observation{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	credentials, err := readCredentials(a.authFile, a.now())
	if err != nil {
		return evidence.Observation{}, collectionError(err)
	}
	profile, err := a.fetchProfile(ctx, credentials.accessToken)
	if err != nil {
		return evidence.Observation{}, collectionError(err)
	}
	if request.Account != "" && request.Account != profile.accountID {
		return evidence.Observation{}, evidence.ErrWrongAccount
	}
	usage, err := a.fetchUsage(ctx, credentials.accessToken)
	if err != nil {
		return evidence.Observation{}, collectionError(err)
	}
	observation, err := normalize(usage, profile.accountID, a.now())
	if err != nil {
		return evidence.Observation{}, collectionError(err)
	}
	return observation, nil
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

func validateRequest(request evidence.Request) error {
	if request.Provider != "claude" || request.Profile != "default" || request.All {
		return fmt.Errorf("%w: Claude native source requires --provider claude --profile default", evidence.ErrInvalidSelection)
	}
	return nil
}

func collectionError(err error) error {
	return fmt.Errorf("%w: %w", evidence.ErrProviderUnavailable, err)
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
