package cursor

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
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
	ErrInvalidResponse = errors.New("Cursor quota response is invalid")

	errForbidden = errors.New("Cursor quota request was forbidden")
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
			return evidence.Observation{}, cache.FetchFailure(ErrKeychainPromptRequired)
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
		return evidence.Observation{}, cache.FetchFailure(err)
	}
	usageBody, err := a.postRPC(ctx, token, "GetCurrentPeriodUsage")
	if err != nil {
		return evidence.Observation{}, cache.FetchFailure(err)
	}
	usage, err := decodeUsage(usageBody)
	if err != nil {
		return evidence.Observation{}, cache.FetchFailure(err)
	}
	observedAt := a.now()

	var plan planResponse
	var sand sandResponse
	failures := make([]evidence.Failure, 0, 2)
	if body, rpcErr := a.postRPC(ctx, token, "GetPlanInfo"); callerCtx.Err() != nil {
		return evidence.Observation{}, callerCtx.Err()
	} else if revoked(rpcErr) {
		return evidence.Observation{}, cache.FetchFailure(rpcErr)
	} else if rpcErr != nil {
		failures = append(failures, evidence.Failure{Scope: "plan", Message: safeFailure(rpcErr)})
	} else if decodeErr := json.Unmarshal(body, &plan); decodeErr != nil {
		failures = append(failures, evidence.Failure{Scope: "plan", Message: "Cursor plan response is malformed JSON"})
	}
	if body, rpcErr := a.postRPC(ctx, token, "GetSandUsageStatus"); callerCtx.Err() != nil {
		return evidence.Observation{}, callerCtx.Err()
	} else if revoked(rpcErr) {
		return evidence.Observation{}, cache.FetchFailure(rpcErr)
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
		return cache.FetchFailure(ErrUnsupported)
	}
	if a.pathErr != nil {
		return cache.FetchFailure(a.pathErr)
	}
	if a.goos == "darwin" && a.configFile == "" {
		return cache.FetchFailure(fmt.Errorf("%w: configuration file path is empty", ErrInvalidAuth))
	}
	if a.goos == "linux" && a.authFile == "" {
		return cache.FetchFailure(fmt.Errorf("%w: authentication file path is empty", ErrInvalidAuth))
	}
	return nil
}

func rpcPath(method string) string { return "/aiserver.v1.DashboardService/" + method }

func revoked(err error) bool {
	kind, _ := cache.Classify(err)
	return kind == cache.FailureRevoked
}

func safeFailure(err error) string {
	kind, _ := cache.Classify(err)
	switch {
	case kind == cache.FailureRevoked:
		return "Cursor CLI authentication was rejected"
	case errors.Is(err, errForbidden):
		return "Cursor quota request was forbidden; the cause is unknown"
	case kind == cache.FailureTransient:
		return "Cursor quota request failed transiently"
	default:
		return "Cursor supplemental quota response is invalid"
	}
}
