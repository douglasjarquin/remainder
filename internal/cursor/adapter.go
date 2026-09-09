package cursor

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
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
	AuthFile       string
	ConfigFile     string
	KeychainReader func(context.Context) (string, error)
	Endpoint       string
	Client         *http.Client
	Timeout        time.Duration
	Now            func() time.Time

	goos string
}

type Adapter struct {
	authFile            string
	configFile          string
	pathErr             error
	keychainReader      func(context.Context) (string, error)
	allowKeychainPrompt bool
	endpoint            string
	client              *http.Client
	timeout             time.Duration
	now                 func() time.Time
	goos                string
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
	options := Options{goos: goos}
	var err error
	if goos == "darwin" {
		options.ConfigFile, err = defaultMacConfigPath(getenv)
	} else {
		options.AuthFile, err = defaultAuthPath(getenv)
	}
	adapter := New(options)
	adapter.pathErr = err
	return adapter
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
	goos := options.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	endpoint := options.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	keychainReader := options.KeychainReader
	if keychainReader == nil {
		keychainReader = readMacKeychain
	}
	return Adapter{authFile: options.AuthFile, configFile: options.ConfigFile, keychainReader: keychainReader, endpoint: endpoint, client: &clientCopy, timeout: timeout, now: now, goos: goos}
}

func (a Adapter) WithKeychainPrompt() Adapter {
	a.allowKeychainPrompt = true
	return a
}

func (a Adapter) Observe(ctx context.Context, request evidence.Request) (evidence.Observation, error) {
	if err := a.validateRequest(ctx, request); err != nil {
		return evidence.Observation{}, err
	}
	callerCtx := ctx
	ctx, cancel := context.WithTimeout(callerCtx, a.timeout)
	defer cancel()
	var token string
	var source evidence.SourceIdentity
	var err error
	if a.goos == "darwin" {
		if !a.allowKeychainPrompt {
			return evidence.Observation{}, collectionError(ErrKeychainPromptRequired)
		}
		token, err = a.readMacAccessToken(ctx)
		source = evidence.SourceIdentity{Kind: "native_keychain_http", Name: "cursor_cli_keychain"}
	} else {
		token, err = readAccessToken(a.authFile, a.now())
		source = evidence.SourceIdentity{Kind: "native_file_http", Name: "cursor_cli_auth_json"}
	}
	if err != nil {
		if callerErr := callerCtx.Err(); callerErr != nil {
			return evidence.Observation{}, callerErr
		}
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
	observedAt := a.now()

	var plan planResponse
	var sand sandResponse
	failures := make([]evidence.Failure, 0, 2)
	if body, rpcErr := a.postRPC(ctx, token, "GetPlanInfo"); callerCtx.Err() != nil {
		return evidence.Observation{}, callerCtx.Err()
	} else if errors.Is(rpcErr, ErrRevoked) {
		return evidence.Observation{}, collectionError(rpcErr)
	} else if rpcErr != nil {
		failures = append(failures, evidence.Failure{Scope: "plan", Message: safeFailure(rpcErr)})
	} else if decodeErr := json.Unmarshal(body, &plan); decodeErr != nil {
		failures = append(failures, evidence.Failure{Scope: "plan", Message: "Cursor plan response is malformed JSON"})
	}
	if body, rpcErr := a.postRPC(ctx, token, "GetSandUsageStatus"); callerCtx.Err() != nil {
		return evidence.Observation{}, callerCtx.Err()
	} else if errors.Is(rpcErr, ErrRevoked) {
		return evidence.Observation{}, collectionError(rpcErr)
	} else if rpcErr != nil {
		failures = append(failures, evidence.Failure{Scope: "grok_bot", Message: safeFailure(rpcErr)})
	} else if decodeErr := json.Unmarshal(body, &sand); decodeErr != nil {
		failures = append(failures, evidence.Failure{Scope: "grok_bot", Message: "Cursor Grok Bot response is malformed JSON"})
	}
	if err := callerCtx.Err(); err != nil {
		return evidence.Observation{}, err
	}
	return normalize(usage, plan, sand, failures, observedAt, source)
}

func (a Adapter) validateRequest(ctx context.Context, request evidence.Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if request.Provider != "cursor" || request.Profile != "default" || request.All {
		return fmt.Errorf("%w: Cursor CLI file source requires --provider cursor --profile default", evidence.ErrInvalidSelection)
	}
	if request.Account != "" {
		return evidence.ErrWrongAccount
	}
	if a.goos != "linux" && a.goos != "darwin" {
		return collectionError(ErrUnsupported)
	}
	if a.pathErr != nil {
		return collectionError(a.pathErr)
	}
	if a.goos == "darwin" && a.configFile == "" {
		return collectionError(fmt.Errorf("%w: configuration file path is empty", ErrInvalidAuth))
	}
	if a.goos == "linux" && a.authFile == "" {
		return collectionError(fmt.Errorf("%w: authentication file path is empty", ErrInvalidAuth))
	}
	return nil
}

func (Adapter) Failure(err error) (cache.FailureKind, time.Time) {
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
