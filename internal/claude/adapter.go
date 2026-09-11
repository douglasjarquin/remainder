package claude

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
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

var (
	fileSourceIdentity     = evidence.SourceIdentity{Kind: "native_file_http", Name: "claude_credentials_json"}
	keychainSourceIdentity = evidence.SourceIdentity{Kind: "native_keychain_http", Name: "claude_keychain"}
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
	KeychainReader  func(context.Context) ([]byte, error)
	Client          *http.Client
	Timeout         time.Duration
	Now             func() time.Time

	goos string
}

type Adapter struct {
	authFile            string
	profileEndpoint     string
	usageEndpoint       string
	keychainReader      func(context.Context) ([]byte, error)
	allowKeychainPrompt bool
	client              *http.Client
	timeout             time.Duration
	now                 func() time.Time
	goos                string
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
	goos := options.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	keychainReader := options.KeychainReader
	if keychainReader == nil {
		keychainReader = readMacKeychain
	}
	return Adapter{authFile: options.AuthFile, profileEndpoint: profileEndpoint, usageEndpoint: usageEndpoint, keychainReader: keychainReader, client: &clientCopy, timeout: timeout, now: now, goos: goos}
}

func (a Adapter) WithKeychainPrompt() Adapter {
	a.allowKeychainPrompt = true
	return a
}

func (a Adapter) CacheBinding(ctx context.Context, request evidence.Request) (cache.Binding, error) {
	if err := validateRequest(request); err != nil {
		return cache.Binding{}, err
	}
	if err := ctx.Err(); err != nil {
		return cache.Binding{}, err
	}
	info, err := inspectAuthFile(a.authFile)
	if err == nil {
		identity := fmt.Sprintf("%s\x00%d\x00%d\x00%d", filepath.Clean(a.authFile), info.Size(), info.ModTime().UnixNano(), info.Mode())
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			identity += fmt.Sprintf("\x00%d\x00%d", stat.Dev, stat.Ino)
		}
		fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
		return cache.Binding{Provider: "claude", Profile: "default", ResponseBoundary: "profile_usage", SourceKind: fileSourceIdentity.Kind, SourceName: fileSourceIdentity.Name, CredentialFingerprint: fingerprint}, nil
	}
	if !errors.Is(err, errAuthFileMissing) || a.goos != "darwin" {
		return cache.Binding{}, collectionError(err)
	}
	account, err := keychainAccount()
	if err != nil {
		return cache.Binding{}, collectionError(err)
	}
	identity := "claude\x00default\x00" + keychainSourceIdentity.Kind + "\x00" + keychainSourceIdentity.Name + "\x00" + account
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
	return cache.Binding{Provider: "claude", Profile: "default", ResponseBoundary: "profile_usage", SourceKind: keychainSourceIdentity.Kind, SourceName: keychainSourceIdentity.Name, CredentialFingerprint: fingerprint}, nil
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
	credentials, source, err := a.acquireCredentials(ctx)
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
	observation, err := normalize(usage, profile.accountID, a.now(), source)
	if err != nil {
		return evidence.Observation{}, collectionError(err)
	}
	return observation, nil
}

func (a Adapter) acquireCredentials(ctx context.Context) (credentials, evidence.SourceIdentity, error) {
	if _, err := inspectAuthFile(a.authFile); err != nil {
		if !errors.Is(err, errAuthFileMissing) || a.goos != "darwin" {
			return credentials{}, evidence.SourceIdentity{}, err
		}
		if !a.allowKeychainPrompt {
			return credentials{}, evidence.SourceIdentity{}, ErrKeychainPromptRequired
		}
		creds, err := a.readKeychainCredentials(ctx)
		return creds, keychainSourceIdentity, err
	}
	creds, err := readCredentials(a.authFile, a.now())
	return creds, fileSourceIdentity, err
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
